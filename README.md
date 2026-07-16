# GoTodo

GoTodo is a self-hosted task manager built with Go, PostgreSQL, and HTMX. It focuses on simplicity and a pleasant experience: user accounts, per-user tasks, invite flow, role-based permissions, a responsive UI, and in-place HTMX interactions.

**Current version:** v0.17.0-beta

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
- HTMX for partial page updates without full reloads

## Quick start

Requirements:

- Go 1.24+
- PostgreSQL
- Node.js + npm (for frontend asset builds)

1. Copy the example env file to `.env` and edit values:

```
DB_HOST=localhost
DB_PORT=5432
DB_USER=youruser
DB_PASSWORD=yourpassword
DB_NAME=gotodo
SESSION_KEY=your-32-char-or-longer-secret-key!!
BASE_PATH=/         # optional
ASSET_VERSION=20251130  # optional; bump to force client cache refresh
```

2. Install frontend dependencies and build assets:

```bash
npm ci
npm run build:assets
```

3. Build and run:

```bash
go build -o gotodo main.go
./gotodo
```

Or run directly:

```bash
go run main.go
```

Open the app in your browser (default: http://localhost:8080).

## Frontend Assets

Pre-built minified CSS/JS are included (`internal/server/public/css/site.min.css`, `internal/server/public/js/site.min.js`) so a fresh clone works immediately.

After modifying frontend source:

```bash
npm ci
npm run build:assets
```

## Database

The app uses `github.com/jackc/pgx/v5/pgxpool`. Migrations run automatically on startup via `internal/storage/migrations.go`.

## Development

- Routes: `internal/server/server.go`
- Handlers: `internal/server/handlers`
- Templates: `internal/server/templates`
- Static assets: `internal/server/public`

Run tests:

```bash
go test ./...
```

No `.env` file is required for tests; a test session key is used automatically under `go test`.

## License

See LICENSE file.

## Architecture & deployment

Ordryn is **one binary**. Operators choose UI+API or API-only; separate web/Android repos are optional clients, not required to self-host.

| Doc | Purpose |
|-----|---------|
| [`openapi.yaml`](openapi.yaml) | Machine-readable `/api/v1` contract (OpenAPI 3) |
| [`docs/DEPLOYMENT_OPTIONS.md`](docs/DEPLOYMENT_OPTIONS.md) | `full` vs `api`, what users run |
| [`docs/LOCAL_TESTING.md`](docs/LOCAL_TESTING.md) | Local smoke tests (UI, API-only, Android against LAN) |
| [`docs/MIGRATION_SERVER_WEB_SPA.md`](docs/MIGRATION_SERVER_WEB_SPA.md) | Server Split plan (branch `cursor/server-split-f103`) |
| [`docs/REPO_SPLIT.md`](docs/REPO_SPLIT.md) | Logical ownership; optional future extracts |

## API-only mode (no web UI)

Run the JSON API without loading HTMX templates or static UI assets. Full walkthrough: [`docs/LOCAL_TESTING.md`](docs/LOCAL_TESTING.md).

```bash
export GOTODO_MODE=api   # or: go run . --mode=api
export GOTODO_BOOTSTRAP_ADMIN_EMAIL=admin@example.com
export GOTODO_BOOTSTRAP_ADMIN_PASSWORD='choose-a-strong-password'
export GOTODO_BOOTSTRAP_ENABLE_API=true
export GOTODO_BOOTSTRAP_CREATE_API_KEY=true
# Also set DB_*, SESSION_KEY, REDIS_URL (Redis required for /api/v1)

go run . --mode=api
curl -s http://localhost:8080/api/v1/health
```

On first boot, bootstrap may print a one-time API key named `bootstrap`. Use:

`Authorization: Bearer <key>` against `/api/v1/tasks` and other v1 routes.
