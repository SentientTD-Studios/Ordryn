#!/usr/bin/env node
// Example GoTodo / Ordryn AI agent backed by a local Ollama model.
//
// It polls the agent's queue, asks the model what to do with each task, and
// applies the answer through the REST API with the agent's own key. GoTodo's
// guardrails still decide what is allowed; refusals are reported in the run
// summary instead of crashing the loop.
//
// Node 18+ (built-in fetch), no dependencies. See README.md for setup.
// Optional: REPO_DIR gives the model read-only access to one local folder (repo.mjs);
// REPO_WRITE=true also lets it edit files on its own git branch (repo-write.mjs).

import { existsSync, readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { answerSchema, planChanges, usableFields } from './changes.mjs'
import { capToolResult, contextChars, estimateTokens, trimContext } from './context.mjs'
import { REPO_TOOLS, RepoSandbox, runRepoTool } from './repo.mjs'
import { RepoWriter, WRITE_TOOLS, runWriteTool } from './repo-write.mjs'

// Settings baked in by the script generator in GoTodo (project settings → AI agents →
// Run a local model). The web build bundles this file and replaces the placeholder
// string with JSON; run from source, it stays empty. Order: environment variables,
// then the .env file, then these, then the defaults below.
const BAKED_RAW = '__ORDRYN_AGENT_CONFIG__'
const BAKED = BAKED_RAW.startsWith('{') ? JSON.parse(BAKED_RAW) : {}

// Load the .env next to this script, so `--env-file` is optional and the agent
// works the same from any folder. Real environment variables win, unless they
// are blank (a stray `$env:X = " "` would otherwise hide the file's value).
const ENV_FILE = fileURLToPath(new URL('./.env', import.meta.url))
const fromFile = new Set()
if (existsSync(ENV_FILE)) {
  for (const raw of readFileSync(ENV_FILE, 'utf8').replace(/^﻿/, '').split(/\r?\n/)) {
    const m = raw.match(/^\s*(?:export\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.*)\s*$/)
    if (!m) continue
    let value = m[2].trim()
    if (/^(['"]).*\1$/.test(value)) value = value.slice(1, -1)
    if ((process.env[m[1]] ?? '').trim() === '') {
      process.env[m[1]] = value
      fromFile.add(m[1])
    }
  }
}

const env = (name, fallback) => {
  const set = (process.env[name] ?? '').trim()
  if (set !== '') return set
  return String(BAKED[name] ?? fallback ?? '').trim()
}

const GOTODO_URL = env('GOTODO_URL', 'http://localhost:8080').replace(/\/+$/, '')
const AGENT_KEY = env('GOTODO_AGENT_KEY')
const OLLAMA_URL = env('OLLAMA_URL', 'http://localhost:11434').replace(/\/+$/, '')
const MODEL = env('OLLAMA_MODEL', 'qwen3.5:9b')
const POLL_SECONDS = Math.max(2, Number(env('POLL_SECONDS', '10')) || 10)
const THINK = env('OLLAMA_THINK', 'false') === 'true'
const ONCE = process.argv.includes('--once')
const DRY_RUN = process.argv.includes('--dry-run') || env('DRY_RUN') === 'true'
const REPO_DIR = env('REPO_DIR')
const MAX_TOOL_STEPS = Math.max(1, Number(env('MAX_TOOL_STEPS', '12')) || 12)
// Context window to request from Ollama (tokens). Bigger fits more code but needs more memory.
const NUM_CTX = Math.max(4096, Number(env('OLLAMA_NUM_CTX', '32768')) || 32768)
// File contents kept in the conversation, in characters (~3.5 per token): about half the window.
const TOOL_BUDGET_CHARS = Math.floor(NUM_CTX * 1.75)

if (!AGENT_KEY) {
  console.error('GOTODO_AGENT_KEY is not set.')
  console.error(
    existsSync(ENV_FILE)
      ? `  ${ENV_FILE} was read but has no non-empty GOTODO_AGENT_KEY=… line.`
      : '  Set it in your shell, or put GOTODO_AGENT_KEY=… in a .env file next to this script.',
  )
  console.error('  Get a key from project settings → AI agents → your agent → Connect the agent → Create key.')
  console.error('  (The script generator there can put the key in the file for you.)')
  process.exit(1)
}

const API = `${GOTODO_URL}/api/v2`

/** Fails early, with the fix, when Ollama isn't running or the model isn't installed. */
async function checkOllama() {
  let tags
  try {
    const res = await fetch(`${OLLAMA_URL}/api/tags`)
    if (!res.ok) throw new Error(`HTTP ${res.status}`)
    tags = await res.json()
  } catch (err) {
    console.error(`Cannot reach Ollama at ${OLLAMA_URL} (${err.message || err}).`)
    console.error('  Start Ollama (install it from https://ollama.com), or set OLLAMA_URL.')
    process.exit(1)
  }
  const names = new Set((tags.models || []).flatMap((m) => [m.name, m.model]).filter(Boolean))
  const wanted = MODEL.includes(':') ? [MODEL] : [MODEL, `${MODEL}:latest`]
  if (!wanted.some((n) => names.has(n))) {
    console.error(`Model "${MODEL}" is not installed in Ollama. Run:\n\n  ollama pull ${MODEL}\n`)
    process.exit(1)
  }
}
await checkOllama()

// Read-only code access, only when REPO_DIR is set.
const repo = REPO_DIR ? await new RepoSandbox(REPO_DIR).init() : null

// Opt-in file editing (REPO_WRITE=true): edits only on a non-protected git branch, never commits or pushes.
const REPO_WRITE = env('REPO_WRITE') === 'true'
if (REPO_WRITE && !repo) {
  console.error('REPO_WRITE=true needs REPO_DIR set to a git checkout.')
  process.exit(1)
}
const writer =
  REPO_WRITE && !DRY_RUN
    ? await new RepoWriter(repo, {
      protectedBranches: env('REPO_PROTECTED_BRANCHES', 'main,master').split(','),
      maxWrites: Math.max(1, Number(env('MAX_FILE_WRITES', '40')) || 40),
    }).init()
    : null

/** "Fix login redirect!" -> "fix-login-redirect", for branch names. */
function slug(text) {
  return (
    String(text || '')
      .toLowerCase()
      .replace(/[^a-z0-9]+/g, '-')
      .replace(/^-+|-+$/g, '')
      .slice(0, 40) || 'change'
  )
}

function log(...args) {
  console.log(new Date().toISOString().slice(11, 19), ...args)
}

/** Calls the GoTodo API as the agent. Returns { ok, status, data, error }. */
async function api(method, path, body) {
  let res
  try {
    res = await fetch(API + path, {
      method,
      headers: {
        Authorization: `Bearer ${AGENT_KEY}`,
        ...(body === undefined ? {} : { 'Content-Type': 'application/json' }),
      },
      body: body === undefined ? undefined : JSON.stringify(body),
    })
  } catch (err) {
    return { ok: false, status: 0, data: null, error: `cannot reach ${GOTODO_URL} (${err.cause?.code || err.message || err})` }
  }
  const text = await res.text()
  let data = null
  try {
    data = text ? JSON.parse(text) : null
  } catch {
    data = text
  }
  const error = res.ok ? '' : `${res.status} ${data?.error ?? ''}: ${data?.message ?? text}`.trim()
  return { ok: res.ok, status: res.status, data, error }
}

function systemPrompt(ctx) {
  const g = ctx.guardrails || {}
  const allowed = (g.allowed_statuses || []).map((s) => `${s.id} (${s.name})`).join(', ')
  const statuses = (ctx.statuses || []).map((s) => `- ${s.id}: ${s.name}${s.is_done ? ' (done)' : ''}`).join('\n')
  const fields = usableFields(g.editable_fields)
  const unsupported = (g.editable_fields || []).filter((f) => !fields.includes(f))
  const tags = (ctx.tags || []).map((t) => t.name).join(', ')
  return [
    `You are ${ctx.name} (@${ctx.handle}), an AI agent working on tasks in the project "${ctx.project?.name ?? ''}".`,
    repo
      ? 'You cannot run code, open links, or change files, but you can read the project code with your tools.'
      : 'You cannot run code, open links, or see files. Work only from the task, its discussion, and your instructions.',
    ctx.project?.github_repo && !repo
      ? `The project's code lives in ${ctx.project.github_repo.full_name}, but you cannot read it.`
      : null,
    'Reply with JSON matching the schema. Put your actual work (plan, answer, draft, checklist, review) in "comment".',
    'If details are missing, do not give up: make reasonable assumptions, say what they are, deliver the best',
    'version of the work you can, and end the comment with the questions that would let you do better.',
    'Set outcome to "failed" only when there is nothing useful you can produce at all.',
    '',
    'Standing instructions from the project managers:',
    ctx.instructions || '(none)',
    '',
    'Board statuses:',
    statuses || '(this project has no board statuses)',
    '',
    'Your guardrails (changes outside these are refused):',
    `- fields you may change: ${fields.join(', ') || 'none (you can only comment)'}`,
    unsupported.length ? `- you cannot change these here, so don't offer to: ${unsupported.join(', ')}` : null,
    `- statuses you may move tasks to: ${allowed || (fields.includes('status') ? 'any non-done status' : 'none')}`,
    `- may complete tasks: ${g.can_complete ? 'yes' : 'no'}`,
    fields.includes('tags') ? `- project tags you can add: ${tags || '(none; you cannot create tags)'}` : null,
    '',
    'Changes happen only through the answer fields (use null or an empty list to leave something as it is).',
    'Never write in your comment that you changed, added, or removed something unless you set the matching field.',
  ]
    .filter((line) => line !== null)
    .join('\n')
}

function taskPrompt(run, task, comments) {
  const thread = comments
    .filter((c) => !c.deleted)
    .map((c) => `[${c.created_at}] ${c.user_name || `user ${c.user_id}`}: ${c.body}`)
    .join('\n\n')
  return [
    `Run #${run.id} — triggered by ${run.trigger}${run.triggered_by ? ` (${run.triggered_by})` : ''}.`,
    run.note ? `Note from the person who sent it:\n${run.note}` : '',
    '',
    `Task #${task.id}: ${task.title}`,
    `Status: ${task.status_name || 'n/a'} (id ${task.status_id ?? 'n/a'}) · Priority: ${task.priority} · Due: ${task.due_date || 'none'}`,
    `Tags: ${(task.tags || []).map((t) => t.name).join(', ') || 'none'}`,
    '',
    'Description:',
    task.description || '(empty)',
    '',
    'Discussion:',
    thread || '(no comments)',
  ]
    .filter((line) => line !== null)
    .join('\n')
}

async function chat(body) {
  const res = await fetch(`${OLLAMA_URL}/api/chat`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      model: MODEL,
      stream: false,
      think: THINK,
      options: { temperature: 0.2, num_ctx: NUM_CTX },
      ...body,
    }),
  })
  if (!res.ok) throw new Error(`Ollama ${res.status}: ${await res.text()}`)
  const data = await res.json()
  return { message: data.message ?? {}, promptTokens: data.prompt_eval_count ?? 0 }
}

// Stop looking things up (and trim harder before answering) past this many prompt tokens.
const CONTEXT_FULL_TOKENS = Math.floor(NUM_CTX * 0.85)

async function askModel(ctx, run, task, comments) {
  const messages = [
    { role: 'system', content: systemPrompt(ctx) },
    { role: 'user', content: taskPrompt(run, task, comments) },
  ]

  // With REPO_DIR set, let the model look around the code first (read-only).
  if (repo) {
    messages[0].content +=
      '\n\nYou can read the project\'s code with the list_files, search_code, and read_file tools ' +
      `(folder: ${repo.root}). Look before you answer: find the relevant files, read them, and cite file paths ` +
      'and line numbers.'
    if (writer) {
      const branch = await writer.currentBranch().catch(() => 'unknown')
      messages[0].content +=
        `\n\nYou can also change files with create_branch, edit_file, and write_file. The current git branch is ` +
        `"${branch}". Edits are refused on ${writer.protected.join('/')}. If the task or note asks for a new branch, ` +
        `or you are on a protected branch, call create_branch first with a name like ` +
        `"agent/task-${task.id}-${slug(task.title)}". Only change files when the task asks for a change; for ` +
        'questions, just answer. Read a file before editing it and copy old_text exactly. Keep changes small and ' +
        'focused. You cannot run commands, tests, commits, or pushes; a person will review and commit. In your ' +
        'comment, explain what you changed and why, and what a reviewer should check or test.'
    } else {
      messages[0].content +=
        ' You cannot edit files; put any proposed change in your comment as a unified diff in a ```diff block.'
    }
    messages[0].content += ' When you have what you need, stop calling tools and say you are done.'
    // The last real prompt size from Ollama, to estimate the next one (tool results included) before sending it.
    let last = { tokens: 0, chars: 0 }
    for (let step = 0; step < MAX_TOOL_STEPS; step++) {
      trimContext(messages, TOOL_BUDGET_CHARS)
      const estimate = estimateTokens(messages, last)
      if (estimate > CONTEXT_FULL_TOKENS) {
        log(`  context nearly full (~${estimate}/${NUM_CTX} tokens); answering now`)
        break
      }
      const sentChars = contextChars(messages)
      let reply
      try {
        reply = await chat({ messages, tools: writer ? [...REPO_TOOLS, ...WRITE_TOOLS] : REPO_TOOLS })
      } catch (err) {
        // Don't lose the run over a failed look-up step; answer with what we have.
        log(`  look-up step failed, answering with what was found: ${err.message || err}`)
        break
      }
      const msg = reply.message
      last = { tokens: reply.promptTokens, chars: sentChars }
      log(`  step ${step + 1}: ~${reply.promptTokens} of ${NUM_CTX} context tokens used`)
      messages.push(msg)
      const calls = msg.tool_calls || []
      if (!calls.length) break
      if (step === MAX_TOOL_STEPS - 1) log(`  reached MAX_TOOL_STEPS (${MAX_TOOL_STEPS}); answering now`)
      for (const call of calls) {
        const name = call.function?.name
        let args = call.function?.arguments ?? {}
        if (typeof args === 'string') {
          try {
            args = JSON.parse(args)
          } catch {
            args = {}
          }
        }
        log(`  tool ${name} ${JSON.stringify(args)}`)
        const written = writer ? await runWriteTool(writer, name, args) : null
        if (written !== null) log(`    -> ${written}`)
        const result = capToolResult(written ?? (await runRepoTool(repo, name, args)))
        messages.push({ role: 'tool', tool_name: name, content: result })
      }
    }
    // A model that stopped mid-call leaves tool calls without a final reply; that's fine for the template.
    messages.push({
      role: 'user',
      content: 'Now give your final answer as JSON matching the schema, based on what you found in the code.',
    })
  }

  trimContext(messages, TOOL_BUDGET_CHARS)
  if (estimateTokens(messages) > CONTEXT_FULL_TOKENS) trimContext(messages, Math.floor(TOOL_BUDGET_CHARS / 4))
  try {
    const { message } = await chat({ messages, format: answerSchema(ctx.guardrails?.editable_fields) })
    return JSON.parse(message.content ?? '{}')
  } catch (err) {
    if (!repo) throw err
    // Last resort: keep only the task and the most recent findings.
    log(`  final answer failed (${err.message || err}); retrying with less context`)
    trimContext(messages, Math.floor(TOOL_BUDGET_CHARS / 4))
    const { message } = await chat({ messages, format: answerSchema(ctx.guardrails?.editable_fields) })
    return JSON.parse(message.content ?? '{}')
  }
}

async function work(ctx, run) {
  log(`run #${run.id}: task #${run.task_id} "${run.task_title}" (${run.trigger})`)
  if (DRY_RUN) {
    log('  dry run: not starting')
  } else {
    const started = await api('POST', `/agent/runs/${run.id}/start`)
    if (!started.ok) {
      log(`  could not start: ${started.error}`)
      return
    }
  }

  const problems = []
  let outcome = 'failed'
  let summary = ''
  writer?.resetBudget()
  try {
    const task = await api('GET', `/tasks/${run.task_id}`)
    if (!task.ok) throw new Error(`read task: ${task.error}`)
    const comments = await api('GET', `/tasks/${run.task_id}/comments`)
    const answer = await askModel(ctx, run, task.data, comments.ok && Array.isArray(comments.data) ? comments.data : [])
    log(`  model says: ${answer.outcome} — ${answer.summary}`)
    if (answer.comment) log(`  comment:\n${answer.comment.replace(/^/gm, '    | ')}`)
    outcome = answer.outcome === 'succeeded' ? 'succeeded' : 'failed'
    summary = answer.summary || ''

    const plan = planChanges(answer, task.data, ctx)
    if (DRY_RUN) {
      console.log(JSON.stringify(answer, null, 2))
      log(`  would change: ${plan.patches.map((p) => p.label).join(', ') || 'nothing'}`)
      if (plan.problems.length) log(`  would report: ${plan.problems.join('; ')}`)
      return
    }

    // Apply field changes before commenting, so the comment can say what didn't happen.
    const notDone = [...plan.problems]
    for (const p of plan.patches) {
      const r = await api('PATCH', `/tasks/${run.task_id}`, p.body)
      if (r.ok) log(`  changed ${p.label}`)
      else notDone.push(`${p.label}: ${r.error}`)
    }
    problems.push(...notDone)

    // Report file changes from our own write log and git, never from the model's claims.
    let commentBody = answer.comment?.trim() || ''
    if (notDone.length) {
      commentBody += `\n\n---\n**Not applied:**\n${notDone.map((p) => `- ${p}`).join('\n')}`
    }
    if (writer && writer.touched.size) {
      const branch = await writer.currentBranch().catch(() => 'unknown')
      const files = [...writer.touched].sort()
      log(`  changed on ${branch}: ${files.join(', ')}`)
      commentBody +=
        `\n\n---\n**Changes (uncommitted)** on branch \`${branch}\`:\n` +
        files.map((f) => `- \`${f}\``).join('\n') +
        `\n\nReview with \`git diff\` on that branch, then commit or discard.`
      summary = `${summary} [branch ${branch}: ${files.length} file(s) changed, uncommitted]`.trim()
    } else if (writer) {
      summary = `${summary} [no files changed]`.trim()
    }
    if (commentBody) {
      const r = await api('POST', `/tasks/${run.task_id}/comments`, { body: commentBody })
      if (!r.ok) problems.push(`comment refused (${r.error})`)
    }
  } catch (err) {
    problems.push(String(err.message || err))
    outcome = 'failed'
  }

  if (DRY_RUN) return
  if (problems.length) log(`  problems: ${problems.join('; ')}`)
  const fullSummary = [summary, problems.length ? `Problems: ${problems.join('; ')}` : '']
    .filter(Boolean)
    .join(' ')
    .slice(0, 4000)
  const done = await api('POST', `/agent/runs/${run.id}/finish`, { status: outcome, summary: fullSummary })
  log(done.ok ? `  finished: ${outcome}` : `  could not finish: ${done.error}`)
}

async function tick() {
  const ctx = await api('GET', '/agent')
  if (!ctx.ok) {
    log(`cannot load agent context: ${ctx.error}`)
    return
  }
  const queue = await api('GET', '/agent/runs?status=queued')
  if (!queue.ok) {
    log(`cannot read queue: ${queue.error}`)
    return
  }
  // Oldest first, one at a time.
  for (const run of [...queue.data].reverse()) {
    await work(ctx.data, run)
  }
}

log(
  `agent loop: ${GOTODO_URL} · model ${MODEL}${THINK ? ' (thinking)' : ''}${DRY_RUN ? ' · dry run' : ''}` +
  (writer
    ? ` · code: ${repo.root} (read + edit on branches; never ${writer.protected.join('/')}, no commits)`
    : repo
      ? ` · read-only code: ${repo.root}`
      : ' · no code access (set REPO_DIR)'),
)
{
  // Names only, never values: shows when a shell variable overrides the .env file.
  const KNOWN = ['GOTODO_URL', 'GOTODO_AGENT_KEY', 'OLLAMA_URL', 'OLLAMA_MODEL', 'OLLAMA_NUM_CTX', 'OLLAMA_THINK',
    'POLL_SECONDS', 'REPO_DIR', 'REPO_WRITE', 'REPO_PROTECTED_BRANCHES', 'MAX_TOOL_STEPS', 'MAX_FILE_WRITES']
  const set = KNOWN.filter((k) => (process.env[k] ?? '').trim() !== '')
  const shell = set.filter((k) => !fromFile.has(k))
  const baked = KNOWN.filter((k) => !set.includes(k) && String(BAKED[k] ?? '').trim() !== '')
  log(`settings from ${existsSync(ENV_FILE) ? '.env' : '(no .env)'}: ${set.filter((k) => fromFile.has(k)).join(', ') || 'none'}` +
    (shell.length ? ` · from shell (override .env): ${shell.join(', ')}` : '') +
    (baked.length ? ` · built into this file: ${baked.join(', ')}` : ''))
}
do {
  try {
    await tick()
  } catch (err) {
    log(`error: ${err.message || err}`)
  }
  if (!ONCE) await new Promise((r) => setTimeout(r, POLL_SECONDS * 1000))
} while (!ONCE)
