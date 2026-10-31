// node --test tools/ollama-agent/repo.test.mjs
import assert from 'node:assert/strict'
import { mkdtemp, mkdir, rm, symlink, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import path from 'node:path'
import { after, before, describe, it } from 'node:test'
import { RepoSandbox, isSecretPath, runRepoTool } from './repo.mjs'

let base, root, sandbox

before(async () => {
  base = await mkdtemp(path.join(tmpdir(), 'agent-repo-'))
  root = path.join(base, 'project')
  await mkdir(path.join(root, 'src'), { recursive: true })
  await mkdir(path.join(root, 'node_modules', 'x'), { recursive: true })
  await mkdir(path.join(root, '.git'), { recursive: true })
  await writeFile(path.join(root, 'src', 'login.go'), 'package auth\n\nfunc Redirect() string {\n\treturn "/404"\n}\n')
  await writeFile(path.join(root, '.env'), 'DB_PASSWORD=hunter2\n')
  await writeFile(path.join(root, 'server.pem'), 'PEMDATA')
  await writeFile(path.join(root, 'node_modules', 'x', 'index.js'), 'redirect')
  await writeFile(path.join(root, '.git', 'config'), 'redirect')
  await writeFile(path.join(base, 'outside.txt'), 'Redirect outside')
  try {
    await symlink(path.join(base, 'outside.txt'), path.join(root, 'escape.txt'))
  } catch {
    /* symlinks may need privileges on Windows */
  }
  sandbox = await new RepoSandbox(root).init()
})

after(async () => {
  await rm(base, { recursive: true, force: true })
})

describe('RepoSandbox', () => {
  it('lists code but skips secrets, .git, and node_modules', async () => {
    const files = await sandbox.listFiles()
    assert.deepEqual(files, ['src/login.go'])
  })
  it('reads files with line numbers and ranges', async () => {
    const out = await sandbox.readFile('src/login.go', 3, 4)
    assert.match(out, /lines 3-4 of 6/)
    assert.match(out, /4: \treturn "\/404"/)
  })
  it('refuses secrets, traversal, symlink escapes, and skipped folders', async () => {
    for (const p of ['.env', 'server.pem', '../outside.txt', 'escape.txt', 'node_modules/x/index.js', '.git/config']) {
      const out = await runRepoTool(sandbox, 'read_file', { path: p })
      assert.match(out, /^error: /, `${p} should be refused, got: ${out}`)
      assert.doesNotMatch(out, /hunter2|Redirect outside|PEMDATA/)
    }
  })
  it('searches only readable files', async () => {
    const out = await runRepoTool(sandbox, 'search_code', { query: 'redirect' })
    assert.equal(out, 'src/login.go:3: func Redirect() string {')
  })
  it('flags common secret file names', () => {
    for (const p of ['.env', 'config/.env.local', 'id_rsa', 'certs/tls.key', 'secrets.yaml']) assert.ok(isSecretPath(p), p)
    for (const p of ['env.go', 'keyboard.ts', 'src/secret_test_helper.go']) assert.ok(!isSecretPath(p), p)
  })
})

describe('search patterns', () => {
  it('accepts regular expressions and falls back to plain text', async () => {
    assert.equal(await runRepoTool(sandbox, 'search_code', { query: String.raw`func\s+redir` }), 'src/login.go:3: func Redirect() string {')
    assert.match(await runRepoTool(sandbox, 'search_code', { query: '"/404' }), /src\/login\.go:4:/)
    assert.match(await runRepoTool(sandbox, 'search_code', { query: '(unclosed' }), /no matches/)
  })
})

describe('chunked reads', () => {
  it('reads big files one chunk at a time', async () => {
    const big = Array.from({ length: 650 }, (_, i) => `line ${i + 1}`).join('\n')
    await writeFile(path.join(root, 'src', 'big.txt'), big)
    const first = await sandbox.readFile('src/big.txt')
    assert.match(first, /lines 1-300 of 650/)
    assert.match(first, /350 more lines; read start_line=301 to continue/)
    const next = await sandbox.readFile('src/big.txt', 301)
    assert.match(next, /lines 301-600 of 650/)
    const tail = await sandbox.readFile('src/big.txt', 601)
    assert.match(tail, /lines 601-650 of 650/)
    assert.doesNotMatch(tail, /more lines/)
  })
})
