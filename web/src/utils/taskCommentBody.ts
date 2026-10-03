export type MentionToken = {
  start: number
  end: number
  query: string
}

const TASK_REF_RE = /\[\[(\d+)\]\]|#(\d+)\b/g
const TASK_NAME_REF_RE = /(?:^|[^A-Za-z0-9_])#([A-Za-z0-9][A-Za-z0-9_-]{0,80})(?=\s|$|[.,!?;:])/g
const IMAGE_RE = /!\[([^\]]*)]\(\s*<?([^)\s>]+)>?(?:\s+["'][^"']*["'])?\s*\)/g

/** http(s) URLs and local /uploads/ keys only — never javascript: or data:. */
export function isSafeImageSrc(src: string): boolean {
  const s = src.trim()
  if (!s || /\s/.test(s) || s.includes('\\') || s.includes('<')) return false
  if (s.startsWith('/uploads/')) {
    return !s.includes('..') && !s.startsWith('//')
  }
  if (s.startsWith('/') || s.startsWith('//')) return false
  try {
    const u = new URL(s)
    return (u.protocol === 'https:' || u.protocol === 'http:') && !!u.hostname
  } catch {
    return false
  }
}

export function hasImageMarkdown(body: string): boolean {
  const re = new RegExp(IMAGE_RE.source, 'g')
  let m: RegExpExecArray | null
  while ((m = re.exec(body))) {
    if (isSafeImageSrc(m[2] || '')) return true
  }
  return false
}

/** Strip image markdown so list previews do not show raw ![alt](url). */
export function previewWithoutImages(body: string, limit = 0): string {
  const re = new RegExp(IMAGE_RE.source, 'g')
  let text = body.replace(re, (_, alt) => {
    const a = String(alt || '').trim()
    return a ? `[image: ${a}]` : '[image]'
  })
  text = text.replace(/\s+/g, ' ').trim()
  if (limit > 0 && text.length > limit) return text.slice(0, limit - 1) + '…'
  return text
}

export function extractTaskRefIDs(body: string): number[] {
  const ids: number[] = []
  const seen = new Set<number>()
  const re = new RegExp(TASK_REF_RE.source, 'g')
  let m: RegExpExecArray | null
  while ((m = re.exec(body))) {
    const id = Number(m[1] || m[2])
    if (!id || seen.has(id)) continue
    seen.add(id)
    ids.push(id)
  }
  return ids
}

export function extractTaskRefQueries(body: string): string[] {
  const queries: string[] = []
  const seen = new Set<string>()
  const re = new RegExp(TASK_NAME_REF_RE.source, 'g')
  let m: RegExpExecArray | null
  while ((m = re.exec(body))) {
    const q = (m[1] ?? '').trim()
    if (!q || /^\d+$/.test(q)) continue
    const key = q.toLowerCase()
    if (seen.has(key)) continue
    seen.add(key)
    queries.push(q)
  }
  return queries
}

export function mentionTokenAtCursor(body: string, cursor: number): MentionToken | null {
  if (cursor < 0) cursor = 0
  if (cursor > body.length) cursor = body.length
  const before = body.slice(0, cursor)
  const m = before.match(/(?:^|[^A-Za-z0-9_@])@([A-Za-z0-9_]{0,32})$/)
  if (!m) return null
  const queryBefore = m[1]
  const afterMatch = body.slice(cursor).match(/^([A-Za-z0-9_]{0,32})/)
  const queryAfter = afterMatch?.[1] ?? ''
  if (queryBefore.length + queryAfter.length > 32) return null
  const atIndex = before.length - 1 - queryBefore.length
  if (atIndex < 0 || body[atIndex] !== '@') return null
  return {
    start: atIndex,
    end: cursor + queryAfter.length,
    query: queryBefore + queryAfter,
  }
}

export function insertMention(
  body: string,
  token: MentionToken,
  userName: string,
): { body: string; cursor: number } {
  const mention = `@${userName}`
  const after = body.slice(token.end)
  const spacer = /^\s/.test(after) ? '' : ' '
  const next = body.slice(0, token.start) + mention + spacer + after
  return { body: next, cursor: token.start + mention.length + spacer.length }
}

export function isInsertedTaskRef(body: string, id: number): boolean {
  return new RegExp(`\\[\\[${id}\\]\\]`).test(body)
}

function escapeRegex(s: string): string {
  return s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

export function insertTaskRef(body: string, id: number, query?: string): string {
  if (isInsertedTaskRef(body, id)) return body
  const hash = new RegExp(`(^|[^A-Za-z0-9_])#${id}\\b`)
  if (hash.test(body)) {
    return body.replace(hash, `$1[[${id}]]`)
  }
  if (query) {
    const q = escapeRegex(query.trim())
    if (q) {
      const qHash = new RegExp(`(^|[^A-Za-z0-9_])#${q}(?=\\s|$|[.,!?;:])`, 'i')
      if (qHash.test(body)) {
        return body.replace(qHash, `$1[[${id}]]`)
      }
    }
  }
  const trimmed = body.trimEnd()
  if (!trimmed) return `[[${id}]]`
  return `${trimmed} [[${id}]]`
}

/** Insert markdown on its own line, GitHub-style, at the cursor. */
export function insertMarkdownAtCursor(
  body: string,
  markdown: string,
  start: number,
  end = start,
): { body: string; cursor: number } {
  if (start < 0) start = 0
  if (end < start) end = start
  if (start > body.length) start = body.length
  if (end > body.length) end = body.length
  const before = body.slice(0, start)
  const after = body.slice(end)
  const lead = before.length > 0 && !before.endsWith('\n') ? '\n' : ''
  const trail = after.length > 0 && !after.startsWith('\n') ? '\n' : ''
  const block = `${lead}${markdown}${trail}`
  return { body: before + block + after, cursor: before.length + lead.length + markdown.length }
}
