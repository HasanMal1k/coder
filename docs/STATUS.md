# Where things stand

Last updated: 2026-10-06.

This is the page to read first when picking the project up again. It says
what exists, what was done most recently, what is known to be wrong, and how
changes have been checked. Update it at the end of every working session.
[ARCHITECTURE.md](ARCHITECTURE.md) is the overview of the design;
[BACKEND.md](BACKEND.md) explains the backend in detail;
[API.md](API.md) is the HTTP and WebSocket API;
[../web/README.md](../web/README.md) covers the frontend.

## State of the repository

- Branch `instances-backend`. The last commit is `08709ab` ("changes", by
  the owner on 2026-10-05), which holds everything built up to then: the
  assistant, agents, designs, services, the chat-first frontend, the panel
  and its tabs, and the documents.
- **Uncommitted: the frontend pass of 2026-10-06** (see below): some twenty
  changed files under `web/src`, `internal/portproxy/proxy.go`, the new
  `tools/uicheck/`, and this page, `CLAUDE.md` and `web/README.md`.
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
  Dashboard). Dark gray with an orange accent.
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
| 2026-10-06 | `learn/` added, for the owner to learn Go from this codebase: five projects to write by hand, each tied to the packages it mirrors (`learn/README.md`), interview notes (`learn/INTERVIEW.md`), and a worked Docker and Compose example that was built and run (`learn/docker-example/`). The example is a Go module of its own, so it is not part of the product's build or of `make test`; `learn` was added to `.dockerignore`. No product code changed. |
| 2026-10-06 | After the owner looked at it: restyled from black and outlined to dark gray with an orange accent and softer shapes, and the graph redone (logos, glowing lines, no library mark). See [the second round](#the-second-round-the-same-day). |
| 2026-10-06 | A pass over the frontend, using two skills found for it and installed in `.claude/skills/` (`frontend-design`, Anthropic's; `web-design-guidelines`, Vercel's). What changed is listed under [The frontend pass](#the-frontend-pass-of-2026-10-06). |

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
23). Paths are from the repository root. Two have been dealt with since and
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
    them), and the end-to-end tooling for the assistant is still not in the
    repository (see below). `tools/uicheck` now is: browser checks of the
    frontend against sample data, without the model.
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

**`tools/uicheck`** (2026-10-06, in the repository) is the smaller thing
that looks at the frontend without any of the above. It writes sample chats,
machines and agents for one test account straight into the database, takes
pictures of any screen, drives the app and reports what held, and removes
the account again. Its [README](../tools/uicheck/README.md) has the
commands. It does not replace the stand-in: it shows what a chat looks
like, not that the assistant can build one. While it was used the server
was started with `LLM_BASE_URL` pointing at a port nothing listens on, so
that nothing could reach the real model by accident.

Do not point tests at the real model without asking the owner: it spends
their credits. Asking the provider which models a key has
(`GET $LLM_BASE_URL/models`) is free.

## What to do next

1. Look at the frontend pass in a browser and commit it, or say what to
   change.
2. Close the database off from machines (issue 1) and fix the sign-out on a
   database error (issue 4); both are small.
3. Decide how machines belong to a project (issue 3) before starting a
   second chat that builds something.
4. Rebuild the stand-in for the model inside the repository, as a Go module
   of its own so that it is never part of the product's build, next to
   `tools/uicheck`.
5. Then the limits (issue 5) and the rest, in the order above.

Further out, and unchanged: a Fly.io provider (waiting on funds), platform-run
CI, per-user model keys and usage limits. See
[ARCHITECTURE.md](ARCHITECTURE.md#planned).
