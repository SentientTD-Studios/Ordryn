import { Marked, type TokenizerAndRendererExtension } from 'marked'
import DOMPurify from 'dompurify'
import hljs from 'highlight.js/lib/core'
import bash from 'highlight.js/lib/languages/bash'
import c from 'highlight.js/lib/languages/c'
import cpp from 'highlight.js/lib/languages/cpp'
import csharp from 'highlight.js/lib/languages/csharp'
import css from 'highlight.js/lib/languages/css'
import diff from 'highlight.js/lib/languages/diff'
import dockerfile from 'highlight.js/lib/languages/dockerfile'
import go from 'highlight.js/lib/languages/go'
import java from 'highlight.js/lib/languages/java'
import javascript from 'highlight.js/lib/languages/javascript'
import json from 'highlight.js/lib/languages/json'
import markdown from 'highlight.js/lib/languages/markdown'
import php from 'highlight.js/lib/languages/php'
import python from 'highlight.js/lib/languages/python'
import ruby from 'highlight.js/lib/languages/ruby'
import rust from 'highlight.js/lib/languages/rust'
import shell from 'highlight.js/lib/languages/shell'
import sql from 'highlight.js/lib/languages/sql'
import typescript from 'highlight.js/lib/languages/typescript'
import xml from 'highlight.js/lib/languages/xml'
import yaml from 'highlight.js/lib/languages/yaml'
import { isSafeImageSrc } from './taskCommentBody.ts'

// Register a curated language set; the full highlight.js bundle is ~1MB.
// Language aliases (js, ts, sh, html, yml, ...) come from each definition.
for (const [name, lang] of Object.entries({
  bash, c, cpp, csharp, css, diff, dockerfile, go, java, javascript, json,
  markdown, php, python, ruby, rust, shell, sql, typescript, xml, yaml,
})) {
  hljs.registerLanguage(name, lang)
}

export type FormatAction = 'bold' | 'italic' | 'underline' | 'ul' | 'ol' | 'link'

export type FormatResult = {
  text: string
  selectionStart: number
  selectionEnd: number
}

function escapeHtml(str: string): string {
  return str
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;')
}

// Reverses escapeHtml. renderMarkdown pre-escapes tag-like input before parsing,
// so code block text must be decoded once before it is escaped or highlighted.
function unescapeHtml(str: string): string {
  return str
    .replace(/&lt;/g, '<')
    .replace(/&gt;/g, '>')
    .replace(/&quot;/g, '"')
    .replace(/&#39;/g, "'")
    .replace(/&amp;/g, '&')
}

/**
 * Configure marked instance with custom extensions for mentions, task references,
 * safe links, safe images, and syntax-highlighted code blocks.
 */
function createMarked(taskTitle?: (id: number) => string | undefined) {
  const marked = new Marked()

  const mentionExtension: TokenizerAndRendererExtension = {
    name: 'mention',
    level: 'inline',
    start(src: string) {
      const match = src.match(/(?:^|[^A-Za-z0-9_@])@/)
      return match ? match.index! + (match[0].length - 1) : -1
    },
    tokenizer(src: string) {
      const rule = /^@([A-Za-z0-9_]{3,32})\b/
      const match = rule.exec(src)
      if (match) {
        return {
          type: 'mention',
          raw: match[0],
          username: match[1],
        }
      }
    },
    renderer(token) {
      const username = String(token.username || '')
      return `<span class="rich-body-mention">@${escapeHtml(username)}</span>`
    },
  }

  const taskRefExtension: TokenizerAndRendererExtension = {
    name: 'taskRef',
    level: 'inline',
    start(src: string) {
      const match = src.match(/\[\[\d+\]\]|#\d+\b/)
      return match ? match.index : -1
    },
    tokenizer(src: string) {
      const bracketMatch = /^\[\[(\d+)\]\]/.exec(src)
      if (bracketMatch) {
        return {
          type: 'taskRef',
          raw: bracketMatch[0],
          id: Number(bracketMatch[1]),
        }
      }
      const hashMatch = /^#(\d+)\b/.exec(src)
      if (hashMatch) {
        return {
          type: 'taskRef',
          raw: hashMatch[0],
          id: Number(hashMatch[1]),
        }
      }
    },
    renderer(token) {
      const id = Number(token.id)
      const title = taskTitle?.(id)
      const label = title ? `Task #${id} - ${title}` : `Task #${id}`
      return `<button type="button" class="rich-body-task-link" data-task-id="${id}" title="Open task #${id}">${escapeHtml(label)}</button>`
    },
  }

  marked.use({
    gfm: true,
    breaks: true,
    extensions: [mentionExtension, taskRefExtension],
    renderer: {
      link({ href, title, text }) {
        const cleanHref = (href || '').trim()
        const isSafe =
          cleanHref.startsWith('https://') ||
          cleanHref.startsWith('http://') ||
          cleanHref.startsWith('mailto:') ||
          (cleanHref.startsWith('/uploads/') && !cleanHref.includes('..') && !cleanHref.startsWith('//'))

        if (!isSafe) {
          return text
        }

        const titleAttr = title ? ` title="${escapeHtml(title)}"` : ''
        return `<a href="${escapeHtml(cleanHref)}" target="_blank" rel="noopener noreferrer" class="rich-body-link"${titleAttr}>${text}</a>`
      },
      image({ href, title, text }) {
        const src = (href || '').trim()
        if (!isSafeImageSrc(src)) {
          return text ? `[image: ${escapeHtml(text)}]` : '[image]'
        }
        const alt = text ? escapeHtml(text) : 'image'
        const titleAttr = title ? ` title="${escapeHtml(title)}"` : ` title="Open ${alt}"`
        return `<a class="rich-body-image-link" href="${escapeHtml(src)}" target="_blank" rel="noopener noreferrer"${titleAttr}><img class="rich-body-image" src="${escapeHtml(src)}" alt="${alt}" loading="lazy" decoding="async" /></a>`
      },
      code({ text: rawText, lang }) {
        const text = unescapeHtml(rawText)
        // Block code only (fenced/indented); inline code is rendered by codespan.
        // Use the first word of the info string as the language, e.g. ```ts title="x".
        const language = (lang || '').trim().split(/\s+/)[0]
        if (!language) {
          return `<pre><code>${escapeHtml(text)}</code></pre>`
        }
        const langClass = `language-${escapeHtml(language)}`
        if (!hljs.getLanguage(language)) {
          return `<pre><code class="${langClass}">${escapeHtml(text)}</code></pre>`
        }
        // highlight() escapes the raw source itself and only emits <span class="hljs-*">.
        const highlighted = hljs.highlight(text, { language, ignoreIllegals: true }).value
        return `<pre class="hljs"><code class="hljs ${langClass}">${highlighted}</code></pre>`
      },
    },
  })

  return marked
}

const ALLOWED_TAGS = [
  'p',
  'br',
  'strong',
  'b',
  'em',
  'i',
  'u',
  'ins',
  's',
  'del',
  'strike',
  'ul',
  'ol',
  'li',
  'a',
  'img',
  'span',
  'button',
  'code',
  'pre',
  'blockquote',
  'h1',
  'h2',
  'h3',
  'h4',
  'h5',
  'h6',
  'hr',
]

const ALLOWED_ATTR = [
  'href',
  'target',
  'rel',
  'src',
  'alt',
  'title',
  'class',
  'data-task-id',
  'type',
  'loading',
  'decoding',
]

/**
 * Render Markdown string to sanitized HTML.
 */
export function renderMarkdown(
  body: string,
  options?: {
    taskTitle?: (id: number) => string | undefined
  },
): string {
  if (!body || !body.trim()) return ''

  // Pre-process: escape angle brackets except <u>, </u>, <ins>, </ins> to preserve underline tags
  const sanitizedInput = body.replace(/<(\/?)([a-zA-Z0-9]+)([^>]*)>/g, (match, slash, tag) => {
    const lower = tag.toLowerCase()
    if (lower === 'u' || lower === 'ins') {
      return `<${slash}${lower}>`
    }
    return escapeHtml(match)
  })

  const markedInstance = createMarked(options?.taskTitle)
  const rawHtml = markedInstance.parse(sanitizedInput) as string

  // Sanitize with DOMPurify if in browser environment
  if (typeof window !== 'undefined' && DOMPurify?.sanitize) {
    return DOMPurify.sanitize(rawHtml, {
      ALLOWED_TAGS,
      ALLOWED_ATTR,
    })
  }

  return rawHtml
}

/**
 * Apply a formatting action (bold, italic, underline, ul, ol, link) to the selected text range in a textarea.
 */
export function applyFormat(
  text: string,
  start: number,
  end: number,
  format: FormatAction,
  extra?: { url?: string; title?: string },
): FormatResult {
  const selected = text.slice(start, end)
  const before = text.slice(0, start)
  const after = text.slice(end)

  switch (format) {
    case 'bold': {
      // Check if already wrapped
      if (
        start >= 2 &&
        text.slice(start - 2, start) === '**' &&
        text.slice(end, end + 2) === '**'
      ) {
        return {
          text: text.slice(0, start - 2) + selected + text.slice(end + 2),
          selectionStart: start - 2,
          selectionEnd: end - 2,
        }
      }
      if (selected.startsWith('**') && selected.endsWith('**') && selected.length >= 4) {
        const inner = selected.slice(2, -2)
        return {
          text: before + inner + after,
          selectionStart: start,
          selectionEnd: start + inner.length,
        }
      }
      const val = selected || 'bold text'
      const formatted = `**${val}**`
      return {
        text: before + formatted + after,
        selectionStart: selected ? start : start + 2,
        selectionEnd: selected ? start + formatted.length : start + 2 + val.length,
      }
    }

    case 'italic': {
      if (
        start >= 1 &&
        text.slice(start - 1, start) === '*' &&
        text.slice(end, end + 1) === '*' &&
        text.slice(start - 2, start) !== '**'
      ) {
        return {
          text: text.slice(0, start - 1) + selected + text.slice(end + 1),
          selectionStart: start - 1,
          selectionEnd: end - 1,
        }
      }
      if (selected.startsWith('*') && selected.endsWith('*') && selected.length >= 2 && !selected.startsWith('**')) {
        const inner = selected.slice(1, -1)
        return {
          text: before + inner + after,
          selectionStart: start,
          selectionEnd: start + inner.length,
        }
      }
      const val = selected || 'italic text'
      const formatted = `*${val}*`
      return {
        text: before + formatted + after,
        selectionStart: selected ? start : start + 1,
        selectionEnd: selected ? start + formatted.length : start + 1 + val.length,
      }
    }

    case 'underline': {
      if (
        start >= 3 &&
        text.slice(start - 3, start) === '<u>' &&
        text.slice(end, end + 4) === '</u>'
      ) {
        return {
          text: text.slice(0, start - 3) + selected + text.slice(end + 4),
          selectionStart: start - 3,
          selectionEnd: end - 3,
        }
      }
      if (selected.startsWith('<u>') && selected.endsWith('</u>') && selected.length >= 7) {
        const inner = selected.slice(3, -4)
        return {
          text: before + inner + after,
          selectionStart: start,
          selectionEnd: start + inner.length,
        }
      }
      const val = selected || 'underlined text'
      const formatted = `<u>${val}</u>`
      return {
        text: before + formatted + after,
        selectionStart: selected ? start : start + 3,
        selectionEnd: selected ? start + formatted.length : start + 3 + val.length,
      }
    }

    case 'ul': {
      // Multi-line or single-line list toggle
      const block = text.slice(start, end) || ''
      if (!block) {
        const lead = start > 0 && !before.endsWith('\n') ? '\n' : ''
        const insert = `${lead}- List item`
        return {
          text: before + insert + after,
          selectionStart: before.length + lead.length + 2,
          selectionEnd: before.length + insert.length,
        }
      }
      const lines = block.split('\n')
      const allBulleted = lines.every((l) => l.trimStart().startsWith('- ') || l.trimStart().startsWith('* '))
      const toggled = lines
        .map((l) => (allBulleted ? l.replace(/^(\s*)[-*]\s+/, '$1') : `- ${l}`))
        .join('\n')
      return {
        text: before + toggled + after,
        selectionStart: start,
        selectionEnd: start + toggled.length,
      }
    }

    case 'ol': {
      const block = text.slice(start, end) || ''
      if (!block) {
        const lead = start > 0 && !before.endsWith('\n') ? '\n' : ''
        const insert = `${lead}1. List item`
        return {
          text: before + insert + after,
          selectionStart: before.length + lead.length + 3,
          selectionEnd: before.length + insert.length,
        }
      }
      const lines = block.split('\n')
      const allNumbered = lines.every((l) => /^\s*\d+\.\s+/.test(l))
      const toggled = lines
        .map((l, i) => (allNumbered ? l.replace(/^(\s*)\d+\.\s+/, '$1') : `${i + 1}. ${l}`))
        .join('\n')
      return {
        text: before + toggled + after,
        selectionStart: start,
        selectionEnd: start + toggled.length,
      }
    }

    case 'link': {
      const title = extra?.title || selected || 'link text'
      const url = extra?.url || 'https://example.com'
      const formatted = `[${title}](${url})`
      return {
        text: before + formatted + after,
        selectionStart: start + 1,
        selectionEnd: start + 1 + title.length,
      }
    }
  }
}

export type ListEnterResult = {
  text: string
  cursor: number
}

function escapeRegex(str: string): string {
  return str.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

function renumberSubsequentOrderedList(
  rest: string,
  indent: string,
  nextNum: number,
): string {
  if (!rest || !rest.startsWith('\n')) return rest

  const lines = rest.slice(1).split(/\r?\n/)
  const escapedIndent = escapeRegex(indent)
  const olRegex = new RegExp(`^(${escapedIndent})(\\d+)\\.\\s+(.*)$`)

  let expectedNum = nextNum

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i]
    const match = line.match(olRegex)
    if (!match) {
      break
    }
    const currentItemNum = parseInt(match[2], 10)
    if (currentItemNum >= expectedNum) {
      lines[i] = `${match[1]}${currentItemNum + 1}. ${match[3]}`
      expectedNum = currentItemNum + 1
    } else {
      break
    }
  }

  return '\n' + lines.join('\n')
}

/**
 * Handle pressing Enter inside an unordered or ordered list item.
 * - If on an empty list item (e.g. "- " or "2. "), exits the list by clearing the marker.
 *   (If indented >= 2 spaces, unindents by 2 spaces first).
 * - If on an unordered list item (e.g. "- item"), inserts a newline with the same bullet and indent.
 *   Supports checkboxes: "- [ ] " or "- [x] " continues with "- [ ] ".
 * - If on an ordered list item (e.g. "1. item"), inserts a newline with incremented number (e.g. "2. ")
 *   and renumbers subsequent ordered list items at the same indentation level.
 * - Returns null if the cursor is not inside a list item, letting default Enter behavior take over.
 */
export function handleListEnter(
  text: string,
  selectionStart: number,
  selectionEnd: number,
): ListEnterResult | null {
  const hadSelection = selectionStart !== selectionEnd
  // Collapse selection first as Enter would replace the selection
  const collapsedText = text.slice(0, selectionStart) + text.slice(selectionEnd)
  const cursor = selectionStart

  // Identify current line bounds around cursor
  const lastNewline = collapsedText.lastIndexOf('\n', cursor - 1)
  const lineStart = lastNewline === -1 ? 0 : lastNewline + 1
  const nextNewline = collapsedText.indexOf('\n', cursor)
  const lineEnd = nextNewline === -1 ? collapsedText.length : nextNewline

  const rawLineBeforeCursor = collapsedText.slice(lineStart, cursor)
  const rawLineAfterCursor = collapsedText.slice(cursor, lineEnd)
  const lineBeforeCursor = rawLineBeforeCursor.replace(/\r$/, '')
  const lineAfterCursor = rawLineAfterCursor.replace(/\r$/, '')
  const currentLine = lineBeforeCursor + lineAfterCursor

  // 1. Check for empty list item (user pressed Enter on a line with only list marker) -> exit list
  if (!hadSelection) {
    const emptyUlMatch = currentLine.match(/^(\s*)([-*+])(?:\s+\[[ xX]?\])?\s*$/)
    if (emptyUlMatch && lineBeforeCursor.trim().length > 0 && lineAfterCursor.trim().length === 0) {
      const indent = emptyUlMatch[1]
      const bullet = emptyUlMatch[2]
      if (indent.length >= 2) {
        const newIndent = indent.slice(2)
        const replacement = `${newIndent}${bullet} `
        const newText = collapsedText.slice(0, lineStart) + replacement + collapsedText.slice(lineEnd)
        return {
          text: newText,
          cursor: lineStart + replacement.length,
        }
      }
      // Clear list marker, leaving a blank line
      const newText = collapsedText.slice(0, lineStart) + collapsedText.slice(lineEnd)
      return {
        text: newText,
        cursor: lineStart,
      }
    }

    const emptyOlMatch = currentLine.match(/^(\s*)\d+\.\s*$/)
    if (emptyOlMatch && lineBeforeCursor.trim().length > 0 && lineAfterCursor.trim().length === 0) {
      const indent = emptyOlMatch[1]
      if (indent.length >= 2) {
        const newIndent = indent.slice(2)
        const replacement = `${newIndent}1. `
        const newText = collapsedText.slice(0, lineStart) + replacement + collapsedText.slice(lineEnd)
        return {
          text: newText,
          cursor: lineStart + replacement.length,
        }
      }
      // Clear list marker, leaving a blank line
      const newText = collapsedText.slice(0, lineStart) + collapsedText.slice(lineEnd)
      return {
        text: newText,
        cursor: lineStart,
      }
    }
  }

  // 2. Check for unordered list item continuation
  // Matches task checkbox items: "- [ ] item", "* [x] item"
  const ulTaskMatch = lineBeforeCursor.match(/^(\s*)([-*+])\s+\[([ xX])?\]\s*(.*)$/)
  if (ulTaskMatch) {
    const indent = ulTaskMatch[1]
    const bullet = ulTaskMatch[2]
    const prefix = `\n${indent}${bullet} [ ] `
    const newText = collapsedText.slice(0, cursor) + prefix + collapsedText.slice(cursor)
    return {
      text: newText,
      cursor: cursor + prefix.length,
    }
  }

  // Matches standard bullet items: "- item", "* item", "+ item"
  const ulMatch = lineBeforeCursor.match(/^(\s*)([-*+])\s+(.*)$/)
  if (ulMatch) {
    const indent = ulMatch[1]
    const bullet = ulMatch[2]
    const prefix = `\n${indent}${bullet} `
    const newText = collapsedText.slice(0, cursor) + prefix + collapsedText.slice(cursor)
    return {
      text: newText,
      cursor: cursor + prefix.length,
    }
  }

  // 3. Check for ordered list item continuation
  const olMatch = lineBeforeCursor.match(/^(\s*)(\d+)\.\s+(.*)$/)
  if (olMatch) {
    const indent = olMatch[1]
    const currentNum = parseInt(olMatch[2], 10)
    const nextNum = currentNum + 1
    const prefix = `\n${indent}${nextNum}. `

    const textBefore = collapsedText.slice(0, cursor) + prefix
    const newCursor = textBefore.length
    const restOfCurrentLine = rawLineAfterCursor
    const afterLineEnd = collapsedText.slice(lineEnd)

    const renumberedRest = renumberSubsequentOrderedList(afterLineEnd, indent, nextNum)
    const newText = textBefore + restOfCurrentLine + renumberedRest

    return {
      text: newText,
      cursor: newCursor,
    }
  }

  return null
}
