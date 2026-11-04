# linkd

The link daemon: a machine's end of the distributed harness. It runs on
every machine, kept up by `guestd`, and holds one gRPC stream open to the
control plane for as long as the machine runs.

[docs/HARNESS.md](../docs/HARNESS.md) is the design it is the first step of,
and [docs/BACKEND.md, section 25](../docs/BACKEND.md#25-the-link) says how it
and the server's side work. Today it attaches to the server, is told which
machines it works with, opens a stream to each directly, and proves it
belongs with them. Nothing is said over those streams yet.

It is Rust where the rest of the backend is Go, because it sits beside the
user's own work on every machine and should cost next to nothing there:
1.25 MB on disk and 1.3 MB of memory.

## Building and checking

It needs Rust, the static target for this computer's processor, and
`protoc` (the protocol's code is generated from
[`internal/linkapi/link.proto`](../internal/linkapi/link.proto) at build
time, the same file the Go side is generated from):

```bash
rustup target add x86_64-unknown-linux-musl
```

```bash
make link        # from the repository root: builds it and puts it where the machine image picks it up
cargo test       # from here
cargo fmt --check
cargo clippy --all-targets -- -D warnings
```

`make image` and `make images` run `make link` first. A machine keeps the
daemon of the image it was created from.

## Running it by hand

It is told everything through its environment, by the names the control
plane sets on a machine:

| Variable | |
|---|---|
| `CODER_LINK_URL` | The server's link gateway, such as `http://localhost:8081`. |
| `CODER_INSTANCE_ID` | Which machine this is. |
| `CODER_GUEST_TOKEN` | That machine's token. With the ID it is the machine's identity. |
| `CODER_LINK_LISTEN` | Where to listen for peers. `0.0.0.0:7600` if not set. |

With a server running and a machine's ID and token to hand, it can be run
on the host with no container at all, which is how most of it was checked:

```bash
CODER_LINK_URL=http://localhost:8081 CODER_INSTANCE_ID=<id> CODER_GUEST_TOKEN=<token> \
  target/x86_64-unknown-linux-musl/release/linkd
```

It logs to standard error, one line when it attaches and one each time the
stream ends, with the reason.

## The code

| File | |
|---|---|
| `src/main.rs` | Reads the environment, starts one thread, stops on `SIGTERM`. |
| `src/lib.rs` | The daemon itself, as a library, so tests can run whole daemons. |
| `src/parent.rs` | The stream to the control plane: dial, hello, serve, and why it ended. |
| `src/mesh.rs` | Which machines are peers, and which are linked right now. |
| `src/peer.rs` | The streams to peers: listening, dialling, and the handshake. |
| `src/proof.rs` | How two machines prove they share a mesh's key. |
| `src/wire.rs` | What every stream shares: limits, timings, answering a ping. |
| `tests/mesh.rs` | Two daemons and a stand-in control plane finding each other. |
| `src/backoff.rs` | How long to wait before dialling again. The tunnel's policy. |
| `build.rs` | Generates the protocol's code. |

`Cargo.toml` says which features of each dependency are used and why. Keep
that list short: every one is paid for on every machine.
