# GoTodo

GoTodo (Ordryn) is a self-hosted task manager built with Go, PostgreSQL, Redis, and a Vue 3 SPA. It focuses on simplicity and a pleasant experience: user accounts, per-user tasks, invite flow, role-based permissions, and a JSON `/api/v1` for web and mobile clients.

**Current version:** v1.0.0

## Features

- User signup / login / logout with forgot-password flow
- Editable profile (display name, timezone, tasks-per-page preference)
- Per-user tasks: add, edit, duplicate, complete, delete, drag-and-drop reorder
- Projects with rename and delete; tags with create-on-type, rename, and delete
- Priority levels (None / Low / Medium / High) with optional sort-by-priority view
- Due dates with smart filters (today, overdue, this week, no date) and relative labels
- Starred tasks pinned above pagination
- Search with project, status, tag, and due-date filters
- Markdown task descriptions with truncated list view and expand-in-place
- Bulk actions: complete, delete, move project, add/remove tag, set/clear due date, set priority
- Undo delete (toast with up to 120 seconds to restore, preserves task IDs when possible)
- ICS calendar feed for due tasks; in-app calendar view; ICS import to sync due dates
- Daily email digest (opt-in on Profile)
- CSV import with preview/confirm; CSV/JSON export (auto-creates projects and tags on import)
- Task activity timeline in the edit sidebar
- Dashboard with overdue/today counts, completion charts, and streak tracking
- Keyboard shortcuts for power users (`?` for help)
- Invite-only registration and role-based permissions (admin, create invites)
- Admin panel: site settings, user management, global announcements
- Dark and light themes
- Vue 3 SPA at the site root (or `BASE_PATH`, e.g. `/gotodo/`) over `/api/v1` (session cookie auth)

## Requirements

- Go 1.24+
- PostgreSQL
- Redis (required for `/api/v1` auth, rate limits, and device SSO)
- Node.js + npm (to build or develop the Vue SPA)

## Quick start (full mode: server + SPA)

Default self-host path: one binary serves `/api/v1` and the Vue UI at `/` (or under `BASE_PATH`, e.g. `/gotodo/`).

1. Copy the example env file and edit values:

```bash
cp .env.example .env
```

**`.env` is required.** The process exits on startup if the file is missing from the working directory, or if any required variable is empty/invalid.

Required `.env` values:

```
DB_HOST=localhost
DB_PORT=5432
DB_USER=youruser
DB_PASSWORD=yourpassword
DB_NAME=gotodo
SESSION_KEY=your-32-char-or-longer-secret-key!!
REDIS_URL=redis://localhost:6379/0
```

Optional (defaults apply if unset): `PORT` (8080), `GOTODO_MODE` (full), `BASE_PATH` (`/`), `USE_HTTPS`, `MAILGUN_*` / `FROM_EMAIL`, and `GOTODO_BOOTSTRAP_*`. See [`.env.example`](.env.example).

`config/config.json` is an optional site overlay (see [`config/config.json.example`](config/config.json.example)). **Environment variables always win** over JSON for overlapping keys (including `BASE_PATH` / `USE_HTTPS`).

2. Build the Vue SPA (required for the UI; without `web/dist`, the UI path returns 503):

```bash
npm run build:web
```

3. Run the server:

```bash
go run .
```

Or build a binary:

```bash
go build -o gotodo .
./gotodo
```

Open http://localhost:8080/. For a subpath demo, set `BASE_PATH=/gotodo` and proxy `/gotodo/` to the backend (strip or preserve both work; the classic demo strips the prefix). Do not let nginx `error_page` / `proxy_intercept_errors` rewrite API JSON errors to HTML.

## Local SPA development (Vite)

Hot-reload the UI while the Go API runs locally:

```bash
# Terminal 1 — API (Redis required; API must be enabled)
GOTODO_MODE=full go run .

# Terminal 2 — Vite (proxies /api → :8080)
npm run dev:web
```

Open http://localhost:5173/

Details: [`web/README.md`](web/README.md), [`ASSETS.md`](ASSETS.md).

## API-only mode (server alone)

Run the JSON API without serving the SPA. Full walkthrough: [`docs/LOCAL_TESTING.md`](docs/LOCAL_TESTING.md).

```bash
export GOTODO_MODE=api   # or: go run . --mode=api
export GOTODO_BOOTSTRAP_ADMIN_EMAIL=admin@example.com
export GOTODO_BOOTSTRAP_ADMIN_PASSWORD='choose-a-strong-password'
export GOTODO_BOOTSTRAP_ENABLE_API=true
export GOTODO_BOOTSTRAP_CREATE_API_KEY=true
# Also set DB_*, SESSION_KEY, REDIS_URL

go run . --mode=api
curl -s http://localhost:8080/api/v1/health
```

On first boot, bootstrap may print a one-time API key named `bootstrap`. Use:

`Authorization: Bearer <key>` against `/api/v1/tasks` and other v1 routes.

## Frontend (Vue SPA)

Source lives in `web/`. Build output is `web/dist/`, served by Go in `full` mode.

After modifying the SPA for production-style serving:

```bash
npm run build:web
```

## Database

The app uses `github.com/jackc/pgx/v5/pgxpool`. Migrations run automatically on startup via `internal/storage/migrations.go`.

## Development

| Path | Role |
|------|------|
| `main.go` | Process entry |
| `internal/domain` | Shared write use-cases (tasks, projects, tags, profile) |
| `internal/storage` | Postgres pool, migrations, models |
| `internal/tasks` | List/filter/stats/export helpers |
| `internal/server` | HTTP wiring, SPA host (`spa.go`) |
| `internal/server/handlers` | `/api/v1` handlers |
| `internal/server/utils` | Auth chains, Redis, CSRF, runtime mode |
| `internal/sessionstore` | Sessions |
| `web/` | Vue 3 + TypeScript + Vite SPA |
| `openapi.yaml` | `/api/v1` contract |

Run tests:

```bash
go test ./...
```

No `.env` file is required for tests; a test session key is used automatically under `go test`.

## Architecture & deployment

Ordryn is **one binary**. Operators choose UI+API or API-only; separate web/Android repos are optional clients, not required to self-host.

| Mode | Flag / env | Serves SPA? | Use case |
|------|------------|-------------|----------|
| `full` (default) | `GOTODO_MODE=full` or omit | Yes (`web/dist` at `/` or `BASE_PATH`) | Normal self-host |
| `api` | `GOTODO_MODE=api` or `--mode=api` | No | Headless / app-only hosts |

| Doc | Purpose |
|-----|---------|
| [`openapi.yaml`](openapi.yaml) | Machine-readable `/api/v1` contract (OpenAPI 3) |
| [`web/README.md`](web/README.md) | Vue SPA, Vite proxy, subpath notes |
| [`ASSETS.md`](ASSETS.md) | Root `build:web` / `dev:web` scripts |
| [`docs/DEPLOYMENT_OPTIONS.md`](docs/DEPLOYMENT_OPTIONS.md) | `full` vs `api`, what users run |
| [`docs/LOCAL_TESTING.md`](docs/LOCAL_TESTING.md) | Local smoke tests (UI, API-only, Vite, Android against LAN) |
| [`docs/MIGRATION_SERVER_WEB_SPA.md`](docs/MIGRATION_SERVER_WEB_SPA.md) | Server Split plan (phases 0–D landed on `dev`; cleanup items remain) |
| [`docs/REPO_SPLIT.md`](docs/REPO_SPLIT.md) | Logical ownership; optional future extracts |

## Releasing

App version lives in [`internal/version/version.go`](internal/version/version.go) and is reported by `/api/v1/health`. It is **not** bumped by `go build`, `npm run build:web`, or CI.

To cut a release:

```bash
make bump-patch   # v1.0.0 -> v1.0.1 (commit + annotated tag)
make bump-minor   # v1.0.0 -> v1.1.0
make bump-major   # v1.0.0 -> v2.0.0
```

Or dry-run (updates the file only):

```bash
./scripts/bump-version.sh patch
./scripts/bump-version.sh set v1.2.3
```

Then push: `git push && git push --tags`.

OpenAPI `info.version` in [`openapi.yaml`](openapi.yaml) is independent — bump that only for large or breaking API contract changes.

## License

See LICENSE file.
