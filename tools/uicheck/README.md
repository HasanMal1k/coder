# uicheck

Scripts for looking at the frontend and checking it behaves, in a real
browser, without the model. They are not part of the product: nothing in
`web/` or `internal/` knows they exist, and they have their own
`package.json`.

They work by giving a throwaway account (`tmpcheck-design@example.com`)
sample chats, machines and agents, written straight into the database, so
that every screen has something on it: a question waiting, a design
proposed, an agent at work, a finished build with a table and code in it, a
chat that ended in an error. The machines are rows only. Their machine IDs
are made up, so there is no container behind them and nothing real can be
started, stopped or deleted through them. (The server will mark them as
failed a couple of minutes after the last browser leaves, when it tries to
stop them; run `seed.mjs` again to have them "running" for a picture.)

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

## Using it

Postgres, the server and the frontend must be running (`make db`, `make run`,
`make web`).

```bash
node seed.mjs      # make the account and its sample data; prints the chats
node shot.mjs shots new=/ built=chat:Crypto proposed=chat:Status \
  phone=chat:Crypto@390x844 machines=/machines          # pictures, into shots/
node check.mjs     # drive the app and report PASS or FAIL for each thing
node clean.mjs     # remove the account and everything it owns
```

`shot.mjs` explains its arguments at the top of the file. `check.mjs` covers
the start of a new chat, the skip link, a built chat (that it opens at its
end, that code can be copied, that a table keeps its words whole), the
question asked before a chat is deleted, a phone (no keyboard raised, no
sideways scroll) and reduced motion.

**The model.** Nothing here calls it, with one exception you have to ask
for: `node check.mjs --start-chat` starts a chat, which makes the server
call whatever model it was started with. Only pass it when the server was
started against a stand-in or a dead address:

```bash
DEEPSEEK_API_KEY=mock-key LLM_BASE_URL=http://localhost:9099/v1 go run ./cmd/server
```

With nothing listening on that port the chat ends in "connection refused",
which is enough to see the page become the chat's and the panel arrive.

## What it is not

It is not the end-to-end test of the assistant building something on real
machines; that needs the stand-in for the model described in
[docs/STATUS.md](../../docs/STATUS.md#how-changes-were-tested), which has
not been rebuilt. `seed.mjs` also knows the database's tables directly, so
a migration that changes `chats`, `chat_events`, `instances`, `agents`,
`architectures` or `services` may need it changed to match.

`seed.json` holds a sign-in token for the test account and is ignored by
git, along with `node_modules` and `shots`.
