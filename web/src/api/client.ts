import type { Project, Tag, Task, TaskList, User } from './types'
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

export const api = {
  health() {
    return request<{ version: string; api_enabled: boolean; redis_ok: boolean; mode: string }>(
      '/api/v1/health',
    )
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
}
