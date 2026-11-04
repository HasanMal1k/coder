# Where things stand

Last updated: 2026-10-19.

This is the page to read first when picking the project up again. It says
what exists, what was done most recently, what is known to be wrong, and how
changes have been checked. Update it at the end of every working session.
[ARCHITECTURE.md](ARCHITECTURE.md) is the overview of the design;
[BACKEND.md](BACKEND.md) explains the backend in detail;
[API.md](API.md) is the HTTP and WebSocket API;
[../web/README.md](../web/README.md) covers the frontend;
[MVP.md](MVP.md) is what the product is and what the first version must do;
[HARNESS.md](HARNESS.md) is the design of the distributed harness, which is
the work in progress.

## State of the repository

- Branch `instances-backend`. The last commit is `6a0564f` ("Frontend
  change", by the owner on 2026-10-06), which holds the frontend pass of
  that day and everything before it.
- **Uncommitted:**
  - from 2026-10-07, `docs/MVP.md` and the edits to this page and
    `CLAUDE.md` that go with it;
  - from 2026-10-12, the end-to-end check: `tools/standin/`,
    `tools/uicheck/build.mjs` with the changes to `common.mjs`, `clean.mjs`
    and that folder's README, and `docs/HARNESS.md`;
  - from the same day, the link: `link/` (Rust), `internal/linkapi/`,
    `internal/link/`, `internal/linkgw/`, `internal/guest/link.go`, and
    changes to `cmd/server`, `cmd/guestd`, `internal/config`,
    `internal/instance`, `internal/guest/connect.go`, the machine image's
    Dockerfile, the `Makefile`, `.dockerignore`, `.gitignore`,
    `.env.example` and the documents;
  - from 2026-10-15, the chat as a room (agents' names, `@` messages,
    agents' posts and questions, files): a migration
    (`20261015120000_chat_people`), `internal/assistant/people.go`,
    `handles.go`, `attachments.go` and their tests,
    `internal/store/attachments.go`, `internal/guest/files.go`,
    `internal/tunnel/files.go` and their tests, changes across
    `internal/assistant`, `internal/teamapi`, `internal/httpapi`,
    `internal/instance`, `internal/guestapi/guest.proto` (regenerated),
    `cmd/server`, `web/src` (new: `assistant/people.tsx`,
    `assistant/attachments.tsx`), `tools/standin` and `tools/uicheck`, and
    the documents.
  - from 2026-10-19, the first step of the plan below (the model's limits,
    its reasoning, the prompts): changes to `internal/assistant` (`llm.go`,
    `service.go`, `tools.go`, `fleet.go`), `internal/llmgateway`,
    `internal/config` (and its first tests), `internal/instance`,
    `internal/guest`, `internal/guestapi/transport.go`, `cmd/server`,
    `.env.example` and the documents.
  `learn/` is the owner's and was not touched.
- `.env` is ignored by git. It holds a real `DEEPSEEK_API_KEY` and
  `LLM_MODEL=deepseek-v4-pro`.

## Starting it

Nothing is left running between sessions except, usually, the Postgres
container.

```bash
make db        # Postgres and migrations
make run       # the server, on :8080 (reads .env)
make web       # the frontend, on http://coder.localhost:3000
```

To run it without the real model, which is how everything here is checked:

```bash
make standin       # answers in place of the model, on :9099
make run-standin   # the server, pointed at it (in place of `make run`)
```

The server listens on two ports: `:8080` for everything a browser uses, and
`:8081` for machines' link daemons.

The standard machine image, `coder-workspace:dev`, was last built on
2026-10-19, with the model's limits passed to OpenCode. Machines made before
then keep 64,000 and 8,192. The `go`, `cpp`, `dotnet` and
`data` ones were last built on 2026-10-12: **machines made from them cannot
be given files or send any back** (a task's files are skipped with a note,
and an agent's post with files fails) until `make images` is run.
Rebuild them with `make images` after changing anything `guestd` runs, or
the link daemon; machines made from an older image keep what they were made
with, and one made before 2026-10-12 has no link.

`make images` first builds the link daemon on this computer (`make link`),
which needs Rust and `protoc`. Both were installed for this user on
2026-10-12: Rust 1.99 through `rustup` (in `~/.cargo` and `~/.rustup`, with
the `x86_64-unknown-linux-musl` target, `rustfmt` and `clippy`), `protoc`
29.3 in `~/.local`, and the two Go plugins for it in `~/go/bin`.
`rustup self uninstall` removes the first.

## What exists

- Accounts, machines (create, start, stop, delete) as local Docker
  containers, a browser terminal, VS Code in the browser, automatic stopping
  of machines nobody is connected to.
- An assistant that designs an application with the user, has the design
  approved as a diagram, then builds it by creating machines and directing a
  coding agent (OpenCode) inside each. Agents are named, run in the
  foreground or the background, can ask each other questions, share notes,
  and register programs as services that the machine keeps running.
- A frontend that is a chat: earlier chats on the left, the chat in the
  middle, and a panel on the right with four tabs (Graph, Frontend, Backend,
  Dashboard). Dark gray with an orange accent.
- A chat is shared like a channel: the user, the assistant and its agents,
  each agent with a name of its own given by the platform (`tom`, `ava`),
  unique among the user's agents. `@tom` in a message sends it to that
  agent directly, and its answer is posted in the chat; agents also post
  there and ask the user to choose (`post_to_chat`, `ask_owner`), with
  pictures from their machine. Images and files can be attached, pasted or
  dropped; the assistant reads text files, and files go onto a machine with
  a task. Laid out like Slack: a picture, name and time on every message,
  agents pictured by the logo of what they build.
- Every backend must describe its API with an OpenAPI document at
  `/openapi.json`; the platform checks for it and the Backend tab is drawn
  from it.
- A stand-in for the model (`tools/standin`) and a script that builds a
  whole project with it in a real browser (`tools/uicheck/build.mjs`), so
  the platform can be checked end to end without spending credits.
- A design for the distributed harness ([HARNESS.md](HARNESS.md)): a small
  Rust program on every machine through which agents settle what passes
  between them before building, and follow each other's progress while
  they build. The connections are built: the program, `linkd`, attaches to
  the server from every new machine, and machines that a design connects
  link to each other directly. Nothing is said over them yet.

## What changed most recently

| When | What |
|---|---|
| 2026-10-02 | The assistant: chats as events, tools, OpenCode in every machine, the model gateway. |
| 2026-10-03 | Agents as processes (named, background, stoppable); the team record, its MCP server and the copy on each machine. |
| 2026-10-04 | Design first: questions, a proposed architecture, approval on a diagram. Stack images, services, connections as environment variables and as paths through the port proxy, agents asking each other, git and an agent-run CI component. |
| 2026-10-04 | Sign-in tokens made permanent by default (`ACCESS_TOKEN_TTL=0`). |
| 2026-10-04 | The frontend rebuilt as a chat with the diagram beside it; the dashboard page removed (the machines list is at `/machines`). |
| 2026-10-04 | First use with the real model. A tool schema it rejected (`"required": null`) was fixed. |
| 2026-10-04 | Restyled black and white, then black only: the light theme and `next-themes` removed, Geist font, small corners, borders. |
| 2026-10-04 | The panel's four tabs. `POST /instances/{id}/ports/{port}/request`. The OpenAPI rule and its check. Service revisions: registering a service again restarts it, and the machine reports what it has started. |
| 2026-10-05 | A review of the whole codebase; its findings are the list below. No code changed. |
| 2026-10-06 | `learn/` added, for the owner to learn Go from this codebase: five projects to write by hand, each tied to the packages it mirrors (`learn/README.md`), interview notes (`learn/INTERVIEW.md`), and a worked Docker and Compose example that was built and run (`learn/docker-example/`). The example is a Go module of its own, so it is not part of the product's build or of `make test`; `learn` was added to `.dockerignore`. No product code changed. |
| 2026-10-06 | After the owner looked at it: restyled from black and outlined to dark gray with an orange accent and softer shapes, and the graph redone (logos, glowing lines, no library mark). See [the second round](#the-second-round-the-same-day). |
| 2026-10-06 | A pass over the frontend, using two skills found for it and installed in `.claude/skills/` (`frontend-design`, Anthropic's; `web-design-guidelines`, Vercel's). What changed is listed under [The frontend pass](#the-frontend-pass-of-2026-10-06). |
| 2026-10-07 | [MVP.md](MVP.md) written: the product (to be sold), who it is for, the distributed harness, a model per machine chosen for its work, the MVP's scope and what "done" means. No code changed. |
| 2026-10-12 | The distributed harness designed with the owner: [HARNESS.md](HARNESS.md). OpenCode stays; a Rust program on each machine carries messages; machines talk directly; a machine's agent has the say on its own component. No product code changed. |
| 2026-10-12 | The stand-in for the model rebuilt, in the repository, with the end-to-end check in a browser. See [How changes were tested](#how-changes-were-tested). The memory OpenCode uses was measured on the way: see [What was noticed](#what-was-noticed-on-2026-10-12). |
| 2026-10-12 | The mesh, step 2a of the harness: machines a design connects link to each other directly and prove themselves. See [The mesh](#the-mesh-step-2a-of-the-harness). |
| 2026-10-12 | Decided, not built: machines are to become small virtual machines on a rented server, with sizes that can be changed. gVisor is dropped as the plan and Fly.io is no longer the long-term answer. [MVP.md](MVP.md#isolation-cheaply). |
| 2026-10-12 | The link, step 1 of the harness: `linkd` (Rust) on every machine holding a gRPC stream to a new listener on the server. Built on this computer, not in Docker, at the owner's asking. See [The link](#the-link-step-1-of-the-harness). |
| 2026-10-15 | The chat as a room, asked for as "more like Slack": agents with names, `@` to talk to one directly, agents posting and asking the owner (mockups and all), image and file uploads. See [The chat as a room](#the-chat-as-a-room-2026-10-15). |
| 2026-10-18 | After a real build: why agents worked in stages and did the minimum, a plan agreed with the owner (OpenCode to be replaced by DeepSeek Harness over ACP, a reviewer agent, lighter machines, Fly.io for testing), and its first step done. See [Agents at once, and doing the whole job](#agents-at-once-and-doing-the-whole-job-2026-10-18). |

## What has been checked, and what has not

- **Unit tests** (`make test`) pass: the Go packages, the stand-in's, and
  the link daemon's nine in Rust. Coverage is high where the logic is:
  `assistant` 85%, `portproxy` 93%, `teamapi` 84%, `tunnel` 82%,
  `llmgateway` 77%. It is low or absent for `store` (no tests), `httpapi`
  (1%), `wsgateway`, `sandbox/docker`, `auth` (18%), `instance` (27%) and
  `guest` (37%).
- **End to end**, the whole flow is run in a real browser against real
  machines and the real OpenCode, with the stand-in for the model:
  `tools/uicheck/build.mjs`, 79 checks. Run four times on 2026-10-12. The
  last two passed all 79; the last also met issue 24 and stepped round it,
  which it says in a line of its own. The first failed two checks that
  misread the agent's log, and the second failed at the Backend tab, which
  is how issue 24 was found. See [How changes were
  tested](#how-changes-were-tested). The frontend has no unit tests.
  **It has not been run since the link was added**, which changed `guestd`
  and every machine image: the owner asked for less use of Docker, and it
  starts four machines. One machine was started instead (below).
- **The link** was checked for real on this computer and in one machine:
  see [The link](#the-link-step-1-of-the-harness).
- **The frontend**, after the pass of 2026-10-06: Prettier, ESLint, `tsc`
  and `next build` are clean, and `tools/uicheck` drove it in headless
  Chromium against sample data: 28 checks, all passing, and each screen
  looked at as a picture at desktop and phone widths. Not checked: any
  browser but Chromium, a real phone, the editor and terminal on a real
  running machine (the sample machines are rows only), and anything said by
  the real model.
- **With the real model** (DeepSeek, `deepseek-v4-pro`) there has been one
  real chat. It asked its questions, proposed a design of five components on
  four machines, was approved and created the machines. The database agent's
  task then ended with "context canceled" and the chat with "You stopped the
  assistant". Whether Stop was pressed on purpose is not known, and this has
  not been looked into. Nothing else has been run against the real model.
- The machines from that chat (`web`, `api`, `db`, `ci`) were made before
  the last image rebuild, so they run the older `guestd`, and their backend
  predates the OpenAPI rule: its Backend tab will most likely say the API
  has no description.

## Open issues

From the review of 2026-10-05, with three added by the frontend pass (21 to
23), one by the end-to-end check (24) and three by the chat as a room
(25 to 27). Paths are from the repository root. Two have been dealt with since and
are struck through; the numbers are kept, since other places refer to them.

### Fix first

1. **The platform's database is reachable from inside every machine.**
   Postgres is published on all interfaces with the password `coder`
   (`docker-compose.yml`, the `ports` entry), and machines can reach the host
   through `host.docker.internal`
   (`internal/sandbox/docker/docker.go`, `ExtraHosts`). Confirmed from inside
   a machine. Anything running on a machine could read or change every
   user's data. Publish it on `127.0.0.1` only and change the password.
2. ~~Nothing since `24ac9be` is committed.~~ Committed by the owner as
   `08709ab`. The frontend pass after it is not.
3. **Machine names are shared between chats.** A name is unique per user,
   a design is per chat, and the assistant is told to reuse a machine of the
   right name (`internal/assistant/tools.go`, "Check list_machines first").
   A second project that also wants `web`, `api` and `db` gets the first
   one's machines; services, ports, routes and the OpenAPI check are all
   keyed by machine name and would collide. Machines need to belong to a
   chat, or be named for it.
4. **A database error signs every user out.** `ParseJWT` wraps any failure
   to load the user (`internal/auth/service.go`), the middleware answers
   `401` to all of them (`internal/middleware/auth.go`), and the frontend
   signs out on `401` (`web/src/lib/api.ts`, `failure`). Only "no such user"
   should be a `401`.

### Before anyone else has an account

5. **No limits.** Sign-up is open and unthrottled, a user can create any
   number of machines (`internal/instance/service.go`, `Create`), and while
   a task is active the model gateway forwards anything
   (`internal/llmgateway/gateway.go`).
6. **Machines are ordinary Docker containers with root** (passwordless
   `sudo`), with no limit on processes or disk and no hardened runtime.
7. **Sign-up accepts anything**: an empty email or password; a password over
   72 bytes is a `500`. Login takes measurably longer for an email that
   exists.
8. **A leaked credential is permanent.** Access tokens never expire (the
   owner's choice) and cannot be revoked. A machine's guest token is fixed
   when the machine is created and is enough, from anywhere that can reach
   the server, to register services on that machine, which is running
   commands there.
9. **CORS is open when `CORS_ALLOWED_ORIGINS` is empty.** The comment in
   `internal/httpapi/router.go` says the opposite of what the library does.

### Correctness

10. **A chat's whole history is sent to the model at every step**, and one
    failed request to the model ends the run. Long chats will eventually be
    refused (`internal/assistant/service.go`, `run`).
11. **A machine can be left "starting", "stopping" or "deleting"** if the
    last database update of the operation fails; only a server restart
    recovers it (`internal/instance/service.go`, `Start`, `Stop`, `Delete`).
12. **Stop can race with Send.** Between a chat being claimed and its run
    being registered, a cancel frees the chat while the run still starts,
    allowing two runs at once (`internal/assistant/service.go`, `Send` and
    `Cancel`).
13. **A machine that fails to start or become ready during creation is left
    behind**, and a retry then finds its name taken
    (`internal/assistant/fleet.go`, `Create`).
14. **Only one server process can run**: event numbering, used tickets,
    presence, what machines report and the route cache are all in memory.

### Tests and smaller things

15. No unit tests for the frontend (`web/src/lib/openapi.ts` most wants
    them). The end-to-end tooling is in the repository since 2026-10-12
    (see below), but nothing runs it by itself: it is run by hand.
16. ~~The port proxy's error pages still use the old violet and light
    colours.~~ The app's own colours now (`internal/portproxy/proxy.go`,
    `errorTemplate`).
17. `npm audit` reports seven high-severity advisories under `shadcn`'s
    dependencies. They are in command-line tooling, not in what is sent to
    the browser.
18. The machine image downloads OpenCode, code-server and the toolchains
    without checking a hash (`images/workspace/Dockerfile`).
19. Every machine asks for its record every three seconds and each answer is
    rebuilt from the database, as is each look at a chat's design.
20. The code's default model name, `deepseek-chat`, is not among the models
    the owner's key lists (`deepseek-flash`, `deepseek-v4-pro`, as of
    2026-10-04). `.env` sets the model, so this only bites a fresh setup.
21. **What is showing is not in the address.** Which of the panel's tabs is
    open, the filter on the machines page, and whether a machine's page is
    on its editor, terminal or overview are all lost on a reload and cannot
    be linked to (`web/src/components/assistant/assistant-provider.tsx`,
    `machines/machines-page.tsx`, `workspace/workspace.tsx`).
22. **Long lists are drawn whole.** A chat's every message, an agent's every
    log line and every chat in the sidebar are in the page at once. Fine for
    dozens; a chat with thousands of log lines will be slow.
23. The panel's tabs still accept being drawn without a chat
    (`chatId?: string` in `web/src/components/pane/`), which can no longer
    happen now that a new chat has no panel. Harmless; worth tidying.
24. **The Backend tab can go on saying an API has no description after it
    has one.** The tab reads the description once, when the backend first
    answers on its port (`Api` in `web/src/components/pane/backend-tab.tsx`,
    whose effect runs again only if the machine or the port changes). If
    the backend first came up without its description and its agent added
    it afterwards, a tab that was already open keeps "doesn't describe its
    API" until "Look again" is pressed. Found on 2026-10-12 by
    `tools/uicheck/build.mjs`, in which the stand-in's API agent forgets
    the description on purpose and takes ten seconds to add it. With that
    pause it happened; before the pause was added it happened in one run of
    three. The script presses the button and says so in a line marked NOTE.
    Looking again every few seconds while the description is missing would
    settle it.
25. **Files uploaded and never sent are kept** until their chat is
    deleted (`attachments`). Nothing removes them sooner.
26. **Pictures in a chat have no stored size**, so they shift the page as
    they load (`web/src/components/assistant/attachments.tsx`, `Picture`).
27. **Images other than the standard one predate file passing.** Rebuild
    with `make images`; machines made before then cannot take files.

## Agents at once, and doing the whole job (2026-10-18)

The owner reported that agents did not all work at once (the frontend should
be built beside the backend), that they did the minimum, that the harness on
machines was shallow, forgetful and slow, that machines take too much memory
and disk, and that testing must move to real hardware for less than a
dedicated server costs.

**What the owner's real build of that day showed** (chat `948c0227…`, a Go
microservices shop, 41 minutes, DeepSeek): the database alone (13:16), then
the four services together (13:20), the storefront only after them (13:34),
although its task already listed every route, a separate round to start git
servers (13:39), and CI last (13:40 to 13:55).

- The stages came from the system prompt ("Build what others depend on
  first"), not the code: a turn's tool calls already run together.
- OpenCode was set to 64,000 tokens of context and 8,192 of output; DeepSeek
  V4 takes 1,000,000 and 384,000. A 40 to 50 step task filled it, so OpenCode
  summarised part-way and forgot.
- The parent assistant dropped the model's `reasoning_content`, which
  DeepSeek's documentation says must be sent back whenever tools are used.
  OpenCode 1 has the same problem (its issues #42553 and #35689, closed on
  2026-10-11 as "not planned").
- The parent's tasks said "keep dependencies minimal", and agents did:
  a hand-written JWT check and Redis client, two frontend tests, every
  service logging in to PostgreSQL as its superuser. Nothing checked the
  work.

**The plan**, agreed with the owner, in this order. Each step ends with the
checks it names.

1. *Done, below.* The model's own limits for agents, its reasoning kept and
   sent back, agents' reasoning effort set, and prompts that start everything
   at once and ask for finished work.
2. **DeepSeek Harness in place of OpenCode, over ACP.** On DeepSeek's own
   model it scores highest of the harnesses compared (DeepSeek's V4.1-Flash
   model card: Terminal-Bench 2.1, 90.6, against Claude Code 88.0, Pi 86.1,
   OpenCode 85.0, Codex 84.1). It is MIT, a developer preview that warns of
   breaking changes (to be pinned), and its memory is not yet measured.
   `guestd` runs it over stdio; an ACP client replaces `worker.go`.
   OpenCode and Goose speak ACP too, so the harness becomes a setting. A
   trial on one fixed task, dsh against OpenCode, with the real model and
   **only with the owner's go-ahead**, before OpenCode leaves the image.
   Claude Code was ruled out: its terms do not allow it inside a product
   that is sold. A harness of our own was ruled out by the owner.
3. **Everyone at once, enforced in code:** each connection's contract
   (routes and shapes, a database's names and logins) in the design, a
   `start_build` tool that starts every component's agent together and
   refuses one left out, a team tool `wait_for_service`, and git served by
   the platform when there is CI.
4. **The whole job:** a definition of done for each kind of component, the
   platform checking routes against the contract before it accepts "done",
   and a reviewer agent (the owner's choice; about 20 to 40% more model
   cost), at most two rounds of fixes.
5. **Memory and disk:** the harness and code-server started only when needed
   and stopped when idle; code-server stays in the image (the owner's
   choice); volumes from 5 GB that grow; OpenCode out of the image; the
   500 MB layer of build tools trimmed. The standard image is 2.24 GB:
   code-server 692 MB, build tools 500 MB, Node 214 MB, OpenCode 186 MB.
6. **Fly.io for testing**: a `sandbox.Provisioner` on Fly's Machines API, an
   app and private network per user, TLS for linkd, sign-up closed to a
   list. About $15 to $25 a month to test.

**Done on 2026-10-19 (step 1):**

- `LLM_CONTEXT_WINDOW` and `LLM_MAX_OUTPUT` (default 1,000,000 and 384,000),
  passed to new machines as `CODER_MODEL_CONTEXT` and `CODER_MODEL_OUTPUT`
  and given to OpenCode. Machines without them keep the old limits.
- `AGENT_REASONING_EFFORT` (default `max`), which the gateway sets on every
  request from a machine, as it sets the model.
- The parent keeps `reasoning_content` in each `assistant` event
  (`reasoning`) and sends it back.
- The system prompt: every component's agent started at once, in the
  background, each task saying the same about each connection; finished,
  working software asked for, never "minimal" or hand-written libraries;
  git served from each code machine's own task; CI started with the rest
  and sent back to run at the end. The text before every task: others are
  building at the same time, build against the design, retry or wait for
  what is not up yet, and do the whole task.

Checked:

- **Unit tests**, new: the reasoning kept apart from the text and sent back
  (stream and run loop); the gateway setting `reasoning_effort`, and passing
  the machine's own when none is configured; the model's limits in
  OpenCode's configuration and the fallback; a new machine's environment;
  the settings and their refusals (`internal/config`, which had no tests).
  `make test` passes: the Go packages under the race detector, the
  stand-in's, and the Rust daemon's.
- **The image** rebuilt (`make image`), and a container started from it with
  the new variables (no server, no account): OpenCode came up and reported
  `{"context": 1000000, "output": 384000}`, at 352 MB doing nothing. The
  container was removed.

Not checked:

- **A server with these changes.** One was already running on this computer,
  most likely the owner's (started about six hours earlier, before the
  changes), on the same database. A second would have marked the first's
  runs and machine operations as interrupted (issue 14), so none was
  started. That server runs the old code until it is restarted.
- The real model (not called), so neither whether DeepSeek takes the full
  limits without complaint nor whether the new prompts end the stages. The
  stand-in follows a script and would not show the latter either.
- The other four stack images (not rebuilt; machines made from them keep the
  old limits).

## The chat as a room (2026-10-15)

Asked for by the owner as: each machine's agent is "sort of a bot with a
basic name", called with `@` ("@tom do this"); the parent and the agents
talk to the user, and an agent can come up with mockups and ask which design
language to follow; "make it more like Slack"; image and file upload; "the
goal is that you do project management type stuff here".
[BACKEND.md, "The chat as a room"](BACKEND.md#the-chat-as-a-room) says how
it works. Choices made without asking, worth a look:

- **A name is given by the platform**, from a fixed list of short names
  (`internal/assistant/handles.go`), unique among all of a user's agents, so
  no two chats share one. The assistant still names each agent's job.
- **`@tom` goes to tom only.** The assistant is not run; it reads the
  message and tom's answer the next time it runs, and is told it need not
  act on them. `@assistant @tom` sends to both. Writing only to agents
  works while the assistant is busy.
- **An agent's post or question does not wake the assistant.** The
  answer to an agent's question goes to that agent.
- **The assistant itself has no name**; it is "Assistant", pictured by the
  terminal mark, dark on light.
- **Files are kept in Postgres** (`attachments`, 10 MB each). DeepSeek
  cannot see pictures; the assistant is told a picture's name and can pass
  it to an agent, and is shown text files' contents.

Checked:

- **Unit tests**: who gets a message, names and mentions, a busy agent
  getting the message afterwards, files to the assistant and onto machines,
  agents' posts and their mistakes (`internal/assistant/people_test.go`);
  the two MCP tools (`teamapi`); `PutFile` and `GetFile` in `guestd`, and a
  5 MB file both ways through the real tunnel (`internal/tunnel/files_test.go`);
  the stand-in's new script. The Go tests pass under the race detector, and
  the stand-in's pass; the Rust ones were not run (the link daemon is
  unchanged).
- **The HTTP API, against the running server**: an upload and download byte
  for byte; an uploaded HTML file comes back only as a sandboxed download;
  `../../` stripped from a name; 10 MB + 1 and empty files refused; a
  download without signing in is `401`; a message to `@zed`, not in the
  chat, is `400`; `post_to_chat` through `/mcp` as a machine lands in the
  chat, and one naming a file on a machine with no container fails cleanly.
- **One real machine**, made from the rebuilt image, with OpenCode and the
  stand-in, in headless Chromium: typing `@t` offers tom, and Enter picks that name;
  the box says where the message goes; a file attached and sent; "tom is on
  it"; tom's `ls` found the file at
  `/home/coder/.coder/attachments/<id>/logo.svg`; tom drew a mockup and asked
  with `ask_owner`, and the mockup came off the machine into the chat as a
  picture; choosing "Minimal" sent it to tom alone, and tom answered; the
  assistant was never run; no errors on the page. 14 checks, all passing.
  The account and machine were removed afterwards.
- **Looked at** with the seeded chats, at desktop and phone widths. One bug
  found and fixed on the way: pictures load after a chat has scrolled to
  its end, and the browser's scroll anchoring then moved the view up, which
  the chat took for the user scrolling away, so it stopped following
  (anchoring is off on the chat now). `check.mjs`: 24 of 24 (one check was
  pointed at the seeded chat's new last line).
- Prettier, ESLint, `tsc` and `next build` are clean.

Not checked: the real model (not called); `build.mjs`, the four-machine
end-to-end check (not run: it would be the first since the link, and the
stand-in's agents now read the new first line of their tasks, which its
unit tests cover); the `go`, `cpp`, `dotnet` and `data` images (not
rebuilt); two agents answering one message at once in a real browser (in
unit tests only); a screen reader.

New loose ends, also in the list below:

- A file uploaded and never sent stays until its chat is deleted.
- Pictures carry no size, so they move the page as they load. The chat
  copes (above); a size stored with each picture would end it.
- An agent's question waits for an answer in the chat only. Nothing reminds
  the user, and if they write to that agent about something else, that is
  taken as the answer.
- When an agent stops without answering (a server restart while it
  worked), the message to it says "stopped before answering" and nothing
  more; the message is not sent again.

## The frontend pass of 2026-10-06

Asked for as "the frontend can be further improved ... a lot better", with
a skill to be found for it. Two were, read in full before being installed,
and are in `.claude/skills/`: `frontend-design` (a way of working: plan,
check the plan against the brief, build, look at screenshots) and
`web-design-guidelines` (a checklist). The owner's rules for how it looks
were kept as they are; [web/README.md](../web/README.md#design) now says
what was added to them.

Things that were wrong, and are fixed:

- A table in a reply broke its words apart on a phone ("Machin e"), and the
  lines under its rows did not reach the last column.
- A component in the diagram let its machine's name wrap and its size run
  out of the box.
- A machine that could not be reached showed a white page inside the black
  app (issue 16).
- A chat did not open quite at its end: the row of machines arriving above
  left the last message under the box you type in.
- Opening a chat on a phone raised the keyboard.
- "Delete chat" deleted at one click. It asks first now.
- With "reduce motion" on, spinners turned thousands of times a second
  (the rule shortened animations without stopping them repeating).

Things that are different by design:

- A new chat (`/`) has the page to itself. The panel, which had nothing to
  show there, arrives with the chat. **This is the one change to how the
  app is laid out; say if the panel should be there from the start.**
- No picture beside the assistant's messages; the conversation is one
  column. What an agent does reads as a log, a kind to each line.
- Code in a reply has its language named and a button to copy it.
- The four figures on the machines page and the dashboard tab are one box
  divided by lines, not four cards.
- The box you type in is one line high until it needs more, with its button
  in the corner. The sidebar's rows are closer together.
- A skip link, focus shown on everything that opens and closes, and
  controls that ease only the properties they change.

### The second round, the same day

The owner looked at the result and said: "It looks too boxy, it's not giving
a great user experience, from black, make it a bit gray, and add another
orange tone, also the graph should look great it doesn't, remove the react
flow logo, also make the lines connecting nodes glowing, to show where the
data comes and goes. Use logos for the stack as well". That replaces the
earlier "black only, colour only for status" rule; `CLAUDE.md` and
[web/README.md](../web/README.md#design) now carry the new one. What was
done:

- **Colour.** The tokens in `web/src/app/globals.css`: a gray page
  (`#171717`), a darker sidebar, lighter cards, a darker "stage" under the
  project panel, and one orange for the main button, focus, and what is
  live. Lines are faint. The terminal's palette and the server's error page
  (`internal/portproxy/proxy.go`) were moved to match.
- **Less boxy.** No line under the header or beside the sidebar; the panel
  is a rounded surface set into the page; the user's messages, the answers
  to a question and the suggestions on the start page are tinted, not
  outlined; corners are larger (`--radius` 0.75rem).
- **The graph.** Logos by technology (a new dependency, `simple-icons`,
  which is CC0; only the logos imported are in the build), plain grey
  lines with one small orange dot moving in the direction of requests
  (glowing orange lines were tried first and the owner found them ugly),
  CI's lines thin and dotted, the node an agent is on lit in orange, React Flow's mark
  hidden. React Flow is MIT-licensed and hiding the mark is allowed; its
  authors ask those who do so in a commercial product to subscribe.

Checked the same way as the first round: the 28 browser checks, the build,
and each screen looked at. Not checked beyond that list, as before; and the
owner has not yet seen this round.

Looked at and left alone: Vercel's list asks for title case on buttons and
headings; this app uses sentence case throughout and keeps it. Issues 21
and 22 above are what the same list found that was not done.

## How changes were tested

The unit tests are in the repository (`make test`). So, since 2026-10-12,
are the end-to-end checks, which before that were written in a temporary
folder and lost twice.

- **The stand-in for the model**, [`tools/standin`](../tools/standin/README.md).
  A Go program, a module of its own so that it is never part of the
  product's build, listening on `:9099` and answering
  `POST /chat/completions` in DeepSeek's format, streamed or whole. It tells
  the assistant from a coding agent by the tools a request offers
  (`code_on_machine` means the assistant; `bash` or `write` means OpenCode),
  and works out how far things have got from the conversation each request
  carries, so it remembers nothing and can be restarted at any time.
  - As the assistant it asks one question, proposes a shop (a storefront, an
    orders API, a database and CI, on machines `web`, `api`, `db` and `ci`),
    proposes again if asked for a change, and once approved creates the
    machines, gives each agent its task, shows previews and sums up.
  - As an agent it does real work with OpenCode's own tools. The database's
    agent creates a PostgreSQL cluster and registers it as a service. The
    API's agent writes a small server, registers it, is told it has no API
    description, adds `/openapi.json`, and registers it again. The
    storefront's agent asks the API's agent a question and writes a page
    that calls `/api`. The CI agent clones the API's repository and checks
    it.
  - It refuses a request whose tool schemas are not valid, as the real API
    does. The stand-in before it accepted anything, which is how
    `"required": null` got as far as the real model.
- **The server pointed at it**, which needs no code change:
  `make run-standin` (the environment wins over `.env`, so the real key
  there is not used).
- **A browser script**, [`tools/uicheck/build.mjs`](../tools/uicheck/README.md)
  (Playwright, headless Chromium). It signs up a new user through the form
  and follows a whole build: the empty state of each tab, the question, the
  proposal on the graph, a change asked for, approval, the dashboard while
  machines are created and agents work, that the API's agent was told about
  its missing description and that the restarted program was the one
  checked, one agent's question to another, the page live in the Frontend
  tab with an order placed from it, the API listed in the Backend tab and
  its endpoints called with a path parameter, a query, a body and
  credentials, the tabs keeping their state, what the port-request endpoint
  refuses, hiding the panel, a second chat, and a phone. It asks the server
  which model it has and stops unless the answer is `standin`.
- **Test users** are named `tmpcheck-<something>@example.com` and are removed
  afterwards, in this order: for each of their instances
  `docker rm -f coder-<instance id>` and `docker volume rm coder-<instance id>`;
  `docker network rm coder-user-<user id>`; then their rows in `chats`,
  `instances` and `users` (a user cannot be deleted while it has instances).
  `node tools/uicheck/clean.mjs <email>` does exactly that, and `build.mjs`
  runs it on its own account before and after.
- **On this computer** (WSL2) headless Chromium lacks three system
  libraries. Without root they can be had with
  `apt-get download libnspr4 libnss3 libasound2t64`, unpacked with
  `dpkg -x` into a folder that is then put on `LD_LIBRARY_PATH`. The folder
  used so far was a temporary one each time; it has to be made again after
  a restart.

**`tools/uicheck`'s other scripts** (2026-10-06) look at the frontend
without any of the above: sample chats, machines and agents written straight
into the database for one test account, pictures of any screen, and checks
of how the pages behave. Its [README](../tools/uicheck/README.md) has the
commands. They show what a chat looks like, not that the assistant can
build one.

Do not point tests at the real model without asking the owner: it spends
their credits. Asking the provider which models a key has
(`GET $LLM_BASE_URL/models`) is free.

## The link: step 1 of the harness

Built on 2026-10-12. [BACKEND.md, section 25](BACKEND.md#25-the-link) says
how it works. What was checked, and how:

- **In unit tests**: the hub, the gRPC server (over an in-memory
  connection, spoken to as a daemon would), the daemon's environment, and in
  Rust the reconnect waits, the handshake and the wording of errors.
- **With `linkd` run directly on this computer**, against the real server,
  as a test machine that was only a row in the database (no container):
  - it attaches, and the server logs which machine;
  - a wrong token is refused, and tried again after 1, 2 and 4 seconds;
  - the server stopping is noticed at once, and the link is back within a
    second of the server returning;
  - killed with no goodbye, it is detached at once;
  - left alone for 75 seconds, nothing drops;
  - with the daemon frozen, the server detaches it after 24 seconds; with
    the server frozen, the daemon notices after 30. Each comes back by
    itself.
- **In one real machine**, created through the API from the rebuilt image:
  - the link attaches in the same second as the tunnel;
  - the daemon killed inside the machine is started again by `guestd` and
    attaches a second later;
  - the server restarted: tunnel and link both back within two seconds;
  - the machine stopped: the link is detached and the daemon exits cleanly;
  - a machine never told of links (as one made before them) starts none and
    says so.
- **Measured in that machine**, after two and a half minutes: `linkd` at
  988 KB of memory on one thread, from a static binary of 973 KB. Beside it,
  `guestd` 13 MB, code-server 155 MB, OpenCode 310 MB.

Not checked: a whole build with links attached (the end-to-end check has
not been run since), more than one machine at a time, a machine on any
stack but the standard one (the other four images were rebuilt but nothing
was started from them), and anything over a network that is not this
computer's Docker bridge.

## The mesh: step 2a of the harness

Built on 2026-10-12, after the link.
[BACKEND.md, section 25](BACKEND.md#machines-finding-each-other) says how it
works. Machines that an approved design connects open a stream to each
other directly, prove with their mesh's key that they belong together, and
tell the server.

- **In tests**: which machines a design makes peers and what each is told
  (Go); the proof and the table of peers (Rust); and two whole daemons with
  a stand-in control plane, in one process over real connections, which
  link with the same key and do not with different ones.
- **In two real machines**, `web` and `api`, under a test account with an
  approved design written into the database (no model, no agents working):
  - `web` dialled `api` at `http://api:7600` and both reported the other
    within a second of `api` starting;
  - `api` stopped: `web` reported no peers at once. Started again: linked
    again by themselves;
  - the server restarted three times, which gives new keys: both proven
    again after 3, 3 and 7 seconds.
- **Measured** in `web`: `linkd` at 1.3 MB of memory, 1.25 MB on disk.
- **A bug found and fixed on the way**: the server refused a link whose
  first report arrived before its first pong, which made links take up to
  half a minute to return after a restart.

Not checked: more than two machines, a whole build with the mesh in place
(the end-to-end check has still not been run since the link was added), and
a design changing while machines are linked.

## What was noticed on 2026-10-12

While the end-to-end check was being rebuilt. None of it has been acted on.

- **OpenCode uses most of a small machine's memory.** Measured with `ps`
  inside the four machines of a build: 330 to 340 MB before it is given
  anything to do, 630 to 925 MB while it works, and 530 to 710 MB afterwards
  (it does not give the memory back). A small machine has 1 GB; the
  database's stood at 96% of its limit while its agent worked. `guestd` is
  about 15 MB and code-server about 150 MB.
- **A small machine is slow under that load.** Creating a PostgreSQL cluster
  took about fifty seconds, and an agent's first step on a machine came up
  to forty seconds after its task was given.
- **Stopping idle machines can cross with removing them.** A test account
  removed just as its machines were being stopped for idleness waited two
  minutes for the stops to finish. Harmless, but it looks like a hang.
- **OpenCode's shell tool takes no description** in the version installed
  (1.18.34): `command`, `timeout` and `workdir` only. The stand-in sends a
  tool only the arguments its schema lists.
- **A machine started through the API alone is stopped within seconds.**
  The idle check counts a machine nobody has been seen on as idle since the
  server started, not since the machine did
  (`internal/presence/tracker.go`, `IdleFor`). With the app open this never
  shows, because the owner is present. A machine created with `curl` thirteen
  seconds earlier was stopped "after 3m30s with nobody connected". The link
  was checked with `IDLE_STOP_AFTER=0`.
- **Two small things on a phone**, in the picture the check takes of the
  panel there: the last tab's name runs into the button that closes the
  panel, and the diagram's zoom buttons sit over the lowest component,
  covering its machine's name and its "Open IDE" button.

## What to do next

1. **Restart the server** to pick up step 1, and, with the owner's go-ahead
   to spend credits, build the same request again (a bicycle shop on Go
   microservices) to see whether the agents start together and how long it
   takes against the 41 minutes of 2026-10-18.
2. **Step 2 of the plan** in [Agents at once, and doing the whole
   job](#agents-at-once-and-doing-the-whole-job-2026-10-18): DeepSeek
   Harness over ACP, then the trial (asking first).
3. Steps 3 to 6 of that plan, in order.
4. **The owner to look at the chat as a room** (seeded with
   `node tools/uicheck/seed.mjs`, the "Crypto brokerage" chat), and say
   whether the choices listed under [The chat as a
   room](#the-chat-as-a-room-2026-10-15) are right.
5. Close the database off from machines (issue 1) and fix the sign-out on a
   database error (issue 4); both are small, and issue 1 must be done before
   anything is on the internet.
6. Decide how machines belong to a project (issue 3).
7. Then the limits (issue 5) and the rest, in the order of the list above.

Further out: platform-run CI, per-user model keys and usage limits, and a
rented server once Fly's memory price outweighs its convenience. See
[ARCHITECTURE.md](ARCHITECTURE.md#planned).
