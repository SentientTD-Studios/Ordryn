# AGENTS.md

Guidance for AI coding agents working in this repository. Human-facing docs: `README.md`
(features, extension manifest, roles, versioning), `web/README.md` (SPA development),
`tools/ollama-agent/README.md`, and `.github/CONTRIBUTING.md`.

Statements are labelled by source:

- **[implementation]**: how the code currently works.
- **[requirement]**: documented in `.github/CONTRIBUTING.md` or enforced by CI or tests.
- **[agent practice]**: recommended practice for agents working here.

## What this is

**Ordryn** (formerly GoTodo) is a self-hosted project and task manager. A single Go binary serves a
JSON API (`/api/v2`, with `/api/v1` as a compatibility alias) and a Vue 3 single-page app.

- **[implementation]** PostgreSQL is required at runtime.
- **[implementation]** Redis is optional at runtime. `STATE_STORE` (`internal/config/config.go`,
  documented in `.env.example`) decides where short-lived server state lives:
  - `redis` uses Redis and requires `REDIS_URL` or `REDIS_ADDR`.
  - `postgres` uses the main database.
  - If unset, it uses Redis when `REDIS_URL` or `REDIS_ADDR` is set, and Postgres otherwise.

  This state covers sign-out revocations, sign-in lockouts, device and SSO sign-in handshakes, undo
  tokens, rate limits, and how live updates reach other app instances.
- **[implementation]** The Go module is named `GoTodo` (imports look like `GoTodo/internal/...`).
  Do not rename it.
- **[implementation]** Environment variables accept `ORDRYN_*` and the legacy `GOTODO_*` names
  (`config.BrandEnv`).
- **[requirement]** The version comes from the git tag, stamped at build time with
  `-ldflags -X GoTodo/internal/version.Version=...`. Nothing in source is bumped for a release.

## Layout

| Path | Purpose |
| --- | --- |
| `main.go` | Startup: load config, `storage.CreateDatabase()`, `storage.RunMigrations()`, `query.BackfillSavedViews()`, `server.StartServer()` |
| `internal/server/server.go` | All route registration |
| `internal/server/handlers/` | HTTP handlers (`api_<area>_v1.go`): decode JSON, call `domain` / `storage` / `tasks`, encode JSON |
| `internal/server/utils/` | Auth chains, rate limits, cross-site and maintenance guards, Redis clients, `APIJSONError` |
| `internal/config/` | Environment loading and validation, including `STATE_STORE` |
| `internal/domain/` | Business rules and permission checks. Sentinel errors are in `errors.go` |
| `internal/storage/` | SQL (pgx v5), schema creation and migrations, connection pool |
| `internal/tasks/` | Task list and fetch queries (visibility, filters, search) |
| `internal/query/` | Saved-query language compiled to parameterized SQL |
| `internal/hooks/` | Outbound extension and chat event delivery |
| `internal/live/` | Live updates over Server-Sent Events |
| `internal/extensions/` | Loading extension `manifest.json` files |
| Other `internal/*` packages | Single-purpose helpers (recurrence, mailer, imagehost, metrics, sessionstore, crypto/secret, and others) |
| `web/` | Vue 3 + TypeScript + Vite SPA. The API client is `src/api/client.ts` and `src/api/types.ts` |
| `tools/ollama-agent/` | Standalone Node script using only Node built-ins (no npm dependencies) |
| `openapi.yaml` | The `/api/v2` contract. It is very large, so search it rather than reading it whole |

## Tracked files vs local files

| Path | In git | Notes |
| --- | --- | --- |
| `config/config.json.example`, `config/changelog.json` | tracked | |
| `config/config.json` | ignored | Local overrides. The `.gitignore` pattern `config.json` matches it at any depth |
| `data/custom-favicon.png`, `data/extensions/.gitkeep` | tracked | |
| `data/extensions/*`, `data/mods/*`, `data/uploads/` | ignored | Extensions installed by the operator, and local uploads |
| `docs/*` | ignored | Ad-hoc local drafts |
| `.env`, `tools/ollama-agent/.env`, `nodemon.json`, `RELEASE_NOTES_DRAFT.md`, `.prettierignore`, `*.exe`, `web/dist/`, `node_modules/` | ignored | Local configuration, drafts and build output |

- **[requirement]** Do not commit secrets, credentials, local configuration or generated build
  output.
- **[agent practice]** Do not change `.gitignore` to start tracking an ignored path unless the task
  asks for it.

## Running locally

See README.md "Quick start" and `web/README.md` for full steps. In short:

- `.env` (copied from `.env.example`) must set `DB_*` and `SESSION_KEY`. The server needs a
  reachable PostgreSQL.
- Redis is optional. See `STATE_STORE` and `REDIS_URL` in `.env.example`.
- `npm run build:web` runs `npm ci` and the production build in `web/`. Without `web/dist`, the
  UI path returns 503.
- Start the server with `make run` or `go run .`.
- `--mode=api` (or `ORDRYN_MODE=api`) serves only the API.

Toolchain:

- **Go:** `go.mod` declares `go 1.24.0`, and CI uses Go 1.24.
- **Node:**
  - CI builds and tests on Node 22.
  - The repo doesn't pin a version (no `engines` field and no `.nvmrc`).
  - The web test script uses `node --experimental-strip-types`, which early Node 22 releases don't
    support.
  - Prefer the latest Node 22.x to match CI unless the task calls for
  validating another version.
- **Dependencies:**
  - `web/` installs with `npm ci` from `web/package-lock.json`.
  - The root `package.json` only holds scripts and has no dependencies.
  - `tools/ollama-agent` needs no install.
  - `go build` and `go test` download Go modules unless they're already cached.

## Tests

| Suite | Command | Needs |
| --- | --- | --- |
| Go | `go test ./...`, or `go test ./internal/<pkg> -run <Name> -count=1` | Go toolchain. No separately run PostgreSQL or Redis |
| Web unit | `npm --prefix web test` | `web/node_modules` (`npm --prefix web ci`), because composable tests import `vue` |
| Web type-check and build | `npm --prefix web run build` | `web/node_modules` |
| Ollama agent | `node --test tools/ollama-agent/*.test.mjs` | Node only |
| Go, web unit and agent | `make test` (does not include the web build) | All of the above |

### Database in Go tests

**[implementation]** No Go test connects to a separately managed database. Three places start an
**embedded PostgreSQL 16** with `fergusstrange/embedded-postgres`:

| Package | Where it starts | Port | Schema setup | If Postgres cannot start |
| --- | --- | --- | --- | --- |
| `internal/domain` | `TestMain` in `workflow_test.go` | 5441 | Calls individual `storage.Create*` / `Migrate*` functions in order | Retries 3 times, then `os.Exit(1)`: the whole package fails |
| `internal/tasks` | `TestMain` in `list_test.go` | 5438 | Hand-written `CREATE TABLE` SQL in `TestMain`, then `storage.MigrateSavedViewsForQueries()` | Retries 3 times, then `os.Exit(1)`: the whole package fails |
| `internal/server/handlers` | `startImportTestDB` in `csv_import_company_test.go`, per test | 5444 | `storage.RunMigrations()` | Those tests skip |

The other handler tests run without a database. `internal/domain/site_mode_test.go` also calls
`storage.RunMigrations()` against the domain package's embedded database.

- **First run needs network access.** The library downloads PostgreSQL binaries from
  `repo1.maven.org` and caches them in `~/.embedded-postgres-go`. Later runs use the cache. Each
  instance unpacks under the OS temp directory. The fixed ports above must be free.
- **Redis.** No Go test needs a Redis server. Tests exercise the code paths without one, which
  works because Redis is an optional runtime backend. This is a separate point from Redis being
  optional for running the app.
- **Skipped vs executed.** Some tests skip instead of failing:
  - when `pg_dump` is missing (`internal/domain/backups_test.go`)
  - when the `pg_trgm` extension is missing (`internal/tasks/list_test.go`)
  - when embedded Postgres cannot start (`internal/server/handlers/csv_import_company_test.go`)
  - on certain fixture conditions (`internal/domain/email_case_test.go`, `onboarding_test.go`,
    `task_reviewer_test.go`)

  A passing package can include skipped tests. Run with `-v` and check for `--- SKIP` before
  claiming those paths ran.
- **Schema changes affect test setup.** A new table or column used by `domain` or `tasks` code may
  need one of these, or tests fail with "relation does not exist" or "column does not exist":
  - a matching call added to `TestMain` in `internal/domain/workflow_test.go`
  - a SQL change in `TestMain` in `internal/tasks/list_test.go`

### Current coverage and CI

- **[implementation]** Web unit tests cover `web/src/utils/*.test.ts` and
  `web/src/composables/*.test.ts`, and there are no Vue component tests today. Go tests sit next to
  the code in each package. This describes what exists now. It does not rule out adding other kinds
  of tests where they add value.
- **[requirement]** `.github/workflows/go-test.yml` runs on pushes and pull requests to `main`:
  - `go test ./...`
  - the OpenAPI coverage tests (see "API endpoints")
  - the SPA build

  `.github/workflows/build-assets.yml` also builds the SPA. CI does not run gofmt or a linter.
- **[requirement]** Add or update tests when changing behavior (CONTRIBUTING.md).

## Conventions

### API endpoints

1. **[implementation]** Register the route in `internal/server/server.go` with
   `handleAPI("/path", chain(handler))`. This mounts it under `/api/v2`, `/api/v1` and
   `BASE_PATH`, and wraps it with metrics, cross-site request rejection and the maintenance guard.
   - Choose the auth chain: `utils.AuthSessionChain` (signed in), `utils.AuthPublicChain`
     (anonymous) or `utils.AuthMeChain`.
   - Add `utils.RateLimitMiddleware` to endpoints that could be abused.
2. **[requirement]** Document every registered route in `openapi.yaml`.
   `internal/server/openapi_coverage_test.go` fails when a registered route is missing from the
   spec. Run it with:

   ```bash
   go test ./internal/server/ -run 'TestOpenAPI|TestServerAPIV1' -count=1
   ```

3. **[agent practice]** Following existing code, keep permission and business logic in
   `internal/domain`, not in handlers.
4. **[agent practice]** Following existing code, map domain errors through
   `utils.APIJSONError(w, status, code, message)`:

   | Error | Status | Code |
   | --- | --- | --- |
   | `domain.ErrValidation` | 400 | `invalid_request` |
   | `domain.ErrForbidden` | 403 | `forbidden` |
   | `domain.ErrNotFound` | 404 | `not_found` |
   | `domain.ErrConflict` | 409 | `conflict` |
   | any other error | 500 | `internal_error`, with a generic message that never includes the internal error |

   Resources the caller cannot see return 404, so their existence is not revealed.
5. **[agent practice]** Update `web/src/api/types.ts` and `web/src/api/client.ts` when the SPA uses
   the endpoint.

### Migrations

**[implementation]** How migrations work today:

- There is no migration framework and no table recording which migrations have run.
  `storage.RunMigrations()` (`internal/storage/migrations.go`) calls every `Create*` and
  `Migrate*` function on **every startup**, in a fixed order. Comments such as
  `// Needs tasks and tags.` record dependencies between them.
- Because every function re-runs, each must be safe to run against both a fresh database and an
  already-migrated one. Patterns in use:
  - `CREATE TABLE IF NOT EXISTS` and `ADD COLUMN IF NOT EXISTS`
  - backfills limited to rows that still need them
  - checking `information_schema` before a one-way conversion. For example,
    `MigrateSavedViewsForQueries` converts data in a transaction, then drops
    `saved_views.project_id`.
  - `DROP ... IF EXISTS` for removals (`MigrateUsersRemoveDigestSettings`)
- Errors do not stop startup. A failing function is logged and the remaining functions still run.
  If any function fails, `RunMigrations` returns an error counting the failures. `main.go` only
  prints a warning and continues, so later migrations that depend on a failed one may also fail.
- SQL is parameterized (`$1`, `$2`). Task visibility goes through `storage.TaskVisibleCondition`
  or the existing access helpers.

**[agent practice]**

- Place a new function after everything it depends on, usually at the end of `RunMigrations`.
- Before dropping anything, move or convert data that must be kept, and guard the drop so it acts
  only once. Dropped data cannot be recovered.
- Prefer adding a new function to editing an existing one. Existing functions re-run on every
  install, so an edit changes behavior on databases that already ran them. If you must edit one, preserve compatibility with already-migrated databases
  and ensure rerunning it is safe, with no unintended additional changes.
- Do not rely on startup aborting when a migration fails.

### Side effects of mutations

**[agent practice]** After a state change, do what neighboring code does:

- call `live.Push` so other tabs and users refresh
- call `hooks.Dispatch` with the specific event name. README.md "Event hooks" explains the
  `task.updated` fallback.
- write activity and audit records, notifications, and watcher updates where applicable

### Security-sensitive areas

**[agent practice]** These areas need extra care:

- auth, sessions, MFA and passkeys, SSO
- API key scopes, extension callback tokens, inbound webhook HMAC
- sandboxed extension iframes
- the saved-query compiler

Keep changes minimal and tested, and do not weaken existing checks.

**[implementation]** Secrets stored in the database are encrypted with `SESSION_KEY`
(`internal/crypto/secret`).

### Frontend

**[agent practice]** Following existing code:

- Use the Vue 3 Composition API with TypeScript.
- Keep pure logic in `web/src/utils/` or `web/src/composables/`. The unit-test runner picks up
  sibling `*.test.ts` files there.
- Make API calls through `web/src/api/client.ts`. Build URLs with `withBase()` so deployments under
  `BASE_PATH` work.
- Sanitize rendered markdown and HTML. DOMPurify is already in use.

### Style

- **[requirement]** Keep changes small, readable and testable, and prefer clear naming over clever
  abstractions.
- **[requirement]** Do not include unrelated formatting changes, drive-by refactors or generated
  build output.
- **[agent practice]** Format Go code with `gofmt`. CI does not check this.
- **[agent practice]** Match the existing package layout and file naming (`api_<area>_v1.go`,
  `<area>_test.go`).
- **[agent practice]** Comments explain why, not what.
- **[agent practice]** User-facing API error messages are short plain sentences ending with a
  period, for example `"Task not found."`.

## Investigation and evidence

**[agent practice]**

- **Read-only tasks.** Some tasks are read-only: review, audit, coverage analysis, or "draft
  without creating files". For these:
  - Make no intentional changes to repository files, application data or external systems.
  - Do not install dependencies or run migrations against a real database.
  - Stay within the permissions the task and environment grant.
  - If you run tests, say so and account for their side effects:
    - Go module downloads, and the first embedded Postgres download into `~/.embedded-postgres-go`
    - temporary Postgres processes on fixed ports
    - migrations applied to throwaway test databases
    - files written under the OS temp directory and build caches

    If those effects are not acceptable for the task, do not run the tests. Report what you learned
    from inspection only.
- **Say how you know.** Keep "I ran X and it passed or failed (with output)" separate from "from
  reading the code, X appears to...". Never present inspection as a test result. Report skipped
  tests as skipped.
- **Judge coverage by behavior.** Compare what existing tests exercise (outcomes, boundaries,
  error paths, interactions), not whether they use the same inputs. A proposed test is worthwhile
  when it adds at least one of:
  - a behavior not otherwise checked
  - a boundary or edge condition
  - an interaction between components
  - protection against a known or plausible regression

  It does not need to reach a branch that no other test reaches.
- **Trace what is relevant.** Before proposing a test or reporting a bug, follow the code path the
  claim depends on far enough to support it. That may be a single function, or a path through
  handlers, domain and storage. Name the condition or outcome the case exercises.
- **"No meaningful gap found" is a valid conclusion** when the evidence supports it. Do not invent
  findings to fill a report.

## Git workflow

- **[requirement]** `main` is the only long-lived branch. Create a short-lived branch from the
  latest `main` (`feat/*`, `fix/*`, `chore/*` or `docs/*`), and target `main` with every pull
  request. `hotfix/*` branches are for maintainers only.
- **[requirement]** Pull requests use `.github/PULL_REQUEST_TEMPLATE.md` and must pass required
  checks.
- **[agent practice]** Agents must not commit, push, tag, publish releases or open pull requests
  unless explicitly authorized.

## Before finishing

**[agent practice]** Run the checks that match the change, and report which you ran and which you
skipped.

| Change | Targeted check | Before calling it done |
| --- | --- | --- |
| Go code in one package | `go test ./internal/<pkg> -count=1`, adding `-run <Name>` while iterating | `go test ./...` |
| Routes or request and response shapes | The OpenAPI coverage tests (command under "API endpoints"), after updating `openapi.yaml` | `go test ./...` |
| Schema or migrations | Tests for the affected packages, including the test schema setup described under "Database in Go tests" | `go test ./...`, and confirm rerunning it is safe and causes no unintended additional changes |
| `web/` logic | `npm --prefix web test` | Also `npm --prefix web run build` for the type-check |
| `web/` components only | `npm --prefix web run build` | Same |
| `tools/ollama-agent` | `node --test tools/ollama-agent/<file>.test.mjs` | All `tools/ollama-agent/*.test.mjs` |
| Documentation only (`*.md` files, code comments) | None required | Check that referenced paths and commands exist and are accurate |
| Configuration (`.env.example`, `config/*.example`, CI workflows, `Makefile`, `package.json`, `go.mod`) | Run the local commands the change affects, such as the edited Make target or npm script, if the task's permissions allow it | Run the local test suites that depend on it, and note any effect that only CI can show |
| Read-only investigation | None | Report what you executed and what you only inspected |

Configuration checks are local only, and only within the task's permissions. They never authorize
any of the following:

- deploying, or pushing branches or tags
- publishing releases
- running destructive commands
- changing external services or shared state

If a check cannot run, say so and give the reason instead of reporting a pass. Common reasons:

- no network for the first embedded Postgres download
- a port in use
- missing `node_modules`

Update README.md or the extension documentation when user-visible behavior changes.
