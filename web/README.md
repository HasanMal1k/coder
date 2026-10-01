# web

The frontend: sign in, a dashboard of your machines, and a terminal for each.
It is a Next.js app that talks to the API directly from the browser; there is
no server-side data fetching.

## Running

The backend must be running first (see the [root README](../README.md)).

```bash
npm install
npm run dev        # http://localhost:3000
```

Open it as `http://localhost:3000`. The backend only accepts browsers from the
origins in its `CORS_ALLOWED_ORIGINS` setting, and `127.0.0.1` is a different
origin from `localhost`.

To point at a backend somewhere else, copy `.env.example` to `.env.local` and
set `NEXT_PUBLIC_API_URL`.

## Layout

```
src/app/                  routes: / (dashboard), /login, /register, /instances/[id]
src/components/
  dashboard.tsx           machine list, create/start/stop/delete
  workspace.tsx           one machine's page; shows the terminal when it is running
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
- **Terminal protocol.** See [docs/API.md](../docs/API.md#terminal). The token
  is sent as the first WebSocket message, because browsers can't set headers
  on a WebSocket.

## Checks

```bash
npm run lint
npm run build
```
