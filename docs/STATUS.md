# Where things stand

Last updated: 2026-10-05.

This is the page to read first when picking the project up again. It says
what exists, what was done most recently, what is known to be wrong, and how
changes have been checked. Update it at the end of every working session.
[ARCHITECTURE.md](ARCHITECTURE.md) is the overview of the design;
[BACKEND.md](BACKEND.md) explains the backend in detail;
[API.md](API.md) is the HTTP and WebSocket API;
[../web/README.md](../web/README.md) covers the frontend.

## State of the repository

- Branch `instances-backend`. The last commit is `24ac9be` ("Embed VS Code
  (code-server) in each machine").
- **Everything built since then is uncommitted**: around fifty changed files
  and sixty new paths, some 12,000 added lines (`git status` has the exact
  list). That is the assistant, agents, designs, services, the chat-first
  frontend, the panel and its tabs, and all of the documentation of them,
  this page included. Committing it is the first thing to do.
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

The machine images (`coder-workspace:dev` and the `go`, `cpp`, `dotnet` and
`data` ones) were last built late on 2026-10-04. Rebuild them with
`make images` after changing anything `guestd` runs; machines made from an
older image keep the older `guestd`.

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
  Dashboard). Black only; styled after Vercel and v0.
- Every backend must describe its API with an OpenAPI document at
  `/openapi.json`; the platform checks for it and the Backend tab is drawn
  from it.

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

## What has been checked, and what has not

- **Unit tests** (`make test`) pass. Coverage is high where the logic is:
  `assistant` 85%, `portproxy` 93%, `teamapi` 84%, `tunnel` 82%,
  `llmgateway` 77%. It is low or absent for `store` (no tests), `httpapi`
  (1%), `wsgateway`, `sandbox/docker`, `auth` (18%), `instance` (27%) and
  `guest` (37%).
- **End to end**, the whole flow was run in a real browser against real
  machines and the real OpenCode, with a scripted stand-in for the model:
  66 checks on 2026-10-04, all passing. See [How changes were
  tested](#how-changes-were-tested). The frontend has no tests in the
  repository.
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

From the review of 2026-10-05. Paths are from the repository root. None has
been fixed.

### Fix first

1. **The platform's database is reachable from inside every machine.**
   Postgres is published on all interfaces with the password `coder`
   (`docker-compose.yml`, the `ports` entry), and machines can reach the host
   through `host.docker.internal`
   (`internal/sandbox/docker/docker.go`, `ExtraHosts`). Confirmed from inside
   a machine. Anything running on a machine could read or change every
   user's data. Publish it on `127.0.0.1` only and change the password.
2. **Nothing since `24ac9be` is committed.**
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

15. No frontend tests in the repository, and the end-to-end tooling is not
    in it either (see below). `web/src/lib/openapi.ts` has no unit tests.
16. The port proxy's error pages still use the old violet and light colours
    (`internal/portproxy/proxy.go`, `errorTemplate`).
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

## How changes were tested

The unit tests are in the repository. The end-to-end checks were not: they
were written in a temporary folder that was emptied when the computer
restarted, more than once. **They no longer exist.** What they were, so they
can be rebuilt, this time inside the repository:

- **A stand-in for the model.** A small Go program listening on `:9099` that
  answers `POST /chat/completions` in DeepSeek's format, streamed or whole.
  It tells the assistant from a coding agent by the tools a request offers
  (`code_on_machine` means the assistant; `write` or `bash` means OpenCode)
  and answers from a script, by counting how far the conversation has got.
  As the assistant it asks one question, proposes a shop (a storefront, an
  orders API, a database and CI, on machines `web`, `api`, `db` and `ci`),
  proposes again if asked for a change, and once approved creates the
  machines, gives each agent its task, shows a preview and sums up. As an
  agent it does real work with OpenCode's own tools: the database agent
  initialises PostgreSQL and registers it as a service; the API agent writes
  a small server, registers it, is told it has no API description, adds
  `/openapi.json`, and registers it again; the frontend agent asks the API
  agent a question and writes a page that calls `/api`; the CI agent clones
  the API's repository and checks it.
- **The server pointed at it**, which needs no code change:
  `DEEPSEEK_API_KEY=mock-key LLM_BASE_URL=http://localhost:9099/v1 LLM_MODEL=deepseek-chat go run ./cmd/server`
  (the environment wins over `.env`).
- **A browser script** (Playwright, headless Chromium) that signs up a new
  user and drives the app: the empty state of each tab, the question, the
  proposal on the graph, approval, the dashboard while machines are created
  and agents work, that the API agent was told about its missing description
  and that the restarted program was the one checked, the page live in the
  Frontend tab, the API listed in the Backend tab and its endpoints called
  with path, query, body and credentials, the tabs keeping their state,
  what the port-request endpoint allows and refuses, a second chat, hiding
  the panel, and a small screen.
- **Test users** are named `tmpcheck-<something>@example.com` and are removed
  afterwards, in this order: for each of their instances
  `docker rm -f coder-<instance id>` and `docker volume rm coder-<instance id>`;
  `docker network rm coder-user-<user id>`; then their rows in `chats`,
  `instances` and `users` (a user cannot be deleted while it has instances).
- **On this computer** (WSL2) headless Chromium lacked three system
  libraries. Without root they can be had with
  `apt-get download libnspr4 libnss3 libasound2t64`, unpacked with
  `dpkg -x` into a folder that is then put on `LD_LIBRARY_PATH`.

Do not point tests at the real model without asking the owner: it spends
their credits. Asking the provider which models a key has
(`GET $LLM_BASE_URL/models`) is free.

## What to do next

1. Commit what is there.
2. Close the database off from machines (issue 1) and fix the sign-out on a
   database error (issue 4); both are small.
3. Decide how machines belong to a project (issue 3) before starting a
   second chat that builds something.
4. Put the end-to-end tooling into the repository, with the stand-in as a
   Go module of its own so that it is never part of the product's build.
5. Then the limits (issue 5) and the rest, in the order above.

Further out, and unchanged: a Fly.io provider (waiting on funds), platform-run
CI, per-user model keys and usage limits. See
[ARCHITECTURE.md](ARCHITECTURE.md#planned).
