/**
 * Helpers for the hourly calendar. Days are 'YYYY-MM-DD' strings and minutes
 * count from local midnight, both in the user's profile time zone (which may
 * differ from the browser's).
 */

export const BLOCK_STEP_MINUTES = 15
export const DAY_MINUTES = 24 * 60
export const MAX_BLOCK_MINUTES = DAY_MINUTES

const partsCache = new Map<string, Intl.DateTimeFormat>()

function formatter(tz: string): Intl.DateTimeFormat {
  let f = partsCache.get(tz)
  if (!f) {
    try {
      f = new Intl.DateTimeFormat('en-US', {
        timeZone: tz,
        hourCycle: 'h23',
        year: 'numeric',
        month: '2-digit',
        day: '2-digit',
        hour: '2-digit',
        minute: '2-digit',
        second: '2-digit',
      })
    } catch {
      f = formatter('UTC')
    }
    partsCache.set(tz, f)
  }
  return f
}

type WallClock = { y: number; mo: number; d: number; h: number; mi: number; s: number }

function wallClock(date: Date, tz: string): WallClock {
  const out: Record<string, number> = {}
  for (const p of formatter(tz).formatToParts(date)) {
    if (p.type !== 'literal') out[p.type] = Number(p.value)
  }
  return { y: out.year, mo: out.month, d: out.day, h: out.hour % 24, mi: out.minute, s: out.second }
}

function pad(n: number): string {
  return String(n).padStart(2, '0')
}

function parseDay(day: string): [number, number, number] {
  const [y, m, d] = day.split('-').map(Number)
  return [y, m, d]
}

/** The zone's offset from UTC at that instant, in minutes (e.g. -300 for New York in winter). */
export function tzOffsetMinutes(date: Date, tz: string): number {
  const w = wallClock(date, tz)
  const asUTC = Date.UTC(w.y, w.mo - 1, w.d, w.h, w.mi, w.s)
  return Math.round((asUTC - Math.floor(date.getTime() / 1000) * 1000) / 60000)
}

/** The instant at `minutes` past midnight on `day` in tz. Wall times skipped by DST land just after the gap. */
export function zonedToInstant(day: string, minutes: number, tz: string): Date {
  const [y, m, d] = parseDay(day)
  const guess = Date.UTC(y, m - 1, d, 0, minutes)
  const first = guess - tzOffsetMinutes(new Date(guess), tz) * 60000
  // Re-check at the result: the offset can differ across a DST change.
  const second = guess - tzOffsetMinutes(new Date(first), tz) * 60000
  const matches = (t: number) => zonedDay(new Date(t), tz) === day && zonedMinutes(new Date(t), tz) === minutes
  if (matches(second)) return new Date(second)
  if (matches(first)) return new Date(first)
  // The wall time doesn't exist (skipped by DST): land just after the gap.
  return new Date(Math.max(first, second))
}

/** The tz calendar day of an instant. */
export function zonedDay(date: Date, tz: string): string {
  const w = wallClock(date, tz)
  return `${w.y}-${pad(w.mo)}-${pad(w.d)}`
}

/** Minutes past tz midnight of an instant. */
export function zonedMinutes(date: Date, tz: string): number {
  const w = wallClock(date, tz)
  return w.h * 60 + w.mi
}

export function addDays(day: string, n: number): string {
  const [y, m, d] = parseDay(day)
  const t = new Date(Date.UTC(y, m - 1, d + n))
  return `${t.getUTCFullYear()}-${pad(t.getUTCMonth() + 1)}-${pad(t.getUTCDate())}`
}

/** 0 = Sunday. */
export function dayOfWeek(day: string): number {
  const [y, m, d] = parseDay(day)
  return new Date(Date.UTC(y, m - 1, d)).getUTCDay()
}

/** The Sunday starting the week that holds `day` (matches the month grid). */
export function weekStart(day: string): string {
  return addDays(day, -dayOfWeek(day))
}

export function snapMinutes(minutes: number, step = BLOCK_STEP_MINUTES): number {
  return Math.round(minutes / step) * step
}

export function clamp(n: number, lo: number, hi: number): number {
  return Math.min(hi, Math.max(lo, n))
}

/** "2h 30m", "45m", "3h". */
export function formatDuration(minutes: number): string {
  const total = Math.max(0, Math.round(minutes))
  if (!total) return '0m'
  const h = Math.floor(total / 60)
  const m = total % 60
  if (h && m) return `${h}h ${m}m`
  if (h) return `${h}h`
  return `${m}m`
}

/** "9 AM", "12 PM", "3:30 PM" for minutes past midnight. */
export function formatClock(minutes: number): string {
  const m = ((Math.round(minutes) % DAY_MINUTES) + DAY_MINUTES) % DAY_MINUTES
  const h24 = Math.floor(m / 60)
  const mm = m % 60
  const suffix = h24 < 12 ? 'AM' : 'PM'
  const h12 = h24 % 12 === 0 ? 12 : h24 % 12
  return mm ? `${h12}:${pad(mm)} ${suffix}` : `${h12} ${suffix}`
}

export type SpanLike = { id: number; start_at: string; end_at: string }

export type DaySegment<T extends SpanLike> = {
  block: T
  /** Minutes past midnight where this day's part starts and ends. */
  startMin: number
  endMin: number
  /** True when the block began on an earlier day / runs into the next one. */
  continuesBefore: boolean
  continuesAfter: boolean
  lane: number
  lanes: number
}

/**
 * The parts of blocks that fall on `day`, with side-by-side lanes for overlaps.
 * Blocks crossing midnight are split across both days.
 */
export function daySegments<T extends SpanLike>(blocks: T[], day: string, tz: string): DaySegment<T>[] {
  const dayStart = zonedToInstant(day, 0, tz).getTime()
  const dayEnd = zonedToInstant(addDays(day, 1), 0, tz).getTime()
  const segs: DaySegment<T>[] = []
  for (const block of blocks) {
    const s = new Date(block.start_at)
    const e = new Date(block.end_at)
    if (!(s.getTime() < dayEnd && e.getTime() > dayStart)) continue
    const continuesBefore = s.getTime() < dayStart
    const continuesAfter = e.getTime() > dayEnd
    const startMin = continuesBefore ? 0 : zonedMinutes(s, tz)
    let endMin = continuesAfter ? DAY_MINUTES : zonedMinutes(e, tz)
    if (!continuesAfter && endMin === 0) endMin = DAY_MINUTES
    if (endMin <= startMin) endMin = Math.min(DAY_MINUTES, startMin + BLOCK_STEP_MINUTES)
    segs.push({ block, startMin, endMin, continuesBefore, continuesAfter, lane: 0, lanes: 1 })
  }
  segs.sort((a, b) => a.startMin - b.startMin || b.endMin - a.endMin || a.block.id - b.block.id)

  // Group overlapping runs into clusters; each cluster shares a lane count.
  let cluster: DaySegment<T>[] = []
  let clusterEnd = -1
  const laneEnds: number[] = []
  const flush = () => {
    const lanes = Math.max(1, ...cluster.map((c) => c.lane + 1))
    for (const c of cluster) c.lanes = lanes
    cluster = []
    laneEnds.length = 0
  }
  for (const seg of segs) {
    if (cluster.length && seg.startMin >= clusterEnd) flush()
    let lane = laneEnds.findIndex((end) => end <= seg.startMin)
    if (lane === -1) {
      lane = laneEnds.length
      laneEnds.push(seg.endMin)
    } else {
      laneEnds[lane] = seg.endMin
    }
    seg.lane = lane
    cluster.push(seg)
    clusterEnd = Math.max(clusterEnd, seg.endMin)
  }
  if (cluster.length) flush()
  return segs
}

/** Minutes of `blocks` that fall on `day` (blocks crossing midnight count their part only). */
export function minutesOnDay<T extends SpanLike>(blocks: T[], day: string, tz: string): number {
  return daySegments(blocks, day, tz).reduce((sum, s) => sum + (s.endMin - s.startMin), 0)
}
