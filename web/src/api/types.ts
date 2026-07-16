export type User = {
  id: number
  email: string
  user_name: string
  timezone: string
  items_per_page: number
  permissions: string[]
  digest_enabled: boolean
  digest_hour: number
}

export type Tag = {
  id: number
  name: string
  color: string
}

export type Project = {
  id: number
  name: string
}

export type Task = {
  id: number
  title: string
  description: string
  completed: boolean
  due_date: string
  project_id?: number | null
  project?: string
  priority: number
  favorite: boolean
  position: number
  tags: Tag[]
  created_at: string
  modified_at: string
}

export type TaskList = {
  tasks: Task[]
  total: number
  page: number
  per_page: number
  total_pages: number
}

export type APIErrorBody = {
  error: string
  message: string
}

export class APIError extends Error {
  code: string
  status: number

  constructor(status: number, code: string, message: string) {
    super(message)
    this.name = 'APIError'
    this.status = status
    this.code = code
  }
}
