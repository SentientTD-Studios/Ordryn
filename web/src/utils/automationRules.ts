import type {
  AutomationAction,
  AutomationActionType,
  AutomationConditions,
  AutomationRule,
  AutomationRuleInput,
  AutomationTriggerConfig,
  AutomationTriggerType,
  ProjectAgent,
  ProjectMember,
  ProjectSprint,
  ProjectStatus,
  Tag,
} from '../api/types.ts'

export const MAX_RULES = 25
export const MAX_ACTIONS = 5

export type TriggerInfo = {
  id: AutomationTriggerType
  label: string
  /** Phrase used in rule summaries: "When <phrase>". */
  phrase: string
  timed: boolean
  kanbanOnly?: boolean
  /** Timed triggers: what "days" means. */
  daysLabel?: string
  minDays?: number
  defaultDays?: number
}

export const TRIGGERS: TriggerInfo[] = [
  { id: 'task.created', label: 'Task is created', phrase: 'a task is created', timed: false },
  { id: 'task.status_changed', label: 'Task moves to a column', phrase: 'a task changes column', timed: false, kanbanOnly: true },
  { id: 'task.completed', label: 'Task is completed', phrase: 'a task is completed', timed: false },
  { id: 'task.reopened', label: 'Task is reopened', phrase: 'a task is reopened', timed: false },
  { id: 'task.claimed', label: 'Task is claimed', phrase: 'a task is claimed', timed: false, kanbanOnly: true },
  { id: 'task.unclaimed', label: 'Task is unclaimed', phrase: 'a task is unclaimed', timed: false, kanbanOnly: true },
  { id: 'task.due_changed', label: 'Due date changes', phrase: 'a due date changes', timed: false },
  { id: 'task.tagged', label: 'Tag is added', phrase: 'a tag is added', timed: false },
  { id: 'task.sprint_changed', label: 'Sprint changes', phrase: 'a task moves sprint', timed: false, kanbanOnly: true },
  { id: 'task.commented', label: 'Someone comments', phrase: 'someone comments', timed: false },
  { id: 'task.unblocked', label: 'Task is unblocked', phrase: 'a task’s last blocker is completed', timed: false },
  { id: 'time.overdue', label: 'Overdue for N days', phrase: 'a task is overdue', timed: true, daysLabel: 'days overdue', minDays: 0, defaultDays: 3 },
  { id: 'time.due_soon', label: 'Due within N days', phrase: 'a task is due soon', timed: true, daysLabel: 'days until due', minDays: 0, defaultDays: 1 },
  { id: 'time.completed_ago', label: 'Completed N days ago', phrase: 'a task has been done a while', timed: true, daysLabel: 'days since completed', minDays: 1, defaultDays: 30 },
  { id: 'time.in_status', label: 'In the same column for N days', phrase: 'a task sits in one column', timed: true, kanbanOnly: true, daysLabel: 'days in column', minDays: 1, defaultDays: 7 },
  { id: 'time.inactive', label: 'No activity for N days', phrase: 'a task goes quiet', timed: true, daysLabel: 'days without activity', minDays: 1, defaultDays: 7 },
  { id: 'time.sprint_ended', label: 'Sprint ended with task unfinished', phrase: 'a sprint ends with the task open', timed: true, kanbanOnly: true, daysLabel: 'days after sprint end', minDays: 0, defaultDays: 0 },
]

export function triggerInfo(id: string): TriggerInfo | undefined {
  return TRIGGERS.find((t) => t.id === id)
}

export type ActionInfo = { id: AutomationActionType; label: string; kanbanOnly?: boolean }

export const ACTIONS: ActionInfo[] = [
  { id: 'set_status', label: 'Move to column', kanbanOnly: true },
  { id: 'set_priority', label: 'Set priority' },
  { id: 'add_tag', label: 'Add tag' },
  { id: 'remove_tag', label: 'Remove tag' },
  { id: 'assign', label: 'Assign to', kanbanOnly: true },
  { id: 'unassign', label: 'Unassign', kanbanOnly: true },
  { id: 'set_sprint', label: 'Move to sprint', kanbanOnly: true },
  { id: 'set_due', label: 'Set due date' },
  { id: 'complete', label: 'Complete' },
  { id: 'reopen', label: 'Reopen' },
  { id: 'archive', label: 'Archive' },
  { id: 'comment', label: 'Post a comment' },
  { id: 'notify', label: 'Send a notification' },
  { id: 'queue_agent', label: 'Send to AI agent', kanbanOnly: true },
]

export const PRIORITIES = [
  { value: 0, label: 'None' },
  { value: 1, label: 'Low' },
  { value: 2, label: 'Medium' },
  { value: 3, label: 'High' },
]

export const TEMPLATE_VARS = ['task', 'id', 'status', 'priority', 'due_date', 'assignee', 'sprint', 'project', 'tags', 'rule', 'today', 'url']

export function availableTriggers(kanban: boolean): TriggerInfo[] {
  return TRIGGERS.filter((t) => kanban || !t.kanbanOnly)
}

export function availableActions(kanban: boolean): ActionInfo[] {
  return ACTIONS.filter((a) => kanban || !a.kanbanOnly)
}

/** Editable copy of a rule. */
export type RuleDraft = {
  name: string
  enabled: boolean
  trigger_type: AutomationTriggerType
  trigger_config: AutomationTriggerConfig
  conditions: AutomationConditions
  actions: AutomationAction[]
  recipe_id: string
}

export function emptyDraft(kanban: boolean): RuleDraft {
  return {
    name: '',
    enabled: true,
    trigger_type: kanban ? 'task.claimed' : 'task.created',
    trigger_config: {},
    conditions: {},
    actions: [defaultAction(kanban ? 'set_status' : 'add_tag')],
    recipe_id: '',
  }
}

export function draftFromRule(rule: AutomationRule): RuleDraft {
  return {
    name: rule.name,
    enabled: rule.enabled,
    trigger_type: rule.trigger_type,
    trigger_config: { ...(rule.trigger_config || {}) },
    conditions: { ...(rule.conditions || {}) },
    actions: (rule.actions || []).map((a) => ({ ...a })),
    recipe_id: rule.recipe_id || '',
  }
}

export function defaultAction(type: AutomationActionType): AutomationAction {
  switch (type) {
    case 'set_priority':
      return { type, priority: 3 }
    case 'set_sprint':
      return { type, sprint: 'current' }
    case 'set_due':
      return { type, days: 3 }
    case 'notify':
      return { type, target: 'watchers', body: '{task} needs attention' }
    case 'comment':
      return { type, body: '' }
    default:
      return { type }
  }
}

/** Fills in the default "days" when the trigger changes to a timed one. */
export function onTriggerChanged(draft: RuleDraft): void {
  const info = triggerInfo(draft.trigger_type)
  const cfg: AutomationTriggerConfig = {}
  if (info?.timed) cfg.days = draft.trigger_config.days ?? info.defaultDays ?? 1
  if (draft.trigger_type === 'task.status_changed') {
    cfg.from_status_ids = draft.trigger_config.from_status_ids
    cfg.to_status_ids = draft.trigger_config.to_status_ids
  }
  if (draft.trigger_type === 'task.tagged') cfg.tag_ids = draft.trigger_config.tag_ids
  draft.trigger_config = cfg
}

/** Strips empty filters so saved rules stay small and readable. */
export function draftPayload(d: RuleDraft): AutomationRuleInput {
  const c = d.conditions
  const cond: AutomationConditions = {}
  if (c.status_ids?.length) cond.status_ids = c.status_ids
  if (c.exclude_status_ids?.length) cond.exclude_status_ids = c.exclude_status_ids
  if (c.min_priority) cond.min_priority = c.min_priority
  if (c.tags_any?.length) cond.tags_any = c.tags_any
  if (c.tags_none?.length) cond.tags_none = c.tags_none
  if (c.sprint) {
    cond.sprint = c.sprint
    if (c.sprint === 'specific') cond.sprint_id = c.sprint_id
  }
  if (c.assignee) {
    cond.assignee = c.assignee
    if (c.assignee === 'user') cond.assignee_id = c.assignee_id
  }
  if (c.field_key?.trim()) {
    cond.field_key = c.field_key.trim()
    cond.field_value = (c.field_value || '').trim()
  }
  if (c.has_due) cond.has_due = c.has_due
  if (c.completion) cond.completion = c.completion
  if (c.task_kind) cond.task_kind = c.task_kind

  const cfg: AutomationTriggerConfig = {}
  const t = d.trigger_config
  if (triggerInfo(d.trigger_type)?.timed) cfg.days = Number(t.days ?? 0)
  if (t.from_status_ids?.length) cfg.from_status_ids = t.from_status_ids
  if (t.to_status_ids?.length) cfg.to_status_ids = t.to_status_ids
  if (t.tag_ids?.length) cfg.tag_ids = t.tag_ids

  return {
    name: d.name.trim(),
    enabled: d.enabled,
    trigger_type: d.trigger_type,
    trigger_config: cfg,
    conditions: cond,
    actions: d.actions.map((a) => ({ ...a })),
    recipe_id: d.recipe_id || undefined,
  }
}

/** Client-side checks mirroring the server; "" when the draft can be saved. */
export function draftError(d: RuleDraft): string {
  if (!d.name.trim()) return 'Give the rule a name.'
  if (d.name.trim().length > 80) return 'Rule names can be at most 80 characters.'
  if (!d.actions.length) return 'Add at least one action.'
  if (d.actions.length > MAX_ACTIONS) return `A rule can have at most ${MAX_ACTIONS} actions.`
  const info = triggerInfo(d.trigger_type)
  if (info?.timed) {
    const days = Number(d.trigger_config.days ?? 0)
    if (!Number.isInteger(days) || days < (info.minDays ?? 0) || days > 365) {
      return `Days must be between ${info.minDays ?? 0} and 365.`
    }
  }
  for (const a of d.actions) {
    const msg = actionError(a)
    if (msg) return msg
  }
  return ''
}

function actionError(a: AutomationAction): string {
  switch (a.type) {
    case 'set_status':
      return a.status_id ? '' : 'Choose a column to move to.'
    case 'add_tag':
    case 'remove_tag':
      return a.tag_id ? '' : 'Choose a tag.'
    case 'assign':
      return a.user_id ? '' : 'Choose who to assign.'
    case 'set_sprint':
      return a.sprint === 'specific' && !a.sprint_id ? 'Choose a sprint.' : ''
    case 'comment':
      return a.body?.trim() ? '' : 'Write the comment text.'
    case 'notify':
      return a.body?.trim() ? '' : 'Write the notification text.'
    case 'queue_agent':
      return a.agent_id ? '' : 'Choose an AI agent.'
    default:
      return ''
  }
}

export type RuleLookups = {
  statuses: ProjectStatus[]
  tags: Tag[]
  sprints: ProjectSprint[]
  members: ProjectMember[]
  agents: ProjectAgent[]
}

function names<T extends { id: number; name: string }>(list: T[], ids: number[] | undefined): string {
  return (ids || []).map((id) => list.find((x) => x.id === id)?.name || `#${id}`).join(' or ')
}

function memberName(members: ProjectMember[], id: number | undefined): string {
  const m = members.find((x) => x.user_id === id)
  return m ? m.user_name || m.email : `user #${id}`
}

export function triggerSummary(rule: Pick<RuleDraft, 'trigger_type' | 'trigger_config'>, l: RuleLookups): string {
  const cfg = rule.trigger_config || {}
  const days = cfg.days ?? 0
  const plural = (n: number, w: string) => `${n} ${w}${n === 1 ? '' : 's'}`
  switch (rule.trigger_type) {
    case 'task.status_changed': {
      let s = 'a task moves'
      if (cfg.from_status_ids?.length) s += ` out of ${names(l.statuses, cfg.from_status_ids)}`
      if (cfg.to_status_ids?.length) s += ` into ${names(l.statuses, cfg.to_status_ids)}`
      return cfg.from_status_ids?.length || cfg.to_status_ids?.length ? s : 'a task changes column'
    }
    case 'task.tagged':
      return cfg.tag_ids?.length ? `${names(l.tags, cfg.tag_ids)} is added` : 'a tag is added'
    case 'time.overdue':
      return days ? `a task is ${plural(days, 'day')} overdue` : 'a task becomes overdue'
    case 'time.due_soon':
      return days ? `a task is due within ${plural(days, 'day')}` : 'a task is due today'
    case 'time.completed_ago':
      return `a task was completed ${plural(days, 'day')} ago`
    case 'time.in_status':
      return `a task sits in one column for ${plural(days, 'day')}`
    case 'time.inactive':
      return `a task has no activity for ${plural(days, 'day')}`
    case 'time.sprint_ended':
      return days ? `a sprint ended ${plural(days, 'day')} ago with the task open` : 'a sprint ends with the task open'
    default:
      return triggerInfo(rule.trigger_type)?.phrase || rule.trigger_type
  }
}

export function conditionSummary(c: AutomationConditions, l: RuleLookups): string[] {
  const out: string[] = []
  if (c.status_ids?.length) out.push(`in ${names(l.statuses, c.status_ids)}`)
  if (c.exclude_status_ids?.length) out.push(`not in ${names(l.statuses, c.exclude_status_ids)}`)
  if (c.min_priority) out.push(`priority ${PRIORITIES[c.min_priority]?.label || c.min_priority} or higher`)
  if (c.tags_any?.length) out.push(`tagged ${names(l.tags, c.tags_any)}`)
  if (c.tags_none?.length) out.push(`not tagged ${names(l.tags, c.tags_none)}`)
  switch (c.sprint) {
    case 'none':
      out.push('in the backlog')
      break
    case 'current':
      out.push('in the current sprint')
      break
    case 'any':
      out.push('in any sprint')
      break
    case 'specific':
      out.push(`in sprint ${l.sprints.find((s) => s.id === c.sprint_id)?.name || `#${c.sprint_id}`}`)
      break
  }
  switch (c.assignee) {
    case 'unassigned':
      out.push('unassigned')
      break
    case 'assigned':
      out.push('assigned')
      break
    case 'user':
      out.push(`assigned to ${memberName(l.members, c.assignee_id)}`)
      break
  }
  if (c.field_key) out.push(`${c.field_key} is “${c.field_value || ''}”`)
  if (c.has_due === 'yes') out.push('has a due date')
  if (c.has_due === 'no') out.push('has no due date')
  if (c.completion === 'open') out.push('open')
  if (c.completion === 'done') out.push('completed')
  if (c.task_kind === 'root') out.push('top-level')
  if (c.task_kind === 'subtask') out.push('a subtask')
  return out
}

export function actionSummary(a: AutomationAction, l: RuleLookups): string {
  switch (a.type) {
    case 'set_status':
      return a.status_id ? `move to ${names(l.statuses, [a.status_id])}` : 'move to a column'
    case 'set_priority':
      return `set priority to ${PRIORITIES[a.priority ?? 0]?.label || a.priority}`
    case 'add_tag':
      return a.tag_id ? `add ${names(l.tags, [a.tag_id])}` : 'add a tag'
    case 'remove_tag':
      return a.tag_id ? `remove ${names(l.tags, [a.tag_id])}` : 'remove a tag'
    case 'assign':
      return `assign to ${memberName(l.members, a.user_id)}`
    case 'unassign':
      return 'unassign'
    case 'set_sprint':
      if (a.sprint === 'backlog') return 'move to the backlog'
      if (a.sprint === 'specific') return `move to sprint ${l.sprints.find((s) => s.id === a.sprint_id)?.name || `#${a.sprint_id}`}`
      return `move to the ${a.sprint || 'current'} sprint`
    case 'set_due':
      return a.days ? `set due ${a.days} day${a.days === 1 ? '' : 's'} out` : 'set due today'
    case 'complete':
      return 'complete it'
    case 'reopen':
      return 'reopen it'
    case 'archive':
      return 'archive it'
    case 'comment':
      return 'post a comment'
    case 'notify':
      return a.target === 'assignee' ? 'notify the assignee' : 'notify watchers'
    case 'queue_agent':
      return `send to ${l.agents.find((x) => x.id === a.agent_id)?.name || 'an AI agent'}`
    default:
      return a.type
  }
}

/** One plain sentence: "When …, if …, then …". */
export function describeRule(rule: Pick<RuleDraft, 'trigger_type' | 'trigger_config' | 'conditions' | 'actions'>, l: RuleLookups): string {
  const when = triggerSummary(rule, l)
  const ifs = conditionSummary(rule.conditions || {}, l)
  const thens = (rule.actions || []).map((a) => actionSummary(a, l))
  let s = `When ${when}`
  if (ifs.length) s += `, if ${ifs.join(', ')}`
  s += `, then ${joinList(thens)}.`
  return s
}

function joinList(items: string[]): string {
  if (items.length <= 1) return items[0] || 'do nothing'
  return `${items.slice(0, -1).join(', ')} and ${items[items.length - 1]}`
}

export function changeSummary(c: { action: string; field?: string; from?: string; to?: string; error?: string }): string {
  if (c.error) return `${c.action.replace(/_/g, ' ')} failed: ${c.error}`
  switch (c.action) {
    case 'comment':
      return `Commented: “${c.to || ''}”`
    case 'notify':
      return `Notified ${c.field === 'assignee' ? 'assignee' : 'watchers'}: “${c.to || ''}”`
    case 'add_tag':
      return `Added tag ${c.to}`
    case 'remove_tag':
      return `Removed tag ${c.from}`
    case 'archive':
      return 'Archived'
    case 'complete':
      return 'Completed'
    case 'reopen':
      return 'Reopened'
    case 'unassign':
      return `Unassigned ${c.from || ''}`.trim()
    case 'queue_agent':
      return `Sent to ${c.to}`
    default: {
      const field = (c.field || c.action).replace(/_/g, ' ')
      if (c.from && c.to) return `${capitalize(field)}: ${c.from} → ${c.to}`
      if (c.to) return `${capitalize(field)} → ${c.to}`
      return capitalize(field)
    }
  }
}

function capitalize(s: string): string {
  return s ? s[0].toUpperCase() + s.slice(1) : s
}

// --- recipes ---

export type RecipeContext = {
  kanban: boolean
  statuses: ProjectStatus[]
  tags: Tag[]
  agents: ProjectAgent[]
}

export type RecipeResolved = {
  /** Rules to create, with {tag:name} placeholders resolved by the caller via ensureTags. */
  rules: AutomationRuleInput[]
  /** Tags (by name) the rules use; created when missing. */
  tagNames: string[]
}

export type Recipe = {
  id: string
  label: string
  summary: string
  kanbanOnly?: boolean
  /** Returns why the recipe cannot be added to this project, or "". */
  unavailable?: (ctx: RecipeContext) => string
  /** Opens in the editor instead of being added in one click (needs a choice). */
  needsChoice?: boolean
  build: (ctx: RecipeContext, tagID: (name: string) => number) => AutomationRuleInput[]
  tagNames?: string[]
}

/** Finds a column by name (case-insensitive), trying each candidate in order. */
export function findStatus(statuses: ProjectStatus[], candidates: string[]): ProjectStatus | undefined {
  for (const c of candidates) {
    const hit = statuses.find((s) => s.name.trim().toLowerCase() === c)
    if (hit) return hit
  }
  return undefined
}

export function todoStatus(statuses: ProjectStatus[]): ProjectStatus | undefined {
  return findStatus(statuses, ['to do', 'todo', 'backlog']) || statuses.find((s) => s.is_default)
}

export function inProgressStatus(statuses: ProjectStatus[]): ProjectStatus | undefined {
  return findStatus(statuses, ['in progress', 'doing', 'in-progress', 'wip'])
}

export function readyStatus(statuses: ProjectStatus[]): ProjectStatus | undefined {
  return findStatus(statuses, ['ready', 'ready for work', 'up next', 'next'])
}

export const RECIPES: Recipe[] = [
  {
    id: 'archive-done',
    label: 'Archive finished work',
    summary: 'Archive tasks 30 days after they are completed.',
    build: () => [
      {
        name: 'Archive tasks done for 30 days',
        trigger_type: 'time.completed_ago',
        trigger_config: { days: 30 },
        actions: [{ type: 'archive' }],
      },
    ],
  },
  {
    id: 'slipping',
    label: 'Flag slipping tasks',
    summary: 'Tag “slipping” at 3+ days overdue; clear it when the task is completed or rescheduled.',
    tagNames: ['slipping'],
    build: (_ctx, tag) => [
      {
        name: 'Tag slipping when 3 days overdue',
        trigger_type: 'time.overdue',
        trigger_config: { days: 3 },
        actions: [{ type: 'add_tag', tag_id: tag('slipping') }],
      },
      {
        name: 'Clear slipping when completed',
        trigger_type: 'task.completed',
        conditions: { tags_any: [tag('slipping')] },
        actions: [{ type: 'remove_tag', tag_id: tag('slipping') }],
      },
      {
        name: 'Clear slipping when rescheduled',
        trigger_type: 'task.due_changed',
        conditions: { tags_any: [tag('slipping')] },
        actions: [{ type: 'remove_tag', tag_id: tag('slipping') }],
      },
    ],
  },
  {
    id: 'unblocked-ready',
    label: 'Unblocked → Ready',
    summary: 'When a task’s last blocker is completed, move it to Ready.',
    kanbanOnly: true,
    unavailable: (ctx) => (readyStatus(ctx.statuses) ? '' : 'Add a “Ready” column first.'),
    build: (ctx) => [
      {
        name: 'Move unblocked tasks to Ready',
        trigger_type: 'task.unblocked',
        conditions: { completion: 'open' },
        actions: [{ type: 'set_status', status_id: readyStatus(ctx.statuses)!.id }],
      },
    ],
  },
  {
    id: 'claim-start',
    label: 'Claim starts work',
    summary: 'When someone claims a To Do task, move it to In Progress.',
    kanbanOnly: true,
    unavailable: (ctx) => (todoStatus(ctx.statuses) && inProgressStatus(ctx.statuses) ? '' : 'Needs “To Do” and “In Progress” columns.'),
    build: (ctx) => [
      {
        name: 'Claiming starts work',
        trigger_type: 'task.claimed',
        conditions: { status_ids: [todoStatus(ctx.statuses)!.id] },
        actions: [{ type: 'set_status', status_id: inProgressStatus(ctx.statuses)!.id }],
      },
    ],
  },
  {
    id: 'unclaim-back',
    label: 'Unclaimed → To Do',
    summary: 'When an In Progress task is unclaimed, move it back to To Do.',
    kanbanOnly: true,
    unavailable: (ctx) => (todoStatus(ctx.statuses) && inProgressStatus(ctx.statuses) ? '' : 'Needs “To Do” and “In Progress” columns.'),
    build: (ctx) => [
      {
        name: 'Unclaimed work goes back to To Do',
        trigger_type: 'task.unclaimed',
        conditions: { status_ids: [inProgressStatus(ctx.statuses)!.id] },
        actions: [{ type: 'set_status', status_id: todoStatus(ctx.statuses)!.id }],
      },
    ],
  },
  {
    id: 'reopen-reset',
    label: 'Reopened → To Do',
    summary: 'When a task is reopened, move it to To Do and clear “slipping”.',
    kanbanOnly: true,
    tagNames: ['slipping'],
    unavailable: (ctx) => (todoStatus(ctx.statuses) ? '' : 'Needs a “To Do” column.'),
    build: (ctx, tag) => [
      {
        name: 'Reopened tasks restart in To Do',
        trigger_type: 'task.reopened',
        actions: [
          { type: 'set_status', status_id: todoStatus(ctx.statuses)!.id },
          { type: 'remove_tag', tag_id: tag('slipping') },
        ],
      },
    ],
  },
  {
    id: 'stale',
    label: 'Nudge stale work',
    summary: 'In Progress with no activity for 7 days: tag “stale” and ask the assignee for an update.',
    kanbanOnly: true,
    tagNames: ['stale'],
    unavailable: (ctx) => (inProgressStatus(ctx.statuses) ? '' : 'Needs an “In Progress” column.'),
    build: (ctx, tag) => [
      {
        name: 'Nudge stale In Progress work',
        trigger_type: 'time.inactive',
        trigger_config: { days: 7 },
        conditions: { status_ids: [inProgressStatus(ctx.statuses)!.id] },
        actions: [
          { type: 'add_tag', tag_id: tag('stale') },
          { type: 'comment', body: '{assignee} still on this? There has been no activity for 7 days.' },
        ],
      },
      {
        name: 'Clear stale on new activity',
        trigger_type: 'task.commented',
        conditions: { tags_any: [tag('stale')] },
        actions: [{ type: 'remove_tag', tag_id: tag('stale') }],
      },
    ],
  },
  {
    id: 'due-soon-unassigned',
    label: 'Due soon, nobody on it',
    summary: 'Due within a day and unassigned: notify watchers and raise priority to High.',
    kanbanOnly: true,
    build: () => [
      {
        name: 'Escalate unassigned work due soon',
        trigger_type: 'time.due_soon',
        trigger_config: { days: 1 },
        conditions: { assignee: 'unassigned' },
        actions: [
          { type: 'notify', target: 'watchers', body: 'Due {due_date} and unassigned: {task}' },
          { type: 'set_priority', priority: 3 },
        ],
      },
    ],
  },
  {
    id: 'bug-priority',
    label: 'Bugs are high priority',
    summary: 'When “bug” is added to a task, set its priority to High.',
    tagNames: ['bug'],
    build: (_ctx, tag) => [
      {
        name: 'Bugs are high priority',
        trigger_type: 'task.tagged',
        trigger_config: { tag_ids: [tag('bug')] },
        actions: [{ type: 'set_priority', priority: 3 }],
      },
    ],
  },
  {
    id: 'sprint-carryover',
    label: 'Carry over unfinished work',
    summary: 'When a sprint ends, move its unfinished tasks into the next sprint.',
    kanbanOnly: true,
    build: () => [
      {
        name: 'Carry unfinished work to the next sprint',
        trigger_type: 'time.sprint_ended',
        trigger_config: { days: 0 },
        actions: [{ type: 'set_sprint', sprint: 'next' }],
      },
    ],
  },
  {
    id: 'column-agent',
    label: 'Column hands off to an AI agent',
    summary: 'When a task enters a column you choose, send it to one of your AI agents.',
    kanbanOnly: true,
    needsChoice: true,
    unavailable: (ctx) => (ctx.agents.length ? '' : 'Add an AI agent first.'),
    build: (ctx) => [
      {
        name: 'Hand off to ' + (ctx.agents[0]?.name || 'AI agent'),
        trigger_type: 'task.status_changed',
        trigger_config: { to_status_ids: readyStatus(ctx.statuses) ? [readyStatus(ctx.statuses)!.id] : [] },
        actions: [{ type: 'queue_agent', agent_id: ctx.agents[0]?.id }],
      },
    ],
  },
]

/** Recipes that fit this project, with why any are unavailable. */
export function recipesFor(ctx: RecipeContext): { recipe: Recipe; reason: string }[] {
  return RECIPES.filter((r) => ctx.kanban || !r.kanbanOnly).map((recipe) => ({
    recipe,
    reason: recipe.unavailable ? recipe.unavailable(ctx) : '',
  }))
}

/** Builds a recipe's rules once its tags exist; tagIDs maps lower-case name to id. */
export function buildRecipe(recipe: Recipe, ctx: RecipeContext, tagIDs: Record<string, number>): AutomationRuleInput[] {
  const tag = (name: string) => tagIDs[name.toLowerCase()] || 0
  return recipe.build(ctx, tag).map((r) => ({ enabled: true, conditions: {}, trigger_config: {}, ...r, recipe_id: recipe.id }))
}

/** Tag names a recipe needs that the project does not have yet. */
export function missingRecipeTags(recipe: Recipe, tags: Tag[]): string[] {
  const have = new Set(tags.map((t) => t.name.toLowerCase()))
  return (recipe.tagNames || []).filter((n) => !have.has(n.toLowerCase()))
}

export function tagIDMap(tags: Tag[]): Record<string, number> {
  const out: Record<string, number> = {}
  for (const t of tags) out[t.name.toLowerCase()] = t.id
  return out
}
