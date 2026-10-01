# Architecture

A browser-based cloud development platform. Every user can run several
persistent Linux machines, each with full root access and a terminal in the
browser. Planned on top of that: a VS Code-style IDE (file tree, editor), a
live preview of whatever the user is running, a chat panel driving an AI
harness of their choice with their own API key, and an MCP endpoint.

**Built so far:** auth, instance lifecycle, the guest daemon, the tunnel, the
browser terminal, and a web frontend (sign in, dashboard, terminal), running
machines as local Docker containers.
**Not built yet:** everything under [Planned](#planned).

## Overview

```
                     ┌──────────────── Control plane (cmd/server) ────────────────┐
Browser ─────HTTPS───►  httpapi      REST: auth, instance lifecycle               │
        ─────WSS─────►  wsgateway    terminal                                     │
                     │        │                                                   │
                     │        ▼                                                   │
                     │  instance ── lifecycle, ownership ───┐                     │
                     │   │     │                            ▼                     │
                     │   │     └──────────────────────►  tunnel ◄── wss ──────┐   │
                     │   ▼                                 (guestd dials OUT) │   │
                     │  sandbox ──► sandbox/docker ──► Docker                 │   │
                     │  store   ──► Postgres                                  │   │
                     └────────────────────────────────────────────────────────┼───┘
                                                                              │
                     ┌──────────── Machine (one per instance) ──────────┐     │
                     │  guestd: runs terminals on PTYs ─────────────────┼─────┘
                     │  Volume mounted at /home/coder  (persistent)      │
                     └───────────────────────────────────────────────────┘
```

Two ideas carry the design:

1. **Lifecycle and access are separate.** `sandbox` only creates, starts,
   stops and deletes machines, and is the only provider-specific code.
   Everything that happens *inside* a machine goes through `tunnel`, which is
   provider-agnostic. Moving from Docker to Fly.io or self-hosted Firecracker
   touches `sandbox/*` only.
2. **The guest daemon dials out.** `guestd` inside each machine opens one
   WebSocket to the control plane. Machines need no public IP and no inbound
   ports, and the control plane can live on any host.

## Packages

| Path | Responsibility |
|---|---|
| `cmd/server` | Wires everything together; graceful shutdown. |
| `cmd/guestd` | Guest daemon, baked into the machine image. |
| `internal/auth` | Users, passwords, access tokens. |
| `internal/instance` | Instance state machine, ownership checks, guest credentials. |
| `internal/sandbox` | `Provisioner` interface (lifecycle only). |
| `internal/sandbox/docker` | Runs machines as local Docker containers. |
| `internal/tunnel` | Tracks connected guests; opens terminals on them. |
| `internal/guest` | What `guestd` runs: the terminal server and the reconnecting dial loop. |
| `internal/guestapi` | Protocol between control plane and `guestd` (gRPC, defined in `guest.proto`). |
| `internal/httpapi` | REST handlers + router. |
| `internal/wsgateway` | WebSocket handlers: browser terminal, guest tunnel. |
| `internal/middleware` | HTTP auth middleware. |
| `internal/store` | Postgres repositories. No business logic. |
| `images/workspace` | Dockerfile for the machine image (Ubuntu, dev tools, `guestd`). |
| `web` | Frontend: Next.js, talking to the API from the browser. |

Dependency rule: transports (`httpapi`, `wsgateway`) → domain (`auth`,
`instance`) → infrastructure (`sandbox`, `tunnel`, `store`). Domain packages
never import transport types.

## The tunnel

```
browser ⇄ WebSocket ⇄ wsgateway ⇄ tunnel.Terminal ⇄ gRPC stream ⇄ yamux stream ⇄ WebSocket ⇄ guestd ⇄ PTY ⇄ bash
```

- `guestd` dials `GUEST_CONNECT_URL` with its instance ID and guest token.
- Over that one WebSocket runs **yamux**, which multiplexes many independent
  streams and detects dead connections with keepalives.
- Each stream carries **gRPC**. The roles are reversed from the usual: the
  machine dialed, but the control plane is the gRPC *client* and `guestd` the
  server.
- Backpressure is end to end. A slow browser slows the WebSocket write, which
  stops the gateway reading the gRPC stream, which blocks `guestd`'s send,
  which stops it reading the PTY, which blocks the program producing output.
  Nothing buffers without bound.
- If the tunnel drops, `guestd` reconnects with exponential backoff (1s to
  30s). A new connection for an instance replaces the old one.

### Guest credentials

Each instance gets a random token at creation. The machine receives it as an
environment variable; Postgres stores only its SHA-256. A database leak
therefore doesn't let anyone impersonate a machine. `guestd` strips the token
from the environment of the shells it starts.

## Instance state

```
creating → stopped ⇄ starting → running ⇄ stopping → stopped
any settled state → deleting → (row removed)        any → error
```

- **Claiming.** Every operation begins with one conditional `UPDATE` that
  moves the row into a transitional status only if it is in an allowed one.
  Two concurrent requests can't both win; the loser gets "busy".
- **Finishing.** Once claimed, the operation runs on a context detached from
  the HTTP request, so a client disconnecting can't strand an instance
  mid-transition.
- **Recovering.** Operations only run inside the server process, so at startup
  anything still transitional was interrupted, and is marked `error`. From
  `error` the user can retry or delete. (This assumes a single server process;
  see Planned.)
- `running` means the machine is started. Whether its `guestd` has connected
  yet is tracked by `tunnel`; opening a terminal waits up to 30s for it.

## Machines on Docker

- One container and one named volume per instance, both named
  `coder-<instance id>` and labelled with the instance and owner IDs.
- The volume is mounted at `/home/coder`. CPU and memory limits are applied;
  disk size is not enforceable on local volumes.
- Restart policy `unless-stopped`: a machine that was running comes back after
  a Docker restart or if `guestd` is killed, but never after we stop it.
- `host.docker.internal` is mapped to the host, which is how `guestd` reaches
  a control plane started with `make run`.

## Data model

```sql
users (id, email, password_hash, display_name, organization, subscription, created_at)

instances (
  id UUID PRIMARY KEY,
  owner_id UUID NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
  name TEXT NOT NULL,                -- unique per owner
  status TEXT NOT NULL,              -- CHECK: one of the statuses above
  provider TEXT NOT NULL,            -- CHECK: 'docker' | 'fly'
  machine_id TEXT, volume_id TEXT,   -- the provider's IDs; NULL until provisioned
  cpus INT, memory_mb INT, disk_gb INT,
  guest_token_hash BYTEA NOT NULL,
  created_at TIMESTAMPTZ, updated_at TIMESTAMPTZ
)
```

`ON DELETE RESTRICT`: a user can't be deleted while they have instances, so
machines are never orphaned by a cascade.

## Planned

Roughly in order.

1. **Fly.io provider** (`sandbox/fly`): machines as Fly Machines (Firecracker
   microVMs), one Fly app per user on its own private network. Notes:
   - Only volumes persist on Fly; the root filesystem resets on every start.
     Bake common toolchains into the image and steer per-user installs into
     the home directory, or later have `guestd` overlay the root filesystem
     onto the volume.
   - Stopped machines cost only storage, so **idle auto-stop** matters:
     `guestd` reports activity, the control plane stops idle machines.
   - `GUEST_CONNECT_URL` becomes the control plane's public `wss://` address.
2. **Reconciler**: periodically compare Postgres with the provider and fix
   drift (machines deleted outside the platform, machines with no row).
3. **Files API** in `guestd` (list, read, write, rename, delete, watch) and the
   IDE (Monaco + file tree).
4. **Preview**: `https://<port>-<instance>.<preview-domain>` proxied through
   the tunnel to `localhost:<port>` in the machine. The preview domain must be
   a separate registrable domain, so user-controlled pages can't touch the
   platform's origin.
5. **Chat**: a panel that runs the user's chosen AI harness *inside their
   machine*, with their own API key (stored encrypted, never returned to the
   browser).
6. **MCP server** (`internal/mcpapi`): the same instance and guest operations,
   exposed as tools, authenticated with per-user API tokens.
7. **Auth hardening**: refresh tokens in Redis with revocation, token type and
   `jti` claims, input validation, login timing.
8. **Quotas and plans**: per-user instance limits, selectable machine sizes.
9. **More than one server process**: guest connections are held in memory, so
    this needs shared presence (Redis) and routing to the process that holds a
    given tunnel; startup recovery must then stop assuming it is alone.

## Known gaps

- Every instance is the same size (1 CPU, 1 GB RAM, 10 GB disk).
- If the server dies between creating a machine and saving its ID, that
  machine is orphaned until the reconciler exists. Its labels identify it.
- No limit on how many instances a user can create.
- Access tokens expire after 15 minutes with no way to refresh.
