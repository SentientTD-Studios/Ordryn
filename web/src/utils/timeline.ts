/** Date math for the project timeline. Dates are YYYY-MM-DD, handled as UTC day numbers. */

const DAY_MS = 86_400_000

/** Day number (days since 1970-01-01) for YYYY-MM-DD, or null if invalid. */
export function dayNumber(iso: string | null | undefined): number | null {
  if (!iso) return null
  const m = /^(\d{4})-(\d{2})-(\d{2})/.exec(iso)
  if (!m) return null
  const ms = Date.UTC(Number(m[1]), Number(m[2]) - 1, Number(m[3]))
  const d = new Date(ms)
  if (d.getUTCMonth() !== Number(m[2]) - 1 || d.getUTCDate() !== Number(m[3])) return null
  return Math.round(ms / DAY_MS)
}

/** YYYY-MM-DD for a day number. */
export function isoFromDay(day: number): string {
  return new Date(day * DAY_MS).toISOString().slice(0, 10)
}

export type TimelineSpan = { start: number; end: number; milestone: boolean }

/**
 * A task's span in day numbers (both ends included). A task with only a due
 * date is a milestone on that day; one with only a start is a single day.
 */
export function taskSpan(startDate?: string | null, dueDate?: string | null): TimelineSpan | null {
  const s = dayNumber(startDate)
  const d = dayNumber(dueDate)
  if (s == null && d == null) return null
  if (s == null) return { start: d!, end: d!, milestone: true }
  if (d == null) return { start: s, end: s, milestone: false }
  return s <= d ? { start: s, end: d, milestone: false } : { start: d, end: s, milestone: false }
}

/**
 * The visible window for some spans: a little before the first, more after the
 * last, always including today, and at least minDays long.
 */
export function timelineWindow(
  spans: TimelineSpan[],
  today: number,
  minDays = 42,
): { start: number; end: number } {
  let lo = today
  let hi = today
  for (const s of spans) {
    lo = Math.min(lo, s.start)
    hi = Math.max(hi, s.end)
  }
  let start = lo - 3
  let end = hi + 7
  if (end - start + 1 < minDays) end = start + minDays - 1
  return { start, end }
}

/** Shift both dates of a task by delta days, keeping whichever are unset unset. */
export function shiftDates(
  startDate: string | null | undefined,
  dueDate: string | null | undefined,
  delta: number,
): { start_date: string | null; due_date: string | null } {
  const s = dayNumber(startDate)
  const d = dayNumber(dueDate)
  return {
    start_date: s == null ? null : isoFromDay(s + delta),
    due_date: d == null ? null : isoFromDay(d + delta),
  }
}

export type MonthBand = { label: string; start: number; days: number }

/** Month bands covering [start, end] for the header. */
export function monthBands(start: number, end: number): MonthBand[] {
  const out: MonthBand[] = []
  let day = start
  while (day <= end) {
    const d = new Date(day * DAY_MS)
    const nextMonth = Math.round(Date.UTC(d.getUTCFullYear(), d.getUTCMonth() + 1, 1) / DAY_MS)
    const bandEnd = Math.min(end, nextMonth - 1)
    out.push({
      label: d.toLocaleDateString(undefined, { month: 'short', year: 'numeric', timeZone: 'UTC' }),
      start: day,
      days: bandEnd - day + 1,
    })
    day = bandEnd + 1
  }
  return out
}

/** Day of week for a day number, 0 = Sunday. */
export function weekday(day: number): number {
  return new Date(day * DAY_MS).getUTCDay()
}
