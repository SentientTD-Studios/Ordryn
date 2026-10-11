export type User = {
  id: number
  email: string
  user_name: string
  timezone: string
  items_per_page: number
  permissions: string[]
  allow_project_invites: boolean
  username_change_available: boolean
  mfa_enabled: boolean
  avatar_url?: string
}

export type MFARequired = {
  mfa_required: true
}

export type MFAStatus = {
  enabled: boolean
  recovery_codes_remaining: number
}

export type MFASetup = {
  secret: string
  otpauth_url: string
}

export type MFARecoveryCodes = {
  recovery_codes: string[]
}

export function isMFARequired(value: User | MFARequired): value is MFARequired {
  return 'mfa_required' in value && value.mfa_required === true
}

export type UserSearchHit = {
  user_name: string
}

export type Tag = {
  id: number
  name: string
  color: string
  project_id?: number | null
  protected?: boolean
}

export type WorkflowMode = 'classic' | 'kanban'

export type Project = {
  id: number
  name: string
  description?: string
  workflow_mode?: WorkflowMode
  archived?: boolean
  backlog_name?: string
  backlog_description?: string
  auto_create_next_sprint?: boolean
  auto_sprint_length_days?: number | null
  auto_sprint_lock_days_before?: number | null
  role?: string
  role_name?: string
  permissions?: string[]
  owner_email?: string
  owner_user_name?: string
  owner_user_id?: number
  organization_id?: number | null
  organization_name?: string
  org_managed?: boolean
}

export type OrgImportMode = 'copy' | 'lock' | 'select'

export type OrgImportMember = {
  user_id: number
  role: string
}

export type OrgMemberProjectImpact = {
  id: number
  name: string
  role: string
  locked: boolean
}

export type OrgMemberRoleImpact = {
  locked: OrgMemberProjectImpact[]
  unlocked: OrgMemberProjectImpact[]
}

export type ProjectStatus = {
  id: number
  project_id: number
  name: string
  description?: string
  position: number
  is_done: boolean
  is_default: boolean
  created_at: string
  enter_role_slugs?: string[]
  leave_role_slugs?: string[]
}

export type ProjectSprint = {
  id: number
  project_id: number
  name: string
  description?: string
  start_date?: string | null
  end_date?: string | null
  lock_date?: string | null
  is_active: boolean
  is_locked: boolean
  task_count: number
  created_at: string
}

export type TaskTimeEntry = {
  id: number
  task_id: number
  user_id: number
  minutes: number
  note: string
  created_at: string
  user_email?: string
  user_name?: string
}

export type TaskCommentLink = {
  id: number
  title: string
}

export type TaskComment = {
  id: number
  task_id: number
  user_id: number
  user_name?: string
  avatar_url?: string
  body: string
  created_at: string
  edited_at?: string | null
  edited_by_user_id?: number
  edited_by_user_name?: string
  author_role?: string
  author_role_name?: string
  /** True when an AI agent wrote the comment. */
  author_is_agent?: boolean
  /** True when a project automation rule wrote the comment (author_is_agent is also set). */
  author_is_system?: boolean
  deleted: boolean
  deleted_at?: string | null
  deleted_by_user_id?: number
  deleted_by_kind?: 'user' | 'owner' | string
  links?: TaskCommentLink[]
}

export type TaskCommentRevision = {
  id: number
  comment_id: number
  task_id: number
  body: string
  kind: 'edit' | 'delete' | 'restore' | string
  created_at: string
  edited_by_user_id?: number
  edited_by_user_name?: string
  author_user_id?: number
  author_user_name?: string
  task_title?: string
  project_id?: number
  project_name?: string
  comment_deleted: boolean
  current_body?: string
}

export type CommentAuditList = {
  items: TaskCommentRevision[]
  total: number
  limit: number
  offset: number
}

export type CommentAuditQuery = {
  kind?: string
  q?: string
  limit?: number
  offset?: number
}

export type ProjectMember = {
  user_id: number
  email: string
  user_name: string
  role: string
  role_name?: string
  inherited?: boolean
  /** True for AI agent members (managed on the AI agents tab). */
  is_agent?: boolean
  created_at: string
}

export type ProjectInvite = {
  id: number
  project_id: number
  email: string
  user_name?: string
  role: string
  expires_at: string
  created_at: string
  project_name?: string
  inviter_email?: string
  inviter_user_name?: string
}

export type ProjectEvent = {
  id: number
  project_id: number
  actor_user_id: number
  actor_email?: string
  actor_user_name?: string
  event_type: string
  source: 'project' | 'task'
  task_id?: number
  label: string
  metadata?: Record<string, unknown>
  created_at: string
}

export type ShareLink = {
  id: number
  token: string
  url: string
  scope_type: 'project'
  scope_id: number
  expires_at?: string | null
  created_at: string
}

export type ShareLinkTask = {
  id: number
  title: string
  description?: string
  completed: boolean
  due_date: string
  priority: number
  project?: string
  tags?: Tag[]
  status_name?: string
}

export type ShareLinkView = {
  scope_type: string
  scope_id: number
  tasks: ShareLinkTask[]
}

export type TaskGitHubIssue = {
  issue_number: number
  issue_id: number
  issue_url: string
  issue_state: string
  issue_title?: string
  last_sync_error?: string
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
  position: number
  parent_id?: number | null
  child_count?: number
  children_completed?: number
  children?: Task[]
  tags: Tag[]
  created_at: string
  modified_at: string
  status_id?: number | null
  status_name?: string
  estimate_points?: number | null
  time_spent_minutes?: number
  project_workflow?: WorkflowMode | string
  claimed_by?: number | null
  claimed_by_name?: string
  sprint_id?: number | null
  sprint_name?: string
  parent_title?: string
  github?: TaskGitHubIssue | null
  fields?: Record<string, unknown>
  /** Repeat rule when this task is the active occurrence of a recurring series. */
  recurrence?: TaskRecurrence | null
  /** Incomplete tasks blocking this one (omitted when 0). */
  blocked_by_open?: number
}

export type TaskWatch = {
  watching: boolean
  via_project: boolean
  watchers: { user_id: number; name: string }[]
}

export type TaskLinkType = 'blocks' | 'blocked_by' | 'relates' | 'duplicates' | 'duplicated_by'

export type TaskLink = {
  link_id: number
  type: TaskLinkType
  task_id: number
  title: string
  completed: boolean
  project_id: number | null
}

export type RecurrenceFrequency = 'daily' | 'weekly' | 'monthly' | 'yearly'
export type RecurrenceBasis = 'due' | 'completion'

/** Rule payload for create / PATCH / PUT /tasks/{id}/recurrence. */
export type TaskRecurrenceInput = {
  frequency: RecurrenceFrequency
  interval?: number
  /** Weekly only: 0=Sunday … 6=Saturday. */
  weekdays?: number[]
  /** Monthly only: 1-31 (31 = last day). */
  month_day?: number | null
  basis?: RecurrenceBasis
  ends_on?: string
  end_after?: number | null
}

export type TaskRecurrence = {
  frequency: RecurrenceFrequency
  interval: number
  weekdays: number[]
  month_day: number | null
  basis: RecurrenceBasis
  ends_on: string | null
  end_after: number | null
  occurrence: number
  series_id: number
  summary: string
  next_due?: string
}

export type TaskRecurrenceHistoryItem = {
  task_id: number
  title: string
  due_date: string
  completed: boolean
  completed_at: string | null
  created_at: string
}

export type TaskRecurrenceDetail = {
  task_id: number
  recurrence: TaskRecurrence | null
  series_id: number | null
  prev_task_id: number | null
  next_task_id: number | null
  can_edit: boolean
  is_subtask: boolean
  history: TaskRecurrenceHistoryItem[]
}

export type GitHubConnection = {
  connected: boolean
  github_login?: string
  auth_method?: 'oauth' | 'pat' | string
  connected_at?: string
}

export type ProjectGitHubRepo = {
  linked: boolean
  owner?: string
  repo?: string
  full_name?: string
  html_url?: string
  repo_id?: number
  linked_by_user_id?: number
  webhook_secret?: string
  linked_at?: string
}

export type Notification = {
  id: number
  type: string
  title: string
  body: string
  project_id?: number | null
  task_id?: number | null
  project_name?: string
  actor_name?: string
  read_at?: string | null
  created_at: string
}

export type NotificationList = {
  notifications: Notification[]
  total: number
  page: number
  per_page: number
  unread_count: number
}

export type NotificationPreference = {
  type: string
  label: string
  description: string
  /** Delivered in-app. */
  enabled: boolean
  /** Also emailed (needs enabled, an email mode other than off, and email.available). */
  email: boolean
  /** False for types that cannot be emailed on their own (automation, join requests). */
  email_available: boolean
}

export type EmailMode = 'off' | 'instant' | 'digest'
export type ReminderTiming = 'off' | 'day_before' | 'morning' | 'both'

export type NotificationEmailSettings = {
  /** An admin enabled notification email and outbound mail is configured. */
  available: boolean
  mode: EmailMode
  digest_hour: number
  reminder_timing: ReminderTiming
}

export type NotificationPreferencesResponse = {
  preferences: NotificationPreference[]
  email: NotificationEmailSettings
}

export type NotificationPreferencesUpdate = {
  preferences?: Record<string, boolean>
  email_types?: Record<string, boolean>
  email_mode?: EmailMode
  digest_hour?: number
  reminder_timing?: ReminderTiming
}

export type TaskEvent = {
  id: number
  task_id: number
  event_type: string
  label: string
  metadata?: Record<string, unknown>
  created_at: string
  actor_user_id?: number
  actor_user_name?: string
  actor_email?: string
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
  enable_registration?: boolean
  invite_only?: boolean
  enable_join_requests?: boolean
  meta_description?: string
  enable_global_announcement: boolean
  global_announcement_text: string
  announcement_dismissed: boolean
  github_oauth_configured?: boolean
  image_hosting_enabled?: boolean
  image_max_bytes?: number
  allow_user_invites?: boolean
  user_invite_limit?: number
  invite_expiration_days?: number
  enable_inbound_webhooks?: boolean
  max_description_length?: number
  max_comment_length?: number
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
  /** Kanban projects only: `me` (claimed by caller) or `none` (unclaimed). */
  claimed?: string
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
  due_this_week_count: number
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
  created_at?: string
  expires_at?: string | null
  created_by?: number | null
  creator_user_name?: string
  creator_email?: string
  status?: 'pending' | 'used' | 'expired'
}

export type JoinRequest = {
  id: number
  email: string
  message: string
  status: 'pending' | 'approved' | 'denied'
  created_at: string
  invite_id?: number
  reviewed_at?: string
  reviewed_by?: number
  invite_token?: string
}

export type AdminSettings = {
  site_name: string
  default_timezone: string
  show_changelog: boolean
  site_version: string
  enable_registration: boolean
  invite_only: boolean
  enable_join_requests: boolean
  meta_description: string
  enable_global_announcement: boolean
  global_announcement_text: string
  enable_api: boolean
  enable_inbound_webhooks: boolean
  notification_emails_enabled: boolean
  allow_user_invites: boolean
  user_invite_limit: number
  invite_expiration_days: number
  max_description_length: number
  max_comment_length: number
  email_provider: string
  email_from_address: string
  email_from_name: string
  email_mailgun_domain: string
  email_mailgun_api_key_set: boolean
  email_smtp_host: string
  email_smtp_port: number
  email_smtp_username: string
  email_smtp_password_set: boolean
  email_smtp_tls: boolean
  email_audit_retention_days: number
  audit_retention_days: number
  github_oauth_client_id: string
  github_oauth_client_secret_set: boolean
  github_oauth_configured: boolean
  image_hosting_provider: string
  image_max_bytes: number
  image_s3_endpoint: string
  image_s3_region: string
  image_s3_bucket: string
  image_s3_access_key: string
  image_s3_secret_key_set: boolean
  image_s3_public_url: string
  image_s3_force_path_style: boolean
  image_local_path: string
}

/** Write-only secret fields accepted by PATCH /admin/settings. */
export type AdminSettingsPatch = Partial<Omit<AdminSettings, 'site_version'>> & {
  email_mailgun_api_key?: string
  email_smtp_password?: string
  github_oauth_client_secret?: string
  image_s3_secret_key?: string
}

export type ImageUpload = {
  url: string
  content_type: string
  size: number
  filename?: string
  key: string
}

export type ImageHostingTestResult = {
  ok: boolean
  message: string
  public_url_ok: boolean
}

export type AdminProjectRef = {
  id: number
  name: string
  archived: boolean
}

export type ExtensionSettingField = {
  key: string
  type:
    | 'secret'
    | 'project_ids'
    | 'hook_select'
    | 'bool'
    | 'string'
    | 'int'
    | 'select'
    | 'status'
    | 'user'
    | 'priority'
    | 'tag_ids'
    | 'status_ids'
    | 'status_exclude_ids'
    | 'time'
    | 'digest'
    | 'field_filter'
    | 'mention_map'
    | string
  label: string
  description?: string
  required?: boolean
  scope?: 'site' | 'project' | 'kanban' | 'user' | 'member' | string | string[]
  options?: CustomFieldOption[]
}

export type ExtensionSurface = {
  id: string
  file: string
  at: string
  label?: string
}

export type ExtensionHook = {
  on: string
  label?: string
}

export type ExtensionManifest = {
  id: string
  name: string
  version: string
  host_api: number
  description?: string
  author?: string
  homepage?: string
  license?: string
  icon?: string
  ui?: string
  surfaces?: ExtensionSurface[]
  hooks?: ExtensionHook[]
  delivery?: { type: string; url_from?: string; format?: string }
  settings?: ExtensionSettingField[]
  templates?: Record<string, string>
  fields?: ExtensionField[]
  controls?: string[]
  permissions?: string[]
  actions?: string[]
}

export type ExtensionField = {
  key: string
  type: string
  label: string
  description?: string
  required?: boolean
  show_on?: string[]
  options?: CustomFieldOption[]
}

export type CustomFieldOption = {
  value: string
  label?: string
  color?: string
}

export type CustomFieldDef = {
  field_key: string
  extension_id?: string
  local_key: string
  label: string
  description?: string
  type: 'string' | 'number' | 'boolean' | 'enum' | 'url' | 'user' | 'date' | 'markdown' | string
  required?: boolean
  options?: CustomFieldOption[]
  show_on?: string[]
  active?: boolean
}

export type CustomFieldDefList = {
  fields: CustomFieldDef[]
}

export type AdminExtensionSettings = {
  enabled: boolean
  triggers?: string[]
  templates?: Record<string, string>
  last_error?: string
  last_delivery_at?: string
}

export type AdminExtension = {
  id: string
  name: string
  version: string
  host_api: number
  status: 'loaded' | 'failed' | string
  error?: string
  manifest: ExtensionManifest
  settings: AdminExtensionSettings
  secrets: Record<string, boolean>
  deliveries?: ExtensionDelivery[]
}

export type AdminExtensionsList = {
  extensions: AdminExtension[]
}

export type AdminExtensionsReloadResponse = {
  ok: boolean
  message: string
  extensions: AdminExtension[]
}

export type AdminExtensionPatch = {
  enabled?: boolean
  webhook_url?: string
  triggers?: string[]
  templates?: Record<string, string>
}

export type ExtensionDelivery = {
  id: number
  event: string
  event_id?: string
  host?: string
  status: string
  http_code?: number
  error?: string
  attempts: number
  created_at: string
  next_attempt_at?: string
}

export type ProjectExtensionSettings = {
  enabled: boolean
  triggers: string[]
  templates: Record<string, string>
  status_only: boolean
  skip_self?: boolean
  min_priority?: number
  tag_ids?: number[]
  status_ids?: number[]
  status_exclude_ids?: number[]
  claimed_only?: boolean
  field_key?: string
  field_value?: string
  quiet_hours_start?: string
  quiet_hours_end?: string
  digest?: string
  mention_map?: Record<string, string>
  values?: Record<string, string>
  last_error?: string
  last_delivery_at?: string
}

export type MemberExtensionSettings = ProjectExtensionSettings & {
  claimed_is_me?: boolean
  skip_self?: boolean
}

export type ExtensionStoreDoc = {
  extension_id: string
  project_id: number
  key: string
  revision: number
  value: unknown
  updated_by?: number
  updated_at?: string
}

export type ProjectExtension = {
  id: string
  name: string
  version: string
  host_api: number
  site_enabled: boolean
  manifest: ExtensionManifest
  settings: ProjectExtensionSettings
  secrets: Record<string, boolean>
  member?: MemberExtensionSettings
  member_secrets?: Record<string, boolean>
  signing_set?: boolean
  member_signing_set?: boolean
  signing_secret?: string
  callback_token?: string
  sample_json?: string
  callback_set?: boolean
  member_callback_set?: boolean
  deliveries?: ExtensionDelivery[]
  member_deliveries?: ExtensionDelivery[]
  destination_host?: string
}

export type ProjectExtensionsList = {
  extensions: ProjectExtension[]
  is_owner?: boolean
}

export type ProjectExtensionPatch = {
  enabled?: boolean
  triggers?: string[]
  templates?: Record<string, string>
  status_only?: boolean
  skip_self?: boolean
  min_priority?: number
  tag_ids?: number[]
  status_ids?: number[]
  status_exclude_ids?: number[]
  claimed_only?: boolean
  claimed_is_me?: boolean
  field_key?: string
  field_value?: string
  quiet_hours_start?: string
  quiet_hours_end?: string
  digest?: string
  mention_map?: Record<string, string>
  webhook_url?: string
  ntfy_auth?: string
  rotate_signing?: boolean
  rotate_callback?: boolean
  values?: Record<string, string>
}

export type ProjectInboundWebhook = {
  enabled: boolean
  allow_create: boolean
  allow_comment: boolean
  secret_set: boolean
  secret?: string
  url: string
  last_error?: string
  last_delivery_at?: string
}

export type ProjectInboundPatch = {
  enabled?: boolean
  allow_create?: boolean
  allow_comment?: boolean
  rotate_secret?: boolean
}

export type AdminUser = {
  id: number
  email: string
  user_name: string
  is_banned: boolean
}

export type EmailAuditStatus = 'sent' | 'failed' | 'not_configured' | 'rate_limited'

export type EmailAuditTrigger =
  | 'password_reset'
  | 'password_changed'
  | 'site_invite'
  | 'join_request'
  | 'project_invite'
  | 'organization_invite'

export type EmailAuditEntry = {
  id: number
  created_at: string
  trigger: string
  to_email: string
  status: EmailAuditStatus | string
  error: string
  provider: string
}

export type EmailAuditList = {
  items: EmailAuditEntry[]
  total: number
  limit: number
  offset: number
}

export type EmailAuditQuery = {
  status?: string
  trigger?: string
  q?: string
  from?: string
  to?: string
  limit?: number
  offset?: number
}

export type APIKey = {
  id: number
  name: string
  key_prefix: string
  created_at: string
  last_used_at?: string | null
}

export type ProjectAPIKey = {
  id: number
  name: string
  key_prefix: string
  scopes: string[]
  created_at: string
  last_used_at: string | null
  expires_at: string | null
  expired: boolean
  created_by_id: number
  created_by?: string
}

export type AgentEditableField =
  | 'status'
  | 'title'
  | 'description'
  | 'priority'
  | 'due_date'
  | 'tags'
  | 'estimate'
  | 'sprint'
  | 'custom_fields'

export type AgentTriggerBy = 'managers' | 'writers' | 'selected'

/** Settings shared by create and PATCH; omitted fields keep their value. */
export type ProjectAgentInput = {
  name?: string
  description?: string
  instructions?: string
  enabled?: boolean
  role?: string
  webhook_url?: string
  trigger_on_mention?: boolean
  trigger_status_ids?: number[]
  trigger_by?: AgentTriggerBy
  /** With trigger_by 'selected': roles whose members may call the agent. */
  trigger_role_slugs?: string[]
  /** With trigger_by 'selected': members who may call the agent. */
  trigger_user_ids?: number[]
  claim_on_dispatch?: boolean
  allowed_status_ids?: number[]
  editable_fields?: AgentEditableField[]
  can_complete?: boolean
  can_create_tasks?: boolean
  can_comment?: boolean
  max_runs_per_hour?: number
}

export type ProjectAgent = Required<ProjectAgentInput> & {
  id: number
  project_id: number
  user_id: number
  handle: string
  role_name: string
  webhook_secret_set: boolean
  last_delivery_at: string | null
  last_delivery_error: string
  created_by_id: number
  created_at: string
  updated_at: string
}

export type AgentRunStatus = 'queued' | 'running' | 'succeeded' | 'failed' | 'cancelled'

export type AgentRun = {
  id: number
  agent_id: number
  agent_name: string
  project_id: number
  task_id: number
  task_title: string
  trigger: 'manual' | 'mention' | 'status' | 'automation'
  triggered_by_id: number
  triggered_by: string
  note: string
  status: AgentRunStatus
  delivered: boolean
  delivery_error: string
  summary: string
  created_at: string
  started_at: string | null
  finished_at: string | null
}

export type TaskAgentRuns = {
  runs: AgentRun[]
  agents: { id: number; name: string; handle: string }[]
}

export type AutomationEventTrigger =
  | 'task.created'
  | 'task.status_changed'
  | 'task.completed'
  | 'task.reopened'
  | 'task.claimed'
  | 'task.unclaimed'
  | 'task.due_changed'
  | 'task.tagged'
  | 'task.sprint_changed'
  | 'task.commented'
  | 'task.unblocked'

export type AutomationTimedTrigger =
  | 'time.overdue'
  | 'time.due_soon'
  | 'time.completed_ago'
  | 'time.in_status'
  | 'time.inactive'
  | 'time.sprint_ended'

export type AutomationTriggerType = AutomationEventTrigger | AutomationTimedTrigger

export type AutomationTriggerConfig = {
  days?: number
  from_status_ids?: number[]
  to_status_ids?: number[]
  tag_ids?: number[]
}

export type AutomationConditions = {
  status_ids?: number[]
  exclude_status_ids?: number[]
  min_priority?: number
  tags_any?: number[]
  tags_none?: number[]
  sprint?: '' | 'none' | 'current' | 'any' | 'specific'
  sprint_id?: number
  assignee?: '' | 'unassigned' | 'assigned' | 'user'
  assignee_id?: number
  field_key?: string
  field_value?: string
  has_due?: '' | 'yes' | 'no'
  completion?: '' | 'open' | 'done'
  task_kind?: '' | 'root' | 'subtask'
}

export type AutomationActionType =
  | 'set_status'
  | 'set_priority'
  | 'add_tag'
  | 'remove_tag'
  | 'assign'
  | 'unassign'
  | 'set_sprint'
  | 'set_due'
  | 'complete'
  | 'reopen'
  | 'archive'
  | 'comment'
  | 'notify'
  | 'queue_agent'

export type AutomationAction = {
  type: AutomationActionType
  status_id?: number
  priority?: number
  tag_id?: number
  user_id?: number
  sprint?: 'current' | 'next' | 'backlog' | 'specific'
  sprint_id?: number
  days?: number
  body?: string
  target?: 'watchers' | 'assignee'
  agent_id?: number
}

/** Create / PATCH body; omitted fields keep their value. */
export type AutomationRuleInput = {
  name?: string
  enabled?: boolean
  trigger_type?: AutomationTriggerType
  trigger_config?: AutomationTriggerConfig
  conditions?: AutomationConditions
  actions?: AutomationAction[]
  recipe_id?: string
}

export type AutomationRule = {
  id: number
  project_id: number
  name: string
  enabled: boolean
  position: number
  trigger_type: AutomationTriggerType
  trigger_config: AutomationTriggerConfig
  conditions: AutomationConditions
  actions: AutomationAction[]
  recipe_id: string
  created_by_id: number
  updated_by_id: number
  created_at: string
  updated_at: string
  run_count: number
  error_count: number
  consecutive_errors: number
  last_run_at: string | null
  last_error: string
  /** Set when the system paused the rule (error streak or hourly limit). */
  paused_reason: string
}

export type AutomationChange = {
  action: AutomationActionType
  field?: string
  from?: string
  to?: string
  error?: string
}

export type AutomationRun = {
  id: number
  rule_id: number
  rule_name: string
  project_id: number
  task_id: number
  task_title: string
  trigger: AutomationTriggerType
  outcome: 'applied' | 'error'
  changes: AutomationChange[]
  error: string
  created_at: string
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
  current?: unknown
}

export type ProjectPermInfo = {
  id: string
  label: string
  description: string
  group: string
}

export type ProjectRoleDef = {
  id: number
  project_id?: number | null
  organization_id?: number | null
  slug: string
  name: string
  description?: string
  permissions: string[]
  is_system: boolean
  sort_order: number
  created_at: string
  overrides_site?: boolean
  default_name?: string
}

export type ProjectRolesList = {
  catalog: ProjectPermInfo[]
  roles: ProjectRoleDef[]
  owner_role?: ProjectRoleDef
}

export type ProjectRoleWrite = {
  slug: string
  name: string
  description?: string
  permissions: string[]
  sort_order?: number
  copy_from_id?: number
}

export type ProjectRolePatch = {
  name?: string
  description?: string
  permissions?: string[]
  sort_order?: number
}

export type Organization = {
  id: number
  name: string
  description?: string
  created_by: number
  role?: string
  role_name?: string
  permissions?: string[]
  can_manage?: boolean
  member_count: number
  project_count: number
  created_at: string
  updated_at: string
}

export type OrganizationMember = {
  user_id: number
  email: string
  user_name: string
  role: string
  role_name?: string
  created_at?: string
}

export type OrganizationProjectRoster = {
  id: number
  name: string
  org_managed: boolean
  org_import?: OrgImportMode | string
  can_manage?: boolean
  members: ProjectMember[]
}

export type OrganizationInvite = {
  id: number
  organization_id: number
  email: string
  user_name?: string
  role: string
  expires_at: string
  created_at: string
  organization_name?: string
  inviter_email?: string
  inviter_user_name?: string
}

export type StatusGates = {
  status_id?: number
  enter_role_slugs: string[]
  leave_role_slugs: string[]
}

export class APIError extends Error {
  code: string
  status: number
  payload?: unknown

  constructor(status: number, code: string, message: string, payload?: unknown) {
    super(message)
    this.name = 'APIError'
    this.status = status
    this.code = code
    this.payload = payload
  }
}

export type AuditSource = 'task' | 'project' | 'comment' | 'email' | 'admin'

export type AuditEntry = {
  key: string
  source: AuditSource
  id: number
  created_at: string
  event_type: string
  summary?: string
  actor_user_id: number
  actor_user_name: string
  actor_email: string
  target_type: string
  target_id: number
  target_label: string
  project_id: number
  project_name: string
  metadata: Record<string, unknown>
}

export type AuditList = {
  items: AuditEntry[]
  total: number
  limit: number
  offset: number
}

export type AuditQuery = {
  user_id?: number | ''
  project_id?: number | ''
  source?: AuditSource | ''
  event_type?: string
  since?: string
  until?: string
  limit?: number
  offset?: number
}

export type AuditFacets = {
  event_types: { source: AuditSource; event_type: string }[]
  projects: { id: number; name: string; archived: boolean }[]
}
