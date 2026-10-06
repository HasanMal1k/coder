# Learning Go by rebuilding this project, in five projects

Five projects. Each covers one area of Go that this codebase depends on,
and each builds on the one before, so the fifth joins the first four into a
small version of `coder`. By then you will have written, by hand, a small
version of every moving part of your own portfolio project.

- This page: the projects.
- [docker-example/](docker-example/): a worked Docker and Compose example,
  already written and tested. It is part of project 3, and the one piece
  here that is not yours to write.
- [INTERVIEW.md](INTERVIEW.md): the questions each project raises, with
  answers, and how to talk about `coder` itself.

| | Project | What you learn | In coder |
|---|---|---|---|
| 1 | [machinectl](#1-machinectl) | the language: types, errors, interfaces, tests | `instance`, `sandbox` |
| 2 | [watchdog](#2-watchdog) | doing many things at once: goroutines, channels, locks, `context` | `presence`, `tunnel.Hub`, idle stop |
| 3 | [machines-api](#3-machines-api) | a server: `net/http`, Postgres, Docker and Compose | `httpapi`, `store`, `cmd/server` |
| 4 | [events](#4-events) | streaming: `io.Reader`, server-sent events, backpressure | a chat's event stream |
| 5 | [agent](#5-agent) | systems: processes, signals, a connection that dials out | `guest`, `tunnel` |

Each is a week or two of evenings. The third and fifth are the largest.
Accounts and sign-in are left out on purpose.

## How to do these

- **Type every line yourself.** No assistant writes the code. Asking one to
  review what you wrote, or to explain a compiler error, is fine.
- **One repository for all five**, since each uses the last: `go mod init
  github.com/<you>/minicoder`, a package or two per project, and a git tag
  when each is done. Under `learn/` is fine (a folder with its own `go.mod`
  is not part of the product's build), and so is a repository of its own,
  which you can then show people.
- **Every project ends the same way**: `gofmt -l .` prints nothing,
  `go vet ./...` prints nothing, `go test -race ./...` passes.
- **Stuck?** In this order: the package's page on
  [pkg.go.dev](https://pkg.go.dev/std) (or `go doc net/http.Handler` in a
  terminal), [Go by Example](https://gobyexample.com), then the file in
  `coder` named under "In coder". Read it, close it, and write yours.
- **Keep a `NOTES.md`**: what surprised you, what broke, how you found it.
  Those notes become your interview stories.
- **When a project is done**, answer its "Ask yourself" questions out loud
  before looking them up in [INTERVIEW.md](INTERVIEW.md).

If you have not yet, do [A Tour of Go](https://go.dev/tour/) first. It takes
an afternoon and project 1 assumes it.

---

## 1 machinectl

**What you learn: the language.** Structs and their zero values, methods on
a pointer, errors a caller can tell apart, interfaces, and tests written as
a table.

**Build** a command that keeps a list of machines in a file.

```
machinectl -file machines.json create web
machinectl list
machinectl start web
machinectl stop web
machinectl delete web
```

```go
type Status string   // "stopped", "starting", "running", "stopping", "error"
type Op int          // Start, Stop

func New(name string) (Machine, error)
func (m *Machine) Begin(op Op) error
func (m *Machine) Finish(err error) error

type Store interface {
    Get(name string) (Machine, error)   // ErrNotFound
    Put(m Machine) error
    Delete(name string) error           // deleting a missing one is not an error
    List() ([]Machine, error)           // sorted by name
}
```

The machine:

- A new machine is `stopped`. Its name is 1 to 40 lowercase letters, digits
  or hyphens, beginning and ending with a letter or digit; anything else is
  `ErrInvalidName`.
- `Begin(Start)` moves `stopped` or `error` to `starting`. `Begin(Stop)`
  moves `running` or `error` to `stopping`.
- `Begin` on a machine that is `starting` or `stopping` returns `ErrBusy`.
  Any other `Begin` returns a `*TransitionError` holding the status it was
  in and the operation asked for.
- `Finish(nil)` moves `starting` to `running` and `stopping` to `stopped`.
  `Finish(err)` moves either to `error` and returns an error that wraps
  `err`.

Where it is kept:

- Two stores. `Memory` keeps a map. `File` keeps a JSON file: read when
  opened, written on every change, by writing a temporary file and renaming
  it over the old one, so a crash can never leave half a file.
- A `Service` is given a `Store` when it is made and holds the rules:
  create (a name already in use is `ErrNameTaken`), start, stop, delete,
  list. It knows the interface and nothing else, and the interface is
  declared in *its* package, not beside the stores.
- For now a start is `Begin` followed at once by `Finish(nil)`. Project 3
  puts real time between them.
- The command prints errors to standard error and exits with code 1.

**Done when**

- [ ] One table-driven test walks every pair of status and operation.
- [ ] `errors.Is(err, ErrBusy)` works, and `errors.As` gives you the
      `*TransitionError` with the right fields.
- [ ] One function, `testStore(t *testing.T, s Store)`, runs the same tests
      against both stores, and `File`'s data is there after reopening it.
- [ ] A third store that exists only in a test file, and fails when told
      to, proves `Service` passes errors on, wrapped with `%w`.
- [ ] You changed one method's receiver from `(m *Machine)` to
      `(m Machine)`, watched a test fail, and wrote down why.
- [ ] You wrote a function with the return type `error` that returns a nil
      `*TransitionError`, saw that `err != nil` is true, and can explain it.

**In coder:** the statuses and the errors at the top of
[internal/instance/service.go](../internal/instance/service.go#L28), and
`begin` and `fail` [near the bottom](../internal/instance/service.go#L499).
`Provisioner` in
[internal/sandbox/sandbox.go](../internal/sandbox/sandbox.go#L66) is an
interface with one implementation,
[sandbox/docker](../internal/sandbox/docker/docker.go), and nothing else in
the project knows which it has. `LLM` in
[internal/assistant/llm.go](../internal/assistant/llm.go#L55) is why the
assistant's tests need no model. `Guests` and `Tasks` in
[internal/llmgateway/gateway.go](../internal/llmgateway/gateway.go#L28) are
one method each, declared by the package that needs them.

**Ask yourself:** What is a zero value? How does a nil slice differ from an
empty one? What is the difference between `%w` and `%v`, and between
`errors.Is` and `errors.As`? When does a method need a pointer receiver?
How does a type come to satisfy an interface, and where should an interface
be declared? What is the difference between a nil interface and an
interface holding a nil pointer? How would you test code that talks to
Docker?

---

## 2 watchdog

**What you learn: doing many things at once.** Goroutines and channels,
then locks and the race detector, then `context`.

**Build** a program that watches a list of addresses. Three packages, each
tested alone, then joined.

**Part 1: check in parallel** (goroutines and channels).

```
watchdog check -workers 20 -timeout 500ms example.com:443 localhost:5432 ...
```

- For each address print whether its TCP port is open and how long the
  answer took. Exit with code 1 if any is closed.
- At most `-workers` checks are in flight at once, and results are printed
  in the order the addresses were given.
- No variable is written by more than one goroutine; everything passes
  over channels. Every goroutine has finished before `main` returns.

**Part 2: remember who is there** (locks).

```go
// Tracker answers: how long has nobody been here?
func (t *Tracker) Connected(key string) (done func())
func (t *Tracker) IdleFor(key string, now time.Time) time.Duration

// Registry answers: is this one here, and tell me when it is.
func (r *Registry) Add(id string)
func (r *Registry) Remove(id string)
func (r *Registry) Wait(id string, timeout time.Duration) error  // ErrOffline
```

- `Connected` counts one more connection for `key`; calling `done` ends it,
  and calling it twice counts once.
- `IdleFor` is zero while any connection is open, and otherwise the time
  since the last one closed. Time is passed in, so tests never sleep.
- `Wait` returns as soon as `id` is added, and `ErrOffline` after the
  timeout. It must not check in a loop with sleeps. (Closing a channel
  wakes everyone who is receiving from it.)

**Part 3: stop what is idle** (`context`).

```go
func Run(ctx context.Context, every time.Duration,
    idle func() []string,
    stop func(ctx context.Context, id string) error)
```

- Every `every`, it calls `idle` and then `stop` for each ID returned, and
  it returns soon after `ctx` is cancelled.
- Each `stop` gets a context with its own two-second limit, which is *not*
  cancelled when `Run`'s is. A stop that has begun is allowed to finish.
- `main` cancels on Ctrl+C with `signal.NotifyContext`, lets a stop in
  flight finish, and exits with code 0.

Then go back and give `Registry.Wait` and the checks a context in place of
their timeout. And join the three: `watchdog run` checks every address
every ten seconds, the tracker records which are answering, and the loop
"stops" (a function that logs and takes a second) any that has not answered
for a minute.

**Done when**

- [ ] A hundred addresses that all time out, with fifty workers and a
      one-second timeout, take about two seconds, not a hundred.
- [ ] You removed the line that closes the results channel, saw `fatal
      error: all goroutines are asleep - deadlock!`, and can explain it.
- [ ] A hundred goroutines each connecting and disconnecting a thousand
      times leave the count at zero, and `go test -race` is silent.
- [ ] You removed the lock, ran `go test -race`, read the report (which two
      lines, which two goroutines), and put the lock back. Notice that
      without `-race` the test may well pass.
- [ ] Fifty goroutines in `Wait` all return within milliseconds of one
      `Add`.
- [ ] A stop in flight when the cancel comes is not cut off; a stop that
      takes too long is, and its error satisfies
      `errors.Is(err, context.DeadlineExceeded)`.
- [ ] No goroutine of yours is left after `Run` returns.

**In coder:** `carryOut` in
[internal/assistant/service.go](../internal/assistant/service.go#L663) runs
every tool the model asked for at the same moment and waits for all of
them. `serveErr` in [cmd/server/main.go](../cmd/server/main.go#L176) is a
channel with room for one value; work out what would go wrong with room for
none. [internal/presence/tracker.go](../internal/presence/tracker.go) is
the tracker, in 104 lines; compare yours when you are done. The registry is
the `Hub` in [internal/tunnel/hub.go](../internal/tunnel/hub.go#L34): see
its `changed` channel, `wait` and `add`. `RunIdleStop` in
[internal/instance/service.go](../internal/instance/service.go#L432) is the
loop, and `operationContext`
[below it](../internal/instance/service.go#L525) is the line that keeps an
operation going after its request has gone; read its comment.

**Ask yourself:** How is a goroutine different from a thread? What happens
when you send on a closed channel, receive from one, or use a nil one? Who
should close a channel? What is a goroutine leak? What is a data race, and
how is it different from a race condition? When a lock and when a channel?
What does `-race` find, and what can it never find? What three things does
a context carry? Why `defer cancel()` even when the work succeeded? How do
you stop a goroutine from outside?

---

## 3 machines-api

**What you learn: a server.** `net/http` with no framework, functions that
wrap handlers, shutting down cleanly, a real database, and containers.

**Build** project 1's machines behind an HTTP API, kept in Postgres, run
with Compose.

**Part 1: the API.**

```
POST   /machines             {"name":"web"}  201, or 400 bad name, 409 name taken
GET    /machines                              200
GET    /machines/{id}                         200, 404
POST   /machines/{id}/start                   200, 404, 409 busy or not allowed
POST   /machines/{id}/stop                    200, 404, 409
DELETE /machines/{id}                         204, 404
GET    /healthz                               204
```

- Starting now takes time. Put it behind an interface, `Runner`, with a
  stand-in that sleeps two seconds. A start is `Begin`, then the runner,
  then `Finish`. A second start that arrives meanwhile gets 409.
- Three packages. `httpapi` decodes the request, calls the next, encodes
  the answer. `machines` holds the rules. `store` holds a map behind a
  lock. `machines` does not import `net/http`.
- Errors become status codes in exactly one function. What is sent and
  received are their own structs, not the stored one. Unknown fields and
  bodies over 1 MiB are refused.
- Settings come from the environment through one `Load() (Config, error)`:
  `PORT` (default 8080, a whole number from 1 to 65535),
  `SHUTDOWN_TIMEOUT` (a duration, default `10s`, more than zero) and, from
  part 4, `DATABASE_URL` (required). If several are wrong, the one error
  reports all of them.

**Part 2: wrappers and shutdown.** Each wrapper is a
`func(http.Handler) http.Handler`:

- **Request ID**, taken from `X-Request-ID` or made up, kept in the
  request's context under a key of an unexported type.
- **Access log**: method, path, status, duration. Seeing the status means
  wrapping the `ResponseWriter`.
- **Recover**: a handler that panics gives a 500, and the server carries
  on.

And a route, `/slow?d=3s`, that waits that long but returns at once if the
request's context ends. On SIGTERM the server stops accepting, gives
requests in flight `SHUTDOWN_TIMEOUT`, and exits with code 0.

**Part 3: containers.** This part is written for you:
[docker-example/](docker-example/). Read its three files, follow its README
by hand, break it in the six ways it lists. Then write a Dockerfile and a
Compose file for your API without copying.

**Part 4: Postgres.** Replace the map, behind the same interface. The
handlers and the rules do not change; that is what the interface was for.

- Postgres and a `migrate` job in Compose. Migrations go up and down.
- `pgx` and its pool. Ping at startup. Every query takes the request's
  context and uses `$1` placeholders.
- A duplicate name (Postgres error code `23505`) becomes `ErrNameTaken`; no
  row becomes `ErrNotFound`.
- **The claim.** `Transition(ctx, id, from, to)` is a single `UPDATE ...
  WHERE id = $1 AND status = ANY($2)` that reports whether it changed a
  row. Write it wrongly first, as a `SELECT`, a check in Go, then an
  `UPDATE`, and run the test below.
- At startup, anything left `starting` or `stopping` becomes `error`: this
  process is the only place such work runs, so it was cut off.

**Done when**

- [ ] `httptest` covers every route and every status above, and a table
      test with `t.Setenv` covers the settings.
- [ ] `go list -f '{{.Imports}}' ./machines` does not mention `net/http`.
- [ ] `kill -TERM` during `/slow?d=3s`: the answer still arrives, then the
      process exits with code 0.
- [ ] Fifty goroutines start the same machine. Exactly one succeeds and
      forty-nine get `ErrBusy`. With the wrong version, the test fails.
- [ ] `kill -9` the server in the middle of a start, start it again: the
      machine is `error` and can be started.
- [ ] `docker compose up --build -d --wait` brings up the database, the
      migrations and the API. The image is under 30 MB, does not run as
      root and has a health check, and `docker compose stop` returns in
      under a second.

**In coder:** [internal/httpapi/router.go](../internal/httpapi/router.go)
is the whole list of routes. In
[internal/httpapi/instances.go](../internal/httpapi/instances.go) the
request and response types are at the top and `writeInstanceError`, the one
place errors become status codes, is at the bottom.
[internal/middleware/auth.go](../internal/middleware/auth.go) is a wrapper;
it happens to check who is signed in, and its shape and its unexported
`contextKey` are what to look at. The server and its shutdown are the last
forty lines of [cmd/server/main.go](../cmd/server/main.go#L162).
`TransitionStatus` in
[internal/store/instances.go](../internal/store/instances.go#L226) is the
claim, and `Create` [in the same file](../internal/store/instances.go#L52)
turns Postgres's "duplicate" into the project's own error. The tables are
in [migrations/](../migrations/). The settings are
[internal/config/config.go](../internal/config/config.go#L54).

**Ask yourself:** What is the one method of `http.Handler`? Are handlers
called one at a time? 400, 404 or 409? What is middleware, in one sentence?
What does `Shutdown` wait for, and what does it not? Why placeholders? Why
is a lock in Go not enough to stop two starts? `UPDATE ... WHERE` or
`SELECT ... FOR UPDATE`? How is an image different from a container? Why
copy `go.mod` before the rest of the source? What does `depends_on` wait
for?

---

## 4 events

**What you learn: streaming.** `io.Reader` and `io.Writer`, a response that
never ends, and what to do about a reader that is slow.

**Build** an event log for each machine, and a live stream of it.

**Part 1: the format.** A decoder and an encoder for server-sent events,
the format a model's reply arrives in.

```
event: greeting
data: hello
data: world

: a comment, to be ignored

data: no name
```

That is two events: `{Name: "greeting", Data: "hello\nworld"}` and
`{Name: "", Data: "no name"}`.

```go
func NewDecoder(r io.Reader) *Decoder
func (d *Decoder) Next() (Event, error)    // io.EOF at a clean end

func NewEncoder(w io.Writer) *Encoder
func (e *Encoder) Encode(ev Event) error
```

- A line is `field: value`. Several `data` lines are joined with newlines.
  A blank line ends the event. Lines starting with `:` are ignored.
- A stream that ends in the middle of an event gives
  `io.ErrUnexpectedEOF`, so a caller can tell a reply that finished from
  one that was cut off.
- A line longer than 1 MiB is an error. Never let the other end decide how
  much memory you use.

**Part 2: the log.** A table of events with a number per machine, counting
from 1. Every change of status appends one; while a machine is running,
append a line each second so that there is something to watch.

**Part 3: the stream.**

- `GET /machines/{id}/events?after=N` answers with server-sent events:
  everything after `N`, then it stays open and sends each new event as it
  is appended. A comment line every fifteen seconds keeps the connection
  alive. It ends when the client goes.
- A hub: `Watch(id)` returns a channel and a function to stop watching;
  `Notify(id)` wakes the watchers. The channel has room for one value and
  `Notify` never blocks. A wake-up says only "something changed"; the
  handler then reads the database.
- The handler starts watching *before* it reads.
- The client in your tests is your own decoder.

**Done when**

- [ ] Events encoded into a `bytes.Buffer` decode to the same events, and
      the decoder still works when its reader is wrapped in
      `iotest.OneByteReader`.
- [ ] Two `curl -N` clients see the same events in the same order.
- [ ] Reconnecting with `?after=` the last number seen misses nothing and
      repeats nothing.
- [ ] A client that connects and never reads does not slow anyone
      appending.
- [ ] After the clients leave, the number of goroutines is what it was.
- [ ] You can describe the event that is lost if the handler reads first
      and watches second.

**In coder:** `readStream` in
[internal/assistant/llm.go](../internal/assistant/llm.go#L150) is the
decoder, written for one caller; look at its line cap and at what it does
with a reply that never finished. `writeEvent` in
[internal/httpapi/chats.go](../internal/httpapi/chats.go#L391) is the
encoder, and `Stream` [above it](../internal/httpapi/chats.go#L222) is the
handler, loop and all. `Watch`, `append` and `notify` in
[internal/assistant/service.go](../internal/assistant/service.go#L532) are
the hub. `AppendEvent` in
[internal/store/chats.go](../internal/store/chats.go#L185) numbers events
with `MAX(seq) + 1`; work out why `append` holds a lock around it, and what
that means for running two servers.

**Ask yourself:** What may `Read` legally return? Why is `io.ReadAll` on a
network response a risk? Server-sent events, WebSockets or polling? What is
backpressure? What do you do with a client that reads too slowly? Why send
a signal and re-read, instead of sending the event down the channel?

---

## 5 agent

**What you learn: systems.** Running other programs, signals, and a
connection made backwards: the machine you want to reach dials you. This
project joins the other four.

**Build** a second program, `agent`, that runs somewhere accepting no
incoming connections, and make "start a machine" mean something.

**Part 1: keep a program running.**

```
agent keepup -- python3 -m http.server 9000
```

- When the program exits, start it again: after 1 second, then 2, 4, up to
  30. Once it has stayed up for a minute, the wait goes back to 1.
- On SIGTERM or Ctrl+C, send the program SIGTERM, give it five seconds, and
  kill it if it is still there. Its output goes to a log file.

**Part 2: dial out.**

- The agent opens a WebSocket to the server's `/agents/connect`, with its
  ID in a header. The server keeps it in the registry from project 2.
- The `Runner` from project 3 stops sleeping: starting a machine asks the
  agent, over that connection, to run a program under the supervisor from
  part 1. Stopping it stops the program.
- Each line the program prints is sent back and appended to the machine's
  events, so project 4's stream shows it live. Design the messages
  yourself; you will need a way to tell one machine's output from
  another's.
- The agent reconnects with the same backoff as part 1. A second
  connection for the same ID replaces the first.

**Part 3: many streams.** Replace your message format with
[yamux](https://pkg.go.dev/github.com/hashicorp/yamux): many independent
streams over the one connection, and keepalives that notice a peer that
has vanished.

**Part 4: join it.** The tracker from project 2 counts who is watching a
machine's event stream, and the loop from project 2 stops any machine
nobody has watched for two minutes. One Compose file runs the server, the
database, the migrations and an agent that publishes no port.

**Done when**

- [ ] `kill` the supervised program: it is back within the backoff. Kill
      it five times quickly and the waits grow. The backoff times are a
      pure function with its own table test.
- [ ] A program that ignores SIGTERM is killed after five seconds, and `ps`
      afterwards shows nothing left behind.
- [ ] With the agent in a container that publishes no ports, `POST
      /machines/{id}/start` starts a program there, and `curl -N` on its
      events shows what it prints.
- [ ] Kill the server and start it again: the agent is back by itself.
- [ ] Two machines' programs on one agent keep their output apart.
- [ ] Close the last `curl -N`, wait two minutes: the machine is stopped.

**Go further**, in any order: forward a port (`GET
/agents/{id}/ports/{port}/...` answered by whatever listens on that port
beside the agent, using `httputil.ReverseProxy`); replace your messages
with a gRPC service; have the server create the agent's container itself
through Docker's API.

**In coder:** `supervise` in
[internal/guest/supervise.go](../internal/guest/supervise.go) is part 1, in
sixty lines; it is what keeps the editor and the coding agent alive inside
every machine. The agent's side of the connection is
[internal/guest/connect.go](../internal/guest/connect.go): `Run`,
`connectAndServe`, `ServeConn`. The server's side is `Serve`, `add` and
`remove` in [internal/tunnel/hub.go](../internal/tunnel/hub.go#L59) and
`GuestConnect` in
[internal/wsgateway/gateway.go](../internal/wsgateway/gateway.go#L200).
"The tunnel" in [docs/ARCHITECTURE.md](../docs/ARCHITECTURE.md#the-tunnel)
says why. For going further:
[internal/tunnel/forward.go](../internal/tunnel/forward.go) and
[internal/portproxy/proxy.go](../internal/portproxy/proxy.go) forward
ports, [internal/guestapi/guest.proto](../internal/guestapi/guest.proto) is
the gRPC service, and
[internal/sandbox/docker/docker.go](../internal/sandbox/docker/docker.go)
creates the containers.

**Ask yourself:** What is the difference between SIGTERM and SIGKILL? What
is a zombie process, and what is special about process 1 in a container?
Why back off, and why reset? Why should the machine dial out? What does
multiplexing buy over opening more connections? How do you find out the
other end has vanished, when nothing was closed?

---

## When you are done: how they come together

Read this after project 5, with the code open.

### Each project is a part of coder

```
Browser ─ HTTP ─► httpapi [3] ─► instance [1 3] ─► sandbox [1] ─► Docker [3]
        ─ SSE ──► chats.Stream [4]   │  presence, idle stop [2]
        ─ WS ───► wsgateway [5]      ▼
                                  tunnel.Hub [2 5] ◄─ WebSocket, yamux, gRPC [5] ─ guestd
                                                                                    │ terminals, ports [2 4]
   assistant: the model behind an interface [1],                                    │ supervisor, services [5]
              its reply read as a stream [4], its tools run together [2]
   store [3]      config [3]      cmd/server: wiring and shutdown [2 3]
```

| You built | In coder it is | What coder adds |
|---|---|---|
| 1 machinectl | the statuses in `internal/instance`; `sandbox.Provisioner`, `assistant.LLM` | The status lives in a database row; Docker and a model behind the interfaces |
| 2 watchdog | `carryOut`, `internal/presence`, `tunnel.Hub`, `RunIdleStop` | Runs that outlive the request that began them |
| 3 machines-api | `internal/httpapi`, `internal/store`, `cmd/server`, the Docker files | Thirty-one routes; the same claim used for chats; Docker driven from Go |
| 4 events | `readStream`, `chats.Stream`, `assistant.Watch` | A draft of the reply, shown as it is written |
| 5 agent | `internal/guest`, `internal/tunnel`, `internal/guestapi` | Terminals as two-way streams; a hostname per port |

### Follow one request: opening a terminal

1. The browser opens a WebSocket to `/instances/{id}/terminal`: `Terminal`
   in [wsgateway/gateway.go](../internal/wsgateway/gateway.go#L88). **[5]**
2. `OpenTerminal` in
   [instance/service.go](../internal/instance/service.go#L303) loads the
   instance by ID *and* owner. Someone else's is not found. **[3]**
3. `wait` in [tunnel/hub.go](../internal/tunnel/hub.go#L147) blocks for up
   to thirty seconds until that machine's daemon is connected, woken by a
   closed channel. **[2]**
4. `g.client.Terminal(ctx)` starts a gRPC call. Its "dial" opens a yamux
   stream on the WebSocket the machine itself opened earlier. **[5]**
5. Inside the machine, `Terminal` in
   [guest/terminal.go](../internal/guest/terminal.go#L58) starts a shell
   and copies each direction in a goroutine of its own. **[5, 2]**
6. Back in the gateway, `pumpInput` and `pumpOutput` do the same between
   the browser and the stream. A browser that reads slowly slows every hop
   behind it, back to the program that is printing. **[2, 4]**
7. `MachineConnected` returned a function, and `defer` calls it when the
   tab closes. Two minutes later `RunIdleStop` notices **[2]**, `Stop`
   claims the row with one `UPDATE` **[3]**, and `provisioner.Stop` goes
   through an interface **[1]** to Docker **[3]**.

Seven steps, and each is something you have written.

### Follow another: sending a chat message

1. `POST /chats/{id}/messages` reaches `Send` in
   [assistant/service.go](../internal/assistant/service.go#L340). **[3]**
2. `Send` claims the chat exactly as a machine is claimed: `idle` to
   `running` in one statement. A second message sent at the same moment is
   told the assistant is busy. **[3]**
3. `startRun` starts a goroutine with a context that does not descend from
   the request, so closing the tab does not stop the work. **[2]**
4. `run` asks the model through the `LLM` interface **[1]**; `Complete`
   in [assistant/llm.go](../internal/assistant/llm.go#L80) reads the answer
   as server-sent events. **[4]**
5. Each piece of text wakes whoever is watching (`drafting`, `notify`), and
   their `Stream` handlers send it on. **[4]**
6. The tools the model asked for run together in `carryOut` **[2]**. One
   of them gives a coding agent a task: over the tunnel, to a program the
   supervisor keeps alive. **[5]**
7. Every step is appended to `chat_events` and streamed. **[3, 4]**

### What no project covered

- **Accounts and sign-in**, left out on purpose:
  [internal/auth](../internal/auth/service.go).
- **The assistant's loop**: asking a model, running the tools it names,
  and asking again.
  [internal/assistant/service.go](../internal/assistant/service.go), `run`.
  Section 16 of [docs/BACKEND.md](../docs/BACKEND.md) explains it.
- **A hostname per port**, with single-use tickets traded for cookies:
  [internal/portproxy](../internal/portproxy/proxy.go).
- **The MCP server** the coding agents ask:
  [internal/teamapi/mcp.go](../internal/teamapi/mcp.go).
- **The frontend.** [web/README.md](../web/README.md).

### Then make it yours

The best proof that it came together is to fix the project's own open
issues, by hand, from [docs/STATUS.md](../docs/STATUS.md). Each is small,
and each is a story for an interview: what was wrong, how you found the
cause, what you changed, how you tested it.

| Issue in STATUS | What it needs | From project |
|---|---|---|
| The database is reachable from every machine | `127.0.0.1:` in Compose | 3 |
| CORS is open when the list is empty | Read what the library does; fix code or comment | 3 |
| A machine can be left "starting" | Decide what happens when the last update fails | 3 |
| Stop can race with Send | Close the gap between the claim and the run | 2 |
