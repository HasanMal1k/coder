# web

The frontend: sign in, a dashboard of your machines, and for each machine an
editor (VS Code) and a terminal.
It is a Next.js app that talks to the API directly from the browser; there is
no server-side data fetching.

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

## Layout

```
src/app/                  routes: / (dashboard), /login, /register, /instances/[id]
src/components/
  dashboard.tsx           machine list, create/start/stop/delete
  workspace.tsx           one machine's page: Code and Terminal tabs
  ide-view.tsx            the editor, in a frame
  terminal-view.tsx       xterm.js wired to the terminal WebSocket
  require-auth.tsx        gate for signed-in pages
src/lib/
  api.ts                  typed API client
  session.ts              the signed-in user, kept in localStorage
  use-polling.ts          refresh on an interval while the tab is visible
```

## Notes

- **Sessions.** The access token lives in `localStorage` and lasts 15 minutes;
  the backend has no refresh yet. When the server rejects a token the app signs
  out and says why. A terminal that is already open keeps working.
- **Editor.** It is VS Code served from the machine itself, on its own
  origin, so it can't read this app's session. The app asks the API for a
  single-use link and loads that in a frame; see
  [docs/API.md](../docs/API.md#ports-and-the-editor).
- **Focus.** The editor takes keyboard focus when it finishes loading, even
  while its tab is hidden. The terminal takes it back within about a tenth of
  a second.
- **Terminal protocol.** See [docs/API.md](../docs/API.md#terminal). The token
  is sent as the first WebSocket message, because browsers can't set headers
  on a WebSocket.

## Checks

```bash
npm run lint
npm run build
```
