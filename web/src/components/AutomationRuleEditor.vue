<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { api } from '@/api/client'
import type { AutomationAction, AutomationActionType, AutomationRule, CustomFieldDef, Project } from '@/api/types'
import { APIError } from '@/api/types'
import { useToast } from '@/composables/useToast'
import {
  MAX_ACTIONS,
  PRIORITIES,
  TEMPLATE_VARS,
  availableActions,
  availableTriggers,
  defaultAction,
  describeRule,
  draftError,
  draftFromRule,
  draftPayload,
  emptyDraft,
  onTriggerChanged,
  triggerInfo,
  type RuleDraft,
  type RuleLookups,
} from '@/utils/automationRules'

const props = defineProps<{
  project: Project
  /** The rule being edited; null for a new rule. */
  rule: AutomationRule | null
  /** Starting values for a new rule (e.g. from a recipe). */
  initial?: RuleDraft | null
  lookups: RuleLookups
  customFields: CustomFieldDef[]
}>()

const emit = defineEmits<{ saved: [rule: AutomationRule]; cancel: [] }>()

const toast = useToast()
const kanban = computed(() => (props.project.workflow_mode || 'classic') === 'kanban')
const draft = reactive<RuleDraft>(
  props.rule ? draftFromRule(props.rule) : props.initial ? structuredClone(props.initial) : emptyDraft(kanban.value),
)
// Selects bind to "" / 0 for "any", so fill the blanks the API leaves out.
draft.conditions = {
  min_priority: 0,
  sprint: '',
  assignee: '',
  field_key: '',
  has_due: '',
  completion: '',
  task_kind: '',
  ...draft.conditions,
}
const saving = ref(false)
const previewing = ref(false)
const preview = ref<{ id: number; title: string }[] | null>(null)
const showFilters = ref(hasFilters())

const triggers = computed(() => availableTriggers(kanban.value))
const eventTriggers = computed(() => triggers.value.filter((t) => !t.timed))
const timedTriggers = computed(() => triggers.value.filter((t) => t.timed))
const actionTypes = computed(() => availableActions(kanban.value))
const trigger = computed(() => triggerInfo(draft.trigger_type))
const error = computed(() => draftError(draft))
const sentence = computed(() => describeRule(draft, props.lookups))
const humans = computed(() => props.lookups.members.filter((m) => !m.is_agent))
const usableTags = computed(() => props.lookups.tags.filter((t) => !t.protected))
const datedSprints = computed(() => props.lookups.sprints)

watch(
  () => draft.trigger_type,
  () => {
    onTriggerChanged(draft)
    preview.value = null
  },
)

function hasFilters(): boolean {
  const c = draft.conditions
  return !!(
    c.status_ids?.length ||
    c.exclude_status_ids?.length ||
    c.min_priority ||
    c.tags_any?.length ||
    c.tags_none?.length ||
    c.sprint ||
    c.assignee ||
    c.field_key ||
    c.has_due ||
    c.completion ||
    c.task_kind
  )
}

function toggle(list: number[] | undefined, id: number): number[] {
  const cur = list || []
  return cur.includes(id) ? cur.filter((x) => x !== id) : [...cur, id]
}

function addAction() {
  if (draft.actions.length >= MAX_ACTIONS) return
  draft.actions.push(defaultAction(kanban.value ? 'set_status' : 'add_tag'))
}

function changeActionType(i: number, type: AutomationActionType) {
  draft.actions[i] = defaultAction(type)
}

function moveAction(i: number, delta: number) {
  const j = i + delta
  if (j < 0 || j >= draft.actions.length) return
  const list = draft.actions
  ;[list[i], list[j]] = [list[j], list[i]]
}

function removeAction(i: number) {
  draft.actions.splice(i, 1)
}

function numberOrUndefined(v: string): number | undefined {
  const n = Number(v)
  return v === '' || Number.isNaN(n) ? undefined : n
}

function insertVar(a: AutomationAction, name: string) {
  a.body = `${a.body || ''}{${name}}`
}

async function runPreview() {
  previewing.value = true
  try {
    const res = await api.previewAutomationRule(props.project.id, draftPayload(draft))
    preview.value = res.tasks
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Preview failed', 'error')
  } finally {
    previewing.value = false
  }
}

async function save() {
  if (error.value) return
  saving.value = true
  try {
    const payload = draftPayload(draft)
    const saved = props.rule
      ? await api.updateAutomationRule(props.project.id, props.rule.id, payload)
      : await api.createAutomationRule(props.project.id, payload)
    toast.push(props.rule ? 'Rule saved' : 'Rule added', 'success')
    emit('saved', saved)
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Could not save the rule', 'error')
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <form class="card card-body mb-3 automation-editor" @submit.prevent="save">
    <h5 class="h6 mb-3">{{ rule ? 'Edit rule' : 'New rule' }}</h5>

    <div class="mb-3">
      <label class="form-label small mb-1" for="rule-name">Name</label>
      <input
        id="rule-name"
        v-model="draft.name"
        type="text"
        class="form-control form-control-sm"
        maxlength="80"
        placeholder="e.g. Archive done tasks after 30 days"
        required
      />
    </div>

    <!-- WHEN -->
    <fieldset class="automation-step mb-3">
      <legend class="automation-step-label">When</legend>
      <select v-model="draft.trigger_type" class="form-select form-select-sm mb-2" aria-label="Trigger">
        <optgroup label="Something happens">
          <option v-for="t in eventTriggers" :key="t.id" :value="t.id">{{ t.label }}</option>
        </optgroup>
        <optgroup label="Checked every 15 minutes">
          <option v-for="t in timedTriggers" :key="t.id" :value="t.id">{{ t.label }}</option>
        </optgroup>
      </select>

      <div v-if="trigger?.timed" class="d-flex align-items-center gap-2 small">
        <input
          v-model.number="draft.trigger_config.days"
          type="number"
          class="form-control form-control-sm automation-days"
          :min="trigger.minDays ?? 0"
          max="365"
          aria-label="Days"
        />
        <span class="text-muted">{{ trigger.daysLabel }}</span>
      </div>
      <p v-if="trigger?.timed" class="form-text mb-0">
        Acts once per task each time it newly qualifies (for example, once per due date).
      </p>

      <template v-if="draft.trigger_type === 'task.status_changed'">
        <div class="small mt-1 mb-1">Into (optional)</div>
        <div class="d-flex flex-wrap gap-1 mb-2">
          <button
            v-for="s in lookups.statuses"
            :key="s.id"
            type="button"
            class="btn btn-sm"
            :class="draft.trigger_config.to_status_ids?.includes(s.id) ? 'btn-primary' : 'btn-outline-secondary'"
            :aria-pressed="!!draft.trigger_config.to_status_ids?.includes(s.id)"
            @click="draft.trigger_config.to_status_ids = toggle(draft.trigger_config.to_status_ids, s.id)"
          >{{ s.name }}</button>
        </div>
        <div class="small mb-1">Out of (optional)</div>
        <div class="d-flex flex-wrap gap-1">
          <button
            v-for="s in lookups.statuses"
            :key="s.id"
            type="button"
            class="btn btn-sm"
            :class="draft.trigger_config.from_status_ids?.includes(s.id) ? 'btn-primary' : 'btn-outline-secondary'"
            :aria-pressed="!!draft.trigger_config.from_status_ids?.includes(s.id)"
            @click="draft.trigger_config.from_status_ids = toggle(draft.trigger_config.from_status_ids, s.id)"
          >{{ s.name }}</button>
        </div>
      </template>

      <template v-if="draft.trigger_type === 'task.tagged'">
        <div class="small mt-1 mb-1">Only these tags (optional)</div>
        <div class="d-flex flex-wrap gap-1">
          <button
            v-for="t in usableTags"
            :key="t.id"
            type="button"
            class="btn btn-sm"
            :class="draft.trigger_config.tag_ids?.includes(t.id) ? 'btn-primary' : 'btn-outline-secondary'"
            :aria-pressed="!!draft.trigger_config.tag_ids?.includes(t.id)"
            @click="draft.trigger_config.tag_ids = toggle(draft.trigger_config.tag_ids, t.id)"
          >{{ t.name }}</button>
          <span v-if="!usableTags.length" class="small text-muted">This project has no tags yet.</span>
        </div>
      </template>
    </fieldset>

    <!-- IF -->
    <fieldset class="automation-step mb-3">
      <legend class="automation-step-label">If <span class="fw-normal text-muted">(optional)</span></legend>
      <button
        v-if="!showFilters"
        type="button"
        class="btn btn-sm btn-outline-secondary"
        @click="showFilters = true"
      >
        <i class="bi bi-funnel me-1" />Only for some tasks…
      </button>
      <div v-else class="row g-2 small">
        <div v-if="kanban" class="col-12">
          <div class="mb-1">In column</div>
          <div class="d-flex flex-wrap gap-1">
            <button
              v-for="s in lookups.statuses"
              :key="s.id"
              type="button"
              class="btn btn-sm"
              :class="draft.conditions.status_ids?.includes(s.id) ? 'btn-primary' : 'btn-outline-secondary'"
              :aria-pressed="!!draft.conditions.status_ids?.includes(s.id)"
              @click="draft.conditions.status_ids = toggle(draft.conditions.status_ids, s.id)"
            >{{ s.name }}</button>
          </div>
        </div>
        <div v-if="usableTags.length" class="col-sm-6">
          <div class="mb-1">Has any of these tags</div>
          <div class="d-flex flex-wrap gap-1">
            <button
              v-for="t in usableTags"
              :key="t.id"
              type="button"
              class="btn btn-sm"
              :class="draft.conditions.tags_any?.includes(t.id) ? 'btn-primary' : 'btn-outline-secondary'"
              :aria-pressed="!!draft.conditions.tags_any?.includes(t.id)"
              @click="draft.conditions.tags_any = toggle(draft.conditions.tags_any, t.id)"
            >{{ t.name }}</button>
          </div>
        </div>
        <div v-if="usableTags.length" class="col-sm-6">
          <div class="mb-1">Has none of these tags</div>
          <div class="d-flex flex-wrap gap-1">
            <button
              v-for="t in usableTags"
              :key="t.id"
              type="button"
              class="btn btn-sm"
              :class="draft.conditions.tags_none?.includes(t.id) ? 'btn-danger' : 'btn-outline-secondary'"
              :aria-pressed="!!draft.conditions.tags_none?.includes(t.id)"
              @click="draft.conditions.tags_none = toggle(draft.conditions.tags_none, t.id)"
            >{{ t.name }}</button>
          </div>
        </div>
        <div class="col-sm-4">
          <label class="form-label mb-1" for="rule-min-priority">Priority at least</label>
          <select id="rule-min-priority" v-model.number="draft.conditions.min_priority" class="form-select form-select-sm">
            <option :value="0">Any</option>
            <option v-for="p in PRIORITIES.slice(1)" :key="p.value" :value="p.value">{{ p.label }}</option>
          </select>
        </div>
        <div class="col-sm-4">
          <label class="form-label mb-1" for="rule-completion">State</label>
          <select id="rule-completion" v-model="draft.conditions.completion" class="form-select form-select-sm">
            <option value="">Open or completed</option>
            <option value="open">Open</option>
            <option value="done">Completed</option>
          </select>
        </div>
        <div class="col-sm-4">
          <label class="form-label mb-1" for="rule-has-due">Due date</label>
          <select id="rule-has-due" v-model="draft.conditions.has_due" class="form-select form-select-sm">
            <option value="">Any</option>
            <option value="yes">Has a due date</option>
            <option value="no">No due date</option>
          </select>
        </div>
        <div v-if="kanban" class="col-sm-4">
          <label class="form-label mb-1" for="rule-assignee">Assignee</label>
          <select id="rule-assignee" v-model="draft.conditions.assignee" class="form-select form-select-sm">
            <option value="">Anyone or nobody</option>
            <option value="unassigned">Unassigned</option>
            <option value="assigned">Assigned</option>
            <option value="user">A specific person</option>
          </select>
          <select
            v-if="draft.conditions.assignee === 'user'"
            v-model.number="draft.conditions.assignee_id"
            class="form-select form-select-sm mt-1"
            aria-label="Person"
          >
            <option v-for="m in humans" :key="m.user_id" :value="m.user_id">{{ m.user_name || m.email }}</option>
          </select>
        </div>
        <div v-if="kanban" class="col-sm-4">
          <label class="form-label mb-1" for="rule-sprint">Sprint</label>
          <select id="rule-sprint" v-model="draft.conditions.sprint" class="form-select form-select-sm">
            <option value="">Any or none</option>
            <option value="current">Current sprint</option>
            <option value="any">In a sprint</option>
            <option value="none">Backlog</option>
            <option v-if="datedSprints.length" value="specific">A specific sprint</option>
          </select>
          <select
            v-if="draft.conditions.sprint === 'specific'"
            v-model.number="draft.conditions.sprint_id"
            class="form-select form-select-sm mt-1"
            aria-label="Sprint"
          >
            <option v-for="s in datedSprints" :key="s.id" :value="s.id">{{ s.name }}</option>
          </select>
        </div>
        <div class="col-sm-4">
          <label class="form-label mb-1" for="rule-kind">Task</label>
          <select id="rule-kind" v-model="draft.conditions.task_kind" class="form-select form-select-sm">
            <option value="">Any</option>
            <option value="root">Top-level only</option>
            <option value="subtask">Subtasks only</option>
          </select>
        </div>
        <div v-if="customFields.length" class="col-12">
          <label class="form-label mb-1" for="rule-field-key">Custom field equals</label>
          <div class="d-flex gap-2">
            <select id="rule-field-key" v-model="draft.conditions.field_key" class="form-select form-select-sm">
              <option value="">No field filter</option>
              <option v-for="f in customFields" :key="f.field_key" :value="f.field_key">{{ f.label }}</option>
            </select>
            <input
              v-if="draft.conditions.field_key"
              v-model="draft.conditions.field_value"
              type="text"
              class="form-control form-control-sm"
              maxlength="200"
              placeholder="Value"
              aria-label="Field value"
            />
          </div>
        </div>
      </div>
    </fieldset>

    <!-- THEN -->
    <fieldset class="automation-step mb-3">
      <legend class="automation-step-label">Then</legend>
      <div v-for="(a, i) in draft.actions" :key="i" class="automation-action mb-2">
        <div class="d-flex gap-2 align-items-start">
          <select
            :value="a.type"
            class="form-select form-select-sm automation-action-type"
            :aria-label="`Action ${i + 1}`"
            @change="changeActionType(i, ($event.target as HTMLSelectElement).value as AutomationActionType)"
          >
            <option v-for="t in actionTypes" :key="t.id" :value="t.id">{{ t.label }}</option>
          </select>

          <div class="flex-grow-1">
            <select v-if="a.type === 'set_status'" v-model.number="a.status_id" class="form-select form-select-sm" aria-label="Column">
              <option :value="undefined" disabled>Choose a column</option>
              <option v-for="s in lookups.statuses" :key="s.id" :value="s.id">{{ s.name }}</option>
            </select>
            <select v-else-if="a.type === 'set_priority'" v-model.number="a.priority" class="form-select form-select-sm" aria-label="Priority">
              <option v-for="p in PRIORITIES" :key="p.value" :value="p.value">{{ p.label }}</option>
            </select>
            <select
              v-else-if="a.type === 'add_tag' || a.type === 'remove_tag'"
              v-model.number="a.tag_id"
              class="form-select form-select-sm"
              aria-label="Tag"
            >
              <option :value="undefined" disabled>{{ usableTags.length ? 'Choose a tag' : 'Create a tag on the Tags tab first' }}</option>
              <option v-for="t in usableTags" :key="t.id" :value="t.id">{{ t.name }}</option>
            </select>
            <select v-else-if="a.type === 'assign'" v-model.number="a.user_id" class="form-select form-select-sm" aria-label="Assignee">
              <option :value="undefined" disabled>Choose a person</option>
              <option v-for="m in humans" :key="m.user_id" :value="m.user_id">{{ m.user_name || m.email }}</option>
            </select>
            <template v-else-if="a.type === 'set_sprint'">
              <select v-model="a.sprint" class="form-select form-select-sm" aria-label="Sprint">
                <option value="current">Current sprint</option>
                <option value="next">Next sprint</option>
                <option value="backlog">Backlog</option>
                <option v-if="datedSprints.length" value="specific">A specific sprint</option>
              </select>
              <select v-if="a.sprint === 'specific'" v-model.number="a.sprint_id" class="form-select form-select-sm mt-1" aria-label="Which sprint">
                <option v-for="s in datedSprints" :key="s.id" :value="s.id">{{ s.name }}</option>
              </select>
            </template>
            <div v-else-if="a.type === 'set_due'" class="d-flex align-items-center gap-2 small">
              <span>Today +</span>
              <input
                :value="a.days ?? 0"
                type="number"
                min="0"
                max="365"
                class="form-control form-control-sm automation-days"
                aria-label="Days from today"
                @input="a.days = numberOrUndefined(($event.target as HTMLInputElement).value)"
              />
              <span>days</span>
            </div>
            <select v-else-if="a.type === 'queue_agent'" v-model.number="a.agent_id" class="form-select form-select-sm" aria-label="AI agent">
              <option :value="undefined" disabled>{{ lookups.agents.length ? 'Choose an AI agent' : 'Add an AI agent first' }}</option>
              <option v-for="ag in lookups.agents" :key="ag.id" :value="ag.id">{{ ag.name }} (@{{ ag.handle }})</option>
            </select>
            <template v-else-if="a.type === 'notify'">
              <select v-model="a.target" class="form-select form-select-sm mb-1" aria-label="Who to notify">
                <option value="watchers">Watchers</option>
                <option v-if="kanban" value="assignee">Assignee</option>
              </select>
            </template>
            <span v-else class="small text-muted d-inline-block pt-1">
              <template v-if="a.type === 'archive'">Archives the task and its subtasks.</template>
              <template v-else-if="a.type === 'unassign'">Clears the claim.</template>
            </span>

            <template v-if="a.type === 'comment' || a.type === 'notify' || a.type === 'queue_agent'">
              <textarea
                v-model="a.body"
                class="form-control form-control-sm"
                :class="{ 'mt-1': a.type === 'queue_agent' }"
                :rows="a.type === 'comment' ? 3 : 2"
                :maxlength="a.type === 'notify' ? 200 : 1000"
                :placeholder="a.type === 'queue_agent' ? 'Note for the agent (optional)' : a.type === 'comment' ? 'Comment text' : 'Notification text'"
                :aria-label="a.type === 'comment' ? 'Comment text' : 'Message'"
              />
              <div class="d-flex flex-wrap gap-1 mt-1">
                <button
                  v-for="v in TEMPLATE_VARS"
                  :key="v"
                  type="button"
                  class="btn btn-sm btn-link p-0 small automation-var"
                  @click="insertVar(a, v)"
                >{{ '{' + v + '}' }}</button>
              </div>
            </template>
          </div>

          <div class="btn-group btn-group-sm flex-shrink-0">
            <button type="button" class="btn btn-outline-secondary" :disabled="i === 0" aria-label="Move up" @click="moveAction(i, -1)">
              <i class="bi bi-arrow-up" />
            </button>
            <button
              type="button"
              class="btn btn-outline-secondary"
              :disabled="i === draft.actions.length - 1"
              aria-label="Move down"
              @click="moveAction(i, 1)"
            >
              <i class="bi bi-arrow-down" />
            </button>
            <button type="button" class="btn btn-outline-danger" :disabled="draft.actions.length === 1" aria-label="Remove action" @click="removeAction(i)">
              <i class="bi bi-x-lg" />
            </button>
          </div>
        </div>
      </div>
      <button type="button" class="btn btn-sm btn-outline-secondary" :disabled="draft.actions.length >= MAX_ACTIONS" @click="addAction">
        <i class="bi bi-plus-lg me-1" />Add action
      </button>
    </fieldset>

    <div class="alert alert-light border small py-2 mb-3">
      <i class="bi bi-lightning-charge me-1 text-warning" aria-hidden="true" />{{ sentence }}
      <div class="text-muted mt-1">
        Changes are made by <strong>Automation</strong> and never trigger other rules.
      </div>
    </div>

    <div v-if="preview" class="mb-3 small">
      <div class="fw-semibold mb-1">
        <template v-if="trigger?.timed">Would act on {{ preview.length }}{{ preview.length >= 50 ? '+' : '' }} task{{ preview.length === 1 ? '' : 's' }} on the next check</template>
        <template v-else>{{ preview.length }}{{ preview.length >= 50 ? '+' : '' }} existing task{{ preview.length === 1 ? '' : 's' }} pass the filters (event rules only act when the event happens)</template>
      </div>
      <ul v-if="preview.length" class="mb-0 ps-3 automation-preview">
        <li v-for="t in preview" :key="t.id">#{{ t.id }} {{ t.title }}</li>
      </ul>
    </div>

    <div class="d-flex flex-wrap align-items-center gap-2">
      <button type="submit" class="btn btn-sm btn-primary" :disabled="!!error || saving">
        {{ saving ? 'Saving…' : rule ? 'Save rule' : 'Add rule' }}
      </button>
      <button type="button" class="btn btn-sm btn-outline-secondary" :disabled="previewing" @click="runPreview">
        <i class="bi bi-eye me-1" />{{ previewing ? 'Checking…' : 'Test rule' }}
      </button>
      <button type="button" class="btn btn-sm btn-link" @click="emit('cancel')">Cancel</button>
      <div class="form-check form-switch ms-auto mb-0 small">
        <input id="rule-enabled" v-model="draft.enabled" class="form-check-input" type="checkbox" role="switch" />
        <label class="form-check-label" for="rule-enabled">Enabled</label>
      </div>
    </div>
    <div v-if="error" class="small text-danger mt-2">{{ error }}</div>
  </form>
</template>

<style scoped>
.automation-step {
  border-left: 3px solid var(--bs-border-color);
  padding-left: 0.75rem;
}
.automation-step-label {
  font-size: 0.75rem;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.04em;
  color: var(--bs-secondary-color);
  margin-bottom: 0.4rem;
  float: none;
  width: auto;
}
.automation-days {
  width: 5rem;
}
.automation-action-type {
  width: 12rem;
  flex-shrink: 0;
}
.automation-var {
  text-decoration: none;
  font-family: var(--bs-font-monospace);
}
.automation-preview {
  max-height: 12rem;
  overflow-y: auto;
}
@media (max-width: 575.98px) {
  .automation-action > .d-flex {
    flex-wrap: wrap;
  }
  .automation-action-type {
    width: 100%;
  }
}
</style>
