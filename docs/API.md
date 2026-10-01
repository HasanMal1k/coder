# API

Everything a frontend needs to build the dashboard and the terminal.

Base URL in development: `http://localhost:8080`.

- Request and response bodies are JSON.
- Errors always look like `{"error": "message"}`.
- Endpoints marked 🔒 need the header `Authorization: Bearer <token>`. A
  missing, invalid or expired token gets `401`.
- Tokens last 15 minutes. There is no refresh yet: on a `401`, send the user
  back to login.

To call the API from a page on another origin (e.g. a dev server on
`http://localhost:5173`), add that origin to `CORS_ALLOWED_ORIGINS` in `.env`.

## Auth

### `POST /register`

```json
{ "email": "a@example.com", "password": "...", "display_name": "Ada", "organization": "Acme" }
```

`201`:

```json
{
  "token": "eyJ...",
  "user": { "id": "uuid", "email": "a@example.com", "display_name": "Ada", "organization": "Acme", "subscription": "free" }
}
```

`409` if the email is already registered.

### `POST /login`

```json
{ "email": "a@example.com", "password": "..." }
```

`200` with the same body as register. `401` for a wrong email or password.

### `GET /me` 🔒

`200` with the `user` object.

## Instances

An instance is one machine belonging to the logged-in user.

```json
{
  "id": "5357ebb3-d8d9-4b33-b0bf-d730a78740d5",
  "name": "my-project",
  "status": "running",
  "cpus": 1,
  "memory_mb": 1024,
  "disk_gb": 10,
  "created_at": "2026-10-01T13:14:26.831987+05:00",
  "updated_at": "2026-10-01T13:14:27.056019+05:00"
}
```

### Status

| Status | Meaning | What the user can do |
|---|---|---|
| `stopped` | Exists, not running. Files are kept. | Start, delete |
| `running` | Running. | Open a terminal, stop, delete |
| `creating`, `starting`, `stopping`, `deleting` | An operation is in progress. | Wait |
| `error` | The last operation failed. | Start or stop to retry, or delete |

Start, stop and create respond only once the operation has finished, so the
status in the response is the final one. The in-progress statuses are only
seen by other requests made in the meantime (another tab, a list refresh).

### Endpoints

| Method and path | Body | Success | Errors |
|---|---|---|---|
| `GET /instances` 🔒 | – | `200` `{"instances": [...]}`, oldest first; `[]` when there are none | |
| `POST /instances` 🔒 | `{"name": "my-project"}` | `201` instance, status `stopped` | `400` invalid name, `409` name already used |
| `GET /instances/{id}` 🔒 | – | `200` instance | `404` |
| `POST /instances/{id}/start` 🔒 | – | `200` instance, status `running` | `404`, `409` busy |
| `POST /instances/{id}/stop` 🔒 | – | `200` instance, status `stopped` | `404`, `409` busy |
| `DELETE /instances/{id}` 🔒 | – | `204`, no body | `404`, `409` busy |

- **Names** are 1–40 characters: lowercase letters, digits and hyphens,
  starting and ending with a letter or digit. They are unique per user.
- **`404`** is also what you get for an instance that belongs to someone else.
- **`409` busy** means another operation on that instance is still running.
- Starting a running instance, or stopping a stopped one, succeeds and
  changes nothing.
- Deleting removes the machine and all its files permanently.

## Terminal

`GET /instances/{id}/terminal` is a WebSocket. Each connection is its own
shell; open as many as you like per instance.

Browsers can't set headers on a WebSocket, so the token is sent as the first
message instead of an `Authorization` header.

### Messages

**Browser → server**

| Kind | Content | When |
|---|---|---|
| Text | `{"type":"auth","token":"<access token>","cols":80,"rows":24}` | Must be the first message, within 10 seconds |
| Binary | Raw bytes the user typed | Any time after `ready` |
| Text | `{"type":"resize","cols":120,"rows":40}` | Whenever the terminal is resized |

**Server → browser**

| Kind | Content | When |
|---|---|---|
| Text | `{"type":"ready"}` | The shell has started |
| Binary | Raw terminal output | Continuously |
| Text | `{"type":"exit","code":0}` | The shell exited; the socket then closes with code `1000` |

If the instance was only just started, `ready` can take a few seconds while
the machine boots. The server waits up to 30 seconds for it.

### Close codes

| Code | Meaning |
|---|---|
| `1000` | The shell exited normally (an `exit` message came first) |
| `1008` | The first message was not a valid `auth` message |
| `4401` | Invalid or expired token |
| `4404` | Instance not found (or not yours) |
| `4409` | Instance is not running |
| `4503` | The machine could not be reached, or the connection to it was lost |
| `1011` | Server error |

### Example with xterm.js

```js
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";

const term = new Terminal();
const fit = new FitAddon();
term.loadAddon(fit);
term.open(document.getElementById("terminal"));
fit.fit();

const ws = new WebSocket(`ws://localhost:8080/instances/${instanceId}/terminal`);
ws.binaryType = "arraybuffer";
const encoder = new TextEncoder();

ws.onopen = () => {
  ws.send(JSON.stringify({ type: "auth", token, cols: term.cols, rows: term.rows }));
};

ws.onmessage = (event) => {
  if (typeof event.data === "string") {
    const msg = JSON.parse(event.data);
    if (msg.type === "exit") term.write(`\r\n[shell exited with code ${msg.code}]\r\n`);
    return;
  }
  term.write(new Uint8Array(event.data));
};

ws.onclose = (event) => {
  if (event.code !== 1000) term.write(`\r\n[disconnected: ${event.reason || event.code}]\r\n`);
};

// Keystrokes go as binary; resizes as JSON.
term.onData((data) => ws.send(encoder.encode(data)));
term.onResize(({ cols, rows }) => ws.send(JSON.stringify({ type: "resize", cols, rows })));
window.addEventListener("resize", () => fit.fit());
```

## Other

- `GET /healthz` → `204`. For load balancers and uptime checks.
- `GET /guest/connect` is for machines, not browsers: each machine's guest
  daemon connects here with its own credentials.
