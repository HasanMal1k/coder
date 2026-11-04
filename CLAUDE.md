# coder

A cloud development platform: each user has persistent Linux machines with a
terminal and VS Code in the browser, and an AI assistant that designs an
application with them and builds it across those machines. A Go backend
(`cmd/`, `internal/`), a Next.js frontend (`web/`), a small Rust daemon that
runs on every machine (`link/`), machines as local Docker containers. A commercial product the owner intends to sell, not a portfolio
piece. Its two ideas: people build software without owning the compute it
needs, and a distributed harness (a parent assistant in the control plane,
and a coding agent with its own harness on every machine).

## Read first

- [docs/STATUS.md](docs/STATUS.md): where things stand. What was done last,
  what is known to be wrong, how changes were tested, what to do next.
  **Start here, and update it at the end of the session.**
- [docs/MVP.md](docs/MVP.md): what the product is, who it is for, and what
  the first version to sell must do. The target the work is aimed at.
- [docs/HARNESS.md](docs/HARNESS.md): the design of the distributed harness,
  and how far it has been built. The work in progress.
- [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md): the design in overview.
- [docs/BACKEND.md](docs/BACKEND.md): the backend in detail. Long; read the
  section you need (it has a table of contents, and section 24 says where to
  change what).
- [docs/API.md](docs/API.md): the HTTP and WebSocket API.
- [web/README.md](web/README.md): the frontend. [web/AGENTS.md](web/AGENTS.md)
  matters too: this version of Next.js differs from older ones.

## Commands

```bash
make db        # Postgres and migrations
make run       # the server, :8080, and :8081 for machines' links
make web       # the frontend, http://coder.localhost:3000 (use this address, not localhost)
make test      # Go tests with the race detector, and the Rust daemon's
make link      # the link daemon (Rust), built here for the machine image
make standin   # a stand-in for the model, on :9099 (tools/standin)
make run-standin   # the server pointed at the stand-in, in place of `make run`
make image     # the machine image; `make images` for every stack
make proto     # after editing guest.proto or internal/linkapi/link.proto
```

The end-to-end check, with `make standin`, `make run-standin` and `make web`
running: `node tools/uicheck/build.mjs` builds a whole project in a real
browser on real machines, and removes them afterwards. It takes about six
minutes, and refuses to run against anything but the stand-in.

Rust checks, from `link/`: `cargo fmt --check`,
`cargo clippy --all-targets -- -D warnings`, `cargo test`.

Frontend checks, from `web/`:

```bash
npx prettier --print-width 100 --check "src/**/*.{ts,tsx,css}"
npm run lint
npx tsc --noEmit
npx next build
```

## Rules for this project

These are the owner's, given over several sessions.

- **No mock, placeholder or demo logic in the product.** A test double (such
  as a stand-in for the model) lives apart from the product's packages and
  is never part of its build.
- **Dependency rule.** Transports (`httpapi`, `wsgateway`, `portproxy`,
  `llmgateway`, `teamapi`, `linkgw`) call the domain (`auth`, `instance`,
  `assistant`), which calls infrastructure (`sandbox`, `tunnel`, `link`,
  `store`). Domain packages never import transport types.
- **Commit only when asked.**
- **Verify for real.** Run the thing and look, in a browser where it is a
  page. Say what was checked and what was not.
- **Do not call the real model to test without asking.** It spends the
  owner's credits. Listing a key's models is free. Never print the key.
  Test against the stand-in (`tools/standin`).
- **The model's key never goes into a machine.** Agents reach the model
  through the gateway, as the machine.
- **Test accounts** are named `tmpcheck-<something>@example.com` and are
  removed afterwards with their machines, volumes and network
  ([docs/STATUS.md](docs/STATUS.md#how-changes-were-tested) says how).
  `tools/uicheck` makes and removes one for looking at the frontend.
- **Frontend formatting** is Prettier at 100 columns. No config file says
  so; always pass `--print-width 100`.
- **How it looks.** Dark gray with one orange (the owner's words on
  2026-10-06, of the black, outlined version before it: "too boxy ... from
  black, make it a bit gray, and add another orange tone ... make it
  clean"). Surfaces are told apart by tone first and a faint line second;
  corners are soft but never pills; orange is for the one thing to press,
  for focus, and for what is live; green, amber and red are for status
  only. The architecture graph is the showpiece: technology logos on its
  nodes, plain grey lines with one small dot hinting which way requests go, no library
  mark. Colours are tokens in `web/src/app/globals.css`. There is no light
  theme; do not add one. For any change to the interface, use the two skills in
  `.claude/skills/` (`frontend-design` to design it, `web-design-guidelines`
  to check it); where they disagree with this rule, this rule wins.
  [web/README.md](web/README.md#design) has the rest.
- **Keep the documents true.** A change to behaviour updates the document
  that describes it, in the same piece of work.
- **Explain plainly**, and do not race ahead of what was asked.

## Decisions already made

Not to be reopened without the owner raising them.

- Sign-in tokens never expire by default (`ACCESS_TOKEN_TTL=0`). Auth stays;
  only the lifetime changed.
- The home page is a chat. There is no dashboard page; the machines list is
  at `/machines`, from the account menu. The panel beside a chat has four
  tabs: Graph, Frontend, Backend, Dashboard.
- The assistant designs first: a few questions, a proposed architecture,
  approval, then building.
- Every backend serves an OpenAPI document at `/openapi.json`. The platform
  checks for it and the Backend tab is drawn from it.
- The model is DeepSeek, through its OpenAI-compatible API. Talking to it
  stays in the Go server, not in a separate service.
- Docker on one host is what runs today. For testing on real hardware, and
  for the wall between users, machines are to run on Fly.io (the owner,
  2026-10-18: the cheapest dedicated server was too dear to test on). Fly's
  machines are small virtual machines billed by the second. A rented server
  (2026-10-12's plan, [docs/MVP.md](docs/MVP.md#isolation-cheaply)) is for
  when Fly's memory price outweighs that; `sandbox.Provisioner` keeps the
  move contained. gVisor is dropped.
- The coding agent (2026-10-18): OpenCode is to be replaced by DeepSeek
  Harness (`dsh`, MIT), driven over ACP, the Agent Client Protocol, so that
  the harness is a setting and not a rewrite. No harness of our own is to be
  written. The context window and longest reply are the model's own.
- The distributed harness (2026-10-12, [docs/HARNESS.md](docs/HARNESS.md)):
  Rust is for communication only;
  machines talk to each other directly, not through the control plane; and
  a machine's agent has the say on its own component, with the parent
  listening to it.
- Docker is left out wherever it is a choice (2026-10-12, for the memory it
  takes on the owner's computer). The link daemon is built on the host with
  Cargo, not in a stage of the machine image. Machines themselves are still
  containers; when checking something, start as few as will show it.

## Things that catch people out

- A machine keeps the `guestd`, `linkd` and tools of the image it was
  created from. After changing `internal/guest`, `link/` or the image,
  rebuild (`make images`) and create new machines to see the change.
- `make image` builds the link daemon first, on this computer, which needs
  Rust with the musl target (`rustup target add x86_64-unknown-linux-musl`)
  and `protoc`. The built daemon is at `images/workspace/bin/linkd`, which
  git ignores.
- `link.proto` has two readers. `make proto` regenerates the Go side; the
  Rust side is regenerated whenever `linkd` is built. Change both ends'
  handling of a frame together.
- A machine started through the API with the app closed is stopped at the
  next idle check. Start the server with `IDLE_STOP_AFTER=0` when checking
  something on a machine without a browser open.
- The assistant can only build a design whose stack images exist on the
  server (`make images`).
- Open the frontend at `coder.localhost:3000`. The editor is framed from
  `<port>-<id>.coder.localhost:8080`, and needs the two to be one site.
- Everything about who is connected, which runs are in progress and which
  agents are working is held in the server's memory. Restarting the server
  ends runs in progress; they are marked as interrupted at the next start.
