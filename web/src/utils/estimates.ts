import type { RefinementState } from '@/api/types'

export const MAX_ESTIMATE_HOURS = 999.75
export const ESTIMATE_STEP = 0.25

/** Rounds to 2 decimals so float noise (0.30000000000000004) never reaches the UI. */
function tidy(n: number): number {
  return Math.round(n * 100) / 100
}

/** "2.5h", "0.25h", "12h". Empty for null/undefined. */
export function formatHours(hours: number | null | undefined): string {
  if (hours == null || !Number.isFinite(hours)) return ''
  return `${tidy(hours)}h`
}

/** True for a non-negative quarter-hour value within the estimate cap. */
export function isValidEstimate(hours: number): boolean {
  if (!Number.isFinite(hours) || hours < 0 || hours > MAX_ESTIMATE_HOURS) return false
  const q = hours * 4
  return Math.abs(q - Math.round(q)) < 1e-9
}

/** Parses an estimate input. '' and whitespace mean "no estimate" (null); invalid input is undefined. */
export function parseEstimateInput(raw: string | number | null | undefined): number | null | undefined {
  if (raw == null) return null
  const text = String(raw).trim()
  if (text === '') return null
  const n = Number(text)
  return isValidEstimate(n) ? n : undefined
}

export const REFINEMENT_LABELS: Record<Exclude<RefinementState, ''>, string> = {
  needs_refinement: 'Needs Refinement',
  refined: 'Refined',
}

export function refinementLabel(state: RefinementState | null | undefined): string {
  return state ? REFINEMENT_LABELS[state] ?? '' : ''
}
