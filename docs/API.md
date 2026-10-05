# API

Everything a frontend needs: accounts, machines, their terminals and ports,
and the assistant's chats, designs and agents.

Base URL in development: `http://localhost:8080`. The frontend runs at
`http://coder.localhost:3000`.

- Request and response bodies are JSON.
- Errors always look like `{"error": "message"}`.
- Endpoints marked 🔒 need the header `Authorization: Bearer <token>`. A
  missing, invalid or expired token gets `401`.
- By default a token never expires: it is good until the user signs out. If
  the server sets `ACCESS_TOKEN_TTL`, tokens carry an expiry (the JWT's `exp`
  claim) and must be renewed with `POST /token/renew` before it passes. Either
  way, on a `401` send the user back to login.

To call the API from a page on another origin, add that origin to
`CORS_ALLOWED_ORIGINS` in `.env`.

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

### `POST /token/renew` 🔒

Trades a still-valid token for a fresh one. `200` with the same body as
login. It matters only where tokens expire: there, call it periodically while
the app is open (the frontend does so when the token has less than five
minutes left), and the user stays signed in for as long as they keep the app
open. A token that has already expired gets `401`, and the user must log in
again. A token with no `exp` claim never needs renewing.

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
| `POST /instances` 🔒 | `{"name": "my-project", "size": "medium", "disk_gb": 10, "stack": "base"}`; all but `name` are optional | `201` instance, status `stopped` | `400` invalid name, size or stack, `409` name already used, `503` that stack's image isn't built on this server |
| `GET /instances/{id}` 🔒 | – | `200` instance | `404` |
| `POST /instances/{id}/start` 🔒 | – | `200` instance, status `running` | `404`, `409` busy |
| `POST /instances/{id}/stop` 🔒 | – | `200` instance, status `stopped` | `404`, `409` busy |
| `DELETE /instances/{id}` 🔒 | – | `204`, no body | `404`, `409` busy |

- **Names** are 1–40 characters: lowercase letters, digits and hyphens,
  starting and ending with a letter or digit. They are unique per user.
- **Sizes.** `size` is how powerful the machine is, and is fixed once it is
  created:

  | `size` | vCPUs | Memory |
  |---|---|---|
  | `small` | 1 | 1 GB |
  | `medium` (default) | 2 | 2 GB |
  | `large` | 4 | 4 GB |

  `disk_gb` is `10` (default), `20` or `50`. With machines on local Docker
  the CPU and memory limits are enforced; the disk size is recorded but not
  enforced, because a local Docker volume has no quota.
- **Stacks.** `stack` is what the machine comes with besides Python and
  Node.js, which every machine has: `base` (nothing more, the default), `go`,
  `cpp` (compilers, CMake, a debugger), `dotnet` (the .NET SDK) or `data`
  (PostgreSQL and Redis, installed but not running). An instance's `stack`
  is in every response.
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

## Ports and the editor

Programs running inside a machine are reachable from the browser, each port
at its own address:

```
http://<port>-<instance id>.coder.localhost:8080/
```

Every machine runs the editor (VS Code, via code-server) on port **13337**.
Anything else the user starts, such as a dev server on port 3000, is
reachable the same way.

These addresses are a different origin from the app, so the app's access
token doesn't apply there. To open one, ask the API for a link:

### `POST /instances/{id}/ports/{port}/url` 🔒

```json
{ "path": "/?folder=/home/coder" }
```

The body is optional; `path` is where on that port to land (default `/`).

`200`:

```json
{ "url": "http://13337-5357ebb3-….coder.localhost:8080/__coder/auth?ticket=…&next=…" }
```

`400` for a port outside 1–65535, `404` if the instance isn't yours.

Load the URL in a frame or a new tab. It signs the browser in to that one
port of that one machine and redirects to `path`.

- The link works **once** and expires after **a minute**. Request a new one
  every time you open a port; don't store it.
- The sign-in then lasts 12 hours for that port.
- If the machine isn't ready, or nothing is listening on the port yet, the
  page says so and retries by itself, so you can load the editor straight
  after starting a machine.
- A machine created before this feature existed shows a "needs recreating"
  page.

```js
const { url } = await api("POST", `/instances/${id}/ports/13337/url`, { path: "/?folder=/home/coder" });
iframe.src = url;
```

### `POST /instances/{id}/ports/{port}/request` 🔒

Makes one HTTP request to a port inside the machine and returns the answer
as data. It is for code in the app that needs to read something a machine
serves (an API's description) or call it (trying an endpoint): the port's
own address is another origin, which the app's JavaScript cannot call and
the app's token does not open.

```json
{
  "method": "POST",
  "path": "/api/orders?dry=1",
  "headers": { "Content-Type": "application/json" },
  "body": "{\"item\":\"tea\"}"
}
```

Everything is optional: an empty body means `GET /`. `method` is one of
GET, HEAD, POST, PUT, PATCH, DELETE, OPTIONS. `path` is a path on that port,
with any query; a full URL is refused. Headers that describe the connection
(`Host`, `Connection`, `Content-Length`, `Accept-Encoding` and the like) are
not passed on.

`200`:

```json
{
  "status": 201,
  "headers": { "Content-Type": "application/json" },
  "body": "{\"id\":7}",
  "duration_ms": 12
}
```

`status` is what the machine answered, so its own 404 or 500 arrives inside
a `200`: an error status from this endpoint means the request could not be
made. Redirects are returned, not followed. `body` is text; if what came
back was not, it is base64 and `"base64": true` says so. Bodies over 2 MB
are cut there, with `"truncated": true`. The request is given 30 seconds.

| Status | Meaning |
|---|---|
| `400` | The port, method or path is not one that can be asked for. |
| `404` | The instance isn't yours. |
| `409` | The instance isn't running. |
| `413` | The request, body included, is over 1 MB. |
| `502` | Nothing is listening on the port, or what is did not answer with HTTP. |
| `503` | The machine is not connected yet; try again in a moment. |
| `504` | The machine took too long to answer. |

## Presence and automatic stopping

A running machine is stopped once **nobody has been connected to it** for a
grace period (2 minutes by default; the server's `IDLE_STOP_AFTER` setting).
Someone is connected while either of these is true:

- the machine's owner has a **presence connection** open (below), or
- a browser is connected to the machine itself: a terminal WebSocket, or a
  request or WebSocket to one of its ports, such as the editor.

A client that wants machines to keep running while its user is around must
hold a presence connection. Calling the REST API does not count.

### `GET /presence` (WebSocket)

Open it when the app loads and keep it open. Send one message:

```json
{ "type": "auth", "token": "<access token>" }
```

The server answers:

```json
{ "type": "ready", "idle_stop_seconds": 120 }
```

`idle_stop_seconds` is the grace period, for telling the user; `0` means
machines are never stopped for being idle. Nothing else is sent either way.
The server pings the connection, which is how a sleeping laptop is noticed.

If the connection drops, reconnect. The grace period is there to cover the
gap, as well as page reloads. Close code `4401` means the token was rejected.

```js
const ws = new WebSocket("ws://localhost:8080/presence");
ws.onopen = () => ws.send(JSON.stringify({ type: "auth", token }));
ws.onclose = () => setTimeout(connect, 1000); // then back off
```

While the assistant (below) is working for a user, that counts as the user
being connected, so their machines stay up until it finishes.

## The assistant

A chat in which the user says what they want built. The assistant creates
machines, has code written on them by a coding agent inside each one, and
asks the user when a choice is theirs.

A chat is a list of **events**. You send a message, and then follow the
events that result: the work happens on the server after the request that
sent the message has returned, and carries on if the client disconnects.

### `GET /assistant` 🔒

```json
{ "enabled": true, "model": "deepseek-chat" }
```

`enabled` is `false` when the server has no key for a model
(`DEEPSEEK_API_KEY`). Chats can still be listed and read; sending a message
returns `503`.

### Chats

```json
{
  "id": "51d408f7-36c3-4197-bede-e3ffa368a1b9",
  "title": "Make a backend on one machine and a frontend on another",
  "status": "idle",
  "created_at": "2026-10-02T09:16:35Z",
  "updated_at": "2026-10-02T09:17:06Z"
}
```

| Status | Meaning |
|---|---|
| `idle` | Nothing in progress. A message can be sent. |
| `running` | The assistant is working. Messages are refused; it can be cancelled. |
| `waiting` | The assistant asked a question. The next message is the answer. |

The title is the first line of the first message.

| Method and path | Does | Success | Errors |
|---|---|---|---|
| `GET /chats` 🔒 | Lists your chats, most recently active first | `200` `{"chats": [...]}` | |
| `POST /chats` 🔒 | Starts an empty chat. No body. | `201` the chat | |
| `GET /chats/{id}` 🔒 | A chat and its whole history | `200` `{"chat": {...}, "events": [...]}` | `404` |
| `DELETE /chats/{id}` 🔒 | Stops and deletes a chat. Machines it created are kept. | `204` | `404` |
| `POST /chats/{id}/messages` 🔒 | Sends a message: `{"text": "..."}`, 1 to 20000 characters | `202`, no body | `400`, `404`, `409` while it is working, `503` if not enabled |
| `POST /chats/{id}/cancel` 🔒 | Stops the assistant, or withdraws the question it is waiting on. Does nothing if it is idle. | `204` | `404` |

### Events

```json
{ "seq": 7, "kind": "tool", "payload": { ... }, "created_at": "2026-10-02T09:16:51Z" }
```

`seq` counts up from 1 within a chat. `payload` depends on `kind`:

| Kind | Payload | Meaning |
|---|---|---|
| `user` | `{"text"}` | Something the user said. |
| `assistant` | `{"text", "tool_calls"}` | Something the assistant said (Markdown, may be empty) and what it set out to do. |
| `tool` | `{"tool_call_id", "name", "content", "failed", "machine"}` | The outcome of one tool call. `machine` (`{"id", "name", "status", "cpus", "memory_mb", "disk_gb"}`) is present when it concerns one. |
| `activity` | `{"tool_call_id", "machine", "text"}` | A line of progress from a step that takes a while: "Starting it" while a machine is created, "Writing api/main.py" during a coding task. |
| `message` | `{"from", "to", "question", "answer", "queued", "failed"}` | One agent asked another something. `from` and `to` are `{"machine", "agent"}`. `queued` means the other was busy and will get the question later. |
| `agent` | `{"agent_id", "agent", "tool_call_id", "machine", "content", "failed", "stopped"}` | A background agent finished. `tool_call_id` is the call that started it; `content` is its report, or why it failed. |
| `error` | `{"message", "stopped"}` | The run ended early. `stopped` is true when the user cancelled it. |

A tool call, inside an `assistant` event:

```json
{ "id": "call_0_abc", "type": "function",
  "function": { "name": "create_machine", "arguments": "{\"name\":\"api\"}" } }
```

`arguments` is a JSON object as text. A call is in progress until a `tool`
event with its `tool_call_id` arrives. The tools:

| Name | Arguments | To show it |
|---|---|---|
| `list_machines` | – | A line of progress. |
| `create_machine` | `name`, and optionally `size`, `disk_gb`, `stack` | A line of progress, with its `activity` events as it goes; the result's `machine` says what was made. |
| `start_machine`, `stop_machine` | `name` | A line of progress; the result's `machine.id` lets you link to the machine. |
| `code_on_machine` | `machine`, `task`, `agent` (optional, default `coder`), `background` (optional) | The task, its `activity` events while it runs, and the result's `content`: the agent's report. With `background`, the result only says it started; it is working until an `agent` event with the same `tool_call_id` arrives, which carries the report. |
| `list_agents` | – | A line of progress. |
| `stop_agent` | `machine`, `agent` | A line of progress. |
| `show_preview` | `machine`, `port`, `label` | A button. Open it with `POST /instances/{machine.id}/ports/{port}/url`, taking `machine.id` from the result. |
| `propose_architecture` | a design (below) | The proposal, with a way to approve it. It is waiting for the user until a `tool` event with its `tool_call_id` arrives; that event's `content` begins "The user approved" if they did. |
| `update_architecture` | a design | A line of progress. |
| `ask_user` | `question`, `options` (optional) | The question, with the options as buttons. Each option is `{"label", "description"}`, the assistant's own recommendation first. The answer is sent as an ordinary message (an option's `label`, or anything the user types) and comes back as this call's `tool` event. |

### Architecture

Before it builds an application the assistant proposes a **design**: the
components, the machine each runs on, and how they connect. The chat is
`waiting` until the user approves it or says what to change.

```json
{
  "summary": "A shop: a storefront, an API and PostgreSQL.",
  "structure": "frontend-and-backend",
  "repository": "repository-per-component",
  "reasons": ["Orders are relational and involve money, so PostgreSQL."],
  "nodes": [
    { "id": "frontend", "kind": "frontend", "name": "Storefront", "tech": "React",
      "machine": "web", "stack": "base", "size": "small", "disk_gb": 10, "port": 3000 },
    { "id": "backend", "kind": "backend", "name": "Orders API", "tech": "ASP.NET Core",
      "machine": "api", "stack": "dotnet", "size": "medium", "disk_gb": 10, "port": 8080 }
  ],
  "edges": [
    { "from": "frontend", "to": "backend", "label": "REST", "path": "/api", "env": "API_URL" }
  ]
}
```

- `structure` is `single-app`, `frontend-and-backend` or `microservices`;
  `repository` is `one-repository` or `repository-per-component`. They are
  separate decisions.
- A node's `kind` is `frontend`, `backend`, `service`, `database`, `cache`,
  `queue`, `ci` or `external`. An external node has no machine.
- An edge's `env` is the variable the first component's programs are given
  the second's address in. Its `path` makes the connection usable from a
  browser: requests to that path on the first component's address are passed
  to the second, unchanged.

| Method and path | Does | Success | Errors |
|---|---|---|---|
| `GET /chats/{id}/architecture` 🔒 | The chat's design and how it stands | `200`, below | `404` if the chat has none |
| `POST /chats/{id}/architecture/approve` 🔒 | Approves the proposed design, which starts the build. Body optional: `{"design": {...}}` approves it as changed. | `202` | `400` the changed design doesn't hold together, `404` nothing is proposed, `409` the assistant isn't waiting |

```json
{
  "status": "approved",
  "design": { "...": "as above" },
  "nodes": [
    { "id": "backend", "machine": { "id": "5357…", "name": "api", "status": "running" }, "listening": true }
  ],
  "updated_at": "2026-10-04T14:12:09Z"
}
```

`status` is `proposed` or `approved`. For each node, `machine` is present
once its machine exists, and `listening` says whether anything answers on its
port (absent if it has no port or its machine isn't running). Poll this to
keep a diagram current.

To ask for a change instead of approving, send an ordinary message: the
assistant takes it as what to change and proposes again.

### Agents

A coding agent works inside one of the user's machines, on tasks from the
assistant. Each has a name ("backend", "tests"), and several can work on one
machine at once.

```json
{
  "id": "6f0d…",
  "name": "tests",
  "chat_id": "51d4…",
  "machine": { "id": "5357…", "name": "api" },
  "status": "working",
  "task": "Write a check for the API on port 8000 and run it.",
  "background": true,
  "activity": "Running python3 checks/check_api.py",
  "report": "",
  "started_at": "2026-10-03T09:21:04Z",
  "finished_at": null,
  "updated_at": "2026-10-03T09:21:09Z"
}
```

`status` is `idle` (nothing to do, or finished its last task), `working`,
`failed` or `stopped`. `activity` is what it is doing this moment, while it
works; `report` is how its last task ended.

| Method and path | Does | Success | Errors |
|---|---|---|---|
| `GET /agents` 🔒 | Lists your agents from every chat: those working first, then the most recently active, at most 100 | `200` `{"agents": [...]}` | |
| `POST /agents/{id}/stop` 🔒 | Stops what the agent is doing. What it did so far is kept. Stopping an agent that isn't working does nothing. | `204` | `404` |

There is no stream of agents: poll `GET /agents` every few seconds while any
is working. The chat an agent belongs to also records its progress as
`activity` events.

### `GET /chats/{id}/stream` 🔒

Follows a chat as [server-sent events](https://developer.mozilla.org/en-US/docs/Web/API/Server-sent_events).
`?after=<seq>` skips events up to and including that number (default `0`:
everything).

```
event: chat
data: {"id":"51d4…","title":"New chat","status":"idle",…}

event: event
data: {"seq":1,"kind":"user","payload":{"text":"Make a backend…"},…}

event: chat
data: {"id":"51d4…","title":"Make a backend…","status":"running",…}

event: draft
data: {"text":"A few choices are"}

event: draft
data: {"text":"A few choices are yours before I start."}

event: event
data: {"seq":2,"kind":"assistant","payload":{"text":"A few choices are yours before I start.","tool_calls":[…]},…}

event: draft
data: {"text":""}

: keep-alive
```

| Event | Data | Sent |
|---|---|---|
| `event` | an event | once for each event after `after`, then as each new one happens |
| `chat` | the chat | at the start, and whenever its status or title changes |
| `draft` | `{"text"}` | while the assistant is writing a message: everything it has written of it so far, a few times a second |
| `deleted` | `{}` | if the chat is deleted; the stream then ends |

A `draft` is for showing the message take shape and is never part of the
history. Each one replaces the last. When the message is complete it arrives
as an ordinary `assistant` event and the draft becomes empty; it also
becomes empty if the assistant is stopped or fails part-way.

A status change is sent after the events that led to it, so `idle` never
arrives before the assistant's last message. Lines starting with `:` are
keep-alives, every 20 seconds.

The stream stays open until you close it. If it drops, reconnect with
`?after=` set to the last `seq` you received: nothing is lost, because
events are read from the database.

The browser's `EventSource` can't send an `Authorization` header, so read
the stream with `fetch`:

```js
const response = await fetch(`http://localhost:8080/chats/${id}/stream?after=${lastSeq}`, {
  headers: { Authorization: `Bearer ${token}` },
});
const reader = response.body.getReader();
const decoder = new TextDecoder();
let buffer = "";

for (;;) {
  const { done, value } = await reader.read();
  if (done) break; // reconnect
  buffer += decoder.decode(value, { stream: true });

  let end;
  while ((end = buffer.indexOf("\n\n")) >= 0) {
    const block = buffer.slice(0, end);
    buffer = buffer.slice(end + 2);

    const name = block.match(/^event: (.*)$/m)?.[1];
    const data = block.match(/^data: (.*)$/m)?.[1];
    if (!data) continue; // keep-alive
    if (name === "event") lastSeq = show(JSON.parse(data)).seq;
    if (name === "chat") setStatus(JSON.parse(data).status);
    if (name === "draft") showDraft(JSON.parse(data).text);
  }
}
```

## Other

- `GET /healthz` → `204`. For load balancers and uptime checks.
- `GET /guest/connect` is for machines, not browsers: each machine's guest
  daemon connects here with its own credentials.
- `GET /guest/team` and `POST /mcp` are for machines as well. The first
  returns what a machine keeps: a record of the owner's machines, agents and
  designs as files, and the services it is to keep running, each with a
  `revision` that goes up every time the service is registered. In asking,
  the machine says what it has started, in the `X-Coder-Services` header: a
  JSON object of service names and the revision each was started for. The
  second is an MCP server its coding agent uses to look other agents up, ask
  them questions, leave notes and register services. Both take the machine's
  own credentials.
- `POST /llm/v1/chat/completions` is for machines too: the coding agent in a
  machine calls it for its model, authenticating as the machine. It answers
  only while the assistant has that machine working on a task.
