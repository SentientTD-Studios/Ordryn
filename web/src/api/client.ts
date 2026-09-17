import { withBase } from '@/base'
import type {
  AdminSettings,
  AdminSettingsPatch,
  AdminUser,
  AdminExtensionsList,
  AdminExtension,
  AdminExtensionPatch,
  ProjectExtensionsList,
  ProjectExtension,
  ProjectExtensionPatch,
  ProjectInboundWebhook,
  ProjectInboundPatch,
  CustomFieldDefList,
  APIKey,
  CalendarInfo,
  CalendarMonth,
  ChangelogEntry,
  DashboardStats,
  DeviceDecisionResult,
  DeviceStatus,
  EmailAuditList,
  EmailAuditQuery,
  GitHubConnection,
  ImageUpload,
  ImageHostingTestResult,
  Invite,
  JoinRequest,
  Project,
  ProjectGitHubRepo,
  TaskGitHubIssue,
  ProjectEvent,
  ProjectInvite,
  ProjectMember,
  ProjectStatus,
  ProjectSprint,
  SavedView,
  SavedViewFilter,
  ShareLink,
  ShareLinkView,
  SiteInfo,
  Tag,
  NotificationList,
  Task,
  TaskEvent,
  TaskList,
  TaskTimeEntry,
  TaskComment,
  TaskCommentRevision,
  CommentAuditList,
  CommentAuditQuery,
  User,
  UserSearchHit,
  WorkflowMode,
  MFARequired,
  MFAStatus,
  MFASetup,
  MFARecoveryCodes,
} from './types'
import { APIError, type APIErrorBody } from './types'

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers)
  if (init.body && !headers.has('Content-Type')) {
    headers.set('Content-Type', 'application/json')
  }

  const res = await fetch(withBase(path), {
    ...init,
    headers,
    credentials: 'include',
  })

  if (res.status === 204) {
    return undefined as T
  }

  const text = await res.text()
  let data: unknown = null
  if (text) {
    try {
      data = JSON.parse(text)
    } catch {
      data = text
    }
  }

  if (!res.ok) {
    const body = data as APIErrorBody | null
    const message =
      body && typeof body === 'object' && typeof body.message === 'string'
        ? body.message
        : typeof data === 'string' && data.includes('<html')
          ? 'Request failed (proxy returned an HTML error page).'
          : res.statusText || 'Request failed'
    throw new APIError(res.status, body?.error || 'request_failed', message)
  }

  return data as T
}

async function download(path: string, fallbackName: string): Promise<void> {
  const res = await fetch(withBase(path), { credentials: 'include' })
  if (!res.ok) {
    let message = res.statusText || 'Download failed'
    try {
      const body = (await res.json()) as APIErrorBody
      message = body.message || message
    } catch {
      /* ignore */
    }
    throw new APIError(res.status, 'request_failed', message)
  }
  const blob = await res.blob()
  const disposition = res.headers.get('Content-Disposition') || ''
  const match = /filename="([^"]+)"/.exec(disposition)
  const filename = match?.[1] || fallbackName
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  a.click()
  URL.revokeObjectURL(url)
}

async function upload<T>(path: string, field: string, file: Blob | File): Promise<T> {
  const fd = new FormData()
  fd.append(field, file)
  const res = await fetch(withBase(path), { method: 'POST', body: fd, credentials: 'include' })
  const text = await res.text()
  let data: unknown = null
  if (text) {
    try {
      data = JSON.parse(text)
    } catch {
      data = text
    }
  }
  if (!res.ok) {
    const body = data as APIErrorBody | null
    const message =
      body && typeof body === 'object' && typeof body.message === 'string'
        ? body.message
        : typeof data === 'string' && data.toLowerCase().includes('<html')
          ? 'Request failed (proxy returned an HTML error page).'
          : res.statusText || 'Request failed'
    throw new APIError(res.status, body?.error || 'request_failed', message)
  }
  return data as T
}

export const api = {
  health() {
    return request<{ version: string; api_enabled: boolean; redis_ok: boolean; mode: string }>(
      '/api/v2/health',
    )
  },

  site() {
    return request<SiteInfo>('/api/v2/site')
  },

  changelog() {
    return request<ChangelogEntry[]>('/changelog')
  },

  me() {
    return request<User | null>('/api/v2/me')
  },

  login(email: string, password: string) {
    return request<User | MFARequired>('/api/v2/auth/login', {
      method: 'POST',
      body: JSON.stringify({ email, password }),
    })
  },

  verifyMFA(code: string) {
    return request<User>('/api/v2/auth/mfa/verify', {
      method: 'POST',
      body: JSON.stringify({ code }),
    })
  },

  getMFA() {
    return request<MFAStatus>('/api/v2/me/mfa')
  },

  setupMFA() {
    return request<MFASetup>('/api/v2/me/mfa/setup', { method: 'POST' })
  },

  enableMFA(code: string) {
    return request<MFARecoveryCodes>('/api/v2/me/mfa/enable', {
      method: 'POST',
      body: JSON.stringify({ code }),
    })
  },

  disableMFA(code: string) {
    return request<{ ok: boolean }>('/api/v2/me/mfa/disable', {
      method: 'POST',
      body: JSON.stringify({ code }),
    })
  },

  regenerateMFARecoveryCodes(code: string) {
    return request<MFARecoveryCodes>('/api/v2/me/mfa/recovery-codes', {
      method: 'POST',
      body: JSON.stringify({ code }),
    })
  },

  register(payload: {
    email: string
    password: string
    confirm_password: string
    user_name: string
    timezone?: string
    invite_token?: string
  }) {
    return request<User>('/api/v2/auth/register', {
      method: 'POST',
      body: JSON.stringify(payload),
    })
  },

  usernameAvailable(username: string) {
    const qs = new URLSearchParams({ username })
    return request<{ username: string; available: boolean; valid: boolean; message?: string }>(
      `/api/v2/auth/username-available?${qs}`,
    )
  },

  logout() {
    return request<{ ok: boolean }>('/api/v2/auth/logout', { method: 'POST' })
  },

  patchMe(payload: Partial<Pick<User, 'timezone' | 'items_per_page' | 'allow_project_invites'>>) {
    return request<User>('/api/v2/me', {
      method: 'PATCH',
      body: JSON.stringify(payload),
    })
  },

  claimUsername(user_name: string) {
    return request<User>('/api/v2/me/username', {
      method: 'POST',
      body: JSON.stringify({ user_name }),
    })
  },

  changePassword(payload: {
    current_password: string
    new_password: string
    confirm_password: string
  }) {
    return request<{ ok: boolean }>('/api/v2/me/password', {
      method: 'POST',
      body: JSON.stringify(payload),
    })
  },

  listAPIKeys() {
    return request<APIKey[]>('/api/v2/api-keys')
  },

  createAPIKey(name: string) {
    return request<APIKey & { key: string }>('/api/v2/api-keys', {
      method: 'POST',
      body: JSON.stringify({ name }),
    })
  },

  renameAPIKey(id: number, name: string) {
    return request<APIKey>(`/api/v2/api-keys/${id}`, {
      method: 'PATCH',
      body: JSON.stringify({ name }),
    })
  },

  revokeAPIKey(id: number) {
    return request<void>(`/api/v2/api-keys/${id}`, { method: 'DELETE' })
  },

  listTasks(params: Record<string, string | number | undefined> = {}) {
    const qs = new URLSearchParams()
    for (const [k, v] of Object.entries(params)) {
      if (v !== undefined && v !== '') qs.set(k, String(v))
    }
    const q = qs.toString()
    return request<TaskList>(`/api/v2/tasks${q ? `?${q}` : ''}`)
  },

  getTask(id: number) {
    return request<Task>(`/api/v2/tasks/${id}`)
  },

  listTaskEvents(id: number) {
    return request<TaskEvent[]>(`/api/v2/tasks/${id}/events`)
  },

  createTask(payload: {
    title: string
    description?: string
    due_date?: string
    project_id?: number | null
    parent_id?: number | null
    priority?: number
    tag_ids?: number[]
    status_id?: number | null
    estimate_points?: number | null
    sprint_id?: number | null
    fields?: Record<string, unknown>
  }) {
    return request<Task>('/api/v2/tasks', {
      method: 'POST',
      body: JSON.stringify(payload),
    })
  },

  patchTask(
    id: number,
    payload: Partial<{
      title: string
      description: string
      due_date: string
      clear_due_date: boolean
      project_id: number | null
      parent_id: number | null
      priority: number
      completed: boolean
      tag_ids: number[]
      status_id: number | null
      estimate_points: number | null
      sprint_id: number | null
      fields: Record<string, unknown>
    }>,
  ) {
    return request<Task>(`/api/v2/tasks/${id}`, {
      method: 'PATCH',
      body: JSON.stringify(payload),
    })
  },

  deleteTask(
    id: number,
    opts?: { mode?: 'cascade' | 'reparent'; new_parent_id?: number | null },
  ) {
    const params = new URLSearchParams()
    if (opts?.mode) params.set('mode', opts.mode)
    if (opts?.mode === 'reparent' && opts.new_parent_id != null) {
      params.set('new_parent_id', String(opts.new_parent_id))
    } else if (opts?.mode === 'reparent') {
      params.set('new_parent_id', '0')
    }
    const qs = params.toString()
    return request<{ ok: boolean; undo_token?: string; expires_in?: number }>(
      `/api/v2/tasks/${id}${qs ? `?${qs}` : ''}`,
      { method: 'DELETE' },
    )
  },

  archiveTask(id: number) {
    return request<Task>(`/api/v2/tasks/${id}/archive`, { method: 'POST' })
  },

  restoreTask(id: number) {
    return request<Task>(`/api/v2/tasks/${id}/restore`, { method: 'POST' })
  },

  undo(undo_token: string) {
    return request<{ ok: boolean; restored: number }>('/api/v2/tasks/undo', {
      method: 'POST',
      body: JSON.stringify({ undo_token }),
    })
  },

  bulkTasks(payload: {
    action: string
    task_ids: number[]
    project_id?: number | null
    tag_id?: number
    priority?: number
    due_date?: string
    status_id?: number
    sprint_id?: number | null
  }) {
    return request<{ ok: boolean; affected: number; undo_token?: string }>('/api/v2/tasks/bulk', {
      method: 'POST',
      body: JSON.stringify(payload),
    })
  },

  reorderTasks(payload: {
    task_ids: number[]
    project?: string
    parent_id?: number | null
    status_id?: number | null
  }) {
    return request<{ ok: boolean }>('/api/v2/tasks/reorder', {
      method: 'POST',
      body: JSON.stringify(payload),
    })
  },

  getGitHubConnection() {
    return request<GitHubConnection>('/api/v2/me/github')
  },

  connectGitHubPAT(token: string) {
    return request<GitHubConnection>('/api/v2/me/github/pat', {
      method: 'POST',
      body: JSON.stringify({ token }),
    })
  },

  disconnectGitHub() {
    return request<{ ok: boolean }>('/api/v2/me/github', { method: 'DELETE' })
  },

  startGitHubOAuth() {
    return request<{ authorize_url: string; redirect_uri: string }>('/api/v2/me/github/oauth/start')
  },

  getProjectGitHub(projectId: number) {
    return request<ProjectGitHubRepo>(`/api/v2/projects/${projectId}/github`)
  },

  linkProjectGitHub(projectId: number, repository: string) {
    return request<ProjectGitHubRepo>(`/api/v2/projects/${projectId}/github`, {
      method: 'PUT',
      body: JSON.stringify({ repository }),
    })
  },

  unlinkProjectGitHub(projectId: number) {
    return request<{ ok: boolean }>(`/api/v2/projects/${projectId}/github`, { method: 'DELETE' })
  },

  listProjectCustomFields(projectId: number) {
    return request<CustomFieldDefList>(`/api/v2/projects/${projectId}/custom-fields`)
  },

  listProjectExtensions(projectId: number) {
    return request<ProjectExtensionsList>(`/api/v2/projects/${projectId}/extensions`)
  },

  patchProjectExtension(projectId: number, extensionId: string, payload: ProjectExtensionPatch) {
    return request<ProjectExtension>(`/api/v2/projects/${projectId}/extensions/${encodeURIComponent(extensionId)}`, {
      method: 'PATCH',
      body: JSON.stringify(payload),
    })
  },

  testProjectExtension(projectId: number, extensionId: string) {
    return request<{ ok: boolean; message: string; sample_json?: string }>(
      `/api/v2/projects/${projectId}/extensions/${encodeURIComponent(extensionId)}/test`,
      { method: 'POST' },
    )
  },

  patchProjectExtensionMe(projectId: number, extensionId: string, payload: ProjectExtensionPatch) {
    return request<ProjectExtension>(
      `/api/v2/projects/${projectId}/extensions/${encodeURIComponent(extensionId)}/me`,
      { method: 'PATCH', body: JSON.stringify(payload) },
    )
  },

  testProjectExtensionMe(projectId: number, extensionId: string) {
    return request<{ ok: boolean; message: string; sample_json?: string }>(
      `/api/v2/projects/${projectId}/extensions/${encodeURIComponent(extensionId)}/me/test`,
      { method: 'POST' },
    )
  },

  retryProjectExtension(projectId: number, extensionId: string, deliveryId: number) {
    return request<{ ok: boolean }>(
      `/api/v2/projects/${projectId}/extensions/${encodeURIComponent(extensionId)}/deliveries/${deliveryId}/retry`,
      { method: 'POST' },
    )
  },

  retryProjectExtensionMe(projectId: number, extensionId: string, deliveryId: number) {
    return request<{ ok: boolean }>(
      `/api/v2/projects/${projectId}/extensions/${encodeURIComponent(extensionId)}/me/deliveries/${deliveryId}/retry`,
      { method: 'POST' },
    )
  },

  listMyExtensions() {
    return request<ProjectExtensionsList>('/api/v2/me/extensions')
  },

  patchMyExtension(extensionId: string, payload: ProjectExtensionPatch) {
    return request<ProjectExtension>(`/api/v2/me/extensions/${encodeURIComponent(extensionId)}`, {
      method: 'PATCH',
      body: JSON.stringify(payload),
    })
  },

  testMyExtension(extensionId: string) {
    return request<{ ok: boolean; message: string; sample_json?: string }>(
      `/api/v2/me/extensions/${encodeURIComponent(extensionId)}/test`,
      { method: 'POST' },
    )
  },

  retryMyExtension(extensionId: string, deliveryId: number) {
    return request<{ ok: boolean }>(
      `/api/v2/me/extensions/${encodeURIComponent(extensionId)}/deliveries/${deliveryId}/retry`,
      { method: 'POST' },
    )
  },

  getProjectInbound(projectId: number) {
    return request<ProjectInboundWebhook>(`/api/v2/projects/${projectId}/inbound`)
  },

  patchProjectInbound(projectId: number, payload: ProjectInboundPatch) {
    return request<ProjectInboundWebhook>(`/api/v2/projects/${projectId}/inbound`, {
      method: 'PATCH',
      body: JSON.stringify(payload),
    })
  },

  createTaskGitHubIssue(taskId: number, payload: { title?: string; body?: string } = {}) {
    return request<TaskGitHubIssue>(`/api/v2/tasks/${taskId}/github-issue`, {
      method: 'POST',
      body: JSON.stringify(payload),
    })
  },

  linkTaskGitHubIssue(taskId: number, issue: string) {
    return request<TaskGitHubIssue>(`/api/v2/tasks/${taskId}/github-issue`, {
      method: 'PUT',
      body: JSON.stringify({ issue }),
    })
  },

  unlinkTaskGitHubIssue(taskId: number) {
    return request<{ ok: boolean }>(`/api/v2/tasks/${taskId}/github-issue`, { method: 'DELETE' })
  },

  listProjects() {
    return request<Project[]>('/api/v2/projects')
  },

  createProject(name: string, description = '') {
    return request<Project>('/api/v2/projects', {
      method: 'POST',
      body: JSON.stringify({ name, description }),
    })
  },

  updateProject(
    id: number,
    payload: Partial<{
      name: string
      description: string
      workflow_mode: WorkflowMode
      backlog_name: string
      backlog_description: string
      auto_create_next_sprint: boolean
      auto_sprint_length_days: number | null
      auto_sprint_lock_days_before: number | null
    }>,
  ) {
    return request<Project>(`/api/v2/projects/${id}`, {
      method: 'PATCH',
      body: JSON.stringify(payload),
    })
  },

  renameProject(id: number, name: string) {
    return request<Project>(`/api/v2/projects/${id}`, {
      method: 'PATCH',
      body: JSON.stringify({ name }),
    })
  },

  reorderProjects(projectIds: number[]) {
    return request<{ ok: boolean }>('/api/v2/projects/reorder', {
      method: 'POST',
      body: JSON.stringify({ project_ids: projectIds }),
    })
  },

  archiveProject(id: number) {
    return request<Project>(`/api/v2/projects/${id}/archive`, { method: 'POST' })
  },

  restoreProject(id: number) {
    return request<Project>(`/api/v2/projects/${id}/restore`, { method: 'POST' })
  },

  listProjectStatuses(projectId: number) {
    return request<ProjectStatus[]>(`/api/v2/projects/${projectId}/statuses`)
  },

  createProjectStatus(
    projectId: number,
    payload: { name: string; description?: string; is_done?: boolean; is_default?: boolean },
  ) {
    return request<ProjectStatus>(`/api/v2/projects/${projectId}/statuses`, {
      method: 'POST',
      body: JSON.stringify(payload),
    })
  },

  updateProjectStatus(
    projectId: number,
    statusId: number,
    payload: Partial<{ name: string; description: string; is_done: boolean; is_default: boolean }>,
  ) {
    return request<ProjectStatus>(`/api/v2/projects/${projectId}/statuses/${statusId}`, {
      method: 'PATCH',
      body: JSON.stringify(payload),
    })
  },

  deleteProjectStatus(projectId: number, statusId: number, moveToStatusId?: number) {
    const qs =
      moveToStatusId != null
        ? `?move_to_status_id=${encodeURIComponent(String(moveToStatusId))}`
        : ''
    return request<void>(`/api/v2/projects/${projectId}/statuses/${statusId}${qs}`, {
      method: 'DELETE',
    })
  },

  reorderProjectStatuses(projectId: number, statusIds: number[]) {
    return request<{ ok: boolean }>(`/api/v2/projects/${projectId}/statuses/reorder`, {
      method: 'POST',
      body: JSON.stringify({ status_ids: statusIds }),
    })
  },

  listProjectSprints(projectId: number) {
    return request<ProjectSprint[]>(`/api/v2/projects/${projectId}/sprints`)
  },

  createProjectSprint(
    projectId: number,
    payload: {
      name: string
      description?: string
      start_date?: string | null
      end_date?: string | null
      lock_date?: string | null
    },
  ) {
    return request<ProjectSprint>(`/api/v2/projects/${projectId}/sprints`, {
      method: 'POST',
      body: JSON.stringify(payload),
    })
  },

  updateProjectSprint(
    projectId: number,
    sprintId: number,
    payload: Partial<{
      name: string
      description: string
      start_date: string | null
      end_date: string | null
      lock_date: string | null
    }>,
  ) {
    return request<ProjectSprint>(`/api/v2/projects/${projectId}/sprints/${sprintId}`, {
      method: 'PATCH',
      body: JSON.stringify(payload),
    })
  },

  updateProjectBacklogSprint(
    projectId: number,
    payload: string | Partial<{ name: string; description: string }>,
  ) {
    const body = typeof payload === 'string' ? { name: payload } : payload
    return request<ProjectSprint>(`/api/v2/projects/${projectId}/sprints/backlog`, {
      method: 'PATCH',
      body: JSON.stringify(body),
    })
  },

  deleteProjectSprint(projectId: number, sprintId: number, moveToSprintId?: number) {
    const qs =
      moveToSprintId != null
        ? `?move_to_sprint_id=${encodeURIComponent(String(moveToSprintId))}`
        : ''
    return request<void>(`/api/v2/projects/${projectId}/sprints/${sprintId}${qs}`, {
      method: 'DELETE',
    })
  },

  listTimeEntries(taskId: number) {
    return request<TaskTimeEntry[]>(`/api/v2/tasks/${taskId}/time-entries`)
  },

  addTimeEntry(taskId: number, minutes: number, note = '') {
    return request<TaskTimeEntry>(`/api/v2/tasks/${taskId}/time-entries`, {
      method: 'POST',
      body: JSON.stringify({ minutes, note }),
    })
  },

  deleteTimeEntry(taskId: number, entryId: number) {
    return request<void>(`/api/v2/tasks/${taskId}/time-entries/${entryId}`, {
      method: 'DELETE',
    })
  },

  listTaskComments(taskId: number) {
    return request<TaskComment[]>(`/api/v2/tasks/${taskId}/comments`)
  },

  addTaskComment(taskId: number, body: string) {
    return request<TaskComment>(`/api/v2/tasks/${taskId}/comments`, {
      method: 'POST',
      body: JSON.stringify({ body }),
    })
  },

  deleteTaskComment(taskId: number, commentId: number) {
    return request<void>(`/api/v2/tasks/${taskId}/comments/${commentId}`, {
      method: 'DELETE',
    })
  },

  editTaskComment(taskId: number, commentId: number, body: string) {
    return request<TaskComment>(`/api/v2/tasks/${taskId}/comments/${commentId}`, {
      method: 'PATCH',
      body: JSON.stringify({ body }),
    })
  },

  listTaskCommentRevisions(taskId: number, commentId: number) {
    return request<TaskCommentRevision[]>(`/api/v2/tasks/${taskId}/comments/${commentId}/revisions`)
  },

  restoreTaskComment(taskId: number, commentId: number, revisionId: number) {
    return request<TaskComment>(`/api/v2/tasks/${taskId}/comments/${commentId}/restore`, {
      method: 'POST',
      body: JSON.stringify({ revision_id: revisionId }),
    })
  },

  claimTask(taskId: number) {
    return request<Task>(`/api/v2/tasks/${taskId}/claim`, { method: 'POST' })
  },

  unclaimTask(taskId: number) {
    return request<Task>(`/api/v2/tasks/${taskId}/claim`, { method: 'DELETE' })
  },

  listNotifications(params: { page?: number; per_page?: number } = {}) {
    const qs = new URLSearchParams()
    if (params.page) qs.set('page', String(params.page))
    if (params.per_page) qs.set('per_page', String(params.per_page))
    const q = qs.toString()
    return request<NotificationList>(`/api/v2/notifications${q ? `?${q}` : ''}`)
  },

  unreadNotificationCount() {
    return request<{ unread_count: number }>('/api/v2/notifications/unread-count')
  },

  markNotificationRead(id: number) {
    return request<void>(`/api/v2/notifications/${id}/read`, { method: 'POST' })
  },

  markAllNotificationsRead() {
    return request<void>('/api/v2/notifications/read-all', { method: 'POST' })
  },

  deleteProject(id: number) {
    return request<void>(`/api/v2/projects/${id}`, { method: 'DELETE' })
  },

  listProjectMembers(projectId: number) {
    return request<ProjectMember[]>(`/api/v2/projects/${projectId}/members`)
  },

  updateProjectMember(projectId: number, userId: number, role: 'editor' | 'viewer') {
    return request<void>(`/api/v2/projects/${projectId}/members/${userId}`, {
      method: 'PATCH',
      body: JSON.stringify({ role }),
    })
  },

  removeProjectMember(projectId: number, userId: number) {
    return request<void>(`/api/v2/projects/${projectId}/members/${userId}`, { method: 'DELETE' })
  },

  listProjectInvites(projectId: number) {
    return request<ProjectInvite[]>(`/api/v2/projects/${projectId}/invites`)
  },

  searchUsers(q: string, init: RequestInit & { projectId?: number } = {}) {
    const { projectId, ...rest } = init
    const qs = new URLSearchParams({ q })
    if (projectId && projectId > 0) qs.set('project_id', String(projectId))
    return request<UserSearchHit[]>(`/api/v2/users/search?${qs}`, rest)
  },

  createProjectInvite(projectId: number, username: string, role: 'editor' | 'viewer') {
    return request<ProjectInvite>(`/api/v2/projects/${projectId}/invites`, {
      method: 'POST',
      body: JSON.stringify({ username, role }),
    })
  },

  revokeProjectInvite(projectId: number, inviteId: number) {
    return request<void>(`/api/v2/projects/${projectId}/invites/${inviteId}`, { method: 'DELETE' })
  },

  listProjectEvents(projectId: number) {
    return request<ProjectEvent[]>(`/api/v2/projects/${projectId}/events`)
  },

  listMyProjectInvites() {
    return request<ProjectInvite[]>('/api/v2/project-invites')
  },

  acceptProjectInvite(id: number) {
    return request<void>(`/api/v2/project-invites/${id}/accept`, { method: 'POST' })
  },

  declineProjectInvite(id: number) {
    return request<void>(`/api/v2/project-invites/${id}/decline`, { method: 'POST' })
  },

  listShareLinks(scopeType: 'project', scopeId: number) {
    return request<ShareLink[]>(
      `/api/v2/share-links?scope_type=${encodeURIComponent(scopeType)}&scope_id=${scopeId}`,
    )
  },

  createShareLink(scopeType: 'project', scopeId: number, expiresAt?: string) {
    return request<ShareLink>('/api/v2/share-links', {
      method: 'POST',
      body: JSON.stringify({
        scope_type: scopeType,
        scope_id: scopeId,
        ...(expiresAt ? { expires_at: expiresAt } : {}),
      }),
    })
  },

  revokeShareLink(id: number) {
    return request<void>(`/api/v2/share-links/${id}`, { method: 'DELETE' })
  },

  viewShareLink(token: string) {
    return request<ShareLinkView>(`/api/v2/share-links/view/${encodeURIComponent(token)}`)
  },

  listTags(opts?: { project_id?: number }) {
    const q = new URLSearchParams()
    if (opts && opts.project_id !== undefined) {
      q.set('project_id', String(opts.project_id))
    }
    const qs = q.toString()
    return request<Tag[]>(`/api/v2/tags${qs ? `?${qs}` : ''}`)
  },

  createTag(name: string, projectId?: number | null) {
    return request<Tag>('/api/v2/tags', {
      method: 'POST',
      body: JSON.stringify({ name, project_id: projectId ?? null }),
    })
  },

  renameTag(id: number, name: string) {
    return request<Tag>(`/api/v2/tags/${id}`, {
      method: 'PATCH',
      body: JSON.stringify({ name }),
    })
  },

  updateTag(id: number, payload: { name?: string; color?: string }) {
    return request<Tag>(`/api/v2/tags/${id}`, {
      method: 'PATCH',
      body: JSON.stringify(payload),
    })
  },

  deleteTag(id: number) {
    return request<void>(`/api/v2/tags/${id}`, { method: 'DELETE' })
  },

  dashboard() {
    return request<DashboardStats>('/api/v2/dashboard')
  },

  getCalendar() {
    return request<CalendarInfo>('/api/v2/calendar')
  },

  calendarMonth(month?: string) {
    const qs = month ? `?month=${encodeURIComponent(month)}` : ''
    return request<CalendarMonth>(`/api/v2/calendar/month${qs}`)
  },

  regenerateCalendar() {
    return request<CalendarInfo>('/api/v2/calendar/regenerate', { method: 'POST' })
  },

  downloadExport(format: 'json' | 'csv' = 'json') {
    return download(`/api/v2/export?format=${format}`, `gotodo-export.${format}`)
  },

  listSavedViews() {
    return request<SavedView[]>('/api/v2/saved-views')
  },

  createSavedView(payload: { name: string; filter: SavedViewFilter; sort_order?: number }) {
    return request<SavedView>('/api/v2/saved-views', {
      method: 'POST',
      body: JSON.stringify(payload),
    })
  },

  deleteSavedView(id: number) {
    return request<void>(`/api/v2/saved-views/${id}`, { method: 'DELETE' })
  },

  deviceStatus(userCode: string) {
    const qs = new URLSearchParams({ user_code: userCode })
    return request<DeviceStatus>(`/api/v2/auth/device/status?${qs}`)
  },

  deviceApprove(userCode: string) {
    return request<DeviceDecisionResult>('/api/v2/auth/device/approve', {
      method: 'POST',
      body: JSON.stringify({ user_code: userCode }),
    })
  },

  deviceDeny(userCode: string) {
    return request<DeviceDecisionResult>('/api/v2/auth/device/deny', {
      method: 'POST',
      body: JSON.stringify({ user_code: userCode }),
    })
  },

  getAdminSettings() {
    return request<AdminSettings>('/api/v2/admin/settings')
  },

  patchAdminSettings(payload: AdminSettingsPatch) {
    return request<AdminSettings>('/api/v2/admin/settings', {
      method: 'PATCH',
      body: JSON.stringify(payload),
    })
  },

  testImageHosting(payload: AdminSettingsPatch) {
    return request<ImageHostingTestResult>('/api/v2/admin/image-hosting/test', {
      method: 'POST',
      body: JSON.stringify(payload),
    })
  },

  listAdminExtensions() {
    return request<AdminExtensionsList>('/api/v2/admin/extensions')
  },

  patchAdminExtension(id: string, payload: AdminExtensionPatch) {
    return request<AdminExtension>(`/api/v2/admin/extensions/${encodeURIComponent(id)}`, {
      method: 'PATCH',
      body: JSON.stringify(payload),
    })
  },

  testAdminExtension(id: string) {
    return request<{ ok: boolean; message: string; sample_json?: string }>(
      `/api/v2/admin/extensions/${encodeURIComponent(id)}/test`,
      { method: 'POST' },
    )
  },

  retryAdminExtension(id: string, deliveryId: number) {
    return request<{ ok: boolean }>(
      `/api/v2/admin/extensions/${encodeURIComponent(id)}/deliveries/${deliveryId}/retry`,
      { method: 'POST' },
    )
  },

  listAdminUsers() {
    return request<AdminUser[]>('/api/v2/admin/users')
  },

  banUser(id: number) {
    return request<{ ok: boolean }>(`/api/v2/admin/users/${id}/ban`, { method: 'POST' })
  },

  unbanUser(id: number) {
    return request<{ ok: boolean }>(`/api/v2/admin/users/${id}/unban`, { method: 'POST' })
  },

  setAdminUsername(id: number, user_name: string) {
    return request<{ ok: boolean; id: number; user_name: string }>(`/api/v2/admin/users/${id}/username`, {
      method: 'PATCH',
      body: JSON.stringify({ user_name }),
    })
  },

  createJoinRequest(email: string, message = '') {
    return request<{ ok: boolean; message: string }>('/api/v2/join-requests', {
      method: 'POST',
      body: JSON.stringify({ email, message }),
    })
  },

  listAdminJoinRequests() {
    return request<JoinRequest[]>('/api/v2/admin/join-requests')
  },

  listAdminEmailAudit(params: EmailAuditQuery = {}) {
    const qs = new URLSearchParams()
    for (const [k, v] of Object.entries(params)) {
      if (v !== undefined && v !== '') qs.set(k, String(v))
    }
    const q = qs.toString()
    return request<EmailAuditList>(`/api/v2/admin/email-audit${q ? `?${q}` : ''}`)
  },

  listAdminCommentAudit(params: CommentAuditQuery = {}) {
    const qs = new URLSearchParams()
    for (const [k, v] of Object.entries(params)) {
      if (v !== undefined && v !== '') qs.set(k, String(v))
    }
    const q = qs.toString()
    return request<CommentAuditList>(`/api/v2/admin/comment-audit${q ? `?${q}` : ''}`)
  },

  restoreAdminCommentRevision(revisionId: number) {
    return request<TaskComment>(`/api/v2/admin/comment-audit/${revisionId}/restore`, {
      method: 'POST',
    })
  },

  approveJoinRequest(id: number) {
    return request<{ ok: boolean; request: JoinRequest; invite: Invite }>(
      `/api/v2/admin/join-requests/${id}/approve`,
      { method: 'POST' },
    )
  },

  denyJoinRequest(id: number) {
    return request<{ ok: boolean; request: JoinRequest }>(`/api/v2/admin/join-requests/${id}/deny`, {
      method: 'POST',
    })
  },

  listInvites() {
    return request<Invite[]>('/api/v2/invites')
  },

  createInvite(email: string, expiresAt?: string, bypassExpiration?: boolean) {
    return request<Invite>('/api/v2/invites', {
      method: 'POST',
      body: JSON.stringify({
        email,
        expires_at: expiresAt || undefined,
        bypass_expiration: !!bypassExpiration,
      }),
    })
  },

  deleteInvite(id: number) {
    return request<void>(`/api/v2/invites/${id}`, { method: 'DELETE' })
  },

  listAdminInvites() {
    return request<Invite[]>('/api/v2/admin/invites')
  },

  createAdminInvite(email: string, expiresAt?: string, bypassExpiration?: boolean) {
    return request<Invite>('/api/v2/admin/invites', {
      method: 'POST',
      body: JSON.stringify({
        email,
        expires_at: expiresAt || undefined,
        bypass_expiration: !!bypassExpiration,
      }),
    })
  },

  deleteAdminInvite(id: number) {
    return request<void>(`/api/v2/admin/invites/${id}`, { method: 'DELETE' })
  },

  forgotPassword(email: string, confirmEmail: string) {
    return request<{ ok: boolean }>('/api/v2/auth/forgot-password', {
      method: 'POST',
      body: JSON.stringify({ email, confirm_email: confirmEmail }),
    })
  },

  validateResetToken(token: string, id: string) {
    const qs = new URLSearchParams({ token, id })
    return request<{ valid: boolean; email: string }>(`/api/v2/auth/reset-password?${qs}`)
  },

  resetPassword(payload: {
    id: string
    token: string
    new_password: string
    confirm_password: string
  }) {
    return request<{ ok: boolean }>('/api/v2/auth/reset-password', {
      method: 'POST',
      body: JSON.stringify(payload),
    })
  },

  importPreview(file: File) {
    return upload<{
      preview: { title: string; project: string; due_date: string; tags: string }[]
      would_import: number
      would_skip: number
      total_rows: number
    }>('/api/v2/import/preview', 'file', file)
  },

  importConfirm() {
    return request<{ imported: number; skipped: number }>('/api/v2/import/confirm', { method: 'POST' })
  },

  importCancel() {
    return request<{ ok: boolean }>('/api/v2/import/cancel', { method: 'POST' })
  },

  uploadImage(file: File) {
    return upload<ImageUpload>('/api/v2/images', 'file', file)
  },

  uploadAvatar(file: Blob | File) {
    return upload<User>('/api/v2/me/avatar', 'file', file)
  },

  deleteAvatar() {
    return request<User>('/api/v2/me/avatar', { method: 'DELETE' })
  },

  syncCalendar(file: File) {
    return upload<{ updated: number }>('/api/v2/calendar/sync', 'ics_file', file)
  },

  dismissAnnouncement() {
    return request<{ ok: boolean }>('/api/v2/announcements/dismiss', { method: 'POST' })
  },
}
