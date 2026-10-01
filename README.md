# coder

A cloud development platform: every user gets their own persistent Linux
machines, reachable from a terminal in the browser.

- [web/](web/) — the frontend (Next.js)
- [docs/API.md](docs/API.md) — the HTTP and WebSocket API
- [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) — how it fits together and what comes next

## Running it locally

You need Go 1.26+, Node.js 20+, Docker with the Compose plugin, and `make`.

```bash
cp .env.example .env      # then set JWT_SECRET to a long random string
make db                   # Postgres + migrations
make image                # the machine image users boot (takes a few minutes the first time)
make run                  # the server, on :8080
```

In a second terminal, start the frontend and open http://localhost:3000:

```bash
make web
```

Use `localhost`, not `127.0.0.1`: the server only accepts browsers from the
origins listed in `CORS_ALLOWED_ORIGINS`.

### Without the frontend

The same things with `curl` and `jq`:

```bash
# Sign up and keep the token
TOKEN=$(curl -s localhost:8080/register -d '{"email":"me@example.com","password":"a long password","display_name":"Me","organization":"none"}' | jq -r .token)

# Create and start a machine
ID=$(curl -s localhost:8080/instances -H "Authorization: Bearer $TOKEN" -d '{"name":"dev"}' | jq -r .id)
curl -s -X POST localhost:8080/instances/$ID/start -H "Authorization: Bearer $TOKEN"

# See it
curl -s localhost:8080/instances -H "Authorization: Bearer $TOKEN"
docker ps --filter label=dev.coder.instance-id
```

The terminal is a WebSocket at `/instances/{id}/terminal`; see
[docs/API.md](docs/API.md#terminal) for the protocol and an xterm.js example.

## Layout

```
cmd/server            the control plane
cmd/guestd            the daemon inside every machine
internal/auth         users and tokens
internal/instance     machine lifecycle and ownership
internal/sandbox      where machines run (docker/ today)
internal/tunnel       connections from machines to the control plane
internal/guest        guestd's terminal server
internal/guestapi     protocol between control plane and guestd
internal/httpapi      REST handlers
internal/wsgateway    WebSocket handlers
internal/store        Postgres
web                   the frontend (Next.js)
images/workspace      the machine image
migrations            database schema
```

## Development

```bash
make test     # unit tests, with the race detector
make image    # rebuild after changing anything guestd uses
make proto    # regenerate internal/guestapi after editing guest.proto
```

Machines created by an older image keep running the old `guestd`; delete and
recreate them to pick up a new one.
