import type { ExtensionDelivery } from '../api/types.ts'

export function isRateLimitedError(text?: string | null): boolean {
  if (!text) return false
  return /rate[- ]limit/i.test(text)
}

export function isRateLimitedDelivery(row: ExtensionDelivery): boolean {
  return row.http_code === 429 || row.status === 'rate_limited' || isRateLimitedError(row.error)
}

export function rateLimitNotice(lastError?: string, deliveries?: ExtensionDelivery[]): string | null {
  if (isRateLimitedError(lastError)) {
    return lastError!.trim()
  }
  const hit = (deliveries || []).find(isRateLimitedDelivery)
  if (!hit) return null
  if (hit.error && isRateLimitedError(hit.error)) {
    return hit.error
  }
  if (hit.next_attempt_at) {
    return `This destination is rate-limited. Delivery will retry at ${hit.next_attempt_at}.`
  }
  return 'This destination is rate-limited. Deliveries will retry automatically.'
}
