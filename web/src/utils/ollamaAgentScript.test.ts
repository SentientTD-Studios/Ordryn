import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import {
  DEFAULT_OLLAMA_SETTINGS,
  agentConfig,
  agentFileName,
  bakeAgentScript,
  runCommand,
  settingsProblems,
  siteUrl,
  type OllamaAgentSettings,
} from './ollamaAgentScript.ts'

const s = (over: Partial<OllamaAgentSettings> = {}): OllamaAgentSettings => ({ ...DEFAULT_OLLAMA_SETTINGS, ...over })
const meta = { handle: 'local_qwen', site: 'https://todo.example.com', generatedAt: new Date('2026-10-31T12:00:00Z') }

// Shaped like the bundle: the slot appears once, as the bundler prints it.
const SOURCE = [
  '#!/usr/bin/env node',
  'var BAKED_RAW = "__ORDRYN_AGENT_CONFIG__";',
  'var BAKED = BAKED_RAW.startsWith("{") ? JSON.parse(BAKED_RAW) : {};',
  'export { BAKED };',
].join('\n')

/** Runs the baked line the way Node would and returns BAKED. */
function bakedValue(script: string): Record<string, string> {
  const line = script.split('\n').find((l) => l.startsWith('var BAKED_RAW'))!
  const raw = new Function(`${line}; return BAKED_RAW`)() as string
  return JSON.parse(raw)
}

describe('settingsProblems', () => {
  it('accepts the defaults', () => {
    assert.deepEqual(settingsProblems(s()), [])
  })
  it('flags bad values', () => {
    assert.equal(settingsProblems(s({ model: '' })).length, 1)
    assert.equal(settingsProblems(s({ model: 'qwen 3' })).length, 1)
    assert.equal(settingsProblems(s({ numCtx: 1000 })).length, 1)
    assert.equal(settingsProblems(s({ pollSeconds: 1 })).length, 1)
    assert.equal(settingsProblems(s({ ollamaUrl: 'localhost:11434' })).length, 1)
    assert.equal(settingsProblems(s({ ollamaUrl: 'file:///x' })).length, 1)
  })
  it('needs a folder for code access', () => {
    assert.equal(settingsProblems(s({ codeAccess: 'read' })).length, 1)
    assert.deepEqual(settingsProblems(s({ codeAccess: 'read', repoDir: 'C:\\code\\app' })), [])
  })
})

describe('agentConfig', () => {
  it('leaves code access out by default', () => {
    const cfg = agentConfig(s(), 'https://todo.example.com', '')
    assert.equal(cfg.OLLAMA_MODEL, 'qwen3.5:9b')
    assert.equal(cfg.OLLAMA_NUM_CTX, '32768')
    assert.equal('REPO_DIR' in cfg, false)
    assert.equal('REPO_WRITE' in cfg, false)
    assert.equal('GOTODO_AGENT_KEY' in cfg, false)
  })
  it('sets REPO_WRITE=false explicitly for read-only', () => {
    const cfg = agentConfig(s({ codeAccess: 'read', repoDir: ' /home/me/app ' }), 'https://x', 'k')
    assert.equal(cfg.REPO_DIR, '/home/me/app')
    assert.equal(cfg.REPO_WRITE, 'false')
    assert.equal('MAX_FILE_WRITES' in cfg, false)
    assert.equal(cfg.GOTODO_AGENT_KEY, 'k')
  })
  it('cleans protected branches and never leaves them empty', () => {
    const w = (b: string) => agentConfig(s({ codeAccess: 'write', repoDir: '/a', protectedBranches: b }), 'https://x', '')
    assert.equal(w(' main , release,, ').REPO_PROTECTED_BRANCHES, 'main,release')
    assert.equal(w(' , ').REPO_PROTECTED_BRANCHES, 'main,master')
    assert.equal(w('main').REPO_WRITE, 'true')
  })
})

describe('bakeAgentScript', () => {
  it('round-trips the settings, including awkward characters', () => {
    const cfg = { REPO_DIR: 'C:\\Users\\me\\"app"\\$&', GOTODO_AGENT_KEY: "gotodo_abc'def" }
    const out = bakeAgentScript(SOURCE, cfg, meta)
    assert.deepEqual(bakedValue(out), cfg)
  })
  it('keeps the shebang first and adds the header', () => {
    const out = bakeAgentScript(SOURCE, { GOTODO_AGENT_KEY: 'k' }, meta)
    const lines = out.split('\n')
    assert.equal(lines[0], '#!/usr/bin/env node')
    assert.match(lines[1], /@local_qwen on https:\/\/todo\.example\.com/)
    assert.match(out, /contains the agent's key/)
  })
  it('says when no key is built in', () => {
    assert.match(bakeAgentScript(SOURCE, {}, meta), /No key is built in/)
  })
  it('accepts single-quoted slots and refuses sources without one', () => {
    const single = "const BAKED_RAW = '__ORDRYN_AGENT_CONFIG__'"
    assert.match(bakeAgentScript(single, { A: '1' }, meta), /BAKED_RAW = "\{\\"A\\":\\"1\\"\}"/)
    assert.throws(() => bakeAgentScript('console.log(1)', {}, meta))
  })
})

describe('helpers', () => {
  it('builds the site URL with a base path', () => {
    assert.equal(siteUrl('https://x.com/', '/gotodo/'), 'https://x.com/gotodo')
    assert.equal(siteUrl('https://x.com', ''), 'https://x.com')
  })
  it('names the file after the handle', () => {
    assert.equal(agentFileName('Local_Qwen'), 'ordryn-agent-local_qwen.mjs')
    assert.equal(agentFileName('***'), 'ordryn-agent-local.mjs')
  })
  it('only asks for the key when it is not built in', () => {
    assert.equal(runCommand('a.mjs', 'powershell', true), 'node a.mjs')
    assert.match(runCommand('a.mjs', 'powershell', false), /^\$env:GOTODO_AGENT_KEY/)
    assert.match(runCommand('a.mjs', 'bash', false), /^GOTODO_AGENT_KEY="<AGENT_KEY>" node a\.mjs$/)
  })
})
