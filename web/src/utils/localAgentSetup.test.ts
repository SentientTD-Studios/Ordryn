import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import {
  agentsMdSnippet,
  localAgentSnippets,
  localServerName,
  KEY_ENV_VAR,
  KEY_PLACEHOLDER,
  LOCAL_AGENT_CLIENTS,
} from './localAgentSetup.ts'

const root = 'https://todo.example.com/gotodo/api/v2'

describe('localServerName', () => {
  it('names the server after a limited project', () => {
    assert.equal(localServerName(), 'gotodo')
    assert.equal(localServerName(''), 'gotodo')
    assert.equal(localServerName('Web App (v2)!'), 'gotodo-web-app-v2')
    assert.equal(localServerName('***'), 'gotodo')
    assert.equal(localServerName('a'.repeat(40)), `gotodo-${'a'.repeat(32)}`)
  })
})

describe('localAgentSnippets', () => {
  it('has setup for every client, with the key filled in', () => {
    for (const c of LOCAL_AGENT_CLIENTS) {
      const snippets = localAgentSnippets(c.id, root, 'gotodo', 'gotodo_abc')
      assert.ok(snippets.length > 0, c.id)
      assert.ok(snippets.some((s) => s.code.includes('gotodo_abc')), `${c.id} should include the key`)
    }
  })

  it('builds the Claude Code command', () => {
    const [s] = localAgentSnippets('claude', root, 'gotodo', 'gotodo_abc')
    assert.equal(
      s.code,
      'claude mcp add --transport http gotodo https://todo.example.com/gotodo/api/v2/mcp --header "Authorization: Bearer gotodo_abc"',
    )
  })

  it('keeps the key out of the Codex config', () => {
    const [cfg, env] = localAgentSnippets('codex', root, 'gotodo-web', 'gotodo_abc')
    assert.match(cfg.code, /^\[mcp_servers\.gotodo-web\]/)
    assert.match(cfg.code, /url = "https:\/\/todo\.example\.com\/gotodo\/api\/v2\/mcp"/)
    assert.match(cfg.code, new RegExp(`bearer_token_env_var = "${KEY_ENV_VAR}"`))
    assert.ok(!cfg.code.includes('gotodo_abc'))
    assert.ok(env.code.includes('gotodo_abc'))
  })

  it('emits valid JSON for JSON-configured clients', () => {
    const gemini = JSON.parse(localAgentSnippets('gemini', root, 'gotodo')[0].code)
    assert.deepEqual(gemini.mcpServers.gotodo, {
      httpUrl: `${root}/mcp`,
      headers: { Authorization: `Bearer ${KEY_PLACEHOLDER}` },
    })
    const generic = JSON.parse(localAgentSnippets('json', root, 'gotodo')[0].code)
    assert.equal(generic.mcpServers.gotodo.url, `${root}/mcp`)
    assert.equal(generic.mcpServers.gotodo.type, 'http')
  })
})

describe('agentsMdSnippet', () => {
  it('tells the agent where tasks live and how to read them', () => {
    const ro = agentsMdSnippet('https://todo.example.com', 'gotodo', false)
    assert.match(ro, /`gotodo` MCP server/)
    assert.match(ro, /https:\/\/todo\.example\.com\/tasks\/123/)
    assert.match(ro, /get_task/)
    assert.ok(!ro.includes('Only comment'))
    assert.match(agentsMdSnippet('https://todo.example.com', 'gotodo', true), /Only comment on or change the task when I ask/)
  })
})
