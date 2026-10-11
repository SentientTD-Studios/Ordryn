/**
 * Multi-value list filters (priority, tag) travel as one comma-separated
 * string, e.g. `"2,3"` or `"work,urgent"`. Tag names cannot contain commas.
 */

/** Split a filter value into its entries, dropping blanks and case-insensitive repeats. */
export function parseFilterList(value: string): string[] {
  const seen = new Set<string>()
  const out: string[] = []
  for (const raw of (value || '').split(',')) {
    const v = raw.trim()
    const key = v.toLowerCase()
    if (!v || seen.has(key)) continue
    seen.add(key)
    out.push(v)
  }
  return out
}

/** Add `item` to the list, or remove it if present (case-insensitive). */
export function toggleFilterValue(value: string, item: string): string {
  const list = parseFilterList(value)
  const i = list.findIndex((v) => v.toLowerCase() === item.toLowerCase())
  if (i >= 0) list.splice(i, 1)
  else list.push(item)
  return list.join(',')
}

export function filterHas(value: string, item: string): boolean {
  return parseFilterList(value).some((v) => v.toLowerCase() === item.toLowerCase())
}

/** Short pill label: the one selected name, or "N selected". */
export function filterSummary(names: string[], empty = 'ALL'): string {
  if (!names.length) return empty
  if (names.length === 1) return names[0]
  return `${names.length} selected`
}

const PRIORITY_SHORT: Record<string, string> = { '0': 'NONE', '1': 'LOW', '2': 'MED', '3': 'HIGH' }

export function priorityFilterLabel(value: string): string {
  const list = parseFilterList(value).sort((a, b) => Number(b) - Number(a))
  return list.length ? list.map((p) => PRIORITY_SHORT[p] ?? p).join(' + ') : 'ALL'
}

/** Label for a custom due range, e.g. "OCT 1 – OCT 14", "FROM OCT 1", "UNTIL OCT 14". */
export function dueRangeLabel(from: string, to: string): string {
  const fmt = (d: string) => {
    const [y, m, day] = d.split('-').map(Number)
    if (!y || !m || !day) return d
    return new Date(y, m - 1, day).toLocaleDateString(undefined, { month: 'short', day: 'numeric' }).toUpperCase()
  }
  if (from && to) return `${fmt(from)} – ${fmt(to)}`
  if (from) return `FROM ${fmt(from)}`
  if (to) return `UNTIL ${fmt(to)}`
  return 'CUSTOM'
}
