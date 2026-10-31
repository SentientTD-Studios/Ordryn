// Turns the model's answer into task updates the agent's guardrails allow.
//
// The answer schema only offers fields this runner can apply AND the guardrails
// allow, so the model can't promise a change that would be silently dropped.

/** Guardrail fields (GET /api/v2/agent → guardrails.editable_fields) this runner can change. */
export const RUNNER_FIELDS = ['status', 'description', 'title', 'priority', 'tags']

/** Fields the model may change: allowed by the guardrails and supported here. */
export function usableFields(editable = []) {
  return RUNNER_FIELDS.filter((f) => editable.includes(f))
}

/** JSON schema for the model's answer (Ollama structured outputs). */
export function answerSchema(editable = []) {
  const can = new Set(usableFields(editable))
  const properties = {
    outcome: { type: 'string', enum: ['succeeded', 'failed'] },
    comment: { type: 'string', description: 'Markdown comment to post on the task (your work or your questions).' },
    summary: { type: 'string', description: 'One sentence for the run log.' },
  }
  if (can.has('status')) {
    properties.move_to_status_id = { type: ['integer', 'null'], description: 'Status id to move the task to, or null.' }
  }
  if (can.has('description')) {
    properties.new_description = { type: ['string', 'null'], description: 'Full replacement description, or null to leave it.' }
  }
  if (can.has('title')) {
    properties.new_title = { type: ['string', 'null'], description: 'New title, or null to leave it.' }
  }
  if (can.has('priority')) {
    properties.new_priority = {
      type: ['integer', 'null'],
      description: 'New priority: 0 none, 1 low, 2 medium, 3 high. Null to leave it.',
    }
  }
  if (can.has('tags')) {
    const tags = { type: 'array', items: { type: 'string' } }
    properties.add_tags = { ...tags, description: 'Names of existing project tags to add. Empty for none.' }
    properties.remove_tags = { ...tags, description: 'Names of tags to remove from the task. Empty for none.' }
  }
  return { type: 'object', properties, required: Object.keys(properties) }
}

const norm = (s) => String(s ?? '').trim().replace(/^#/, '').toLowerCase()

/**
 * Works out the task's new tag_ids. tag_ids replaces the whole list, so this starts
 * from the task's current tags. Protected tags are left out: the server keeps them
 * and refuses them in tag_ids.
 * @returns {{ tagIds: number[] | null, added: string[], removed: string[], problems: string[] }}
 */
export function tagChange(answer, taskTags = [], projectTags = []) {
  const problems = []
  const current = taskTags.filter((t) => !t.protected)
  const byName = new Map()
  for (const t of [...projectTags, ...current]) byName.set(norm(t.name), t)

  const next = new Map(current.map((t) => [t.id, t]))
  const removed = []
  for (const name of answer.remove_tags ?? []) {
    const hit = current.find((t) => norm(t.name) === norm(name))
    if (hit && next.delete(hit.id)) removed.push(hit.name)
  }
  const added = []
  for (const name of answer.add_tags ?? []) {
    if (!norm(name)) continue
    const tag = byName.get(norm(name))
    if (!tag) {
      problems.push(`no tag named "${String(name).trim()}" in this project (agents can't create tags)`)
      continue
    }
    if (!next.has(tag.id)) {
      next.set(tag.id, tag)
      added.push(tag.name)
    }
  }
  const tagIds = [...next.keys()]
  const same = tagIds.length === current.length && current.every((t) => next.has(t.id))
  return { tagIds: same ? null : tagIds, added, removed, problems }
}

/**
 * The PATCH bodies to send, one per field so a refusal of one doesn't block the
 * others. Status goes last so the card moves only after its content changed.
 * @returns {{ patches: { label: string, body: object }[], problems: string[] }}
 */
export function planChanges(answer, task, ctx) {
  const can = new Set(usableFields(ctx?.guardrails?.editable_fields))
  const patches = []
  const problems = []

  if (can.has('title') && typeof answer.new_title === 'string') {
    const title = answer.new_title.trim()
    if (title && title !== task.title) patches.push({ label: 'title', body: { title } })
  }
  if (can.has('description') && typeof answer.new_description === 'string' && answer.new_description.trim()) {
    if (answer.new_description !== task.description) {
      patches.push({ label: 'description', body: { description: answer.new_description } })
    }
  }
  if (can.has('priority') && Number.isInteger(answer.new_priority) && answer.new_priority !== task.priority) {
    if (answer.new_priority >= 0 && answer.new_priority <= 3) {
      patches.push({ label: 'priority', body: { priority: answer.new_priority } })
    } else {
      problems.push(`priority ${answer.new_priority} is not 0–3`)
    }
  }
  if (can.has('tags')) {
    const t = tagChange(answer, task.tags, ctx?.tags)
    problems.push(...t.problems)
    if (t.tagIds) {
      const what = [...t.added.map((n) => `+${n}`), ...t.removed.map((n) => `-${n}`)].join(' ')
      patches.push({ label: `tags (${what})`, body: { tag_ids: t.tagIds } })
    }
  }
  if (can.has('status') && Number.isInteger(answer.move_to_status_id) && answer.move_to_status_id !== task.status_id) {
    patches.push({ label: 'status', body: { status_id: answer.move_to_status_id } })
  }
  return { patches, problems }
}
