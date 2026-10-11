/**
 * Quick-add parsing: turns "Fix login #bug !high tomorrow" into a title plus
 * task fields. Anything that does not resolve (an unknown tag or project, a
 * word that only looks like a date) stays in the title, so nothing typed is lost.
 */

export type QuickAddTag = { id: number; name: string }
export type QuickAddProject = { id: number; name: string }

export type QuickAddChip =
  | { kind: 'due'; label: string; value: string }
  | { kind: 'priority'; label: string; value: number }
  | { kind: 'tag'; label: string; value: number }
  | { kind: 'project'; label: string; value: number }
  | { kind: 'claim'; label: string }

export type QuickAddResult = {
  title: string
  dueDate?: string
  priority?: number
  tagIds: number[]
  projectId?: number
  claim: boolean
  chips: QuickAddChip[]
}

export type QuickAddOptions = {
  /** Reference "today"; defaults to the current local date. */
  today?: Date
  /** Tags that `#name` may resolve to (already scoped to the target project). */
  tags?: QuickAddTag[]
  /** Projects that `+name` may resolve to. Omit to disable `+project`. */
  projects?: QuickAddProject[]
  /** Whether `@me` (claim after create) is understood. */
  allowClaim?: boolean
}

const PRIORITIES: Record<string, number> = {
  low: 1,
  l: 1,
  '1': 1,
  med: 2,
  medium: 2,
  m: 2,
  '2': 2,
  high: 3,
  h: 3,
  '3': 3,
}
export const PRIORITY_LABELS = ['None', 'Low', 'Medium', 'High']

const WEEKDAYS: Record<string, number> = {
  sunday: 0,
  monday: 1,
  tuesday: 2,
  wednesday: 3,
  thursday: 4,
  friday: 5,
  saturday: 6,
}
// Short forms collide with real words ("sun", "sat", "wed"), so they only count after a trigger word.
const WEEKDAY_SHORT: Record<string, number> = {
  sun: 0,
  mon: 1,
  tue: 2,
  tues: 2,
  wed: 3,
  thu: 4,
  thur: 4,
  thurs: 4,
  fri: 5,
  sat: 6,
}
const MONTHS: Record<string, number> = {
  jan: 0,
  january: 0,
  feb: 1,
  february: 1,
  mar: 2,
  march: 2,
  apr: 3,
  april: 3,
  may: 4,
  jun: 5,
  june: 5,
  jul: 6,
  july: 6,
  aug: 7,
  august: 7,
  sep: 8,
  sept: 8,
  september: 8,
  oct: 9,
  october: 9,
  nov: 10,
  november: 10,
  dec: 11,
  december: 11,
}
const TRIGGERS = new Set(['on', 'by', 'due'])

export function formatLocalDate(d: Date): string {
  const y = d.getFullYear()
  const m = String(d.getMonth() + 1).padStart(2, '0')
  const day = String(d.getDate()).padStart(2, '0')
  return `${y}-${m}-${day}`
}

function addDays(d: Date, n: number): Date {
  return new Date(d.getFullYear(), d.getMonth(), d.getDate() + n)
}

function addMonths(d: Date, n: number): Date {
  const target = new Date(d.getFullYear(), d.getMonth() + n, 1)
  const lastDay = new Date(target.getFullYear(), target.getMonth() + 1, 0).getDate()
  return new Date(target.getFullYear(), target.getMonth(), Math.min(d.getDate(), lastDay))
}

/** The next given weekday, counting today. */
function upcomingWeekday(today: Date, dow: number): Date {
  return addDays(today, (dow - today.getDay() + 7) % 7)
}

/** The given weekday in the week after this one (weeks start on Monday). */
function weekdayNextWeek(today: Date, dow: number): Date {
  const mondayOffset = (today.getDay() + 6) % 7
  const nextMonday = addDays(today, 7 - mondayOffset)
  return addDays(nextMonday, (dow + 6) % 7)
}

function validDate(y: number, m: number, d: number): Date | null {
  const out = new Date(y, m, d)
  if (out.getFullYear() !== y || out.getMonth() !== m || out.getDate() !== d) return null
  return out
}

function clean(word: string): string {
  return word.toLowerCase().replace(/[.,;:!?]+$/, '')
}

function dayNumber(word: string): number | null {
  const m = /^(\d{1,2})(st|nd|rd|th)?$/.exec(clean(word))
  return m ? parseInt(m[1], 10) : null
}

function yearNumber(word: string | undefined): number | null {
  if (!word) return null
  const m = /^(\d{4})$/.exec(clean(word))
  return m ? parseInt(m[1], 10) : null
}

/** Month + day with no year: this year, or next year once the date has passed. */
function monthDay(today: Date, month: number, day: number, year: number | null): Date | null {
  if (year != null) return validDate(year, month, day)
  const sameYear = validDate(today.getFullYear(), month, day)
  if (sameYear && sameYear >= today) return sameYear
  return validDate(today.getFullYear() + 1, month, day)
}

/** Tries to read a date starting at words[i]; returns the date and how many words it used. */
function matchDate(words: string[], i: number, today: Date, triggered: boolean): { date: Date; used: number } | null {
  const w = clean(words[i])
  const next = words[i + 1] != null ? clean(words[i + 1]) : ''

  if (w === 'today' || w === 'tonight') return { date: today, used: 1 }
  if (w === 'tomorrow' || w === 'tmr' || w === 'tmrw') return { date: addDays(today, 1), used: 1 }
  if (w in WEEKDAYS) return { date: upcomingWeekday(today, WEEKDAYS[w]), used: 1 }
  if (triggered && w in WEEKDAY_SHORT) return { date: upcomingWeekday(today, WEEKDAY_SHORT[w]), used: 1 }

  if (w === 'next') {
    if (next === 'week') return { date: weekdayNextWeek(today, 1), used: 2 }
    if (next === 'month') return { date: new Date(today.getFullYear(), today.getMonth() + 1, 1), used: 2 }
    const dow = WEEKDAYS[next] ?? WEEKDAY_SHORT[next]
    if (dow != null) return { date: weekdayNextWeek(today, dow), used: 2 }
  }

  if (w === 'in' && words[i + 2] != null) {
    const n = next === 'a' || next === 'an' ? 1 : /^\d{1,3}$/.test(next) ? parseInt(next, 10) : NaN
    const unit = clean(words[i + 2])
    if (!Number.isNaN(n)) {
      if (unit === 'day' || unit === 'days') return { date: addDays(today, n), used: 3 }
      if (unit === 'week' || unit === 'weeks') return { date: addDays(today, n * 7), used: 3 }
      if (unit === 'month' || unit === 'months') return { date: addMonths(today, n), used: 3 }
    }
  }

  const iso = /^(\d{4})-(\d{2})-(\d{2})$/.exec(w)
  if (iso) {
    const d = validDate(parseInt(iso[1], 10), parseInt(iso[2], 10) - 1, parseInt(iso[3], 10))
    if (d) return { date: d, used: 1 }
  }

  // "oct 20", "october 20th 2027"
  if (w in MONTHS) {
    const day = dayNumber(words[i + 1] ?? '')
    if (day != null) {
      const year = yearNumber(words[i + 2])
      const d = monthDay(today, MONTHS[w], day, year)
      if (d) return { date: d, used: year != null ? 3 : 2 }
    }
  }
  // "20 oct", "20th october 2027"
  const lead = dayNumber(words[i])
  if (lead != null && next in MONTHS) {
    const year = yearNumber(words[i + 2])
    const d = monthDay(today, MONTHS[next], lead, year)
    if (d) return { date: d, used: year != null ? 3 : 2 }
  }
  return null
}

function normalizeName(s: string): string {
  return s.toLowerCase().replace(/[^\p{L}\p{N}]+/gu, '')
}

function findProject(projects: QuickAddProject[], key: string): QuickAddProject | null {
  const k = normalizeName(key)
  if (!k) return null
  const exact = projects.filter((p) => normalizeName(p.name) === k)
  if (exact.length === 1) return exact[0]
  if (exact.length > 1) return null
  const prefix = projects.filter((p) => normalizeName(p.name).startsWith(k))
  return prefix.length === 1 ? prefix[0] : null
}

function friendlyDate(d: Date, today: Date): string {
  const diff = Math.round((d.getTime() - today.getTime()) / 86400000)
  if (diff === 0) return 'Today'
  if (diff === 1) return 'Tomorrow'
  const opts: Intl.DateTimeFormatOptions = { weekday: 'short', month: 'short', day: 'numeric' }
  if (d.getFullYear() !== today.getFullYear()) opts.year = 'numeric'
  return d.toLocaleDateString(undefined, opts)
}

export function parseQuickAdd(text: string, opts: QuickAddOptions = {}): QuickAddResult {
  const now = opts.today ?? new Date()
  const today = new Date(now.getFullYear(), now.getMonth(), now.getDate())
  const words = text.trim().split(/\s+/).filter(Boolean)
  const used = new Array<boolean>(words.length).fill(false)
  const result: QuickAddResult = { title: '', tagIds: [], claim: false, chips: [] }
  let due: Date | null = null

  for (let i = 0; i < words.length; i++) {
    const raw = words[i]
    const lower = raw.toLowerCase()

    if (raw.length > 1 && raw.startsWith('#') && opts.tags?.length) {
      const name = lower.slice(1)
      const tag = opts.tags.find((t) => t.name.toLowerCase() === name)
      if (tag) {
        used[i] = true
        if (!result.tagIds.includes(tag.id)) {
          result.tagIds.push(tag.id)
          result.chips.push({ kind: 'tag', label: tag.name, value: tag.id })
        }
      }
      continue
    }

    if (raw.startsWith('!')) {
      const key = /^!{1,3}$/.test(raw) ? String(raw.length) : lower.slice(1)
      const p = PRIORITIES[key]
      if (p != null) {
        used[i] = true
        result.priority = p
      }
      continue
    }

    if (lower === '@me' && opts.allowClaim) {
      used[i] = true
      result.claim = true
      continue
    }

    if (raw.length > 1 && raw.startsWith('+') && opts.projects?.length && result.projectId == null) {
      const project = findProject(opts.projects, raw.slice(1))
      if (project) {
        used[i] = true
        result.projectId = project.id
        result.chips.push({ kind: 'project', label: project.name, value: project.id })
      }
      continue
    }

    if (due) continue
    const prev = i > 0 && !used[i - 1] ? clean(words[i - 1]) : ''
    const triggered = TRIGGERS.has(prev) || clean(raw) === 'next'
    const hit = matchDate(words, i, today, triggered)
    if (hit) {
      due = hit.date
      for (let k = 0; k < hit.used; k++) used[i + k] = true
      if (TRIGGERS.has(prev)) used[i - 1] = true
      i += hit.used - 1
    }
  }

  const title = words.filter((_, i) => !used[i]).join(' ')
  // A title made only of keywords ("Today") is taken literally.
  if (!title) return { title: text.trim(), tagIds: [], claim: false, chips: [] }
  result.title = title

  if (due) {
    result.dueDate = formatLocalDate(due)
    result.chips.unshift({ kind: 'due', label: friendlyDate(due, today), value: result.dueDate })
  }
  if (result.priority != null) {
    result.chips.push({ kind: 'priority', label: PRIORITY_LABELS[result.priority], value: result.priority })
  }
  if (result.claim) result.chips.push({ kind: 'claim', label: 'Claim' })
  return result
}
