//! The streams between this machine and its peers, opened directly across
//! their owner's private network.
//!
//! Of two machines the one with the smaller ID dials and the other accepts.
//! Each proves to the other that it holds their mesh's key before the stream
//! counts for anything.

use std::net::SocketAddr;
use std::sync::Arc;
use std::time::Instant;

use tokio::sync::mpsc;
use tokio::time::timeout;
use tokio_stream::wrappers::ReceiverStream;
use tonic::transport::{Endpoint, Server};
use tonic::{Request, Response, Status, Streaming};

use crate::backoff::Backoff;
use crate::mesh::{Mesh, Target};
use crate::proof::{self, Claim, Role};
use crate::proto::link_client::LinkClient;
use crate::proto::link_server::{Link, LinkServer};
use crate::proto::{Frame, Hello, Proof, frame::Body};
use crate::wire::{
    DIAL_TIMEOUT, HANDSHAKE_TIMEOUT, KEEPALIVE_EVERY, KEEPALIVE_TIMEOUT, MAX_FRAME, OUTBOX,
    PROTOCOL, answer, causes, name, refusal,
};

fn frame(body: Body) -> Frame {
    Frame {
        body: Some(body),
        ..Frame::default()
    }
}

/// Listens for peers until it cannot, and returns why.
pub async fn serve(listen: SocketAddr, mesh: Arc<Mesh>) -> String {
    let service = LinkServer::new(Accept { mesh }).max_decoding_message_size(MAX_FRAME);

    let served = Server::builder()
        .http2_keepalive_interval(Some(KEEPALIVE_EVERY))
        .http2_keepalive_timeout(Some(KEEPALIVE_TIMEOUT))
        .tcp_nodelay(true)
        .add_service(service)
        .serve(listen)
        .await;

    match served {
        Ok(()) => format!("stopped listening for peers on {listen}"),
        Err(problem) => format!("listen for peers on {listen}: {}", causes(&problem)),
    }
}

/// The side that is dialled.
struct Accept {
    mesh: Arc<Mesh>,
}

#[tonic::async_trait]
impl Link for Accept {
    type ChannelStream = ReceiverStream<Result<Frame, Status>>;

    async fn channel(
        &self,
        request: Request<Streaming<Frame>>,
    ) -> Result<Response<Self::ChannelStream>, Status> {
        let mut incoming = request.into_inner();
        let me = self.mesh.me().to_string();

        // The dialler says who it is, and for which mesh, first.
        let hello = match timeout(HANDSHAKE_TIMEOUT, incoming.message()).await {
            Err(_) => return Err(Status::deadline_exceeded("no hello in time")),
            Ok(Err(status)) => return Err(status),
            Ok(Ok(Some(Frame {
                body: Some(Body::Hello(hello)),
                ..
            }))) => hello,
            Ok(Ok(_)) => return Err(Status::failed_precondition("the first frame must be hello")),
        };
        if hello.protocol != PROTOCOL {
            return Err(Status::failed_precondition(format!(
                "this machine speaks revision {PROTOCOL} of the link protocol, not {}",
                hello.protocol
            )));
        }

        // Only a machine the control plane named as a peer in that mesh is
        // answered, and only the one whose place it is to dial.
        let peer = hello.instance_id;
        let key = match self.mesh.key_for(&hello.mesh, &peer) {
            Some(key) if peer < me && hello.nonce.len() >= proof::NONCE_BYTES => key,
            _ => return Err(Status::permission_denied("not a peer of this machine")),
        };

        let nonce = proof::nonce().map_err(|_| Status::internal("no randomness"))?;
        let (outbox, sending) = mpsc::channel(OUTBOX);

        // This side proves itself first: it answers the dialler's challenge
        // along with its own hello.
        let greeting = frame(Body::Hello(Hello {
            protocol: PROTOCOL,
            name: name(),
            instance_id: me.clone(),
            mesh: hello.mesh.clone(),
            nonce: nonce.clone(),
        }));
        let mine = frame(Body::Proof(Proof {
            mac: proof::prove(
                &key,
                &Claim {
                    role: Role::Accept,
                    from: &me,
                    to: &peer,
                    mesh: &hello.mesh,
                    nonce: &hello.nonce,
                },
            ),
        }));
        for first in [greeting, mine] {
            if outbox.send(Ok(first)).await.is_err() {
                return Err(Status::unavailable("the connection was lost"));
            }
        }

        let mesh = self.mesh.clone();
        tokio::spawn(async move {
            // Then the dialler proves itself, with this side's challenge.
            let theirs = match timeout(HANDSHAKE_TIMEOUT, incoming.message()).await {
                Ok(Ok(Some(Frame {
                    body: Some(Body::Proof(theirs)),
                    ..
                }))) => theirs,
                _ => return,
            };
            let claim = Claim {
                role: Role::Dial,
                from: &peer,
                to: &me,
                mesh: &hello.mesh,
                nonce: &nonce,
            };
            if !proof::holds(&key, &claim, &theirs.mac) {
                eprintln!("linkd: refused {peer}: it could not prove it holds the mesh's key");
                let _ = outbox
                    .send(Err(Status::permission_denied("the proof does not hold")))
                    .await;
                return;
            }

            let _linked = mesh.linked(peer.clone());
            eprintln!("linkd: linked to peer {peer}, which dialled");

            let ended = loop {
                match incoming.message().await {
                    Ok(Some(received)) => {
                        if let Some(reply) = answer(&received)
                            && outbox.send(Ok(reply)).await.is_err()
                        {
                            break "the connection was lost".to_string();
                        }
                    }
                    Ok(None) => break "it closed the stream".to_string(),
                    Err(status) => break refusal(&status),
                }
            };
            eprintln!("linkd: link to peer {peer} ended: {ended}");
        });

        Ok(Response::new(ReceiverStream::new(sending)))
    }
}

/// Keeps a stream to a peer open, dialling again with a growing wait
/// whenever it drops. It runs until it is stopped from outside, which the
/// mesh does when the peer is one no longer.
pub async fn dial(mesh: Arc<Mesh>, to: Target) {
    let mut backoff = Backoff::new();

    loop {
        let started = Instant::now();
        let ended = dial_once(&mesh, &to).await;
        eprintln!("linkd: link to peer {} ended: {ended}", to.peer);

        tokio::time::sleep(backoff.after(started.elapsed())).await;
    }
}

/// Dials a peer, proves and is proven to, and serves the stream until it
/// ends. It returns why it ended, in words.
async fn dial_once(mesh: &Arc<Mesh>, to: &Target) -> String {
    let me = mesh.me();

    let endpoint = match Endpoint::from_shared(to.address.clone()) {
        Ok(endpoint) => endpoint
            .connect_timeout(DIAL_TIMEOUT)
            .tcp_nodelay(true)
            .http2_keep_alive_interval(KEEPALIVE_EVERY)
            .keep_alive_timeout(KEEPALIVE_TIMEOUT)
            .keep_alive_while_idle(true),
        Err(problem) => return format!("{} is not an address: {}", to.address, causes(&problem)),
    };
    let channel = match endpoint.connect().await {
        Ok(channel) => channel,
        Err(problem) => return format!("dial {}: {}", to.address, causes(&problem)),
    };
    let mut client = LinkClient::new(channel).max_decoding_message_size(MAX_FRAME);

    let nonce = match proof::nonce() {
        Ok(nonce) => nonce,
        Err(problem) => return format!("no randomness: {problem}"),
    };
    let (outbox, sending) = mpsc::channel::<Frame>(OUTBOX);
    let hello = frame(Body::Hello(Hello {
        protocol: PROTOCOL,
        name: name(),
        instance_id: me.to_string(),
        mesh: to.mesh.clone(),
        nonce: nonce.clone(),
    }));
    if outbox.send(hello).await.is_err() {
        return "the outbox closed before anything was sent".into();
    }

    let mut incoming = match client
        .channel(Request::new(ReceiverStream::new(sending)))
        .await
    {
        Ok(response) => response.into_inner(),
        Err(status) => return refusal(&status),
    };

    // The peer's hello, with its challenge.
    let theirs = match next(&mut incoming).await {
        Ok(Body::Hello(hello)) => hello,
        Ok(_) => return "its first frame was not hello".into(),
        Err(problem) => return problem,
    };
    if theirs.protocol != PROTOCOL {
        return format!(
            "it speaks revision {} of the link protocol",
            theirs.protocol
        );
    }
    if theirs.instance_id != to.peer || theirs.mesh != to.mesh {
        return "it is not the machine that was dialled".into();
    }
    if theirs.nonce.len() < proof::NONCE_BYTES {
        return "its challenge is too short to be one".into();
    }

    // Its proof, of this side's challenge.
    let proven = match next(&mut incoming).await {
        Ok(Body::Proof(proven)) => proven,
        Ok(_) => return "it sent no proof".into(),
        Err(problem) => return problem,
    };
    let claim = Claim {
        role: Role::Accept,
        from: &to.peer,
        to: me,
        mesh: &to.mesh,
        nonce: &nonce,
    };
    if !proof::holds(&to.key, &claim, &proven.mac) {
        return "it could not prove it holds the mesh's key".into();
    }

    // And this side's, of its challenge.
    let mine = frame(Body::Proof(Proof {
        mac: proof::prove(
            &to.key,
            &Claim {
                role: Role::Dial,
                from: me,
                to: &to.peer,
                mesh: &to.mesh,
                nonce: &theirs.nonce,
            },
        ),
    }));
    if outbox.send(mine).await.is_err() {
        return "the connection was lost".into();
    }

    let _linked = mesh.linked(to.peer.clone());
    eprintln!("linkd: linked to peer {}, at {}", to.peer, to.address);

    loop {
        match incoming.message().await {
            Ok(Some(received)) => {
                if let Some(reply) = answer(&received)
                    && outbox.send(reply).await.is_err()
                {
                    return "the connection was lost".into();
                }
            }
            Ok(None) => return "it closed the stream".into(),
            Err(status) => return refusal(&status),
        }
    }
}

/// The body of the next frame of a handshake, or why there was none.
async fn next(incoming: &mut Streaming<Frame>) -> Result<Body, String> {
    match timeout(HANDSHAKE_TIMEOUT, incoming.message()).await {
        Err(_) => Err("it took too long over the handshake".into()),
        Ok(Err(status)) => Err(refusal(&status)),
        Ok(Ok(None)) => Err("it closed the stream during the handshake".into()),
        Ok(Ok(Some(Frame { body: None, .. }))) => Err("it sent a frame of no known kind".into()),
        Ok(Ok(Some(Frame {
            body: Some(body), ..
        }))) => Ok(body),
    }
}
