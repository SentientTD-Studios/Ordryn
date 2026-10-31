// Keeps the conversation inside the model's context window (num_ctx).
//
// When a conversation outgrows num_ctx, Ollama silently drops the oldest
// messages, which can include the task itself ("no user query found"). Keep
// file contents within a budget, trimming the oldest first, so the system
// prompt and task always fit. File contents show up in two places: tool
// results (read_file, search_code) and the model's own tool calls (write_file
// content, edit_file old_text/new_text).

export const TOOL_RESULT_MAX_CHARS = 12_000
export const STUB = '[earlier result trimmed to save context; read it again if you still need it]'
// Replaces long tool-call arguments; short ones (path, name) are kept so the history still reads.
export const CALL_STUB = '[trimmed to save context]'
const CALL_ARG_KEEP_CHARS = 200
// Rough size of a token before Ollama has counted any (code is ~3.3, prose ~4).
export const CHARS_PER_TOKEN = 3.5

export function capToolResult(text) {
  if (text.length <= TOOL_RESULT_MAX_CHARS) return text
  return text.slice(0, TOOL_RESULT_MAX_CHARS) + '\n… (cut to save context; use start_line/end_line to read more)'
}

function argChars(args) {
  if (args == null) return 0
  return typeof args === 'string' ? args.length : JSON.stringify(args).length
}

/** Characters in the parts of a message we may trim: tool results and tool-call arguments. */
function trimmableChars(m) {
  if (m.role === 'tool') return m.content.length
  return (m.tool_calls || []).reduce((n, c) => n + argChars(c.function?.arguments), 0)
}

/** Characters in the whole conversation, for estimating its size in tokens. */
export function contextChars(messages) {
  return messages.reduce(
    (n, m) => n + (m.content?.length ?? 0) + (m.role === 'tool' ? 0 : trimmableChars(m)),
    0,
  )
}

function stubArgs(args) {
  if (typeof args === 'string') {
    try {
      args = JSON.parse(args)
    } catch {
      return args.length > CALL_ARG_KEEP_CHARS ? CALL_STUB : args
    }
  }
  return Object.fromEntries(
    Object.entries(args ?? {}).map(([k, v]) => [
      k,
      typeof v === 'string' && v.length > CALL_ARG_KEEP_CHARS ? CALL_STUB : v,
    ]),
  )
}

/** Trims the oldest tool results and tool-call arguments until their total is under budget chars. */
export function trimContext(messages, budget) {
  let total = messages.reduce((n, m) => n + trimmableChars(m), 0)
  for (const m of messages) {
    if (total <= budget) return
    const before = trimmableChars(m)
    if (m.role === 'tool') {
      if (m.content !== STUB) m.content = STUB
    } else {
      for (const call of m.tool_calls || []) {
        if (call.function) call.function.arguments = stubArgs(call.function.arguments)
      }
    }
    total -= before - trimmableChars(m)
  }
}

/**
 * Estimated prompt tokens for the next call: the last real count from Ollama
 * (prompt_eval_count, which includes cached tokens) plus the change in
 * characters since that prompt was sent. Before the first call, it's chars alone.
 */
export function estimateTokens(messages, last = { tokens: 0, chars: 0 }) {
  return Math.max(0, Math.round(last.tokens + (contextChars(messages) - last.chars) / CHARS_PER_TOKEN))
}
