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

export type TaskEvent = {
  id: number
  task_id: number
  event_type: string
  label: string
  metadata?: Record<string, unknown>
  created_at: string
}

export type TaskList = {
  tasks: Task[]
  total: number
  page: number
  per_page: number
  total_pages: number
  completed_count: number
  incomplete_count: number
}

export type SiteInfo = {
  site_name: string
  show_changelog: boolean
  enable_global_announcement: boolean
  global_announcement_text: string
  announcement_dismissed: boolean
}

export type ChangelogEntry = {
  version: string
  date: string
  title: string
  notes: string[]
  html?: string
  prerelease?: boolean
}

export type SavedViewFilter = {
  project?: string
  status?: string
  due?: string
  completed?: string
  priority?: string
  tag?: string
  sort?: string
  search?: string
}

export type SavedView = {
  id: number
  name: string
  filter: SavedViewFilter
  sort_order: number
  created_at: string
  updated_at: string
}

export type DashboardStats = {
  overdue_count: number
  due_today_count: number
  completed_this_week: number
  completed_this_month: number
  streak_days: number
  by_project: { name: string; count: number }[]
  by_priority: { priority: number; label: string; count: number }[]
  completions_last_7_days: { date: string; count: number }[]
}

export type CalendarMonthTask = {
  id: number
  title: string
  due: string
  priority: number
  project_name: string
  completed: boolean
}

export type CalendarMonthCell = {
  date: string
  day: number
  in_month: boolean
  is_today: boolean
  tasks: CalendarMonthTask[]
}

export type CalendarMonth = {
  year_month: string
  month_label: string
  prev_month: string
  next_month: string
  today_month: string
  year: number
  weeks: CalendarMonthCell[][]
}

export type CalendarInfo = {
  token: string
  feed_url: string
}

export type Invite = {
  id: number
  email: string
  token: string
  used: boolean
}

export type AdminSettings = {
  site_name: string
  default_timezone: string
  show_changelog: boolean
  site_version: string
  enable_registration: boolean
  invite_only: boolean
  meta_description: string
  enable_global_announcement: boolean
  global_announcement_text: string
  enable_api: boolean
}

export type AdminUser = {
  id: number
  email: string
  user_name: string
  is_banned: boolean
}

export type APIKey = {
  id: number
  name: string
  key_prefix: string
  created_at: string
  last_used_at?: string | null
}

export type DeviceStatus = {
  user_code: string
  client_name: string
  status: string
  redirect_uri?: string
}

export type DeviceDecisionResult = {
  ok: boolean
  status: string
  redirect_uri?: string
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
