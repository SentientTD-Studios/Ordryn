import type { AgentEditableField, AgentRunStatus, ProjectAgentInput } from '../api/types.ts'

export const AGENT_FIELDS: { id: AgentEditableField; label: string; help: string }[] = [
  { id: 'status', label: 'Status', help: 'Move cards between columns (limited by the columns below)' },
  { id: 'title', label: 'Title', help: 'Rename tasks' },
  { id: 'description', label: 'Description', help: 'Rewrite or append to the description' },
  { id: 'priority', label: 'Priority', help: 'Change priority' },
  { id: 'due_date', label: 'Due date', help: 'Set or clear due dates' },
  { id: 'tags', label: 'Tags', help: 'Add or remove tags' },
  { id: 'estimate', label: 'Estimate', help: 'Set story points' },
  { id: 'sprint', label: 'Sprint', help: 'Move tasks between sprints' },
  { id: 'custom_fields', label: 'Custom fields', help: 'Edit extension-defined fields' },
]

const HANDLE_RE = /^[A-Za-z0-9_]{3,32}$/

/** Client-side check matching the server's username rules; "" when valid. */
export function handleError(handle: string): string {
  const h = handle.trim()
  if (!h) return 'Pick a handle people can @mention.'
  if (h.length < 3) return 'Handles need at least 3 characters.'
  if (h.length > 32) return 'Handles can be at most 32 characters.'
  if (!HANDLE_RE.test(h)) return 'Use letters, numbers, and underscores only.'
  return ''
}

/** Suggests a handle from a display name ("Release Bot" -> "release_bot"). */
export function suggestHandle(name: string): string {
  const base = name
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '_')
    .replace(/^_+|_+$/g, '')
    .slice(0, 28)
  if (!base) return ''
  return base.length >= 3 ? base : `${base}_ai`
}

export function runStatusBadge(status: AgentRunStatus | string): { label: string; cls: string } {
  switch (status) {
    case 'queued':
      return { label: 'Queued', cls: 'text-bg-secondary' }
    case 'running':
      return { label: 'Running', cls: 'text-bg-primary' }
    case 'succeeded':
      return { label: 'Succeeded', cls: 'text-bg-success' }
    case 'failed':
      return { label: 'Failed', cls: 'text-bg-danger' }
    case 'cancelled':
      return { label: 'Cancelled', cls: 'text-bg-light border' }
    default:
      return { label: status, cls: 'text-bg-secondary' }
  }
}

export function triggerLabel(trigger: string): string {
  switch (trigger) {
    case 'manual':
      return 'Sent manually'
    case 'mention':
      return '@mentioned'
    case 'status':
      return 'Column move'
    default:
      return trigger
  }
}

export function isOpenRun(status: string): boolean {
  return status === 'queued' || status === 'running'
}

/** Absolute API root for snippets, e.g. "https://todo.example.com/api/v2". */
export function apiRoot(origin: string, prefix: string): string {
  return `${origin.replace(/\/+$/, '')}${prefix.replace(/\/+$/, '')}/api/v2`
}

/** One-line command that registers the MCP server in Claude Code. */
export function claudeCodeMcpCommand(root: string, name: string, key = '<AGENT_KEY>'): string {
  return `claude mcp add --transport http ${name} ${root}/mcp --header "Authorization: Bearer ${key}"`
}

/** .mcp.json / generic MCP client config for the agent. */
export function mcpJsonConfig(root: string, name: string, key = '<AGENT_KEY>'): string {
  return JSON.stringify(
    {
      mcpServers: {
        [name]: {
          type: 'http',
          url: `${root}/mcp`,
          headers: { Authorization: `Bearer ${key}` },
        },
      },
    },
    null,
    2,
  )
}

/** MCP server names must be simple; derive one from the agent's handle. */
export function mcpServerName(handle: string): string {
  const n = handle.toLowerCase().replace(/[^a-z0-9_-]/g, '')
  return n ? `gotodo-${n}` : 'gotodo'
}

/** What a starting setup needs from whatever runs the agent. */
// any: any model. read: needs read access to the code. repo: needs to edit the code.
export type AgentRunner = 'any' | 'read' | 'repo'

/** A ready-made starting point for a new agent: instructions plus matching guardrails. */
export type AgentRecipe = {
  id: string
  label: string
  /** One line shown in the picker. */
  summary: string
  /** What good tasks for this agent look like, shown to managers. */
  goodFor: string
  runner: AgentRunner
  instructions: string
  settings: Pick<ProjectAgentInput, 'editable_fields' | 'can_create_tasks' | 'can_complete' | 'claim_on_dispatch'>
  /** Limit status moves to a review-like column when the board has one. */
  reviewOnly: boolean
}

export const AGENT_RECIPES: AgentRecipe[] = [
  {
    id: 'triage',
    label: 'Triage & clarify',
    summary: 'Turns rough requests into clear tasks with acceptance criteria.',
    goodFor: 'One-line or vague tasks. Send it "Fix login" and get back a proper description, a priority, and open questions.',
    runner: 'any',
    instructions: [
      'Rewrite the task description so someone else could pick it up: a short summary, the steps to do, and a',
      '"Done when" list of acceptance criteria. Set a priority (0 none, 1 low, 2 medium, 3 high) that fits.',
      'If details are missing, make reasonable assumptions and say so. Post a comment listing your assumptions',
      'and the questions a person should answer. Then move the card to Review.',
    ].join(' '),
    settings: { editable_fields: ['status', 'description', 'priority'], can_create_tasks: false, can_complete: false, claim_on_dispatch: true },
    reviewOnly: true,
  },
  {
    id: 'breakdown',
    label: 'Break down into subtasks',
    summary: 'Splits a large task into small, ordered subtasks.',
    goodFor: 'Epics and features: "Launch the pricing page" becomes 5–10 concrete subtasks.',
    runner: 'any',
    instructions: [
      'Break the task into 3–10 subtasks that each take less than a day. Create each one as a subtask of this',
      'task with a clear title and a one-paragraph description. Comment with the plan and the order to do them',
      'in, then move the card to Review. Do not create duplicates of subtasks that already exist.',
    ].join(' '),
    settings: { editable_fields: ['status'], can_create_tasks: true, can_complete: false, claim_on_dispatch: true },
    reviewOnly: true,
  },
  {
    id: 'writer',
    label: 'Draft & write',
    summary: 'Writes first drafts: docs, emails, release notes, checklists, plans.',
    goodFor: 'Writing work where the task says what is needed and who it is for.',
    runner: 'any',
    instructions: [
      'Produce the requested writing as a comment, formatted in Markdown and ready to use. Follow any audience,',
      'tone, or length in the task. If something is unclear, write the best draft you can, then list your',
      'assumptions and questions under it. Move the card to Review when the draft is posted.',
    ].join(' '),
    settings: { editable_fields: ['status'], can_create_tasks: false, can_complete: false, claim_on_dispatch: true },
    reviewOnly: true,
  },
  {
    id: 'investigate',
    label: 'Investigate code (read-only)',
    summary: 'Reads the code to explain a bug, answer a question, or review, and proposes a fix.',
    goodFor:
      'Bug reports and "how does X work?" questions, with an agent that can read the code (e.g. the Ollama example with REPO_DIR set).',
    runner: 'read',
    instructions: [
      'Use your code tools to find and read the files that matter before answering. Reply with a comment that has:',
      'what you found, with file:line references; the likely cause or the answer; and, if a change is needed, a',
      'proposed patch in a ```diff block. Never claim you changed code; you can only read it. If you could not find',
      'the relevant code, say where you looked. Then move the card to Review.',
    ].join(' '),
    settings: { editable_fields: ['status'], can_create_tasks: false, can_complete: false, claim_on_dispatch: true },
    reviewOnly: true,
  },
  {
    id: 'code',
    label: 'Code changes',
    summary: 'Makes code changes in the linked repository and reports back.',
    goodFor:
      'Bugs and small features, when the agent runs where the code is checked out (Claude Code over MCP, or the local agent with REPO_WRITE=true).',
    runner: 'repo',
    instructions: [
      'Work in the project\'s repository. Read the task, its discussion, and any linked GitHub issue. Make the',
      'change on a new branch and add or update tests. Run the tests if you can; if you cannot run commands, say',
      'so and list what a reviewer should run or check. Comment with what changed, the branch or pull request',
      'link, and how it was verified, then move the card to Review. Never commit to main. If you cannot reproduce or',
      'the request is ambiguous, comment with what you found and your questions, and finish the run as failed.',
    ].join(' '),
    settings: { editable_fields: ['status'], can_create_tasks: false, can_complete: false, claim_on_dispatch: true },
    reviewOnly: true,
  },
  {
    id: 'blank',
    label: 'Start from scratch',
    summary: 'Default guardrails and no instructions.',
    goodFor: 'Anything else. Write your own instructions after creating it.',
    runner: 'any',
    instructions: '',
    settings: { editable_fields: ['status'], can_create_tasks: false, can_complete: false, claim_on_dispatch: true },
    reviewOnly: false,
  },
]

/** Finds a "Review"-like column to fence status moves into. */
export function reviewStatusId(statuses: { id: number; name: string; is_done: boolean }[]): number | null {
  const open = statuses.filter((s) => !s.is_done)
  const match =
    open.find((s) => /^review$/i.test(s.name.trim())) ||
    open.find((s) => /review|qa|verify|check/i.test(s.name))
  return match ? match.id : null
}

/** Settings to send with the create call for a recipe. */
export function recipeSettings(
  recipe: AgentRecipe,
  statuses: { id: number; name: string; is_done: boolean }[],
): ProjectAgentInput {
  const review = recipe.reviewOnly ? reviewStatusId(statuses) : null
  return {
    instructions: recipe.instructions,
    ...recipe.settings,
    allowed_status_ids: review ? [review] : [],
  }
}
