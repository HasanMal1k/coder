//! The stream to the control plane, which the harness calls the parent.

use std::sync::Arc;
use std::time::Instant;

use tokio::sync::mpsc;
use tokio::time::timeout;
use tokio_stream::wrappers::ReceiverStream;
use tonic::Request;
use tonic::metadata::MetadataValue;
use tonic::transport::Endpoint;

use crate::Config;
use crate::backoff::Backoff;
use crate::mesh::Mesh;
use crate::proto::link_client::LinkClient;
use crate::proto::{Frame, Hello, Status, frame::Body};
use crate::wire::{
    DIAL_TIMEOUT, HANDSHAKE_TIMEOUT, KEEPALIVE_EVERY, KEEPALIVE_TIMEOUT, MAX_FRAME, OUTBOX,
    PROTOCOL, answer, causes, name, refusal,
};

/// Keeps a stream to the control plane open, dialling again with a growing
/// wait whenever it drops. It never returns.
pub async fn run(config: &Config, mesh: Arc<Mesh>) {
    let mut backoff = Backoff::new();

    loop {
        let started = Instant::now();
        let ended = attach(config, &mesh).await;
        eprintln!("linkd: link to the control plane ended: {ended}");

        tokio::time::sleep(backoff.after(started.elapsed())).await;
    }
}

/// Dials the control plane, shakes hands, and serves the stream until it
/// ends. It returns why it ended, in words.
async fn attach(config: &Config, mesh: &Arc<Mesh>) -> String {
    let endpoint = match Endpoint::from_shared(config.link_url.clone()) {
        Ok(endpoint) => endpoint
            .connect_timeout(DIAL_TIMEOUT)
            .tcp_nodelay(true)
            .http2_keep_alive_interval(KEEPALIVE_EVERY)
            .keep_alive_timeout(KEEPALIVE_TIMEOUT)
            .keep_alive_while_idle(true),
        Err(problem) => {
            return format!(
                "{} is not an address: {}",
                config.link_url,
                causes(&problem)
            );
        }
    };

    let channel = match endpoint.connect().await {
        Ok(channel) => channel,
        Err(problem) => return format!("dial {}: {}", config.link_url, causes(&problem)),
    };
    let mut client = LinkClient::new(channel).max_decoding_message_size(MAX_FRAME);

    // What this side sends goes through a queue of fixed size. Its hello is
    // put there before the call is made: the control plane answers nothing
    // until it has seen it.
    let (outbox, sending) = mpsc::channel::<Frame>(OUTBOX);
    let hello = Frame {
        body: Some(Body::Hello(Hello {
            protocol: PROTOCOL,
            name: name(),
            ..Hello::default()
        })),
        ..Frame::default()
    };
    if outbox.send(hello).await.is_err() {
        return "the outbox closed before anything was sent".into();
    }

    // The machine's own identity, the same credential it presents to the
    // model gateway and the team API.
    let identity = format!("Bearer {}.{}", config.instance_id, config.token);
    let mut request = Request::new(ReceiverStream::new(sending));
    match MetadataValue::try_from(identity) {
        Ok(value) => request.metadata_mut().insert("authorization", value),
        Err(_) => return "this machine's identity is not something a header can carry".into(),
    };

    let mut incoming = match client.channel(request).await {
        Ok(response) => response.into_inner(),
        Err(status) => return refusal(&status),
    };

    // The control plane says what it is first, as this side did.
    let parent = match timeout(HANDSHAKE_TIMEOUT, incoming.message()).await {
        Err(_) => return "the control plane did not say hello in time".into(),
        Ok(Err(status)) => return refusal(&status),
        Ok(Ok(None)) => return "the control plane closed the stream before saying hello".into(),
        Ok(Ok(Some(frame))) => match greeting(&frame) {
            Ok(name) => name,
            Err(problem) => return problem,
        },
    };
    eprintln!("linkd: attached to {parent}");

    // The control plane is told which peers this machine has a proven
    // stream with: now, since it may be a new control plane that never
    // heard, and again whenever that changes.
    let mut linked = mesh.watch();
    linked.mark_changed();

    loop {
        tokio::select! {
            received = incoming.message() => {
                let frame = match received {
                    Ok(Some(frame)) => frame,
                    Ok(None) => return "the control plane closed the stream".into(),
                    Err(status) => return refusal(&status),
                };

                let reply = match frame.body {
                    Some(Body::Peers(peers)) => {
                        mesh.update(peers);
                        None
                    }
                    _ => answer(&frame),
                };
                // The queue's other end goes when the connection does.
                if let Some(reply) = reply
                    && outbox.send(reply).await.is_err()
                {
                    return "the connection was lost".into();
                }
            }
            changed = linked.changed() => {
                if changed.is_err() {
                    return "the mesh is gone".into();
                }

                let status = Frame {
                    body: Some(Body::Status(Status {
                        linked: linked.borrow_and_update().clone(),
                    })),
                    ..Frame::default()
                };
                if outbox.send(status).await.is_err() {
                    return "the connection was lost".into();
                }
            }
        }
    }
}

/// Checks the other side's hello, and returns what it called itself.
fn greeting(frame: &Frame) -> Result<String, String> {
    match &frame.body {
        Some(Body::Hello(hello)) if hello.protocol == PROTOCOL => Ok(hello.name.clone()),
        Some(Body::Hello(hello)) => Err(format!(
            "the control plane speaks revision {} of the link protocol, and this linkd speaks {PROTOCOL}",
            hello.protocol
        )),
        _ => Err("the control plane's first frame was not hello".into()),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn hello(protocol: u32) -> Frame {
        Frame {
            body: Some(Body::Hello(Hello {
                protocol,
                name: "control plane".into(),
                ..Hello::default()
            })),
            ..Frame::default()
        }
    }

    #[test]
    fn a_hello_in_this_revision_is_accepted() {
        assert_eq!(greeting(&hello(PROTOCOL)).unwrap(), "control plane");
    }

    #[test]
    fn another_revision_is_refused() {
        let problem = greeting(&hello(PROTOCOL + 1)).unwrap_err();
        assert!(problem.contains("revision 2"), "{problem}");
    }

    #[test]
    fn a_first_frame_that_is_not_hello_is_refused() {
        assert!(greeting(&Frame::default()).is_err());
    }
}
