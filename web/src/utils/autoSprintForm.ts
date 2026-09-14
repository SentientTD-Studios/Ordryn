/** Vue number inputs (type="number" or v-model.number) emit numbers, not strings. */
export function parseOptionalNumberInput(value: unknown): number | null {
  if (value === '' || value == null) return null
  if (typeof value === 'number') return value
  if (typeof value === 'string') {
    const trimmed = value.trim()
    if (!trimmed) return null
    return Number(trimmed)
  }
  return Number(value)
}

function addUTCDays(iso: string, days: number): string {
  const d = new Date(`${iso}T00:00:00Z`)
  d.setUTCDate(d.getUTCDate() + days)
  return d.toISOString().slice(0, 10)
}

const PREVIEW_PREV_END = '2026-08-31'

export function formatAutoSprintPreview(lengthValue: unknown, lockValue: unknown): string {
  const length = parseOptionalNumberInput(lengthValue)
  if (length == null || !Number.isInteger(length) || length < 1) return ''
  const lockDays = parseOptionalNumberInput(lockValue)
  if (lockDays != null && (!Number.isInteger(lockDays) || lockDays < 0 || lockDays >= length)) return ''
  const start = addUTCDays(PREVIEW_PREV_END, 1)
  const end = addUTCDays(start, length - 1)
  if (lockDays == null) {
    return `Example: a sprint ending ${PREVIEW_PREV_END} is followed by ${start} – ${end} with no lock date.`
  }
  const lock = addUTCDays(end, -lockDays)
  return `Example: a sprint ending ${PREVIEW_PREV_END} is followed by ${start} – ${end}, locking on ${lock}.`
}
