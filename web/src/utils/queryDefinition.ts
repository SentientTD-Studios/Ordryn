import type {
  SavedView,
  SavedViewFilter,
  QueryDefinition,
  QueryField,
  QueryFieldType,
  QueryNode,
  QueryOperator,
  QueryOptions,
  QueryValue,
} from '../api/types'

export const DEFAULT_COLUMNS = ['id', 'title', 'project', 'workflow_status', 'priority', 'due_date', 'claimed_by', 'tags']

export const OPERATOR_LABELS: Record<QueryOperator, string> = {
  '=': '=',
  '<>': '≠',
  '<': '<',
  '<=': '≤',
  '>': '>',
  '>=': '≥',
  in: 'is any of',
  not_in: 'is none of',
  contains: 'contains',
  not_contains: 'does not contain',
  is_empty: 'is empty',
  is_not_empty: 'is not empty',
  between: 'is between',
}

/** Operator labels that read better for a field type. */
export function operatorLabel(op: QueryOperator, type?: QueryFieldType): string {
  if (type === 'tags') {
    if (op === 'in') return 'has any of'
    if (op === 'not_in') return 'has none of'
    if (op === 'is_empty') return 'has no tags'
    if (op === 'is_not_empty') return 'has any tag'
  }
  if (type === 'date') {
    if (op === '<') return 'is before'
    if (op === '<=') return 'is on or before'
    if (op === '>') return 'is after'
    if (op === '>=') return 'is on or after'
  }
  return OPERATOR_LABELS[op] ?? op
}

export const PRIORITY_LABELS = ['None', 'Low', 'Medium', 'High']

export function emptyDefinition(): QueryDefinition {
  return {
    version: 2,
    scope: { projects: [], include_personal: true, include_archived: false, exclude_view_only: false },
    where: { op: 'and', clauses: [] },
    mode: 'flat',
    columns: [...DEFAULT_COLUMNS],
    sort: [],
  }
}

export function cloneDefinition(d: QueryDefinition): QueryDefinition {
  return JSON.parse(JSON.stringify(d)) as QueryDefinition
}

export function isGroup(n: QueryNode): boolean {
  return !n.field
}

export function isListOperator(op?: QueryOperator): boolean {
  return op === 'in' || op === 'not_in'
}

/** "between" takes a [from, to] pair. */
export function isRangeOperator(op?: QueryOperator): boolean {
  return op === 'between'
}

/** The range a new "between" clause starts with: this week. */
export const DEFAULT_DATE_RANGE: [string, string] = ['@start_of_week', '@end_of_week']

export function takesValue(op?: QueryOperator): boolean {
  return !!op && op !== 'is_empty' && op !== 'is_not_empty'
}

export function countClauses(n: QueryNode): number {
  if (!isGroup(n)) return 1
  return (n.clauses || []).reduce((sum, c) => sum + countClauses(c), 0)
}

export function groupDepth(n: QueryNode): number {
  if (!isGroup(n)) return 0
  return 1 + Math.max(0, ...(n.clauses || []).map(groupDepth))
}

/** A sensible starting value when a field or operator changes. */
export function defaultValue(type: QueryFieldType | undefined, op: QueryOperator | undefined): QueryValue {
  if (!takesValue(op)) return null
  if (isRangeOperator(op)) return [...DEFAULT_DATE_RANGE]
  if (isListOperator(op)) return []
  switch (type) {
    case 'boolean':
      return true
    case 'date':
      return '@today'
    case 'user':
      return '@me'
    case 'sprint':
      return '@current_sprint'
    case 'number':
      return 0
    case 'project':
      return null
    default:
      return ''
  }
}

/** A new clause on field, with its first operator and a default value. */
export function newClause(field: QueryField | undefined): QueryNode {
  if (!field) return { field: 'title', operator: 'contains', value: '' }
  const op = preferredOperator(field)
  return { field: field.id, operator: op, value: defaultValue(field.type, op) }
}

function preferredOperator(field: QueryField): QueryOperator {
  const prefer: Partial<Record<QueryFieldType, QueryOperator>> = {
    text: 'contains',
    search: 'contains',
    tags: 'in',
    status: 'in',
    date: 'between',
  }
  const p = prefer[field.type]
  if (p && field.operators.includes(p)) return p
  return field.operators[0] ?? '='
}

/**
 * Keep a clause valid after its field or operator changed: the operator must
 * be one the field allows, and the value must fit the operator.
 */
export function fixClause(n: QueryNode, field: QueryField | undefined): QueryNode {
  if (!field) return n
  let op = n.operator
  if (!op || !field.operators.includes(op)) op = preferredOperator(field)
  let value = n.value ?? null
  if (!takesValue(op)) {
    value = null
  } else if (isRangeOperator(op)) {
    // Keep a single date as the start of the range.
    const from = Array.isArray(value) ? value[0] : value
    value =
      Array.isArray(value) && value.length === 2
        ? value
        : typeof from === 'string' && from !== ''
          ? [from, DEFAULT_DATE_RANGE[1]]
          : [...DEFAULT_DATE_RANGE]
  } else if (isListOperator(op)) {
    if (!Array.isArray(value)) value = value === null || value === '' ? [] : [value as string | number]
  } else if (Array.isArray(value)) {
    value = value.length ? value[0] : defaultValue(field.type, op)
  } else if (value === null) {
    value = defaultValue(field.type, op)
  }
  return { field: n.field, operator: op, value }
}

/** Problems that would stop the server accepting a definition. */
export function definitionProblems(d: QueryDefinition, limits: Record<string, number> = {}): string[] {
  const out: string[] = []
  const maxClauses = limits.clauses ?? 50
  const maxDepth = limits.depth ?? 5
  if (countClauses(d.where) > maxClauses) out.push(`A query can have at most ${maxClauses} clauses.`)
  if (groupDepth(d.where) > maxDepth) out.push(`Groups can be nested at most ${maxDepth} deep.`)
  const walk = (n: QueryNode) => {
    if (isGroup(n)) {
      ;(n.clauses || []).forEach(walk)
      return
    }
    if (!takesValue(n.operator)) return
    const v = n.value
    if (isRangeOperator(n.operator)) {
      if (!Array.isArray(v) || v.length !== 2 || v.some((x) => typeof x !== 'string' || !isValidDateValue(x))) {
        out.push(`Enter a valid from and to date for “${n.field}”.`)
      }
    } else if (isListOperator(n.operator)) {
      if (!Array.isArray(v) || v.length === 0) out.push(`Pick at least one value for “${n.field}”.`)
    } else if (v === null || v === undefined || v === '') {
      out.push(`Enter a value for “${n.field}”.`)
    }
  }
  walk(d.where)
  if (d.columns.length === 0) out.push('Pick at least one column.')
  return out
}

/** Drop empty groups so the saved form matches what the server stores. */
export function pruneEmptyGroups(n: QueryNode): QueryNode {
  if (!isGroup(n)) return n
  const kept = (n.clauses || []).map(pruneEmptyGroups).filter((c) => !isGroup(c) || (c.clauses || []).length > 0)
  const clauses = kept.map((c, i) => withJoin(c, i === 0 ? undefined : (c.join ?? n.op ?? 'and')))
  const out: QueryNode = { op: n.op || 'and', clauses }
  if (n.join) out.join = n.join
  return out
}

/** The connector a row shows: its own, else its group's op. The first row has none. */
export function rowJoin(group: QueryNode, index: number): 'and' | 'or' | undefined {
  if (index === 0) return undefined
  return group.clauses?.[index]?.join ?? group.op ?? 'and'
}

/** n with its connector set (or removed). */
export function withJoin(n: QueryNode, join: 'and' | 'or' | undefined): QueryNode {
  const { join: _old, ...rest } = n
  return join ? { join, ...rest } : rest
}

/**
 * Turn task-list filters into a starting query, mirroring the server's own
 * conversion. tagName resolves a tag id to its name.
 */
export function filterToDefinition(f: SavedViewFilter, tagName: (id: number) => string | undefined): QueryDefinition {
  const d = emptyDefinition()
  const add = (field: string, operator: QueryOperator, value: QueryValue = null) =>
    d.where.clauses!.push(d.where.clauses!.length ? { join: 'and', field, operator, value } : { field, operator, value })
  const project = (f.project || '').trim().toLowerCase()
  if (project === 'none' || project === '0') add('project', 'is_empty')
  else if (/^\d+$/.test(project)) d.scope = { ...d.scope, projects: [Number(project)], include_personal: false }

  if (f.status === 'complete' || f.status === 'completed') add('completed', '=', true)
  else if (f.status === 'incomplete') add('completed', '=', false)

  switch (f.due) {
    case 'overdue':
      add('due_date', '<', '@today')
      add('completed', '=', false)
      break
    case 'today':
      add('due_date', '=', '@today')
      break
    case 'week':
      add('due_date', 'between', ['@today', '@today+7'])
      break
    case 'through_week':
      add('due_date', 'between', ['@today', '@end_of_week'])
      add('completed', '=', false)
      break
    case 'none':
      add('due_date', 'is_empty')
      break
  }
  if (f.due_from) add('due_date', '>=', f.due_from)
  if (f.due_to) add('due_date', '<=', f.due_to)
  if (f.completed === 'week') {
    add('completed', '=', true)
    add('completed_date', '>=', '@start_of_week')
  }

  const priorities = (f.priority || '')
    .split(',')
    .map((p) => p.trim())
    .filter((p) => /^[0-3]$/.test(p))
    .map(Number)
  if (priorities.length === 1) add('priority', '=', priorities[0])
  else if (priorities.length > 1) add('priority', 'in', priorities)

  const tags = (f.tag || '')
    .split(',')
    .map((t) => t.trim())
    .filter(Boolean)
    .map((t) => (/^\d+$/.test(t) ? tagName(Number(t)) : t))
    .filter((t): t is string => !!t)
    .map((t) => t.toLowerCase())
  if (tags.length) add('tags', 'in', tags)

  if (f.claimed === 'me') add('claimed_by', '=', '@me')
  else if (f.claimed === 'none') add('claimed_by', 'is_empty')
  if (f.search?.trim()) add('text', 'contains', f.search.trim())
  if (f.sort === 'priority') d.sort = [{ field: 'priority', dir: 'desc' }]
  return d
}

/** A human-readable label for a clause value. */
export function valueLabel(field: QueryField | undefined, value: QueryValue, opts?: Pick<QueryOptions, 'projects' | 'people' | 'sprints'>): string {
  if (value === null || value === undefined) return ''
  if (Array.isArray(value)) return value.map((v) => valueLabel(field, v, opts)).join(', ')
  if (typeof value === 'boolean') return value ? 'Yes' : 'No'
  const type = field?.type
  if (value === '@me') return 'Me'
  if (value === '@current_sprint') return 'Current sprint'
  if (type === 'project') return opts?.projects.find((p) => p.id === value)?.name ?? 'Unavailable project'
  if (type === 'user') return opts?.people.find((p) => p.id === value)?.name ?? 'Unavailable person'
  if (type === 'sprint') return opts?.sprints.find((s) => s.id === value)?.name ?? 'Unavailable sprint'
  if (field?.id === 'priority' && typeof value === 'number') return PRIORITY_LABELS[value] ?? String(value)
  if (type === 'enum') return field?.options?.find((o) => o.value === value)?.label ?? String(value)
  return String(value)
}

/** One line summarizing a clause, e.g. "Status is any of Doing, Review". */
export function describeClause(n: QueryNode, fields: QueryField[], opts?: Pick<QueryOptions, 'projects' | 'people' | 'sprints'>): string {
  const field = fields.find((f) => f.id === n.field)
  const label = field?.label ?? n.field ?? ''
  const op = operatorLabel(n.operator as QueryOperator, field?.type)
  if (!takesValue(n.operator)) return `${label} ${op}`
  if (isRangeOperator(n.operator) && Array.isArray(n.value)) return `${label} ${op} ${n.value[0]} and ${n.value[1]}`
  return `${label} ${op} ${valueLabel(field, n.value ?? null, opts)}`
}

/** Parse a date-field text input: a date or a macro like @today-7. */
export function isValidDateValue(s: string): boolean {
  const v = s.trim().toLowerCase()
  if (/^@(today|start_of_week|end_of_week|start_of_month|start_of_year)([+-]\d{1,4})?$/.test(v)) return true
  if (!/^\d{4}-\d{2}-\d{2}$/.test(v)) return false
  const d = new Date(`${v}T00:00:00Z`)
  return !Number.isNaN(d.getTime()) && d.toISOString().slice(0, 10) === v
}

/** Turn "1, 2,3" into numbers for number list operators. */
export function parseNumberList(s: string): number[] {
  return s
    .split(',')
    .map((p) => p.trim())
    .filter((p) => p !== '')
    .map(Number)
    .filter((n) => Number.isFinite(n))
}

/** Who can open a saved query, in words. */
export function audienceLabel(v: Pick<SavedView, 'scope' | 'organization_name'>): string {
  if (v.scope === 'site') return 'Everyone'
  if (v.scope === 'organization') return `Members of ${v.organization_name || 'an organization'}`
  return 'Only me'
}

/** Bootstrap icon for a saved query's audience. */
export function audienceIcon(v: Pick<SavedView, 'scope'>): string {
  if (v.scope === 'site') return 'bi bi-globe2'
  if (v.scope === 'organization') return 'bi bi-people'
  return 'bi bi-bookmark-star'
}
