//! What the stream to the control plane and the streams to peers share.

use std::time::Duration;

use crate::proto::{Frame, Pong, frame::Body};

/// The revision of the protocol this program speaks. The control plane's is
/// linkapi.Protocol; a stream between two that differ is ended.
pub const PROTOCOL: u32 = 1;
/// One frame is never larger than this, in bytes (linkapi.MaxFrame).
pub const MAX_FRAME: usize = 256 << 10;

pub const DIAL_TIMEOUT: Duration = Duration::from_secs(15);
/// How long the other side gets over each step of the handshake.
pub const HANDSHAKE_TIMEOUT: Duration = Duration::from_secs(10);
/// A stream with nothing to say is pinged this often, and given this long to
/// answer: how a node that vanished without closing its connection gets
/// noticed. The control plane allows pings no more often than every ten
/// seconds.
pub const KEEPALIVE_EVERY: Duration = Duration::from_secs(20);
pub const KEEPALIVE_TIMEOUT: Duration = Duration::from_secs(10);

/// How many frames may wait to be sent on one stream. Nothing here buffers
/// without bound.
pub const OUTBOX: usize = 64;

/// What this program calls itself in its hello.
pub fn name() -> String {
    format!("linkd {}", env!("CARGO_PKG_VERSION"))
}

/// What to send back for a frame, if anything. A kind of frame this program
/// does not know is ignored, as the protocol asks.
pub fn answer(frame: &Frame) -> Option<Frame> {
    match &frame.body {
        Some(Body::Ping(_)) => Some(Frame {
            reply_to: frame.id,
            body: Some(Body::Pong(Pong {})),
            ..Frame::default()
        }),
        _ => None,
    }
}

/// Says in words why the other side ended or refused a stream.
pub fn refusal(status: &tonic::Status) -> String {
    use std::error::Error;

    // A status the library made itself, for a connection that broke, keeps
    // the reason underneath.
    let said = chain(status.message().to_string(), status.source());
    format!("{:?}: {said}", status.code())
}

/// An error with what caused it, and what caused that: the library says only
/// "transport error" and keeps "connection refused" underneath.
pub fn causes(problem: &dyn std::error::Error) -> String {
    chain(problem.to_string(), problem.source())
}

/// Joins what went wrong to its causes, saying each thing once: layers of
/// the library often repeat each other word for word.
fn chain(first: String, mut cause: Option<&(dyn std::error::Error + 'static)>) -> String {
    let mut said = first.clone();
    let mut last = first;

    while let Some(under) = cause {
        let text = under.to_string();
        if text != last {
            said = format!("{said}: {text}");
        }
        last = text;
        cause = under.source();
    }

    said
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::proto::{Hello, Ping};

    #[test]
    fn a_ping_is_answered_with_its_pong() {
        let ping = Frame {
            id: 7,
            body: Some(Body::Ping(Ping {})),
            ..Frame::default()
        };

        let reply = answer(&ping).expect("a ping gets an answer");
        assert_eq!(reply.reply_to, 7);
        assert_eq!(reply.id, 0, "a pong expects no answer of its own");
        assert!(matches!(reply.body, Some(Body::Pong(_))));
    }

    #[test]
    fn other_frames_get_no_answer() {
        let hello = Frame {
            body: Some(Body::Hello(Hello::default())),
            ..Frame::default()
        };

        assert!(answer(&hello).is_none());
        // A frame of a kind this program does not know arrives with no body.
        assert!(answer(&Frame::default()).is_none());
    }

    #[test]
    fn an_error_is_reported_with_what_caused_it_said_once() {
        use std::fmt;

        /// An error that says `.0` and was caused by `.1`.
        #[derive(Debug)]
        struct Layer(&'static str, Option<Box<Layer>>);
        impl fmt::Display for Layer {
            fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
                write!(f, "{}", self.0)
            }
        }
        impl std::error::Error for Layer {
            fn source(&self) -> Option<&(dyn std::error::Error + 'static)> {
                self.1.as_deref().map(|under| under as _)
            }
        }

        let refused = Layer("connection refused", None);
        let connect = Layer("tcp connect error", Some(Box::new(refused)));
        let again = Layer("tcp connect error", Some(Box::new(connect)));
        let transport = Layer("transport error", Some(Box::new(again)));

        assert_eq!(
            causes(&transport),
            "transport error: tcp connect error: connection refused"
        );
    }
}
