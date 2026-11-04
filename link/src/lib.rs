//! linkd is the link daemon: a machine's end of the distributed harness.
//!
//! It runs on every machine, kept up by guestd. It holds one stream open to
//! the control plane, which tells it which of its owner's other machines it
//! works with, and one stream to each of those, opened directly across the
//! owner's private network. docs/HARNESS.md is the design this is part of.
//!
//! So far the streams carry greetings, proofs and pings: the machines find
//! each other, prove they belong together, and notice when one goes. What
//! the harness is for is built on top of this.

mod backoff;
mod mesh;
mod parent;
mod peer;
mod proof;
mod wire;

/// The protocol, generated at build time from internal/linkapi/link.proto,
/// the file the control plane's Go code is generated from too.
pub mod proto {
    tonic::include_proto!("coder.link.v1");
}

use std::net::SocketAddr;
use std::sync::Arc;

/// Where the control plane is, and which machine this is.
pub struct Config {
    /// The control plane's link gateway: "http://host:8081".
    pub link_url: String,
    pub instance_id: String,
    /// The machine's guest token. With the instance ID it is the machine's
    /// identity towards the control plane, the only one it ever presents.
    pub token: String,
    /// Where to listen for peers.
    pub listen: SocketAddr,
}

/// Runs the daemon: the stream to the control plane and the streams to
/// peers, each kept up for as long as this runs. It returns only if it
/// cannot listen for peers, with the reason.
pub async fn run(config: Config) -> String {
    let mesh = Arc::new(mesh::Mesh::new(config.instance_id.clone()));

    tokio::select! {
        problem = peer::serve(config.listen, mesh.clone()) => problem,
        _ = parent::run(&config, mesh) => "the link to the control plane stopped being kept up".into(),
    }
}
