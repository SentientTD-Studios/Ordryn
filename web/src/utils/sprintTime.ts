import type { ProjectSprint } from '@/api/types'
import { formatHours } from './estimates.ts'
import { formatDuration } from './timeBlocks.ts'

/** "6h 30m logged of 12h estimated", "2h logged", "12h estimated", or '' when neither is set. */
export function sprintTimeSummary(s: Pick<ProjectSprint, 'time_spent_minutes' | 'estimate_hours'>): string {
  const spent = s.time_spent_minutes ?? 0
  const est = s.estimate_hours ?? 0
  if (spent > 0 && est > 0) return `${formatDuration(spent)} logged of ${formatHours(est)} estimated`
  if (spent > 0) return `${formatDuration(spent)} logged`
  if (est > 0) return `${formatHours(est)} estimated`
  return ''
}
