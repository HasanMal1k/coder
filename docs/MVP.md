# The product and its first version

Last updated: 2026-10-19.

This page says what we are building, for whom, and what the first version
people can pay for (the MVP) must do. It is the target. Where things stand
today is [STATUS.md](STATUS.md); how the system is built is
[ARCHITECTURE.md](ARCHITECTURE.md).

## In one paragraph

A user describes an application in a chat. The platform designs it with
them as a system of components (a frontend, a Go or .NET service, a
database), puts each component on its own cloud machine with the toolchain
it needs, and builds it with a team of AI agents: one agent on each machine,
directed by a parent assistant, each using the model best suited to its
work. What comes out is real code on real machines that keep running. The
user can open any machine in VS Code in the browser or in a terminal, change
anything, and keep it. They need no hardware beyond a browser.

## Who it is for

**First:** people who want a real backend system and lack the machine or the
operations skill to run one. That means indie developers building a product,
students and bootcamps, small agencies, and developers where good hardware is
expensive. They can read code, but they don't want to set up four servers to
get a working system.

**Not first:** people who never want to see code, who are already well
served by Lovable, Bolt and Replit; and professional teams with their own
machines and tooling.

**Why they would choose this over the others:**

- **A system, not a page.** Most AI builders produce a frontend with a hosted
  database behind it. This builds services in Go, .NET, C++ and Python, with
  their own databases and queues, talking over a real network.
- **Real machines they own.** Each component runs on a persistent Linux
  machine with root, VS Code and a terminal. Nothing is a sandbox that you
  export files from at the end.
- **They can see it.** The design is a live diagram showing what runs where,
  whether each part answers, and which agent is working on it. Every backend
  publishes its API, and you can call it from the page.
- **No compute of their own.** A Chromebook is enough to build and run a
  multi-service system.

## The three ideas

### 1. Machines in the cloud, with an editor

Each user has persistent Linux machines. Each one has:

- a stack image (base, Go, C++, .NET, data) with that toolchain installed;
- VS Code (code-server) and a terminal in the browser;
- a volume that survives restarts.

A user's machines share a private network and reach each other by name.
Machines nobody is connected to are stopped, so an idle user costs only
storage.

### 2. The distributed harness

A *harness* is what turns a model into an agent: its instructions, its tools,
its memory of the conversation, and the loop that runs them. This platform
has two levels of harness.

```
                        ┌──────── parent harness (control plane) ────────┐
   user ── chat ──────► │  the assistant: asks, designs, gets approval,  │
                        │  creates machines, hands out tasks, reports    │
                        └───────┬───────────────┬───────────────┬────────┘
                                │ task          │ task          │ task
                                ▼               ▼               ▼
                        ┌─ machine: web ─┐ ┌─ machine: api ─┐ ┌─ machine: db ──┐
                        │ harness+agent  │ │ harness+agent  │ │ harness+agent  │
                        │ Next.js        │ │ Go / .NET      │ │ PostgreSQL     │
                        │ model: A       │ │ model: B       │ │ model: C       │
                        └───────▲────────┘ └──▲─────────▲───┘ └───────▲────────┘
                                └── ask / notes ──┘        └── ask / notes ──┘
                                    (team record, MCP)
```

- **The parent harness** runs in the control plane. It talks to the user,
  owns the design, decides which machines exist, and gives each agent a task
  in words. It never writes code itself.
- **Each machine has its own harness** (OpenCode, kept running by the
  machine's daemon). It has its own context, its own tools (files, shell,
  git), and the machine's running services. It knows its component deeply
  and does not have to hold the whole system in its head.
- **The agents work side by side.** They ask each other questions directly
  ("which port is the API on?", "what does `/orders` return?"), leave notes
  in a shared team record, and read each other's reports. They don't need to
  go through the parent to do so.
- **The parent is woken by results.** Agents can work in the background. When
  one finishes, the parent reads its report and decides what happens next.

**Why this is better than one agent in one big machine:** each agent's
context holds one component, not the whole system, so the system can grow
beyond what one context can hold. Each component gets the right toolchain
and, under idea 3, the right model. Components are built in parallel. The
result is shaped like software that people build, one owner per service.

**What it has to prove:** that this produces working multi-service systems
more reliably than one agent does, at a cost people will pay. That is the
MVP's main test (see [Done means](#done-means)).

**Where it is going:** [HARNESS.md](HARNESS.md) is the design for the next
form of this, agreed on 2026-10-12. Agents settle what passes between their
components before any code is written, and follow each other's progress
over streams that stay open, through a small Rust program on each machine.

### 3. A model per machine, chosen for its work

No single model is best at everything. Planning a system, writing a React
page, writing a Go service, writing SQL migrations and fixing a failing CI
run are different kinds of work, and they are worth different amounts.

**The rule:** every harness has its own model. The parent uses a strong
model for reasoning and planning. Each machine's agent uses the model that
does best on its component's kind and technology, at a cost that suits it.

How it works:

- **A routing table** maps a component to a model, by its kind (frontend,
  backend, database, CI) and its technology (Next.js, Go, ASP.NET Core,
  PostgreSQL). The table is kept and tuned by us, the platform, from
  measured results. Users don't have to know model names.
- **The design shows it.** On the proposed diagram each node says which model
  will build it, beside its machine size. The user can change it before
  approving, just as they can change the size.
- **The gateway picks it.** Agents already reach the model only through the
  control plane's model gateway, which today replaces whatever model a
  request names with the server's one model
  (`internal/llmgateway/gateway.go`). Under this rule it looks up the
  machine's model instead. The machine's configuration doesn't change, and
  no key goes into a machine.
- **Several providers.** Any OpenAI-compatible provider can be listed, each
  with its key kept on the server. DeepSeek, the only one used today, stays
  one of them.
- **Cheap work goes to cheap models.** Titles, summaries and an agent's small
  steps can use a smaller model (OpenCode has a separate `small_model`
  setting for this).
- **Every call is recorded** against its user, chat, machine and model, with
  tokens and cost. That is what lets the routing table be tuned, and it is
  also how use is limited and billed.

This changes an earlier decision, "the model is DeepSeek". DeepSeek
becomes one model among several.

## What a user does in the MVP

1. Sign up and land in a chat.
2. Say what they want: "an online shop with a Go API, Postgres, and a Next.js
   storefront".
3. Answer at most three questions.
4. See the proposed system as a diagram: components, the machine and stack
   for each, connections, the model for each, and the reasons. Change sizes
   or models; approve.
5. Watch it being built. Machines appear, agents start, the diagram lights
   up where work is happening, and the chat reports progress.
6. Use the result. The Frontend tab shows the live page. The Backend tab lists
   the API, where each endpoint can be called. The Dashboard shows the
   machines and agents.
7. Open any machine in VS Code or a terminal and change anything by hand, or
   ask the assistant for a change, which goes to the right agent.
8. Come back the next day and find it all still there.

## MVP scope

What the first version must have. Status is as of 2026-10-12; see
[STATUS.md](STATUS.md) for detail.

| Area | Must have | Today |
|---|---|---|
| Machines | Create, start, stop, delete; persistent volumes; stopped when idle | Done |
| Machines | Stack images: Go, .NET, C++, data, base | Done |
| Editor | VS Code and a terminal in the browser for every machine | Done |
| Parent harness | Questions, design, approval, then building | Done |
| Machine harness | An agent on every machine; named, background, stoppable | Done |
| Distributed harness | Agents ask each other, share notes, read a team record | Done |
| Services | What agents build keeps running and restarts with the machine | Done |
| Backends | Every backend serves `/openapi.json`; the Backend tab is drawn from it | Done |
| Model per machine | Routing table, model shown and changeable on the design, gateway routes per machine | **Not started** |
| Model per machine | Several providers, keys only on the server | **Not started** |
| Use and cost | Every model call and machine hour recorded per user | **Not started** |
| Projects | Machines belong to the chat that made them (issue 3) | **Not started** |
| Isolation | A real boundary between users' machines: each a small virtual machine. Platform database closed to machines (issues 1, 6) | **Not started** |
| Machines | A machine's memory and processors can be changed after it is made | **Not started** |
| Limits | Sign-up throttling, machine quotas, spending caps (issue 5) | **Not started** |
| Reliability | Long chats kept within the model's limit; failed model calls retried (issue 10) | **Not started** |
| Accounts | Sign-up validation; a database error doesn't sign people out (issues 4, 7) | **Not started** |
| Payment | Plans, and a way to pay | **Not started** |
| Hosting | Runs on a public host with a domain and TLS | **Not started** |
| Testing | A stand-in model and an end-to-end browser test in the repository | Done |

### Isolation, cheaply

**Changed on 2026-10-18:** the owner found the cheapest suitable dedicated
server (Hetzner AX42, €97 a month) too dear to test on, and machines are to
run on **Fly.io** first. Its machines are small virtual machines already,
billed by the second (a 2 GB machine is $0.0176 an hour; a stopped one
costs only its disk), so the wall between users comes with them. The
reasoning below still holds for later: memory there costs several times a
rented server's, and once about five 2 GB machines are running around the
clock a rented server is cheaper. What follows is the earlier plan, kept for
that move.

Each machine becomes a **small virtual machine on a rented server**, in
place of a plain container: its own kernel, so one user's root is no threat
to another's machine. The owner chose this on 2026-10-12, over two
alternatives:

- **gVisor**, a safer way to run the same containers, was the earlier plan
  here. It is the smallest change, but it is not a full virtual machine,
  some of Linux does not work inside it, and builds that touch many files
  run slower.
- **Fly.io**, which rents such virtual machines, was called the long-term
  answer. Memory there costs roughly eight to ten times what a rented
  server's does while a machine runs, and memory is this product's main
  cost: the coding agent alone holds 0.5 to 0.9 GB on every machine
  ([HARNESS.md](HARNESS.md#what-was-measured)). It stays an option for when
  there is revenue and running servers is worth paying to avoid.

Which software runs the virtual machines (Kata Containers, which plugs into
Docker, or Firecracker directly) is not decided, and none has been tried
with this project's images. With Kata the change is confined to
`sandbox/docker`.

**A machine's size should be changeable.** A project grows, and its machine
has to be able to grow with it. A virtual machine's memory and processors
are numbers it is started with, so the simple way always works: stop it,
change the number, start it; its files are untouched. Today a size is fixed
when the machine is created, which is this platform's rule and not a limit
of the machines. Changing it is part of this work.

### Not in the MVP

- Deploying a user's system to production hosting outside the platform.
- CI run by the platform (agents can still set up CI on a machine).
- Teams and sharing between users.
- An MCP server for users' own AI clients.
- More than one server process.
- Users bringing their own model keys. Possible later; the MVP sells credits.

## Done means

The MVP is done when all of these hold:

1. **It builds real systems.** A fixed set of reference requests is run
   against the real models, for example:
   - a Go API with PostgreSQL and a Next.js frontend;
   - an ASP.NET Core API with PostgreSQL and Redis;
   - a Python data service with a queue and a worker.

   At least 8 runs in 10 end with a system that works (its page loads, its
   API answers as its OpenAPI document says) without anyone editing code.
   The 8-in-10 bar is a proposal; the owner sets it.
2. **The model per machine is worth it.** The same reference set, run with
   one model everywhere and run with the routing table, shows the table does
   at least as well for less money, or better for similar money.
3. **The cost of a build is known.** A typical build's model cost and machine
   hours are measured, and the price covers them.
4. **It is safe to let strangers in.** Every row of the scope table about
   isolation, limits and accounts is done.
5. **Someone has paid.** At least one person from the target group pays for
   it.

## Order of work

1. Close the database off from machines, and stop database errors signing
   people out (issues 1 and 4). Both are small.
2. Machines belong to projects (issue 3).
3. ~~Put the stand-in model and the end-to-end test in the repository, so
   changes can be checked without spending credits.~~ Done on 2026-10-12:
   `tools/standin` and `tools/uicheck/build.mjs`.
4. Record every model call and machine hour (needed for 5, 6 and billing).
5. Model per machine: routing table, gateway lookup, model shown on the
   design.
6. Run the reference set against the real models (with the owner's go-ahead,
   since it spends credits). Tune the routing table. This answers whether the
   idea works.
7. Isolation with small virtual machines on a rented server, a machine's
   size made changeable, plus limits and sign-up validation.
8. Long chats and retries.
9. Payment, a public host, the first paying users.

## Risks and open questions

- **Does splitting help?** If one strong agent in one machine builds the same
  systems as reliably for less, the distributed harness is a cost, not an
  advantage. Step 6 answers this before any money goes on launch.
- **Cost per build.** Several machines and several agents per project add up.
  Measure before setting prices.
- **Which models?** The routing table needs real candidates for each kind of
  work, chosen by measurement on the reference set, not by reputation.
- **Abuse.** Free compute attracts crypto mining and spam within days.
  Limits must be in place before sign-up opens.
- **The name.** Coder (coder.com) is an established company selling cloud
  development environments, the same market. Pick another name before
  launch.
- **Competition.** Replit, Lovable, Bolt, Cursor, OpenAI Codex and Claude Code
  all move fast. The defensible part is the distributed harness building real
  multi-service systems that the user can see and own. Keep the product
  centred on that.
- **Pricing.** Likely a monthly plan that includes machine hours and model
  credits, with more sold as needed. Not decided.
