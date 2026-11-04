# Backend architecture, in detail

This document explains how the backend works: what each part does, how the
parts talk to each other, and why they are built the way they are. It
describes the code as it is today. For the short overview and the roadmap,
see [ARCHITECTURE.md](ARCHITECTURE.md); for the HTTP and WebSocket contract a
client codes against, see [API.md](API.md).

It assumes you can read Go and know what HTTP and a database are. Everything
else is explained where it first appears.

## Contents

1. [What the backend does](#1-what-the-backend-does)
2. [The moving parts](#2-the-moving-parts)
3. [Vocabulary](#3-vocabulary)
4. [Code layout and the dependency rule](#4-code-layout-and-the-dependency-rule)
5. [Starting up and shutting down](#5-starting-up-and-shutting-down)
6. [Configuration](#6-configuration)
7. [The database](#7-the-database)
8. [Authentication](#8-authentication)
9. [Instances and their lifecycle](#9-instances-and-their-lifecycle)
10. [Running machines: the sandbox provider](#10-running-machines-the-sandbox-provider)
11. [The tunnel](#11-the-tunnel)
12. [The guest daemon](#12-the-guest-daemon)
13. [The terminal, end to end](#13-the-terminal-end-to-end)
14. [Ports and the editor](#14-ports-and-the-editor)
15. [Stopping idle machines](#15-stopping-idle-machines)
16. [The assistant](#16-the-assistant)
17. [The HTTP layer](#17-the-http-layer)
18. [Three requests, step by step](#18-three-requests-step-by-step)
19. [When things go wrong](#19-when-things-go-wrong)
20. [Security model](#20-security-model)
21. [Timeouts and limits](#21-timeouts-and-limits)
22. [Tests](#22-tests)
23. [Assumptions and known gaps](#23-assumptions-and-known-gaps)
24. [Where to change things](#24-where-to-change-things)
25. [The link](#25-the-link)

---

## 1. What the backend does

A user signs up, creates one or more **machines**, and uses each machine from
the browser through a terminal and an editor (VS Code). A machine is a Linux
environment with root access whose home folder survives being stopped and
started.

The backend's job is to:

- know who the user is;
- create, start, stop and delete machines, and remember which belong to whom;
- carry a terminal session between the browser and a shell inside a machine;
- serve programs running inside a machine (the editor, a dev server) to the
  browser;
- run an AI assistant that builds things for the user, by creating machines
  and directing a coding agent inside each one;
- do all of that without machines being reachable from the internet.

Today a machine is a Docker container on the same computer as the backend.
The design keeps that detail in one small package so it can be replaced by
cloud virtual machines later.

## 2. The moving parts

There are three programs, built from this repository:

- **`server`** ([cmd/server](../cmd/server/main.go)) is the backend proper.
  One process. It is often called the *control plane* below.
- **`guestd`** ([cmd/guestd](../cmd/guestd/main.go)) is a small daemon that
  runs *inside every machine*. It is baked into the machine image.
- **`linkd`** ([link/](../link/src/main.rs)) is the link daemon, also inside
  every machine and kept running by `guestd`. It is written in Rust, where
  the other two are Go. Section 25 is about it.

And the things they depend on: PostgreSQL, the Docker daemon, two programs
that `guestd` runs inside each machine (code-server, which is VS Code for the
browser, and OpenCode, a coding agent), and, for the assistant only, a
language model reached over the internet (DeepSeek by default).

```
 ┌───────────┐  HTTPS: REST API                 ┌────────────────────────────┐
 │           │ ───────────────────────────────► │                            │      ┌────────────┐
 │           │  WebSocket: terminal             │                            │ ───► │ PostgreSQL │
 │  Browser  │ ───────────────────────────────► │      server process        │      └────────────┘
 │           │  HTTPS + WebSocket: editor,      │      (control plane)       │      ┌────────────┐
 │           │  on <port>-<id>.<workspace host> │                            │ ───► │   Docker   │
 │           │ ───────────────────────────────► │                            │      │   daemon   │
 └───────────┘                                  └────────────────────────────┘      └─────┬──────┘
                                                              ▲                           │ creates
                                                              │ one WebSocket,            ▼
                                                              │ opened BY the machine  ┌──────────────────────────┐
                                                              └─────────────────────── │ Machine (container)      │
                                                                                       │                          │
                                                                                       │  guestd                  │
                                                                                       │   ├─ shells on PTYs      │
                                                                                       │   ├─ port forwarding     │
                                                                                       │   ├─ runs code-server    │
                                                                                       │   └─ runs OpenCode       │
                                                                                       │  /home/coder  ← volume   │
                                                                                       └──────────────────────────┘
```

The arrow at the bottom is the unusual one. The server never connects *to* a
machine. Each machine's `guestd` connects *out* to the server and keeps that
connection open; the server then sends its requests down that same
connection. Section 11 explains why and how. `linkd`, not drawn, does the
same on a second connection of its own (section 25).

## 3. Vocabulary

| Term | Meaning |
|---|---|
| **Instance** | The record of a machine: a row in Postgres with an owner, a name and a status. What the API calls a machine. |
| **Machine** | The running thing an instance refers to. A Docker container today. |
| **Volume** | The machine's persistent disk, mounted at `/home/coder`. It outlives stop and start. |
| **Control plane** | The `server` process. |
| **Guest**, **guestd** | The daemon inside a machine, and by extension the machine's end of the tunnel. Nothing to do with AI agents. |
| **Tunnel** | The long-lived connection from a machine's `guestd` to the control plane. |
| **Hub** | The control plane's table of which machines currently have a tunnel open. |
| **Provisioner**, **provider** | Code that knows how to create and control machines on one kind of infrastructure. The Docker provisioner is the only one. |
| **Handle** | The provider's own IDs for a machine and its volume (a container ID and a volume name). |
| **PTY** | Pseudo-terminal: the kernel object that makes a program believe it is attached to a real terminal. |
| **Access token** | The JWT a signed-in user sends with API requests. |
| **Guest token** | A per-instance secret that proves a tunnel really comes from that instance's machine. |
| **Ticket**, **session** | The two tokens that let a browser reach a port on a machine (section 14). |
| **Origin** | Scheme + host + port of a web page. Browsers isolate pages by origin. |
| **Assistant** | The AI the user chats with. It runs in the control plane and decides what to do; it never touches a file itself (section 16). |
| **Coding agent**, **worker** | OpenCode running inside a machine. It does the actual work there: writes files, runs commands. The assistant gives it tasks. |
| **Model** | The language model both of them think with. One model, configured on the server. |
| **Chat**, **event**, **run** | A conversation with the assistant; one entry in its history; one stretch of the assistant working, from a user's message until it answers or asks something. |
| **Tool** | Something the model can ask the assistant's code to do, such as create a machine. |
| **Model gateway** | The endpoint through which coding agents reach the model without holding its API key. |

## 4. Code layout and the dependency rule

```
cmd/
  server/main.go         builds every component and connects them
  guestd/main.go         the daemon inside machines

link/                    linkd, the link daemon inside machines (Rust, its own Cargo project)

internal/
  config/                environment → Config struct
  store/                 Postgres: connection pool and repositories
  auth/                  sign-up, login, access tokens
  middleware/            HTTP middleware that requires an access token
  instance/              the instance state machine; ownership; guest tokens
  assistant/             the AI assistant: chats, the run loop, its tools, the coding agents
  sandbox/               the Provisioner interface
  sandbox/docker/        Provisioner implemented with Docker
  presence/              who is connected: users with the app open, browsers on a machine
  guestapi/              the protocol between server and guestd (protobuf/gRPC)
  tunnel/                server side of the tunnel: the Hub, terminals, port connections
  guest/                 guestd's logic: dial loop, terminal server, port forwarder,
                         and keeping the editor and the coding agent running
  httpapi/               REST handlers and the router
  wsgateway/             WebSocket handlers: browser terminal, guest connect
  portproxy/             serves machine ports to the browser
  llmgateway/            lets coding agents use the model without its key
  teamapi/               what machines ask the control plane for: an MCP server for their agents, and
                         the record and list of services each machine keeps
  linkapi/               the protocol between the server and linkd (protobuf/gRPC)
  link/                  server side of the links: the Hub of machines with one attached
  linkgw/                the gRPC server machines' link daemons connect to

images/workspace/        Dockerfile for the machine image
migrations/              database schema, as ordered SQL files
```

### Three layers

```
 transport        httpapi    wsgateway    portproxy    llmgateway    middleware
 (speaks HTTP)       │           │            │             │             │
                     ▼           ▼            ▼             ▼             ▼
 domain                  auth          assistant ──────► instance
 (rules)                   │               │           ┌────┼─────────┐
                           ▼               ▼           ▼    ▼         ▼
 infrastructure          store           store      store  sandbox  tunnel ── guestapi
 (talks to the outside)                                       │
                                                         sandbox/docker
```

The rule: **a package may import packages below it, never above it.**

- *Transport* packages turn HTTP requests and WebSockets into calls on the
  domain, and domain results back into responses. They contain no rules about
  what is allowed.
- *Domain* packages (`auth`, `instance`, `assistant`) hold the rules: who may
  do what, and in which order things happen. They know nothing about HTTP.
  That is what let the assistant be added without touching the instance
  rules: it drives the same `instance.Service` the REST handlers do, and so
  could a command-line tool or an MCP server.
- *Infrastructure* packages talk to something external (Postgres, Docker, a
  machine) and contain no rules.

This is checked by the compiler in one direction: Go refuses import cycles,
so `store` cannot import `auth`, and `instance` cannot import `httpapi`.
The actual import graph is:

```
cmd/server    → assistant auth config httpapi instance llmgateway portproxy sandbox/docker store teamapi tunnel wsgateway
httpapi       → assistant auth guestapi instance llmgateway middleware portproxy store teamapi wsgateway
wsgateway     → auth guestapi instance tunnel
portproxy     → instance
llmgateway    → instance
teamapi       → assistant instance store
middleware    → auth store
assistant     → guestapi instance store
instance      → guestapi presence sandbox store tunnel
auth          → store
tunnel        → guestapi
sandbox/docker→ sandbox
cmd/guestd    → guest
guest         → guestapi
```

`guestd` only pulls in `guest` and `guestapi`, so the binary shipped inside
machines contains no database, Docker or HTTP-API code.

### Conventions used throughout

- **Constructors return concrete types** (`*auth.Service`,
  `*store.InstanceRepository`) and are wired by hand in `main.go`. There is no
  dependency-injection framework. Interfaces appear only where there is a
  real second implementation or a test double: `sandbox.Provisioner`,
  `portproxy.Machines`, and in the assistant, whose tests replace the model,
  the database and the machines (`assistant.LLM`, `Store`, `Machines`,
  `Instances`, `Sessions`).
- **Sentinel errors.** Each package exports errors such as
  `instance.ErrNotFound`. A layer translates the errors of the layer below
  into its own (`store.ErrNotFound` → `instance.ErrNotFound`), so a caller
  only needs to know the errors of the package it calls. Callers compare with
  `errors.Is`.
- **Errors are wrapped with context** as they travel up
  (`fmt.Errorf("create container: %w", err)`), and logged once, at the top, by
  the transport layer.
- **`context.Context` is the first argument** of anything that does I/O, and
  carries cancellation and deadlines.

## 5. Starting up and shutting down

Everything is assembled in `run()` in
[cmd/server/main.go](../cmd/server/main.go), in this order:

1. **Load configuration** from the environment. Missing required values stop
   the process here, with a clear message.
2. **Create the root context**, which is cancelled when the process receives
   `SIGINT` (Ctrl+C) or `SIGTERM`.
3. **Connect to Postgres** and ping it, so a bad `DATABASE_URL` fails at
   startup and not on the first request.
4. **Connect to Docker** and ping it.
5. **Build the components**: repositories → `auth.Service` → `tunnel.Hub` →
   `instance.Service` → the assistant (`assistant.Fleet`, then
   `assistant.Service`, with a model client only if a key is configured) →
   `wsgateway.Gateway` → `portproxy.Proxy` → `llmgateway.Gateway` → router;
   and `link.Hub` → the link gateway's gRPC server (section 25).
6. **Recover interrupted operations**: any instance whose status says an
   operation is in progress is marked `error` (section 9 explains why this is
   safe). Likewise any chat still marked `running` is set back to `idle`,
   with a note in it saying the server restarted.
7. **Start the HTTP server and the link gateway**, each in a goroutine and
   on a port of its own, and wait for either a fatal error from one of them
   or the root context being cancelled.

The HTTP server's handler is `proxy.Wrap(router)`: every request is first
checked for a machine hostname; if it has one, the port proxy handles it, and
otherwise it goes to the API router.

### Shutdown

On a signal, `srv.Shutdown` stops accepting connections and waits up to 30
seconds for requests in flight.

`Shutdown` does not close WebSockets: once a connection has been upgraded,
`net/http` no longer manages it. To end them, the server is configured with
`BaseContext` returning the root context. Every request's context is derived
from it, so cancelling the root cancels them all; terminal and tunnel handlers
notice and return.

The link gateway is simply stopped. Its streams last as long as machines
do, so there is nothing in flight to wait for.

Machines are not stopped when the server stops. Their `guestd` reconnects when
the server is back, and so does their link daemon.

A run of the assistant in progress is cut off, like any other goroutine. The
next startup frees its chat (step 6); the user sends a message to carry on.

## 6. Configuration

Read once at startup by [internal/config](../internal/config/config.go), from
the environment, after loading a `.env` file if one exists.

| Variable | Required | Default | Purpose |
|---|---|---|---|
| `DATABASE_URL` | yes | – | Postgres connection string. |
| `JWT_SECRET` | yes | – | Signs access tokens; a key derived from it signs port tickets and sessions. |
| `PORT` | no | `8080` | Port the server listens on. |
| `WORKSPACE_IMAGE` | no | `coder-workspace:dev` | Docker image new machines are created from. |
| `GUEST_CONNECT_URL` | no | `ws://host.docker.internal:<PORT>/guest/connect` | Where machines dial the server. Must be reachable *from inside a machine*. |
| `LINK_ADDR` | no | `:8081` | Where the server listens for machines' link daemons. gRPC, so a port of its own. |
| `LINK_CONNECT_URL` | no | `http://host.docker.internal:<port of LINK_ADDR>` | Where machines dial that. Must be reachable *from inside a machine*. |
| `WORKSPACE_HOST` | no | `coder.localhost:<PORT>` | Machines' ports are served at `<port>-<instance id>.<WORKSPACE_HOST>`. |
| `WORKSPACE_SCHEME` | no | `http` | `http` or `https`: how browsers reach `WORKSPACE_HOST`. |
| `ACCESS_TOKEN_TTL` | no | `0` | How long a sign-in token is valid, as a duration (`15m`, `24h`). `0` means it never expires. |
| `IDLE_STOP_AFTER` | no | `2m` | How long a running machine may have nobody connected before it is stopped. `0` turns it off. |
| `CORS_ALLOWED_ORIGINS` | no | none | Comma-separated browser origins allowed to call the API and open terminals. |
| `DEEPSEEK_API_KEY` | no | none | Key for the assistant's model. Without it the assistant is off and everything else works. |
| `LLM_BASE_URL` | no | `https://api.deepseek.com/v1` | Root of the model's API. Any service that speaks the OpenAI chat-completions protocol works. |
| `LLM_MODEL` | no | `deepseek-chat` | The model to ask for, by the assistant and the coding agents alike. It must support tool calling. Which names a key can use differs: `GET <LLM_BASE_URL>/models` lists them. |
| `LLM_CONTEXT_WINDOW` | no | `1000000` | The model's context window, in tokens: how much a coding agent holds before it summarises its session. Set it to the model's real one (the default is DeepSeek V4's); less makes agents forget their work part-way through a task. Passed to new machines. |
| `LLM_MAX_OUTPUT` | no | `384000` | The longest reply a coding agent may ask the model for, in tokens. Less than `LLM_CONTEXT_WINDOW`. Passed to new machines. |
| `AGENT_REASONING_EFFORT` | no | `max` | How hard the model thinks for coding agents: `low`, `high` or `max`. The gateway sets it on every request from a machine. Harder costs more output tokens; `max` is what DeepSeek's own comparisons of coding agents use. |

## 7. The database

Postgres is the source of truth for users, instances and chats. The schema is in
[migrations/](../migrations), applied in filename order by
[golang-migrate](https://github.com/golang-migrate/migrate). Each migration
has an `up` file and a `down` file that undoes it.

### `users`

| Column | Type | Notes |
|---|---|---|
| `id` | `UUID` | Primary key, generated by the application. |
| `email` | `TEXT` | `UNIQUE`. Lower-cased and trimmed before it is stored or looked up. |
| `password_hash` | `TEXT` | bcrypt. |
| `display_name`, `organization`, `subscription` | `TEXT` | `subscription` is always `free`; it is set by the server, never taken from the client. |
| `created_at` | `TIMESTAMPTZ` | |

### `instances`

| Column | Type | Notes |
|---|---|---|
| `id` | `UUID` | Primary key. Also used to name the container and volume. |
| `owner_id` | `UUID` | `REFERENCES users (id) ON DELETE RESTRICT`. |
| `name` | `TEXT` | `UNIQUE (owner_id, name)`: unique per user, not globally. |
| `status` | `TEXT` | `CHECK` limits it to the seven statuses in section 9. |
| `provider` | `TEXT` | `CHECK`: `docker` or `fly`. |
| `machine_id`, `volume_id` | `TEXT`, nullable | The provider's IDs. `NULL` until the machine has been created. |
| `cpus`, `memory_mb`, `disk_gb` | `INT` | Each `CHECK`ed to be positive. |
| `stack` | `TEXT` | The toolchain the machine came with: `base`, `go`, `cpp`, `dotnet` or `data` (section 10). |
| `guest_token_hash` | `BYTEA` | SHA-256 of the guest token. |
| `created_at`, `updated_at` | `TIMESTAMPTZ` | |

Decisions worth knowing:

- **`ON DELETE RESTRICT`**, not `CASCADE`. Deleting a user who still has
  instances fails. With `CASCADE` the rows would vanish while the containers
  kept running with nothing pointing at them.
- **Constraints live in the database** as well as in the code. Even a bug in
  the Go code cannot store a status that doesn't exist.
- **No separate index on `owner_id`.** The unique constraint creates an index
  on `(owner_id, name)`, and Postgres can use an index for a query that
  filters on its leading column, which covers "all instances of this user".
- **Nullable columns are pointers in Go** (`MachineID *string`), so "not set"
  and "empty string" are different things.

### `chats`, `chat_events`, `agents`

The assistant's conversations (section 16).

`chats`:

| Column | Type | Notes |
|---|---|---|
| `id` | `UUID` | Primary key. |
| `owner_id` | `UUID` | `REFERENCES users (id) ON DELETE CASCADE`. |
| `title` | `TEXT` | The first line of the first message. |
| `status` | `TEXT` | `CHECK`: `idle`, `running` or `waiting`. |
| `created_at`, `updated_at` | `TIMESTAMPTZ` | `updated_at` moves with every event, and orders the list of chats. |

`chat_events`, the history of a chat, append-only:

| Column | Type | Notes |
|---|---|---|
| `chat_id` | `UUID` | `REFERENCES chats (id) ON DELETE CASCADE`. |
| `seq` | `BIGINT` | 1, 2, 3… within the chat. Primary key with `chat_id`. |
| `kind` | `TEXT` | `CHECK`: `user`, `assistant`, `tool`, `activity`, `agent`, `message`, `post` or `error`. |
| `payload` | `JSONB` | Shape depends on `kind`. Postgres stores it without interpreting it. |
| `created_at` | `TIMESTAMPTZ` | |

`agents`, the table of coding agents: each a named worker on one machine,
belonging to one chat (section 16):

| Column | Type | Notes |
|---|---|---|
| `id` | `UUID` | Primary key. |
| `owner_id`, `chat_id`, `instance_id` | `UUID` | Each `ON DELETE CASCADE`: an agent goes with its user, its chat or its machine. |
| `name` | `TEXT` | Its job, chosen by the assistant ("backend", "tests"). `UNIQUE (chat_id, instance_id, name)`. |
| `handle` | `TEXT` | Its own name, chosen by the platform ("tom", "ava"): what the user writes after an @. `UNIQUE (owner_id, handle)`, so no two of a user's agents share one, in any chat. |
| `status` | `TEXT` | `CHECK`: `idle`, `working`, `failed` or `stopped`. |
| `session_id` | `TEXT`, nullable | OpenCode's ID for the agent's conversation, which is how the agent remembers earlier tasks. `NULL` until its first task. |
| `task`, `background` | `TEXT`, `BOOLEAN` | The task it is on (or was last on), and whether the assistant is waiting for it. |
| `activity` | `TEXT` | What it is doing right now, while working. |
| `report` | `TEXT` | How its last task ended: the report, or why it failed. |
| `started_at`, `finished_at` | `TIMESTAMPTZ`, nullable | Of its current or last task. |
| `created_at`, `updated_at` | `TIMESTAMPTZ` | |

`agent_tasks` is the history behind that: one row for every task an agent
was given (`task`, `background`, `status`: `working`, `done`, `failed` or
`stopped`), with `log`, a `TEXT[]` of what the agent did step by step (up to
300 lines), and its `report`. The `agents` row says what an agent is doing
now; these rows say everything it has done.

`agent_messages` holds questions one agent asked another that was busy:
who it is for, who asked, the question, and when it was handed over. It also
holds what the user wrote to an agent that was busy (`from_owner`), with the
IDs of the files that came with it (`attachments`, a `UUID[]`) and the chat
event it was (`chat_seq`), so that the answer can be shown as a reply to it.

`attachments` holds the files put in chats, by the user or by agents, whole:
`owner_id`, `chat_id` (both `ON DELETE CASCADE`), `name`, `content_type`,
`size` and `data` (`BYTEA`), at most 10 MB each (the application's limit).
Events refer to them by ID. A file uploaded and never sent stays until its
chat is deleted.

`architectures` holds the design of what a chat is building, one row per
chat: `status` (`proposed` or `approved`) and `design`, a `JSONB` document of
components and connections that only the application reads (section 16).

`services` holds the programs the platform keeps running on a machine:
`(instance_id, name)`, the `command`, the `directory` it runs in and its
`port`. Its `updated_at` is set on every registration, with
`clock_timestamp()` so that two in quick succession differ, and read back
in microseconds as the service's **revision** (section 12). It cascades
from the instance.

`agent_notes` holds what agents have written down for each other: `owner_id`,
the `machine` it was written from (by name, so the note outlives the
machine), the `author` (the agent's name, as it gave it), a `topic` and a
`body`.

Here `CASCADE` is right where it was wrong for instances: chats and agents own
nothing outside the database, so there is nothing to orphan. (An agent's
conversation lives in OpenCode on the machine's disk, and goes when the
machine does.)

### The `store` package

[internal/store](../internal/store) is the only package that contains SQL. It
uses [pgx](https://github.com/jackc/pgx) directly, with a connection pool
(`pgxpool`), and no ORM.

`UserRepository`: `CreateUser`, `GetUserByEmail`, `GetUserByID`.

`InstanceRepository`:

| Method | What it does | Detail that matters |
|---|---|---|
| `Create` | Inserts a row | Maps Postgres error `23505` (unique violation) to `ErrInstanceNameTaken`. |
| `GetByID(id, ownerID)` | Fetches one | The owner is part of the `WHERE` clause. Someone else's instance is indistinguishable from a missing one. |
| `ListByOwner` | All of a user's instances | Oldest first. |
| `GuestTokenHash(id)` | The stored token hash | Not owner-scoped: the caller is a machine, not a user. |
| `UpdateStatus` | Sets the status | Returns `ErrNotFound` if no row matched. |
| `MarkProvisioned` | Sets machine ID, volume ID and status | One statement, so a row never claims a status that implies a machine without having its ID. |
| `TransitionStatus(id, from, to)` | Sets status only if it is currently one of `from` | The compare-and-set that section 9 is built on. Reports whether it changed a row. |
| `SetStatusWhere(from, to)` | Bulk status change | Used once, at startup. |
| `Delete` | Removes the row | |

`ChatRepository` has the same shape for chats (`Create`, `GetByID(id,
ownerID)`, `ListByOwner`, `TransitionStatus`, `SetStatus`, `Delete`), plus:

| Method | What it does | Detail that matters |
|---|---|---|
| `AppendEvent(chatID, kind, payload)` | Adds to the end of a chat's history | The next `seq` is computed and inserted in one statement. Two appends to one chat racing would pick the same number and one would fail, so the assistant serializes them per chat. |
| `ListEvents(chatID, after)` | Events with `seq > after`, in order | `after` is what makes resuming a stream cheap. |


`AgentRepository`:

| Method | What it does | Detail that matters |
|---|---|---|
| `Ensure(ownerID, chatID, instanceID, name)` | The chat's agent of that name on that machine, created if new | One `INSERT … ON CONFLICT`, so two tasks naming a new agent at once get the same row. |
| `Begin(id, task, background)` | Marks it working on a task | Only if it isn't already: `WHERE status <> 'working'`, the same compare-and-set as instances. Reports whether it won. |
| `Finish(id, status, report)` | Records how the task ended | |
| `SetActivity(id, text)` | What it is doing right now | Written for every step the agent takes. |
| `Session`, `SetSession` | OpenCode's ID for its conversation | |
| `GetByID(id, ownerID)`, `ListByOwner`, `ListByChat`, `ListByStatus` | Reads | Joined with `instances` and `chats` for the machine's name and the chat's title. `ListByOwner` puts working agents first and stops at 100. |
| `ListTasks(ownerID, perAgent)` | The latest tasks of each of the owner's agents | A window function numbers each agent's tasks from the newest, so one query returns "the last five of each". |
| `AddNote`, `ListNotes(ownerID, limit)` | Notes agents leave each other | |

`Begin`, `Finish` and `SetActivity` each change the agent's row and its
current row in `agent_tasks` in one statement (a data-modifying `WITH`), so
the two can't disagree.

## 8. Authentication

[internal/auth](../internal/auth/service.go) has one type, `Service`.

- **Register**: normalise the email, hash the password with bcrypt, insert the
  user, return an access token. A duplicate email becomes `ErrEmailTaken`.
- **Login**: look the user up, compare the password with bcrypt, return an
  access token. An unknown email and a wrong password both return
  `ErrInvalidCredentials`, so the response doesn't reveal which emails are
  registered.
- **Access token**: a JWT signed with HMAC-SHA256 using `JWT_SECRET`. Its
  claims are `sub` (the user's ID) and `iat`. By default that is all, and a
  token with no expiry claim never expires: a sign-in lasts until the user
  signs out. With `ACCESS_TOKEN_TTL` set to a duration, tokens also carry
  `exp` and stop being accepted when it passes.
- **Renew**: given a request that carried a valid token, issue a fresh one.
  Where tokens expire, a client that calls this before each one does stays
  signed in for as long as it keeps running, while every individual token
  stays short-lived.
- **What a token that never expires costs.** It cannot be taken back. A
  token copied from a browser works until `JWT_SECRET` is changed, which
  signs every user out at once, or its user is deleted. That is a deliberate
  choice for now, while there is one developer and no real users; revocable
  sessions come with the refresh-token work listed below, and until then
  `ACCESS_TOKEN_TTL=15m` gives back short-lived tokens.
- **ParseJWT**: verifies the signature and expiry, then loads the user from
  Postgres. A deleted user is therefore locked out immediately, at the cost of
  one query per authenticated request.

[internal/middleware](../internal/middleware/auth.go) provides `RequireAuth`.
It reads `Authorization: Bearer <token>`, calls `ParseJWT`, and puts the user
into the request context under an unexported key; handlers get it back with
`UserFromContext`. Any failure is a `401`.

Known weaknesses, deliberately left for later (they are listed in
[ARCHITECTURE.md](ARCHITECTURE.md#planned)): no revocable refresh tokens, no input
validation on sign-up, login timing differs between known and unknown emails,
and a database outage is reported as `401`.

## 9. Instances and their lifecycle

[internal/instance](../internal/instance/service.go) is the heart of the
backend. `instance.Service` decides which operations are allowed, records
them in Postgres, and drives the provider to make them real.

### Statuses

```
                       ┌──────────── start ────────────┐
                       │                               ▼
  create ──► creating ──► stopped ──► starting ──► running
                              ▲                        │
                              └── stopping ◄── stop ───┘

  stopped | running | error  ──► deleting ──► (row removed)

  any operation that fails ──► error ──► start / stop (retry) or delete
```

| Status | Kind | Meaning |
|---|---|---|
| `stopped` | settled | Machine exists and is not running. |
| `running` | settled | Machine is started. |
| `error` | settled | The last operation failed. |
| `creating`, `starting`, `stopping`, `deleting` | transitional | An operation is in progress. |

`running` means the *machine* is started. Whether its `guestd` has connected
yet is a separate fact that only the Hub knows (section 11).

### The three mechanisms

Every operation that changes a machine uses the same three pieces.

**1. Claim the instance.** `begin()` runs one SQL statement:

```sql
UPDATE instances SET status = 'starting', updated_at = NOW()
WHERE id = $1 AND status = ANY('{stopped,error}')
```

The check ("is it stopped?") and the change ("it is now starting") happen
inside Postgres as a single step. If two requests to start the same instance
arrive together, both run this statement, exactly one of them changes a row,
and the other sees zero rows changed and returns `ErrBusy`. Checking the
status in Go and then updating it would leave a gap in which both could
pass. The status column doubles as a lock, and one that survives a process
restart.

**2. Finish what was started.** Normally, when a client disconnects, Go
cancels the request's context and everything using it stops. For an operation
that has already claimed an instance that would be harmful: the status would
stay `starting` for ever. So after the claim, the work runs on
`operationContext(ctx)`: the same context with cancellation removed
(`context.WithoutCancel`) and a two-minute timeout added. The client can go
away; the operation completes and records its result.

**3. Record failure.** If the provider call fails, `fail()` sets the status to
`error` and returns the original error. If recording that also fails, both
errors are returned together with `errors.Join`, so neither is lost.

### Each operation

**Create** (`ownerID`, `name`, `size`, `stack`)

1. Trim the name and check it against
   `^[a-z0-9]([a-z0-9-]{0,38}[a-z0-9])?$`. This also makes every name a valid
   hostname, which is why it can be used as the machine's hostname. Check
   that the size is one on offer ([size.go](../internal/instance/size.go):
   three levels of CPU and memory, three disk sizes); anything else is
   `ErrInvalidSize`. The sizes are defined in that one file, and both the
   REST API and the assistant's instructions are built from it. The stack
   ([stack.go](../internal/instance/stack.go)) is checked the same way, and
   chooses the image the machine is created from. If that image has not
   been built on this server, the row is removed again and the error is
   `ErrStackUnavailable`: nothing was made.
2. Generate the guest token (random text); keep only its SHA-256.
3. Insert the row with status `creating`. A duplicate name is `ErrNameTaken`.
4. Ask the provisioner to create the machine, passing the instance ID, guest
   token and connect URL as environment variables.
5. On success, `MarkProvisioned`: store the handle and set status `stopped`.
   If *that* fails, delete the machine just created, so nothing is left
   running that the database doesn't know about.

Creation doesn't start the machine. That is a separate call, so the handle is
safely stored before anything runs.

**Start**: load the instance (owner-scoped). Already `running`: return it
unchanged. No machine: `ErrNotProvisioned` if creation failed, `ErrBusy` if it
is still being created. Otherwise claim `stopped|error → starting`, call the
provisioner, set `running`.

**Stop**: the mirror image. Already `stopped`: no-op. Claim
`running|error → stopping`, call the provisioner, set `stopped`.

**Delete**: claim `stopped|running|error → deleting`, ask the provisioner to
remove machine and volume, then delete the row.

**Get / List**: owner-scoped reads. `Get` first checks that the ID is a UUID;
passing anything else to Postgres would produce a type error, not "no
rows", so it is turned into `ErrNotFound` up front.

Starting a running instance and stopping a stopped one succeed without doing
anything, and the provider's own operations are idempotent too. A client that
didn't see the response can simply repeat the request.

### Recovery at startup

Operations run only inside the server process. So when the process starts, no
operation can be in progress, and any instance whose status is transitional
was cut off by a crash or restart. `RecoverInterrupted` sets all of those to
`error`. From `error` the user can retry (start or stop) or delete.

This reasoning holds only while there is exactly one server process.

### Operations for the other layers

`instance.Service` is also the gate in front of the tunnel:

| Method | Called by | Check it performs |
|---|---|---|
| `OpenTerminal(ownerID, id, size)` | terminal WebSocket | Owner owns the instance, and it is `running`. |
| `AuthenticateGuest(id, token)` | guest connect endpoint | SHA-256 of the token equals the stored hash, compared in constant time. |
| `ServeGuest(id, conn)` | guest connect endpoint | None (authentication was the previous call); normalises the ID. |
| `DialPort(id, port)` | port proxy | None; the proxy has already verified a session for that instance. |

## 10. Running machines: the sandbox provider

[internal/sandbox](../internal/sandbox/sandbox.go) defines what any
infrastructure must be able to do:

```go
type Provisioner interface {
    Create(ctx context.Context, spec Spec) (Handle, error) // machine + volume, not started
    Start(ctx context.Context, h Handle) error
    Stop(ctx context.Context, h Handle) error
    Delete(ctx context.Context, h Handle) error           // machine + volume
    State(ctx context.Context, h Handle) (State, error)
}
```

It covers *lifecycle only*. Nothing here opens a terminal or reads a file:
that all goes through the tunnel, which works the same on any infrastructure.
This split is what keeps a provider small.

`Start`, `Stop` and `Delete` are required to be idempotent, which is what
makes retries after a partial failure safe.

### The Docker provider

[internal/sandbox/docker](../internal/sandbox/docker/docker.go) uses the
Docker Engine API through `github.com/moby/moby/client`.

`Create` makes sure the owner has a **network** (`coder-user-<owner id>`, an
ordinary Docker bridge network, created the first time and reused after), and
then makes two things, both named `coder-<instance id>`:

- a **named volume**, mounted at `/home/coder`;
- a **container** from the workspace image, with:

| Setting | Value | Why |
|---|---|---|
| CPU and memory limits | from the spec (the size chosen for the instance) | A machine can't starve its neighbours. |
| Hostname | the instance's name | The shell prompt reads `coder@my-project`. |
| Environment | instance ID, guest token, connect URL | How `guestd` knows who it is and where to dial. |
| Labels | `dev.coder.instance-id`, `dev.coder.owner-id` | Every container and volume can be traced to its instance. |
| `Init: true` | | A tiny init runs as PID 1 and reaps orphaned processes left by closed shells. |
| Extra host | `host.docker.internal → host-gateway` | Lets `guestd` reach a server running on the host. |
| Restart policy | `unless-stopped` | See below. |
| Network | the owner's, with the instance's name as an alias | One user's machines can reach each other by name (`http://api:8000`), and nobody else's. See below. |

If creating the container fails, the volume is removed again, so a failed
create leaves nothing behind.

`Start`, `Stop` (10-second grace period), `Delete` (force-remove the
container, then the volume, ignoring "not found") and `State` map directly
onto Docker calls. Docker's "no such container" becomes `sandbox.ErrNotFound`,
so nothing above this package sees a Docker-specific error.

**The owner's network.** On a user-defined network Docker runs a small DNS
server that resolves each container's aliases, which is all "reach each other
by name" needs. Instance names are unique per owner, so aliases never clash
within a network. Each user having their own network is also what keeps one
user's machines from reaching another's. `Start` puts a machine on its
owner's network if it isn't on it (a machine created before networks existed,
or whose network was removed), and `Delete` removes the network when Docker
agrees it is empty: removing a network that still has machines on it fails,
which is exactly the check wanted.

**Restart policy.** With `unless-stopped`, Docker restarts the container if
its main process dies or the Docker daemon restarts, but never after an
explicit stop. A machine the database calls `running` therefore comes back by
itself after a reboot, and a machine we stopped stays stopped.

Disk size is recorded but not enforced: local Docker volumes have no quota.

### The machine image

[images/workspace/Dockerfile](../images/workspace/Dockerfile) builds in two
stages:

1. A Go stage compiles `guestd` from this repository (which is why the build
   context is the repository root).
2. A stage called `os` is the machine every stack shares: Ubuntu 24.04 with
   common tools (git, curl, a compiler toolchain, vim, sudo), Python 3 and
   Node.js, a user `coder` (uid 1000) with passwordless `sudo`, code-server
   and OpenCode. Versions are pinned with build arguments.
3. One stage per **stack** adds a toolchain to that: `go` (Go), `cpp` (Clang,
   CMake, Ninja, GDB and a few libraries), `dotnet` (the .NET SDK), `data`
   (PostgreSQL and Redis, installed, not running), and `base`, which adds
   nothing.
4. The last stage starts from the stage the `STACK` build argument names and
   adds `guestd` and the editor's settings. The toolchains come before
   `guestd` so that changing the daemon doesn't reinstall them.

`make image` builds the base image; `make images` builds one image per
stack, named after the base (`coder-workspace-go:dev`). A machine's image is
chosen from its stack when it is created.

The image's command is `guestd`, so it is the machine's main process.

When a named volume is mounted over a directory that has content in the image,
Docker copies that content into the new volume the first time. That is how a
new machine's home folder starts out with `.bashrc` and the editor settings.

## 11. The tunnel

### Why machines dial out

The server needs to open terminals and reach ports inside machines. The
obvious design, the server connecting to each machine, requires every machine
to have a reachable address and an open port, and the server to be on the same
network.

Instead, each machine's `guestd` opens **one outgoing WebSocket** to the
server and keeps it open. Consequences:

- machines need no public IP and accept no incoming connections;
- it works through NAT and firewalls, wherever the machine runs;
- the server can be hosted anywhere the machines can reach;
- nothing about it is specific to Docker.

The catch is that the connection goes the "wrong" way: the machine dialled,
but it is the server that wants to make requests. The next subsection is
about turning that around.

### Three layers on one connection

```
 server                                               machine
 ──────                                               ───────
 gRPC client   ◄──────── gRPC calls ────────►   gRPC server
     │                                               │
 yamux client  ◄─── many independent streams ───►  yamux server
     │                                               │
 WebSocket (accepted)  ◄── one TCP connection ──►  WebSocket (dialled)
```

1. **WebSocket** carries bytes over an ordinary HTTP port. `websocket.NetConn`
   wraps it as a Go `net.Conn`, so the layers above neither know nor care that
   it is a WebSocket.
2. **yamux** is a *multiplexer*: it makes one connection behave like many
   independent streams, each of which looks like its own connection. Either
   end can open a stream. Here the *server* opens them, even though the
   machine opened the underlying connection. yamux also sends keepalives
   (every 15 seconds), which is how a machine that vanished without closing
   its connection gets noticed.
3. **gRPC** runs on those streams. On the machine, a yamux session satisfies
   Go's `net.Listener` interface: `grpcServer.Serve(session)` accepts each
   stream the server opens as if it were a new incoming connection. On the
   server, the gRPC client is given a custom dialer that, instead of dialling
   a network address, calls `session.Open()`.

The result: the server holds a normal-looking gRPC client for each connected
machine, and calls methods on it.

The gRPC service is defined in
[internal/guestapi/guest.proto](../internal/guestapi/guest.proto):

| Method | Kind | Purpose |
|---|---|---|
| `Terminal` | bidirectional stream | A shell on a PTY. |
| `Forward` | bidirectional stream | A TCP connection to a port on the machine. |
| `Ping` | request/response | Liveness check. |

The `.pb.go` files beside it are generated; `make proto` regenerates them.
[transport.go](../internal/guestapi/transport.go) holds what both ends must
agree on outside the protobuf definition: the header name, the environment
variable names, the editor's port, and the yamux settings.

### How a machine connects

1. `guestd` starts with three environment variables set by the control plane:
   `CODER_INSTANCE_ID`, `CODER_GUEST_TOKEN`, `CODER_CONNECT_URL`.
2. It dials `CODER_CONNECT_URL` as a WebSocket, with headers
   `Authorization: Bearer <guest token>` and `Coder-Instance-Id: <id>`.
3. The server's `GuestConnect` handler
   ([wsgateway](../internal/wsgateway/gateway.go)) calls
   `instance.AuthenticateGuest` **before** upgrading. A wrong token gets a
   plain `401` and no WebSocket.
4. After the upgrade, the handler hands the connection to `Hub.Serve`, which:
   - starts a yamux session and builds the gRPC client on it;
   - calls `Ping` (10-second limit), so a machine is only advertised once it
     actually answers;
   - registers the guest in the Hub;
   - blocks until the session closes or the request context is cancelled;
   - unregisters the guest.

### The Hub

[tunnel.Hub](../internal/tunnel/hub.go) is a map from instance ID to that
machine's live gRPC client, guarded by a mutex.

- **Replacement.** If a tunnel arrives for an instance that already has one,
  the new one wins and the old session is closed. When the old `Serve` call
  returns and tries to unregister, it does so only if the map still points at
  *its own* guest, so it can't remove its replacement.
- **Waiting for a machine.** `OpenTerminal` waits up to 30 seconds for the
  guest if it isn't connected: a machine that was started a moment ago needs
  time to boot and dial in. The Hub keeps a channel, `changed`, that it closes
  and replaces each time a guest connects. A waiter reads the map, and if the
  guest isn't there, waits on that channel or its deadline. Closing a channel
  wakes every waiter at once, and there is no polling.
- **Not waiting.** `DialPort` does not wait. It serves web requests, which
  should fail fast with a clear page.

The Hub does no authorisation. It trusts that its callers (in practice
`instance.Service`) have already decided the request is allowed.

### Reconnecting

`guest.Run` is a loop: connect, serve until the connection ends, wait, try
again. The wait starts at one second and doubles up to 30. A connection that
lasted more than a minute resets it to one second. So a server restart is
followed by machines reconnecting within a second or so, while a server that
is down for a long time is retried at most every 30 seconds.

## 12. The guest daemon

[internal/guest](../internal/guest) is everything `guestd` does.
[cmd/guestd/main.go](../cmd/guestd/main.go) just reads the environment, wires
`SIGTERM` to a context, and starts five things: the editor supervisor, the
coding agent supervisor, the link daemon's supervisor (section 25), the loop
that keeps a copy of the team's record on disk and the machine's services
running, and the tunnel loop.

`guestd` runs as the unprivileged user `coder`, and so do the shells it
starts. Users get root through `sudo`.

### Terminals

`Server.Terminal` ([terminal.go](../internal/guest/terminal.go)) handles one
shell per call.

1. Read the first message, which must be `start` and carries the terminal
   size.
2. Start `/bin/bash -l` on a new PTY, in the home directory. `pty.StartWithSize`
   makes the shell the leader of a new session with the PTY as its controlling
   terminal, which is what makes Ctrl+C and job control work.
3. Run three goroutines:
   - **output**: read the PTY in 32 KB chunks and send each as a `data`
     message;
   - **input**: receive messages; write `data` to the PTY, apply `resize` with
     an `ioctl`;
   - **wait**: wait for the shell process to exit.
4. Then wait for whichever comes first:
   - **the shell exits**: let output drain for up to 200 ms, close the PTY,
     send one `exit_code` message, end the stream;
   - **the client goes away**: send `SIGHUP` to the shell's process group, as
     closing a real terminal window would, close the PTY, and `SIGKILL` after
     three seconds if it is still alive.

Details that exist for a reason:

- **The PTY is put back into non-blocking mode.** The PTY library leaves it in
  blocking mode, in which closing the file cannot interrupt a read already in
  progress. If a user exits the shell while a background job still holds the
  terminal open, the read never ends and the terminal hangs. In non-blocking
  mode Go's runtime handles the waiting, and `Close` interrupts it. The
  descriptor is only ever touched through `SyscallConn().Control`, because
  calling `File.Fd()` would switch it back to blocking.
- **Only the output goroutine sends on the stream** until it has finished, and
  only then is the exit code sent. gRPC allows one sender at a time on a
  stream.
- **The drain timeout** exists because end-of-file on the PTY may never come
  (same background-job case), so "wait for all output" cannot be unbounded.
- **The exit code** follows shell convention: a process killed by signal *N*
  reports `128 + N`.
- **The environment** given to shells is `guestd`'s own with the guest token
  removed and `TERM=xterm-256color` set.

### Port forwarding

`Server.Forward` ([forward.go](../internal/guest/forward.go)) relays one TCP
connection.

1. The first message is `open` with a port number.
2. Dial `localhost:<port>` (five-second limit). Dialling `localhost` and not
   `127.0.0.1` also reaches programs that listen only on IPv6 loopback.
3. If that fails, the call fails with gRPC status `Unavailable`. If it
   succeeds, send `opened`. Only then does the server treat the connection as
   established, so "nothing is listening" is reported up front and not as a
   connection that mysteriously dies.
4. Relay in both directions. When the client finishes sending (half-close),
   close only the *write* side of the TCP connection, so the reply can still
   come back. When the machine's side closes, end the stream.

### Files

`Server.PutFile` and `Server.GetFile` ([files.go](../internal/guest/files.go))
carry one file each way, whole, in one message: the user's files to a
machine before its agent's task, and an agent's files (a mockup, a
screenshot) to the chat. A path is absolute, or relative to the home folder
(`~/` too). `PutFile` creates the folders it needs and writes beside the
file then renames, so nothing ever reads half of one; `guestd` runs as
`coder`, so the file is the user's. `GetFile` refuses a folder, a missing
file (`NotFound`) and one over the size asked for (`FailedPrecondition`).
Both ends raise gRPC's message limit to a little over the 10 MB a file may
be (`guestapi.MaxMessageSize`). The hub's `PutFile` and `GetFile`
([files.go](../internal/tunnel/files.go)) wait for the guest as opening a
terminal does; a guest without them (an older image) answers
`Unimplemented`, which the instance service reports as
`ErrFilesUnsupported`.

### The editor

`RunIDE` ([ide.go](../internal/guest/ide.go)) keeps code-server running,
using `supervise` ([supervise.go](../internal/guest/supervise.go)), a loop
shared with the coding agent:

```
code-server --bind-addr 127.0.0.1:13337 --auth none
            --disable-telemetry --disable-update-check --disable-workspace-trust
            /home/coder
```

It restarts it with the same backoff as the tunnel loop if it exits, and on
shutdown sends `SIGTERM` and waits up to five seconds.

`--auth none` is safe because the editor listens on loopback only. The one way
to reach it from outside the machine is the tunnel, and the control plane
decides who may use that.

### The coding agent

`RunAgent` ([agent.go](../internal/guest/agent.go)) keeps OpenCode running as
a server, the same way:

```
opencode serve --hostname 127.0.0.1 --port 4096
```

Its configuration is built in Go and passed in the environment variable
`OPENCODE_CONFIG_CONTENT`, so it applies to this server process only and
nothing is written into the user's home folder. If the user runs OpenCode
themselves from a terminal, with their own account, their settings are
theirs. The configuration says:

- **One model provider, the control plane.** Its address is derived from the
  tunnel's (`ws://host:8080/guest/connect` → `http://host:8080/llm/v1`), and
  the "API key" it presents is `<instance id>.<guest token>`: the machine's
  own identity. The real key never reaches a machine (section 16).
- **No other provider is enabled.** Without that line OpenCode quietly falls
  back to other providers it knows about, including a hosted one.
- **The model's own limits**: the context window and longest reply from
  `CODER_MODEL_CONTEXT` and `CODER_MODEL_OUTPUT`, which the server sets on
  every new machine from `LLM_CONTEXT_WINDOW` and `LLM_MAX_OUTPUT`. A machine
  made before 2026-10-19, or with nonsense in them, gets the old cautious
  64,000 and 8,192, with which a long task is summarised part-way and the
  agent forgets what it did.
- **Every permission is granted, and asking questions is denied.** Nobody is
  watching the agent to approve a command, and a question would wait for
  ever. Questions for the user go through the assistant instead.

- **One MCP server, also the control plane** (`http://host:8080/mcp`), with
  the same identity in an `Authorization` header and signing in with a browser
  switched off. It gives the agent tools for asking about the owner's other
  machines and agents (section 16).

Like the editor it has no authentication of its own, and for the same reason
needs none: loopback only, reached through the tunnel.

### The team's record on disk

`RunTeamSync` ([team.go](../internal/guest/team.go)) keeps a folder,
`~/.coder/team/`, holding what the owner's machines and agents are doing and
have done. Every three seconds it asks the control plane for the record
(`GET /guest/team`, authenticating as the machine) and writes what it gets:

- The control plane decides what the files are and what they say; `guestd`
  only writes them. The record can change shape without rebuilding the
  machine image.
- The request carries the `ETag` of what was last written. If nothing has
  changed the answer is `304` with no body, so asking this often costs
  little.
- A file is written beside itself and renamed into place, so an agent
  reading it never sees half of it. Files with the same content are left
  alone; files no longer in the record are removed.
- A path that would land outside the folder is refused, although the control
  plane is trusted: this runs as the user, in their home folder.

### Services

The same answer from the control plane lists the machine's **services**:
programs to keep running (a name, a command, a folder, a port, and
environment variables). `serviceManager`
([services.go](../internal/guest/services.go)) makes what is running match
that list every time it arrives:

- A service not yet running is started: `bash -lc <command>` in its folder,
  as a login shell so the user's `PATH` applies, in a process group of its
  own, with its output appended to `~/.coder/logs/<name>.log`.
- One that exits is started again, with the same backoff as everything else
  `guestd` supervises.
- One whose definition changed is stopped and started as it is now. One no
  longer listed is stopped. Stopping signals the whole process group, then
  kills what is left, so a shell's children go with it.
- A definition carries a **revision**, which goes up every time the service
  is registered, changed or not. A new revision is a changed definition, so
  registering a service again restarts it. That is how an agent gets code it
  has edited to run.

In asking for its record, the machine also says what it has started: the
`X-Coder-Services` header, a JSON object of service names and the revision
each was started for (a service is in it once its process has been
started). After starting or stopping anything it asks again within half a
second instead of three, so the control plane hears promptly. This is what
lets the control plane tell "the program I just asked for is up" from "the
one it replaces is still answering".

Because the list is fetched whenever the machine starts, what was running
before a stop is running again after it. That is the difference from a
program an agent starts with `nohup`, which is gone the first time the
machine stops.

## 13. The terminal, end to end

```
 browser      wsgateway        instance        tunnel          guestd         bash
 (xterm.js)   .Terminal        .Service        .Hub            .Terminal
    │              │               │              │               │             │
    │─ WebSocket ─►│               │              │               │             │
    │─ auth msg ──►│─ ParseJWT     │              │               │             │
    │              │─ OpenTerminal►│─ owner? ─────│               │             │
    │              │               │  running?    │               │             │
    │              │               │─ OpenTerminal►─ wait for ────│             │
    │              │               │              │  guest, then  │             │
    │              │               │              │─ gRPC stream ►│─ start PTY ►│
    │◄── ready ────│◄──────────────┴──────────────┘               │             │
    │                                                             │             │
    │── keystrokes (binary) ──► pumpInput ──► Terminal.Write ────►│── write ───►│
    │◄─ output (binary) ◄────── pumpOutput ◄─ Terminal.Recv ◄─────│◄─ read ─────│
```

The browser-facing protocol is in [API.md](API.md#terminal). On the server,
`Gateway.Terminal` does this:

1. **Accept** the WebSocket. The library rejects the upgrade unless the
   request's `Origin` is the server itself or one of the allowed origins.
2. **Read the first message** within 10 seconds. It must be
   `{"type":"auth","token":…,"cols":…,"rows":…}`. The token travels here and
   not in a header because browsers cannot set headers on a WebSocket; it is
   not put in the URL because URLs end up in logs.
3. **Authenticate** with `auth.ParseJWT`.
4. **Open the terminal** with `instance.OpenTerminal`, which checks ownership
   and status and then asks the Hub.
5. Send `{"type":"ready"}` and start three goroutines' worth of work:
   - `pumpInput` (in the handler's own goroutine): binary messages go to the
     shell; a text message `{"type":"resize",…}` resizes it;
   - `pumpOutput`: shell output goes to the browser as binary messages. When
     the shell exits, it sends `{"type":"exit","code":N}` and closes with code
     1000;
   - `keepAlive`: a WebSocket ping every 30 seconds, which keeps idle
     connections alive through proxies and detects a browser that vanished.
6. When input ends (the browser left) or the terminal is lost, close the
   terminal and wait for `pumpOutput` to finish.

Every failure before `ready` closes the WebSocket with a code that mirrors an
HTTP status: 4401 (bad token), 4404 (not found or not yours), 4409 (not
running), 4503 (machine unreachable).

### Backpressure

Nothing in this chain buffers without limit. If the browser is slow:

```
 WebSocket write blocks → pumpOutput stops calling Recv → gRPC flow control fills
 → guestd's Send blocks → guestd stops reading the PTY → the kernel's PTY buffer
 fills → the program writing output blocks
```

A program that prints endlessly is slowed to the speed of the slowest link,
and memory use stays flat. This was tested with 1.49 MB of output, which
arrived byte-for-byte intact.

## 14. Ports and the editor

The editor is a web application running inside the machine. Serving it to the
browser needs two things: a way to carry HTTP to a port inside a machine, and
a way to decide who may use it.

### Carrying HTTP to a machine

`Hub.DialPort` ([tunnel/forward.go](../internal/tunnel/forward.go)) opens a
`Forward` stream and wraps it as `portConn`, which implements Go's `net.Conn`:
`Read` receives stream messages, `Write` sends them in chunks of at most
32 KB, `CloseWrite` half-closes.

Because it *is* a `net.Conn`, the standard library can use it as if it were a
TCP connection. [portproxy](../internal/portproxy/proxy.go) builds an ordinary
`httputil.ReverseProxy` whose `http.Transport` has a custom `DialContext`:

```go
DialContext: func(ctx context.Context, _, addr string) (net.Conn, error) {
    t, err := parseDialAddr(addr)            // "13337.<instance id>.guest:80"
    ...
    return dialer.DialPort(ctx, t.instanceID, t.port)
}
```

The proxy rewrites each request's target to a made-up hostname of the form
`<port>.<instance id>.guest`. That name is never resolved; it exists so the
dial function can recover which machine and port to connect to, and so the
transport pools connections separately per machine and port. HTTP keep-alive,
WebSocket upgrades and streaming all work unchanged, because the proxy and
transport are the standard ones.

One subtlety: the stream must outlive the context it was dialled with. The
transport dials using one request's context and then keeps the connection for
later requests. So `DialPort` gives the stream its own context and uses the
caller's only to abandon a dial still in progress.

The `Host` header is passed through as the browser sent it, so the program
inside the machine generates URLs, and checks origins, against the public
hostname.

### One origin per port

Each port of each machine is served at its own hostname:

```
<port>-<instance id>.<WORKSPACE_HOST>          e.g. 13337-5357ebb3-….coder.localhost:8080
```

A different hostname is a different *origin*, and browsers isolate origins
from each other. Code running on a user's machine therefore cannot read the
frontend's storage (where the access token is kept) or another machine's
pages.

`Proxy.Wrap` is the first handler every request meets. It looks at the `Host`
header: if it ends with `.<WORKSPACE_HOST>`, the request is for a machine;
otherwise it is passed on to the API router. A hostname under the workspace
host that doesn't parse as `<port>-<uuid>` gets a `404` and is *not* passed to
the API.

### A path that leads to another machine

A page served from one machine often needs an API on another. The browser
can't be sent to the other machine's address: it has no session there, and a
page calling another origin runs into every cross-origin rule there is.

So the proxy can be told that a path on one machine's port belongs to
another machine. Before forwarding, it asks a `Router`
(`Route(instanceID, port, path)`); if the answer names another machine and
port, the request goes there instead. The browser is signed in to the first
machine only and asked the first machine only; it never learns the other's
address. The request reaches the other program with its path unchanged, and
with the hostname the browser used.

The routes come from designs (section 16): a connection from a frontend to a
backend with `path: "/api"` is a route. `assistant.Service` implements
`Router`, working the routes of a machine out from its owner's approved
designs and its owner's machines, so a route can only ever join two machines
of the same user. The answer is remembered for five seconds per machine,
because the proxy asks on every request.

### Deciding who gets in

The API authenticates with a header, which a page loading in a frame can't
send. And the frontend's token must not be given to the machine's origin. So
access is handed over in two steps:

```
 app (has access token)            API                       machine origin
        │                           │                              │
        │── POST …/ports/13337/url ►│ check ownership              │
        │◄── { url with ticket } ───│ sign ticket                  │
        │                                                          │
        │── load url in a frame ──────────────────────────────────►│ verify ticket,
        │                                                          │ mark it used,
        │◄── 302 + Set-Cookie: session ────────────────────────────│ sign session
        │── follow redirect, cookie attached ─────────────────────►│ verify session,
        │◄── the editor ───────────────────────────────────────────│ proxy to machine
```

| | Ticket | Session |
|---|---|---|
| Travels in | a URL | a cookie |
| Lifetime | 1 minute | 12 hours |
| Uses | one | many |
| Names | user, instance, port | user, instance, port |

Both are JWTs created in [grants.go](../internal/portproxy/grants.go).

- **Bound to one target.** A ticket or session for one instance and port is
  rejected on any other. The check compares the claims with the hostname the
  request arrived on.
- **Single use.** A ticket's ID is remembered until it expires; a second use
  is refused. A link that leaks through browser history or a log is useless.
- **Two kinds, not interchangeable.** The `aud` claim says which kind a token
  is, and verification requires the right one.
- **A separate signing key.** The key is `HMAC-SHA256(JWT_SECRET, "coder
  workspace grants v1")`, not `JWT_SECRET` itself. Access tokens are signed
  with the raw secret. With different keys, a ticket can never be accepted as
  an access token, nor an access token dressed up as a session, whatever
  their claims say.

Issuing a ticket is `Proxy.URL`, called from the `PortURL` API handler *after*
`instance.Get` has confirmed the caller owns the instance.

### The session cookie

| Attribute | Value | Why |
|---|---|---|
| `HttpOnly` | yes | Scripts on the machine's pages can't read it. |
| Domain | none (host-only) | Sent only to the exact hostname that set it. |
| `Path` | `/` | |
| `Max-Age` | 12 hours | |
| `Secure`, `SameSite=None`, `Partitioned` | when the origin is HTTPS or under `localhost` | Keeps the frame working if it is embedded from a different site. |
| `SameSite=Lax` | otherwise | Plain HTTP on a real hostname can't have a `Secure` cookie. |

Before forwarding a request, the proxy removes this cookie from the `Cookie`
header. The program inside the machine never sees it. Other cookies, including
ones the program sets itself, pass through untouched.

### Other checks

- **Origin check.** A request carrying an `Origin` header is allowed only if
  that origin is a port of the *same* machine. Ordinary page loads carry no
  `Origin`; scripted requests and WebSockets do. This stops another website
  from using the visitor's session to talk to their machine. Different ports
  of one machine may call each other, which a frontend on one port and an API
  on another need.
- **Redirect target.** After sign-in the proxy redirects to the `next`
  parameter only if it is a path on the same origin; anything else becomes
  `/`.
- **The sign-in response** is sent with `Cache-Control: no-store` and
  `Referrer-Policy: no-referrer`, since its URL contains a credential.

### Error pages

When a request can't be forwarded, the proxy answers with a small page (or
plain text for non-page requests):

| Cause | Status | Retries by itself |
|---|---|---|
| Machine's guest not connected | 503 | yes |
| Nothing listening on the port | 502 | yes |
| `guestd` too old to forward ports | 502 | no |
| No valid session | 401 | no |
| Request from another site | 403 | no |

The retrying pages reload every two seconds. That is what lets the frontend
load the editor immediately after starting a machine: the frame shows "Nothing
on port 13337 yet" for a moment and then becomes the editor.

### Same site

*Origin* and *site* are different. Two hostnames that share a registrable
domain (`app.example.com` and `13337-….example.com`) are different origins on
the same site. Browsers apply the isolation described above per origin, but
restrict cookies in embedded frames per *site*.

The frontend and the machines should therefore share a site. In development
the frontend is opened at `coder.localhost:3000` and machines are under
`coder.localhost:8080`. If they are on different sites the editor still loads,
but its service worker cannot register, which breaks VS Code's webviews.

## 15. Stopping idle machines

A machine that nobody is using should not keep running. The rule:

> A running machine is stopped once nobody has been connected to it for
> `IDLE_STOP_AFTER` (2 minutes by default).

"Nobody" has a precise meaning. A machine has someone as long as either of
these holds:

| Who | How the server knows |
|---|---|
| Its owner has the app open | Every open tab holds a WebSocket to `GET /presence`. |
| A browser is using the machine itself | A terminal WebSocket is open, or a request or WebSocket to one of its ports (the editor holds one) is in progress. |

So closing the last tab, signing out, the browser crashing and the laptop
going to sleep all end the same way: the connections go, and after the grace
period the machine stops. Having only the editor open in its own tab keeps
that one machine running, because the editor's WebSocket passes through the
port proxy.

A program left running on the machine does **not** count. That is deliberate:
the point is that walking away is enough to stop a machine.

### Why a connection and not a "closing" message

A page can't be relied on to say goodbye. Browsers give no dependable hook
for "this tab is closing", and none at all for a crash, a lost network or a
closed lid. A reload looks the same as a close. So the server doesn't wait to
be told; it watches for the absence of connections, and the grace period
absorbs reloads and brief drops.

A held connection is used in preference to periodic "still here" requests
because the server learns the instant it closes, and because browsers slow
timers in background tabs but leave an open connection alone. Pings every 30
seconds catch a peer that vanished without closing.

### The pieces

- [internal/presence](../internal/presence/tracker.go) is an in-memory
  `Tracker` with two maps, users and machines, each holding a count of open
  connections and the time the count last reached zero. `UserConnected` and
  `MachineConnected` increment a count and return a function that decrements
  it; `IdleFor(user, machine)` returns zero if either count is above zero, and
  otherwise the time since the later of the two last closed.
- `instance.Service` owns the tracker and exposes those two calls.
  - `Gateway.Presence` calls `UserConnected` for the life of the WebSocket.
  - `Gateway.Terminal` calls `MachineConnected` for the life of the terminal.
  - `portproxy` calls `MachineConnected` around every request it forwards,
    after checking the session. For an ordinary request that is a moment;
    for a WebSocket it is until the tab closes.
- `instance.Service.RunIdleStop` is a loop started by `main`. Every few
  seconds it lists the running instances and calls the ordinary `Stop` on
  each that has been idle long enough. Because `Stop` claims the instance
  like any other operation, it can't collide with something the user does at
  the same moment; one of the two finds the instance busy.

### Starting up

The tracker starts empty, so a freshly started server has no record of
anyone. Rather than treat every machine as idle since for ever, it measures
from its own start time: after a restart every running machine gets one full
grace period for its users' tabs to reconnect, which they do within seconds.

## 16. The assistant

The assistant is a chat in which the user says what they want built. It
creates machines, has code written on them, and asks the user when a choice
is theirs. "Make a backend on one machine and a frontend on another" is one
message.

### Two levels

There are two AI agents at work, and it matters to keep them apart.

```
 ┌─────────┐  chat   ┌────────────── control plane ──────────────┐  a task, through   ┌──────────────────┐
 │ Browser │ ──────► │  assistant: asks the model what to do,    │  the tunnel        │ machine "api"    │
 └─────────┘         │  carries out the tools it asks for        │ ─────────────────► │  OpenCode        │
                     │                                           │ ─────────────────► ├──────────────────┤
                     │  model gateway: adds the key, forwards    │ ◄───────────────── │ machine "web"    │
                     └──────────┬───────────────────┬────────────┘  the agents' own   │  OpenCode        │
                                │                   │               model calls       └──────────────────┘
                                ▼                   ▼
                          ┌──────────────────────────────┐
                          │     the model (DeepSeek)     │
                          └──────────────────────────────┘
```

- **The assistant** runs in the control plane
  ([internal/assistant](../internal/assistant)). It talks to the user and
  decides what should happen. It has no shell and edits no file. Everything
  it can do is one of eleven tools.
- **Coding agents** run inside machines. The program is [OpenCode](https://opencode.ai),
  an open-source agent, started by `guestd` (section 12) and serving any
  number of conversations at once. Each conversation is one agent, with a
  name: a machine can have a `backend` agent and a `tests` agent working at
  the same moment. An agent has the tools that matter there (read, write,
  edit, run a command), knows nothing about the chat, gets a task in words
  and returns a report in words.

Why not one agent? The work happens inside machines, where the control plane
has no hands, and where a mature coding agent already exists. Splitting it
this way also means two machines are worked on at the same time by two
agents, each keeping only its own machine's details in mind, and the
assistant's own conversation stays short: it sees reports, not file contents.

Both use the same model. The assistant calls it directly. The agents call it
through the model gateway, described below.

### A chat is a list of events

A chat's history is rows in `chat_events`, only ever appended to. Nothing
about a conversation lives in memory except who is watching it and how to
stop it.

| Kind | Written when | Payload |
|---|---|---|
| `user` | the user sends a message | `text`; `to`, the handles it was for when the user named agents; `attachments` |
| `post` | an agent writes in the chat | who (`agent`, `role`, `machine`), `text`, `attachments`, `reply_to`, `question` and `options`, `preview`, `queued`, `failed` |
| `assistant` | the model replies | `text`, and `tool_calls`: what it wants done; `reasoning`, what the model thought before replying |
| `tool` | a tool finishes | `tool_call_id`, `name`, `content` (what the model is told), `failed`, and the `machine` it concerns |
| `activity` | a coding agent does something | `tool_call_id`, `machine`, `text` such as "Writing api/main.py" |
| `error` | a run fails or is stopped | `message`, `stopped` |

The first three are the conversation the model sees. `history()` rebuilds it
from the events before every call to the model; `activity` and `error` are
for the person watching and are left out. `agent` and `post` events, and
`user` events written to agents and not to the assistant, reach the model as
messages marked "[From the platform": it reads them, and is told it need not
act on them.

A thinking model (DeepSeek's are, by default) streams its reasoning apart
from its reply, as `reasoning_content`. It is kept in the `assistant` event
and sent back with that message on every later call: DeepSeek requires it
whenever tools are in use, and without it the model loses the thread between
one tool call and the next. It is not drawn in the chat. Events from before
2026-10-19 have none, and are sent as they are.

The model's API refuses a conversation in which the assistant asked for a
tool and no result follows. A run that was cut short leaves exactly that
behind. So `repair()` runs over the rebuilt conversation: every request gets
its results placed directly after it, with a stand-in ("This was interrupted
before it finished") for any that never arrived. The stored history is never
edited to make this true.

### A run

`Send` is what happens when the user sends a message
([service.go](../internal/assistant/service.go)):

1. **Claim the chat**: `idle` or `waiting` → `running`, with the same
   compare-and-set as instances (section 9). A second message while the
   assistant is working gets `ErrBusy` (`409`), never a second run.
2. **Record the message.** If the assistant had asked a question, the message
   is recorded as that question's answer (a `tool` event); otherwise as a
   `user` event.
3. **Start the run in a goroutine and return.** The HTTP request ends with
   `202 Accepted` at once. The run's context descends from nothing but a
   90-minute timeout: closing the browser tab must not stop it.

The run is a loop, at most 40 times round:

```
 ┌──► rebuild the conversation from the events
 │    ask the model for the next message          ── fails ──► error event, idle
 │    record it as an assistant event
 │    no tool calls? ───────────────────────────────────────► idle (it answered)
 │    carry out the tools, all at once; record each result
 │    one of them was ask_user? ────────────────────────────► waiting
 └────┘
```

- **Tools requested together run at the same time**, each in its own
  goroutine. That is what makes "backend on one machine, frontend on another"
  take as long as the slower of the two and not the sum.
- **A tool that fails is not an error.** Its result says `Error: …`, the
  model reads it, and decides what to do: pick another name, try again, tell
  the user. Only a failure of the loop itself (the model can't be reached,
  the database is down) ends the run with an `error` event.
- **`ask_user` is a tool with no implementation.** The loop records the
  request and stops, leaving the chat `waiting`. The answer arrives later as
  an ordinary message (step 2 above) and becomes the tool's result, so to the
  model it looks like a tool that took a while.
- **Stopping.** Each running chat has a cancel function in memory. `Cancel`
  calls it; every tool in progress sees its context end; their results are
  still recorded (so the history stays well-formed); an `error` event marked
  `stopped` is added, and the chat becomes `idle`. If the chat was only
  waiting on a question, `Cancel` just sets it back to `idle`.
- **However a run ends, the chat's status is set** in a deferred function on
  a context that cannot be cancelled. A chat is never left `running` by a
  run that has ended. The one case that can't be covered that way is the
  process dying, and startup handles that (section 5).

### The tools

Defined in [tools.go](../internal/assistant/tools.go), carried out in
[execute.go](../internal/assistant/execute.go):

| Tool | Does |
|---|---|
| `list_machines` | Names and statuses of the user's machines. |
| `create_machine(name, size, disk_gb, stack)` | Creates a machine, starts it, and waits until its coding agent answers, noting each stage as an `activity`. A machine the approved design names is created as the design says, whatever the model passes. |
| `start_machine(name)`, `stop_machine(name)` | As the buttons do. |
| `code_on_machine(machine, agent, task, background)` | Gives a task to the named agent on that machine (created if new). Waits for its report, or with `background` returns at once (see "Agents" below). |
| `list_agents()` | This chat's agents: where each is, whether it is working, on what, and what it is doing right now. |
| `stop_agent(machine, agent)` | Stops an agent's task. |
| `show_preview(machine, port, label)` | Does nothing on the server. It is recorded, and the interface turns it into a button that opens that port (section 14). |
| `ask_user(question, options)` | Pauses the run until the user answers. Each option has a label and a sentence of explanation, the model's own pick first. |
| `propose_architecture(design)` | Puts a design to the user and pauses the run until they approve it or ask for a change (see "Designing first"). |
| `update_architecture(design)` | Corrects an approved design to match what was built. |

The model refers to machines by name, the way a person would; the code
resolves the name among that user's machines only. There is no tool to delete
a machine: that stays a decision the user makes with their own hands.

The system prompt, in the same file, tells the model how to use these: that
the coding agent knows nothing of the conversation and each task must stand
alone, that servers must listen on `0.0.0.0` and be started in the
background, that machines reach each other by name, and when to ask. Since
2026-10-19 it also tells it to start every component's agent at once, in the
background, never in stages (database, then backend, then frontend), with
each task saying the same about every connection it touches, so that each
side can be written against the other before either runs; and to ask for
finished, working software, never "keep it minimal". The text before every
task (`taskPreamble` in `fleet.go`) says the same from the agent's side:
others are building at the same time, build against what the design says
they offer, and retry, or wait up to ten minutes, for what is not up yet.

The sizes and stacks it may use are written into the prompt from
`instance.Powers()`, `instance.Disks()` and `instance.Stacks()`, so the model
can't be told about one that doesn't exist.

### Designing first

Nothing is built until the user has agreed to a design.

1. **A few questions.** The model is told to ask at most three, and only
   ones whose answer changes the design.
2. **A proposal.** It decides everything else itself, by rules written into
   the prompt, and calls `propose_architecture` with a **design**
   ([design.go](../internal/assistant/design.go)): the components (each with
   a kind, a technology, the machine it runs on, that machine's stack, size
   and disk, and its port), the connections between them, and the reasons
   for the choices. The design is checked (`normalize`): ids unique, kinds
   and sizes real, connections between components that exist, components on
   one machine agreeing about that machine. A design that fails goes back to
   the model as a tool error to fix. One that passes is stored as `proposed`
   and the run **waits**, exactly as it does for a question.
3. **The user's turn.** The frontend draws the design as a diagram. The user
   can approve it (`POST /chats/{id}/architecture/approve`), optionally with
   the design as they changed it, or just say what to change. An approval
   becomes the proposal's tool result, "approved, build exactly this" with
   the design in words, and the run resumes. Anything typed instead becomes
   "not approved, they said …", and the model proposes again.
4. **Building.** The prompt tells the model to create the design's machines,
   give each component to an agent named after it, register what must keep
   running as a service, and correct the design with `update_architecture`
   when something ends up different, so the diagram stays true.

The rules in the prompt are opinions, on purpose: one application unless
there is a reason for more, and which reasons count; structure (how the
running code is divided) and repository (how the source is kept) as two
separate decisions; PostgreSQL unless the data says otherwise; Redis and
Kafka only when something needs them; and the language by what the work is,
including saying plainly when C++ or Go fits better than what was asked for.
How well the model applies them is the part that only a real model can show.

**Connections are made real** in two ways, both from the design's edges:

- A connection with `env` gives the first component's programs the second's
  address in that environment variable. Services are started with it
  (section 12), and it is written to `~/.coder/team/connections.env` and
  explained in `CONNECTIONS.md` on that machine.
- A connection with `path` becomes a route in the port proxy (section 14):
  the browser's requests to that path on the first component's address are
  passed to the second.

`GET /chats/{id}/architecture` returns the design with how each component
stands: the machine it is on, if it exists yet, and whether anything answers
on its port, which is found out by dialling the port through the tunnel.

### Things that keep running

An agent that starts a server from its shell leaves something that dies with
the machine's next stop. So agents are told not to. They register a
**service** with the `run_service` tool: a name, a command, a folder and a
port. It is stored in `services`, the machine hears of it within seconds and
starts it (section 12), and the tool waits up to 45 seconds for the port to
answer and tells the agent whether it did, pointing at the log if not. From
then on the machine starts it whenever the machine starts.

Registering a service that is already registered starts it afresh, which is
how changed code gets to run. The wait is therefore not just for the port:
the program being replaced may still be answering on it. `RunService`
([services.go](../internal/assistant/services.go)) waits, in order, for the
machine to have asked for its record since the registration, for the machine
to say it has started that revision (`NoteGuest`, fed by the header described
in section 12), and only then for the port. A machine whose daemon is from
before it could say what it runs is given a moment after hearing of the
registration and then judged by its port; on those the revision still
reaches the program's environment (`CODER_SERVICE_REVISION`), and a changed
environment is a changed definition, so the restart happens there too.

### Backends describe their APIs

Every component of kind `backend` or `service` must serve an OpenAPI 3
document describing all of its routes at `/openapi.json` on its own port
(`APIDescriptionPath`, [apidocs.go](../internal/assistant/apidocs.go)). The
frontend's Backend tab is drawn from that document, so a backend without one
has nothing to show.

The rule is stated in three places and checked in one. The assistant's
instructions make it part of the design rules and of every backend's task;
the text put before every agent's task says the same to the agent. The
check is in `run_service`: when a service comes up on a machine and port
that an approved design gives to a backend, the control plane asks it for
`/openapi.json` (`Fleet.Get`, over the tunnel) and adds what it found to
what the agent is told: that the description is served, with how many paths
it has, or that it is missing, is not an OpenAPI document, or lists no
paths, with the rule and what to do. The agent reads that as the result of
its own tool call and puts it right, registering the service again, which
restarts it and repeats the check on the new program.

The frontend reads the document and tries endpoints through
`POST /instances/{id}/ports/{port}/request`
([request.go](../internal/instance/request.go)): one HTTP request to a port
inside the caller's machine, made over the tunnel and returned as data. The
browser cannot call the port's own origin (it is another site, and the
application there decides its own CORS), and this keeps what a user can
reach exactly what the port proxy already lets them reach: their own
machines.

### Agents asking each other

Looking another agent up tells you what it wrote down. `ask_agent` asks it.
The question is put to the other agent in its own conversation, with
everything it remembers, as a short task ("another agent asks you …; answer
from what you built; change nothing"), and its reply is the answer. The
agent that asked simply gets a tool result.

- If the other agent is **busy**, nothing interrupts it. The question is
  stored in `agent_messages` and handed to it in front of its next task; the
  asker is told so and given what is recorded about that agent meanwhile.
- Agents setting each other to work, with nobody deciding it should happen,
  is bounded: 30 questions per user in ten minutes, after which agents are
  told to look things up instead.
- Each exchange is recorded in the chat as a `message` event, so the user
  can read what the agents said to each other.

### Source control and CI

Agents are told to keep each project in a git repository and commit at the
end of every task; the image gives git a default identity so that works.
With a repository per component, each machine holds its own.

A design can include a CI component on its own machine. There is no CI
engine in the platform: the model is told how to set one up from parts that
exist. Each code machine serves its repositories with `git daemon`,
registered as a service, as part of its own build task; the CI machine's
agent starts with the others, writes a script that clones them over the
user's private network and runs each build and each test, waits for
repositories that are not there yet, and is sent back to run it once every
other agent has reported. That makes CI as reliable as the agent that set it
up, which is a limit worth knowing.

### The reply, as it is written

The model's answer is requested as a stream ([llm.go](../internal/assistant/llm.go))
and assembled: text arrives in fragments, and so do tool calls, whose
arguments come a few characters at a time with an index saying which call
each fragment belongs to. Two things are deliberate:

- **A reply that is cut off is an error.** If the connection ends before the
  model says it has finished, nothing is kept. Half a message could be shown,
  but half a tool call must never be carried out.
- **Text is passed on as it arrives.** Each fragment is appended to a
  *draft* held in memory for that chat, and watchers are told, at most every
  50 ms. The chat stream sends the draft so far; the browser shows it. When
  the reply is complete it is stored as an `assistant` event and the draft is
  cleared, so a draft is never part of the history and a page opened later
  sees only finished messages.

### The chat as a room

A chat is shared by the user, the assistant and the agents
([people.go](../internal/assistant/people.go)). It is meant to work as a
project's channel: the assistant runs the project, and the user can talk to
any agent directly.

- **Names.** An agent has its job (`name`, "frontend"), which the assistant
  chooses, and a name of its own (`handle`, "tom"), which the platform gives
  it when it is created: the first of a fixed list of short names
  ([handles.go](../internal/assistant/handles.go)) that none of the user's
  agents has, then the list again with numbers. Two agents created at once
  may be offered the same one; the unique index refuses the second, which
  tries the next. Agents from before handles were given theirs by the
  migration, in the order they were created. `assistant` is never given.
- **Who a message is for.** `SendMessage` takes `to`, a list of handles. Left
  out, it is the chat's agents the text mentions as `@handle` (a mention of
  anyone else is just a word). With no agent named, or `assistant` named,
  the message goes to the assistant as before: it claims the chat and starts
  a run. Each agent named gets it as a task of its own, in the background,
  whatever the chat's status: the user can write to an agent while the
  assistant works.
- **An agent's answer.** The task says the owner wrote to it directly, and
  that its report will be posted as its reply. Its progress is recorded as
  `activity` events under `direct-<seq>-<handle>`, `seq` being the user's
  message, and its report as a `post` event with `reply_to` set to it. Such
  a task does not wake the assistant.
- **A busy agent** gets the message when its task ends. It is stored in
  `agent_messages`, a `post` with `queued` says so, and when any task of
  that agent ends (`followUp`), everything the user wrote to it meanwhile is
  given to it as one task.
- **Agents speak up.** Two tools on the team's MCP server
  ([mcp.go](../internal/teamapi/mcp.go)): `post_to_chat`, to show the user
  something, and `ask_owner`, to ask them to decide something with options.
  Either can carry files from the machine (read with `GetFile`, section 12,
  and stored as attachments) and a page the machine serves (a port and a
  path). The agent names itself; the machine is known for certain. The
  user's answer to a question is a message to that agent, so it comes back
  to it as a new task: the tool tells the agent not to wait for it. Posts are
  bounded like questions between agents: 40 per user in ten minutes.
- **Files.** Uploaded first (`UploadAttachment`), then sent by ID with a
  message. The assistant is told each file's name, type, size and ID, and is
  shown the start of each text file (16 KB of each, 48 KB in all); it cannot
  see pictures. It passes files on with `code_on_machine`'s `files`. Files
  that go with a task, from the assistant or from the user, are put on the
  machine with `PutFile` before the task is given, at
  `/home/coder/.coder/attachments/<first 8 of the id>/<name>`, and the task
  says where. A machine too old for `PutFile` gets the task with a note that
  the files could not be put there. Names are cut to their last part, with
  no leading dots and nothing unprintable.

### Agents

Agents are to the assistant what processes are to an operating system: many
can run at once, each has an identity and a state, they can run without
anyone waiting on them, and they can be listed and stopped.

- **Identity.** An agent is a row in `agents`, found by chat, machine and
  name. The first task given to a new name creates it; later tasks to the
  same name reach the same OpenCode conversation, so it remembers what it
  did before. Names follow the machine-name rule.
- **One task at a time, per agent.** `Begin` claims the agent with a
  compare-and-set. A second task for a busy agent is refused with a message
  telling the model to use another agent or wait; nothing queues. Different
  agents never wait for each other, on one machine or on several.
- **Foreground or background.** By default the tool waits for the report,
  which is simplest for the model. With `background: true` the task is
  started in its own goroutine, on a context of its own (it outlives the run
  that started it), and the tool answers straight away: "working in the
  background; you will be told when it finishes". The chat goes `idle` once
  the model has nothing more to say, and the user can carry on talking.
- **Finishing.** When a background agent finishes, an `agent` event is
  appended to its chat and the chat is *woken*: if it is idle, a run starts
  as if the user had sent something, and the model gets the news as a
  message marked "[From the platform, not the user]" with the agent's report.
  If the chat is already running, that run checks for news when it ends. If
  it is waiting on a question, the news waits in the history until the user
  answers.
- **News the model hadn't seen.** News can arrive while the model is writing
  a message, so it is stored before that message although the model never
  saw it. Every assistant event records the last event the model had seen
  (`seen`), and `history()` moves news the model hadn't seen to after the
  message it was writing. Without that, the model would believe it had
  already dealt with it.
- **Stopping.** Each working agent has a cancel function in memory.
  `StopAgent` (from the API, by the user) and the `stop_agent` tool call it;
  the task's context ends, OpenCode is told to abort (its commands are
  killed), and the agent is marked `stopped`. Stopping a chat's run stops
  the agents it was waiting for, not its background ones; deleting a chat
  stops all of its agents.
- **Not for ever.** Background agents finishing can set the assistant going,
  which can start more agents. After 20 such runs in a row with nothing from
  the user, the assistant stops reacting and says so; the user's next message
  resets the count.
- **Restarts.** At startup any agent still marked `working` is set to
  `failed`, and a background one's chat is sent the news, so nothing shows as
  working for ever.

While any agent works for a user, that counts as the user being present
(section 15), so their other machines, which the agent may well need, stay
up too.

### What agents know about each other

An agent building a frontend needs to know what the backend agent on another
machine built: which port, which endpoints, where the files are. The
assistant could repeat all that in every task, and get it wrong. Instead the
platform keeps a **record of the team** and gives every agent two ways to
read it.

**The record** ([team.go](../internal/assistant/team.go)) is, for one user:
their machines; every agent on them, with its latest five tasks, and for
each task what it was asked, what it did step by step, and its report; and
the notes agents have left each other. It is assembled from `agents`,
`agent_tasks` and `agent_notes` whenever it is asked for. It is scoped by
owner: an agent sees its owner's whole team, across chats, and nothing of
anyone else's.

**Asking: an MCP server.** MCP (the Model Context Protocol) is the standard
way to hand an AI agent tools from outside it. OpenCode in each machine is
configured (section 12) with the control plane as an MCP server, and so gains
these tools, next to its own for files and commands:

| Tool | Gives |
|---|---|
| `list_machines` | The owner's machines, and the address each is reached at from the others. |
| `list_agents` | Every agent: where it is, whether it is working, what it is doing right now, its latest task. |
| `get_agent(machine, agent)` | Everything recorded about one agent: each task, each step, each report. |
| `read_notes(topic)` | The notes agents have left. |
| `share_note(agent, topic, text)` | Leaves a note for the others: an API's endpoints, a port, a decision. |
| `get_architecture` | The design the owner agreed to, and what this machine is connected to. |
| `ask_agent(agent_name, machine, agent, question)` | Asks another agent and returns its answer. |
| `run_service(name, command, directory, port)`, `stop_service(name)` | Has the platform keep a program running on this machine, or stop. Registering a service again restarts it; a backend is also checked for its API description. |

The server is [internal/teamapi/mcp.go](../internal/teamapi/mcp.go), written
directly against the protocol, which is small: JSON-RPC 2.0, where the
client `POST`s a message and the server answers with JSON. It handles
`initialize` (agreeing to the client's revision of the protocol if it knows
it), `tools/list`, `tools/call` and `ping`; a notification, which expects no
answer, gets `202`. There are no sessions and nothing is pushed to the
client. A mistake by the agent (asking for an agent that doesn't exist) comes
back as a tool result marked as an error, with the names that do exist, so
the agent can correct itself; only a malformed message is a protocol error.

The caller is authenticated exactly as for the model gateway: the machine's
own ID and guest token. That says which machine is asking, and so whose team
to show. It can't say which *agent* on the machine is asking, since all of a
machine's agents share one connection, so `share_note` takes the agent's name
as an argument: a note's author is what the agent said it was.

**Reading: files on the machine.** The same record is kept on every machine's
disk, in `~/.coder/team/`, by `guestd` (section 12):

```
TEAM.md                     the team at a glance: machines, services, agents, latest notes
ARCHITECTURE.md             the design the owner agreed to
CONNECTIONS.md              what this machine is connected to, and the addresses to use
connections.env             the same addresses, as a file a shell can load
agents/api--backend.md      everything about one agent
agents/web--frontend.md
NOTES.md                    the notes agents have left
team.json                   all of it, as data
```

`Team.Files` renders them. An agent can read these with nothing but `cat`,
and they are still there when the control plane can't be reached. They trail
the live record by a few seconds.

Every task tells the agent both exist, and to look another agent's work up
before guessing how it works. The assistant's own instructions say the same
from the other side: it can name the agent to look up and need not repeat
what that agent built.

### Giving a machine a task

`assistant.Service` knows machines only through the `Machines` interface.
`Fleet` ([fleet.go](../internal/assistant/fleet.go)) implements it on top of
`instance.Service` and OpenCode. `RunTask(machine, agent, task)` does this:

1. **Find the machine** by name among the owner's.
2. **Mark it as in use** (`MachineConnected`, section 15) for as long as the
   task takes.
3. **Count it as a task on the machine**, which is what the model gateway
   checks. Several can be counted at once.
4. **Start the machine** if it is stopped. If another agent is starting it at
   that moment, wait for it instead.
5. **Wait until the agent answers** `GET /global/health`. This covers the
   machine booting, its tunnel connecting and OpenCode starting. A machine
   that is online but has nothing on the agent's port for 45 seconds was
   built from an image without one, and the model is told so.
6. **Find or start the agent's session.** OpenCode keeps conversations of
   its own, called sessions, on the machine's disk. Each agent has one
   (`agents.session_id`), so a later task for the same agent lands in the
   same session and it remembers what it built before. If OpenCode no longer
   has the session, a new one is started.
7. **Listen to the agent's events** (`GET /event`, a server-sent-event
   stream), turning each tool it uses into a line such as "Running npm
   install". Each is stored as an `activity` event.
8. **Send the task** (`POST /session/{id}/message`) and wait, for up to 30
   minutes, for the agent to finish. The response carries what it said last:
   its report, which becomes the tool's result.
9. **If the task was stopped or ran out of time,** tell the agent to abort.
   Otherwise it would carry on working with nobody listening.

Several tasks on one machine are simply several sessions on one OpenCode
server, each with its own request and its own event filter.

All of this is plain HTTP. The requests reach OpenCode through the tunnel:
the `worker` ([worker.go](../internal/assistant/worker.go)) is an
`http.Client` whose transport, instead of opening a TCP connection, calls
`instance.DialPort(id, 4096)`. It is the same trick the port proxy uses for
the editor (section 14), pointed at a different port.

Every task is prefixed with a few lines the model can't be relied on to
repeat each time: which agent and machine this is, that other agents may be
working on the same machine and are to be left alone, that nobody is watching, that
servers go in the background and must be checked with `curl`, and to finish
with a report.

One detail of OpenCode shaped step 5. A request that reaches it in the
instant it starts listening is accepted and then never answered. A health
check with no time limit of its own could therefore wait for ever on an agent
that was, by then, ready. Each check gets three seconds, and is simply
repeated.

### The model gateway

OpenCode needs a model to think with. Giving it the DeepSeek key would put
the platform's key on a machine where the user is root. Instead, OpenCode is
configured (section 12) to believe the control plane *is* its model provider:

```
 OpenCode ──► POST /llm/v1/chat/completions            control plane
              Authorization: Bearer <instance id>.<guest token>
                                │
                                ▼   llmgateway
              1. is that a real machine?     instance.AuthenticateGuest
              2. is the assistant working on it right now?     Fleet.TaskActive
              3. replace "model" and "reasoning_effort" with the configured ones
              4. forward with the real key ──────────────────► DeepSeek
              5. stream the answer back as it arrives
```

[internal/llmgateway](../internal/llmgateway/gateway.go) is that handler.

- The credential is one the machine already has: its guest token (section
  11). No new secret is created or distributed.
- **Step 2 is what keeps the token from being a free API key.** A user can
  read their machine's guest token. Without the check they could call the
  model through the gateway whenever they liked, at the platform's expense.
  With it, the gateway answers a machine only during a task the assistant
  gave it.
- The machine doesn't choose the model: whatever name the request carries is
  overwritten. Nor how hard it thinks: `reasoning_effort` is set to
  `AGENT_REASONING_EFFORT` (`max` unless configured).
- The response is copied through and flushed chunk by chunk, because it is a
  stream of small events and the agent acts on each as it arrives. Errors
  from the model (a rate limit, an empty balance) are passed on unchanged, so
  the agent reports them and they end up in the chat.

### Following a chat

A run happens in the background, so the browser needs a way to watch.
`GET /chats/{id}/stream` is a server-sent-event stream
([chats.go](../internal/httpapi/chats.go)):

- `Service.Watch(id)` returns a channel that gets a value whenever the chat
  changes. It carries no data: it only says "look again".
- The handler loops: read the chat, the events after the last one it sent,
  and the draft of the reply being written; write whatever is new; wait for
  the channel (or 20 seconds, to send a keep-alive).
- It subscribes *before* its first read. A change that happens between the
  two wakes the loop for another look; subscribing second would lose it.

Because events come from the database and not from memory, a client that
lost its connection reconnects with `?after=<last seq it saw>` and misses
nothing, and a page opened in the middle of a run shows everything so far.

### The assistant and idle machines

Section 15 stops machines when nobody is connected. A run counts as the user
being present (`KeepAwake` calls `UserConnected`), so someone who asks for
something, closes the laptop and comes back later finds it done and not
abandoned half-way. A task also counts as a connection to its machine. Once
the run ends, the ordinary rule applies again: with the app closed, the
machines stop after the grace period, and servers that were running on them
stop too. Their files stay.

### What has not been verified

The assistant is run end to end against real machines and the real
OpenCode, with a scripted stand-in for the model that speaks DeepSeek's API
(`tools/standin`; section 22). That covers the mechanics, the API-description
rule included (a backend registered without one, its agent told, the
description added, the service registered again and the new program
checked).

With the real model (DeepSeek) there has been one chat, on 2026-10-04. The
first request was refused for a tool schema the stand-in had never checked
(`"required": null`; fixed, with a test). The chat then asked its questions,
proposed a design, was approved and created its machines before it was
stopped. How well a real model follows the prompts (whether it writes
self-contained tasks, generates an accurate API description, finishes with
CI) is still mostly unseen. [STATUS.md](STATUS.md) keeps the running
account.

## 17. The HTTP layer

### Routing

Requests pass through, in order:

1. `portproxy.Wrap`: machine hostnames are handled here and go no further.
2. The [chi](https://github.com/go-chi/chi) router, with CORS middleware.
3. For protected routes, `middleware.RequireAuth`.

| Method and path | Auth | Handler | Calls |
|---|---|---|---|
| `GET /healthz` | – | inline | – |
| `POST /register` | – | `AuthHandler.Register` | `auth.Register` |
| `POST /login` | – | `AuthHandler.Login` | `auth.Login` |
| `GET /me` | token | `AuthHandler.Me` | – |
| `POST /token/renew` | token | `AuthHandler.Renew` | `auth.Renew` |
| `GET /instances` | token | `InstanceHandler.List` | `instance.List` |
| `POST /instances` | token | `InstanceHandler.Create` | `instance.Create` |
| `GET /instances/{id}` | token | `InstanceHandler.Get` | `instance.Get` |
| `POST /instances/{id}/start` | token | `InstanceHandler.Start` | `instance.Start` |
| `POST /instances/{id}/stop` | token | `InstanceHandler.Stop` | `instance.Stop` |
| `DELETE /instances/{id}` | token | `InstanceHandler.Delete` | `instance.Delete` |
| `POST /instances/{id}/ports/{port}/url` | token | `InstanceHandler.PortURL` | `instance.Get`, `portproxy.URL` |
| `POST /instances/{id}/ports/{port}/request` | token | `InstanceHandler.Request` | `instance.Request` |
| `GET /instances/{id}/terminal` | first message | `Gateway.Terminal` | `auth.ParseJWT`, `instance.OpenTerminal` |
| `GET /presence` | first message | `Gateway.Presence` | `auth.ParseJWT`, `instance.UserConnected` |
| `GET /guest/connect` | guest token | `Gateway.GuestConnect` | `instance.AuthenticateGuest`, `instance.ServeGuest` |
| `GET /assistant` | token | `ChatHandler.Info` | `assistant.Enabled` |
| `GET /chats` | token | `ChatHandler.List` | `assistant.ListChats` |
| `POST /chats` | token | `ChatHandler.Create` | `assistant.CreateChat` |
| `GET /chats/{id}` | token | `ChatHandler.Get` | `assistant.GetChat`, `assistant.Events` |
| `DELETE /chats/{id}` | token | `ChatHandler.Delete` | `assistant.DeleteChat` |
| `POST /chats/{id}/messages` | token | `ChatHandler.Send` | `assistant.Send` |
| `POST /chats/{id}/cancel` | token | `ChatHandler.Cancel` | `assistant.Cancel` |
| `GET /chats/{id}/stream` | token | `ChatHandler.Stream` | `assistant.Watch`, `GetChat`, `Events`, `Draft` |
| `GET /chats/{id}/architecture` | token | `ChatHandler.Architecture` | `assistant.Architecture` |
| `POST /chats/{id}/architecture/approve` | token | `ChatHandler.ApproveArchitecture` | `assistant.ApproveArchitecture` |
| `GET /agents` | token | `AgentHandler.List` | `assistant.ListAgents` |
| `POST /agents/{id}/stop` | token | `AgentHandler.Stop` | `assistant.StopAgent` |
| `POST /llm/v1/chat/completions` | guest token | `llmgateway.ChatCompletions` | `instance.AuthenticateGuest`, `Fleet.TaskActive` |
| `GET /guest/team` | guest token | `teamapi.Files` | `instance.AuthenticateGuest`, `assistant.MachineRecord` |
| `POST /mcp` | guest token | `teamapi.MCP` | `instance.AuthenticateGuest`, `assistant.Team`, `ShareNote`, `AskAgent`, `RunService`, `StopService` |

The three WebSocket routes and the routes machines call (the model gateway,
the team's record and the MCP server) sit outside `RequireAuth` because they
authenticate in their own way. The chat stream is an ordinary
authenticated request: the frontend reads it with `fetch`, which can send an
`Authorization` header, where the browser's `EventSource` cannot.

### Handlers

Handlers in [internal/httpapi](../internal/httpapi) are thin. Each one reads
the user from the context, decodes the request, calls one service method, and
encodes the result. Response types are separate structs from the database
types, so a new column is never exposed by accident (`guest_token_hash` and
`password_hash` have no way out).

### Errors

`writeInstanceError` is the single place that maps domain errors to HTTP:

| Error | Status |
|---|---|
| `ErrInvalidName`, `ErrInvalidSize`, `ErrInvalidStack` | 400 |
| `ErrStackUnavailable` | 503 |
| `ErrNotFound` | 404 |
| `ErrNameTaken`, `ErrBusy`, `ErrNotProvisioned` | 409 |
| anything else | 500, logged; the client sees only "internal server error" |

`writeChatError` does the same for chats: `ErrNotFound` → 404, `ErrBusy` →
409, `ErrInvalidMessage` → 400, `ErrDisabled` (no model configured) → 503.

Every error body is `{"error": "message"}`. The model gateway is the
exception: it answers in the shape OpenAI-compatible clients expect
(`{"error": {"message": …}}`), so that OpenCode can show the message.

### CORS

The frontend is served from a different origin than the API, so browsers
require the API to opt in. The router allows the origins in
`CORS_ALLOWED_ORIGINS`, the methods `GET`, `POST`, `DELETE`, and the headers
`Authorization` and `Content-Type`. The same list is the WebSocket origin
allow-list.

## 18. Three requests, step by step

### "New machine" (create, then start)

`POST /instances {"name":"dev"}` then `POST /instances/{id}/start`

1. `RequireAuth` verifies the token and loads the user.
2. `InstanceHandler.Create` decodes the body and calls `instance.Create`.
3. The name is validated, a guest token generated, and a row inserted with
   status `creating`.
4. On a context detached from the request, the Docker provider creates the
   volume and the container.
5. The handle is stored and the status becomes `stopped`. Response: `201`.
6. The start request claims the instance (`stopped → starting`), the provider
   starts the container, and the status becomes `running`. Response: `200`.
7. Inside the container, `guestd` starts, launches code-server, and dials the
   server. The server authenticates it and registers it in the Hub. This
   happens *after* the start request has returned, typically within a second.

### Opening a terminal

`GET /instances/{id}/terminal` (WebSocket)

1. The browser connects and sends its `auth` message.
2. The gateway verifies the token, then `instance.OpenTerminal` checks that the
   user owns the instance and that it is `running`.
3. The Hub finds the machine's gRPC client, waiting if `guestd` has not
   connected yet, and opens a `Terminal` stream.
4. `guestd` starts bash on a PTY.
5. The gateway sends `ready` and relays in both directions until one side
   ends.

### Opening the editor

1. The frontend calls `POST /instances/{id}/ports/13337/url` with path
   `/?folder=/home/coder`.
2. The handler confirms ownership and returns a URL on
   `13337-<id>.coder.localhost:8080` containing a ticket.
3. The frontend sets that URL as a frame's source.
4. `Proxy.Wrap` recognises the machine hostname. The path is the sign-in path,
   so the ticket is verified and marked used, a session cookie is set, and the
   browser is redirected to `/?folder=/home/coder`.
5. The browser requests that page with the cookie. The proxy verifies the
   session, strips the cookie, and forwards the request.
6. The transport needs a connection, so it calls `DialPort`. A `Forward`
   stream is opened; `guestd` connects to `localhost:13337` and answers
   `opened`.
7. code-server's response flows back. The editor then opens a WebSocket to the
   same origin, which takes the same path through the proxy.

## 19. When things go wrong

| Event | What happens |
|---|---|
| Browser tab with a terminal closes | The WebSocket read fails; the gateway closes the terminal; the gRPC stream is cancelled; `guestd` sends `SIGHUP` to the shell's process group, then `SIGKILL` after 3 s. |
| The shell exits | `guestd` sends the exit code; the gateway forwards it and closes the WebSocket with code 1000. |
| The shell exits with a background job still running | Same: output is given 200 ms to drain, then the PTY is closed regardless. |
| Client disconnects during create / start / stop | The operation continues on its detached context and records its result. |
| Two requests operate on one instance at once | One wins the claim; the other gets `409`. |
| The provider call fails | Status becomes `error`; the user can retry or delete. |
| The server crashes during an operation | At next startup the instance is marked `error`. |
| The server restarts | Open terminals drop. Machines keep running; each `guestd` reconnects (1 s, 2 s, 4 s … up to 30 s). Port sessions stay valid, since they are signed and not stored. Idle timing starts afresh, so every machine gets a full grace period. |
| A machine is stopped | Its tunnel closes; the Hub unregisters it; open terminals end with code 4503. |
| The user closes the browser, signs out, or the laptop sleeps | Their connections close; after the grace period their running machines are stopped (section 15). |
| The user reloads the page, or the network drops briefly | The presence connection comes back within seconds, well inside the grace period; nothing stops. |
| A user kills `guestd` inside their machine | The container exits; Docker restarts it; `guestd` reconnects. |
| The Docker daemon or the host restarts | Machines that were running start again (`unless-stopped`). |
| A machine vanishes without closing its tunnel | yamux keepalives fail; the session closes; the Hub unregisters it. |
| A second tunnel arrives for the same instance | It replaces the first, which is closed. |
| Nothing is listening on a requested port | `guestd` fails the `Forward` call; the proxy shows a page that retries. |
| The server is asked to stop | New connections are refused; requests in flight get 30 s; WebSockets end when the root context is cancelled. |
| The user closes the browser while the assistant is working | The run carries on: it belongs to the server, not to the request. Their machines stay up until it ends. Reopening the chat shows everything that happened. |
| The user sends a second message while the assistant is working | `409`; nothing is recorded. |
| The user presses stop | The run's context is cancelled; the agents it was waiting for are told to abort (their running commands are killed); the chat becomes `idle` with a "stopped" note. Background agents carry on. |
| The user stops one agent | That agent's task ends and OpenCode aborts it. If the assistant was waiting for it, it is told and carries on; if it was in the background, the chat is told and woken. |
| The user types instead of approving a design | It is taken as what to change: the model is told the design was not approved and what they said, and proposes again. |
| A machine with services stops and starts | Its services start again by themselves, from the list the machine fetches on starting. |
| A service keeps exiting | It is restarted with backoff, and each exit is in its log. The agent that registered it is told it never answered. |
| One agent asks another that is busy | The question waits for the other's next task; the asker is given what is recorded and carries on. |
| A background agent finishes while nobody has the app open | Its chat is woken and the assistant deals with it; the user sees it all when they come back. |
| Two tasks are given to one agent at once | One starts; the other is refused, and the model is told to use another agent. |
| A tool fails (name taken, machine won't start, the agent reports an error) | The model is told, as the tool's result, and decides what to do next. |
| The model can't be reached, or rejects the key | The run ends with an `error` event carrying the model's message; the chat is `idle` and the user can try again. |
| The model keeps calling tools without finishing | The run stops after 40 steps and says so. |
| The server restarts during a run | At startup the chat is set to `idle` with a note. An agent that was mid-task stops at its next call to the model, which the gateway now refuses; what it had done is on disk. |
| A coding task takes more than 30 minutes | The agent is told to abort; the model is told the task was stopped and that what it did so far remains. |
| The stream of a chat drops | The frontend reconnects from the last event it saw. |
| A machine is deleted while a task runs on it | The request to its agent fails; the tool's result says so. |
| Postgres is down | Requests fail. Authenticated ones return `401`, because the middleware treats every `ParseJWT` failure alike (a known gap). |

## 20. Security model

### Who can do what

- **A user** can act only on instances they own. The owner is part of the SQL
  query (`WHERE id = $1 AND owner_id = $2`), so there is no code path where
  the check can be forgotten, and another user's instance returns the same
  `404` as one that doesn't exist.
- **A machine** can act only as itself. It proves its identity with its guest
  token, and the tunnel is registered under that instance ID.
- **A browser frame on a machine origin** can reach one port of one machine,
  with a session bound to both.
- **The assistant** acts as the user whose chat it is, and only through the
  eleven tools. Every tool takes the chat's owner and resolves machine names
  among that owner's machines, through the same owner-scoped queries as the
  REST API. It cannot delete a machine.
- **A coding agent** can do anything inside its own machine, as the user
  could, and reach the user's other machines over their private network. It
  can call the model, through the gateway, only while it has a task.

### Credentials

| Credential | Held by | Proves | Lifetime | Stored server-side as |
|---|---|---|---|---|
| Password | user | identity at login | – | bcrypt hash |
| Access token | frontend | "I am user U" | for ever by default; `ACCESS_TOKEN_TTL` if set | not stored (signed) |
| Guest token | machine | "I am instance I" | life of the instance | SHA-256 hash |
| Ticket | a URL | "U may open port P of I" | 1 min, once | ID remembered until expiry |
| Session | cookie on the machine origin | same as the ticket | 12 h | not stored (signed) |
| Model API key | the server's environment | the platform, to the model provider | until rotated | not stored; never sent to a browser or a machine |

Nothing that can be used to impersonate someone is stored in a usable form:
a copy of the database yields password hashes and token hashes only.

### Boundaries

- **Between users:** owner-scoped queries.
- **Between machines:** today, Docker's container isolation, with CPU and
  memory limits. Users have root inside their container, and containers share
  the host's kernel. This is acceptable for trusted users, and is the reason
  the roadmap moves to virtual machines.
- **Between a machine's web content and the app:** separate origins.
- **Between one user's machines and another's:** each user's machines are on
  a Docker network of their own.
- **Between the machine and the control plane:** machines call in for four
  things only: to open their tunnel, to attach their link, to use the model
  while the assistant has them working, and to read their owner's team
  record or add a note to it. All other traffic is the control plane calling
  the machine. Each of the four is authenticated as the machine, with its
  guest token.
- **Between what the model says and what happens:** the model's output is
  treated as a request, not a command. Tool arguments are parsed and checked,
  names are resolved per owner, and anything the code doesn't recognise comes
  back to the model as an error.

### Things that are deliberately not secrets

- The guest token is visible to the user of that machine (it is in `guestd`'s
  environment). It only lets you be the guest of your own machine, which you
  already control. It is still removed from the environment of shells and the
  editor so that it doesn't end up in pasted `env` output. Since the
  assistant exists it is also what a machine presents to the model gateway,
  which is why the gateway only answers during a task (section 16). The link
  daemon is the one program `guestd` starts that is given it, because it
  attaches as the machine.

## 21. Timeouts and limits

| What | Value | Where |
|---|---|---|
| Access token lifetime | never expires (`ACCESS_TOKEN_TTL`, default `0`) | `config` |
| Provider operation | 2 min | `instance.operationTimeout` |
| Wait for a guest when opening a terminal | 30 s | `tunnel.guestWait` |
| Ping of a newly connected guest | 10 s | `tunnel.pingTimeout` |
| Tunnel keepalive interval / write timeout | 15 s / 10 s | `guestapi.MuxConfig` |
| Guest dial timeout | 15 s | `guest.dialTimeout` |
| Guest reconnect backoff | 1 s → 30 s, reset after 1 min connected | `guest` |
| Link handshake: the daemon's hello, and its answer to the first ping | 10 s each | `linkgw.handshakeTimeout` |
| Link keepalive interval / timeout, both ends | 20 s / 10 s | `linkgw`, `link/src/parent.rs` |
| Link dial timeout / reconnect backoff | 15 s / 1 s → 30 s, reset after 1 min connected | `link/src/parent.rs`, `backoff.rs` |
| Largest link frame | 256 KB | `linkapi.MaxFrame` |
| Frames waiting to be sent to one machine's link | 64 | `link.backlog` |
| Terminal auth message | 10 s | `wsgateway.helloTimeout` |
| Terminal WebSocket ping | every 30 s, 10 s timeout | `wsgateway` |
| Largest message from a browser | 1 MB | `wsgateway.maxMessageBytes` |
| Shell output drain after exit | 200 ms | `guest.drainTimeout` |
| Shell kill after hangup | 3 s | `guest.killTimeout` |
| Port dial inside the machine | 5 s | `guest.forwardDialTimeout` |
| Forwarded chunk size | 32 KB | `tunnel.maxForwardChunk` |
| Ticket / session lifetime | 1 min / 12 h | `portproxy` |
| Idle grace period | 2 min (`IDLE_STOP_AFTER`) | `config` |
| Idle check interval | a quarter of the grace period, between 5 s and 30 s | `instance.RunIdleStop` |
| Container stop grace period | 10 s | `sandbox/docker` |
| One run of the assistant | 90 min, 40 steps | `assistant.runTimeout`, `maxSteps` |
| One request to the model for the assistant's next step | 5 min | `cmd/server` |
| One coding task | 30 min | `assistant.taskTimeout` |
| Runs started by background agents with no word from the user | 20 | `assistant.maxWakes` |
| A file in a chat, or passed to or from a machine | 10 MB | `guestapi.MaxFileSize`, `assistant.MaxAttachmentSize` |
| Files with one message / one agent's post | 10 / 5 | `assistant.maxAttachments`, `maxPostFiles` |
| A text file's contents shown to the assistant | 16 KB each, 48 KB a message | `assistant.maxInlineText`, `maxInlineTotal` |
| Agents' posts in chats | 40 per user in 10 min | `assistant.postLimit`, `postWindow` |
| Agents in one listing | 100, working first | `store.maxAgentsListed` |
| Machine ready for a task / agent starting once online | 90 s / 45 s | `assistant.readyTimeout`, `agentStartTimeout` |
| One health check of an agent / other short calls to it | 3 s / 30 s | `assistant.healthTimeout`, `callTimeout` |
| Telling an agent to abort | 10 s | `assistant.abortTimeout` |
| A user's message | 20 000 characters | `assistant.maxMessageLength` |
| A tool result kept for the model | 8 000 bytes, beginning and end | `assistant.maxToolOutput` |
| Progress notes kept per task | 200 | `assistant.maxActivitiesPerTask` |
| Components in one design | 12 | `assistant.maxNodes` |
| Wait for a registered service to answer on its port | 45 s | `assistant.serviceStartTimeout` |
| One agent's answer to another | 5 min | `assistant.askTimeout` |
| Questions between one user's agents | 30 in 10 min | `assistant.askLimit` |
| How long a machine's routes are remembered | 5 s | `assistant.routeCacheFor` |
| A service's log before it is started afresh | 5 MB | `guest.maxServiceLog` |
| A request from an agent to the model | 16 MB | `llmgateway.maxRequestBody` |
| How often a machine checks for a newer team record | 3 s, backing off to 30 s while it fails | `guest.teamInterval` |
| An agent's request to the MCP server, as OpenCode waits for it | 10 min, because some tools wait for a service or another agent | `guest.agentConfig` |
| Tasks kept per agent in the team record / steps logged per task | 5 / 300 | `assistant.teamTasksPerAgent`, `store.maxTaskLog` |
| Notes in the team record; a note's topic and text | 50; 120 and 8 000 characters | `assistant` |
| Wait for the model to start answering an agent | 5 min | `llmgateway` |
| Chat stream keep-alive | every 20 s | `httpapi.streamHeartbeat` |
| HTTP header read / idle | 10 s / 2 min | `cmd/server` |
| Graceful shutdown | 30 s | `cmd/server` |
| Machine sizes | small 1 CPU / 1 GB, medium 2 / 2 GB (default), large 4 / 4 GB; disk 10 (default), 20 or 50 GB | `instance/size.go` |
| Least time between updates of a draft reply | 50 ms | `assistant.draftInterval` |

## 22. Tests

Run with `make test` (`go test -race ./...`, the same for the stand-in
described below, and `cargo test` for the link daemon: its reconnect waits, its handshake, the
proof peers give each other, and two whole daemons with a stand-in control
plane finding each other over real connections on this computer).
They need no database and no Docker.

| Package | What is tested | How |
|---|---|---|
| `instance` | Name validation | Table of valid and invalid names. |
| `tunnel` | Terminals: commands run, exit code, resize, exit with a background job, waiting for a guest, offline guest, disconnect detection | A real Hub and a real guest server joined by an in-memory `net.Pipe`, running real shells. |
| `tunnel` | Port forwarding: 3 MB HTTP bodies over a reused connection, half-close, closed port, offline guest, connection outliving its dial context | Same, forwarding to a local test server. |
| `presence` | Idle time: while the owner or the machine has a connection, after each leaves, with several connections, after reconnecting, and for things never seen | The tracker alone. |
| `portproxy` | Requests and WebSockets counting as use of the machine. Ticket and session rules, single use, binding to instance and port, key separation, expiry, origin check, host routing, error pages, WebSockets, cookie attributes, redirect safety | The real proxy in front of a test HTTP server, with a fake dialer. |

| `assistant` | The run loop: a plain answer, tools and their results, tools running at the same time, asking and resuming, refusing a second message, stopping, the step limit, a failing model, failing and malformed tool requests, ownership, recovery, watchers, history repair, the draft of a reply while it is written | The real service with a scripted model, in-memory storage and pretend machines. |
| `assistant` | Agents: a background agent working while the chat is free and waking it when done, one task per agent but agents in parallel, stopping background and foreground agents, news arriving while the model writes, the limit on wake-ups, recovery after a restart, deleting a chat, listing and stopping agents through the tools | The real service with fakes, as above. |
| `assistant` | Reading the model's reply: text and interleaved tool calls assembled from fragments, a reply cut off part-way, an error inside the stream, a reply that arrives whole, a refusal, and that the tool definitions are valid | The real client against a test server sending recorded shapes of the protocol. |
| `instance` | Sizes: each level and disk, defaults, and anything not on offer refused | Plain functions. |
| `assistant` | Coding tasks: the task and its report, progress notes (once per tool use, other sessions ignored, oversized events skipped), one session per chat and machine, a lost session, starting a stopped machine, one task at a time per machine, stopping and timing out (the agent is told to abort), an agent's errors, a machine with no agent, an unanswered health check, wording of errors | The real `Fleet` and HTTP client against a small HTTP server that speaks the parts of OpenCode's API in use. |
| `llmgateway` | The server's key and model replace the machine's; bad credentials; no task in progress; streaming is not held back; the model's errors pass through; oversized and malformed requests; no key configured | The real handler between a test client and a test upstream. |
| `guest` | The agent's configuration and where it finds the model and the MCP server | Plain functions. |
| `guest` | The copy of the team record: files written, an unchanged record not fetched twice, files of agents that are gone removed, paths outside the folder refused | The real sync against a test server, in a temporary folder. |
| `assistant` | The team record: only the owner's machines, agents and notes; each task with its steps and report; the files it is rendered to; notes checked | The real service with fakes. |
| `assistant` | Designs: proposing, asking for a change, approving as proposed and as edited, a design that doesn't hold together, who may approve, machines created as the design says, updating, routes from approved designs only, how each component stands, connections reaching machines as files and as services' environment, registering and stopping services, agents asking each other (free, busy, bounded) | The real service with fakes. |
| `guest` | Services: restarted after exiting, given their environment, left alone when unchanged, replaced when changed, stopped when no longer wanted, children stopped with them | The real manager running real processes. |
| `portproxy` | A path that leads to another machine, and that it grants no access of its own | The real proxy with a router of one route. |
| `teamapi` | The MCP server: agreeing a protocol revision, notifications, the tool list and its schemas, each tool, a note attributed to the machine that sent it, protocol errors, credentials; and the files endpoint with its `ETag` | The real handlers behind a test server, spoken to as an MCP client would. |
| `link` | The Hub: attached while serving, frames in order both ways, a machine that is not attached or not reading, a newer link replacing the old, ending with its context or a failed send, waiting for a link | The real Hub with in-memory streams. |
| `linkgw` | A daemon attaching and being answered; wrong, unknown and missing credentials; a failure to check them; a first frame that is not hello; another revision of the protocol; a daemon that never answers or never speaks; the same machine attaching twice; a daemon vanishing; a frame over the limit | The real gRPC server over an in-memory connection, spoken to as a daemon would. |
| `assistant` | The mesh: which machines a design makes peers, what each is told when a link attaches, a machine that does not exist yet, another owner's machine of the same name, a design only proposed, keys per mesh, what machines report | The real service with fakes. |
| `guest` | The link daemon's environment: the machine's identity with the token once, other programs still without it, and a machine from before links | Plain functions. |

The packages with no tests of their own are `store`, `wsgateway`,
`sandbox/docker`, `middleware` and `config`; `httpapi` has almost none. What
covers them is the end-to-end check, which is in the repository but apart
from the product:

- [`tools/standin`](../tools/standin/README.md) answers in place of the
  model. It is a Go module of its own; nothing in `cmd/` or `internal/`
  imports it, and the server reaches it only by being given its address
  (`make run-standin`). As the assistant it designs and builds a small shop;
  as a coding agent it does the work for real, with OpenCode's own tools.
- [`tools/uicheck/build.mjs`](../tools/uicheck/README.md) signs up in a real
  browser and follows that build on real machines, checking about eighty
  things from the question to the page being served.

It is run by hand, takes about six minutes, and needs Docker, Postgres and
the machine images. [STATUS.md](STATUS.md#how-changes-were-tested) says how
it was last run and what it does not cover.

## 23. Assumptions and known gaps

What follows is how the backend is, by design or by omission.
[STATUS.md](STATUS.md#open-issues) lists what a review found wrong and is
waiting to be fixed, in order.

- **One server process.** Several things depend on it: the Hub is in
  memory, startup recovery assumes nothing else is running operations, chats
  or agents, used tickets are remembered in memory, so is who is connected,
  so are the runs and agents in progress (and how to stop them), who is
  watching each chat, and which machines have a task (which the model
  gateway asks).
- **CI is set up by an agent, not run by the platform.** A design's CI
  component is a machine and instructions. Nothing triggers it on a change,
  records its results, or deploys anything.
- **Designs only describe our machines.** A component can be marked
  external, but nothing provisions or connects to a real cloud service, and
  there is nowhere to keep its credentials.
- **A design's connections give addresses, not credentials.** A database's
  user and password travel between agents as notes.
- **Stack images must be built on the server** (`make images`); a design
  that asks for one that isn't there has its machine refused.
- **A note's author is self-declared.** The MCP server knows which machine a
  note came from, not which agent on it, so the name on a note is whatever
  the agent gave. All of a user's agents are the user's own, so nothing is
  gained by lying, but it is not verified.
- **The team record is rebuilt on every request**, and every running machine
  asks every three seconds. Each request is three small queries and an `ETag`
  comparison; with many machines it would want caching per owner.
- **Agents on one machine share it.** They are told to keep to separate
  folders and leave each other's programs alone, but nothing enforces that.
  Two agents told to edit the same files will get in each other's way.
- **Idle means "nobody connected", not "nothing happening".** A long build
  stops with the machine if its owner closes the browser, and a machine whose
  owner leaves a tab open runs for as long as the tab does.
- **The API alone doesn't keep a machine running.** A machine started with
  `curl` and no presence connection is stopped after the grace period.
- **No reconciler.** Nothing compares Postgres with Docker. If the server dies
  between creating a container and storing its ID, that container is orphaned
  (its labels identify it). If a container is removed by hand, the row still
  says `running`.
- **Operations are synchronous.** The HTTP request waits for the provider.
  That is fine for Docker and will need rethinking for slower providers.
- **No quotas, and a machine can't be resized.** Size is chosen at creation
  from three levels; nothing limits how many machines a user makes, or how
  fast accounts are made.
- **Machine names are per user; designs are per chat.** The assistant is told
  to reuse a machine of the right name, and services, routes and the check
  of a backend's API description all go by machine name. Two chats whose
  designs both name a machine `api` share it.
- **Machines can reach the host**, through `host.docker.internal`, and with
  it anything the host publishes on all interfaces. In development that
  includes Postgres as Compose publishes it.
- **A guest token never changes.** It is set when a machine is created, and
  whoever holds it can act as that machine from anywhere that reaches the
  control plane: read the owner's team record, and register services there.
- **An operation's last database update is not retried.** If it fails, the
  instance stays in its transitional status until the server restarts and
  recovery marks it.
- **A cancel can arrive between a chat being claimed and its run being
  registered**, and then frees a chat whose run is about to start.
- **Port sessions are not revocable.** A session stays valid for its 12 hours
  even if the user signs out.
- **Auth is minimal** (section 8).
- **The assistant has barely been run against the real model** (section 16).
- **A failed request to the model ends the run.** Nothing tries it again.
- **One model key for everyone, and no limit on its use.** Every user's
  assistant spends the same key, with no per-user quota or record of usage.
  During a task, the user of that machine could also call the model through
  the gateway themselves.
- **The whole chat is sent to the model every step.** Tool results are
  shortened, but a very long conversation will eventually exceed what the
  model accepts. Nothing summarises or trims it yet.
- **A page reaches another machine only by a path the design gives it.** A
  page shown through the port proxy talks to its own machine's origin; a
  connection with a path is what sends part of that origin to another
  machine (section 14). Anything else, the frontend calls from its server
  side.
- **Docker is not a strong boundary** between mutually distrusting users.

## 24. Where to change things

| To… | Look at |
|---|---|
| add an API endpoint | `internal/httpapi/router.go` and a handler beside the existing ones |
| add a rule about what users may do | `internal/instance/service.go` |
| add a column | a new migration, then `internal/store` |
| change how machines are created | `internal/sandbox/docker/docker.go` |
| add another infrastructure | a new package implementing `sandbox.Provisioner`; choose it in `cmd/server/main.go` |
| add something the server can ask a machine to do | `guest.proto`, `make proto`, implement it in `internal/guest`, call it from `internal/tunnel` |
| change what is installed in machines | `images/workspace/Dockerfile`, then `make image` |
| change the terminal protocol | `internal/wsgateway/gateway.go` and [API.md](API.md) |
| change who can reach a machine's ports | `internal/portproxy` |
| change what counts as a machine being in use | `internal/presence`, and the callers of `MachineConnected` and `UserConnected` |
| add a setting | `internal/config/config.go` and `.env.example` |
| give the assistant a new ability | a tool in `internal/assistant/tools.go`, its implementation in `execute.go`, and how it is shown in `web/src/components/assistant/transcript.tsx` |
| change how the assistant behaves | the system prompt in `internal/assistant/tools.go`; the text put before every task in `fleet.go` |
| use a different model or provider | `LLM_BASE_URL` and `LLM_MODEL`; `internal/assistant/llm.go` if it doesn't speak the OpenAI protocol |
| change how the coding agent is set up | `internal/guest/agent.go`, then `make image` |
| change who may use the model | `internal/llmgateway` |
| add a stack | a stage in `images/workspace/Dockerfile`, its name in the `Makefile`, and an entry in `internal/instance/stack.go` |
| change how the assistant designs | the "Designing what to build" part of the prompt in `internal/assistant/tools.go` |
| change what a backend must publish about its API | `internal/assistant/apidocs.go` (the path and the check), the rule's wording in `tools.go` and `fleet.go`, and `web/src/lib/openapi.ts` with `web/src/components/pane/backend-tab.tsx` for how it is shown |
| change what a design can hold | `internal/assistant/design.go`, the tool's schema in `tools.go`, and the diagram in `web/src/components/architecture/` |
| change how services are run on a machine | `internal/guest/services.go`, then `make images` |
| give agents another tool for knowing about each other | `tools` and `call` in `internal/teamapi/mcp.go` |
| change who a message goes to, how agents answer the user, or what they can post | `internal/assistant/people.go`; the tools in `internal/teamapi/mcp.go`; the transcript in `web/src/lib/chat.ts` and `web/src/components/assistant/transcript.tsx` |
| change the names agents are given | the list in `internal/assistant/handles.go` |
| change what files a chat takes, or where they go on a machine | `internal/assistant/attachments.go`; how they are served in `internal/httpapi/chats.go` |
| change what agents can know about each other, or how the files read | `internal/assistant/team.go` |
| change what the server and a machine's link daemon say to each other | `internal/linkapi/link.proto`, then `make proto` for the Go side and `make link` for the Rust side; handle the new frame in `internal/linkgw/server.go` and `link/src/parent.rs` |
| change the link daemon | `link/src/`, then `make images` and new machines |
| check a change end to end without the real model | `make standin`, `make run-standin`, then `node tools/uicheck/build.mjs`; a new step for the stand-in goes in `tools/standin/shop.go`, a new check in `build.mjs` |

## 25. The link

The distributed harness ([HARNESS.md](HARNESS.md)) has agents on different
machines settle things between themselves and follow each other's progress.
What carries that is a second connection from every machine: the **link**.
So far it connects them: a machine's link attaches to the server, stays
attached and comes back when it drops, and machines that a design connects
open streams to each other directly. Nothing is said over any of it yet but
greetings, proofs and pings.

```
 machine                                            server
 ───────                                            ──────
 linkd (Rust)  ── gRPC over HTTP/2, one stream ──►  linkgw :8081 ──► link.Hub
   started and kept up by guestd                      checks the machine's credentials
```

### Why a second connection, and why Rust

The tunnel (section 11) is the server reaching *into* a machine: it is the
gRPC client, and it opens a stream when it wants a terminal or a port. The
link is the opposite kind of thing: one stream that stays open, on which
either end speaks whenever it has something to say, and which the daemon
will later open to the owner's other machines as well. That is its own
protocol, so it is its own connection.

It is Rust because it sits on every machine beside the user's own work, for
as long as the machine runs, and should cost next to nothing there. Measured
in a machine: a static binary of 1.25 MB and 1.3 MB of memory. `guestd`, in
Go, is 12 MB and about 13 MB.

### The protocol

[internal/linkapi/link.proto](../internal/linkapi/link.proto) is the whole of
it. The Go code beside it is generated by `make proto`; the Rust code is
generated from the same file each time `linkd` is built
([link/build.rs](../link/build.rs)), so the two ends cannot drift apart.

One call, `Channel`, a stream of `Frame` in each direction. A frame has an
`id` if it expects an answer, a `reply_to` if it is one, and a body, which
so far is one of three: `Hello` (what the sender is, and which revision of
the protocol it speaks), `Ping`, `Pong`, and three for the mesh described below (`Peers`, `Proof`,
`Status`). A node ignores a kind of frame
it does not know, which is what lets the protocol grow without every machine
being rebuilt at once.

### Attaching

1. `linkd` starts with `CODER_LINK_URL`, `CODER_INSTANCE_ID` and
   `CODER_GUEST_TOKEN`. It dials the URL and calls `Channel`, with
   `authorization: Bearer <instance id>.<guest token>` in the call's
   metadata: the same credential the machine presents to the model gateway
   and the team API.
2. `Server.Channel` ([linkgw](../internal/linkgw/server.go)) checks it with
   `instance.AuthenticateGuest`. A wrong token ends the call with
   `Unauthenticated`; a database that could not be asked ends it with
   `Internal`, so a daemon is never told its credentials are wrong when it
   is the server that is in trouble.
3. Each side sends `Hello` first. The server ends the call if the daemon's
   is not the first frame, or names another revision.
4. The server sends a `Ping` and waits for its `Pong`. Only a link that
   answers is advertised, as with a newly connected guest.
5. The stream is handed to `Hub.Serve`, which blocks until it ends.

Each step of the handshake has ten seconds.

### The Hub

[link.Hub](../internal/link/hub.go) is a map from instance ID to that
machine's attached link, modelled on the tunnel's Hub.

- **One sender.** A gRPC stream takes one sender at a time. `Serve` is that
  sender: everything else puts frames on a queue with `Send`.
- **Nothing waits, and nothing grows.** The queue holds 64 frames. `Send`
  to a machine that is not attached, or whose queue is full, returns an
  error at once. A frame that was queued can still be lost if the stream
  drops first; whoever needs to know a frame arrived will ask for an answer
  to it.
- **In order.** Frames from a machine are passed to a handler one at a time.
- **Replacement.** The same machine attaching again wins, and the older
  stream's `Serve` returns `ErrReplaced`. On its way out it unregisters only
  itself, never its replacement.
- **Waiting.** `Wait` blocks until a machine's link attaches, woken by a
  channel that is closed whenever one does.

Like the tunnel's Hub it does no authorisation, and it holds everything in
memory, which is one more thing that assumes a single server process.

### Noticing a dead connection

Both ends send HTTP/2 keepalive pings on a stream that has been quiet for 20
seconds, and give up on it if a ping goes unanswered for 10. So a machine
that vanished is detached within half a minute, and a daemon whose server
vanished notices in the same time and starts dialling again. The server
lets daemons ping no more often than every 10 seconds.

### Reconnecting

`linkd` follows the tunnel's policy exactly ([backoff.rs](../link/src/backoff.rs)):
wait one second, double it up to thirty, and start again from one second
after a connection that lasted more than a minute. It dials again whatever
the reason the stream ended, a refusal included: a machine has no way to
tell a token that is wrong for ever from a server that is being restored.

### On the machine

`RunLink` ([internal/guest/link.go](../internal/guest/link.go)) keeps `linkd`
running with the same `supervise` loop as the editor and the coding agent.
The daemon is given `guestd`'s environment and, unlike everything else
`guestd` starts, the guest token: it is the machine speaking for itself.

Two kinds of machine run none, and say so in their log: one created before
links existed, which was never given `CODER_LINK_URL`, and one whose image
has no `linkd`. Everything else on such a machine works as before.

### Building it

`linkd` is not built in the machine image's Dockerfile. `make link` builds
it on the host with Cargo, as a static binary for
`<processor>-unknown-linux-musl`, and copies it to
`images/workspace/bin/linkd`, where the Dockerfile picks it up; `make image`
and `make images` run it first. This needs Rust with that target, and
`protoc`:

```bash
rustup target add x86_64-unknown-linux-musl
```

The release profile is set for size (`opt-level = "z"`, link-time
optimisation, one codegen unit, `panic = "abort"`, stripped). It uses `tonic`
with only its client half, and `tokio` with one thread.

### Machines finding each other

A design's connections say which machines work together. The assistant
turns that into a **mesh** for each chat with an approved design
([mesh.go](../internal/assistant/mesh.go)): for every machine, the machines
its components have a connection with, in either direction.

- **Telling them.** Whenever a link attaches, and whenever a design is
  approved or corrected, every attached machine of that owner is sent a
  `Peers` frame: for each mesh it is in, the mesh's key and its peers, each
  with an ID and an address (`http://<machine name>:7600`, on the owner's
  private network). It replaces what the machine was told before. A machine
  the design names but nobody has made yet is left out, and its peers are
  told again when it attaches.
- **The key.** Each mesh has a random 32-byte key, made the first time it
  is needed and held in the server's memory only. After a restart machines
  are given new ones and prove themselves again, which takes a few seconds.
- **Who dials.** Of two machines the one with the smaller ID dials the
  other, so there is one stream between them. `linkd` listens for peers on
  port 7600 ([peer.rs](../link/src/peer.rs)).
- **Proving.** Neither has a credential the other can check, so each sends
  a nonce in its hello and answers the other's with an HMAC under the
  mesh's key, over its role, both IDs, the mesh and the nonce
  ([proof.rs](../link/src/proof.rs)). The key never crosses the wire. The
  side that accepted proves itself first, and answers only a machine the
  control plane named as its peer.
- **Reporting.** Each machine sends the control plane a `Status` saying
  which peers it has a proven stream with, on attaching and on every
  change. The server keeps the latest and logs changes. A frame that
  arrives before the daemon's first pong is kept and dealt with once the
  link is attached; refusing it was a bug that made links take half a
  minute to come back.

Nothing is sent between machines yet but greetings, proofs and pings.

### What it does not do yet

- Nothing in the server sends a frame down a link except the handshake and
  `Peers`, and nothing shows a user which machines are linked.
- Machines say nothing to each other yet, and the coding agent has no tools
  for any of this.
- The connections are not encrypted by `linkd` itself. In development they
  run over the Docker bridge; in production the one to the server would sit
  behind whatever terminates TLS, which `linkd` cannot yet dial.

[HARNESS.md](HARNESS.md#order-of-work-and-where-it-stands) says what comes
next.
