# uicheck

Scripts for looking at the frontend and checking it behaves, in a real
browser, without the real model. They are not part of the product: nothing
in `web/` or `internal/` knows they exist, and they have their own
`package.json`.

There are two kinds:

- **Looking at screens** (`seed.mjs`, `shot.mjs`, `check.mjs`): sample data
  written into the database, no model and no real machines. Quick.
- **Building something** (`build.mjs`): the end-to-end check. A real
  sign-up, real machines, the real coding agent in each, and the
  [stand-in for the model](../standin/README.md) answering in the model's
  place. About six minutes.

## Setting up

```bash
cd tools/uicheck
npm install
npx playwright install chromium
```

On this computer (WSL2) headless Chromium lacks three system libraries.
[docs/STATUS.md](../../docs/STATUS.md#how-changes-were-tested) says how to
get them without root; put the folder on `LD_LIBRARY_PATH` before running
anything here.

## Looking at screens

They work by giving a throwaway account (`tmpcheck-design@example.com`)
sample chats, machines and agents, written straight into the database, so
that every screen has something on it: a question waiting, a design
proposed, an agent at work, a finished build with a table and code in it, a
chat that ended in an error. The machines are rows only. Their machine IDs
are made up, so there is no container behind them and nothing real can be
started, stopped or deleted through them. (The server will mark them as
failed a couple of minutes after the last browser leaves, when it tries to
stop them; run `seed.mjs` again to have them "running" for a picture.)

Postgres, the server and the frontend must be running (`make db`, `make run`,
`make web`).

```bash
node seed.mjs      # make the account and its sample data; prints the chats
node shot.mjs shots new=/ built=chat:Crypto proposed=chat:Status \
  phone=chat:Crypto@390x844 machines=/machines          # pictures, into shots/
node check.mjs     # drive the app and report PASS or FAIL for each thing
node clean.mjs     # remove the account and everything it owns
```

`clean.mjs` removes any test account it is given, with its machines'
containers, volumes and network: `node clean.mjs tmpcheck-build@example.com`.
It refuses an address that is not `tmpcheck-<something>@example.com`.

`shot.mjs` explains its arguments at the top of the file. `check.mjs` covers
the start of a new chat, the skip link, a built chat (that it opens at its
end, that code can be copied, that a table keeps its words whole), the
question asked before a chat is deleted, a phone (no keyboard raised, no
sideways scroll) and reduced motion.

**The model.** Nothing here calls it, with one exception you have to ask
for: `node check.mjs --start-chat` starts a chat, which makes the server
call whatever model it was started with. Only pass it when the server was
started with `make run-standin`, never against the real one. With the
stand-in not running the chat ends in "connection refused", which is enough
to see the page become the chat's and the panel arrive.

`seed.mjs` knows the database's tables directly, so a migration that changes
`chats`, `chat_events`, `instances`, `agents`, `architectures`, `services`
or `attachments` may need it changed to match. Its brokerage chat ends with
the user writing to agents by name, with a screenshot and a text file, one
agent's answer, its question with two mockups, and another agent still
working on an answer.

## Building something

`build.mjs` signs up as `tmpcheck-build@example.com` through the form, asks
for a shop, and follows the build to its end, reporting PASS or FAIL for
about eighty things on the way:

- the empty state of each tab, the question, the design on the graph, a
  change asked for and proposed again, approval;
- the dashboard as machines are made and agents start, and the graph
  showing which component is being worked on;
- what the agents did: the API's agent told that its description was
  missing and putting it right, one agent asking another, the checks
  finishing in the background and setting the assistant going again;
- the page live in the Frontend tab, calling the API on another machine
  through its own address, and an order placed from it;
- the API in the Backend tab, drawn from its own description, with a path
  parameter, a query, a body and credentials each tried;
- tabs keeping what was in them, what the request endpoint refuses, the
  panel hidden and brought back, a second chat, and a phone.

Start the stand-in and the server against it, then run it:

```bash
make standin            # in one terminal: answers for the model, on :9099
make run-standin        # in another: the server, pointed at it
make web                # in a third: the frontend

node build.mjs                  # run it, then remove the account and machines
node build.mjs --keep           # leave them, to look at afterwards
node build.mjs --shots shots    # also save a picture of each stage
```

The machine images must have been built (`make images`): the design puts
the database on the `data` stack.

**It cannot spend credits by mistake.** Before sending anything it asks the
server which model it is using, and stops unless the answer is `standin`,
which is what `make run-standin` sets.

What it does not cover: any browser but Chromium, the editor and the
terminal, a machine being stopped and started again with its services
coming back, and anything a real model would say.

`seed.json` holds a sign-in token for the test account and is ignored by
git, along with `node_modules` and `shots`.
