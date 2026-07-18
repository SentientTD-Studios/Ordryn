import type {
  AdminSettings,
  AdminUser,
  APIKey,
  CalendarInfo,
  DashboardStats,
  DeviceStatus,
  Invite,
  Project,
  SavedView,
  SavedViewFilter,
  SiteInfo,
  Tag,
  Task,
  TaskEvent,
  TaskList,
  User,
} from './types'
import { APIError, type APIErrorBody } from './types'

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers)
  if (init.body && !headers.has('Content-Type')) {
    headers.set('Content-Type', 'application/json')
  }

  const res = await fetch(path, {
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
    throw new APIError(
      res.status,
      body?.error || 'request_failed',
      body?.message || res.statusText || 'Request failed',
    )
  }

  return data as T
}

async function download(path: string, fallbackName: string): Promise<void> {
  const res = await fetch(path, { credentials: 'include' })
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

async function upload<T>(path: string, field: string, file: File): Promise<T> {
  const fd = new FormData()
  fd.append(field, file)
  const res = await fetch(path, { method: 'POST', body: fd, credentials: 'include' })
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
    throw new APIError(
      res.status,
      body?.error || 'request_failed',
      body?.message || res.statusText || 'Request failed',
    )
  }
  return data as T
}

export const api = {
  health() {
    return request<{ version: string; api_enabled: boolean; redis_ok: boolean; mode: string }>(
      '/api/v1/health',
    )
  },

  site() {
    return request<SiteInfo>('/api/v1/site')
  },

  me() {
    return request<User>('/api/v1/me')
  },

  login(email: string, password: string) {
    return request<User>('/api/v1/auth/login', {
      method: 'POST',
      body: JSON.stringify({ email, password }),
    })
  },

  register(payload: {
    email: string
    password: string
    confirm_password: string
    timezone?: string
    invite_token?: string
  }) {
    return request<User>('/api/v1/auth/register', {
      method: 'POST',
      body: JSON.stringify(payload),
    })
  },

  logout() {
    return request<{ ok: boolean }>('/api/v1/auth/logout', { method: 'POST' })
  },

  patchMe(payload: Partial<Pick<User, 'user_name' | 'timezone' | 'items_per_page' | 'digest_enabled' | 'digest_hour'>>) {
    return request<User>('/api/v1/me', {
      method: 'PATCH',
      body: JSON.stringify(payload),
    })
  },

  changePassword(payload: {
    current_password: string
    new_password: string
    confirm_password: string
  }) {
    return request<{ ok: boolean }>('/api/v1/me/password', {
      method: 'POST',
      body: JSON.stringify(payload),
    })
  },

  listAPIKeys() {
    return request<APIKey[]>('/api/v1/api-keys')
  },

  createAPIKey(name: string) {
    return request<APIKey & { key: string }>('/api/v1/api-keys', {
      method: 'POST',
      body: JSON.stringify({ name }),
    })
  },

  revokeAPIKey(id: number) {
    return request<void>(`/api/v1/api-keys/${id}`, { method: 'DELETE' })
  },

  listTasks(params: Record<string, string | number | undefined> = {}) {
    const qs = new URLSearchParams()
    for (const [k, v] of Object.entries(params)) {
      if (v !== undefined && v !== '') qs.set(k, String(v))
    }
    const q = qs.toString()
    return request<TaskList>(`/api/v1/tasks${q ? `?${q}` : ''}`)
  },

  getTask(id: number) {
    return request<Task>(`/api/v1/tasks/${id}`)
  },

  listTaskEvents(id: number) {
    return request<TaskEvent[]>(`/api/v1/tasks/${id}/events`)
  },

  createTask(payload: {
    title: string
    description?: string
    due_date?: string
    project_id?: number | null
    priority?: number
    favorite?: boolean
    tag_ids?: number[]
  }) {
    return request<Task>('/api/v1/tasks', {
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
      priority: number
      completed: boolean
      favorite: boolean
      tag_ids: number[]
    }>,
  ) {
    return request<Task>(`/api/v1/tasks/${id}`, {
      method: 'PATCH',
      body: JSON.stringify(payload),
    })
  },

  deleteTask(id: number) {
    return request<{ ok: boolean; undo_token?: string; expires_in?: number }>(`/api/v1/tasks/${id}`, {
      method: 'DELETE',
    })
  },

  undo(undo_token: string) {
    return request<{ ok: boolean; restored: number }>('/api/v1/tasks/undo', {
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
  }) {
    return request<{ ok: boolean; affected: number; undo_token?: string }>('/api/v1/tasks/bulk', {
      method: 'POST',
      body: JSON.stringify(payload),
    })
  },

  reorderTasks(payload: { task_ids: number[]; favorite: boolean; project?: string }) {
    return request<{ ok: boolean }>('/api/v1/tasks/reorder', {
      method: 'POST',
      body: JSON.stringify(payload),
    })
  },

  listProjects() {
    return request<Project[]>('/api/v1/projects')
  },

  createProject(name: string) {
    return request<Project>('/api/v1/projects', {
      method: 'POST',
      body: JSON.stringify({ name }),
    })
  },

  renameProject(id: number, name: string) {
    return request<Project>(`/api/v1/projects/${id}`, {
      method: 'PATCH',
      body: JSON.stringify({ name }),
    })
  },

  deleteProject(id: number) {
    return request<void>(`/api/v1/projects/${id}`, { method: 'DELETE' })
  },

  listTags() {
    return request<Tag[]>('/api/v1/tags')
  },

  createTag(name: string) {
    return request<Tag>('/api/v1/tags', {
      method: 'POST',
      body: JSON.stringify({ name }),
    })
  },

  renameTag(id: number, name: string) {
    return request<Tag>(`/api/v1/tags/${id}`, {
      method: 'PATCH',
      body: JSON.stringify({ name }),
    })
  },

  deleteTag(id: number) {
    return request<void>(`/api/v1/tags/${id}`, { method: 'DELETE' })
  },

  dashboard() {
    return request<DashboardStats>('/api/v1/dashboard')
  },

  getCalendar() {
    return request<CalendarInfo>('/api/v1/calendar')
  },

  regenerateCalendar() {
    return request<CalendarInfo>('/api/v1/calendar/regenerate', { method: 'POST' })
  },

  downloadExport(format: 'json' | 'csv' = 'json') {
    return download(`/api/v1/export?format=${format}`, `gotodo-export.${format}`)
  },

  listSavedViews() {
    return request<SavedView[]>('/api/v1/saved-views')
  },

  createSavedView(payload: { name: string; filter: SavedViewFilter; sort_order?: number }) {
    return request<SavedView>('/api/v1/saved-views', {
      method: 'POST',
      body: JSON.stringify(payload),
    })
  },

  deleteSavedView(id: number) {
    return request<void>(`/api/v1/saved-views/${id}`, { method: 'DELETE' })
  },

  deviceStatus(userCode: string) {
    const qs = new URLSearchParams({ user_code: userCode })
    return request<DeviceStatus>(`/api/v1/auth/device/status?${qs}`)
  },

  deviceApprove(userCode: string) {
    return request<{ ok: boolean; status: string }>('/api/v1/auth/device/approve', {
      method: 'POST',
      body: JSON.stringify({ user_code: userCode }),
    })
  },

  deviceDeny(userCode: string) {
    return request<{ ok: boolean; status: string }>('/api/v1/auth/device/deny', {
      method: 'POST',
      body: JSON.stringify({ user_code: userCode }),
    })
  },

  getAdminSettings() {
    return request<AdminSettings>('/api/v1/admin/settings')
  },

  patchAdminSettings(payload: Partial<AdminSettings>) {
    return request<AdminSettings>('/api/v1/admin/settings', {
      method: 'PATCH',
      body: JSON.stringify(payload),
    })
  },

  listAdminUsers() {
    return request<AdminUser[]>('/api/v1/admin/users')
  },

  banUser(id: number) {
    return request<{ ok: boolean }>(`/api/v1/admin/users/${id}/ban`, { method: 'POST' })
  },

  unbanUser(id: number) {
    return request<{ ok: boolean }>(`/api/v1/admin/users/${id}/unban`, { method: 'POST' })
  },

  listInvites() {
    return request<Invite[]>('/api/v1/invites')
  },

  createInvite(email: string) {
    return request<Invite>('/api/v1/invites', {
      method: 'POST',
      body: JSON.stringify({ email }),
    })
  },

  deleteInvite(id: number) {
    return request<void>(`/api/v1/invites/${id}`, { method: 'DELETE' })
  },

  forgotPassword(email: string, confirmEmail: string) {
    return request<{ ok: boolean }>('/api/v1/auth/forgot-password', {
      method: 'POST',
      body: JSON.stringify({ email, confirm_email: confirmEmail }),
    })
  },

  validateResetToken(token: string, id: string) {
    const qs = new URLSearchParams({ token, id })
    return request<{ valid: boolean; email: string }>(`/api/v1/auth/reset-password?${qs}`)
  },

  resetPassword(payload: {
    id: string
    token: string
    new_password: string
    confirm_password: string
  }) {
    return request<{ ok: boolean }>('/api/v1/auth/reset-password', {
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
    }>('/api/v1/import/preview', 'file', file)
  },

  importConfirm() {
    return request<{ imported: number; skipped: number }>('/api/v1/import/confirm', { method: 'POST' })
  },

  importCancel() {
    return request<{ ok: boolean }>('/api/v1/import/cancel', { method: 'POST' })
  },

  syncCalendar(file: File) {
    return upload<{ updated: number }>('/api/v1/calendar/sync', 'ics_file', file)
  },

  dismissAnnouncement() {
    return request<{ ok: boolean }>('/api/v1/announcements/dismiss', { method: 'POST' })
  },
}
