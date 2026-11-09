import type {
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
    date: '<=',
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
    if (isListOperator(n.operator)) {
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
  const clauses = (n.clauses || []).map(pruneEmptyGroups).filter((c) => !isGroup(c) || (c.clauses || []).length > 0)
  return { op: n.op || 'and', clauses }
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
  return `${label} ${op} ${valueLabel(field, n.value ?? null, opts)}`
}

/** Parse a date-field text input: a date or a macro like @today-7. */
export function isValidDateValue(s: string): boolean {
  const v = s.trim().toLowerCase()
  if (/^@(today|start_of_week|start_of_month|start_of_year)([+-]\d{1,4})?$/.test(v)) return true
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
