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

Public URLs stay under **`/gotodo`**. The usual nginx pattern **strips** that prefix before Go (same as the old HTMX demo).

1. Set `BASE_PATH=/gotodo` (path only) in `.env`.
2. Build the SPA on the host: `cd web && rm -rf node_modules && npm ci && npm run build`
3. Use a strip-prefix proxy (do **not** turn on `proxy_intercept_errors` here — it replaces API JSON 400/401 with HTML):

```nginx
location /gotodo/ {
    proxy_pass http://127.0.0.1:9000/;  # strip /gotodo
    proxy_intercept_errors off;

    proxy_http_version 1.1;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
}
```

Browser: `/gotodo/…` · Go receives: `/…` · SPA still knows the public prefix via `<meta name="gotodo-base">`. The server rewrites Vite `./assets` URLs to absolute `/gotodo/assets/…` so nested routes work without a `<base href>` (which would break in-page `#` anchors).

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
Anonymous `GET /api/v1/me` returns HTTP 200 with JSON `null` when there is no session.
