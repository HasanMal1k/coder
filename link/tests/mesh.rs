//! Two daemons and a stand-in for the control plane, all in this process and
//! over real connections on this computer: the daemons are told of each
//! other, find each other, prove themselves, and say so.

use std::collections::HashMap;
use std::net::SocketAddr;
use std::sync::{Arc, Mutex};
use std::time::Duration;

use linkd::Config;
use linkd::proto::link_server::{Link, LinkServer};
use linkd::proto::{Frame, Hello, Mesh, Peer, Peers, Ping, frame::Body};
use tokio::sync::mpsc;
use tokio_stream::wrappers::ReceiverStream;
use tonic::transport::Server;
use tonic::{Request, Response, Status, Streaming};

const A: &str = "11111111-1111-4111-8111-111111111111";
const B: &str = "22222222-2222-4222-8222-222222222222";

/// A port nothing is listening on.
fn free_port() -> SocketAddr {
    let listener = std::net::TcpListener::bind("127.0.0.1:0").unwrap();
    listener.local_addr().unwrap()
}

/// What the stand-in control plane tells each machine, and what each has
/// reported back.
#[derive(Default)]
struct Parent {
    peers: Mutex<HashMap<String, Peers>>,
    linked: Mutex<HashMap<String, Vec<String>>>,
}

struct ParentService(Arc<Parent>);

#[tonic::async_trait]
impl Link for ParentService {
    type ChannelStream = ReceiverStream<Result<Frame, Status>>;

    async fn channel(
        &self,
        request: Request<Streaming<Frame>>,
    ) -> Result<Response<Self::ChannelStream>, Status> {
        // "Bearer <instance id>.<token>", as a machine presents itself.
        let identity = request
            .metadata()
            .get("authorization")
            .and_then(|value| value.to_str().ok())
            .and_then(|value| value.strip_prefix("Bearer "))
            .and_then(|value| value.split_once('.'))
            .map(|(id, _)| id.to_string())
            .ok_or_else(|| Status::unauthenticated("no credentials"))?;

        let mut incoming = request.into_inner();
        let (outbox, sending) = mpsc::channel(16);
        let parent = self.0.clone();

        tokio::spawn(async move {
            let frame = |body| {
                Ok(Frame {
                    body: Some(body),
                    ..Frame::default()
                })
            };

            // The handshake the real control plane makes.
            let Ok(Some(_hello)) = incoming.message().await else {
                return;
            };
            let hello = Hello {
                protocol: 1,
                name: "test parent".into(),
                ..Hello::default()
            };
            let _ = outbox.send(frame(Body::Hello(hello))).await;
            let _ = outbox
                .send(Ok(Frame {
                    id: 1,
                    body: Some(Body::Ping(Ping {})),
                    ..Frame::default()
                }))
                .await;

            let told = parent.peers.lock().unwrap().get(&identity).cloned();
            if let Some(peers) = told {
                let _ = outbox.send(frame(Body::Peers(peers))).await;
            }

            while let Ok(Some(received)) = incoming.message().await {
                if let Some(Body::Status(status)) = received.body {
                    parent
                        .linked
                        .lock()
                        .unwrap()
                        .insert(identity.clone(), status.linked);
                }
            }
        });

        Ok(Response::new(ReceiverStream::new(sending)))
    }
}

/// Starts the stand-in control plane and two daemons that are each told the
/// other is its peer, under the keys given. Returns what the daemons report.
async fn start(key_for_a: &[u8], key_for_b: &[u8]) -> Arc<Parent> {
    let (parent_at, a_at, b_at) = (free_port(), free_port(), free_port());

    let mesh = |key: &[u8], id: &str, name: &str, at: SocketAddr| Peers {
        meshes: vec![Mesh {
            id: "chat-1".into(),
            key: key.to_vec(),
            peers: vec![Peer {
                instance_id: id.into(),
                machine: name.into(),
                address: format!("http://{at}"),
            }],
        }],
    };

    let parent = Arc::new(Parent::default());
    {
        let mut peers = parent.peers.lock().unwrap();
        peers.insert(A.into(), mesh(key_for_a, B, "api", b_at));
        peers.insert(B.into(), mesh(key_for_b, A, "web", a_at));
    }

    let service = LinkServer::new(ParentService(parent.clone()));
    tokio::spawn(Server::builder().add_service(service).serve(parent_at));

    for (id, listen) in [(A, a_at), (B, b_at)] {
        tokio::spawn(linkd::run(Config {
            link_url: format!("http://{parent_at}"),
            instance_id: id.into(),
            token: "token".into(),
            listen,
        }));
    }

    parent
}

/// Waits for what each daemon reports to be `want`, for up to ten seconds.
async fn reports(parent: &Parent, want: &[(&str, &[&str])]) -> bool {
    for _ in 0..200 {
        let linked = parent.linked.lock().unwrap().clone();
        let all = want.iter().all(|(id, peers)| {
            linked
                .get(*id)
                .is_some_and(|got| got.iter().map(String::as_str).eq(peers.iter().copied()))
        });
        if all {
            return true;
        }
        tokio::time::sleep(Duration::from_millis(50)).await;
    }

    false
}

#[tokio::test]
async fn two_machines_find_and_prove_each_other() {
    let parent = start(
        b"the same key for both of them...",
        b"the same key for both of them...",
    )
    .await;

    assert!(
        reports(&parent, &[(A, &[B]), (B, &[A])]).await,
        "each should report a proven stream with the other; got {:?}",
        parent.linked.lock().unwrap()
    );
}

#[tokio::test]
async fn machines_with_different_keys_do_not_link() {
    let parent = start(
        b"one key.........................",
        b"another key.....................",
    )
    .await;

    // Both attach to the control plane and report, but never each other.
    assert!(
        reports(&parent, &[(A, &[]), (B, &[])]).await,
        "both should report in; got {:?}",
        parent.linked.lock().unwrap()
    );
    tokio::time::sleep(Duration::from_secs(3)).await;
    assert!(
        reports(&parent, &[(A, &[]), (B, &[])]).await,
        "machines without a shared key linked: {:?}",
        parent.linked.lock().unwrap()
    );
}
