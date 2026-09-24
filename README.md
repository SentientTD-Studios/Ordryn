# Ordryn

Ordryn (formerly GoTodo) is a self-hosted project and task management platform built with Go, PostgreSQL, Redis, and Vue 3. It brings personal tasks, collaborative projects, and Kanban boards together with user accounts, invite-based access, role-based permissions, and a JSON /api/v2 API for web and mobile clients (/api/v1 remains a compatibility alias).

Published versions: [GitHub Releases](https://github.com/SentientTD-Studios/Ordryn/releases).

## Features

- User signup / login / logout with forgot-password flow and optional TOTP MFA
- Editable profile (display name, timezone, tasks-per-page preference)
- Per-user tasks: add, edit, duplicate, complete, delete, drag-and-drop reorder
- Projects with rename, archive, and delete; tags with create-on-type, rename, recolor, and delete
- Kanban boards with custom statuses, estimates, claims, and named sprints (descriptions, date ranges, lock dates, and a board sprint switcher)
- Priority levels (None / Low / Medium / High) with optional sort-by-priority view
- Due dates with smart filters (today, overdue, this week, no date) and relative labels
- Search with status, tag, due-date, and priority filters; clearing those filters keeps you in the current project
- Markdown task descriptions with truncated list view and expand-in-place
- Task discussion comments with @-mentions of project members (notifies them) and #task links; authors and project owners can edit comments, with posted/edited timestamps and an admin restore log
- Bulk actions: complete, delete, move project, add/remove tag, set/clear due date, set priority, set kanban status, move sprint
- Undo delete (toast with up to 120 seconds to restore, preserves task IDs when possible)
- ICS calendar feed for due tasks; in-app calendar view; ICS import to sync due dates
- CSV import with preview/confirm; CSV/JSON export (auto-creates projects and tags on import)
- Task activity timeline in the edit sidebar
- Dashboard with overdue/today counts, completion charts, and streak tracking
- Keyboard shortcuts for power users (`?` for help)
- Invite-only registration and role-based permissions (admin, create invites)
- Admin panel: site settings, user management, global announcements, configurable image hosting (S3-compatible or local uploads)
- Dark and light themes
- Vue 3 SPA at the site root (or `BASE_PATH`, e.g. `/gotodo/`) over `/api/v2` (session cookie auth)
- Live updates over Server-Sent Events so shared projects and other tabs stay in sync without a refresh
- Drop-in **extensions** (`data/extensions/<id>/manifest.json`): outbound event hooks, custom fields, a sandboxed project panel, and scoped callback tokens for HTTP relays

## Extensions

Ordryn loads folders from `data/extensions/` (or `EXTENSIONS_DIR`) at startup. Each folder is one extension. Copy examples from `examples/extensions/` (or keep a local copy under `data/extensions/`), enable them in **Admin → Extensions**, then configure destinations where `settings.scope` allows: classic project Extensions, kanban Extensions, Profile → Integrations, or Admin for site-only hooks. Project members can open the Extensions tab to see which destinations are turned on (name, description, triggers, and the webhook hostname) without the secret, quiet hours, or other owner settings.

Bundled examples cover chat destinations (Discord, Slack, Teams, Google Chat, ntfy, generic webhook, email relay), focused hooks (due-dates including `task.due_soon`, comments + mentions, claimed, activity, lifecycle, join-requests), a personal **Mentions** ntfy destination, **Standup** (sandboxed daily check-in panel), **Retro** (kanban-tab retrospective using the document store), **Callback bot** (sandboxed panel, callback tokens, inbound `complete`/`set_field`), and custom fields (`severity`, `estimate`, `fields-demo` including `date` and `markdown`).

A `manifest.json` is the whole contract (`host_api` 1 or 2). No JavaScript or WASM plugin runtime: Ordryn delivers events to Discord, Slack, Teams, Google Chat, ntfy, or a generic HTTPS webhook, optionally serves a sandboxed HTML panel (including a kanban tab in host API 2), and can accept inbound actions.

### Identity

Optional fields help the directory look like a real product:

| Field | Purpose |
| --- | --- |
| `author` | Display name (80 chars) |
| `homepage` | `http(s)` URL |
| `license` | Short SPDX-style label (64 chars) |
| `icon` | Image file in the extension folder, shown in Admin and project settings |

### Event hooks

Declare `hooks` with `on` (and optional `label`). The UI uses the label; templates and APIs still use the event name.

Task: `task.created`, `task.updated`, `task.deleted`, `task.commented`, `task.comment_edited`, `task.comment_deleted`, `task.comment_restored`, `task.reordered`, `task.claimed`, `task.unclaimed`, `task.due_changed`, `task.moved`, `task.project_changed`, `task.sprint_changed`, `task.tagged`, `task.overdue`, `task.mentioned`, `task.completed`, `task.reopened`, `task.due_soon` (due tomorrow), `task.archived`, `task.restored`, `task.status_changed`

Project / sprint / import: `project.created`, `project.updated`, `project.deleted`, `project.archived`, `project.restored`, `project.member_joined`, `project.member_left`, `project.member_role_changed`, `project.invite_sent`, `project.invite_declined`, `sprint.created`, `sprint.updated`, `sprint.deleted`, `sprint.started`, `sprint.ended`, `import.completed`

Site (Admin): `join.request`, `join.approved`, `join.denied`

Triggers may include `*` to match every **declared** hook. Empty project/member trigger lists still mean none. `templates["*"]` is the default message when an event has no specific template.

SSE still uses `task.updated` / `project.updated` / `task.commented` so the UI refreshes. Outbound hooks use the specific event (`task.status_changed`, `task.due_changed`, `task.comment_edited`, …). Residual `task.updated` fires only for leftover field keys (title, description, priority, parent, estimate, favorite, custom fields). Destinations subscribed to `task.updated` still receive those specialized task mutations (kanban status, due date, tags, claims, complete/archive, and project/sprint moves) so chat extensions that only declare `task.updated` keep posting. A destination that selected a specific event uses that event’s template; otherwise the `task.updated` template is used.

`task.moved` still fires when a task changes project or sprint. `task.project_changed` / `task.sprint_changed` fire in addition so relays can tell them apart.

Template tokens include `{task}` `{name}` `{status}` `{old_status}` `{project}` `{actor}` `{actor_id}` `{url}` `{id}` `{priority}` `{old_priority}` `{description}` `{parent_id}` `{estimate}` `{comment}` `{claimed_by}` `{due_date}` `{sprint}` `{tags}` `{mentions}` `{member}` `{join_email}` `{event}` `{event_id}` `{occurred_at}` `{changed}` `{fields}`.

For `http.webhook` with `format: json`, the body keeps those flat fields and adds nested `actor_detail`, `task_detail`, `project_detail`, `changes`, and `digest_events`. Outbound requests send `X-Ordryn-Event-Id` and, for queued rows, `X-Ordryn-Delivery-Id`. Failed deliveries become `dead` after 8 attempts; owners can `POST …/deliveries/{id}/retry`. HTTP 429 (Discord/Google Chat rate limits) is retried using `Retry-After` / `retry_after`, and the extensions panel shows a rate-limit notice while that destination is waiting.

Status filters (`status_ids` / `status_exclude_ids`) work like tag filters. Quiet hours and digest (`hourly` / `daily`) still apply. Digest bodies list up to 10 `{name} ({event})` lines.

### Settings and fields

Setting widgets: `secret`, `bool`, `string`, `int`, `select` (requires `options`), `status`, `user`, `hook_select`, `priority`, `tag_ids`, `status_ids`, `status_exclude_ids`, `time`, `digest`, `field_filter`, `mention_map`, `project_ids`.

Each setting has `scope`: a string or array that controls **where it can be configured**. Omitted scope is `site`.

| Scope | Configurable at |
| --- | --- |
| `site` | Admin → Extensions only |
| `project` | Classic/list project Extensions tab only |
| `kanban` | Kanban board Extensions tab only |
| `user` | Profile → Integrations. Classic project owners can also use “Notify me” on the Extensions tab |

`member` is accepted as an alias of `user`. A setting can list more than one surface: `"scope": ["project", "kanban"]` is the usual team webhook (classic project and board). `"scope": "project"` does **not** include kanban boards.

Site-only extensions (for example join-request hooks) stay in Admin. User-only settings do not appear on kanban boards. Custom fields and sandboxed panels still show on both workflows.

Custom field types: `string`, `number`, `boolean`, `enum`, `url`, `user`, `date` (`YYYY-MM-DD`), `markdown`.

### Sandboxed UI

Set `ui` to an HTML file in the folder. Project members see it in an iframe (`sandbox` without `allow-same-origin`, so it cannot read the session). Extra files next to that HTML are served under `/api/v2/projects/{id}/extensions/{id}/ui/…`. Do not put secrets in the panel; use outbound JSON callbacks instead. The **Standup** example is a working check-in worksheet in that iframe.

Host API 2 adds `surfaces` so a panel can also sit on the kanban board (`at: "kanban.tab"`). Those frames still cannot read the session. The parent SPA forwards `store.get` / `store.put` / `store.list` over `postMessage` to `GET`/`PUT /api/v2/projects/{id}/extensions/{id}/store/{key}` (64 KiB JSON, optimistic `revision`, SSE `extension.store`). Permissions: `store:read`, `store:write` (viewers are read-only). The **Retro** example is a Went well / Improve / Actions board keyed to the sprint switcher.

### Callback tokens and inbound actions

`permissions` (`tasks:read`, `tasks:write`, `comments:write`) mints a **project-scoped** callback token (not a user API key). JSON deliveries include it. Relays `POST /api/v2/ext/callback` with `Authorization: Bearer …` and `{ "action": "get"|"complete"|"comment"|"set_field", "task_id": 1, … }`. Rotate it from the project Extensions tab (`controls`: `rotate_callback`).

`actions` (`complete`, `comment`, `set_field`) opt the extension into extra inbound webhook verbs on `POST /api/v2/webhooks/inbound` when the extension is enabled on that project. First-party `create` / `comment` still use the project's inbound flags. JSON **must** include `project_id`. HMAC requests send `X-Ordryn-Timestamp` (unix seconds, ±5 minutes) and `X-Ordryn-Signature: sha256=HMAC(secret, timestamp + "." + body)`. Shared secret `X-Ordryn-Webhook-Secret` is unchanged. Both inbound and callback endpoints are rate-limited.

### Delivery

`delivery.type`: `discord.webhook`, `slack.webhook`, `teams.webhook`, `googlechat.webhook`, `ntfy.webhook`, `http.webhook`. `delivery.url_from` must be a `secret` setting key. Optional `format` for HTTP: `text`, `content`, or `json`. Optional HMAC signing via `controls`: `rotate_signing`.

## Requirements

- Go 1.24+
- PostgreSQL
- Redis (required for `/api/v2` auth, rate limits, device SSO, and live updates)
- Node.js + npm (to build or develop the Vue SPA)

## Quick start

One binary serves `/api/v2` and the Vue UI at `/` (or under `BASE_PATH`).

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
- [`openapi.yaml`](openapi.yaml) — `/api/v2` contract (also `GET /openapi.yaml` on a running instance)
- [`web/README.md`](web/README.md) — Vue SPA development
- [License](LICENSE)
- Extensions authoring (including `settings.scope` of `site`, `project`, `kanban`, and `user`): see **Extensions** above. The wiki covers hosting, not the hook manifest.

SPA source lives in `web/`. Vite hot reload and tests: [Local development](https://github.com/SentientTD-Studios/Ordryn/wiki/Local-development).
