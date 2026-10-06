# coder

A cloud development platform: each user has persistent Linux machines with a
terminal and VS Code in the browser, and an AI assistant that designs an
application with them and builds it across those machines. A Go backend
(`cmd/`, `internal/`), a Next.js frontend (`web/`), machines as local Docker
containers. A portfolio project, built to production standard.

## Read first

- [docs/STATUS.md](docs/STATUS.md): where things stand. What was done last,
  what is known to be wrong, how changes were tested, what to do next.
  **Start here, and update it at the end of the session.**
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
make run       # the server, :8080
make web       # the frontend, http://coder.localhost:3000 (use this address, not localhost)
make test      # Go tests, with the race detector
make image     # the machine image; `make images` for every stack
make proto     # after editing internal/guestapi/guest.proto
```

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
  `llmgateway`, `teamapi`) call the domain (`auth`, `instance`, `assistant`),
  which calls infrastructure (`sandbox`, `tunnel`, `store`). Domain packages
  never import transport types.
- **Commit only when asked.**
- **Verify for real.** Run the thing and look, in a browser where it is a
  page. Say what was checked and what was not.
- **Do not call the real model to test without asking.** It spends the
  owner's credits. Listing a key's models is free. Never print the key.
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
- A Fly.io provider is wanted and deferred for lack of funds. Docker on one
  host is what runs today.

## Things that catch people out

- A machine keeps the `guestd` and tools of the image it was created from.
  After changing `internal/guest` or the image, rebuild (`make images`) and
  create new machines to see the change.
- The assistant can only build a design whose stack images exist on the
  server (`make images`).
- Open the frontend at `coder.localhost:3000`. The editor is framed from
  `<port>-<id>.coder.localhost:8080`, and needs the two to be one site.
- Everything about who is connected, which runs are in progress and which
  agents are working is held in the server's memory. Restarting the server
  ends runs in progress; they are marked as interrupted at the next start.
