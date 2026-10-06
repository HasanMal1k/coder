# web

The frontend. It is a chat with the assistant that designs and builds things
on your machines: your earlier chats on the left, the chat itself in the
middle, and on the right a panel showing what that chat is building, as a
graph of its parts, its frontend live, its backend's API to read and try,
and a dashboard of its machines and agents. Each machine also has a page
with an editor (VS Code), a terminal and an overview. It is a Next.js app that talks to the API directly from the
browser; there is no server-side data fetching.

## Running

The backend must be running first (see the [root README](../README.md)).

```bash
npm install
npm run dev
```

Open it as **http://coder.localhost:3000**. The editor is embedded from
`<port>-<id>.coder.localhost:8080`; with the app on `coder.localhost` too, the
browser treats them as one site, which the editor needs. The backend also only
accepts browsers from the origins in its `CORS_ALLOWED_ORIGINS` setting.

To point at a backend somewhere else, copy `.env.example` to `.env.local` and
set `NEXT_PUBLIC_API_URL`.

## Design

The interface is built from [shadcn/ui](https://ui.shadcn.com) components
(on Radix), which live in `src/components/ui/` as ordinary source files; their
corners and outlines have been edited to the shape described below. Icons are [Lucide](https://lucide.dev).

- **Colour.** Dark gray, with one orange. The page is one gray, the sidebar
  a darker one, a card a lighter one, and the project panel stands on the
  darkest (`--stage`). Orange
  (`--primary`) is for the one thing to press on a screen, for where focus
  is, and for what is live or being worked on; a list of rows does not get
  an orange button each. Green, amber and red say how something stands and
  nothing else, and the amber is yellower than the orange so the two are
  never confused. Every colour is a token in
  [globals.css](src/app/globals.css); components use the tokens
  (`bg-primary`, `text-muted-foreground`), never raw colours. To re-colour
  the app, change the tokens.
- **Shape.** Soft corners (`--radius` in the same file): 10px on buttons
  and inputs, 12px on cards, 16px on the largest surfaces; nothing is a
  pill. Surfaces are told apart by their tone first; lines are faint (a
  little white laid over whatever is beneath) and are left out where the
  tone already says it: there is none under the header, none beside the
  sidebar, and the project panel is a rounded surface set into the page
  rather than a column behind a line.
- **The diagram** ([canvas.tsx](src/components/architecture/canvas.tsx)) is
  the part to show off. A component carries the logo of what it is made
  with ([tech.tsx](src/components/architecture/tech.tsx), from Simple
  Icons, matched loosely against the design's own words: "Go + SQLite" is
  Go), in its brand's colour, lifted where that would be too dark to see. A
  connection is a plain grey line; one that requests travel along has one small orange dot
  moving from the component that asks to the one that answers; a
  connection that carries none (CI fetching code) is a thin dotted line.
  The component an agent is working on is outlined and lit in orange.
  React Flow's own mark is left off (`hideAttribution`), which its licence
  allows.
- **Type.** Geist for text and Geist Mono for names of machines, commands
  and code.
- **One theme.** There is no light theme and no switch. `<html>` carries the
  `dark` class so that the shadcn components' `dark:` styles apply. The
  terminal has its own palette to match, and the embedded editor is VS Code
  with its own dark theme. The page the server shows in place of an editor
  or a preview that isn't up (`internal/portproxy/proxy.go`) uses the same
  gray.
- **The work, shown as what it is.** The conversation is one column with no
  pictures beside it: the user's words in a box on the right, everything
  else the assistant's. What an agent does is a log, with the kind of each
  line (`run`, `write`, `edit`) in a column of its own and commands brighter
  than files. Code the assistant writes comes in a block with its language
  and a button to copy it. A table keeps its words whole and scrolls
  sideways when the page is too narrow. Figures (machines running, memory
  in use) are one box divided by lines ([stats.tsx](src/components/stats.tsx)),
  not a card each.
- **Motion** answers something the user did, or shows that something is
  happening now (a spinner, the mark at the end of a message being written,
  the dot travelling along each of the diagram's lines). Nothing animates in by
  itself. Controls name the properties they ease,
  never `transition-all`, and with "reduce motion" on, nothing eases and
  nothing repeats.
- **Keyboard and phone.** Everything that can be clicked shows where focus
  is, in orange. The first Tab on any page offers a way past the sidebar. The box you
  type in takes focus by itself only where there is a real keyboard, so
  opening a chat on a phone doesn't raise the on-screen one. Deleting a
  chat or a machine always asks first.

To add a shadcn component: `npx shadcn@latest add <name>`.

Two skills in [`.claude/skills/`](../.claude/skills/) are what this was last
gone over with, and are worth using for any change to how it looks:
`frontend-design` (Anthropic's: how to arrive at a design that isn't the
default one) and `web-design-guidelines` (Vercel's: a list of about a
hundred things an interface should get right). Where the two disagree with
the rules above, the rules above win: buttons and headings here are in
sentence case, for one, where Vercel's list asks for title case.

## Layout of the code

```
src/app/
  (auth)/login, (auth)/register     signed-out pages, sharing one layout
  (app)/layout.tsx                  everything behind sign-in: the auth gate, the providers, the sidebar
  (app)/page.tsx                    home: the start of a new chat
  (app)/chats/[id]/page.tsx         one chat, with its design beside it
  (app)/instances/[id]/page.tsx     one machine
  (app)/machines/page.tsx           every machine and every agent, as lists
  (app)/account/page.tsx
  not-found.tsx, error.tsx
src/components/
  ui/           shadcn components
  providers.tsx tooltips, notifications
  shell/        sidebar (the list of chats), page header, command palette,
                and what any page can summon
  machines/     the list page, and the create and delete dialogs
  workspace/    a machine's page: editor frame, terminal, overview
  assistant/    the chat: its page, the conversation, the transcript, the
                composer, Markdown, and the question asked before one is
                deleted
  stats.tsx     a few figures in one box, for the machines page and the
                dashboard tab
  agents/       the list of agents, on the machines page and the dashboard tab
  architecture/ the design drawn as a diagram, and the technology logos
                on it
  pane/         the panel beside the chat and its four tabs: graph,
                frontend, backend, dashboard
  auth/         sign-in form and the gate for signed-in pages
src/lib/
  api.ts          typed API client
  session.ts      the signed-in user, kept in localStorage
  instances.tsx   the user's machines, shared by every page
  chat.ts         a chat's live history, and how it is arranged for display
  agents.tsx      the user's agents, kept current, and a clock for elapsed times
  architecture.ts a chat's design, kept current
  openapi.ts      reading an API's description into what the backend tab draws
  sizes.ts        the machine sizes on offer (mirrors the server's)
  format.ts, use-polling.ts, use-title.ts, use-platform.ts,
  use-media-query.ts, use-element-size.ts
```

## How it fits together

- **One source of truth for machines.** `InstancesProvider` holds the list,
  refreshes it every five seconds while the tab is visible, and owns every
  action (create, start, stop, delete). The diagram, the machines page, a
  machine's page and the command palette all read it, so an action taken in one place
  shows everywhere at once. It lives in the `(app)` layout, so it survives
  moving between pages.
- **Things any page can summon** (the create dialog, the delete confirmation,
  the command palette) are rendered once, by `ShellProvider`, and opened
  through `useShell()`.
- **Sessions.** By default the server's tokens never expire, and the user
  stays signed in until they sign out. If the server gives tokens an expiry,
  `ShellProvider` trades each for a fresh one when it has under five minutes
  left, so the user stays signed in while the app is open; if the app was
  asleep past the expiry, the user is signed out with an explanation, and
  signing in returns them to the page they were on. The frontend tells the
  two apart from the token itself (`tokenExpiresAt`). Code that needs the
  token reads it at the moment of use (`currentToken()`) and never holds on
  to it.
- **Presence.** While a tab is open, `ShellProvider` holds a WebSocket to
  `/presence`. That connection is how the server knows the user is here; when
  the last one closes, the server stops the user's machines after a grace
  period. The server reports that period on connecting, and the app shows it
  wherever it explains the auto-stop.
- **Three parts, side by side.** The sidebar lists the user's chats, newest
  first under how long ago each was last active, with a mark on any that is
  being worked on or is waiting for an answer; each chat is one thing being
  built, at its own address (`/chats/<id>`), and `/` is the start of a new
  one. `ChatPage` ([chat-page.tsx](src/components/assistant/chat-page.tsx))
  draws the other two: the conversation, and `SidePane` to its right. A
  chat not yet started has nothing to put in the panel, so `/` has the page
  to itself and the panel arrives with the chat. The line between them can
  be dragged, or moved with the arrow keys. Whether
  the design is showing and how wide the chat is are kept by
  `AssistantProvider` and remembered per user in `localStorage`. Below
  1100 px there is no room for both, so the design slides over the chat
  when asked for, and never opens by itself.
- **Machines are reached from what they are part of.** There is no
  dashboard. A machine is opened from its node in the diagram, from the
  strip at the top of the chat that made it, or from search; the list of all
  of them (and of all agents) is a page in the account menu. A machine's
  page keeps the chats on the left and names, first in its header, the chat
  the user came from, as the way back.
- **Pages size themselves by their own width.** The area a page is drawn in
  is a CSS container (`@container/page`), and pages use container variants
  (`@3xl/page:grid-cols-4`) where they would otherwise use window
  breakpoints. The chat is a container of its own inside that, since with
  the design beside it a wide window can still mean a narrow chat.
- **Following a chat.** Sending a message returns at once; the reply is
  followed over a stream (`useChat` in [chat.ts](src/lib/chat.ts)), read with
  `fetch` because `EventSource` can't send the access token. The stream
  delivers the chat's whole history as events and then each new one, so there
  is no separate "load" step, and after a dropped connection it resumes from
  the last event seen. While the assistant is writing, the stream also
  carries the message so far, which is shown until the finished one arrives.
- **From events to what is drawn.** `buildTranscript` turns the flat list of
  events into each of the assistant's messages with the steps it took, and
  each step with its progress and outcome. A step with no outcome is "in
  progress" while the chat is running and "did not finish" otherwise, which
  is how a run that was stopped or cut off shows correctly with no extra
  state. A question shows its options, each with a line of explanation,
  until it is answered, and then shrinks to the question and the answer.
- **Four tabs on what is being built.** `SidePane`
  ([side-pane.tsx](src/components/pane/side-pane.tsx)) looks the chat's
  design up once and gives it to all four. A tab is drawn when first opened
  and kept from then on, hidden while another shows, so the page in
  Frontend stays where it was and a request half filled in under Backend is
  still there. Each chat starts on its graph. A dot on the Frontend and
  Backend tabs says theirs is up.
- **Frontend: the page, live.** The design's frontend in a frame, loaded
  through a single-use link like the editor, with a reload button, a path to
  go to, and a way out to a new tab. Until the frontend's machine exists, is
  running and is answering on its port, the tab says which of those is
  missing, and offers to start a stopped machine.
- **Backend: the API, from its own description.** Every backend is made to
  serve an OpenAPI document at `/openapi.json` (the server tells an agent
  that leaves it out). The tab reads it
  ([openapi.ts](src/lib/openapi.ts): OpenAPI 3 and Swagger 2, references
  followed, an example made from each schema) and lists the operations by
  tag: what each takes, what it can answer, and a form to send it. Reading
  the description and sending requests both go through
  `POST /instances/{id}/ports/{port}/request`, because the API's own origin
  is one this app's JavaScript cannot call. A backend with no description
  gets a button that asks the assistant to add one.
- **Dashboard: how things stand.** The machines this chat's design runs on
  (and any its agents have worked on), each with its status, what runs on it
  and whether that is answering, its editor, and start and stop; then the
  chat's agents with what each is doing. In a new chat it shows everything
  the user has.
- **The design, as a diagram.** The Graph tab draws the chat's
  design ([canvas.tsx](src/components/architecture/canvas.tsx), with React
  Flow): a node for each component, a line for each connection, in tiers by
  what each is. Before there is a design, the tab says what will appear
  there. While it is a proposal the user can change a machine's size from
  its node and approve it, in the pane or in the chat, where the reasons for
  it are set out; once it is being built each node shows its machine,
  whether its port answers, and any agent working there, and pointing at a
  node offers its editor, a preview, and a way to stop that agent. The
  design is fetched every three seconds, which is what keeps the "is it
  answering" marks current.
- **The diagram fits the room it has.** It fills the pane, and is fitted
  again whenever the pane changes size. The tiers run left to right, or top
  to bottom when that would show the design clearly larger (a tall, narrow
  pane), and each line leaves and arrives by whichever sides of its two
  components face each other, so it holds up in either direction and after
  nodes are dragged about. The diagram's own knowledge of each node (where
  it was dragged to, its measured size) is kept beside the data and handed
  back on every update, so a node isn't treated as new each time the data
  changes.
- **Agents, like a task manager.** `AgentsProvider` ([agents.tsx](src/lib/agents.tsx))
  holds the user's agents for every page, looking again every two seconds
  while any is working and every eight otherwise, and straight away when a
  chat says one started or finished. The machines page lists them with what
  each is doing this moment, for how long, and a Stop button; the sidebar
  marks a chat as being worked on while any of its agents is. When one
  finishes in a chat that isn't on screen, a notification says so, with a
  link to its chat. In the conversation, a background task stays "working"
  until the news of it arrives, whatever the chat is doing meanwhile, and a
  line marks where that news came in.
- **Everything keeps up with the assistant.** Whenever the chat's history
  says something happened to a machine (a stage of creating one, a start, a
  stop), the shared list of machines is refreshed, so the diagram changes as
  the assistant works. The chat also lists the machines it has touched, with
  their live status.
- **Previews.** A preview button opens the tab during the click and points it
  at the page once its single-use link arrives, since a tab opened after
  waiting would be blocked as a pop-up.
- **Editor.** It is VS Code served from the machine itself, on its own origin,
  so it can't read this app's session. The app asks the API for a single-use
  link and loads that in a frame; see
  [docs/API.md](../docs/API.md#ports-and-the-editor).
- **Tabs keep their state.** The editor and the terminal stay mounted while a
  machine runs; the inactive one is hidden. The editor takes keyboard focus
  when it finishes loading, even while hidden, and the terminal takes it back
  within about a tenth of a second.
- **Keyboard.** `Ctrl+K` (`⌘K` on a Mac) opens the command palette and
  `Ctrl+B` toggles the sidebar, except while typing in a terminal, where both
  keys belong to the shell, and inside the editor, which handles its own keys.
  The search button in the header opens the palette from anywhere.
- **Focus and the tabs.** shadcn's tabs switch on mouse-down, and the browser
  focuses the pressed tab when that event ends. The terminal therefore takes
  focus a moment after its tab is chosen, not during.

## Checks

```bash
npx prettier --print-width 100 --check "src/**/*.{ts,tsx,css}"
npm run lint
npx tsc --noEmit
npm run build
```

Formatting is Prettier at 100 columns; no config file sets that, so the
width is given each time.

There are no automated frontend tests in the repository. The app has been
checked in a real browser against a running backend, with tooling that is
not in the repository yet;
[docs/STATUS.md](../docs/STATUS.md#how-changes-were-tested) describes it.
