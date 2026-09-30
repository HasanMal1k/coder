# Architecture

A browser-based cloud development platform. Every user can run several
persistent Linux machines, each with full root access, a VS Code-style IDE
(file tree, editor, terminal), a live preview of whatever they are running,
and an MCP endpoint so an AI harness of their choice can drive the machine.

Compute runs on **Fly.io Machines** (Firecracker microVMs). The platform
itself (the "control plane") can be hosted anywhere.

## Overview

```
                     ┌──────────────────── Control plane (cmd/server) ────────────────────┐
Browser IDE ──HTTPS──►  httpapi      REST: auth, dashboard, instance lifecycle            │
            ──WSS────►  wsgateway    terminal / file-watch streams                        │
Preview tab ──HTTPS──►  preview      reverse proxy for *.preview-domain                   │
AI harness  ──HTTPS──►  mcpapi       MCP tools: run_command, read_file, write_file, ...   │
                     │        │                                                           │
                     │        ▼                                                           │
                     │  instance ── lifecycle, ownership, quotas, idle stop                │
                     │   │     │                                                          │
                     │   │     └──► tunnel ◄───────── wss (guestd dials OUT) ─────────┐   │
                     │   ▼                                                            │   │
                     │  sandbox ──► sandbox/fly ──► Fly Machines API                 │   │
                     │  store   ──► Postgres                                          │   │
                     └────────────────────────────────────────────────────────────────┼───┘
                                                                                      │
                     ┌──────────── Fly Machine (one per instance) ─────────────┐     │
                     │  guestd: terminal (PTY), files, port-forward, activity ─┼─────┘
                     │  Volume mounted at /home/coder  (persistent)             │
                     │  everything else: reset on every start                   │
                     └──────────────────────────────────────────────────────────┘
```

Two ideas carry the design:

1. **Lifecycle and access are separate.** `sandbox` only creates, starts,
   stops and deletes machines, and is the only provider-specific code.
   Everything that happens *inside* a machine (terminal, files, preview)
   goes through `tunnel`, which is provider-agnostic. Swapping Fly for
   self-hosted Firecracker, or for local Docker in development, touches
   `sandbox/*` only.
2. **The guest daemon dials out.** `guestd` inside each machine opens one
   WebSocket to the control plane and multiplexes many streams over it
   (yamux). User machines need no public IP and no inbound ports, and the
   control plane can live on any host.

## Packages

| Path | Responsibility |
|---|---|
| `cmd/server` | Wires everything together. |
| `cmd/guestd` | Guest daemon, baked into the machine image. |
| `internal/auth` | Users, passwords, tokens. **Done.** |
| `internal/instance` | Instance lifecycle state machine, ownership checks, quotas, idle stop, reconciler. |
| `internal/sandbox` | `Provisioner` interface (lifecycle only). |
| `internal/sandbox/fly` | Fly Machines API client + `Provisioner`. |
| `internal/sandbox/docker` | Local-development `Provisioner` using the same guest image. |
| `internal/tunnel` | Accepts `guestd` connections, tracks which instance is online, opens streams to it. |
| `internal/guestapi` | Protocol between control plane and `guestd` (gRPC over yamux streams). |
| `internal/wsgateway` | Browser WebSocket ⇄ tunnel stream (terminal, file watch). |
| `internal/preview` | `https://<port>-<instance>.<preview-domain>` ⇄ tunnel ⇄ `localhost:<port>` in the machine. |
| `internal/mcpapi` | MCP server; calls `instance` + `tunnel` exactly like the UI does. |
| `internal/httpapi` | REST handlers + router. |
| `internal/middleware` | Auth and other HTTP middleware. |
| `internal/store` | Postgres repositories. No business logic. |
| `images/workspace` | Dockerfile for the machine image (Ubuntu, dev tools, `guestd`). |
| `web/` | Frontend (Monaco + xterm.js + file tree). |

Dependency rule: transports (`httpapi`, `wsgateway`, `preview`, `mcpapi`)
→ domain (`auth`, `instance`) → infrastructure (`sandbox`, `tunnel`,
`store`). Domain packages never import transport types.

## Sandbox interface (lifecycle only)

```go
type Resources struct {
    CPUKind  string // "shared" | "performance"
    CPUs     int
    MemoryMB int
    DiskGB   int    // size of the persistent volume
}

type Spec struct {
    InstanceID string
    OwnerID    string
    Image      string
    Region     string
    Resources  Resources
    Env        map[string]string // includes the guestd connect token
}

type Provisioner interface {
    Create(ctx context.Context, spec Spec) (Handle, error) // machine + volume, not started
    Start(ctx context.Context, h Handle) error
    Stop(ctx context.Context, h Handle) error
    Delete(ctx context.Context, h Handle) error             // machine + volume
    State(ctx context.Context, h Handle) (State, error)
}

// Handle holds the provider's IDs, persisted on the instance row.
type Handle struct {
    MachineID string
    VolumeID  string
}
```

`Attach` moves out of `sandbox`: terminals, files and ports are served by
`tunnel`, whatever the provider.

## How lifecycle maps to Fly

- **Isolation**: one Fly app per user, each created with its own custom 6PN
  network (`POST /v1/apps` with `network`), so users' machines cannot reach
  each other or anything else in the org. Fly recommends this for untrusted
  code. The control plane is not on these networks.
- **Create**: create a volume (`DiskGB`, in `Region`), then a machine from
  the workspace image with the volume mounted at `/home/coder`, `guestd`
  token in env, restart policy `no`, not started.
- **Start / Stop**: machine start / stop endpoints; wait for the state.
  Stopped machines cost only storage (volume + rootfs, $0.15/GB-month).
- **Delete**: destroy machine, then delete volume.
- **State**: get machine.

### Root filesystem is ephemeral

On Fly only volumes persist; the root filesystem resets on every start.
Anything a user installs outside `/home/coder` (e.g. `apt install`) is lost
on stop. Options, in order of effort:

1. Bake common toolchains into the image; per-user installs go to the home
   directory (`mise`, `nvm`, `pip --user`, `cargo`, Homebrew-in-home).
2. Use Nix with `/nix` on the volume.
3. `guestd` mounts an overlayfs at boot whose upper layer lives on the
   volume, making the whole system writable and persistent.

Start with 1. Revisit 3 if "real machine" persistence becomes a selling point.

## Guest daemon (`guestd`)

- Entry point of the machine image; runs supervised so a user killing it
  gets it restarted.
- On boot: dials `wss://<control-plane>/guest/connect` with its per-instance
  token, then serves gRPC on streams the control plane opens over yamux.
- Services:
  - `Terminal`: bidirectional stream; PTY + shell; stdin, resize, output, exit.
  - `Files`: list, stat, read, write, mkdir, rename, delete, watch.
  - `Exec`: run a command, stream output, return exit code (used by MCP).
  - `Ports`: open a TCP connection to `localhost:<port>` (preview).
  - `Activity`: last input time, listening ports.
- Token: random, generated at Create, only a hash stored in Postgres.

## Instance state

```
creating → stopped ⇄ starting → running ⇄ stopping → stopped
                                    │
any → deleting → (row soft-deleted)     any → error
```

`running` means the Fly machine is started; `online` (guestd connected) is
tracked separately by `tunnel`. The UI shows "booting" while
running-but-offline.

A **reconciler** loop compares Postgres with Fly periodically and fixes
drift: stuck transitions, machines deleted outside the platform, machines
running with no row.

**Idle stop**: `guestd` reports activity; `instance` stops machines idle for
N minutes (default 30). Opening the IDE starts the machine again.

## Data model

```sql
instances (
  id               UUID PRIMARY KEY,
  owner_id         UUID NOT NULL REFERENCES users(id),
  name             TEXT NOT NULL,
  status           TEXT NOT NULL,
  provider         TEXT NOT NULL,          -- 'fly' | 'docker'
  machine_id       TEXT,
  volume_id        TEXT,
  region           TEXT NOT NULL,
  cpu_kind         TEXT NOT NULL,
  cpus             INT  NOT NULL,
  memory_mb        INT  NOT NULL,
  disk_gb          INT  NOT NULL,
  guest_token_hash BYTEA NOT NULL,
  last_active_at   TIMESTAMPTZ,
  created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  deleted_at       TIMESTAMPTZ,
  UNIQUE (owner_id, name)
)

api_tokens (                             -- for MCP clients
  id           UUID PRIMARY KEY,
  user_id      UUID NOT NULL REFERENCES users(id),
  name         TEXT NOT NULL,
  token_hash   BYTEA NOT NULL UNIQUE,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  last_used_at TIMESTAMPTZ
)
```

## Security notes

- **Preview on a separate registrable domain** (e.g. `yourapp-preview.dev`,
  not a subdomain of the platform domain), so user-controlled pages cannot
  read platform cookies or run script in the platform's origin.
- **Fly API token** held by the control plane is org-scoped; keep user
  machines in a dedicated Fly org, separate from anything else you run.
- **Abuse**: users have root and outbound internet. Plan for crypto mining,
  spam and phishing on preview URLs: CPU/egress quotas, preview URLs that
  require login by default, and a way to suspend an account.

## Milestones

Each one is testable on its own. 1–5 run entirely on a laptop.

1. **Workspace image**: `images/workspace/Dockerfile` (Ubuntu + tools),
   runs locally with `docker run -it`.
2. **guestd + tunnel, locally**: control plane on the host, `guestd` in a
   local container, terminal stream works from a small CLI client.
3. **Instances**: migration, `store` repo, `instance` service,
   `sandbox/docker`, REST endpoints.
4. **Browser terminal**: `wsgateway` + a minimal xterm.js page.
5. **Files + IDE shell**: `Files` service, Monaco + file tree.
6. **Fly provider**: `sandbox/fly`; same flows on real Fly machines.
7. **Preview**: wildcard DNS + TLS for the preview domain, `preview` proxy.
8. **Idle stop + reconciler.**
9. **MCP**: `api_tokens`, `mcpapi` with `run_command`, `read_file`,
   `write_file`, `list_files`, `start_instance`, `stop_instance`.

Later: refresh tokens + Redis, quotas and billing, templates, GitHub
integration, publishing user apps on permanent URLs, collaboration,
multiple control-plane replicas (tunnel presence in Redis), self-hosted
Firecracker provider.
