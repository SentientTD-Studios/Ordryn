# AGENTS.md

## Cursor Cloud specific instructions

GoTodo is a single self-hosted web service: a Go (1.24) + PostgreSQL task manager
that serves HTML/HTMX pages on `http://localhost:8080`. Standard build/run/test
commands live in `README.md` and `package.json`; only the non-obvious caveats are
captured here.

### Required environment

- A `.env` file at the repo root is required to run the app and is git-ignored (it
  persists in the VM but is never committed). It must contain the `DB_*` variables
  plus `SESSION_KEY` (must be **at least 32 characters** or `sessionstore` calls
  `log.Fatal` on startup). The setup session already created this file.
- PostgreSQL 16 is installed in the VM with database `gotodo`, user `postgres`,
  password `password`. **The cluster does not auto-start** — start it before running
  the app or DB-backed tests:
  ```
  sudo pg_ctlcluster 16 main start
  ```
- Redis is optional. If `REDIS_URL`/`REDIS_ADDR` are unset it is skipped (rate
  limiting just runs in-memory); a "Redis init failed" warning is harmless.

### Running

- Dev run: `go run main.go` (serves on `:8080`; override with `PORT`).
- On an already-migrated DB, startup prints a benign warning
  `MigrateTasksAddProjectID failed: ... constraint "fk_tasks_projects" ... already exists`.
  Migrations are idempotent and the server continues normally.

### Testing

- `go test ./...` needs `SESSION_KEY` exported in the shell (each package runs from
  its own directory, so the root `.env` is NOT auto-loaded and the `sessionstore`
  `init()` will `log.Fatal` without it). Export the DB vars too:
  ```
  export SESSION_KEY=dev-session-key-32-characters-minimum-length \
         DB_HOST=localhost DB_PORT=5432 DB_USER=postgres DB_PASSWORD=password DB_NAME=gotodo
  go test ./...
  ```
- The `internal/tasks` tests spin up their own throwaway `embedded-postgres`
  (downloads a Postgres binary on first run, so they need network access); they do
  not use the `gotodo` database.
- Lint/vet: `go vet ./...`.

### Frontend assets

- Minified assets in `internal/server/public/{css,js}` are pre-built and committed.
  Only run `npm run build:assets` (after `npm ci`) if you change the CSS/JS sources
  under `internal/server/public/`.

### Signup / registration

- Signup is **invite-only by default**. To allow open self-registration (e.g. to
  create a first user), set the singleton `site_settings` row's `invite_only=false`
  (and `enable_registration=true`). New accounts are not auto-logged-in after signup;
  log in via the login modal afterward.
