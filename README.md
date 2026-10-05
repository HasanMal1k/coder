# coder

A cloud development platform: every user gets their own persistent Linux
machines, each with VS Code and a terminal in the browser, and an AI
assistant that designs an application with them and builds it across those
machines: "a React frontend on one machine, an ASP.NET backend on another,
and PostgreSQL".

You describe what you want in a chat. The assistant asks what it needs to,
proposes an architecture as a diagram, and builds it once you approve.
Beside the chat you watch the diagram come alive, see the frontend's page,
read and try the backend's API, and follow the machines and agents.

- [docs/STATUS.md](docs/STATUS.md) — where the work stands, what is known to be wrong, what is next
- [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) — overview and roadmap
- [docs/BACKEND.md](docs/BACKEND.md) — how the backend works, in detail
- [docs/API.md](docs/API.md) — the HTTP and WebSocket API
- [web/](web/) — the frontend (Next.js)
- [CLAUDE.md](CLAUDE.md) — the project's rules and decisions, for whoever works on it next

## Running it locally

You need Go 1.26+, Node.js 20+, Docker with the Compose plugin, and `make`.

```bash
cp .env.example .env      # then set JWT_SECRET to a long random string
make db                   # Postgres + migrations
make image                # the machine image users boot (takes a few minutes the first time)
make images               # also the Go, C++, .NET and data images (several GB); the assistant needs these
make run                  # the server, on :8080
```

In a second terminal, start the frontend:

```bash
make web
```

Then open **http://coder.localhost:3000**. Browsers resolve every `*.localhost`
name to your own computer, so this needs no setup.

Use that address rather than plain `localhost:3000`. Each machine's editor is
served at `<port>-<id>.coder.localhost:8080`, and the editor only works fully
when the app and the machines are on the same site (`coder.localhost`).

### The assistant

The assistant needs a key for its model. Put a [DeepSeek](https://platform.deepseek.com)
key in `.env` and restart the server:

```bash
DEEPSEEK_API_KEY=sk-...
```

Set the model too. The names a key can use differ; ask the provider:

```bash
curl -s https://api.deepseek.com/v1/models -H "Authorization: Bearer $DEEPSEEK_API_KEY"
```

```bash
LLM_MODEL=deepseek-v4-pro
```

Without a key, everything else works and the chat says the assistant isn't
set up. Any service that speaks the OpenAI chat-completions protocol and supports
tool calling can be used instead, with `LLM_BASE_URL` and `LLM_MODEL`.

The assistant gives each part of a design a machine with the right toolchain
already on it, so it needs the stack images (`make images`): a design that
asks for one that hasn't been built has that machine refused. It works on
machines created from the current images; older machines keep the tools and
daemon they were made with.

A machine stops by itself two minutes after you close the app, unless its
terminal or editor is still open somewhere; your files are kept. Change the
delay with `IDLE_STOP_AFTER` in `.env` (`0` turns it off).

### Without the frontend

The same things with `curl` and `jq`:

```bash
# Sign up and keep the token
TOKEN=$(curl -s localhost:8080/register -d '{"email":"me@example.com","password":"a long password","display_name":"Me","organization":"none"}' | jq -r .token)

# Create and start a machine
ID=$(curl -s localhost:8080/instances -H "Authorization: Bearer $TOKEN" -d '{"name":"dev"}' | jq -r .id)
curl -s -X POST localhost:8080/instances/$ID/start -H "Authorization: Bearer $TOKEN"

# See it. Without the app open nothing is "connected", so the machine is
# stopped after IDLE_STOP_AFTER; set that to 0 to experiment this way.
curl -s localhost:8080/instances -H "Authorization: Bearer $TOKEN"
docker ps --filter label=dev.coder.instance-id
```

The terminal is a WebSocket at `/instances/{id}/terminal`; see
[docs/API.md](docs/API.md#terminal) for the protocol and an xterm.js example.

## Layout

```
cmd/server            the control plane
cmd/guestd            the daemon inside every machine
internal/config       settings, from the environment and .env
internal/auth         users and tokens
internal/instance     machine lifecycle and ownership
internal/assistant    the AI assistant: chats, its tools, designs, agents, services
internal/sandbox      where machines run (docker/ today)
internal/tunnel       connections from machines to the control plane
internal/portproxy    serves machines' ports (the editor) to the browser
internal/llmgateway   lets coding agents use the model without its key
internal/teamapi      what machines ask the server: an MCP server for agents, and each machine's record
internal/presence     who is connected, for stopping idle machines
internal/guest        guestd: terminals, port forwarding, the editor, the coding agent, services
internal/guestapi     protocol between control plane and guestd
internal/httpapi      REST handlers
internal/wsgateway    WebSocket handlers
internal/middleware   checks the access token on protected routes
internal/store        Postgres
web                   the frontend (Next.js)
images/workspace      the machine images
migrations            database schema
docs                  status, architecture, backend, API
```

## Development

```bash
make test     # unit tests, with the race detector
make images   # rebuild after changing anything guestd uses
make proto    # regenerate internal/guestapi after editing guest.proto
```

Frontend checks are in [web/README.md](web/README.md#checks).

Machines created by an older image keep running the old `guestd` and tools;
delete and recreate them to pick up new ones.

The unit tests cover the logic. The whole flow (a browser, real machines,
the real coding agent, a stand-in for the model) has been checked by hand
with tooling that is not in the repository yet;
[docs/STATUS.md](docs/STATUS.md#how-changes-were-tested) describes it.
