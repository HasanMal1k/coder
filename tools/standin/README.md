# standin

A program that answers in place of the model, so the whole platform can be
run and checked without spending credits. It speaks the OpenAI-compatible
chat completions API (the one DeepSeek speaks), which is what the assistant
and the coding agents in machines both call.

It is not part of the product. It is a Go module of its own, nothing under
`cmd/` or `internal/` imports it, `make test` does not run it, and the server
needs no change to use it: only a different address for its model.

## Using it

```bash
cd tools/standin && go run .          # listens on 127.0.0.1:9099
```

Then start the server against it. The environment wins over `.env`, so the
real key in `.env` is not used:

```bash
DEEPSEEK_API_KEY=standin LLM_BASE_URL=http://localhost:9099/v1 LLM_MODEL=standin make run
```

`LLM_MODEL=standin` matters: [`tools/uicheck/build.mjs`](../uicheck/build.mjs),
the end-to-end check, refuses to start a chat on a server whose model is
called anything else.

Machines never reach the stand-in themselves. Their agents call the
platform's model gateway as always, and the gateway forwards to wherever
`LLM_BASE_URL` points.

| Flag | Default | |
|---|---|---|
| `-addr` | `127.0.0.1:9099` | Where to listen. |
| `-pace` | `15ms` | The wait between the events of a streamed reply. Raise it to watch a reply being written. |
| `-dump` | off | A folder to write every request and reply to. The way to see exactly what the assistant or OpenCode sends. |

## What it does

It knows how to build one thing, a small online shop
([shop.go](shop.go)), and plays both sides:

**As the assistant** it asks one question, proposes a design of four
components on four machines (`web`, `api`, `db`, `ci`), proposes it again
with a larger API machine if the user asks for any change, and once the
design is approved creates the machines, gives the database and the API
their tasks together, then the storefront, then the checks in the
background. When the checks finish it shows previews and sums up. If a tool
fails it says so and stops.

**As a coding agent** it does the work for real, with OpenCode's own tools:

| Agent | Machine | Does |
|---|---|---|
| `database` | `db` | Creates a PostgreSQL cluster, registers it as a service, creates a database with three products, leaves a note. |
| `api` | `api` | Writes a Python API **without** a description of itself, registers it, is told by the platform that `/openapi.json` is missing, takes ten seconds over adding it (as a model would, so that the backend is really up without it for a while), registers it again. Commits, serves the repository with `git daemon`, leaves a note. |
| `storefront` | `web` | Asks the `api` agent what its endpoints return, writes a page that calls `/api` on its own address, serves it, commits. |
| `ci` | `ci` | Clones the API's repository from the `api` machine and checks that it compiles. |

**Written to directly** (`@tom …` in the chat), any agent lists the files it
was given with the message, from the machine's disk, and, if the message
mentions a mockup or a design, draws an SVG mockup and asks the owner to
choose with `ask_owner`, the mockup attached. Its answer says what it found.
That shows the files reaching the machine and coming back from it.

An agent that is asked a question by another agent answers it in words.

## How it decides what to say

It remembers nothing between requests. Every request to a model carries the
whole conversation, and the script works out from that how far things have
got ([script.go](script.go)):

- **Who is asking** is told from the tools on offer: only the assistant has
  `code_on_machine`, only a coding agent has `bash`. A request with no tools
  is OpenCode asking for a session's title.
- **Which agent** is read from the line the platform puts in front of every
  task (`You are "api", a coding agent on the machine "api"`). An agent keeps
  one conversation for all its tasks; the newest task is the one it is on.
- **Which step** is read from the tool calls already made. Each call's ID
  carries the name of the step it belongs to (`call_serve_1a2b3c4d`), and
  IDs come back in the conversation.

So it can be restarted at any moment, and several chats can use it at once.

Two things it is deliberately strict about, because a real model's API is:

- A tool whose schema is not valid (`"required": null`, a required name that
  is not a property) gets the request refused with `400`.
- Replies are streamed the way a model streams them: text a few words at a
  time, and each tool call's arguments in fragments that only make JSON
  once joined.

## Changing it

To have it build something else, or to exercise a new tool, add steps to the
scripts in [shop.go](shop.go). A step is a name and a tool call:

```go
case !c.asked("fill"):
    return c.bash("fill", "Create the shop database", pgFill)
```

`c.asked(step)` says whether that step's call has been made, and
`c.result(step)` gives what it came back with, which is how a script reacts
to what the platform told it. `.after(duration)` on a reply makes it take
that long to come, for where a model's thinking time is part of what is
being checked. Agents' calls go through `c.call`, which finds
the tool under whatever prefix OpenCode gives it (`team_run_service`) and
sends only the arguments that tool takes.

`go test ./...` plays each script against made-up tool results and reads a
streamed reply back the way a client does.
