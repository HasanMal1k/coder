# Architecture

A browser-based cloud development platform. Every user can run several
persistent Linux machines, each with full root access, a terminal and an
editor in the browser, and an AI assistant that designs an application with
them and builds it across those machines.

This is the overview and the roadmap. [STATUS.md](STATUS.md) says where the
work stands today and what is known to be wrong; [BACKEND.md](BACKEND.md)
explains how the backend works in detail; [API.md](API.md) is the API;
[HARNESS.md](HARNESS.md) is the design for how the assistant and the agents
on machines are to work together, most of which is not built yet.

**Built so far:** auth, instance lifecycle, the guest daemon, the tunnel, the
browser terminal, an embedded editor (VS Code) served through a port proxy,
automatic stopping of machines nobody is connected to, an AI assistant that
designs first and then creates machines and directs coding agents inside
them, services the machines keep running, a web frontend that is a chat
with a panel showing what is being built, and the first piece of the
distributed harness: a link from every machine to the control plane.
Machines run as local Docker containers.
**Not built yet:** everything under [Planned](#planned).

## Overview

```
                     ┌──────────────── Control plane (cmd/server) ────────────────┐
Browser ─────HTTPS───►  httpapi      REST: auth, instances, chats, agents         │
        ─────WSS─────►  wsgateway    terminal, presence                           │
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
                     │  guestd: terminals on PTYs, port forwarding ─────┼─────┘
                     │          keeps the editor, the coding agent and   │
                     │          the machine's services up                │
                     │  Volume mounted at /home/coder  (persistent)      │
                     └───────────────────────────────────────────────────┘
```

Not drawn: `portproxy`, which serves machines' ports to the browser; the
assistant with what serves its agents (`llmgateway`, `teamapi`), which has
[a section of its own](#the-assistant); and `linkd`, a second daemon on each
machine with a connection of its own to the control plane, which has
[one too](#the-link).

Two ideas carry the design:

1. **Lifecycle and access are separate.** `sandbox` only creates, starts,
   stops and deletes machines, and is the only provider-specific code.
   Everything that happens *inside* a machine goes through `tunnel`, which is
   provider-agnostic. Moving from Docker to Fly.io or self-hosted Firecracker
   touches `sandbox/*` only.
2. **The guest daemon dials out.** `guestd` inside each machine opens one
   WebSocket to the control plane. Machines need no public IP and no inbound
   ports, and the control plane can live on any host.

## What the user sees

The app is a chat. Each chat is one thing being built.

```
┌ earlier chats ┬──── the chat ─────┬──────────── the panel ─────────────┐
│ New chat      │ questions, the    │ Graph │ Frontend │ Backend │ Dash… │
│ Today         │ proposed design,  │                                    │
│  · a shop     │ each step as it   │  the design as nodes and lines,    │
│  · a blog     │ happens, reports  │  or the page live, or the API,     │
│ Yesterday …   │                   │  or the machines and agents        │
└───────────────┴───────────────────┴────────────────────────────────────┘
```

- **Graph**: the design as a diagram. While it is a proposal the user
  changes machine sizes on it and approves it; afterwards each node shows
  its machine, whether it answers on its port, and any agent at work there,
  with the editor one click away.
- **Frontend**: the page the design's frontend serves, live, in a frame.
- **Backend**: the backend's API, drawn from the OpenAPI document every
  backend is made to serve, with a form to call each operation.
- **Dashboard**: the chat's machines and agents, and how each stands.

A chat is shared, like a channel: the user, the assistant (which runs the
project) and the agents it has set to work, each agent with a name of its
own ("tom") and the logo of what it builds as its picture. Writing `@tom`
sends a message straight to that agent; agents write back, show mockups and
ask the user to choose. Images and files can be attached, pasted or dropped.

A machine also has a page of its own (editor, terminal, overview), and a list
of all machines is in the account menu. [web/README.md](../web/README.md)
describes the frontend.

## Packages

| Path | Responsibility |
|---|---|
| `cmd/server` | Wires everything together; graceful shutdown. |
| `cmd/guestd` | Guest daemon, baked into the machine image. |
| `link` | `linkd`, the link daemon, also in every machine: a machine's end of the distributed harness. Rust, a Cargo project of its own. |
| `internal/auth` | Users, passwords, access tokens. |
| `internal/config` | Reads the environment and `.env`. |
| `internal/instance` | Instance state machine, ownership checks, guest credentials, and making one HTTP request to a port inside a machine for its owner. |
| `internal/assistant` | The AI assistant: chats, the loop that runs a conversation, its tools, designs, agents and what they know of each other, services, and the client for the coding agent in each machine. |
| `internal/sandbox` | `Provisioner` interface (lifecycle only). |
| `internal/sandbox/docker` | Runs machines as local Docker containers. |
| `internal/tunnel` | Tracks connected guests; opens terminals on them and connections to their ports. |
| `internal/presence` | Tracks who is connected (users with the app open, browsers on a machine), for stopping idle machines. |
| `internal/guest` | What `guestd` runs: the terminal server, the port forwarder, the reconnecting dial loop, supervisors for the editor and the coding agent, the copy of the team's record, and the machine's services. |
| `internal/guestapi` | Protocol between control plane and `guestd` (gRPC, defined in `guest.proto`). |
| `internal/httpapi` | REST handlers + router. |
| `internal/wsgateway` | WebSocket handlers: browser terminal, presence, guest tunnel. |
| `internal/portproxy` | Serves ports inside machines (the editor, dev servers) to the browser, each on its own origin. |
| `internal/llmgateway` | Lets the coding agents in machines use the model without holding its API key. |
| `internal/teamapi` | What machines ask the control plane: an MCP server their agents use (to look each other up, ask each other, leave notes, register services), and the record each machine keeps a copy of. |
| `internal/linkapi` | Protocol between control plane and `linkd` (gRPC, defined in `link.proto`). |
| `internal/link` | Tracks which machines have a link attached, and sends frames to them. |
| `internal/linkgw` | The gRPC server machines' link daemons connect to. |
| `internal/middleware` | HTTP auth middleware. |
| `internal/store` | Postgres repositories. No business logic. |
| `images/workspace` | Dockerfile for the machine images: one per stack (base, Go, C++, .NET, data), all with Ubuntu, dev tools, Python, Node.js, code-server, OpenCode and `guestd`. |
| `web` | Frontend: Next.js, talking to the API from the browser. A chat, with a panel of four tabs beside it. |

Dependency rule: transports (`httpapi`, `wsgateway`, `portproxy`,
`llmgateway`, `teamapi`, `linkgw`) → domain (`auth`, `instance`, `assistant`) →
infrastructure (`sandbox`, `tunnel`, `link`, `store`). Domain packages never
import transport types.

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

### Ports and the editor

`guestd` also forwards TCP: the `Forward` call connects to a loopback port on
the machine and relays bytes. On the control plane that stream is wrapped as
a `net.Conn`, and an ordinary HTTP reverse proxy uses it as its transport, so
HTTP and WebSockets to a machine's port work unchanged.

Every machine runs code-server on loopback port 13337, started and kept
alive by `guestd`, with its own authentication off: the tunnel is the only
way in.

Each port of each machine has its own hostname,
`<port>-<instance id>.<WORKSPACE_HOST>`, and therefore its own browser origin.
Code running on a user's machine can't read the app's storage (where the
access token lives) or another machine's pages.

Getting a browser in:

1. The app, holding the user's access token, asks the API for a URL. The API
   checks ownership and returns one containing a **ticket**: signed,
   single-use, valid for a minute, naming the user, instance and port.
2. Loading that URL on the machine's origin trades the ticket for a
   **session** cookie (12 hours, `HttpOnly`, host-only) and redirects.
3. Later requests carry the cookie. The proxy strips it before forwarding,
   and rejects requests whose `Origin` is anything but the same machine.

Tickets and sessions are signed with a key *derived* from the server secret,
not the secret itself, so neither can be passed off as an API access token,
or the reverse.

**Same site.** The app and the machines should share a registrable domain
(`coder.localhost` in development; `example.com` with `app.example.com` and
`*.example.com` in production). Browsers restrict cookies inside a frame
embedded from a different site; in that setup the editor loads but its
service worker can't register, which breaks its webviews. Different origins
on one site keep the isolation above without that problem. The remaining
exposure of sharing a site is that a page on a machine could set cookies for
the parent domain; the app authenticates with a header, not cookies, so that
gives it nothing.

### Guest credentials

Each instance gets a random token at creation. The machine receives it as an
environment variable; Postgres stores only its SHA-256. A database leak
therefore doesn't let anyone impersonate a machine. `guestd` strips the token
from the environment of the shells it starts.

## The link

Every machine runs a second small daemon, `linkd`, kept up by `guestd`. It
opens one gRPC stream to the control plane and keeps it open, dialling again
whenever it drops; either end can speak on it at any time.

```
linkd (Rust, in the machine) ── gRPC over HTTP/2, one stream ──► linkgw :8081 ──► link.Hub
```

- It attaches as the machine, with the machine's guest token, and only once
  it has answered a ping is it counted as attached.
- Both ends ping a quiet stream, so a machine or a server that vanished is
  noticed within half a minute.
- It is small on purpose: a static binary of about 1 MB, using under 1 MB
  of memory on one thread.

Machines that an approved design connects also open a stream to each other,
directly across their owner's private network. The control plane tells each
who its peers are and gives them a key; they prove themselves to each other
with it and report back which peers they are linked to.

Today the links only connect. They are the first steps of the distributed
harness, in which agents on different machines settle what passes between
their components before building and follow each other's progress while
they build: [HARNESS.md](HARNESS.md) is that design, and
[BACKEND.md, section 25](BACKEND.md#25-the-link) is how the link works.

## The assistant

A chat in which the user says what they want ("a backend on one machine and
a frontend on another"). Two agents do the work:

```
Browser ── chat ──► assistant (control plane) ── a task, over the tunnel ──► OpenCode in machine A
                      │        ▲                                          ──► OpenCode in machine B
                      │        └── model gateway ◄── the agents' own model calls ──┘
                      ▼
                  the model (DeepSeek, or anything OpenAI-compatible)
```

- **The assistant** lives in the control plane. It talks to the user and
  decides what to do. It can only call eleven tools: list, create, start and
  stop machines; give an agent a coding task; list agents; stop an agent;
  show a preview button; ask the user a question; propose a design; correct
  an approved design. It cannot delete a machine.
- **Coding agents** ([OpenCode](https://opencode.ai)) run in every machine,
  kept up by `guestd`. They write the files and run the commands. Each gets
  a task in words and returns a report in words. OpenCode is to be replaced
  by DeepSeek Harness, driven over ACP (decided 2026-10-18;
  [STATUS.md](STATUS.md#agents-at-once-and-doing-the-whole-job-2026-10-18)).

What follows from that split:

- **A design comes first.** The assistant asks at most three questions, then
  proposes an architecture: components, the machine and stack for each, and
  the connections between them, with its reasons. The user sees it as a
  diagram, can change it, and approves it; only then is anything built. A
  question or a proposal pauses the run, and the user's reply resumes it.
- **A chat is an append-only list of events in Postgres.** The conversation
  sent to the model is rebuilt from it each step. A run happens in the
  background, detached from any request, so closing the tab doesn't stop it,
  and a page opened mid-run shows everything so far.
- **Its replies are shown as they are written.** The model's answer is read
  as a stream; what it has written so far is held in memory and offered to
  whoever is watching the chat, and only the finished message is stored.
- **The user can talk to agents directly.** Each agent has a name of its
  own, given by the platform. A message that names agents (`@tom`) is given
  to them as a task, and each answer is posted in the chat; the assistant
  reads both but is not run. Agents also post in the chat, and ask the user
  to decide things, through two tools on the team's MCP server. Files the
  user attaches are read by the assistant where they are text, and put on a
  machine when they go with a task.
- **Agents work like processes.** Each has a name and a state, several can
  work on one machine at once, and they can be listed and stopped, by the
  assistant or by the user. The assistant can wait for an agent's report or
  start it in the background and carry on; a background agent that finishes
  wakes the assistant, which deals with the result without being asked.
- **Tools the model asks for together run at the same time**, so two
  machines, or two agents, are worked on in parallel.
- **The model's key never reaches a machine.** OpenCode is configured to use
  the control plane as its model provider, authenticating with the machine's
  own guest token. The gateway adds the real key and forwards. It answers a
  machine only while the assistant has it working on a task, so the token is
  not a standing free key for whoever has root on the machine.
- **Stacks.** A machine is created from an image with the toolchain its
  component needs: Go, C++, .NET, or PostgreSQL and Redis.
- **A user's machines share a private network** and resolve each other by
  name, which is what lets the frontend on one call `http://api:8000` on the
  other.
- **Connections are real.** A connection in the design gives the component
  the other's address as an environment variable, and one marked with a path
  lets a page call a backend on another machine through its own address: the
  port proxy passes that path on.
- **What agents build stays up.** Agents register servers and databases as
  services; the machine's guest daemon starts them, restarts them, and
  starts them again whenever the machine starts. Registering a service again
  starts it afresh, which is how changed code gets to run; the machine tells
  the control plane which registration it has started, so the control plane
  knows the program answering is the new one.
- **A backend describes its own API.** Every backend must serve an OpenAPI
  document at `/openapi.json`. The rule is in the assistant's instructions
  and in every agent's, and it is checked: when a backend's service comes
  up, the control plane asks it for the document and tells the agent if it
  is missing or empty. The frontend draws the API from it.
- **The owner can call a port from the app.** One endpoint makes an HTTP
  request to a port inside the caller's own machine and returns the answer
  as data. It is how the frontend reads that document and tries endpoints:
  a machine's own origin is another site, which the app's scripts cannot
  call.
- **Agents can look each other up, and ask each other.** The platform keeps
  a record of every agent: each task it was given, what it did step by step,
  and its report, plus notes agents leave for one another. An agent on any
  machine reads it through an MCP server on the control plane (tools such as
  `get_agent` and `share_note`), and the same record is kept as files in
  `~/.coder/team/` on every machine. One agent can also put a question to
  another and get its answer, with no involvement from the assistant. So the
  agent building a frontend finds out from the record or from the backend's
  agent, not from guesswork, which port and endpoints there are.

[BACKEND.md, section 16](BACKEND.md#16-the-assistant) has the detail.

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
- Each owner has a bridge network, `coder-user-<owner id>`, created with
  their first machine and removed with their last. A machine joins it with
  its instance name as an alias, so one user's machines find each other by
  name and can't reach anyone else's.

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
  stack TEXT NOT NULL,               -- the toolchain it came with: base, go, cpp, dotnet, data
  guest_token_hash BYTEA NOT NULL,
  created_at TIMESTAMPTZ, updated_at TIMESTAMPTZ
)
```

`ON DELETE RESTRICT`: a user can't be deleted while they have instances, so
machines are never orphaned by a cascade.

```sql
chats (id, owner_id → users ON DELETE CASCADE, title,
       status,                       -- CHECK: 'idle' | 'running' | 'waiting'
       created_at, updated_at)

chat_events (chat_id → chats ON DELETE CASCADE, seq,   -- PRIMARY KEY (chat_id, seq)
             kind,                   -- CHECK: 'user' | 'assistant' | 'tool' | 'activity' | 'agent' | 'message' | 'post' | 'error'
             payload JSONB, created_at)

-- files put in chats, by the user or by agents; at most 10 MB each
attachments (id, owner_id → users, chat_id → chats,   -- both ON DELETE CASCADE
             name, content_type, size, data BYTEA, created_at)

-- the design of what a chat is building; one per chat
architectures (chat_id → chats ON DELETE CASCADE, owner_id → users,
               status,               -- CHECK: 'proposed' | 'approved'
               design JSONB, created_at, updated_at)

-- programs the platform keeps running on a machine
services (instance_id → instances ON DELETE CASCADE, name, command, directory, port,
          created_at,
          updated_at)                -- set on every registration; it is the service's revision
                                     -- PRIMARY KEY (instance_id, name)

-- questions left for an agent that was busy, and what the user wrote to one
agent_messages (id, to_agent_id → agents ON DELETE CASCADE, from_machine, from_agent,
                from_owner, body, attachments UUID[], chat_seq, created_at, delivered_at)

-- every task an agent was given: what it did, step by step, and its report
agent_tasks (id, agent_id → agents ON DELETE CASCADE, task, background,
             status,                 -- CHECK: 'working' | 'done' | 'failed' | 'stopped'
             log TEXT[], report, started_at, finished_at)

-- what agents have written down for each other
agent_notes (id, owner_id → users ON DELETE CASCADE, machine, author, topic, body, created_at)

-- coding agents: a named worker on a machine, belonging to a chat
agents (id, owner_id → users, chat_id → chats, instance_id → instances,   -- all ON DELETE CASCADE
        name,                        -- its job; UNIQUE (chat_id, instance_id, name)
        handle,                      -- its own name ("tom"); UNIQUE (owner_id, handle)
        status,                      -- CHECK: 'idle' | 'working' | 'failed' | 'stopped'
        session_id,                  -- OpenCode's conversation, its memory
        task, background, activity, report, started_at, finished_at, created_at, updated_at)
```

## Planned

Roughly in order. The first group is what the review of 2026-10-05 found
wrong; [STATUS.md](STATUS.md#open-issues) has it in full.

1. **Close what is open**: the database reachable from machines, a database
   error signing users out, machine names shared between chats.
2. **Machines that belong to a project.** A machine's name is unique per
   user while a design is per chat, so two projects that both want `web` and
   `api` share them. Machines need to belong to the chat that made them, or
   carry its name.
3. **Limits**: how many machines a user may have, how fast accounts can be
   made, and how much of the model each user may spend, with a record of it.
   With that, per-user model keys (stored encrypted, never returned to the
   browser) and a choice of model.
4. **Keeping a long chat within what the model accepts**: summarising or
   trimming history, and trying a failed request to the model again.
5. **The distributed harness** ([HARNESS.md](HARNESS.md)): a small Rust
   program on every machine, with streams to the control plane and directly
   to the owner's other machines. Agents settle what passes between their
   components before any code is written, and follow each other's progress
   while they build. Two steps are done: tests in the repository for the
   whole flow (`tools/standin` and `tools/uicheck/build.mjs`), and the
   connections themselves: every machine linked to the control plane, and
   to the machines it works with ([above](#the-link)).
6. **Machines as small virtual machines**: each with its own kernel, which
   is the wall between users that a container with root is not.

   **A Fly.io provider** (`sandbox/fly`) is the plan again, since
   2026-10-18, first for testing on real hardware: machines as Fly Machines
   (Firecracker microVMs, billed by the second), one Fly app per user on its
   own private network. A rented server (the plan of 2026-10-12,
   [MVP.md](MVP.md#isolation-cheaply)) is for when Fly's memory price
   outweighs it; Kata Containers there would keep the change inside
   `sandbox/docker`. Either way a machine's memory and processors become
   changeable after it is made: stop, change, start. Notes for Fly:
   - Only volumes persist on Fly by default; the root filesystem resets on
     every start unless the machine is made with `persist_rootfs`, which
     Fly says is not fully reliable.
     Bake common toolchains into the image and steer per-user installs into
     the home directory, or later have `guestd` overlay the root filesystem
     onto the volume.
   - Stopped machines cost only storage, which is what makes stopping idle
     machines (already built) worth having there.
   - `GUEST_CONNECT_URL` becomes the control plane's public `wss://` address.
   - Memory there costs roughly eight to ten times a rented server's while
     a machine runs, and a stopped machine's disk and volumes are still
     paid for.
7. **Reconciler**: periodically compare Postgres with the provider and fix
   drift (machines deleted outside the platform, machines with no row,
   instances left in a transitional status).
8. **CI and deployment the platform runs itself**: triggered by a change,
   with results kept; deployment once there is somewhere to deploy to; and
   designs that include real cloud services with their credentials kept
   safely.
9. **MCP server for users** (`internal/mcpapi`): the same instance and guest
   operations, exposed as tools to a user's own AI clients, authenticated
   with per-user API tokens. (The MCP server that exists today is a different
   one: it serves the agents inside machines, see above.)
10. **Auth hardening**: revocable tokens, rotating guest tokens, input
    validation, login timing.
11. **More than one server process**: guest connections are held in memory, so
    this needs shared presence (Redis) and routing to the process that holds a
    given tunnel; startup recovery must then stop assuming it is alone.

## Known gaps

The lasting ones, by design or by omission. Bugs waiting to be fixed are in
[STATUS.md](STATUS.md#open-issues).

- **Docker is not a boundary between users.** A machine is a container whose
  user has root, with CPU and memory limits and nothing else: no limit on
  processes, no enforced disk size, no hardened runtime. Right for one
  person on their own computer.
- **Machines can reach the host.** `host.docker.internal` is how `guestd`
  finds a control plane started with `make run`; it also reaches anything
  else the host publishes on all interfaces.
- Machines come in three sizes and three disk sizes, chosen at creation and
  fixed afterwards, which the owner wants changed (Planned, 6). On local
  Docker the disk size is recorded but not enforced.
- If the server dies between creating a machine and saving its ID, that
  machine is orphaned until the reconciler exists. Its labels identify it.
- No limit on how many instances a user can create, or on sign-ups.
- A sign-in token never expires by default (`ACCESS_TOKEN_TTL=0`) and can't
  be revoked: it works until `JWT_SECRET` changes or its user is deleted.
  Chosen for convenience while the project has no real users.
- A machine's guest token is set when the machine is created and never
  changes. With it, anything that can reach the control plane can act as
  that machine: read its owner's team record and register services on it.
- A port session, once issued, lasts its 12 hours even if the user signs out
  or the instance changes hands; it is not checked against the database.
- Used tickets, who is connected, which machines have a link attached, runs
  in progress and what machines report are remembered in memory, which is
  only correct with a single server process.
- The link is not encrypted by `linkd` itself, which cannot yet dial a
  server over TLS. That is fine across the Docker bridge to the host, and
  has to be dealt with before machines run anywhere else.
- The whole of a chat is sent to the model at every step, and nothing
  shortens it.
- Every user's assistant spends the server's one model key, without limits
  or accounting.
- The assistant's mechanics are tested end to end with a stand-in for the
  model (`tools/standin`, driven by `tools/uicheck/build.mjs`). With the
  real model it has had one chat; how well a real model follows its
  instructions is only seen in use.
- The coding agent takes most of a small machine's memory: several hundred
  megabytes at rest and up to 900 MB at work, of 1 GB
  ([HARNESS.md](HARNESS.md#what-was-measured)).
