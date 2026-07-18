# Local testing guide (Server Split)

Use this to verify Ordryn after Server Split changes **on one machine**, without creating extra repos.

**Prerequisites**

- Go 1.24+
- PostgreSQL
- Redis (required for `/api/v1`)
- Node/npm only if you change the Vue SPA (`npm run build:web`)

Copy `.env.example` → `.env` and set at least:

```bash
DB_HOST=localhost
DB_PORT=5432
DB_USER=...
DB_PASSWORD=...
DB_NAME=gotodo
SESSION_KEY=use-a-long-random-secret-at-least-32-chars
REDIS_URL=redis://localhost:6379/0
PORT=8080
```

---

## Option A — Full stack (UI + API)

Default self-host path.

```bash
# .env: GOTODO_MODE=full   # or leave unset
go run .
```

Open `http://localhost:8080` (redirects to `/app/`).

**Smoke checks**

1. Sign up / log in (invite-only may apply — use admin UI or disable invite-only in admin after first SQL/admin bootstrap).
2. Create a task in the SPA.
3. Profile → enable REST API (if not bootstrapped) → create an API key.
4. `curl -s http://localhost:8080/api/v1/health`
5. `curl -s -H "Authorization: Bearer YOUR_KEY" http://localhost:8080/api/v1/tasks`

---

## Option B — API only (no web UI)

Simulates “host server without a frontend.”

```bash
export GOTODO_MODE=api
export GOTODO_BOOTSTRAP_ADMIN_EMAIL=admin@example.com
export GOTODO_BOOTSTRAP_ADMIN_PASSWORD='choose-a-strong-password'
export GOTODO_BOOTSTRAP_ENABLE_API=true
export GOTODO_BOOTSTRAP_CREATE_API_KEY=true

go run . --mode=api
```

On first boot, the process prints a one-time API key named `bootstrap` (not shown again).

**Smoke checks**

```bash
# Health (no auth)
curl -s http://localhost:8080/api/v1/health | jq .

# Expect mode=api, api_enabled=true, redis_ok=true when Redis is up

# Session JSON auth (SPA-ready)
curl -s -c /tmp/gotodo.cookies -H 'Content-Type: application/json' \
  -d '{"email":"admin@example.com","password":"choose-a-strong-password"}' \
  http://localhost:8080/api/v1/auth/login | jq .

curl -s -b /tmp/gotodo.cookies http://localhost:8080/api/v1/me | jq .

# Bearer API (Android / scripts)
export KEY='paste-bootstrap-key-here'
curl -s -H "Authorization: Bearer $KEY" http://localhost:8080/api/v1/me | jq .
curl -s -H "Authorization: Bearer $KEY" \
  -H 'Content-Type: application/json' \
  -d '{"title":"API-only smoke task"}' \
  http://localhost:8080/api/v1/tasks | jq .

# Projects + tags (A3 P0)
curl -s -H "Authorization: Bearer $KEY" \
  -H 'Content-Type: application/json' \
  -d '{"name":"Smoke project"}' \
  http://localhost:8080/api/v1/projects | jq .
curl -s -H "Authorization: Bearer $KEY" \
  -H 'Content-Type: application/json' \
  -d '{"name":"smoke-tag"}' \
  http://localhost:8080/api/v1/tags | jq .

# Confirm UI routes are not registered
curl -s -o /dev/null -w '%{http_code}\n' http://localhost:8080/
# Expect 404 (no HomeHandler in api mode)
```

**Register (when invite-only is off)**

```bash
curl -s -c /tmp/gotodo.cookies -H 'Content-Type: application/json' \
  -d '{"email":"user@example.com","password":"secret","confirm_password":"secret","timezone":"UTC"}' \
  http://localhost:8080/api/v1/auth/register | jq .
```

If invite-only remains on, use an invite token field `invite_token`, or bootstrap an admin and change settings via SQL / full-mode admin once.

---

## Option C — Android (or any API client) against local Ordryn

No Ordryn code changes required.

1. Run Ordryn in **api** or **full** mode with API enabled + Redis.
2. Point the app at `http://<your-lan-ip>:8080` (not `localhost` on the phone’s own loopback).
3. Auth options:
   - Paste Bearer API key (from bootstrap or Profile), or
   - Device SSO: `POST /api/v1/auth/device/code` → open verification URL → approve in SPA at `/app/auth/device`.

Minimum server checks before app testing:

```bash
curl -s http://HOST:8080/api/v1/health
# api_enabled true, redis_ok true
```

---

## Option D — Vue SPA (`/app`)

```bash
# Terminal 1: API
GOTODO_MODE=full go run .

# Terminal 2: Vite
npm run dev:web
# → http://localhost:5173/app/
```

Production-style (built assets served by Go):

```bash
npm run build:web
GOTODO_MODE=full go run .
# → http://localhost:8080/ redirects to /app/
```

Requires API enabled + Redis (same as other `/api/v1` flows).

## Automated tests (no Postgres UI)

```bash
go test ./...
```

Package-focused:

```bash
go test ./internal/domain/ ./internal/server/handlers/ ./internal/server/utils/ ./internal/tasks/
```

OpenAPI contract (paths in `openapi.yaml` cover server registrations):

```bash
go test ./internal/server/ -run 'TestOpenAPI|TestServerAPIV1' -count=1
```

---

## Checklist before calling Server Split “locally verified”

- [ ] `go test ./...` passes
- [ ] **Full mode:** browser login + task CRUD
- [ ] **API mode:** process starts without SPA routes; `/` is not the web app
- [ ] Bootstrap creates admin + prints key once; second boot does not reprint the same key
- [ ] `GET /api/v1/health` reports `mode`, `api_enabled`, `redis_ok`
- [ ] `POST /api/v1/auth/login` + `GET /api/v1/me` (cookie)
- [ ] Bearer `GET /api/v1/tasks` and `POST /api/v1/tasks`
- [ ] Bearer project create/rename/delete and tag create/rename/delete
- [ ] `PATCH /api/v1/me`, `POST /api/v1/me/password`, API key list/create/revoke
- [ ] `POST /api/v1/tasks/bulk`, delete → `undo_token` → `POST /api/v1/tasks/undo`, `GET .../events`
- [ ] SPA: dashboard, saved views, import, settings export/calendar/sync, device approve, admin/invites (with perms), forgot/reset password
- [ ] (Optional) Android or HTTP client against LAN URL

---

## What you do *not* need for this testing

- A separate **gotodo-server** clone — this repo *is* the server
- A **gotodo-web** repo — SPA lives in `web/` and is served at `/app/`
- An Android repo — only needed if you are testing the mobile app specifically

See [`DEPLOYMENT_OPTIONS.md`](./DEPLOYMENT_OPTIONS.md) for the user-facing model.
