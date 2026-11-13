/** Server clock minus client clock, in ms, from a response's server_time captured at receivedAtMs. */
export function clockOffsetMs(serverTimeIso: string, receivedAtMs: number): number {
  const server = Date.parse(serverTimeIso)
  return Number.isFinite(server) ? server - receivedAtMs : 0
}

/** Seconds left for the current speaker (negative once over time); null when there is no timer. */
export function speakerSecondsLeft(
  speakerSinceIso: string,
  speakerSeconds: number,
  clientNowMs: number,
  offsetMs: number,
): number | null {
  if (!speakerSeconds) return null
  const since = Date.parse(speakerSinceIso)
  if (!Number.isFinite(since)) return null
  const elapsed = Math.floor((clientNowMs + offsetMs - since) / 1000)
  return speakerSeconds - Math.max(0, elapsed)
}

/** 75 → "1:15", -12 → "-0:12". */
export function formatClock(seconds: number): string {
  const sign = seconds < 0 ? '-' : ''
  const abs = Math.abs(seconds)
  return `${sign}${Math.floor(abs / 60)}:${String(abs % 60).padStart(2, '0')}`
}

/** Seconds as a short setting label: 0 → "Off", 90 → "1m 30s", 120 → "2m". */
export function formatTimerSetting(seconds: number): string {
  if (!seconds) return 'Off'
  const m = Math.floor(seconds / 60)
  const s = seconds % 60
  if (!m) return `${s}s`
  return s ? `${m}m ${s}s` : `${m}m`
}
