// Opt-in write access for the example agent (REPO_WRITE=true).
//
// Same sandbox as reading (repo.mjs): only inside the root, no symlink escapes,
// never secrets, .git, or node_modules. On top of that:
//   - the folder must be a git repository, so every change shows up in `git diff`;
//   - nothing is written while a protected branch (main/master) is checked out,
//     so the agent has to create its own branch first;
//   - the only git command the agent can trigger is `git checkout -b <name>`,
//     run without a shell and with the name validated. No commit, no push.

import { execFile } from 'node:child_process'
import { mkdir, readFile, realpath, stat, writeFile } from 'node:fs/promises'
import path from 'node:path'
import { promisify } from 'node:util'
import { SKIP_DIRS, isSecretPath } from './repo.mjs'

const run = promisify(execFile)
const MAX_WRITE_BYTES = 256 * 1024
const BRANCH_NAME = /^(?!-)(?!.*\.\.)(?!.*\/\/)[A-Za-z0-9._/-]{1,100}$/

export class RepoWriter {
  /**
   * @param {import('./repo.mjs').RepoSandbox} sandbox initialized read sandbox
   * @param {{ protectedBranches?: string[], maxWrites?: number }} opts
   */
  constructor(sandbox, opts = {}) {
    this.sandbox = sandbox
    this.root = sandbox.root
    this.protected = (opts.protectedBranches ?? ['main', 'master']).map((b) => b.trim()).filter(Boolean)
    this.maxWrites = opts.maxWrites ?? 40
    this.writes = 0
    /** Files this run actually wrote (from our own log, not the model's word). */
    this.touched = new Set()
  }

  async gitRaw(...args) {
    const { stdout } = await run('git', args, { cwd: this.root, windowsHide: true, timeout: 15_000 })
    return stdout
  }

  async git(...args) {
    return (await this.gitRaw(...args)).trim()
  }

  /** Fails unless the root is the top of a git work tree. */
  async init() {
    let top
    try {
      top = await this.git('rev-parse', '--show-toplevel')
    } catch {
      throw new Error(`REPO_WRITE needs REPO_DIR to be a git repository: ${this.root}`)
    }
    if ((await realpath(path.resolve(top))) !== this.root) {
      throw new Error(`REPO_DIR must be the top of the git repository (${top}) when REPO_WRITE is on`)
    }
    return this
  }

  async currentBranch() {
    return this.git('rev-parse', '--abbrev-ref', 'HEAD')
  }

  /** Starts a run's write budget over. */
  resetBudget() {
    this.writes = 0
    this.touched = new Set()
  }

  /** Files changed in the work tree, from git itself (not from the model). */
  async changedFiles() {
    // Untrimmed: porcelain lines start with a status column that may be a space.
    const out = await this.gitRaw('status', '--porcelain', '--untracked-files=all')
    return out
      .split(/\r?\n/)
      .filter(Boolean)
      .map((l) => l.slice(3).trim())
  }

  async createBranch(name) {
    const branch = String(name || '').trim()
    if (!BRANCH_NAME.test(branch) || branch.endsWith('.lock') || branch.endsWith('/') || branch.endsWith('.')) {
      throw new Error(`invalid branch name: ${branch}`)
    }
    if (this.protected.includes(branch)) throw new Error(`refusing to use protected branch ${branch}`)
    await this.git('check-ref-format', '--branch', branch)
    await this.git('checkout', '-b', branch)
    return `created and switched to branch ${branch}`
  }

  /** Resolves a repo-relative path for writing (the file may not exist yet). */
  async resolveForWrite(rel) {
    const cleaned = String(rel || '').replace(/^[\\/]+/, '')
    if (!cleaned) throw new Error('path is required')
    const target = path.resolve(this.root, cleaned)
    const inside = (p) => p === this.root || p.startsWith(this.root + path.sep)
    if (!inside(target) || target === this.root) throw new Error(`outside the allowed folder: ${cleaned}`)
    const relPath = path.relative(this.root, target)
    if (relPath.split(path.sep).some((part) => SKIP_DIRS.has(part))) throw new Error(`not writable: ${cleaned}`)
    if (isSecretPath(relPath)) throw new Error(`refused (looks like a secret): ${cleaned}`)

    // Follow symlinks on the nearest existing ancestor (and the file itself) so a
    // link inside the repo can't redirect a write outside it.
    let probe = target
    for (;;) {
      try {
        const real = await realpath(probe)
        if (!inside(real)) throw new Error(`outside the allowed folder: ${cleaned}`)
        if (probe === target && !(await stat(real)).isFile()) throw new Error(`not a file: ${cleaned}`)
        break
      } catch (err) {
        if (err.code !== 'ENOENT') throw err
        probe = path.dirname(probe)
      }
    }
    return target
  }

  async assertCanWrite() {
    const branch = await this.currentBranch()
    if (this.protected.includes(branch) || branch === 'HEAD') {
      throw new Error(`on ${branch === 'HEAD' ? 'a detached HEAD' : `protected branch ${branch}`}; call create_branch first`)
    }
    if (this.writes >= this.maxWrites) throw new Error(`write limit reached (${this.maxWrites} per run)`)
  }

  async writeFile(rel, content) {
    await this.assertCanWrite()
    const text = String(content ?? '')
    if (Buffer.byteLength(text) > MAX_WRITE_BYTES) throw new Error(`content too large (max ${MAX_WRITE_BYTES} bytes)`)
    const abs = await this.resolveForWrite(rel)
    let existed = true
    try {
      await stat(abs)
    } catch {
      existed = false
    }
    await mkdir(path.dirname(abs), { recursive: true })
    await writeFile(abs, text, 'utf8')
    this.writes++
    this.touched.add(this.sandbox.relative(abs))
    return `${existed ? 'overwrote' : 'created'} ${this.sandbox.relative(abs)} (${text.split(/\r?\n/).length} lines)`
  }

  async editFile(rel, oldText, newText) {
    await this.assertCanWrite()
    const abs = await this.resolveForWrite(rel)
    let current
    try {
      current = await readFile(abs, 'utf8')
    } catch {
      throw new Error(`not found: ${rel} (use write_file to create a new file)`)
    }
    let find = String(oldText ?? '')
    let replace = String(newText ?? '')
    if (!find) throw new Error('old_text is required')
    // Models write \n; keep the file's own line endings.
    if (!current.includes(find) && current.includes('\r\n')) {
      find = find.replace(/\r?\n/g, '\r\n')
      replace = replace.replace(/\r?\n/g, '\r\n')
    }
    const count = current.split(find).length - 1
    if (count > 1) throw new Error(`old_text appears ${count} times in ${rel}; include more surrounding lines so it is unique`)
    let next
    if (count === 1) {
      next = current.replace(find, () => replace)
    } else {
      // Models often re-indent copied code (spaces vs tabs, e.g. in Go). Retry
      // ignoring indentation and trailing spaces, and only if that is unique.
      next = editIgnoringIndent(current, find, replace)
      if (next === null) throw new Error(`old_text not found in ${rel}; read the file again and copy the exact lines`)
      if (next === undefined) {
        throw new Error(`old_text matches several places in ${rel} when ignoring indentation; include more surrounding lines`)
      }
    }
    if (Buffer.byteLength(next) > MAX_WRITE_BYTES) throw new Error(`result too large (max ${MAX_WRITE_BYTES} bytes)`)
    await writeFile(abs, next, 'utf8')
    this.writes++
    this.touched.add(this.sandbox.relative(abs))
    return `edited ${this.sandbox.relative(abs)}`
  }
}

export const WRITE_TOOLS = [
  {
    type: 'function',
    function: {
      name: 'create_branch',
      description:
        'Create a new git branch from the current one and switch to it (git checkout -b). Required before any edit ' +
        'when main/master is checked out. Use names like agent/task-123-short-summary.',
      parameters: { type: 'object', properties: { name: { type: 'string' } }, required: ['name'] },
    },
  },
  {
    type: 'function',
    function: {
      name: 'edit_file',
      description:
        'Replace one exact snippet in a file. old_text must match the file exactly (copy it from read_file without ' +
        'the line-number prefixes) and appear only once; differences in indentation (tabs vs spaces) are tolerated. ' +
        'Prefer this over write_file for existing files. Works for any source file (.go, .vue, .js, .ts, .css, …).',
      parameters: {
        type: 'object',
        properties: { path: { type: 'string' }, old_text: { type: 'string' }, new_text: { type: 'string' } },
        required: ['path', 'old_text', 'new_text'],
      },
    },
  },
  {
    type: 'function',
    function: {
      name: 'write_file',
      description: 'Create a new file, or replace a whole file, with the given content.',
      parameters: {
        type: 'object',
        properties: { path: { type: 'string' }, content: { type: 'string' } },
        required: ['path', 'content'],
      },
    },
  },
]

/** Runs one write tool call; errors come back as text so the model can recover. */
export async function runWriteTool(writer, name, args = {}) {
  try {
    switch (name) {
      case 'create_branch':
        return await writer.createBranch(args.name)
      case 'edit_file':
        return await writer.editFile(args.path, args.old_text, args.new_text)
      case 'write_file':
        return await writer.writeFile(args.path, args.content)
      default:
        return null // not a write tool
    }
  } catch (err) {
    return `error: ${err.message || err}`
  }
}

/**
 * Finds oldText in content comparing lines without leading/trailing whitespace.
 * Returns the edited content, null if there is no match, or undefined if the
 * match is ambiguous. Replacement lines are re-indented to the file's style.
 */
export function editIgnoringIndent(content, oldText, newText) {
  const eol = content.includes('\r\n') ? '\r\n' : '\n'
  const fileLines = content.split(/\r?\n/)
  const want = oldText.replace(/\r\n/g, '\n').replace(/^\n+|\n+$/g, '').split('\n')
  if (!want.length || want.every((l) => !l.trim())) return null
  const norm = (l) => l.trim()
  const starts = []
  for (let i = 0; i + want.length <= fileLines.length; i++) {
    let ok = true
    for (let j = 0; j < want.length; j++) {
      if (norm(fileLines[i + j]) !== norm(want[j])) {
        ok = false
        break
      }
    }
    if (ok) starts.push(i)
  }
  if (starts.length === 0) return null
  if (starts.length > 1) return undefined
  const at = starts[0]

  // Map the model's indentation onto the file's: same unit (tab or N spaces)
  // and the same base level as the first matched line.
  const lead = (l) => l.match(/^[ \t]*/)[0]
  const fileIndent = lead(fileLines[at])
  const useTabs = fileLines.slice(at, at + want.length).some((l) => lead(l).includes('\t'))
  const modelLead = want.map(lead).filter((s, i) => want[i].trim())
  const modelBase = Math.min(...modelLead.map((s) => s.replace(/\t/g, '    ').length))
  const steps = modelLead.map((s) => s.replace(/\t/g, '    ').length - modelBase).filter((n) => n > 0)
  const unit = steps.length ? Math.min(...steps) : 4
  const reindent = (line) => {
    if (!line.trim()) return ''
    const depth = Math.max(0, Math.round((lead(line).replace(/\t/g, '    ').length - modelBase) / unit))
    const extra = useTabs ? '\t'.repeat(depth) : ' '.repeat(depth * (fileIndent.includes('\t') ? 4 : unit))
    return fileIndent + extra + line.trimStart()
  }
  const replacement = newText.replace(/\r\n/g, '\n').replace(/^\n+|\n+$/g, '').split('\n').map(reindent)
  const out = [...fileLines.slice(0, at), ...replacement, ...fileLines.slice(at + want.length)]
  return out.join(eol)
}
