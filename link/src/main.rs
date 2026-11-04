//! The link daemon's executable. What it does is in the library beside it.

use std::net::SocketAddr;
use std::process::ExitCode;

use linkd::Config;
use tokio::signal::unix::{SignalKind, signal};

/// What linkd is told through its environment, by the names the control
/// plane sets them under (internal/guestapi and internal/linkapi).
const ENV_LINK_URL: &str = "CODER_LINK_URL";
const ENV_INSTANCE_ID: &str = "CODER_INSTANCE_ID";
const ENV_GUEST_TOKEN: &str = "CODER_GUEST_TOKEN";
/// Where to listen for peers, if not on every address at the usual port.
const ENV_LISTEN: &str = "CODER_LINK_LISTEN";

/// The port peers are told to dial (linkapi.PeerPort).
const DEFAULT_LISTEN: &str = "0.0.0.0:7600";

fn config_from_env() -> Result<Config, String> {
    let var = |name: &str| match std::env::var(name) {
        Ok(value) if !value.is_empty() => Ok(value),
        _ => Err(format!("{name} is required")),
    };

    let listen = std::env::var(ENV_LISTEN).unwrap_or_else(|_| DEFAULT_LISTEN.into());
    let listen: SocketAddr = listen
        .parse()
        .map_err(|_| format!("{ENV_LISTEN} is {listen:?}, not an address and port"))?;

    Ok(Config {
        link_url: var(ENV_LINK_URL)?,
        instance_id: var(ENV_INSTANCE_ID)?,
        token: var(ENV_GUEST_TOKEN)?,
        listen,
    })
}

fn main() -> ExitCode {
    let config = match config_from_env() {
        Ok(config) => config,
        Err(problem) => {
            eprintln!("linkd: {problem}");
            return ExitCode::FAILURE;
        }
    };

    // One thread: everything here waits on the network, and a thread that is
    // not there costs no memory.
    let runtime = match tokio::runtime::Builder::new_current_thread()
        .enable_io()
        .enable_time()
        .build()
    {
        Ok(runtime) => runtime,
        Err(problem) => {
            eprintln!("linkd: start: {problem}");
            return ExitCode::FAILURE;
        }
    };

    runtime.block_on(async {
        let (mut terminate, mut interrupt) = match (
            signal(SignalKind::terminate()),
            signal(SignalKind::interrupt()),
        ) {
            (Ok(terminate), Ok(interrupt)) => (terminate, interrupt),
            (Err(problem), _) | (_, Err(problem)) => {
                eprintln!("linkd: listen for signals: {problem}");
                return ExitCode::FAILURE;
            }
        };

        tokio::select! {
            problem = linkd::run(config) => {
                eprintln!("linkd: {problem}");
                ExitCode::FAILURE
            }
            _ = terminate.recv() => {
                eprintln!("linkd: shutting down");
                ExitCode::SUCCESS
            }
            _ = interrupt.recv() => {
                eprintln!("linkd: shutting down");
                ExitCode::SUCCESS
            }
        }
    })
}
