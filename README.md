# GoTodo

GoTodo is a small, self-hosted task manager built with Go and PostgreSQL. It focuses on simplicity and a pleasant developer experience: user accounts, per-user tasks, invite flow, role-based permissions, a responsive UI, and a lightweight AJAX/HTMX interaction model.

## Features

- User signup / login / logout
- Editable profile with a single `user_name` field and timezone preference
- Per-user tasks: add, list, update status, delete
- Invite creation and confirmation (permission gated)
- Role-based permissions and a default role
- Responsive UI with Bootstrap and a dark/light theme toggle
- HTMX for in-place interactions and partial updates
- Asset cache-busting via `AssetVersion`

## Quick start

Requirements:

- Go (1.20+)
- PostgreSQL
- Node.js + npm (for frontend assets)

1. Copy the example env file to `.env` and edit values:

```
DB_HOST=localhost
DB_PORT=5432
DB_USER=youruser
DB_PASSWORD=yourpassword
DB_NAME=gotodo
BASE_PATH=/         # optional
ASSET_VERSION=20251130  # optional; bump to force client cache refresh
```

2. Install frontend dependencies and builds assets:

```powershell
npm ci
npm run build:assets
```

3. Build and run:

```powershell
go build -o app.exe main.go
.\app.exe
```

Or run directly:

```powershell
go run main.go
```

Open the app in your browser (default from code: http://localhost:8080).

## Frontend Assets

The repository includes pre-built minified CSS/JS files (internal/server/public/css/site.min.css, internal/server/public/js/site.min.js) so a fresh clone works immediately.

If you modify frontend source files (CSS/JS in internal/server/public/src/):

```powershell
npm ci
npm run build:assets
# update .asset_version if needed
Get-Date -UFormat %Y%m%d%H%M%S | Out-File -FilePath "internal/server/public/.asset_version" -Encoding utf8
git add internal/server/public/css/site.min.css internal/server/public/js/site.min.js internal/server/public/.asset_version
git commit -m "Update frontend assets"

```

## Database

The app uses `github.com/jackc/pgx/v5/pgxpool`. There are helper/migration functions in `internal/storage/database.go` you can run or inspect.

## Templates & Assets

- Templates are rendered via `utils.RenderTemplate`. The renderer injects `AssetVersion` into your templates automatically (from `ASSET_VERSION` env or fallback).
- Use `?v={{.AssetVersion}}` on CSS/JS includes to bust caches when you update assets.

## Sessions & Profile

- Sessions are managed through `sessionstore`.
- `GetSessionUserWithTimezone` returns the logged-in user's email, role, permissions, timezone, a logged-in flag, and `user_name`.
- Profile updates are handled by `/api/update-profile` (AJAX). On success the session is updated so UI reflects the change immediately.

## Theme toggle

- The dark/light theme is implemented with CSS custom properties in `site.css` and a small `site.js` script that stores the choice in `localStorage`.
- Make sure `site.js` is included on pages so the toggle initialization works correctly.

## HTMX

- HTMX is used for in-place swaps and short server-client flows. Some handlers set `HX-Redirect` for client-side redirects after an XHR-style submit.

## Troubleshooting

- Blank name after login: ensure `session.Values["user_name"]` is set during login (login handler loads it from DB).
- 500 on profile save: confirm DB has `user_name` and `timezone` columns and check server logs for the error.
- Theme toggle not persisting: ensure `site.js` is loaded and `localStorage.theme` is set.
- Asset caching: bump `ASSET_VERSION` or set `ASSET_VERSION` env var to force clients to fetch updated assets.

## Development notes

- Code is organized under `internal/` with clear separation for handlers, templates, storage, and utils.
- If you want automated asset versioning, set `ASSET_VERSION` in your CI/CD pipeline (build timestamp or git SHA).

## Where to look

- **Architecture migration (server / SPA web / app):** [`docs/MIGRATION_SERVER_WEB_SPA.md`](docs/MIGRATION_SERVER_WEB_SPA.md) — work branch `cursor/server-split-f103`
- Routes and server setup: `internal/server/server.go`
- Handlers: `internal/server/handlers`
- Templates: `internal/server/templates` (+ partials)
- Static assets: `internal/server/public`
- DB helpers and migrations: `internal/storage/database.go`
- Template helper and session utils: `internal/server/utils`

---

README file generated with AI Assistance
