# Ordryn web SPA (Vue 3)

Vue 3 + TypeScript + Vite client for `/api/v1`. The UI uses **Bootstrap 5**, **Bootstrap Icons**, and the original `site.css` theme (Fira Code, light/dark toggle) from the HTMX app.

## Develop

Terminal 1 — API (API must be enabled + Redis):

```bash
GOTODO_MODE=full go run .
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
GOTODO_MODE=full go run .   # "/" redirects to /app/
```

## Surfaces

- Auth: login / register / forgot password / reset password (session cookie)
- Tasks: create, complete, delete, undo, bulk
- Projects, tags, saved views, dashboard
- Import: CSV preview + confirm
- Settings: profile, password, calendar feed + ICS sync, export, API keys
- Device approve: `/app/auth/device` (legacy `/auth/device` redirects)
- Admin + invites (permission-gated)

## Auth

Login/register use JSON endpoints and the httpOnly session cookie (`credentials: 'include'`).
