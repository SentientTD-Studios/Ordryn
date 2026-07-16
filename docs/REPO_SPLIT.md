# Repository Split Guide: Server vs Web (and App)

**Status:** Active during Server Split (`cursor/server-split-f103`)  
**Companion:** [`MIGRATION_SERVER_WEB_SPA.md`](./MIGRATION_SERVER_WEB_SPA.md)

This document specifies **what must move into new repositories** (or stay) when the monorepo is physically split. Use it when creating org-level repos after local testing on Server Split.

---

## Target repositories

| Repo (suggested name) | Role | Ships |
|-----------------------|------|-------|
| **GoTodo** / **gotodo-server** | API + domain + DB | Go binary, migrations, OpenAPI, optional digest worker |
| **gotodo-web** | SPA front end | React + Vite app; talks only to `/api/v1` |
| **gotodo-android** | Mobile client | Kotlin/Compose; already planned as separate (see `planning/ANDROID_APP.md` when present) |

Until extraction, this monorepo holds **server + legacy HTMX web**. The SPA (`web/`) will be added here first (Phase B), then can be copied to `gotodo-web`.

---

## What stays in the **server** repo

Move / keep these as the server product:

### Must include

| Path / concern | Notes |
|----------------|-------|
| `main.go` / future `cmd/gotodo` | Entry; supports `GOTODO_MODE=api\|full` |
| `internal/storage/**` | Postgres, migrations, API keys, settings |
| `internal/tasks/**` | List/filter/stats (later `internal/domain`) |
| `internal/sessionstore/**` | Cookie sessions for SPA login |
| `internal/config/**`, `internal/version/**` | Runtime config + version |
| `internal/server` API surface | `/api/v1/**`, bootstrap, digest, Redis rate limit |
| Future `internal/api/**`, `internal/domain/**` | After Phase A extraction |
| `openapi.yaml` (to add) | Contract for web + Android |
| `docs/MIGRATION_SERVER_WEB_SPA.md`, `docs/REPO_SPLIT.md` | Until migration complete |
| Go module (`go.mod`), `.env.example`, CI for `go test` | |

### Server-only runtime

- PostgreSQL, Redis (required for `/api/v1`)
- Env: `DB_*`, `SESSION_KEY`, `REDIS_URL`, `GOTODO_MODE`, `GOTODO_BOOTSTRAP_*`
- **Does not require** Node, templates, or `internal/server/public` when `GOTODO_MODE=api`

### Must NOT ship in a pure server image (api mode)

| Path | Why |
|------|-----|
| `internal/server/templates/**` | HTMX HTML (removed in Phase D; skip in api mode today) |
| `internal/server/public/**` | CSS/JS/vendor for HTMX UI |
| `package.json` / npm asset pipeline | Web-only build |
| Future `web/**` source | Belongs in gotodo-web (or served as built static from server `full` mode) |

---

## What goes in the **web** repo (`gotodo-web`)

Create when Phase B SPA exists (or extract then). Contents:

| Path / concern | Notes |
|----------------|-------|
| `package.json`, Vite, TypeScript, React | SPA toolchain |
| `src/**` | Routes, auth screens, task UI, admin UI |
| API client generated/hand-written from **server** `openapi.yaml` | Pin minimum server version in README |
| Env: `VITE_API_BASE_URL` (or similar) | Points at gotodo-server |
| CI: lint + unit + build `dist/` | |
| **No** Go code, **no** SQL migrations | |

### Web repo responsibilities

- [ ] Login / register / logout against `/api/v1/auth/*` (Phase A)
- [ ] Task/project/tag/saved-view UX against `/api/v1`
- [ ] Device approve page `/auth/device` (or server hosts a minimal page until SPA owns it)
- [ ] Document required server version (e.g. “needs GoTodo ≥ 0.19 with API enabled”)

### Web repo must NOT contain

- Database credentials or migrations
- API key hashing / Redis rate-limit implementation
- HTMX templates (those are deleted from server in Phase D, not moved)

---

## What goes in the **Android** repo

Already decided separately. Consumer of the same OpenAPI contract as web.

| Needs from server before app parity | Status |
|-------------------------------------|--------|
| `/api/v1/tasks`, projects, tags, saved-views | Exists |
| Device SSO code/token | Exists |
| `GET /api/v1/health` | **Phase 0 (this branch)** |
| `GET /api/v1/me`, register/login JSON | Phase A |
| Bootstrap / enable API without HTML | **Phase 0 (this branch)** |

---

## Extraction checklist (when cutting new repos)

### A. Create `gotodo-server` from this repo

- [ ] Copy Go tree; drop or gitignore `web/` source if SPA already extracted
- [ ] Default docs emphasize `GOTODO_MODE=api` and bootstrap env
- [ ] Publish `openapi.yaml` and version tags
- [ ] CI: `go test ./...`; optional Docker image without templates

### B. Create `gotodo-web`

- [ ] `npm create` / copy `web/` from monorepo after Phase B
- [ ] Point CI at a running server (or mocked OpenAPI)
- [ ] README: how to run against local server + production URL config
- [ ] Optional: server `full` mode continues to embed/serve `web/dist` for single-binary self-host; web repo remains source of truth for SPA

### C. Org PR flow (this effort)

1. Develop + test on `cursor/server-split-f103` in the working fork/repo  
2. Maintainer opens PR into **organization** main when ready  
3. Physical repo split can happen after Phase B (SPA exists) or after Phase D (HTMX gone) — prefer **after Phase B** for web, keep server repo as the current tree

---

## Current monorepo layout vs future

```
Today (Server Split branch)          Future
---------------------------          ------
GoTodo/                              gotodo-server/     gotodo-web/      gotodo-android/
  internal/storage ───────────────►  internal/storage
  internal/tasks ─────────────────►  internal/tasks|domain
  internal/server (API+HTMX) ─────►  internal/api (+ optional static)
  internal/server/templates ─(D)─►  DELETE
  internal/server/public ───(D)─►  DELETE (or only SPA dist)
  web/ (Phase B) ─────────────────►                    src/
  openapi.yaml ───────────────────►  openapi.yaml ──►  generate client
                                                     generate client ──►
```

---

## Phase 0 operator notes (server, already in this branch)

API-only process (no templates required):

```bash
export GOTODO_MODE=api
export GOTODO_BOOTSTRAP_ADMIN_EMAIL=admin@example.com
export GOTODO_BOOTSTRAP_ADMIN_PASSWORD='choose-a-strong-password'
export GOTODO_BOOTSTRAP_ENABLE_API=true
export GOTODO_BOOTSTRAP_CREATE_API_KEY=true
# plus DB_*, SESSION_KEY, REDIS_URL
go run . --mode=api
curl -s localhost:8080/api/v1/health
```

Bootstrap prints a one-time API key named `bootstrap` when created.

---

## Changelog

| Date | Change |
|------|--------|
| 2026-07-16 | Initial repo-split inventory for server / web / Android |
