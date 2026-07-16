# Ordryn web SPA (Vue 3)

Vue 3 + TypeScript + Vite client for `/api/v1`.

## Develop

Terminal 1 — API (API must be enabled + Redis):

```bash
GOTODO_MODE=full GOTODO_UI=spa go run .
```

Terminal 2 — Vite (proxies `/api` → `:8080`):

```bash
cd web
npm ci
npm run dev
```

Open http://localhost:5173/app/

## Production build

```bash
cd web
npm ci
npm run build
```

Output lands in `web/dist`. The Go server serves it at `/app/` in `full` mode.

```bash
GOTODO_UI=spa go run .   # default; "/" redirects to /app/
GOTODO_UI=htmx go run .  # legacy HTMX UI
```

## Surfaces

- Auth: login / register (session cookie)
- Tasks: create, complete, delete, undo, bulk
- Projects, tags, saved views, dashboard
- Settings: profile, password, calendar feed token, export, API keys
- Device approve: `/app/auth/device` (also via `/auth/device` redirect)
- Admin + invites (permission-gated)

## Auth

Login/register use JSON endpoints and the httpOnly session cookie (`credentials: 'include'`).
