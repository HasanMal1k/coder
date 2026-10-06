# Interview notes

Two things get asked of someone with this project on their CV: "tell me
about it", followed by questions that dig into it, and ordinary Go
questions. Both are here, with the Docker ones at the end.

Use it as a check, not a script. After each project in
[README.md](README.md), answer its questions out loud first, then read the
answers here. An answer you can only recite is found out by the second
follow-up question; one you have built is not.

- [Talking about coder](#talking-about-coder)
- [Go questions, with answers](#go-questions-with-answers)
- [What does this print?](#what-does-this-print)
- [Programs you may be asked to write](#programs-you-may-be-asked-to-write)
- [Docker](#docker)

## Talking about coder

### Say how it was built

If an assistant wrote parts of it with you, say so plainly. It is common
and nobody minds. What sinks an interview is being unable to explain a
file with your name on it. The five projects exist so that you can
explain all of them, and so that the fixes listed at the end of the
curriculum are commits you wrote alone.

### In one breath

> It is a cloud development platform. Each user gets persistent Linux
> machines with a terminal and VS Code in the browser, and an assistant
> that designs an application with them and then builds it across those
> machines, by directing a coding agent inside each one. The backend is
> Go, the frontend is Next.js, and the machines are Docker containers.

### In two minutes

There are two programs. The **control plane** is one Go server: the API,
the database, the assistant. **`guestd`** is a small daemon inside every
machine.

Two ideas carry the design.

1. **Creating a machine and using a machine are separate.** One interface,
   `sandbox.Provisioner`, creates, starts, stops and deletes. Docker is the
   only implementation today. Everything that happens *inside* a machine
   (terminals, ports, the coding agent) goes through a tunnel that does not
   care where the machine runs.
2. **The machine dials out.** `guestd` opens one WebSocket to the control
   plane. yamux splits that one connection into many streams, and each
   stream carries gRPC, with the control plane as the client. So a machine
   needs no public address and no open port.

Postgres holds users, machines and chats. A chat is an append-only list of
events, which is why closing the tab loses nothing. The assistant is a
loop: ask the model, run the tools it names, ask again. The coding agents
reach the model through a gateway in the control plane, so the model's key
never enters a machine.

### Decisions, and what each cost

An interviewer wants the second column as much as the first.

| Decision | Why | What it cost |
|---|---|---|
| The machine dials out | No public address or open port per machine; works on any provider | Every connection lives in one server's memory, so only one server can run |
| An operation claims its row with one conditional `UPDATE` | Two requests cannot both start a machine; nothing is locked while Docker works | A crash leaves the row "starting"; startup marks it `error`, which assumes one server |
| A chat is a list of events | Reconnect and miss nothing; a run survives a closed tab | The whole history is rebuilt and sent to the model at every step |
| A signal, then re-read, for live updates | A slow browser cannot make the server queue without limit | A database read per wake-up |
| The model's key stays in the control plane | A user with root on a machine cannot take it | One more hop; the machine's own token becomes worth stealing |
| Every port of every machine has its own hostname | A page on a machine cannot read the app's storage | Tickets, cookies and wildcard DNS |

### Say what is wrong with it before they find it

[docs/STATUS.md](../docs/STATUS.md) lists the open issues. Knowing them,
and what you would do, is a strength. The ones worth having ready:

- The database is published on every network interface, so a machine can
  reach it. The fix is one line in Compose.
- A container with root is not a wall between users who do not trust each
  other. The plan is microVMs.
- Sign-in tokens never expire and cannot be revoked. Chosen for
  convenience while there are no users; the fix is short-lived tokens with
  a refresh token kept on the server.
- No limits on sign-ups, machines or use of the model.
- One server process only.
- Tests are thick where the logic is (the assistant, the proxy, the
  tunnel) and thin in the database layer and the handlers.

### Questions you will get

**Two requests to start the same machine arrive together.** Both read the
row as `stopped`. Both run `UPDATE instances SET status = 'starting' WHERE
id = $1 AND status = ANY($2)`. Postgres lets one change the row; the other
changes nothing, sees zero rows affected, and returns "busy", a 409.
(`TransitionStatus`, [store/instances.go](../internal/store/instances.go#L226).)

**The server dies halfway through starting a machine.** The row stays
`starting`. At the next start, `RecoverInterrupted` moves every such row to
`error`, and the user can retry or delete. If it dies between creating the
container and saving its ID, that container is orphaned; its labels say
which instance it was for, and a reconciler to clean up is planned.

**The user closes the tab while the assistant is working.** The run was
started on a context of its own, not the request's
(`startRun`, [assistant/service.go](../internal/assistant/service.go#L387)),
and everything it does is written to Postgres. Reopen the page and the
stream begins from the last event the page saw.

**A terminal prints faster than the browser can take it. Where does the
data pile up?** Nowhere without limit. The WebSocket write blocks, so the
gateway stops reading the gRPC stream, so `guestd`'s send blocks, so it
stops reading the pseudo-terminal, so the program's own `write` blocks.

**Why gRPC on yamux on a WebSocket? That is three layers.** Each does one
job. The WebSocket gets out of the machine over ordinary HTTP. yamux gives
many independent streams on that one connection, and keepalives that
notice a dead peer. gRPC gives typed calls, streaming and cancellation in
place of a protocol invented by hand. yamux is also what lets the roles be
reversed: the side that *accepted* the connection can open streams on it.

**How would you run two servers?** Start from what is in memory: the
tunnel connections, who is present, runs in progress, who is watching a
chat, used tickets, the lock that numbers events. A request must reach the
server holding that machine's tunnel, so either route to it or pass
messages between servers; presence moves to something shared; event
numbers come from the database; startup recovery stops assuming it is
alone.

**How do you test something that talks to a model?** The model is behind
an interface with one method (`LLM`). Tests give the assistant a scripted
one. The whole flow was also run in a real browser against a stand-in
model server; those scripts were not kept in the repository, which is a
gap.

**What was the hardest bug?** Have your own answer. Your `NOTES.md` files
are where it comes from.

## Go questions, with answers

### Values and errors (project 1)

**What is a zero value?** What a variable holds before you set it: `0`,
`""`, `false`, and `nil` for pointers, slices, maps, channels, functions
and interfaces. A struct's is each field's. Good Go types are usable as
their zero value, like `sync.Mutex`.

**What is a slice?** A small header: a pointer to an array, a length and a
capacity. Copying a slice copies the header, not the array, so two slices
can share memory. `append` writes into spare capacity if there is any and
otherwise allocates a new array.

**Nil slice or empty slice?** Both have length 0 and both work with
`append` and `range`. Only one equals `nil`, and JSON encodes them as
`null` and `[]`.

**What should I know about maps?** Reading a nil map is fine and writing
to one panics. Order of iteration is deliberately random. Two goroutines
writing one map crash the program.

**Why errors as return values?** A failure is an ordinary outcome, handled
where it happens, visible in the function's signature. Wrap with
`fmt.Errorf("load config: %w", err)` to add where it happened and keep
the original.

**`%w` or `%v`?** `%w` keeps the original error reachable; `%v` keeps only
its text.

**`errors.Is` or `errors.As`?** `Is` asks "is this particular error
anywhere in the chain?". `As` asks "is there an error of this type in the
chain? Give it to me." In `coder`, `errors.Is(err, store.ErrNotFound)` and
`errors.As(err, &pgErr)` to read a Postgres error code.

**A fixed error value or an error type?** A value (`ErrBusy`) when the
caller needs to know which thing happened. A type when the caller needs
details.

**When `panic`?** For a bug: a state that cannot happen. Never for a
failure you expect. `recover` works only in a deferred function, and only
for a panic in the same goroutine; a panic in a goroutine nobody recovers
ends the whole program.

**What does `defer` do?** Runs a call when the surrounding function
returns, last deferred first. Its arguments are evaluated at the `defer`
line, not when it runs.

**Pointer or value receiver?** Pointer if the method changes the receiver,
if the struct is large, or if it holds something that must not be copied,
such as a lock. Use one kind throughout a type.

**What makes a name public?** A capital first letter. A package under
`internal/` can be imported only from within the tree that holds it.

**`go.mod` and `go.sum`?** `go.mod` names the module, the Go version and
the version of each dependency. `go.sum` holds checksums, so a download
that differs from what was first seen is refused.

**Strings?** Immutable bytes, usually UTF-8. `len` counts bytes; `range`
steps through characters (runes). Converting to `[]byte` copies.

**Generics?** Type parameters, since Go 1.18: `func Map[T, U any](...)`.
For code that is the same for many types, such as containers. Not a
replacement for interfaces, which are about behaviour.

### Interfaces and io (projects 1, 4)

**How does a type satisfy an interface?** By having the methods. Nothing is
declared.

**Where do interfaces go, and how big?** In the package that uses them,
holding only the methods that package calls. That is why
`llmgateway.Guests` is one method, though the type that satisfies it has
about twenty. Functions take interfaces and return concrete types.

**Why does `err != nil` hold for a nil pointer?** An interface value is a
pair: a type and a value. It is `nil` only when both are missing. A nil
`*MyError` put in an `error` has a type, so the interface is not nil.
Return a bare `nil`.

**Method sets?** A `T` has the methods with value receivers; a `*T` has
those and the pointer-receiver ones. So if any method has a pointer
receiver, only `*T` satisfies the interface.

**Type assertion?** `v, ok := x.(T)`: is the thing in this interface a
`T`? Without `ok` it panics when it is not. A type switch asks about
several types.

**Embedding?** Putting one type in another without a field name; its
methods become the outer type's. It is composition. There is no
overriding and no parent that calls down into a child.

**What may `Read` return?** Anything from 0 to `len(p)` bytes, and
possibly an error with them. Use the bytes first, then look at the error.
`io.EOF` means the end. It takes a buffer so the caller decides what is
allocated.

**Why is `io.ReadAll` a risk?** The other end decides how much memory you
use. Cap it with `io.LimitReader` or `http.MaxBytesReader`.

**Why close a response body?** Until you do, the connection behind it
cannot be reused and is leaked.

**How do you test code that talks to Docker?** Put Docker behind an
interface, and give the tests an implementation that records calls and
fails on demand.

### Goroutines and channels (project 2)

**Goroutine or thread?** A goroutine is scheduled by Go's runtime, not the
operating system. It starts with a stack of a few kilobytes that grows, so
a program can have hundreds of thousands. The runtime runs them on a small
number of threads.

**How does the scheduler work?** Three things: goroutines (G), OS threads
(M), and processors (P), of which there are `GOMAXPROCS`. A thread needs a
P to run Go code; each P has a queue of goroutines and steals from others
when its own is empty. A goroutine waiting on the network is parked and
costs no thread.

**Unbuffered or buffered?** Unbuffered: a send completes only when someone
receives, so the two goroutines meet. Buffered: a send completes while
there is room. A buffer hides a slow receiver until it is full.

**What happens on a nil or closed channel?**

| | nil channel | closed channel |
|---|---|---|
| send | blocks for ever | panics |
| receive | blocks for ever | returns the zero value at once, `ok` false |
| close | panics | panics |

**Who closes a channel?** The sender, and only when receivers need to
know there is no more. Never the receiver.

**What is `select`?** Waits on several channel operations and does one
that is ready, chosen at random if several are. With `default` it does not
wait: that is how `notify` in `coder` wakes a watcher without ever
blocking.

**How does `WaitGroup` go wrong?** Calling `Add` inside the goroutine: the
`Wait` can run first and see zero. Add before `go`, or use `wg.Go`.

**What is a goroutine leak?** A goroutine blocked for ever, usually on a
channel nobody will use again. Prevent it by knowing, when you start a
goroutine, how it ends. Find it with the goroutine profile from `pprof`.

**Worker pool or a goroutine each?** Goroutines are cheap, so limit them
only when what they use is limited: connections, an API's rate, memory.

**"Share memory by communicating"?** Hand a value to another goroutine
over a channel, so one owns it at a time, instead of both reaching for it
under a lock. It is advice. `coder` uses a lock for the presence table and
a closed channel to wake waiters, each where it is simpler.

### Locks (project 2)

**Data race or race condition?** A data race is two goroutines touching
the same variable at once, at least one writing, with nothing ordering
them. A race condition is a result that depends on timing, and can exist
with no data race at all: `SELECT` then `UPDATE` is one.

**What does `-race` do?** Watches memory accesses while the program runs
and reports a data race when it sees one. It finds only what actually
happened in that run, and makes the program several times slower. Use it
in tests.

**`Mutex` or `RWMutex`?** `RWMutex` lets many readers in together. It is
worth it only when reads far outnumber writes and take a while.

**Lock or channel?** A lock to protect a piece of state. A channel to hand
work over or to signal that something happened.

**How do deadlocks happen?** Two goroutines each hold what the other
needs. Avoid them by taking locks in one order everywhere, by never
blocking (on I/O or a channel) while holding one, and by remembering Go's
locks are not re-entrant.

**Why not copy a struct that holds a lock?** The copy has its own lock, so
the two no longer exclude each other. `go vet` catches it.

**`sync.Once`?** Runs a function exactly once however many goroutines
ask. In `coder` it makes a "done" callback safe to call twice.

### Context (project 2)

**What is a context for?** Carrying, down a chain of calls, a signal to
stop, a deadline, and a few values belonging to the request.

**Why the first parameter and not a field?** It belongs to one call, not
to an object that outlives it.

**Why always `defer cancel()`?** A context with a timeout holds a timer
and a link from its parent until it is cancelled. Not cancelling leaks
both.

**What does cancelling a parent do?** Cancels every context derived from
it. Never the other way.

**How do you stop a goroutine?** You ask. It has to be watching
`ctx.Done()` or a channel, and return.

**When is `context.WithoutCancel` right?** When work, once begun, must
finish even though whoever asked has gone. In `coder`: a machine that has
been claimed must be started and its status written, or it is stuck.

**What belongs in a context's values?** Things about the request that
cross package boundaries: the signed-in user, a request ID. Under a key of
an unexported type, so no other package can collide with it. Never
optional arguments.

### HTTP (projects 3, 4)

**What is `http.Handler`?** One method:
`ServeHTTP(http.ResponseWriter, *http.Request)`. `http.HandlerFunc` turns
an ordinary function into one.

**Are handlers called one at a time?** No. Each request runs in its own
goroutine, so anything they share needs protecting.

**What is middleware?** A function that takes a handler and returns a
handler that does something before or after calling it.

**Why separate request and response structs?** What is stored and what is
promised to clients change for different reasons, and it keeps fields
like `password_hash` from leaking out.

**Which status code?** 400 the request is malformed. 401 you are not
signed in. 403 you are, and may not. 404 not found, also used to hide
that something exists. 409 it conflicts with the current state ("busy",
"name taken"). 422 well-formed but not valid. 503 something we depend on
is down.

**Idempotent?** Doing it twice has the same effect as once: `GET`, `PUT`,
`DELETE`. `POST` is not, which is what makes retrying one dangerous.

**Why timeouts?** On a server, a client that sends very slowly holds a
connection for ever. On a client, `http.DefaultClient` has none, so a
server that never answers hangs you.

**What does `Shutdown` do?** Stops accepting, waits for requests in flight
until its context ends, closes idle connections. It does not wait for or
close connections that were taken over, such as WebSockets; `coder`
cancels a context they all descend from.

**Server-sent events or WebSockets?** SSE is an ordinary HTTP response
that stays open: one way, text, easy through proxies. A WebSocket is two
ways and binary. `coder` uses SSE for a chat's events and WebSockets for
the terminal and the tunnel.

**What is backpressure?** A slow receiver slowing the sender, so that data
does not queue without limit. Blocking writes give it for free; an
unbounded queue takes it away.

**A client reads too slowly. What can you do?** Block the sender. Drop
messages. Send only "something changed" and let the client re-read, which
is what `coder` does. Or disconnect it.

**What is a lost wake-up?** You check for news, find none, and start
waiting; the news arrived in between. Start listening first, then check.

### Databases (project 3)

**Why placeholders?** The values travel separately from the SQL, so
nothing a user types can become part of the statement.

**Why a pool?** Opening a connection is slow and Postgres allows a limited
number. A pool reuses them and caps how many are open.

**Why not a lock in Go for "one start at a time"?** It does not survive a
crash and is not shared with another server. The row is the truth, so the
row is where the claim is made.

**Conditional `UPDATE` or `SELECT ... FOR UPDATE`?** `FOR UPDATE` locks
the row for the length of a transaction, which would be open for as long
as Docker takes. The conditional update takes a moment and uses the
status column itself as the claim.

**What are isolation levels?** How much one transaction sees of others in
progress. Postgres defaults to Read Committed: each statement sees what
was committed before it began.

**What does `UNIQUE (owner_id, name)` also do?** Its index is sorted by
owner first, so it also serves "all of this user's machines".

**`RESTRICT` or `CASCADE`?** In `coder`, deleting a user is refused while
they have machines, because real containers exist and must not be
orphaned. Deleting a chat removes its events.

**What is a migration?** A numbered change to the schema, kept in the
repository and applied in order. The "down" file undoes it.

**`database/sql` or `pgx`?** `database/sql` is the standard interface for
any database. `pgx` is for Postgres only, and gives its types (arrays,
JSONB) and its errors (`pgconn.PgError`) directly.

### Processes and networks (project 5)

**SIGTERM or SIGKILL?** SIGTERM asks, and a program can catch it and tidy
up. SIGKILL cannot be caught.

**What is a zombie?** A child that has exited and whose parent has not
collected its status. Process 1 inherits orphans and must collect them;
in a container your program is process 1 and usually does not, which is
why `coder` starts machines with an init process.

**Why back off, and why reset?** Retrying at once hammers something that
is already failing. Doubling the wait up to a cap gives it room;
resetting after a stable period keeps one old failure from slowing every
later recovery. With many clients, add randomness so they do not all
retry together.

**Why should the machine dial out?** Outgoing connections pass firewalls
and NAT; incoming ones need an address and an open port for every
machine.

**What is multiplexing?** Many independent streams on one connection: one
handshake, one place to check the peer is alive. The cost is that one
lost packet delays all of them.

**How do you notice the other end has vanished?** TCP says nothing until
you send. So send: a ping on a timer, and give up when it is not answered.

**What is a reverse proxy?** A server that takes requests on behalf of
another and passes them on. `coder`'s reads which machine and port from
the hostname and forwards through the tunnel.

**Protobuf or JSON?** Protobuf is binary, has a schema and generated
types, and is smaller and faster. JSON can be read by a person and by
every tool. On the wire a field is its number, so never renumber or reuse
one; adding a field with a new number is safe.

**The four kinds of gRPC call?** One request and one response; a stream
from the server; a stream from the client; a stream each way.

### The runtime

**How does the garbage collector work?** It marks what is reachable and
sweeps the rest, running alongside your program with very short pauses.
`GOGC` trades memory for collector time; `GOMEMLIMIT` sets a soft
ceiling.

**Stack or heap?** The compiler decides. A value that may outlive its
function "escapes" to the heap. `go build -gcflags=-m` shows what did.

**What is `GOMAXPROCS`?** How many threads run Go code at once: by default
the number of CPUs, and since Go 1.25 the container's CPU limit if that is
lower.

## What does this print?

Each of these was run with Go 1.26. Cover the answers.

```go
type MyErr struct{}
func (*MyErr) Error() string { return "boom" }

func find() error {
    var e *MyErr
    return e
}
fmt.Println(find() == nil)
```

`false`. The interface holds a type, so it is not nil.

```go
a := []int{1, 2, 3, 4}
b := a[:2]
b = append(b, 99)
fmt.Println(a, b)
```

`[1 2 99 4] [1 2 99]`. `b` had spare capacity, so `append` wrote into
`a`'s array. `a[:2:2]` would have forced a copy.

```go
for i := 0; i < 3; i++ {
    defer fmt.Print(i, " ")
}
```

`2 1 0`. Last deferred runs first; each `i` was read at its `defer`.

```go
var fns []func()
for i := 0; i < 3; i++ {
    fns = append(fns, func() { fmt.Print(i, " ") })
}
for _, f := range fns { f() }
```

`0 1 2`. Before Go 1.22 it printed `3 3 3`: the loop had one `i` and
every function shared it. Interviewers still ask.

```go
items := []item{{}, {}}
for _, it := range items {
    it.done = true
}
fmt.Println(items)
```

`[{false} {false}]`. `it` is a copy. Use `items[i].done = true`.

```go
s := "héllo"
n := 0
for range s { n++ }
fmt.Println(len(s), n)
```

`6 5`. Six bytes, five characters.

```go
ch := make(chan int, 1)
ch <- 5
close(ch)
v, ok := <-ch; fmt.Println(v, ok)
v, ok = <-ch;  fmt.Println(v, ok)
```

`5 true`, then `0 false`. What was buffered is still delivered.

```go
func parse() error {
    var err error
    if true {
        _, err := strconv.Atoi("x")
        _ = err
    }
    return err
}
```

Returns `nil`. `:=` inside the block made a second `err`.

```go
func (c Counter) IncValue()    { c.n++ }
func (c *Counter) IncPointer() { c.n++ }

var k Counter
k.IncValue(); k.IncPointer()
fmt.Println(k.n)
```

`1`. The value receiver changed a copy.

```go
var m map[string]int
fmt.Println(m["x"])
m["x"] = 1
```

Prints `0`, then panics: `assignment to entry in nil map`.

## Programs you may be asked to write

Short enough for a shared editor. The project that prepares you is in
brackets.

1. A worker pool that returns results in the order of its inputs. [2]
2. Run several tasks; return the first error and cancel the rest. [2]
3. A map safe for many goroutines, with entries that expire. [2]
4. A rate limiter: at most N calls a second. [2]
5. Two goroutines printing odd and even numbers in turn. [2]
6. A pipeline of stages joined by channels, that stops cleanly. [2]
7. Merge several channels into one. [2]
8. Retry a function with backoff until a context ends. [5]
9. A least-recently-used cache. [2]
10. Logging middleware that records the status code. [3]

## Docker

Answers to these are things you did in
[docker-example/](docker-example/README.md).

**Image or container?** An image is the package: files and a command. A
container is that package running, with its own view of files, network
and processes.

**Container or virtual machine?** A container is an ordinary process on
the host's kernel, fenced in by the kernel (namespaces for what it can
see, cgroups for what it can use). A virtual machine runs a whole kernel
of its own. Containers start in milliseconds and isolate less, which is
why `coder` says a container with root is not a wall between users.

**What is a layer?** The files one instruction added. Layers are stored
once and shared between images, and reused on the next build if nothing
they depend on changed.

**Why copy `go.mod` before the rest?** So the layer that downloads
dependencies is reused until the dependencies change.

**What is a multi-stage build?** Several `FROM`s in one Dockerfile. Build
in an image that has the compiler; copy only the result into a small one.
Here that is 364 MB against 21 MB.

**Why `CGO_ENABLED=0`?** For a binary that needs no C library, so it runs
in an image that has none.

**Why not run as root?** If the program is taken over, so is root inside
the container, and a mistake in the configuration can turn that into root
on the host.

**`ENTRYPOINT` or `CMD`?** `ENTRYPOINT` is the program; `CMD` is its
default arguments, replaced by whatever follows the image name in `docker
run`.

**What does `EXPOSE` do?** Nothing but document. `-p` publishes.

**Volume or bind mount?** A volume is storage Docker keeps for you, and
is where a database's files go. A bind mount shows one of your own
folders inside the container.

**How do containers find each other?** On a network you create (Compose
creates one per project), Docker runs a name server, and a container's
name or its service's name resolves to it.

**What does `depends_on` wait for?** By default only for the other
container to start. With `condition: service_healthy` it waits for its
health check; with `service_completed_successfully`, for a job to end
with exit code 0.

**What does `-p 5432:5432` open, exactly?** The port on every network
interface of the host. `127.0.0.1:5432:5432` keeps it to the host itself.

**What does `docker stop` do?** Sends SIGTERM to process 1, waits ten
seconds, then sends SIGKILL. A program that catches SIGTERM can finish
what it is doing.

**Liveness or readiness?** "Is it alive?" and "Can it take requests?" are
different questions, and an orchestrator restarts on the first and stops
sending traffic on the second. Docker by itself has one health check and
only reports it.

**Why does the server in `coder` not run in Compose?** It creates
containers, so it needs the Docker daemon, and runs on the host where the
daemon is.
