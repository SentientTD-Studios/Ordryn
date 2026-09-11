<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { RouterLink } from 'vue-router'
import { pathPrefix } from '@/base'
import { useAuth } from '@/composables/useAuth'
import { useSite } from '@/composables/useSite'

const { isAuthenticated } = useAuth()
const { siteName, refresh: refreshSite } = useSite()
const basePath = ref(pathPrefix())

onMounted(() => {
  document.body.classList.add('api-docs-page')
  void refreshSite()
})

onUnmounted(() => {
  document.body.classList.remove('api-docs-page')
})
</script>

<template>
<div class="container mt-3">
            <div class="card">
                <div class="card-body">
                    <h1 class="card-title">REST API v1</h1>
                    <p class="lead">
                        Machine-readable JSON API for managing your tasks, saved views, projects, and tags in {{ siteName }}.
                    </p>

                    <div v-if="!isAuthenticated" class="alert alert-info" role="alert">
                        <RouterLink to="/login">Log in</RouterLink>
                        and create an API key on your
                        <RouterLink :to="{ path: '/settings', hash: '#developer' }">settings</RouterLink>
                        page to start using the API.
                    </div>
                    <div v-else class="alert alert-info" role="alert">
                        Create and manage API keys on your
                        <RouterLink :to="{ path: '/settings', hash: '#developer' }">settings</RouterLink>
                        page.
                        Machine-readable OpenAPI spec:
                        <a :href="`${basePath}/openapi.yaml`"><code>/openapi.yaml</code></a>.
                    </div>

                    <nav class="api-docs-toc mb-4" aria-label="API documentation sections">
                        <ul class="list-inline mb-0">
                            <li class="list-inline-item"><a href="#overview">Overview</a></li>
                            <li class="list-inline-item"><a href="#authentication">Authentication</a></li>
                            <li class="list-inline-item"><a href="#session-auth">Session auth (SPA)</a></li>
                            <li class="list-inline-item"><a href="#device-auth">Device authorization</a></li>
                            <li class="list-inline-item"><a href="#errors">Errors</a></li>
                            <li class="list-inline-item"><a href="#rate-limits">Rate limits</a></li>
                            <li class="list-inline-item"><a href="#tasks">Tasks</a></li>
                            <li class="list-inline-item"><a href="#saved-views">Saved views</a></li>
                            <li class="list-inline-item"><a href="#projects">Projects</a></li>
                            <li class="list-inline-item"><a href="#tags">Tags</a></li>
                            <li class="list-inline-item"><a href="#images">Images</a></li>
                        </ul>
                    </nav>

                    <h2 id="overview" class="h4 mt-4">Overview</h2>
                    <p>
                        All endpoints live under <code>{{ basePath }}/api/v1/</code> and return JSON.
                        Requests must include a valid API key (see <a href="#authentication">Authentication</a>).
                        Date and time fields use your account timezone for display; due dates are stored and returned as <code>YYYY-MM-DD</code>.
                    </p>
                    <table class="table table-sm api-docs-table">
                        <thead>
                            <tr>
                                <th>Method</th>
                                <th>Path</th>
                                <th>Description</th>
                            </tr>
                        </thead>
                        <tbody>
                            <tr><td><span class="badge bg-success">GET</span></td><td><a href="#overview"><code>/api/v1/health</code></a></td><td>Public health probe</td></tr>
                            <tr><td><span class="badge bg-primary">POST</span></td><td><a href="#session-auth"><code>/api/v1/auth/register</code></a></td><td>Register (session cookie)</td></tr>
                            <tr><td><span class="badge bg-primary">POST</span></td><td><a href="#session-auth"><code>/api/v1/auth/login</code></a></td><td>Login (session cookie; may require MFA)</td></tr>
                            <tr><td><span class="badge bg-primary">POST</span></td><td><a href="#session-auth"><code>/api/v1/auth/mfa/verify</code></a></td><td>Complete MFA login</td></tr>
                            <tr><td><span class="badge bg-primary">POST</span></td><td><a href="#session-auth"><code>/api/v1/auth/logout</code></a></td><td>Clear session cookie</td></tr>
                            <tr><td><span class="badge bg-success">GET</span></td><td><a href="#session-auth"><code>/api/v1/auth/username-available</code></a></td><td>Check username availability</td></tr>
                            <tr><td><span class="badge bg-success">GET</span></td><td><a href="#session-auth"><code>/api/v1/users/search</code></a></td><td>Search usernames for project invites or discussion mentions</td></tr>
                            <tr><td><span class="badge bg-success">GET</span> <span class="badge bg-warning text-dark">PATCH</span></td><td><a href="#session-auth"><code>/api/v1/me</code></a></td><td>Current user / update profile prefs</td></tr>
                            <tr><td><span class="badge bg-primary">POST</span></td><td><a href="#session-auth"><code>/api/v1/me/username</code></a></td><td>One-time username claim</td></tr>
                            <tr><td><span class="badge bg-primary">POST</span></td><td><a href="#session-auth"><code>/api/v1/me/password</code></a></td><td>Change password</td></tr>
                            <tr><td><span class="badge bg-success">GET</span></td><td><a href="#session-auth"><code>/api/v1/me/mfa</code></a></td><td>MFA status</td></tr>
                            <tr><td><span class="badge bg-primary">POST</span></td><td><a href="#session-auth"><code>/api/v1/me/mfa/setup</code></a></td><td>Start MFA enrollment</td></tr>
                            <tr><td><span class="badge bg-primary">POST</span></td><td><a href="#session-auth"><code>/api/v1/me/mfa/enable</code></a></td><td>Confirm TOTP and enable MFA</td></tr>
                            <tr><td><span class="badge bg-primary">POST</span></td><td><a href="#session-auth"><code>/api/v1/me/mfa/disable</code></a></td><td>Disable MFA</td></tr>
                            <tr><td><span class="badge bg-primary">POST</span></td><td><a href="#session-auth"><code>/api/v1/me/mfa/recovery-codes</code></a></td><td>Replace recovery codes</td></tr>
                            <tr><td><span class="badge bg-success">GET</span> <span class="badge bg-primary">POST</span></td><td><a href="#session-auth"><code>/api/v1/api-keys</code></a></td><td>List / create API keys</td></tr>
                            <tr><td><span class="badge bg-warning text-dark">PATCH</span> <span class="badge bg-danger">DELETE</span></td><td><a href="#session-auth"><code>/api/v1/api-keys/{id}</code></a></td><td>Rename or revoke an API key</td></tr>
                            <tr><td><span class="badge bg-success">GET</span></td><td><a href="#tasks"><code>/api/v1/tasks</code></a></td><td>List tasks (with filters and pagination)</td></tr>
                            <tr><td><span class="badge bg-primary">POST</span></td><td><a href="#tasks"><code>/api/v1/tasks</code></a></td><td>Create a task</td></tr>
                            <tr><td><span class="badge bg-success">GET</span></td><td><a href="#tasks"><code>/api/v1/tasks/{id}</code></a></td><td>Get one task</td></tr>
                            <tr><td><span class="badge bg-warning text-dark">PATCH</span></td><td><a href="#tasks"><code>/api/v1/tasks/{id}</code></a></td><td>Update a task</td></tr>
                            <tr><td><span class="badge bg-danger">DELETE</span></td><td><a href="#tasks"><code>/api/v1/tasks/{id}</code></a></td><td>Delete a task (permanent; may return undo_token)</td></tr>
                            <tr><td><span class="badge bg-primary">POST</span></td><td><a href="#tasks"><code>/api/v1/tasks/{id}/archive</code></a></td><td>Archive a task (applies protected archived tag)</td></tr>
                            <tr><td><span class="badge bg-primary">POST</span></td><td><a href="#tasks"><code>/api/v1/tasks/{id}/restore</code></a></td><td>Restore an archived task</td></tr>
                            <tr><td><span class="badge bg-primary">POST</span></td><td><a href="#tasks"><code>/api/v1/tasks/reorder</code></a></td><td>Reorder tasks (favorite grouping is deprecated)</td></tr>
                            <tr><td><span class="badge bg-primary">POST</span></td><td><a href="#tasks"><code>/api/v1/tasks/bulk</code></a></td><td>Bulk actions</td></tr>
                            <tr><td><span class="badge bg-primary">POST</span></td><td><a href="#tasks"><code>/api/v1/tasks/undo</code></a></td><td>Restore deleted tasks via undo_token</td></tr>
                            <tr><td><span class="badge bg-success">GET</span></td><td><a href="#tasks"><code>/api/v1/tasks/{id}/events</code></a></td><td>Task activity timeline</td></tr>
                            <tr><td><span class="badge bg-success">GET</span></td><td><a href="#saved-views"><code>/api/v1/saved-views</code></a></td><td>List saved views</td></tr>
                            <tr><td><span class="badge bg-primary">POST</span></td><td><a href="#saved-views"><code>/api/v1/saved-views</code></a></td><td>Create a saved view</td></tr>
                            <tr><td><span class="badge bg-success">GET</span></td><td><a href="#saved-views"><code>/api/v1/saved-views/{id}</code></a></td><td>Get one saved view</td></tr>
                            <tr><td><span class="badge bg-info text-dark">PUT</span> <span class="badge bg-warning text-dark">PATCH</span></td><td><a href="#saved-views"><code>/api/v1/saved-views/{id}</code></a></td><td>Replace or update a saved view</td></tr>
                            <tr><td><span class="badge bg-danger">DELETE</span></td><td><a href="#saved-views"><code>/api/v1/saved-views/{id}</code></a></td><td>Delete a saved view</td></tr>
                            <tr><td><span class="badge bg-success">GET</span></td><td><a href="#projects"><code>/api/v1/projects</code></a></td><td>List projects</td></tr>
                            <tr><td><span class="badge bg-primary">POST</span></td><td><a href="#projects"><code>/api/v1/projects</code></a></td><td>Create a project</td></tr>
                            <tr><td><span class="badge bg-warning text-dark">PATCH</span></td><td><a href="#projects"><code>/api/v1/projects/{id}</code></a></td><td>Rename a project</td></tr>
                            <tr><td><span class="badge bg-primary">POST</span></td><td><a href="#projects"><code>/api/v1/projects/{id}/archive</code></a></td><td>Archive a project (owner only)</td></tr>
                            <tr><td><span class="badge bg-primary">POST</span></td><td><a href="#projects"><code>/api/v1/projects/{id}/restore</code></a></td><td>Restore an archived project (owner only)</td></tr>
                            <tr><td><span class="badge bg-danger">DELETE</span></td><td><a href="#projects"><code>/api/v1/projects/{id}</code></a></td><td>Delete a project</td></tr>
                            <tr><td><span class="badge bg-success">GET</span></td><td><a href="#tags"><code>/api/v1/tags</code></a></td><td>List tags</td></tr>
                            <tr><td><span class="badge bg-primary">POST</span></td><td><a href="#tags"><code>/api/v1/tags</code></a></td><td>Create a tag</td></tr>
                            <tr><td><span class="badge bg-warning text-dark">PATCH</span></td><td><a href="#tags"><code>/api/v1/tags/{id}</code></a></td><td>Update a tag (name and/or color)</td></tr>
                            <tr><td><span class="badge bg-danger">DELETE</span></td><td><a href="#tags"><code>/api/v1/tags/{id}</code></a></td><td>Delete a tag</td></tr>
                            <tr><td><span class="badge bg-primary">POST</span></td><td><a href="#images"><code>/api/v1/images</code></a></td><td>Upload an image (S3 or local hosting)</td></tr>
                        </tbody>
                    </table>

                    <h2 id="authentication" class="h4 mt-4">Authentication</h2>
                    <p>
                        Send your API key in the <code>Authorization</code> header using the Bearer scheme:
                    </p>
                    <pre class="api-docs-pre"><code>Authorization: Bearer YOUR_API_KEY</code></pre>
                    <p class="text-muted small mb-0">
                        Keys are created on the profile page and shown in full only once at creation.
                        Revoked keys stop working immediately. The API requires Redis for key validation and rate limiting.
                    </p>

                    <h2 id="session-auth" class="h4 mt-4">Session auth (SPA)</h2>
                    <p>
                        Browser / SPA clients can register and log in with JSON and receive an httpOnly session cookie
                        (same cookie store as the legacy web UI). Resource routes under <code>/api/v1</code> accept either
                        that cookie or a Bearer API key (Redis required for rate limiting / key lookup).
                        Session traffic uses a higher rate-limit budget than Bearer keys; see <a href="#rate-limits">Rate limits</a>.
                    </p>
                    <table class="table table-sm api-docs-table">
                        <thead>
                            <tr>
                                <th>Method</th>
                                <th>Path</th>
                                <th>Auth</th>
                                <th>Description</th>
                            </tr>
                        </thead>
                        <tbody>
                            <tr><td><span class="badge bg-primary">POST</span></td><td><code>/api/v1/auth/register</code></td><td>Public (API enabled + Redis)</td><td>Create account with unique <code>user_name</code>; respects invite-only settings; sets session cookie; returns user JSON</td></tr>
                            <tr><td><span class="badge bg-primary">POST</span></td><td><code>/api/v1/auth/login</code></td><td>Public (API enabled + Redis)</td><td>Email/password login; if MFA is enabled returns <code>{"mfa_required":true}</code> with a pending cookie instead of a full session</td></tr>
                            <tr><td><span class="badge bg-primary">POST</span></td><td><code>/api/v1/auth/mfa/verify</code></td><td>Public (pending MFA cookie)</td><td>Submit a TOTP or recovery code to complete login</td></tr>
                            <tr><td><span class="badge bg-primary">POST</span></td><td><code>/api/v1/auth/logout</code></td><td>API enabled</td><td>Clears session cookie</td></tr>
                            <tr><td><span class="badge bg-success">GET</span></td><td><code>/api/v1/auth/username-available</code></td><td>Public</td><td>Check username format and availability</td></tr>
                            <tr><td><span class="badge bg-success">GET</span></td><td><code>/api/v1/users/search</code></td><td>Session cookie or Bearer</td><td>Username prefix search for project invites, or project members when <code>project_id</code> is set (no emails)</td></tr>
                            <tr><td><span class="badge bg-success">GET</span> <span class="badge bg-warning text-dark">PATCH</span></td><td><code>/api/v1/me</code></td><td>Session cookie or Bearer</td><td>Read profile or update prefs (username not editable here)</td></tr>
                            <tr><td><span class="badge bg-primary">POST</span></td><td><code>/api/v1/me/username</code></td><td>Session cookie or Bearer</td><td>One-time username claim when <code>username_change_available</code></td></tr>
                            <tr><td><span class="badge bg-primary">POST</span></td><td><code>/api/v1/me/password</code></td><td>Session cookie or Bearer</td><td>Change password</td></tr>
                            <tr><td><span class="badge bg-success">GET</span></td><td><code>/api/v1/me/mfa</code></td><td>Session cookie or Bearer</td><td>Whether MFA is enabled and unused recovery codes remaining</td></tr>
                            <tr><td><span class="badge bg-primary">POST</span></td><td><code>/api/v1/me/mfa/setup</code></td><td>Session cookie or Bearer</td><td>Begin TOTP enrollment; returns secret and otpauth URL</td></tr>
                            <tr><td><span class="badge bg-primary">POST</span></td><td><code>/api/v1/me/mfa/enable</code></td><td>Session cookie or Bearer</td><td>Confirm a TOTP code; returns five recovery codes once</td></tr>
                            <tr><td><span class="badge bg-primary">POST</span></td><td><code>/api/v1/me/mfa/disable</code></td><td>Session cookie or Bearer</td><td>Turn MFA off with a TOTP or recovery code</td></tr>
                            <tr><td><span class="badge bg-primary">POST</span></td><td><code>/api/v1/me/mfa/recovery-codes</code></td><td>Session cookie or Bearer</td><td>Replace recovery codes after proving TOTP or a remaining code</td></tr>
                            <tr><td><span class="badge bg-success">GET</span> <span class="badge bg-primary">POST</span></td><td><code>/api/v1/api-keys</code></td><td>Session cookie or Bearer</td><td>List or create keys</td></tr>
                            <tr><td><span class="badge bg-warning text-dark">PATCH</span> <span class="badge bg-danger">DELETE</span></td><td><code>/api/v1/api-keys/{id}</code></td><td>Session cookie or Bearer</td><td>Rename (label only) or revoke a key</td></tr>
                        </tbody>
                    </table>
                    <pre class="api-docs-pre"><code>POST {{ basePath }}/api/v1/auth/login
Content-Type: application/json

{ "email": "you@example.com", "password": "secret" }

→ 200 { "id", "email", "user_name", "timezone", "items_per_page", "permissions", "username_change_available", "mfa_enabled" }
   (+ Set-Cookie: session=…)
→ 200 { "mfa_required": true } when MFA is enabled (pending cookie only)

POST {{ basePath }}/api/v1/auth/mfa/verify
{ "code": "123456" }
→ 200 user JSON (+ full session cookie)</code></pre>
                    <p class="text-muted small">
                        Password reset is also available via <code>/api/v1/auth/forgot-password</code> and <code>/api/v1/auth/reset-password</code>.
                        Resetting a password does not disable MFA. Bearer API keys skip the MFA login step.
                    </p>

                    <h2 id="device-auth" class="h4 mt-4">Device authorization</h2>
                    <p>
                        Mobile and desktop clients can obtain an API key through a browser hand-off when you are already signed in on the web.
                        The client requests a device code, opens the verification URL in a browser, and polls until you approve access.
                    </p>
                    <table class="table table-sm api-docs-table">
                        <thead>
                            <tr>
                                <th>Method</th>
                                <th>Path</th>
                                <th>Auth</th>
                                <th>Description</th>
                            </tr>
                        </thead>
                        <tbody>
                            <tr><td><span class="badge bg-primary">POST</span></td><td><code>/api/v1/auth/device/code</code></td><td>Public (API enabled + Redis)</td><td>Start device authorization; returns <code>device_code</code>, <code>user_code</code>, and verification URLs</td></tr>
                            <tr><td><span class="badge bg-primary">POST</span></td><td><code>/api/v1/auth/device/token</code></td><td>Public (API enabled + Redis)</td><td>Poll for approval; returns <code>api_key</code> once when approved</td></tr>
                            <tr><td><span class="badge bg-success">GET</span></td><td><code>/auth/device?user_code=…</code></td><td>Browser session (optional)</td><td>Approve or deny the request in the browser; returns to the app via <code>redirect_uri</code> when set</td></tr>
                        </tbody>
                    </table>
                    <h3 class="h5 mt-3">Start authorization</h3>
                    <pre class="api-docs-pre"><code>POST {{ basePath }}/api/v1/auth/device/code
Content-Type: application/json

{
  "client_name": "Android app",
  "redirect_uri": "ordryn://auth-complete"
}</code></pre>
                    <p class="text-muted small">
                        Response includes <code>verification_uri_complete</code> (open in a browser), <code>expires_in</code> (600 seconds), and <code>interval</code> (poll every 5 seconds).
                        Optional <code>redirect_uri</code> must be <code>ordryn://auth-complete</code>; after approve/deny the browser opens
                        <code>ordryn://auth-complete?status=approved</code> or <code>ordryn://auth-complete?error=access_denied</code>.
                    </p>
                    <h3 class="h5 mt-3">Poll for token</h3>
                    <pre class="api-docs-pre"><code>POST {{ basePath }}/api/v1/auth/device/token
Content-Type: application/json

{
  "device_code": "DEVICE_CODE",
  "grant_type": "urn:ietf:params:oauth:grant-type:device_code"
}</code></pre>
                    <p class="text-muted small mb-0">
                        While pending, the token endpoint returns <code>400</code> with <code>{"error":"authorization_pending"}</code>.
                        On approval it returns <code>api_key</code>, <code>name</code>, and <code>key_prefix</code> once.
                        Approving again with the same <code>client_name</code> rotates (revokes) the previous key for that name.
                    </p>

                    <h2 id="errors" class="h4 mt-4">Errors</h2>
                    <p>Failed requests return JSON with a stable <code>error</code> code and a human-readable <code>message</code>:</p>
                    <pre class="api-docs-pre"><code>{
  "error": "unauthorized",
  "message": "Invalid or revoked API key."
}</code></pre>
                    <table class="table table-sm api-docs-table">
                        <thead>
                            <tr>
                                <th>HTTP</th>
                                <th><code>error</code></th>
                                <th>When</th>
                            </tr>
                        </thead>
                        <tbody>
                            <tr><td>400</td><td><code>invalid_request</code></td><td>Bad parameters or JSON body</td></tr>
                            <tr><td>400</td><td><code>authorization_pending</code> / <code>access_denied</code> / <code>expired_token</code></td><td>Device authorization polling states</td></tr>
                            <tr><td>401</td><td><code>unauthorized</code></td><td>Missing, invalid, or revoked API key</td></tr>
                            <tr><td>403</td><td><code>api_disabled</code></td><td>REST API disabled in site settings</td></tr>
                            <tr><td>404</td><td><code>not_found</code></td><td>Resource not found (or not owned by you)</td></tr>
                            <tr><td>405</td><td><code>method_not_allowed</code></td><td>HTTP method not supported for this path</td></tr>
                            <tr><td>409</td><td><code>name_conflict</code> / <code>limit_reached</code></td><td>Saved view name or per-user limit conflict</td></tr>
                            <tr><td>429</td><td><code>rate_limit_exceeded</code></td><td>Too many requests (see <code>Retry-After</code> header)</td></tr>
                            <tr><td>503</td><td><code>api_unavailable</code></td><td>Redis unavailable</td></tr>
                            <tr><td>500</td><td><code>internal_error</code></td><td>Unexpected server error</td></tr>
                        </tbody>
                    </table>

                    <h2 id="rate-limits" class="h4 mt-4">Rate limits</h2>
                    <p>
                        Limits apply per user using independent token buckets in Redis
                        (reads and writes do not share a budget):
                    </p>
                    <p class="fw-semibold mb-1">External API (Bearer API key)</p>
                    <ul>
                        <li><strong>Read</strong> requests (<code>GET</code>): 120 requests per minute (refill ~2/sec)</li>
                        <li><strong>Write</strong> requests (<code>POST</code>, <code>PUT</code>, <code>PATCH</code>, <code>DELETE</code>): 60 requests per minute (refill ~1/sec)</li>
                    </ul>
                    <p class="fw-semibold mb-1">Web app (session cookie)</p>
                    <ul>
                        <li><strong>Read</strong> requests (<code>GET</code>): 600 requests per minute (refill ~10/sec)</li>
                        <li><strong>Write</strong> requests: 240 requests per minute (refill ~4/sec)</li>
                    </ul>
                    <p class="text-muted small mb-0">
                        When limited, the response is <code>429</code> with a <code>Retry-After</code> header (seconds until you can retry).
                        Session limits are higher so the UI can move cards and edit boards without competing with machine API clients.
                    </p>

                    <h2 id="tasks" class="h4 mt-4">Tasks</h2>

                    <div class="alert alert-warning" role="alert">
                        <strong>Deprecation:</strong> Task favoriting (<code>favorite</code>) is deprecated and
                        will be removed in API v4. Create, update, reorder (<code>favorite: true</code>), and
                        CSV import requests that use <code>favorite</code> still succeed. Those responses include
                        <code>Deprecation: true</code>, a RFC 7234 <code>Warning</code> header, and
                        <code>deprecation_notice</code> in the JSON body.
                    </div>

                    <h3 class="h5 mt-3">Task object</h3>
                    <pre class="api-docs-pre"><code>{
  "id": 42,
  "title": "Buy groceries",
  "description": "Milk, eggs, bread",
  "completed": false,
  "due_date": "2026-07-15",
  "project_id": 3,
  "project": "Personal",
  "priority": 2,
  "favorite": false,             // deprecated; will be removed in API v4
  "position": 5,
  "tags": [
  { "id": 1, "name": "errands", "color": "#6c757d", "project_id": 3 }
  ],
  "created_at": "2026-07-01T14:30:00Z",
  "modified_at": "2026-07-10T09:00:00Z"
}</code></pre>
                    <p class="text-muted small">
                        <code>priority</code>: 0 = None, 1 = Low, 2 = Medium, 3 = High.
                        <code>due_date</code> is empty string when unset.
                        <code>project_id</code> is omitted when the task has no project.
                        <code>favorite</code> is deprecated and will be removed in API v4.
                        Responses that set or change it also include <code>deprecation_notice</code>.
                    </p>

                    <h3 class="h5 mt-3">List tasks</h3>
                    <p><span class="badge bg-success">GET</span> <code>/api/v1/tasks</code></p>
                    <p>Query parameters (all optional):</p>
                    <table class="table table-sm api-docs-table">
                        <thead>
                            <tr><th>Parameter</th><th>Values</th><th>Description</th></tr>
                        </thead>
                        <tbody>
                            <tr><td><code>project</code></td><td>project ID, <code>none</code>, or <code>0</code></td><td>Filter by project</td></tr>
                            <tr><td><code>status</code></td><td><code>incomplete</code>, <code>complete</code></td><td>Completion status</td></tr>
                            <tr><td><code>due</code></td><td><code>overdue</code>, <code>today</code>, <code>week</code>, <code>through_week</code>, <code>none</code></td><td>Due date quick filter</td></tr>
                            <tr><td><code>completed</code></td><td><code>week</code></td><td>Tasks completed in the last 7 days</td></tr>
                            <tr><td><code>priority</code></td><td><code>0</code>–<code>3</code></td><td>Priority level</td></tr>
                            <tr><td><code>tag</code></td><td>tag ID or name</td><td>Filter by tag (name matches across accessible namespaces when not in a project view)</td></tr>
                            <tr><td><code>sort</code></td><td><code>priority</code></td><td>Sort by priority (descending)</td></tr>
                            <tr><td><code>search</code></td><td>text</td><td>Search title and description</td></tr>
                            <tr><td><code>page</code></td><td>integer ≥ 1</td><td>Page number (default 1)</td></tr>
                            <tr><td><code>per_page</code></td><td>1–100</td><td>Results per page (default 50)</td></tr>
                        </tbody>
                    </table>
                    <p>Response:</p>
                    <pre class="api-docs-pre"><code>{
  "tasks": [ /* task objects */ ],
  "total": 128,
  "page": 1,
  "per_page": 50,
  "total_pages": 3
}</code></pre>

                    <h3 class="h5 mt-3">Create task</h3>
                    <p><span class="badge bg-primary">POST</span> <code>/api/v1/tasks</code></p>
                    <p>JSON body:</p>
                    <pre class="api-docs-pre"><code>{
  "title": "New task",           // required
  "description": "",             // optional, max 1000 chars
  "due_date": "2026-07-20",      // optional, YYYY-MM-DD
  "project_id": 3,               // optional; omit or use 0 for no project
  "priority": 1,                 // optional, 0–3 (default 0)
  "completed": false,            // optional (default false)
  "favorite": false,             // deprecated; optional (default false); removed in API v4
  "tag_ids": [1, 2]              // optional
}</code></pre>
                    <p>Returns <code>201 Created</code> with the new task object. Including <code>favorite</code> adds deprecation headers and <code>deprecation_notice</code>.</p>

                    <h3 class="h5 mt-3">Get task</h3>
                    <p><span class="badge bg-success">GET</span> <code>/api/v1/tasks/{id}</code></p>
                    <p>Returns a single task object.</p>

                    <h3 class="h5 mt-3">Update task</h3>
                    <p><span class="badge bg-warning text-dark">PATCH</span> <code>/api/v1/tasks/{id}</code></p>
                    <p>Send only the fields you want to change. All fields are optional:</p>
                    <pre class="api-docs-pre"><code>{
  "title": "Updated title",
  "description": "New description",
  "due_date": "2026-08-01",
  "clear_due_date": true,        // set true to remove due date
  "project_id": null,            // null or 0 clears project; number sets project
  "priority": 3,
  "completed": true,
  "favorite": true,              // deprecated; will be removed in API v4
  "tag_ids": [1]                 // replaces all tags on the task
}</code></pre>
                    <p>Returns the updated task object. Including <code>favorite</code> adds deprecation headers and <code>deprecation_notice</code>.</p>

                    <h3 class="h5 mt-3">Archive / restore</h3>
                    <p><span class="badge bg-primary">POST</span> <code>/api/v1/tasks/{id}/archive</code></p>
                    <p>
                      Applies the protected <code>archived</code> tag in the task’s namespace (and to subtasks).
                      Archived tasks are hidden from default lists unless you filter by that tag. Returns the updated task.
                      Owners and editors only (403 for viewers).
                    </p>
                    <p><span class="badge bg-primary">POST</span> <code>/api/v1/tasks/{id}/restore</code></p>
                    <p>Removes the protected <code>archived</code> tag from the task and its subtasks. Returns the updated task.</p>

                    <h3 class="h5 mt-3">Delete task</h3>
                    <p><span class="badge bg-danger">DELETE</span> <code>/api/v1/tasks/{id}</code></p>
                    <p>Permanently deletes the task. Returns JSON with a short-lived <code>undo_token</code> (valid ~120 seconds) as a safety net:</p>
                    <pre class="api-docs-pre"><code>{ "ok": true, "undo_token": "…", "expires_in": 120 }</code></pre>

                    <h3 class="h5 mt-3">Bulk actions</h3>
                    <p><span class="badge bg-primary">POST</span> <code>/api/v1/tasks/bulk</code></p>
                    <pre class="api-docs-pre"><code>{
  "action": "complete",          // complete|incomplete|delete|move_project|add_tag|remove_tag|set_priority|set_due_date
  "task_ids": [1, 2, 3],
  "project_id": 4,               // move_project (omit/null clears project)
  "tag_id": 7,                   // add_tag / remove_tag
  "priority": 2,                 // set_priority (0-3)
  "due_date": "2026-08-01"       // set_due_date ("" clears)
}</code></pre>
                    <p>Returns <code>{ "ok": true, "affected": N, "action": "…" }</code>. Deletes also include <code>undo_token</code>.</p>

                    <h3 class="h5 mt-3">Undo delete</h3>
                    <p><span class="badge bg-primary">POST</span> <code>/api/v1/tasks/undo</code></p>
                    <pre class="api-docs-pre"><code>{ "undo_token": "…" }</code></pre>
                    <p>Restores tasks from the token (or from the session cookie undo buffer if no token is sent).</p>

                    <h3 class="h5 mt-3">Task events</h3>
                    <p><span class="badge bg-success">GET</span> <code>/api/v1/tasks/{id}/events</code></p>
                    <p>Returns a JSON array of activity entries (<code>event_type</code>, <code>label</code>, <code>metadata</code>, <code>created_at</code>).</p>

                    <h3 class="h5 mt-3">Reorder tasks</h3>
                    <p><span class="badge bg-primary">POST</span> <code>/api/v1/tasks/reorder</code></p>
                    <p>
                        Updates manual sort order (<code>position</code>) within one favorite or non-favorite group.
                        Favorite grouping is deprecated and will be removed in API v4; the <code>favorite</code>
                        field is still required. Reordering the starred group (<code>favorite: true</code>)
                        returns deprecation headers and <code>deprecation_notice</code>.
                        Tasks cannot move across favorite groups.
                        Listed IDs are rearranged among the slots they already occupy, so filtered lists
                        (e.g. incomplete-only) do not overwrite unrelated tasks.
                    </p>
                    <pre class="api-docs-pre"><code>{
  "task_ids": [12, 5, 9],   // required: new order for this page window
  "favorite": false,        // required: which group is being reordered (deprecated; removed in API v4)
  "page": 1,                // optional; default 1
  "per_page": 50,           // optional; default 50, max 100
  "project": "3"            // optional: project id, or "none"/"0" for no project
}</code></pre>
                    <p>Returns <code>{ "ok": true }</code> on success. Relist tasks with <code>GET /api/v1/tasks</code> to read the new order.</p>

                    <h3 class="h5 mt-3">Example: list incomplete tasks</h3>
                    <pre class="api-docs-pre"><code>curl -s -H "Authorization: Bearer YOUR_API_KEY" \
  "{{ basePath }}/api/v1/tasks?status=incomplete&amp;per_page=10"</code></pre>

                    <h2 id="saved-views" class="h4 mt-4">Saved views</h2>
                    <p>
                        Saved views are named, reusable sets of task-list filters. They are private to the API key owner,
                        and each user can store up to 20. Fetching a view returns its filters; apply it by passing those
                        values as query parameters to <code>GET /api/v1/tasks</code>. Page numbers are not stored.
                    </p>

                    <h3 class="h5 mt-3">Saved view object</h3>
                    <pre class="api-docs-pre"><code>{
  "id": 12,
  "name": "Overdue work",
  "filter": {
    "project": "3",
    "status": "incomplete",
    "due": "overdue",
    "completed": "",
    "priority": "2",
    "tag": "7",
    "sort": "priority",
    "search": "release"
  },
  "sort_order": 0,
  "created_at": "2026-07-14T15:44:17Z",
  "updated_at": "2026-07-14T15:44:17Z"
}</code></pre>
                    <p>
                        <code>id</code>, <code>created_at</code>, and <code>updated_at</code> are server-managed.
                        Names are trimmed, limited to 80 characters, and unique per user.
                        <code>sort_order</code> must be between 0 and 2147483647.
                    </p>

                    <h3 class="h5 mt-3">Filter fields</h3>
                    <table class="table table-sm api-docs-table">
                        <thead>
                            <tr><th>Field</th><th>Accepted values</th></tr>
                        </thead>
                        <tbody>
                            <tr><td><code>project</code></td><td>Empty for all projects; a positive project ID; <code>0</code> or <code>none</code> for no project</td></tr>
                            <tr><td><code>status</code></td><td>Empty, <code>complete</code>, <code>completed</code>, or <code>incomplete</code></td></tr>
                            <tr><td><code>due</code></td><td>Empty, <code>overdue</code>, <code>today</code>, <code>week</code>, <code>through_week</code>, or <code>none</code></td></tr>
                            <tr><td><code>completed</code></td><td>Empty, or <code>week</code> for tasks completed in the last 7 days</td></tr>
                            <tr><td><code>priority</code></td><td>Empty, or <code>0</code>–<code>3</code></td></tr>
                            <tr><td><code>tag</code></td><td>Empty, a positive tag ID (project views), or a tag name (all-tasks)</td></tr>
                            <tr><td><code>sort</code></td><td>Empty for default ordering, or <code>priority</code></td></tr>
                            <tr><td><code>search</code></td><td>Any string up to 500 characters</td></tr>
                        </tbody>
                    </table>
                    <p class="text-muted small">
                        Filter values are trimmed. Named values are case-insensitive and normalized in responses;
                        for example, <code>completed</code> in <code>status</code> is returned as <code>complete</code>.
                    </p>

                    <h3 class="h5 mt-3">List and get saved views</h3>
                    <p><span class="badge bg-success">GET</span> <code>/api/v1/saved-views</code></p>
                    <p>
                        Returns an array ordered by <code>sort_order</code>, then case-insensitively by
                        <code>name</code>, then by <code>id</code>.
                    </p>
                    <p><span class="badge bg-success">GET</span> <code>/api/v1/saved-views/{id}</code></p>
                    <p>Returns one saved view. Views owned by another user return <code>404 Not Found</code>.</p>

                    <h3 class="h5 mt-3">Create a saved view</h3>
                    <p><span class="badge bg-primary">POST</span> <code>/api/v1/saved-views</code></p>
                    <pre class="api-docs-pre"><code>{
  "name": "Overdue work",
  "filter": {
    "status": "incomplete",
    "due": "overdue",
    "sort": "priority"
  },
  "sort_order": 0
}</code></pre>
                    <p>
                        <code>name</code> is required. <code>filter</code> and <code>sort_order</code> are optional.
                        Without <code>sort_order</code>, the view is placed after the user's existing views.
                        Returns <code>201 Created</code> with the new object and a <code>Location</code> header.
                    </p>

                    <h3 class="h5 mt-3">Replace or update a saved view</h3>
                    <p><span class="badge bg-info text-dark">PUT</span> <code>/api/v1/saved-views/{id}</code></p>
                    <p>
                        Requires both <code>name</code> and <code>filter</code>. <code>sort_order</code> is optional
                        and remains unchanged when omitted.
                    </p>
                    <p><span class="badge bg-warning text-dark">PATCH</span> <code>/api/v1/saved-views/{id}</code></p>
                    <p>Updates any supplied field and requires at least one of <code>name</code>, <code>filter</code>, or <code>sort_order</code>:</p>
                    <pre class="api-docs-pre"><code>{
  "sort_order": 2
}</code></pre>

                    <h3 class="h5 mt-3">Delete a saved view</h3>
                    <p><span class="badge bg-danger">DELETE</span> <code>/api/v1/saved-views/{id}</code></p>
                    <p>Returns <code>204 No Content</code> on success.</p>

                    <h3 class="h5 mt-3">Saved view validation and conflicts</h3>
                    <p>
                        Request bodies must contain one JSON object, may be at most 1 MiB, and may not contain unknown fields.
                        Duplicate names return <code>409 name_conflict</code>; creating a 21st view returns
                        <code>409 limit_reached</code>.
                    </p>

                    <h2 id="projects" class="h4 mt-4">Projects</h2>

                    <h3 class="h5 mt-3">List projects</h3>
                    <p><span class="badge bg-success">GET</span> <code>/api/v1/projects</code></p>
                    <p>Returns a JSON array of project objects:</p>
                    <pre class="api-docs-pre"><code>[
  { "id": 1, "name": "Work", "archived": false },
  { "id": 2, "name": "Personal", "archived": false }
]</code></pre>

                    <h3 class="h5 mt-3">Create project</h3>
                    <p><span class="badge bg-primary">POST</span> <code>/api/v1/projects</code></p>
                    <p>JSON body:</p>
                    <pre class="api-docs-pre"><code>{
  "name": "Work"    // required, max 50 characters
}</code></pre>
                    <p>Returns <code>201 Created</code> with the project object.</p>

                    <h3 class="h5 mt-3">Rename project</h3>
                    <p><span class="badge bg-warning text-dark">PATCH</span> <code>/api/v1/projects/{id}</code></p>
                    <p>JSON body:</p>
                    <pre class="api-docs-pre"><code>{
  "name": "Renamed"
}</code></pre>
                    <p>Returns the updated project object. Missing ids return <code>404 not_found</code>.</p>

                    <h3 class="h5 mt-3">Delete project</h3>
                    <p><span class="badge bg-danger">DELETE</span> <code>/api/v1/projects/{id}</code></p>
                    <p>Returns <code>204 No Content</code> on success. Tasks keep their data; project association is cleared by the database rules.</p>
                    <p class="text-muted small">
                        Use project IDs when creating or updating tasks.
                    </p>

                    <h3 class="h5 mt-3">Archive project</h3>
                    <p><span class="badge bg-primary">POST</span> <code>/api/v1/projects/{id}/archive</code></p>
                    <p>
                        Owner only. Marks the project archived, moves it into the Archived section,
                        and applies the protected <code>archived</code> tag to its tasks.
                        Creating or moving tasks into the project then returns <code>409 conflict</code>.
                    </p>
                    <p>Returns the updated project object with <code>archived: true</code>.</p>

                    <h3 class="h5 mt-3">Restore project</h3>
                    <p><span class="badge bg-primary">POST</span> <code>/api/v1/projects/{id}/restore</code></p>
                    <p>
                        Owner only. Returns the project to active lists, removes the protected
                        <code>archived</code> tag from its tasks, and allows new tasks again.
                    </p>
                    <p>Returns the updated project object with <code>archived: false</code>.</p>

                    <h2 id="tags" class="h4 mt-4">Tags</h2>
                    <p>
                        Tags are either personal (inbox tasks) or scoped to a project.
                        Project tags are shared with members; only the project owner or editors can create, update, or delete them.
                    </p>

                    <h3 class="h5 mt-3">List tags</h3>
                    <p><span class="badge bg-success">GET</span> <code>/api/v1/tags</code></p>
                    <p>
                        Optional query <code>project_id</code>: omit for all accessible tags,
                        <code>0</code> for personal tags, or a project id for that project’s tags.
                    </p>
                    <p>Returns a JSON array of tag objects:</p>
                    <pre class="api-docs-pre"><code>[
  { "id": 1, "name": "urgent", "color": "#dc3545", "project_id": 3 },
  { "id": 2, "name": "home", "color": "#198754", "project_id": null }
]</code></pre>

                    <h3 class="h5 mt-3">Create tag</h3>
                    <p><span class="badge bg-primary">POST</span> <code>/api/v1/tags</code></p>
                    <p>JSON body:</p>
                    <pre class="api-docs-pre"><code>{
  "name": "errands",   // required, max 50 characters
  "project_id": 3      // optional; omit/null/0 for a personal tag
}</code></pre>
                    <p>
                        Returns <code>201 Created</code> with the tag object.
                        Names are unique per namespace (personal per user, or per project), case-insensitive;
                        if a tag with the same name already exists in that namespace, that tag is returned.
                        Creating a project tag requires owner or editor access (<code>403</code> otherwise).
                    </p>

                    <h3 class="h5 mt-3">Update tag</h3>
                    <p><span class="badge bg-warning text-dark">PATCH</span> <code>/api/v1/tags/{id}</code></p>
                    <p>JSON body (at least one field required; omit a field to leave it unchanged):</p>
                    <pre class="api-docs-pre"><code>{
  "name": "renamed",   // optional, max 50 characters
  "color": "#dc3545"   // optional, max 20 characters (CSS color)
}</code></pre>
                    <p>
                        Returns the updated tag object. Duplicate names in the same namespace return
                        <code>400 invalid_request</code>. Viewers receive <code>403</code>.
                        Protected system tags cannot be renamed or recolored.
                    </p>

                    <h3 class="h5 mt-3">Delete tag</h3>
                    <p><span class="badge bg-danger">DELETE</span> <code>/api/v1/tags/{id}</code></p>
                    <p>Removes the tag and its associations on tasks. Returns <code>204 No Content</code> on success. Project tags may be deleted by the owner or editors.</p>

                    <p class="text-muted small mb-0">
                        Pass <code>tag_ids</code> when creating or updating tasks to assign tags from the task’s namespace
                        (that project, or personal tags for inbox tasks).
                    </p>

                    <h2 id="images" class="h4 mt-4">Images</h2>
                    <p>
                        Image uploads are available when an admin has configured hosting under Admin → Image hosting
                        (<code>s3</code> or <code>local</code>). JPEG, PNG, GIF, and WebP are accepted.
                        Size is limited by <code>image_max_bytes</code> (default 5&nbsp;MiB).
                    </p>
                    <p><span class="badge bg-primary">POST</span> <code>/api/v1/images</code></p>
                    <p>Multipart form field <code>file</code>. Returns <code>201 Created</code>:</p>
                    <pre class="api-docs-pre"><code>{
  "url": "https://cdn.example.com/aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee.png",
  "content_type": "image/png",
  "size": 67,
  "filename": "dot.png",
  "key": "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee.png"
}</code></pre>
                    <p>
                        Local uploads are also readable at <code>/uploads/{key}</code>.
                        If hosting is disabled the API returns <code>400 not_configured</code>.
                    </p>
                </div>
            </div>
        </div>
</template>
