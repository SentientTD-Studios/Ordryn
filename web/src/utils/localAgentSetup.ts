// Setup snippets for local agent keys: personal keys an AI agent on your own
// machine uses to read (and, if allowed, update) your tasks over MCP or REST.

export type LocalAgentClient = 'claude' | 'codex' | 'gemini' | 'json' | 'rest'

export const LOCAL_AGENT_CLIENTS: { id: LocalAgentClient; label: string }[] = [
  { id: 'claude', label: 'Claude Code' },
  { id: 'codex', label: 'Codex CLI' },
  { id: 'gemini', label: 'Gemini CLI' },
  { id: 'json', label: 'Cursor / other MCP' },
  { id: 'rest', label: 'No MCP (REST)' },
]

export const KEY_PLACEHOLDER = '<YOUR_KEY>'
export const KEY_ENV_VAR = 'GOTODO_API_KEY'

/** MCP server name: "gotodo", or "gotodo-<project>" for a project-limited key. */
export function localServerName(projectName?: string | null): string {
  const slug = (projectName ?? '')
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, 32)
    .replace(/-+$/g, '')
  return slug ? `gotodo-${slug}` : 'gotodo'
}

export type Snippet = { title: string; note?: string; code: string }

/** Copy-paste setup for one client. root is the absolute API root, e.g. https://host/api/v2. */
export function localAgentSnippets(
  client: LocalAgentClient,
  root: string,
  name: string,
  key: string = KEY_PLACEHOLDER,
): Snippet[] {
  const mcpUrl = `${root}/mcp`
  const auth = `Bearer ${key}`
  switch (client) {
    case 'claude':
      return [
        {
          title: 'Run in your repo',
          note: 'Saved privately for this folder. Add --scope user to use it in every folder.',
          code: `claude mcp add --transport http ${name} ${mcpUrl} --header "Authorization: ${auth}"`,
        },
      ]
    case 'codex':
      return [
        {
          title: 'Add to ~/.codex/config.toml',
          code: `[mcp_servers.${tomlKey(name)}]\nurl = "${mcpUrl}"\nbearer_token_env_var = "${KEY_ENV_VAR}"`,
        },
        {
          title: 'Set the key in your environment',
          note: 'Codex reads the key from this variable, so it never sits in the config file.',
          code: `# PowerShell\nsetx ${KEY_ENV_VAR} "${key}"\n# bash / zsh (add to your shell profile)\nexport ${KEY_ENV_VAR}="${key}"`,
        },
      ]
    case 'gemini':
      return [
        {
          title: 'Add to ~/.gemini/settings.json (or .gemini/settings.json in the repo)',
          code: JSON.stringify({ mcpServers: { [name]: { httpUrl: mcpUrl, headers: { Authorization: auth } } } }, null, 2),
        },
      ]
    case 'json':
      return [
        {
          title: 'MCP client config',
          note: 'Cursor: ~/.cursor/mcp.json. Windsurf, Cline, and most other clients take the same shape; some call the URL field "serverUrl" or "httpUrl".',
          code: JSON.stringify({ mcpServers: { [name]: { type: 'http', url: mcpUrl, headers: { Authorization: auth } } } }, null, 2),
        },
      ]
    case 'rest':
      return [
        {
          title: 'Read a task and its discussion',
          note: 'For agents or scripts without MCP. The same key works with the REST API.',
          code: `curl -H "Authorization: ${auth}" ${root}/tasks/123\ncurl -H "Authorization: ${auth}" ${root}/tasks/123/comments`,
        },
      ]
  }
}

/** A note for the repo's AGENTS.md / CLAUDE.md / GEMINI.md so the agent knows where tasks live. */
export function agentsMdSnippet(site: string, name: string, canWrite: boolean): string {
  const lines = [
    '## Tasks',
    '',
    `Tasks for this repo are tracked in GoTodo (${site}), available through the \`${name}\` MCP server.`,
    `When I mention a task as #123 or paste a ${site}/tasks/123 link, read it with \`get_task\``,
    '(include_comments: true) and treat its description and discussion as the requirements.',
  ]
  if (canWrite) {
    lines.push('Only comment on or change the task when I ask.')
  }
  return lines.join('\n')
}

function tomlKey(name: string): string {
  return /^[A-Za-z0-9_-]+$/.test(name) ? name : JSON.stringify(name)
}
