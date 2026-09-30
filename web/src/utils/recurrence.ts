import type { RecurrenceBasis, RecurrenceFrequency, TaskRecurrence, TaskRecurrenceInput } from '@/api/types'

export type RecurrenceEndMode = 'never' | 'on' | 'after'

/** Editable form state for a task's repeat rule. */
export type RecurrenceDraft = {
  enabled: boolean
  frequency: RecurrenceFrequency
  interval: number
  weekdays: number[]
  /** 0 = same day as the due date. */
  monthDay: number
  basis: RecurrenceBasis
  endMode: RecurrenceEndMode
  endsOn: string
  endAfter: number
}

export const WEEKDAY_SHORT = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat']

export function emptyRecurrenceDraft(): RecurrenceDraft {
  return {
    enabled: false,
    frequency: 'weekly',
    interval: 1,
    weekdays: [],
    monthDay: 0,
    basis: 'due',
    endMode: 'never',
    endsOn: '',
    endAfter: 5,
  }
}

export function draftFromRecurrence(r: TaskRecurrence | null | undefined): RecurrenceDraft {
  const d = emptyRecurrenceDraft()
  if (!r) return d
  d.enabled = true
  d.frequency = r.frequency
  d.interval = r.interval || 1
  d.weekdays = [...(r.weekdays || [])].sort((a, b) => a - b)
  d.monthDay = r.month_day ?? 0
  d.basis = r.basis || 'due'
  if (r.ends_on) {
    d.endMode = 'on'
    d.endsOn = r.ends_on
  } else if (r.end_after && r.end_after > 0) {
    d.endMode = 'after'
    d.endAfter = r.end_after
  }
  return d
}

/** API payload for a draft; null when repeating is off. */
export function recurrencePayload(d: RecurrenceDraft): TaskRecurrenceInput | null {
  if (!d.enabled) return null
  const out: TaskRecurrenceInput = {
    frequency: d.frequency,
    interval: Math.max(1, Math.floor(Number(d.interval) || 1)),
    basis: d.basis,
  }
  if (d.frequency === 'weekly' && d.weekdays.length) out.weekdays = [...d.weekdays].sort((a, b) => a - b)
  if (d.frequency === 'monthly' && d.monthDay > 0) out.month_day = d.monthDay
  if (d.endMode === 'on' && d.endsOn) out.ends_on = d.endsOn
  if (d.endMode === 'after' && d.endAfter > 0) out.end_after = Math.floor(d.endAfter)
  return out
}

/** Compare drafts by the payload they would send (ignores hidden fields). */
export function sameRecurrenceDraft(a: RecurrenceDraft, b: RecurrenceDraft): boolean {
  return JSON.stringify(recurrencePayload(a)) === JSON.stringify(recurrencePayload(b))
}

function ordinal(n: number): string {
  const mod100 = n % 100
  if (mod100 >= 11 && mod100 <= 13) return `${n}th`
  switch (n % 10) {
    case 1:
      return `${n}st`
    case 2:
      return `${n}nd`
    case 3:
      return `${n}rd`
    default:
      return `${n}th`
  }
}

/** Human summary; mirrors the server's recurrence.Rule.Summary(). */
export function recurrenceSummary(d: RecurrenceDraft): string {
  if (!d.enabled) return ''
  const unit = { daily: 'day', weekly: 'week', monthly: 'month', yearly: 'year' }[d.frequency]
  const interval = Math.max(1, Math.floor(Number(d.interval) || 1))
  let s =
    interval <= 1
      ? { daily: 'Daily', weekly: 'Weekly', monthly: 'Monthly', yearly: 'Yearly' }[d.frequency]
      : `Every ${interval} ${unit}s`
  if (d.frequency === 'weekly' && d.weekdays.length) {
    s += ' on ' + [...d.weekdays].sort((a, b) => a - b).map((w) => WEEKDAY_SHORT[w]).join(', ')
  }
  if (d.frequency === 'monthly' && d.monthDay > 0) {
    s += d.monthDay >= 31 ? ' on the last day' : ` on the ${ordinal(d.monthDay)}`
  }
  if (d.basis === 'completion') s += ' after completion'
  if (d.endMode === 'on' && d.endsOn) s += ` until ${d.endsOn}`
  else if (d.endMode === 'after' && d.endAfter > 0) s += `, ${Math.floor(d.endAfter)} times`
  return s
}
