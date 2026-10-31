// Builds the downloadable local-model agent script (tools/ollama-agent, bundled by
// vite.config.ts) with a person's chosen settings written into it.

export type CodeAccess = 'none' | 'read' | 'write'

export type OllamaAgentSettings = {
  model: string
  /** Context window requested from Ollama, in tokens. */
  numCtx: number
  think: boolean
  pollSeconds: number
  ollamaUrl: string
  codeAccess: CodeAccess
  /** Folder on the person's computer; only used with code access. */
  repoDir: string
  maxToolSteps: number
  maxFileWrites: number
  /** Comma-separated branches the agent never edits. */
  protectedBranches: string
}

export const DEFAULT_OLLAMA_SETTINGS: OllamaAgentSettings = {
  model: 'qwen3.5:9b',
  numCtx: 32768,
  think: false,
  pollSeconds: 10,
  ollamaUrl: 'http://localhost:11434',
  codeAccess: 'none',
  repoDir: '',
  maxToolSteps: 12,
  maxFileWrites: 40,
  protectedBranches: 'main,master',
}

export const MODEL_SUGGESTIONS: { name: string; note: string }[] = [
  { name: 'qwen3.5:9b', note: 'Good start: fast, fits 8 GB GPUs' },
  { name: 'llama3.1:8b', note: 'Small, general writing' },
  { name: 'qwen2.5-coder:14b', note: 'Reading code on 12–16 GB GPUs' },
  { name: 'qwen3-coder:30b', note: 'Code work on 24 GB GPUs' },
  { name: 'qwen3.6:35b', note: 'Best answers, 24 GB+ GPUs' },
]

export const CONTEXT_PRESETS: { tokens: number; label: string; hint: string }[] = [
  { tokens: 8192, label: '8K', hint: 'Short tasks; low memory or CPU only' },
  { tokens: 16384, label: '16K', hint: 'Tasks with long discussions' },
  { tokens: 32768, label: '32K', hint: 'Recommended. Room to read a few files' },
  { tokens: 65536, label: '64K', hint: 'Reading lots of code; 16 GB+ GPU' },
  { tokens: 131072, label: '128K', hint: 'Small models on 24 GB GPUs only' },
]

const MODEL_RE = /^[A-Za-z0-9._\-:/]+$/
// Must match BAKED_RAW in tools/ollama-agent/agent.mjs (either quote style, since the bundler may change it).
const PLACEHOLDER_RE = /(['"])__ORDRYN_AGENT_CONFIG__\1/

/** Problems that would stop the script from working; empty when the settings are usable. */
export function settingsProblems(s: OllamaAgentSettings): string[] {
  const out: string[] = []
  if (!s.model.trim()) out.push('Pick a model.')
  else if (!MODEL_RE.test(s.model.trim())) out.push('Model names look like "qwen3.5:9b" (no spaces).')
  if (!Number.isInteger(s.numCtx) || s.numCtx < 2048 || s.numCtx > 262144) out.push('Context size must be 2048–262144 tokens.')
  if (!Number.isInteger(s.pollSeconds) || s.pollSeconds < 2 || s.pollSeconds > 3600) out.push('Check every 2–3600 seconds.')
  try {
    const u = new URL(s.ollamaUrl.trim())
    if (u.protocol !== 'http:' && u.protocol !== 'https:') out.push('The Ollama address must start with http:// or https://.')
  } catch {
    out.push('Enter the Ollama address, e.g. http://localhost:11434.')
  }
  if (s.codeAccess !== 'none') {
    if (!s.repoDir.trim()) out.push('Enter the folder the agent may use.')
    if (!Number.isInteger(s.maxToolSteps) || s.maxToolSteps < 1 || s.maxToolSteps > 100) out.push('File look-ups must be 1–100.')
  }
  if (s.codeAccess === 'write') {
    if (!Number.isInteger(s.maxFileWrites) || s.maxFileWrites < 1 || s.maxFileWrites > 200) out.push('File edits must be 1–200.')
  }
  return out
}

/** The site address the script talks to: origin plus any base path, no trailing slash. */
export function siteUrl(origin: string, prefix: string): string {
  return `${origin.replace(/\/+$/, '')}${prefix.replace(/\/+$/, '')}`
}

/** Settings as the environment variables the agent reads. */
export function agentConfig(s: OllamaAgentSettings, site: string, key: string): Record<string, string> {
  const cfg: Record<string, string> = {
    GOTODO_URL: site,
    OLLAMA_URL: s.ollamaUrl.trim().replace(/\/+$/, ''),
    OLLAMA_MODEL: s.model.trim(),
    OLLAMA_NUM_CTX: String(s.numCtx),
    OLLAMA_THINK: s.think ? 'true' : 'false',
    POLL_SECONDS: String(s.pollSeconds),
  }
  if (key.trim()) cfg.GOTODO_AGENT_KEY = key.trim()
  if (s.codeAccess !== 'none') {
    cfg.REPO_DIR = s.repoDir.trim()
    cfg.MAX_TOOL_STEPS = String(s.maxToolSteps)
    cfg.REPO_WRITE = s.codeAccess === 'write' ? 'true' : 'false'
  }
  if (s.codeAccess === 'write') {
    cfg.MAX_FILE_WRITES = String(s.maxFileWrites)
    const branches = s.protectedBranches
      .split(',')
      .map((b) => b.trim())
      .filter(Boolean)
    cfg.REPO_PROTECTED_BRANCHES = (branches.length ? branches : ['main', 'master']).join(',')
  }
  return cfg
}

/** Writes the settings into the bundled agent source. Throws if the source has no settings slot. */
export function bakeAgentScript(
  source: string,
  config: Record<string, string>,
  meta: { handle: string; site: string; generatedAt: Date },
): string {
  if (!PLACEHOLDER_RE.test(source)) throw new Error('This agent script has no settings slot; rebuild the web app.')
  // A function replacement, so "$&" and friends in paths or keys are copied literally.
  const literal = JSON.stringify(JSON.stringify(config))
  const baked = source.replace(PLACEHOLDER_RE, () => literal)
  const header = [
    `// Ordryn local AI agent for @${meta.handle} on ${meta.site}`,
    `// Generated ${meta.generatedAt.toISOString().slice(0, 10)} from the agent's settings page. Run: node <this file>`,
    `// Needs Node 18+ and Ollama. Shell variables (e.g. OLLAMA_MODEL) override the settings built in here.`,
    config.GOTODO_AGENT_KEY
      ? `// This file contains the agent's key. Keep it private; revoke the key on the settings page if it leaks.`
      : `// No key is built in: set GOTODO_AGENT_KEY before running.`,
    '',
  ].join('\n')
  // Keep a shebang on the first line.
  if (baked.startsWith('#!')) {
    const nl = baked.indexOf('\n')
    return baked.slice(0, nl + 1) + header + baked.slice(nl + 1)
  }
  return header + baked
}

/** "release_bot" -> "ordryn-agent-release_bot.mjs" */
export function agentFileName(handle: string): string {
  const h = handle.toLowerCase().replace(/[^a-z0-9_-]/g, '')
  return `ordryn-agent-${h || 'local'}.mjs`
}

/** The command to start the script, setting the key in the shell when it isn't built in. */
export function runCommand(fileName: string, shell: 'powershell' | 'bash', keyBuiltIn: boolean): string {
  if (keyBuiltIn) return `node ${fileName}`
  return shell === 'powershell'
    ? `$env:GOTODO_AGENT_KEY = "<AGENT_KEY>"; node ${fileName}`
    : `GOTODO_AGENT_KEY="<AGENT_KEY>" node ${fileName}`
}
