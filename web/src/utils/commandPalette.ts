// Ranking for the command palette (Ctrl/⌘+K): prefix and word-start matches
// beat substrings, which beat letters in order ("nwtsk" finds "New task").

export type PaletteItem = {
  id: string
  label: string
  /** Extra words that should match, e.g. "create add" for "New task". */
  keywords?: string
  group: string
}

/** Lower is better; null means no match. */
export function matchScore(text: string, query: string): number | null {
  const t = text.toLowerCase()
  const q = query.trim().toLowerCase()
  if (!q) return 0
  if (t === q) return 0
  if (t.startsWith(q)) return 1
  const words = t.split(/[\s\-_/.:#]+/)
  if (words.some((w) => w.startsWith(q))) return 2
  const idx = t.indexOf(q)
  if (idx >= 0) return 3 + idx / 1000
  // Letters in order, possibly with gaps; tighter spans score better.
  let pos = -1
  let first = -1
  for (const ch of q) {
    if (ch === ' ') continue
    pos = t.indexOf(ch, pos + 1)
    if (pos < 0) return null
    if (first < 0) first = pos
  }
  return 5 + (pos - first) / 100
}

/** Items matching query, best first; ties keep their original order. */
export function rankItems<T extends PaletteItem>(items: T[], query: string): T[] {
  if (!query.trim()) return items
  const scored: { item: T; score: number; i: number }[] = []
  items.forEach((item, i) => {
    const a = matchScore(item.label, query)
    const b = item.keywords ? matchScore(item.keywords, query) : null
    const best = a === null ? b : b === null ? a : Math.min(a, b + 0.5)
    if (best !== null) scored.push({ item, score: best, i })
  })
  scored.sort((x, y) => x.score - y.score || x.i - y.i)
  return scored.map((s) => s.item)
}

/** "#123" or "123" → 123, for jumping straight to a task. */
export function taskNumber(query: string): number | null {
  const m = query.trim().match(/^#?(\d{1,9})$/)
  return m ? Number(m[1]) : null
}
