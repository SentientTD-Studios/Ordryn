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

Open http://localhost:5173/

## Production build

```bash
cd web
npm ci
npm run build
```

Output lands in `web/dist`. Production assets use a relative Vite `base` so the same build works at `/` or under a subpath such as `/gotodo/`.

```bash
GOTODO_MODE=full go run .   # UI at / (or BASE_PATH)
```

### Subpath deploys (`BASE_PATH=/gotodo`)

1. Set `BASE_PATH=/gotodo` (env or `config/config.json`).
2. Rebuild is optional for pathing — the Go server injects `window.__GOTODO_BASE__` into `index.html`.
3. Proxy **without stripping** the prefix, e.g.:

```nginx
location /gotodo/ {
    proxy_pass http://127.0.0.1:8080/gotodo/;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
}
```

UI: `https://domain/gotodo/` · API: `https://domain/gotodo/api/v1`  
Legacy bookmarks under `/gotodo/app/...` redirect to `/gotodo/...`.

## Surfaces

- Auth: login / register / forgot password / reset password (session cookie)
- Tasks: create, complete, delete, undo, bulk
- Projects, tags, saved views, dashboard
- Import: CSV preview + confirm
- Settings: profile, password, calendar feed + ICS sync, export, API keys
- Device approve: `/auth/device` (legacy `/app/auth/device` redirects)
- Admin + invites (permission-gated)

## Auth

Login/register use JSON endpoints and the httpOnly session cookie (`credentials: 'include'`).
