// Read-only, sandboxed access to one local folder for the example agent.
//
// Every path is resolved (following symlinks) and must stay inside the root.
// Secrets and bulky folders are never listed or read. Nothing here writes.

import { readdir, readFile, realpath, stat } from 'node:fs/promises'
import path from 'node:path'

export const SKIP_DIRS = new Set(['.git', 'node_modules', 'dist', 'build', 'vendor', '.venv', '__pycache__', '.next', 'coverage'])
const SECRET_FILE = /(^|[\\/])(\.env(\..*)?|.*\.(pem|key|p12|pfx|jks|keystore)|id_(rsa|ed25519|ecdsa|dsa)(\.pub)?|\.npmrc|\.pypirc|\.netrc|credentials(\.json)?|secrets?\.(json|ya?ml|toml))$/i
const MAX_READ_BYTES = 64 * 1024
const DEFAULT_READ_LINES = 300
const MAX_LIST = 400
const MAX_SEARCH_FILE_BYTES = 512 * 1024
const MAX_SEARCH_HITS = 60

export function isSecretPath(p) {
  return SECRET_FILE.test(p.replace(/\\/g, '/'))
}

export class RepoSandbox {
  /** @param {string} root absolute or relative folder the agent may read */
  constructor(root) {
    this.rootInput = root
    this.root = ''
  }

  async init() {
    this.root = await realpath(path.resolve(this.rootInput))
    const s = await stat(this.root)
    if (!s.isDirectory()) throw new Error(`REPO_DIR is not a folder: ${this.root}`)
    return this
  }

  /** Resolves a repo-relative path, refusing anything outside the root or secret. */
  async resolve(rel) {
    const cleaned = String(rel || '.').replace(/^[\\/]+/, '')
    const joined = path.resolve(this.root, cleaned)
    let real
    try {
      real = await realpath(joined)
    } catch {
      throw new Error(`not found: ${cleaned}`)
    }
    const inside = real === this.root || real.startsWith(this.root + path.sep)
    if (!inside) throw new Error(`outside the allowed folder: ${cleaned}`)
    const relReal = path.relative(this.root, real)
    if (relReal.split(path.sep).some((part) => SKIP_DIRS.has(part))) throw new Error(`not readable: ${cleaned}`)
    if (isSecretPath(relReal)) throw new Error(`refused (looks like a secret): ${cleaned}`)
    return real
  }

  relative(abs) {
    return path.relative(this.root, abs).split(path.sep).join('/') || '.'
  }

  /** Lists files under dir (recursive), skipping bulky folders and secrets. */
  async listFiles(dir = '.') {
    const start = await this.resolve(dir)
    const out = []
    const walk = async (d) => {
      if (out.length >= MAX_LIST) return
      let entries
      try {
        entries = await readdir(d, { withFileTypes: true })
      } catch {
        return
      }
      entries.sort((a, b) => a.name.localeCompare(b.name))
      for (const e of entries) {
        if (out.length >= MAX_LIST) return
        if (e.isSymbolicLink()) continue
        const abs = path.join(d, e.name)
        if (e.isDirectory()) {
          if (!SKIP_DIRS.has(e.name)) await walk(abs)
        } else if (e.isFile() && !isSecretPath(this.relative(abs))) {
          out.push(this.relative(abs))
        }
      }
    }
    await walk(start)
    return out.length >= MAX_LIST ? [...out, `… (stopped at ${MAX_LIST} files; list a subfolder)`] : out
  }

  /** Reads a text file, optionally a 1-based inclusive line range. */
  async readFile(rel, startLine, endLine) {
    const abs = await this.resolve(rel)
    const s = await stat(abs)
    if (!s.isFile()) throw new Error(`not a file: ${rel}`)
    const buf = await readFile(abs)
    if (buf.includes(0)) throw new Error(`binary file: ${rel}`)
    const lines = buf.toString('utf8').split(/\r?\n/)
    const from = Math.max(1, Number(startLine) || 1)
    // Without an explicit end, read one chunk so big files don't flood the model's context.
    const to = Math.min(lines.length, Number(endLine) || from + DEFAULT_READ_LINES - 1)
    let text = lines
      .slice(from - 1, to)
      .map((l, i) => `${from + i}: ${l}`)
      .join('\n')
    if (text.length > MAX_READ_BYTES) {
      text = text.slice(0, MAX_READ_BYTES) + `\n… (truncated; read a smaller line range)`
    }
    const more = to < lines.length ? `\n… (${lines.length - to} more lines; read start_line=${to + 1} to continue)` : ''
    return `${this.relative(abs)} (lines ${from}-${to} of ${lines.length})\n${text}${more}`
  }

  /** Case-insensitive search across files: a regular expression, or plain text if it is not valid regex. */
  async search(query, dir = '.') {
    const raw = String(query || '')
    if (raw.length < 2) throw new Error('search text must be at least 2 characters')
    let matcher
    try {
      const re = new RegExp(raw, 'i')
      matcher = (line) => re.test(line)
    } catch {
      const needle = raw.toLowerCase()
      matcher = (line) => line.toLowerCase().includes(needle)
    }
    const files = (await this.listFiles(dir)).filter((f) => !f.startsWith('…'))
    const hits = []
    for (const f of files) {
      if (hits.length >= MAX_SEARCH_HITS) break
      let buf
      try {
        const abs = await this.resolve(f)
        if ((await stat(abs)).size > MAX_SEARCH_FILE_BYTES) continue
        buf = await readFile(abs)
      } catch {
        continue
      }
      if (buf.includes(0)) continue
      const lines = buf.toString('utf8').split(/\r?\n/)
      for (let i = 0; i < lines.length && hits.length < MAX_SEARCH_HITS; i++) {
        if (matcher(lines[i])) hits.push(`${f}:${i + 1}: ${lines[i].trim().slice(0, 200)}`)
      }
    }
    return hits.length ? hits.join('\n') : `no matches for "${query}"`
  }
}

/** Tool definitions handed to the model (Ollama / OpenAI function format). */
export const REPO_TOOLS = [
  {
    type: 'function',
    function: {
      name: 'list_files',
      description: 'List files in the project folder (or a subfolder). Start here to find your way around.',
      parameters: { type: 'object', properties: { dir: { type: 'string', description: 'Subfolder, e.g. "internal/server". Default: whole project.' } } },
    },
  },
  {
    type: 'function',
    function: {
      name: 'search_code',
      description: 'Find lines matching a case-insensitive regular expression (or plain text). Returns file:line: text.',
      parameters: {
        type: 'object',
        properties: { query: { type: 'string' }, dir: { type: 'string', description: 'Optional subfolder to search.' } },
        required: ['query'],
      },
    },
  },
  {
    type: 'function',
    function: {
      name: 'read_file',
      description: 'Read a file with line numbers, 300 lines at a time unless you pass end_line. Prefer search_code first, then read only the lines you need.',
      parameters: {
        type: 'object',
        properties: { path: { type: 'string' }, start_line: { type: 'integer' }, end_line: { type: 'integer' } },
        required: ['path'],
      },
    },
  },
]

/** Runs one tool call; errors come back as text so the model can recover. */
export async function runRepoTool(sandbox, name, args = {}) {
  try {
    switch (name) {
      case 'list_files':
        return (await sandbox.listFiles(args.dir || '.')).join('\n') || '(empty)'
      case 'search_code':
        return await sandbox.search(args.query, args.dir || '.')
      case 'read_file':
        return await sandbox.readFile(args.path, args.start_line, args.end_line)
      default:
        return `unknown tool: ${name}`
    }
  } catch (err) {
    return `error: ${err.message || err}`
  }
}
