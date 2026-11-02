// node --test tools/ollama-agent/context.test.mjs
import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { CALL_STUB, STUB, capToolResult, contextChars, estimateTokens, trimContext } from './context.mjs'

const big = (n, ch = 'x') => ch.repeat(n)

function conversation() {
  return [
    { role: 'system', content: big(1000, 's') },
    { role: 'user', content: 'Task #1: fix it' },
    { role: 'assistant', content: '', tool_calls: [{ function: { name: 'read_file', arguments: { path: 'a.go' } } }] },
    { role: 'tool', tool_name: 'read_file', content: big(5000, 'a') },
    {
      role: 'assistant',
      content: '',
      tool_calls: [{ function: { name: 'write_file', arguments: { path: 'b.go', content: big(5000, 'b') } } }],
    },
    { role: 'tool', tool_name: 'write_file', content: 'wrote b.go' },
    { role: 'tool', tool_name: 'read_file', content: big(5000, 'c') },
  ]
}

describe('trimContext', () => {
  it('leaves a conversation under budget alone', () => {
    const messages = conversation()
    const before = JSON.stringify(messages)
    trimContext(messages, 1_000_000)
    assert.equal(JSON.stringify(messages), before)
  })

  it('trims the oldest file contents first and never the system prompt or task', () => {
    const messages = conversation()
    trimContext(messages, 11_000)
    assert.equal(messages[0].content, big(1000, 's'))
    assert.equal(messages[1].content, 'Task #1: fix it')
    assert.equal(messages[3].content, STUB)
    assert.equal(messages[4].tool_calls[0].function.arguments.content, big(5000, 'b'))
    assert.equal(messages[6].content, big(5000, 'c'))
  })

  it('trims long write_file arguments but keeps the path', () => {
    const messages = conversation()
    trimContext(messages, 6_000)
    const args = messages[4].tool_calls[0].function.arguments
    assert.equal(args.path, 'b.go')
    assert.equal(args.content, CALL_STUB)
    assert.equal(messages[2].tool_calls[0].function.arguments.path, 'a.go')
    assert.equal(messages[6].content, big(5000, 'c'))
  })

  it('handles arguments sent as a JSON string', () => {
    const messages = [
      {
        role: 'assistant',
        content: '',
        tool_calls: [{ function: { name: 'edit_file', arguments: JSON.stringify({ path: 'x.go', old_text: big(500), new_text: 'y' }) } }],
      },
    ]
    trimContext(messages, 10)
    assert.deepEqual(messages[0].tool_calls[0].function.arguments, { path: 'x.go', old_text: CALL_STUB, new_text: 'y' })
  })
})

describe('estimateTokens', () => {
  it('counts tool-call arguments, not just tool results', () => {
    const messages = conversation()
    assert.ok(contextChars(messages) >= 1000 + 5000 * 3)
  })

  it('adds new tool results to the last real count before sending', () => {
    const messages = conversation()
    const last = { tokens: 4000, chars: contextChars(messages) }
    messages.push({ role: 'tool', tool_name: 'read_file', content: big(3500) })
    assert.equal(estimateTokens(messages, last), 5000)
  })

  it('goes down after trimming', () => {
    const messages = conversation()
    const last = { tokens: 4000, chars: contextChars(messages) }
    trimContext(messages, 0)
    assert.ok(estimateTokens(messages, last) < 4000)
  })
})

describe('capToolResult', () => {
  it('cuts very long results', () => {
    assert.ok(capToolResult(big(20_000)).length < 13_000)
    assert.equal(capToolResult('short'), 'short')
  })
})
