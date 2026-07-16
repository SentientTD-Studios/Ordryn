# GoTodo / Ordryn — Deployment options

**Product model:** One repository, one binary. Operators choose whether the process serves a web UI.

This is **not** three mandatory products. Android (and a future SPA repo) are optional *clients* of the same server.

---

## What users actually run

```text
                    ┌─────────────────────────────┐
                    │         Ordryn binary         │
                    │  PostgreSQL + Redis + SESSION │
                    └──────────────┬──────────────┘
                                   │
              ┌────────────────────┼────────────────────┐
              ▼                    ▼                    ▼
        GOTODO_MODE=full     GOTODO_MODE=api      (same binary)
        browser UI + API     JSON API only        either mode
              │                    │
              │                    ├── Android app (separate repo, optional)
              │                    └── future SPA / scripts / integrations
              └── Vue SPA at `/app/` (when `web/dist` is built)
```

| Option | Env / flag | Who it’s for | UI |
|--------|------------|--------------|-----|
| **Full** (default) | `GOTODO_MODE=full` or omit | Normal self-host | Yes — Vue SPA at `/app/` when built |
| **API only** | `GOTODO_MODE=api` or `--mode=api` | Headless / app clients / “server without frontend” | No HTML UI |

Both modes share the same database, migrations, `/api/v1`, bootstrap, and Redis requirements for the REST API.

---

## Optional separate repositories (not required to use Ordryn)

| Repo | Required to self-host? | Role |
|------|------------------------|------|
| **Ordryn** (this repo) | **Yes** | Server + optional built-in web UI |
| **gotodo-android** (or similar) | No | Mobile client of `/api/v1` |
| **gotodo-web** (future) | No | SPA source if extracted from `web/`; can still be served by Ordryn `full` mode |

You can self-host successfully with **only Ordryn**. Android and a separate web repo are for people who want those clients, not a prerequisite.

Physical package ownership for a future extract is detailed in [`REPO_SPLIT.md`](./REPO_SPLIT.md).

---

## Quick comparison

| Capability | Full mode | API mode |
|------------|-----------|----------|
| Browser task UI (Vue SPA) | Yes (when `web/dist` is built) | No |
| `/api/v1/*` JSON | Yes (if `enable_api`) | Yes (if `enable_api`) |
| Bootstrap admin / API key via env | Yes | Yes |
| `GET /api/v1/health` | Yes | Yes |
| Templates / `public/` assets required | SPA build (`web/dist`) + favicon | No |
| Redis | Required for REST API | Required for REST API |

Enable the API with admin UI (full mode) or:

```bash
GOTODO_BOOTSTRAP_ENABLE_API=true
```

---

## See also

- Local walkthrough: [`LOCAL_TESTING.md`](./LOCAL_TESTING.md)
- Architecture migration: [`MIGRATION_SERVER_WEB_SPA.md`](./MIGRATION_SERVER_WEB_SPA.md)
