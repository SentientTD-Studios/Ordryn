/** Date helpers for the planner. Dates are YYYY-MM-DD strings. */

/** The day n days after iso (n may be negative). */
export function addDays(iso: string, n: number): string {
  const [y, m, d] = iso.split('-').map(Number)
  return new Date(Date.UTC(y, m - 1, d + n)).toISOString().slice(0, 10)
}

/** The next Monday strictly after iso. */
export function nextMonday(iso: string): string {
  const [y, m, d] = iso.split('-').map(Number)
  const dow = new Date(Date.UTC(y, m - 1, d)).getUTCDay() // 0 = Sunday
  const ahead = ((8 - dow) % 7) || 7
  return addDays(iso, ahead)
}

/** Snooze choices relative to today. */
export function snoozeOptions(today: string): { label: string; until: string }[] {
  return [
    { label: 'Tomorrow', until: addDays(today, 1) },
    { label: 'In 3 days', until: addDays(today, 3) },
    { label: 'Next Monday', until: nextMonday(today) },
    { label: 'In a week', until: addDays(today, 7) },
  ]
}

/** "Today", "Tomorrow", or a short weekday + date label for an upcoming day. */
export function dayLabel(iso: string, today: string): string {
  if (iso === today) return 'Today'
  if (iso === addDays(today, 1)) return 'Tomorrow'
  const [y, m, d] = iso.split('-').map(Number)
  return new Date(Date.UTC(y, m - 1, d)).toLocaleDateString(undefined, {
    weekday: 'long',
    month: 'short',
    day: 'numeric',
    timeZone: 'UTC',
  })
}
