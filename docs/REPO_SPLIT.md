# Code ownership: server vs web vs Android

**Status:** Active during Server Split (`cursor/server-split-f103`)  
**Companions:** [`DEPLOYMENT_OPTIONS.md`](./DEPLOYMENT_OPTIONS.md) · [`LOCAL_TESTING.md`](./LOCAL_TESTING.md) · [`MIGRATION_SERVER_WEB_SPA.md`](./MIGRATION_SERVER_WEB_SPA.md)

---

## Read this first

**Ordryn is one product.** Users decide at runtime:

- serve **API + web UI** (`GOTODO_MODE=full`), or  
- serve **API only** (`GOTODO_MODE=api`)

They do **not** need three repositories to install or test GoTodo.

| Artifact | Required? | Notes |
|----------|-----------|--------|
| This repo (Ordryn) | **Yes** | Server + optional built-in UI |
| Separate web repo | No | Optional later extract of SPA source |
| Android repo | No | Optional client of `/api/v1` |

This document describes **logical ownership** (and optional future extracts), not a mandate to split the org into three apps today.

---

## Runtime options (what users choose)

See [`DEPLOYMENT_OPTIONS.md`](./DEPLOYMENT_OPTIONS.md).

```text
Ordryn binary
 ├─ full → HTMX UI (today) + /api/v1
 └─ api  → /api/v1 only
```

---

## Logical ownership inside this repo

| Area | Path | Ships in `api` mode? | Ships in `full` mode? |
|------|------|----------------------|------------------------|
| Domain / DB | `internal/domain`, `internal/storage`, `internal/tasks` | Yes | Yes |
| JSON API | `internal/server` `/api/v1`, bootstrap, digest | Yes | Yes |
| Legacy web | `templates/`, `public/`, HTMX `/api/*` | No (not registered) | Yes |
| Future SPA source | `web/` (Phase B) | Optional static embed | Yes when built |
| Docs | `docs/*` | n/a | n/a |

---

## Optional future repositories

Create these only when you have a reason (separate release cadence, different contributors).

### gotodo-server (rename / continue this repo)

Keep as the canonical server. Same as today.

### gotodo-web (after Phase B SPA exists)

| Include | Exclude |
|---------|---------|
| React + Vite `web/` source | Go, SQL, migrations |
| Client generated from server OpenAPI | Redis/Postgres config |

Self-hosters can still use Ordryn `full` mode serving `web/dist` without cloning gotodo-web.

### gotodo-android

| Include | Exclude |
|---------|---------|
| Kotlin/Compose app | Server code |
| Config: server URL + API key / device SSO | |

---

## Extraction checklist (later — not blocking local test)

### A. Keep Ordryn as server

- [x] `GOTODO_MODE=api|full`
- [x] Bootstrap + `/api/v1/health` + session JSON auth
- [ ] `openapi.yaml` (Phase A4)
- [ ] SPA under `web/` (Phase B)

### B. Create gotodo-web (only after SPA MVP)

- [ ] Copy `web/` out
- [ ] Document `VITE_API_BASE_URL` → Ordryn
- [ ] Pin minimum server version

### C. Android repo

- [ ] Point at Ordryn `/api/v1` + health check
- [ ] Document Bearer vs device SSO (device approve needs browser UI)

---

## Changelog

| Date | Change |
|------|--------|
| 2026-07-16 | Initial inventory |
| 2026-07-16 | Clarified: one product + deploy modes; multi-repo optional |
