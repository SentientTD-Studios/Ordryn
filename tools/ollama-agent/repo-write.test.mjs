// node --test tools/ollama-agent/repo-write.test.mjs
import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import { mkdtemp, mkdir, readFile, rm, symlink, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import path from 'node:path'
import { after, before, describe, it } from 'node:test'
import { RepoSandbox } from './repo.mjs'
import { RepoWriter, runWriteTool } from './repo-write.mjs'

let base, root, writer
const git = (...args) => execFileSync('git', args, { cwd: root, windowsHide: true }).toString().trim()
const tool = (name, args) => runWriteTool(writer, name, args)

before(async () => {
  base = await mkdtemp(path.join(tmpdir(), 'agent-write-'))
  root = path.join(base, 'repo')
  await mkdir(path.join(root, 'src'), { recursive: true })
  await writeFile(path.join(root, 'src', 'app.js'), 'const greeting = "hi"\nexport default greeting\n')
  await writeFile(path.join(root, 'src', 'win.txt'), 'one\r\ntwo\r\nthree\r\n')
  await writeFile(path.join(root, 'src', 'dup.js'), 'x()\nx()\n')
  await writeFile(path.join(root, '.env'), 'SECRET=1\n')
  await writeFile(path.join(base, 'outside.txt'), 'outside')
  git('init', '-q', '-b', 'main')
  git('-c', 'user.email=t@example.com', '-c', 'user.name=t', 'add', '.')
  git('-c', 'user.email=t@example.com', '-c', 'user.name=t', 'commit', '-qm', 'init')
  const sandbox = await new RepoSandbox(root).init()
  writer = await new RepoWriter(sandbox, { maxWrites: 6 }).init()
})

after(async () => {
  await rm(base, { recursive: true, force: true })
})

describe('RepoWriter', () => {
  it('refuses to write while main is checked out', async () => {
    const out = await tool('write_file', { path: 'src/new.js', content: 'x' })
    assert.match(out, /protected branch main; call create_branch first/)
  })

  it('validates branch names and refuses protected ones', async () => {
    for (const name of ['--help', '-b', 'a..b', 'bad name', 'x.lock', 'main', '']) {
      assert.match(await tool('create_branch', { name }), /^error: /, name)
    }
    assert.equal(git('rev-parse', '--abbrev-ref', 'HEAD'), 'main')
  })

  it('creates a branch and then edits files on it', async () => {
    assert.equal(await tool('create_branch', { name: 'agent/task-1-greeting' }), 'created and switched to branch agent/task-1-greeting')
    assert.equal(git('rev-parse', '--abbrev-ref', 'HEAD'), 'agent/task-1-greeting')

    assert.equal(await tool('edit_file', { path: 'src/app.js', old_text: '"hi"', new_text: '"hello"' }), 'edited src/app.js')
    assert.match(await readFile(path.join(root, 'src', 'app.js'), 'utf8'), /"hello"/)

    assert.match(await tool('write_file', { path: 'src/lib/util.js', content: 'export const x = 1\n' }), /^created src\/lib\/util\.js/)
    assert.deepEqual((await writer.changedFiles()).sort(), ['src/app.js', 'src/lib/util.js'])
  })

  it('keeps CRLF files intact', async () => {
    assert.equal(await tool('edit_file', { path: 'src/win.txt', old_text: 'one\ntwo', new_text: 'one\nTWO' }), 'edited src/win.txt')
    assert.equal(await readFile(path.join(root, 'src', 'win.txt'), 'utf8'), 'one\r\nTWO\r\nthree\r\n')
  })

  it('requires old_text to match exactly once', async () => {
    assert.match(await tool('edit_file', { path: 'src/app.js', old_text: 'nope', new_text: 'x' }), /not found in src\/app\.js/)
    assert.match(await tool('edit_file', { path: 'src/dup.js', old_text: 'x()', new_text: 'y()' }), /appears 2 times/)
  })

  it('never writes secrets, .git, or outside the folder', async () => {
    let escaped = false
    try {
      await symlink(base, path.join(root, 'src', 'link'))
      escaped = true
    } catch {
      /* symlinks may need privileges on Windows */
    }
    const attempts = [
      ['write_file', { path: '.env', content: 'SECRET=2' }],
      ['write_file', { path: 'config/prod.pem', content: 'x' }],
      ['write_file', { path: '.git/hooks/pre-commit', content: 'x' }],
      ['write_file', { path: 'node_modules/x/index.js', content: 'x' }],
      ['write_file', { path: '../outside.txt', content: 'pwned' }],
      ['edit_file', { path: '../outside.txt', old_text: 'outside', new_text: 'pwned' }],
      ...(escaped ? [['write_file', { path: 'src/link/outside.txt', content: 'pwned' }]] : []),
    ]
    for (const [name, args] of attempts) {
      assert.match(await tool(name, args), /^error: /, `${name} ${args.path}`)
    }
    assert.equal(await readFile(path.join(root, '.env'), 'utf8'), 'SECRET=1\n')
    assert.equal(await readFile(path.join(base, 'outside.txt'), 'utf8'), 'outside')
  })

  it('stops at the write budget', async () => {
    writer.resetBudget()
    for (let i = 0; i < 6; i++) await tool('write_file', { path: `src/gen/${i}.txt`, content: String(i) })
    assert.match(await tool('write_file', { path: 'src/gen/7.txt', content: '7' }), /write limit reached/)
  })

  it('refuses a folder that is not the top of a git repo', async () => {
    const sub = await new RepoSandbox(path.join(root, 'src')).init()
    await assert.rejects(new RepoWriter(sub).init(), /top of the git repository/)
  })
})

describe('source file types', () => {
  it('edits and creates .vue, .go, .css, .js, and .ts files', async () => {
    const files = {
      'web/src/components/Badge.vue': '<template>\n  <span class="badge">old</span>\n</template>\n',
      'internal/domain/badge.go': 'package domain\n\nconst Badge = "old"\n',
      'web/src/style.css': '.badge {\n  color: red;\n}\n',
      'web/src/badge.js': 'export const badge = "old"\n',
      'web/src/badge.ts': 'export const badge: string = "old"\n',
    }
    for (const [p, body] of Object.entries(files)) {
      await mkdir(path.dirname(path.join(root, p)), { recursive: true })
      await writeFile(path.join(root, p), body)
    }
    writer.resetBudget()
    for (const p of Object.keys(files)) {
      const old = p.endsWith('.css') ? 'red' : 'old'
      assert.equal(await tool('edit_file', { path: p, old_text: old, new_text: 'new' }), `edited ${p}`)
      assert.match(await readFile(path.join(root, p), 'utf8'), /new/)
    }
    writer.resetBudget()
    for (const p of ['web/src/views/New.vue', 'internal/x/new.go', 'web/src/new.css', 'web/src/new.js']) {
      assert.match(await tool('write_file', { path: p, content: '// new\n' }), new RegExp(`^created ${p.replace(/\./g, '\.')}`))
    }
  })

  it('edits a large single-file component', async () => {
    const lines = Array.from({ length: 2000 }, (_, i) => `  <div class="row-${i}">row ${i}</div>`)
    const p = 'web/src/components/Big.vue'
    await writeFile(path.join(root, p), `<template>\n${lines.join('\n')}\n</template>\n`)
    writer.resetBudget()
    assert.equal(await tool('edit_file', { path: p, old_text: 'row-1500">row 1500', new_text: 'row-1500">changed' }), `edited ${p}`)
    assert.match(await readFile(path.join(root, p), 'utf8'), /row-1500">changed/)
  })
})

describe('indentation-tolerant edits', () => {
  it('edits tab-indented Go when the model used spaces, keeping tabs', async () => {
    const p = 'internal/domain/tabs.go'
    await writeFile(path.join(root, p), 'package domain\n\nfunc Greet(name string) string {\n\tif name == "" {\n\t\treturn "Hi"\n\t}\n\treturn "Hi " + name\n}\n')
    writer.resetBudget()
    const out = await tool('edit_file', {
      path: p,
      old_text: '    if name == "" {\n        return "Hi"\n    }',
      new_text: '    if name == "" {\n        return "Hello"\n    }\n    name = strings.TrimSpace(name)',
    })
    assert.equal(out, `edited ${p}`)
    assert.equal(
      await readFile(path.join(root, p), 'utf8'),
      'package domain\n\nfunc Greet(name string) string {\n\tif name == "" {\n\t\treturn "Hello"\n\t}\n\tname = strings.TrimSpace(name)\n\treturn "Hi " + name\n}\n',
    )
  })

  it('keeps space indentation in Vue/JS files', async () => {
    const p = 'web/src/components/Indent.vue'
    await writeFile(path.join(root, p), '<script setup>\nfunction a() {\n  if (x) {\n    run()\n  }\n}\n</script>\n')
    writer.resetBudget()
    const out = await tool('edit_file', { path: p, old_text: 'if (x) {\n  run()\n}', new_text: 'if (x) {\n  run()\n  done()\n}' })
    assert.equal(out, `edited ${p}`)
    assert.equal(await readFile(path.join(root, p), 'utf8'), '<script setup>\nfunction a() {\n  if (x) {\n    run()\n    done()\n  }\n}\n</script>\n')
  })

  it('still refuses ambiguous or missing snippets', async () => {
    const p = 'internal/domain/twice.go'
    await writeFile(path.join(root, p), 'func a() {\n\tx()\n}\nfunc b() {\n\tx()\n}\n')
    writer.resetBudget()
    assert.match(await tool('edit_file', { path: p, old_text: '  x()', new_text: '  y()' }), /several places/)
    assert.match(await tool('edit_file', { path: p, old_text: '  z()', new_text: '  y()' }), /not found/)
  })
})
