# Ordryn

Ordryn (formerly GoTodo) is a self-hosted task manager built with Go, PostgreSQL, Redis, and a Vue 3 SPA. It focuses on simplicity and a pleasant experience: user accounts, per-user tasks, invite flow, role-based permissions, and a JSON `/api/v1` for web and mobile clients.

Published versions: [GitHub Releases](https://github.com/SentientTD-Studios/Ordryn/releases).

## Features

- User signup / login / logout with forgot-password flow and optional TOTP MFA
- Editable profile (display name, timezone, tasks-per-page preference)
- Per-user tasks: add, edit, duplicate, complete, delete, drag-and-drop reorder
- Projects with rename, archive, and delete; tags with create-on-type, rename, recolor, and delete
- Kanban boards with custom statuses, estimates, claims, and named sprints (descriptions, date ranges, lock dates, and a board sprint switcher)
- Priority levels (None / Low / Medium / High) with optional sort-by-priority view
- Due dates with smart filters (today, overdue, this week, no date) and relative labels
- Search with project, status, tag, and due-date filters
- Markdown task descriptions with truncated list view and expand-in-place
- Task discussion comments with @-mentions of project members (notifies them) and #task links; authors and project owners can edit comments, with posted/edited timestamps and an admin restore log
- Bulk actions: complete, delete, move project, add/remove tag, set/clear due date, set priority
- Undo delete (toast with up to 120 seconds to restore, preserves task IDs when possible)
- ICS calendar feed for due tasks; in-app calendar view; ICS import to sync due dates
- CSV import with preview/confirm; CSV/JSON export (auto-creates projects and tags on import)
- Task activity timeline in the edit sidebar
- Dashboard with overdue/today counts, completion charts, and streak tracking
- Keyboard shortcuts for power users (`?` for help)
- Invite-only registration and role-based permissions (admin, create invites)
- Admin panel: site settings, user management, global announcements, configurable image hosting (S3-compatible or local uploads)
- Dark and light themes
- Vue 3 SPA at the site root (or `BASE_PATH`, e.g. `/gotodo/`) over `/api/v1` (session cookie auth)
- Live updates over Server-Sent Events so shared projects and other tabs stay in sync without a refresh

## Requirements

- Go 1.24+
- PostgreSQL
- Redis (required for `/api/v1` auth, rate limits, device SSO, and live updates)
- Node.js + npm (to build or develop the Vue SPA)

## Quick start

One binary serves `/api/v1` and the Vue UI at `/` (or under `BASE_PATH`).

```bash
cp .env.example .env   # required; process will not start without it
npm run build:web      # writes web/dist; UI path is 503 without it
make run
```

Set `DB_*`, `SESSION_KEY`, and `REDIS_URL` in `.env`. Open http://localhost:8080/

`go run .` is fine too.

Install, reverse proxy, API-only mode, and upgrades: **[wiki](https://github.com/SentientTD-Studios/Ordryn/wiki)**.

## Version

The git tag is the version. Nothing in source is bumped for a release.

- **GitHub Release:** Creating a release named `vX.Y.Z` (target `main`) creates the tag and is enough. CI stamps that tag into the attached server binaries. Those binaries do not need a `.git` directory.
- **Git checkout (`git pull` / `go run` / `make run`):** The process reports the latest tag reachable from HEAD. If you only pull production when you publish a tag, a host still on the `v3.22.0` commit shows `v3.22.0`; after you tag `v4.0.0` and pull that commit, it shows `v4.0.0`.
- **Changelog:** The in-app changelog only lists GitHub releases at or below that version, so `v3.22.0` will not show `v4.0.0` notes.
- **Forks:** The fork uses **its** tags, not new tags from upstream. A fork created at `v3.22.0` keeps reporting `v3.22.0` until you tag (or fetch tags) on that fork. Syncing `main` without tags can leave you with newer code still labeled as the old tag.
- **`"dev"`:** Fallback only when the checkout has no usable tag and the binary was not stamped. Fetch tags with `git fetch --tags` if a tagged checkout still shows `dev`.

## Docs

- [Wiki](https://github.com/SentientTD-Studios/Ordryn/wiki) — self-hosting, configuration, API guide, example clients
- [`openapi.yaml`](openapi.yaml) — `/api/v1` contract (also `GET /openapi.yaml` on a running instance)
- [`web/README.md`](web/README.md) — Vue SPA development
- [License](LICENSE)

SPA source lives in `web/`. Vite hot reload and tests: [Local development](https://github.com/SentientTD-Studios/Ordryn/wiki/Local-development).
