import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import type { ProjectStatus, Tag } from '../api/types.ts'
import {
  RECIPES,
  availableActions,
  availableTriggers,
  buildRecipe,
  changeSummary,
  describeRule,
  draftError,
  draftPayload,
  emptyDraft,
  missingRecipeTags,
  onTriggerChanged,
  recipesFor,
  tagIDMap,
  type RecipeContext,
  type RuleLookups,
} from './automationRules.ts'

function status(id: number, name: string, extra: Partial<ProjectStatus> = {}): ProjectStatus {
  return { id, project_id: 1, name, position: id, is_done: false, is_default: false, created_at: '', ...extra }
}

const statuses = [
  status(1, 'To Do', { is_default: true }),
  status(2, 'In Progress'),
  status(3, 'Ready'),
  status(4, 'Done', { is_done: true }),
]
const tags: Tag[] = [
  { id: 10, name: 'slipping', color: '#000' },
  { id: 11, name: 'Bug', color: '#000' },
]
const lookups: RuleLookups = { statuses, tags, sprints: [], members: [], agents: [] }
const ctx: RecipeContext = { kanban: true, statuses, tags, agents: [] }

describe('catalogs', () => {
  it('hides kanban-only triggers and actions on classic projects', () => {
    assert.ok(!availableTriggers(false).some((t) => t.id === 'task.claimed'))
    assert.ok(availableTriggers(true).some((t) => t.id === 'task.claimed'))
    assert.ok(!availableActions(false).some((a) => a.id === 'set_status'))
  })
})

describe('describeRule', () => {
  it('reads as one sentence', () => {
    const s = describeRule(
      {
        trigger_type: 'task.claimed',
        trigger_config: {},
        conditions: { status_ids: [1], min_priority: 2 },
        actions: [{ type: 'set_status', status_id: 2 }, { type: 'add_tag', tag_id: 10 }],
      },
      lookups,
    )
    assert.equal(s, 'When a task is claimed, if in To Do, priority Medium or higher, then move to In Progress and add slipping.')
  })
  it('describes timed triggers with their days', () => {
    const s = describeRule(
      { trigger_type: 'time.overdue', trigger_config: { days: 3 }, conditions: {}, actions: [{ type: 'archive' }] },
      lookups,
    )
    assert.equal(s, 'When a task is 3 days overdue, then archive it.')
  })
})

describe('drafts', () => {
  it('requires a name and complete actions', () => {
    const d = emptyDraft(true)
    assert.match(draftError(d), /name/)
    d.name = 'x'
    assert.match(draftError(d), /column/)
    d.actions[0].status_id = 2
    assert.equal(draftError(d), '')
  })
  it('sets default days when switching to a timed trigger', () => {
    const d = emptyDraft(true)
    d.trigger_type = 'time.completed_ago'
    onTriggerChanged(d)
    assert.equal(d.trigger_config.days, 30)
    d.trigger_type = 'task.created'
    onTriggerChanged(d)
    assert.equal(d.trigger_config.days, undefined)
  })
  it('drops empty filters from the payload', () => {
    const d = emptyDraft(false)
    d.name = ' Clean '
    d.conditions = { status_ids: [], min_priority: 0, sprint: '', field_key: '  ', has_due: 'yes' }
    d.actions = [{ type: 'archive' }]
    const p = draftPayload(d)
    assert.equal(p.name, 'Clean')
    assert.deepEqual(p.conditions, { has_due: 'yes' })
    assert.deepEqual(p.trigger_config, {})
  })
})

describe('recipes', () => {
  it('all have unique ids', () => {
    const ids = RECIPES.map((r) => r.id)
    assert.equal(new Set(ids).size, ids.length)
  })
  it('hides kanban recipes on classic projects', () => {
    const list = recipesFor({ ...ctx, kanban: false }).map((r) => r.recipe.id)
    assert.ok(list.includes('archive-done'))
    assert.ok(!list.includes('claim-start'))
  })
  it('explains missing columns or agents', () => {
    const noReady = recipesFor({ ...ctx, statuses: statuses.filter((s) => s.name !== 'Ready') })
    assert.match(noReady.find((r) => r.recipe.id === 'unblocked-ready')!.reason, /Ready/)
    assert.match(recipesFor(ctx).find((r) => r.recipe.id === 'column-agent')!.reason, /AI agent/)
    assert.equal(recipesFor(ctx).find((r) => r.recipe.id === 'claim-start')!.reason, '')
  })
  it('finds missing tags case-insensitively', () => {
    const stale = RECIPES.find((r) => r.id === 'stale')!
    assert.deepEqual(missingRecipeTags(stale, tags), ['stale'])
    const bug = RECIPES.find((r) => r.id === 'bug-priority')!
    assert.deepEqual(missingRecipeTags(bug, tags), [])
  })
  it('builds rules with resolved columns and tags', () => {
    const slipping = buildRecipe(RECIPES.find((r) => r.id === 'slipping')!, ctx, tagIDMap(tags))
    assert.equal(slipping.length, 3)
    assert.deepEqual(slipping[0].actions, [{ type: 'add_tag', tag_id: 10 }])
    assert.equal(slipping[0].recipe_id, 'slipping')
    const claim = buildRecipe(RECIPES.find((r) => r.id === 'claim-start')!, ctx, {})
    assert.deepEqual(claim[0].conditions, { status_ids: [1] })
    assert.deepEqual(claim[0].actions, [{ type: 'set_status', status_id: 2 }])
  })
})

describe('changeSummary', () => {
  it('formats field moves and errors', () => {
    assert.equal(changeSummary({ action: 'set_status', field: 'status', from: 'To Do', to: 'Doing' }), 'Status: To Do → Doing')
    assert.equal(changeSummary({ action: 'add_tag', field: 'tags', to: 'slipping' }), 'Added tag slipping')
    assert.equal(changeSummary({ action: 'set_sprint', error: 'no sprint is running' }), 'set sprint failed: no sprint is running')
  })
})
