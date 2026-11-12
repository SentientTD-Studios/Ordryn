import type { Task } from '@/api/types'

/**
 * Search box helpers. They mirror the server's tasks.ParseSearchQuery: words
 * split on spaces outside double quotes, and every word must match.
 */

const QUALIFIERS = new Set([
  'id', 'project', 'proj', 'tag', 'tags', 'status', 'state', 'is', 'claimed', 'assignee', 'assigned',
])

/** Split a query into words, keeping "quoted phrases" whole (quotes removed). */
export function tokenizeSearch(raw: string): string[] {
  const out: string[] = []
  let cur = ''
  let inQuote = false
  for (const ch of raw || '') {
    if (ch === '"') {
      inQuote = !inQuote
      cur += ch
    } else if (/\s/.test(ch) && !inQuote) {
      if (cur) out.push(cur)
      cur = ''
    } else {
      cur += ch
    }
  }
  if (cur) out.push(cur)
  return out.map((t) => t.replace(/"/g, '').trim()).filter(Boolean)
}

function isQualifier(token: string): boolean {
  const i = token.indexOf(':')
  return i > 0 && QUALIFIERS.has(token.slice(0, i).toLowerCase())
}

/** Free-text words of a search query: no qualifiers and no #ids. Used for highlighting. */
export function freeTextTerms(raw: string): string[] {
  return tokenizeSearch(raw).filter((t) => !isQualifier(t) && !/^#\d+$/.test(t))
}

/** The task id a lone "#123" / "123" query names, or null. */
export function searchTaskId(raw: string): number | null {
  const m = /^#?(\d+)$/.exec((raw || '').trim())
  if (!m) return null
  const n = Number(m[1])
  return n > 0 && Number.isSafeInteger(n) ? n : null
}

/**
 * Whether a task matches a list's keyword filter (no qualifiers): every word
 * appears in its title, description, or a tag name, or names its id. Mirrors the
 * server so live updates keep or drop rows the way a reload would.
 */
export function taskMatchesKeyword(task: Pick<Task, 'id' | 'title' | 'description' | 'tags'>, raw: string): boolean {
  const terms = tokenizeSearch(raw)
  if (!terms.length) return true
  const hay = [task.title, task.description, ...(task.tags || []).map((t) => t.name)].join('\n').toLowerCase()
  return terms.every((term) => hay.includes(term.toLowerCase()) || searchTaskId(term) === task.id)
}

/**
 * Whether a full search result matched only through its comments: some word is
 * not in the title (as typed or one typo off), description, or a tag name, and
 * isn't the task's id. The search page labels these, since comments aren't
 * shown in results.
 */
export function matchedInComments(task: Pick<Task, 'id' | 'title' | 'description' | 'tags'>, terms: string[]): boolean {
  const hay = [task.title, task.description, ...(task.tags || []).map((t) => t.name)].join('\n').toLowerCase()
  const titleWords = (task.title || '').toLowerCase().match(/[\p{L}\p{N}]+/gu) || []
  return terms.some((raw) => {
    const term = raw.toLowerCase()
    if (hay.includes(term) || searchTaskId(raw) === task.id) return false
    return !(isFuzzyTerm(term) && titleWords.some((w) => editDistance(w, term) === 1))
  })
}

/**
 * Site search also matches a word one typo away (an inserted, missing, wrong
 * or swapped letter) from any term of five or more letters, in titles and tag
 * names. Mirrors the server's tasks.fuzzyTerm and editDistance.
 */
export const MIN_FUZZY_TERM_LENGTH = 5

export function isFuzzyTerm(term: string): boolean {
  const chars = [...term.trim()]
  return chars.length >= MIN_FUZZY_TERM_LENGTH && !/^#?\d+$/.test(term.trim()) && chars.every((c) => /[\p{L}\p{N}]/u.test(c))
}

/** Optimal string alignment distance: Levenshtein plus adjacent swaps. */
export function editDistance(a: string, b: string): number {
  const x = [...a]
  const y = [...b]
  let prev2: number[] = new Array(y.length + 1).fill(0)
  let prev: number[] = Array.from({ length: y.length + 1 }, (_, j) => j)
  let cur: number[] = new Array(y.length + 1).fill(0)
  for (let i = 1; i <= x.length; i++) {
    cur[0] = i
    for (let j = 1; j <= y.length; j++) {
      const cost = x[i - 1] === y[j - 1] ? 0 : 1
      cur[j] = Math.min(prev[j]! + 1, cur[j - 1]! + 1, prev[j - 1]! + cost)
      if (i > 1 && j > 1 && x[i - 1] === y[j - 2] && x[i - 2] === y[j - 1]) {
        cur[j] = Math.min(cur[j]!, prev2[j - 2]! + 1)
      }
    }
    ;[prev2, prev, cur] = [prev, cur, prev2]
  }
  return prev[y.length]!
}

export type HighlightSegment = { text: string; match: boolean }

/**
 * Split text into plain and matching runs for the given words
 * (case-insensitive). Whole words one typo from a long enough term are marked
 * too, so typo matches show what they matched. Worked out here from the text
 * on screen, not sent by the server: the server's dictionary holds words from
 * every task, and echoing its spellings would leak words from tasks the
 * searcher can't open.
 */
export function highlightSegments(text: string, terms: string[]): HighlightSegment[] {
  const words = [...new Set(terms.map((t) => t.toLowerCase()).filter(Boolean))].sort((a, b) => b.length - a.length)
  if (!text || !words.length) return [{ text: text || '', match: false }]
  const lower = text.toLowerCase()
  const marks = new Array<boolean>(text.length).fill(false)
  for (const w of words) {
    let from = 0
    for (;;) {
      const i = lower.indexOf(w, from)
      if (i < 0) break
      for (let j = i; j < i + w.length; j++) marks[j] = true
      from = i + w.length
    }
  }
  const fuzzy = words.filter(isFuzzyTerm)
  if (fuzzy.length) {
    for (const m of lower.matchAll(/[\p{L}\p{N}]+/gu)) {
      const word = m[0]
      if (fuzzy.some((t) => !word.includes(t) && editDistance(word, t) === 1)) {
        for (let j = m.index!; j < m.index! + word.length; j++) marks[j] = true
      }
    }
  }
  const out: HighlightSegment[] = []
  for (let i = 0; i < text.length; i++) {
    const last = out[out.length - 1]
    if (last && last.match === marks[i]) last.text += text[i]
    else out.push({ text: text[i]!, match: marks[i]! })
  }
  return out
}

/** A one-line excerpt of a description around the first matching word. */
export function searchExcerpt(description: string, terms: string[], width = 140): string {
  const plain = (description || '').replace(/<[^>]*>/g, ' ').replace(/[#*_`>[\]()]/g, ' ').replace(/\s+/g, ' ').trim()
  if (!plain) return ''
  const lower = plain.toLowerCase()
  let at = -1
  for (const t of terms) {
    const i = lower.indexOf(t.toLowerCase())
    if (i >= 0 && (at < 0 || i < at)) at = i
  }
  if (at < 0 || plain.length <= width) return plain.length <= width ? plain : plain.slice(0, width).trimEnd() + '…'
  const start = Math.max(0, at - Math.floor(width / 3))
  const end = Math.min(plain.length, start + width)
  return (start > 0 ? '…' : '') + plain.slice(start, end).trim() + (end < plain.length ? '…' : '')
}
