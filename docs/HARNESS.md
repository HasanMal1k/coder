# The distributed harness

Last updated: 2026-10-19.

This page is the design of how the parent assistant and the agents on
machines work together: what each does, how they reach each other, and what
they settle before any code is written. **It is a design, agreed with the
owner on 2026-10-12, and mostly not built.** What is built is the link
itself: every machine's `linkd` attaches to the control plane and stays
attached. [The table below](#order-of-work-and-where-it-stands) says how far
the rest has got, and [BACKEND.md, section 25](BACKEND.md#25-the-link)
describes what exists.
[MVP.md](MVP.md#2-the-distributed-harness) says why the product has a
distributed harness at all; [BACKEND.md, section 16](BACKEND.md#16-the-assistant)
describes what exists today.

## What is wrong today

The parent hands each machine's agent (OpenCode) a task, and agents find out
about each other afterwards: from notes, from a record each machine asks for
every three seconds, or by asking through the control plane.

- Nothing is agreed before code is written. A frontend's agent learns the
  backend's port and endpoints by looking them up once they exist.
- Nothing is pushed. Every exchange is a request and an answer.
- A busy agent cannot answer a question. It is kept until that agent's next
  task.

## What was decided

Three decisions the owner made, which shape the rest:

1. ~~**OpenCode stays as the coding agent.**~~ Reopened by the owner on
   2026-10-18, after a real build in which agents did the minimum and
   forgot their work. OpenCode is to be replaced by DeepSeek Harness
   (`dsh`), driven over ACP so the harness becomes a setting. On DeepSeek's
   own model it scores highest of the harnesses compared (Terminal-Bench 2.1,
   90.6 against Claude Code's 88.0 and OpenCode's 85.0, by DeepSeek's
   measurement), and OpenCode 1 has an open problem with DeepSeek's
   reasoning between tool calls. What stays: writing a harness of our own
   would be a large job, and Rust is for communication only. The plan and
   its order are in [STATUS.md](STATUS.md#what-to-do-next).
2. **Machines talk to each other directly**, not through the control plane.
3. **A machine's agent has the say on its own component.** The backend's
   agent knows the backend better than the parent does, so the parent
   listens to it.

One consequence to be plain about: this does **not** make a machine lighter.
OpenCode's memory stays, and it is most of a machine's (see [What was
measured](#what-was-measured)). The Rust program adds a few megabytes. What
the design saves is model calls and waiting.

## The parts

```
                 ┌──────────── control plane (Go) ────────────┐
   user ─ chat ─►│ assistant (the parent)                     │
                 │ link gateway  :8081  gRPC                  │
                 └─────▲──────────────────────────▲───────────┘
            Channel    │ (machine dials out)      │    Channel
                 ┌─────┴──── machine: web ─┐ ┌────┴─── machine: api ──┐
                 │ linkd (Rust)            │ │ linkd (Rust)           │
                 │   :7600 peers ◄─────────┼─┼──────► :7600 peers     │
                 │   127.0.0.1:7601 MCP    │ │   127.0.0.1:7601 MCP   │
                 │        ▲ tools          │ │        ▲ tools         │
                 │ OpenCode (the lead)     │ │ OpenCode (the lead)    │
                 │ guestd (Go, unchanged)  │ │ guestd (Go, unchanged) │
                 └─────────────────────────┘ └────────────────────────┘
                     Channel, direct, over the user's private network
```

- **`linkd`** is a new Rust daemon on every machine, started and kept up by
  `guestd` exactly as the editor and OpenCode are.
- It holds **one stream to the parent** (it dials out, like the tunnel) and
  **one stream to each peer** it shares a connection with in the design.
  Machines already resolve each other by name on the owner's private
  network, so a peer is `http://api:7600`.
- It gives OpenCode its new tools as a **second MCP server, on loopback**.
  The existing `team` MCP server on the control plane stays. Since
  2026-10-15 it also has the two tools through which an agent speaks to the
  owner in the chat (`post_to_chat`, `ask_owner`); those are between an
  agent and the person, so they belong on the control plane, not the link.

| | Does | Does not |
|---|---|---|
| OpenCode | Thinks, writes code, runs commands. Decides what its component offers and needs. | Change. It only gains tools. |
| `linkd` (Rust) | Carries messages, holds the contracts, tracks progress, answers "how far along are you?" itself. | Call the model, or write code. |
| Parent (Go) | Starts each stage, tells machines who their peers are, hears and records what was agreed, steps in on a disagreement. | Carry peer messages. |

## The wire: one service, one frame

One protocol for parent-to-machine and machine-to-machine, in
[`internal/linkapi/link.proto`](../internal/linkapi/link.proto). Go and Rust
both generate from that file. `Hello`, `Ping`, `Pong`, `Peers`, `Proof` and
`Status` are in it today; each other kind of frame is added with the step
that needs it, and a node ignores a kind it does not know. `Status` so far
says only which peers a machine is linked to.

```proto
service Link {
  // One long-lived stream between two nodes. Either side sends at any time.
  rpc Channel(stream Frame) returns (stream Frame);
}
message Frame {
  uint64 id = 1;         // unique on this stream
  uint64 reply_to = 2;   // the frame this answers, if any
  oneof body { ... }
}
```

| Frame | Between | Meaning |
|---|---|---|
| `Hello`, `Proof` | any two | Who I am; proof of the shared key (peers). |
| `Peers` | parent → machine | The build's components, where each runs, its connections, the mesh key, and the wording of prompts. |
| `Lead` | parent → machine | Which OpenCode session is the lead's, and when a task starts and ends. |
| `Status` | machine → peers, parent | Stage, plan items done and total, the current item, milestones. Sent on every change. |
| `Offer`, `OfferReply` | machine ↔ machine | A contract for one connection; accepted, or changes wanted. |
| `Agreed` | machine → parent | The contract both sides accepted. Both report; the two must match. |
| `Question`, `Answer` | machine ↔ machine | Something only the other agent's model can answer. |
| `Lease` | machine ↔ parent | Permission to use the model to answer a peer. |
| `Record` | machine → parent | A copy of an exchange, for the chat. |
| `Raise` | machine → parent | A disagreement, or a change to the design the parent should hear. |
| `Ping`, `Pong` | any two | Liveness. |

## A build, step by step

### 1. Clear first

After the design is approved, the parent calls a new tool,
`clear_contracts`. The platform then, for all machines at once:

- sends each `linkd` its `Peers`, and the links open streams to each other;
- gives each lead a short *prepare* task: write no code yet; for each
  connection, the side being called (the provider) writes what it offers,
  and the calling side (the consumer) says whether that covers what it needs.

The leads settle it directly:

```
api lead ── contract_offer(web, …) ──► linkd ══ Offer ══► linkd ── contract_review ──► web lead
api lead ◄── "accepted" / changes ──── linkd ◄═ Reply ═══ linkd ◄── contract_reply ─── web lead
              both links ── Agreed ──► parent: checks the two match, stores it, marks the line cleared
```

- The provider decides *how* its side works; the consumer decides *what it
  needs*. At most three rounds, then `raise_to_parent`.
- The parent is told the outcome. Its instructions say: the machine's agent
  knows its component better than you; take the contract as decided and
  correct the design to match (`update_architecture`); overrule only where
  it contradicts what the user asked for.
- **A gate, in code:** `code_on_machine` is refused for a component that
  still has an uncleared connection. Machines from an older image, with no
  link, are exempt and behave as today.

### 2. Work apart

Each lead gets its build task with its agreed contracts in front of it.

- Every link pushes its `Status` to its peers as it changes, and each link
  keeps the latest from every peer. So "how far along is the API?" is
  answered **from the asking machine's own memory**: no network round trip,
  no model call on either side.
- Progress is observed, not self-reported: `linkd` follows OpenCode's event
  stream on loopback and reads the lead's own to-do list from it.
- `wait_for_peer(api, "serving")` returns the moment the API's link says so.
- A real question (`ask_peer`) is answered **while the other agent keeps
  working**: its link forks the lead's OpenCode session, asks the fork, and
  sends the answer back. Today that question waits for the task to end.
- A contract that must change mid-build is offered again as a new version.
  The other side is told at its next `sync`.

### 3. Done

A lead marks itself done; peers see it at once. When all are done the parent
is woken, as it is today by a background agent finishing.

## Tools the lead gains

From the MCP server `link`, on loopback.

| Tool | Does | Costs a model call elsewhere? |
|---|---|---|
| `sync` | Peers' progress, and anything new for me: contract changes, answers, messages. | No |
| `peer_status(peer)` | One peer's stage, plan and milestones. | No |
| `wait_for_peer(peer, milestone)` | Waits until the peer reaches it. | No |
| `mark_milestone(name)` | Says I reached it: `serving`, `done`. | No |
| `contract_offer(peer, contract)` | Provider: puts a contract to the consumer. | Yes, the peer reviews it |
| `contract_review(peer)`, `contract_reply(peer, accept, changes)` | Consumer: reads the offer, answers it. | No |
| `contract_get(peer)` | The agreed contract. Also a file in `~/.coder/link/`. | No |
| `ask_peer(peer, question)` | Asks the peer's agent. | Yes, one |
| `raise_to_parent(text)` | Tells the parent of a disagreement or a design change. | Wakes the parent |

A tool that waits does so for at most a minute, then says "not yet, call
again", so it does not depend on how long OpenCode lets a tool call run.

## Staying small

- One thread (tokio's current-thread runtime); every queue has a fixed size.
- Dependencies: `tonic`, `prost`, `tokio` (only the features used), `hyper`
  (already inside `tonic`) for the loopback MCP server and the OpenCode
  client, `serde_json`, `hmac` and `sha2`. No `reqwest`, no `clap`, no
  logging framework, no TLS in the first version.
- Built for size: `opt-level = "z"`, LTO, one codegen unit, `panic = "abort"`,
  stripped, static (musl).
- Frames are capped (contracts 64 KB, answers 8 KB, as the Go side caps tool
  output). Event lines over 1 MB are skipped, as the Go watcher does.
- **Targets:** a binary under 4 MB and under 8 MB of memory when idle. As
  of step 2a, with the peer server in it, it is 1.25 MB and 1.3 MB. Both
  will grow as it gains the MCP server and the client for OpenCode.

## What was measured

On 2026-10-12, inside the four machines of a build run with the stand-in for
the model (`ps`, resident memory):

| Program | Never given a task | At work | At rest after one task |
|---|---|---|---|
| OpenCode (185 MB on disk) | 330 to 340 MB | 630 to 925 MB | 530 to 710 MB |
| code-server, two processes | about 75 MB each | | about 70 MB each |
| `guestd` (12 MB on disk) | about 15 MB | | |
| `linkd` (1.25 MB on disk), as of step 2a | 1.3 MB, linked to one peer | | |

A small machine has 1 GB. The database's machine stood at 96% of its limit
while its agent worked, and creating a PostgreSQL cluster there took about
fifty seconds. OpenCode does not give the memory back when the task ends.
That is a cost of keeping OpenCode, and it is worth weighing against the
size of the smallest machine.

## What going direct costs

Chosen, and worth knowing:

- **An open port between a user's machines** (7600, on their private network
  only). Peers prove themselves with a key the parent makes per chat and
  sends over the authenticated parent stream; it is held in memory, never on
  disk. The proof is a challenge and response, so the key never crosses the
  wire.
- **The parent sees only what machines report.** Each link sends a copy of
  every exchange up for the chat. Someone with root on a machine could
  withhold or alter that copy.
- **The model gateway answers a machine only during a task.** Answering a
  peer's question when idle needs a lease from the parent: at most five
  minutes, counted against the existing limit of 30 questions in ten minutes.
- **A stopped machine cannot be asked.** A build already keeps its owner's
  machines awake, so this only bites afterwards.
- **No encryption between peers** in the first version: they are on one
  private bridge. It will be needed on a provider where they are not.

## Where the code is, and will go

**Exists, since step 1**

- `internal/linkapi/`: `link.proto`, generated Go, constants. Modelled on
  `internal/guestapi/`.
- `internal/link/`: which machines have a link attached, a bounded send
  queue for each, and waking those waiting for one. Modelled on
  `internal/tunnel/hub.go`. Infrastructure.
- `internal/linkgw/`: the gRPC server for `Link.Channel`. Authenticates with
  `instance.AuthenticateGuest`, the check `teamapi` and `llmgateway` use. A
  transport. It will pass frames to the assistant; today it answers pings.
- `internal/guest/link.go`: `RunLink`, using the existing `supervise`.
- `link/`: the Rust crate. `parent.rs` is the stream to the control plane.
- `LINK_ADDR` and `LINK_CONNECT_URL` in `internal/config`, a second listener
  in `cmd/server`, and `CODER_LINK_URL` on new machines.

**To come**

- `internal/assistant/contracts.go`, `mesh.go`: the clear-first stage, the
  peers and key for a chat, the board of statuses, leases.
- `internal/store/contracts.go` and a migration: a `contracts` table, a
  `progress` column on `agents`, and `contract` among the kinds of
  `chat_events`.
- In `link/`: the peer server and client, the MCP server, the tools, and
  the client for OpenCode.
- `internal/guest/agent.go`: a second MCP server in OpenCode's
  configuration.
- `internal/assistant/tools.go`, `execute.go`, `fleet.go`, `agents.go`: the
  `clear_contracts` tool, the gate, the instructions, the preambles, and
  peer exchanges stored as the existing `message` events.

One thing went differently from the first plan. `linkd` is **not** built in
a stage of the machine image's Dockerfile: the owner asked, on 2026-10-12,
for Docker to be left out of building it. `make link` builds it on the host
and the Dockerfile copies the result in.

## Order of work, and where it stands

Each step ends with something that runs and has been looked at.

| | Step | State |
|---|---|---|
| 0 | The stand-in for the model and a browser script, in the repository | **Done**, 2026-10-12. [`tools/standin`](../tools/standin/README.md) and [`tools/uicheck/build.mjs`](../tools/uicheck/README.md). |
| 1 | A link that attaches: the protocol, the Rust skeleton, the Go listener and hub, the supervisor, the image. Its size and memory measured. | **Done**, 2026-10-12. Checked on the host and in one real machine; [STATUS.md](STATUS.md#the-link-step-1-of-the-harness) says how. |
| 2a | The mesh: `Peers`, the key, the handshake. Machines a design connects find each other, prove themselves, and report it. | **Done**, 2026-10-12. Checked with two daemons on the host and in two real machines; [STATUS.md](STATUS.md#the-mesh-step-2a-of-the-harness). |
| 2b | Progress: `Status` carrying the plan and pushed to peers; `sync`, `peer_status`, `wait_for_peer`, `mark_milestone`. | Not started |
| 3 | Contracts: the offer and reply tools, `Agreed`, the table, `clear_contracts`, the gate, the prompts. | Not started |
| 4 | Questions: `ask_peer`, leases, the forked session, records in the chat. | Not started |
| 5 | Showing it: each line of the graph says whether its contract is cleared, each node its progress. | Not started |

## To find out while building

- **Whether a fork taken while the lead is mid-step is usable.** OpenCode
  documents `POST /session/:id/fork`; it has not been tried here. If it is
  not, the question goes to a fresh session that reads the files and the
  contract.
- **Machine names are shared between chats** ([STATUS.md](STATUS.md#open-issues),
  issue 3). A machine in two chats' designs would have two sets of peers.
  Peers and keys are kept per chat so nothing breaks, but issue 3 is better
  fixed before step 3.
- **A stream that was accepted outlives the peer's being one.** If the
  control plane takes a peer away, the side that dials ends its stream; the
  side that accepted keeps one it already has until the other end goes.
- **Any machine of a mesh can claim to be any other.** They share one key.
  They are all one owner's machines in one chat, so this is accepted.
