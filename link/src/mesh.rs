//! Which machines this one works with, and which of them it has a proven
//! stream with right now.

use std::collections::{BTreeMap, HashMap, HashSet};
use std::sync::{Arc, Mutex};

use tokio::sync::watch;
use tokio::task::JoinHandle;

use crate::peer;
use crate::proto::Peers;

/// What this machine knows of its peers. The control plane says who they
/// are; the streams to them report here when they are proven and when they
/// end.
pub struct Mesh {
    /// This machine's ID.
    me: String,
    inner: Mutex<Inner>,
    /// The IDs of the peers there is a proven stream with, in order.
    linked: watch::Sender<Vec<String>>,
}

#[derive(Default)]
struct Inner {
    /// The meshes this machine is in, by ID.
    meshes: HashMap<String, Known>,
    /// The streams this machine keeps dialling, by peer.
    dialers: HashMap<String, Dialer>,
    /// How many proven streams there are with each peer. More than one
    /// only for the moment in which a new stream takes over from an old.
    streams: BTreeMap<String, u32>,
}

/// One mesh, as the control plane described it.
struct Known {
    key: Vec<u8>,
    peers: HashSet<String>,
}

/// A stream to a peer that this machine is the one to dial.
struct Dialer {
    to: Target,
    task: JoinHandle<()>,
}

/// Where a peer is, and the mesh whose key proves the two to each other.
#[derive(Clone, PartialEq, Eq)]
pub struct Target {
    pub peer: String,
    pub address: String,
    pub mesh: String,
    pub key: Vec<u8>,
}

impl Mesh {
    pub fn new(me: String) -> Mesh {
        Mesh {
            me,
            inner: Mutex::new(Inner::default()),
            linked: watch::channel(Vec::new()).0,
        }
    }

    pub fn me(&self) -> &str {
        &self.me
    }

    /// The peers there is a proven stream with, to be watched for changes.
    pub fn watch(&self) -> watch::Receiver<Vec<String>> {
        self.linked.subscribe()
    }

    /// Takes the control plane's word for who this machine's peers are. It
    /// replaces what was known before: streams to machines that are still
    /// peers, at the same address and under the same key, are left alone,
    /// and the rest are ended or begun.
    pub fn update(self: &Arc<Self>, told: Peers) {
        let mut meshes = HashMap::new();
        let mut wanted: HashMap<String, Target> = HashMap::new();

        // In a fixed order, so that two machines that share more than one
        // mesh agree on which one's key to use.
        let mut told = told.meshes;
        told.sort_by(|a, b| a.id.cmp(&b.id));

        for mesh in told {
            let mut peers = HashSet::new();
            for peer in mesh.peers {
                if peer.instance_id == self.me {
                    continue;
                }
                peers.insert(peer.instance_id.clone());

                // Of two machines, the one with the smaller ID dials. That
                // way there is one stream between them, not two.
                if self.me < peer.instance_id {
                    wanted.entry(peer.instance_id.clone()).or_insert(Target {
                        peer: peer.instance_id,
                        address: peer.address,
                        mesh: mesh.id.clone(),
                        key: mesh.key.clone(),
                    });
                }
            }
            meshes.insert(
                mesh.id,
                Known {
                    key: mesh.key,
                    peers,
                },
            );
        }

        let mut inner = self.inner.lock().unwrap();
        inner.meshes = meshes;

        // A stream that is no longer wanted as it is ends with its task.
        inner.dialers.retain(|peer, dialer| {
            let keep = wanted.get(peer) == Some(&dialer.to);
            if !keep {
                dialer.task.abort();
            }
            keep
        });

        for (peer, to) in wanted {
            inner.dialers.entry(peer).or_insert_with(|| Dialer {
                task: tokio::spawn(peer::dial(self.clone(), to.clone())),
                to,
            });
        }
    }

    /// The key that proves `peer` belongs to `mesh` with this machine, if
    /// the control plane has said it does.
    pub fn key_for(&self, mesh: &str, peer: &str) -> Option<Vec<u8>> {
        let inner = self.inner.lock().unwrap();
        let known = inner.meshes.get(mesh)?;

        known.peers.contains(peer).then(|| known.key.clone())
    }

    /// Records a proven stream with a peer, for as long as what is returned
    /// is kept.
    pub fn linked(self: &Arc<Self>, peer: String) -> Linked {
        {
            let mut inner = self.inner.lock().unwrap();
            *inner.streams.entry(peer.clone()).or_default() += 1;
            self.publish(&inner);
        }

        Linked {
            mesh: self.clone(),
            peer,
        }
    }

    fn publish(&self, inner: &Inner) {
        let now: Vec<String> = inner.streams.keys().cloned().collect();
        // Only a change is news.
        self.linked.send_if_modified(|was| {
            let changed = *was != now;
            if changed {
                *was = now;
            }
            changed
        });
    }
}

/// A proven stream with a peer. When it is dropped, the stream is over.
pub struct Linked {
    mesh: Arc<Mesh>,
    peer: String,
}

impl Drop for Linked {
    fn drop(&mut self) {
        let mut inner = self.mesh.inner.lock().unwrap();

        if let Some(count) = inner.streams.get_mut(&self.peer) {
            *count -= 1;
            if *count == 0 {
                inner.streams.remove(&self.peer);
            }
        }
        self.mesh.publish(&inner);
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn a_peer_is_linked_while_a_stream_with_it_is_kept() {
        let mesh = Arc::new(Mesh::new("a".into()));
        let seen = mesh.watch();

        let first = mesh.linked("b".into());
        let second = mesh.linked("c".into());
        assert_eq!(*seen.borrow(), ["b", "c"]);

        drop(first);
        assert_eq!(*seen.borrow(), ["c"]);
        drop(second);
        assert!(seen.borrow().is_empty());
    }

    #[test]
    fn a_stream_taking_over_from_another_does_not_unlink_the_peer() {
        let mesh = Arc::new(Mesh::new("a".into()));
        let seen = mesh.watch();

        let old = mesh.linked("b".into());
        let new = mesh.linked("b".into());
        drop(old);
        assert_eq!(*seen.borrow(), ["b"]);

        drop(new);
        assert!(seen.borrow().is_empty());
    }

    #[test]
    fn a_key_is_given_only_for_a_peer_in_that_mesh() {
        use crate::proto::{Mesh as ToldMesh, Peer};

        let mesh = Arc::new(Mesh::new("b".into()));
        // "a" is smaller than this machine's ID, so nothing is dialled and
        // no runtime is needed.
        mesh.update(Peers {
            meshes: vec![ToldMesh {
                id: "chat-1".into(),
                key: b"key-1".to_vec(),
                peers: vec![Peer {
                    instance_id: "a".into(),
                    machine: "web".into(),
                    address: "http://web:7600".into(),
                }],
            }],
        });

        assert_eq!(mesh.key_for("chat-1", "a").as_deref(), Some(&b"key-1"[..]));
        assert!(mesh.key_for("chat-1", "stranger").is_none());
        assert!(mesh.key_for("chat-2", "a").is_none());

        // What the control plane says next replaces it.
        mesh.update(Peers { meshes: vec![] });
        assert!(mesh.key_for("chat-1", "a").is_none());
    }
}
