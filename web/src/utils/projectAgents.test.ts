import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import {
  apiRoot,
  claudeCodeMcpCommand,
  handleError,
  isOpenRun,
  mcpJsonConfig,
  mcpServerName,
  runStatusBadge,
  suggestHandle,
  triggerLabel,
  AGENT_RECIPES,
  recipeSettings,
  reviewStatusId,
} from './projectAgents.ts'

describe('handleError', () => {
  it('accepts usernames the server accepts', () => {
    assert.equal(handleError('release_bot'), '')
    assert.equal(handleError('  Bot42 '), '')
  })
  it('rejects empty, short, long, and odd characters', () => {
    assert.match(handleError(''), /handle/)
    assert.match(handleError('ab'), /at least 3/)
    assert.match(handleError('a'.repeat(33)), /at most 32/)
    assert.match(handleError('has-dash'), /letters, numbers/)
    assert.match(handleError('has space'), /letters, numbers/)
  })
})

describe('suggestHandle', () => {
  it('slugs display names', () => {
    assert.equal(suggestHandle('Release Bot'), 'release_bot')
    assert.equal(suggestHandle('  QA — Triage!! '), 'qa_triage')
    assert.equal(suggestHandle('X'), 'x_ai')
    assert.equal(suggestHandle('***'), '')
  })
})

describe('run labels', () => {
  it('maps statuses and triggers', () => {
    assert.equal(runStatusBadge('failed').label, 'Failed')
    assert.equal(runStatusBadge('weird').label, 'weird')
    assert.equal(triggerLabel('mention'), '@mentioned')
    assert.equal(isOpenRun('running'), true)
    assert.equal(isOpenRun('succeeded'), false)
  })
})

describe('MCP snippets', () => {
  const root = apiRoot('https://todo.example.com/', '/gotodo/')
  it('builds the API root with the deploy prefix', () => {
    assert.equal(root, 'https://todo.example.com/gotodo/api/v2')
    assert.equal(apiRoot('http://localhost:8080', ''), 'http://localhost:8080/api/v2')
  })
  it('builds a Claude Code command and JSON config', () => {
    const name = mcpServerName('Release_Bot')
    assert.equal(name, 'gotodo-release_bot')
    assert.equal(
      claudeCodeMcpCommand(root, name, 'gotodo_abc'),
      'claude mcp add --transport http gotodo-release_bot https://todo.example.com/gotodo/api/v2/mcp --header "Authorization: Bearer gotodo_abc"',
    )
    const cfg = JSON.parse(mcpJsonConfig(root, name))
    assert.deepEqual(cfg.mcpServers[name], {
      type: 'http',
      url: 'https://todo.example.com/gotodo/api/v2/mcp',
      headers: { Authorization: 'Bearer <AGENT_KEY>' },
    })
  })
})

describe('recipes', () => {
  it('fences moves to a review column when one exists', () => {
    const statuses = [
      { id: 1, name: 'To do', is_done: false },
      { id: 2, name: 'In QA', is_done: false },
      { id: 3, name: 'Done', is_done: true },
    ]
    assert.equal(reviewStatusId(statuses), 2)
    assert.equal(reviewStatusId([{ id: 4, name: 'Review', is_done: false }, ...statuses]), 4)
    assert.equal(reviewStatusId([{ id: 1, name: 'To do', is_done: false }]), null)
    const triage = AGENT_RECIPES.find((r) => r.id === 'triage')!
    const s = recipeSettings(triage, statuses)
    assert.deepEqual(s.allowed_status_ids, [2])
    assert.ok(s.editable_fields?.includes('description'))
    assert.equal(s.can_complete, false)
  })
  it('never lets a recipe complete tasks', () => {
    for (const r of AGENT_RECIPES) assert.equal(r.settings.can_complete, false, r.id)
  })
})
