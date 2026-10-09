<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { RouterLink } from 'vue-router'
import Sortable from 'sortablejs'
import { api } from '@/api/client'
import type {
  AutomationRule,
  AutomationRun,
  CustomFieldDef,
  Project,
  ProjectAgent,
  ProjectMember,
  ProjectSprint,
  ProjectStatus,
  Tag,
} from '@/api/types'
import { APIError } from '@/api/types'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import AutomationRuleEditor from '@/components/AutomationRuleEditor.vue'
import {
  MAX_RULES,
  buildRecipe,
  changeSummary,
  describeRule,
  missingRecipeTags,
  recipesFor,
  tagIDMap,
  triggerInfo,
  type Recipe,
  type RecipeContext,
  type RuleDraft,
  type RuleLookups,
} from '@/utils/automationRules'

const props = defineProps<{ project: Project }>()
const emit = defineEmits<{ changed: [] }>()

const toast = useToast()
const { askConfirm } = useConfirm()

const rules = ref<AutomationRule[]>([])
const runs = ref<AutomationRun[]>([])
const statuses = ref<ProjectStatus[]>([])
const tags = ref<Tag[]>([])
const sprints = ref<ProjectSprint[]>([])
const members = ref<ProjectMember[]>([])
const agents = ref<ProjectAgent[]>([])
const customFields = ref<CustomFieldDef[]>([])
const loading = ref(false)
const busyId = ref<number | null>(null)
const addingRecipe = ref<string | null>(null)

/** null: list view; 'new': creating; number: editing that rule. */
const editing = ref<'new' | number | null>(null)
const editorInitial = ref<RuleDraft | null>(null)
const runFilter = ref(0)
const ruleListEl = ref<HTMLElement | null>(null)
const reordering = ref(false)
let sortable: Sortable | null = null

const kanban = computed(() => (props.project.workflow_mode || 'classic') === 'kanban')
const archived = computed(() => !!props.project.archived)
const lookups = computed<RuleLookups>(() => ({
  statuses: statuses.value,
  tags: tags.value,
  sprints: sprints.value,
  members: members.value,
  agents: agents.value,
}))
const recipeCtx = computed<RecipeContext>(() => ({
  kanban: kanban.value,
  statuses: statuses.value,
  tags: tags.value,
  agents: agents.value,
}))
const recipes = computed(() => recipesFor(recipeCtx.value))
const installedRecipes = computed(() => new Set(rules.value.map((r) => r.recipe_id).filter(Boolean)))
const editingRule = computed(() =>
  typeof editing.value === 'number' ? rules.value.find((r) => r.id === editing.value) || null : null,
)
const filteredRuns = computed(() => (runFilter.value ? runs.value.filter((r) => r.rule_id === runFilter.value) : runs.value))
const atLimit = computed(() => rules.value.length >= MAX_RULES)

function formatTime(iso: string | null) {
  if (!iso) return ''
  try {
    return new Date(iso).toLocaleString()
  } catch {
    return iso
  }
}

function errMsg(err: unknown, fallback: string) {
  return err instanceof APIError ? err.message : fallback
}

async function load() {
  loading.value = true
  const pid = props.project.id
  try {
    const [ruleList, runList, tagList, memberList, fieldList] = await Promise.all([
      api.listAutomationRules(pid),
      api.listAutomationRuns(pid).catch(() => [] as AutomationRun[]),
      api.listTags({ project_id: pid }).catch(() => [] as Tag[]),
      api.listProjectMembers(pid).catch(() => [] as ProjectMember[]),
      api.listProjectCustomFields(pid).catch(() => ({ fields: [] as CustomFieldDef[] })),
    ])
    rules.value = ruleList
    runs.value = runList
    tags.value = tagList.filter((t) => t.project_id === pid && !t.protected)
    members.value = memberList
    customFields.value = fieldList.fields || []
    if (kanban.value) {
      const [st, sp, ag] = await Promise.all([
        api.listProjectStatuses(pid).catch(() => [] as ProjectStatus[]),
        api.listProjectSprints(pid).catch(() => [] as ProjectSprint[]),
        api.listProjectAgents(pid).catch(() => [] as ProjectAgent[]),
      ])
      statuses.value = st
      sprints.value = sp
      agents.value = ag
    } else {
      statuses.value = []
      sprints.value = []
      agents.value = []
    }
  } catch (err) {
    toast.push(errMsg(err, 'Failed to load automation rules'), 'error')
  } finally {
    loading.value = false
  }
}

watch(
  () => props.project.id,
  () => {
    editing.value = null
    runFilter.value = 0
    void load()
  },
  { immediate: true },
)

async function refreshRuns() {
  runs.value = await api.listAutomationRuns(props.project.id).catch(() => runs.value)
}

function openNew(initial: RuleDraft | null = null) {
  editorInitial.value = initial
  editing.value = 'new'
}

function editRule(rule: AutomationRule) {
  editorInitial.value = null
  editing.value = rule.id
}

function onSaved(rule: AutomationRule) {
  const i = rules.value.findIndex((r) => r.id === rule.id)
  if (i >= 0) rules.value[i] = rule
  else rules.value.push(rule)
  editing.value = null
  editorInitial.value = null
  emit('changed')
}

async function toggleEnabled(rule: AutomationRule) {
  busyId.value = rule.id
  try {
    const saved = await api.updateAutomationRule(props.project.id, rule.id, { enabled: !rule.enabled })
    onSaved(saved)
  } catch (err) {
    toast.push(errMsg(err, 'Could not update the rule'), 'error')
  } finally {
    busyId.value = null
  }
}

async function removeRule(rule: AutomationRule) {
  const ok = await askConfirm({
    title: 'Delete rule',
    message: `Delete “${rule.name}” and its run history? Changes it already made stay as they are.`,
    confirmLabel: 'Delete',
    danger: true,
  })
  if (!ok) return
  busyId.value = rule.id
  try {
    await api.deleteAutomationRule(props.project.id, rule.id)
    rules.value = rules.value.filter((r) => r.id !== rule.id)
    runs.value = runs.value.filter((r) => r.rule_id !== rule.id)
    if (runFilter.value === rule.id) runFilter.value = 0
    emit('changed')
  } catch (err) {
    toast.push(errMsg(err, 'Could not delete the rule'), 'error')
  } finally {
    busyId.value = null
  }
}

async function persistOrder(ids: number[]) {
  const previous = rules.value
  const byId = new Map(previous.map((r) => [r.id, r]))
  rules.value = ids.map((id) => byId.get(id)!).filter(Boolean)
  reordering.value = true
  try {
    rules.value = await api.reorderAutomationRules(props.project.id, ids)
  } catch (err) {
    rules.value = previous
    toast.push(errMsg(err, 'Could not reorder rules'), 'error')
  } finally {
    reordering.value = false
  }
}

function destroySortable() {
  sortable?.destroy()
  sortable = null
}

/** The rule list unmounts while the editor is open, so (re)attach whenever the element changes. */
watch(ruleListEl, (el) => {
  destroySortable()
  if (!el) return
  sortable = Sortable.create(el, {
    handle: '.rule-drag-handle',
    draggable: '.rule-reorder-item',
    animation: 150,
    onEnd(evt) {
      const from = evt.oldDraggableIndex
      const to = evt.newDraggableIndex
      if (from === undefined || to === undefined || from === to) return
      // Put the node back where Vue rendered it; the reactive reorder below moves it for real.
      const parent = evt.from
      parent.removeChild(evt.item)
      parent.insertBefore(evt.item, parent.children[from] ?? null)
      const ids = rules.value.map((r) => r.id)
      const [id] = ids.splice(from, 1)
      ids.splice(to, 0, id)
      void persistOrder(ids)
    },
  })
})

function move(index: number, delta: number) {
  const next = index + delta
  if (reordering.value || next < 0 || next >= rules.value.length) return
  const ids = rules.value.map((r) => r.id)
  const [id] = ids.splice(index, 1)
  ids.splice(next, 0, id)
  void persistOrder(ids)
}

watch(reordering,(busy) => sortable?.option('disabled', busy))

onBeforeUnmount(destroySortable)

/** Creates any tags the recipe needs, then its rules (or opens the editor when it needs a choice). */
async function addRecipe(recipe: Recipe) {
  addingRecipe.value = recipe.id
  try {
    for (const name of missingRecipeTags(recipe, tags.value)) {
      const tag = await api.createTag(name, props.project.id)
      tags.value = [...tags.value, tag]
    }
    const built = buildRecipe(recipe, recipeCtx.value, tagIDMap(tags.value))
    if (recipe.needsChoice && built.length === 1) {
      const r = built[0]
      openNew({
        name: r.name || recipe.label,
        enabled: true,
        trigger_type: r.trigger_type!,
        trigger_config: r.trigger_config || {},
        conditions: r.conditions || {},
        actions: r.actions || [],
        recipe_id: recipe.id,
      })
      return
    }
    if (rules.value.length + built.length > MAX_RULES) {
      toast.push(`That would go over the limit of ${MAX_RULES} rules.`, 'error')
      return
    }
    for (const input of built) {
      const saved = await api.createAutomationRule(props.project.id, input)
      rules.value.push(saved)
    }
    toast.push(built.length === 1 ? 'Rule added' : `${built.length} rules added`, 'success')
    emit('changed')
  } catch (err) {
    toast.push(errMsg(err, 'Could not add the starter rule'), 'error')
  } finally {
    addingRecipe.value = null
  }
}

function lastRunLabel(rule: AutomationRule) {
  if (!rule.last_run_at) return 'Never run'
  return `Last ran ${formatTime(rule.last_run_at)}`
}
</script>

<template>
  <div>
    <template v-if="editing !== null">
      <button type="button" class="btn btn-sm btn-link px-0 mb-2" @click="editing = null">
        <i class="bi bi-arrow-left me-1" />All rules
      </button>
      <AutomationRuleEditor
        :key="String(editing)"
        :project="project"
        :rule="editingRule"
        :initial="editorInitial"
        :lookups="lookups"
        :custom-fields="customFields"
        @saved="onSaved"
        @cancel="editing = null"
      />
    </template>

    <template v-else>
      <h4 class="h6 mb-2">Automation rules</h4>
      <p class="small text-muted mb-2">
        Let the project tidy itself up: each rule says <strong>when</strong> something happens, <strong>if</strong>
        the task matches, <strong>then</strong> what to change. Rules act as the protected
        <span class="badge text-bg-light border"><i class="bi bi-gear-wide-connected me-1" />Automation</span> account,
        so every change shows up in task history with the rule that made it. Changes made by a rule never trigger other
        rules.
      </p>

      <div v-if="archived" class="alert alert-warning small py-2">
        This project is archived, so its rules are not running. Restore it to resume them.
      </div>

      <p v-if="loading && !rules.length" class="small text-muted">Loading rules…</p>
      <ul v-else ref="ruleListEl" class="list-group mb-3">
        <li
          v-for="(r, i) in rules"
          :key="r.id"
          class="list-group-item rule-reorder-item"
          :class="{ 'automation-rule--off': !r.enabled }"
        >
          <div class="d-flex flex-wrap align-items-start gap-2">
            <span
              v-if="rules.length > 1"
              class="rule-drag-handle text-muted pt-1"
              title="Drag to reorder"
              aria-label="Drag to reorder rule"
            >
              <i class="bi bi-grip-vertical" />
            </span>
            <div class="form-check form-switch mb-0 pt-1">
              <input
                :id="`rule-on-${r.id}`"
                class="form-check-input"
                type="checkbox"
                role="switch"
                :checked="r.enabled"
                :disabled="busyId === r.id || archived"
                :aria-label="r.enabled ? `Turn off ${r.name}` : `Turn on ${r.name}`"
                @change="toggleEnabled(r)"
              />
            </div>
            <div class="flex-grow-1" style="min-width: 0">
              <div class="fw-semibold">
                {{ r.name }}
                <span v-if="triggerInfo(r.trigger_type)?.timed" class="badge text-bg-light border fw-normal ms-1">
                  <i class="bi bi-clock me-1" />Timed
                </span>
              </div>
              <div class="small">{{ describeRule(r, lookups) }}</div>
              <div class="small text-muted">
                {{ lastRunLabel(r) }} · {{ r.run_count }} run{{ r.run_count === 1 ? '' : 's' }}
                <template v-if="r.error_count"> · {{ r.error_count }} with errors</template>
              </div>
              <div v-if="r.paused_reason" class="alert alert-warning small py-1 px-2 mt-1 mb-0">
                <i class="bi bi-pause-circle me-1" />{{ r.paused_reason }} Fix the rule, then turn it back on.
              </div>
              <div v-else-if="r.consecutive_errors && r.last_error" class="small text-danger mt-1">
                Last run failed: {{ r.last_error }}
              </div>
            </div>
            <div class="d-flex gap-1 flex-shrink-0">
              <div v-if="rules.length > 1" class="btn-group btn-group-sm">
                <button
                  type="button"
                  class="btn btn-outline-secondary"
                  :disabled="i === 0 || reordering"
                  :aria-label="`Move ${r.name} up`"
                  @click="move(i, -1)"
                >
                  <i class="bi bi-arrow-up" />
                </button>
                <button
                  type="button"
                  class="btn btn-outline-secondary"
                  :disabled="i === rules.length - 1 || reordering"
                  :aria-label="`Move ${r.name} down`"
                  @click="move(i, 1)"
                >
                  <i class="bi bi-arrow-down" />
                </button>
              </div>
              <button type="button" class="btn btn-sm btn-outline-primary" @click="editRule(r)">Edit</button>
              <button
                type="button"
                class="btn btn-sm btn-outline-danger"
                :disabled="busyId === r.id"
                :aria-label="`Delete ${r.name}`"
                @click="removeRule(r)"
              >
                <i class="bi bi-trash" />
              </button>
            </div>
          </div>
        </li>
        <li v-if="!rules.length && !loading" class="list-group-item text-muted small">
          No rules yet. Start from one below, or build your own.
        </li>
      </ul>

      <div class="d-flex flex-wrap align-items-center gap-2 mb-4">
        <button type="button" class="btn btn-sm btn-primary" :disabled="atLimit || archived" @click="openNew(null)">
          <i class="bi bi-plus-lg me-1" />New rule
        </button>
        <span v-if="atLimit" class="small text-muted">A project can have up to {{ MAX_RULES }} rules.</span>
        <span v-else-if="rules.length > 1" class="small text-muted">Rules run top to bottom; later rules see earlier changes. Drag to reorder.</span>
      </div>

      <details class="mb-4" :open="!rules.length">
        <summary class="fw-semibold small mb-2">Starter rules</summary>
        <div class="row g-2">
          <div v-for="{ recipe, reason } in recipes" :key="recipe.id" class="col-md-6">
            <div class="card card-body h-100 py-2 px-3 small">
              <div class="d-flex align-items-start gap-2">
                <div class="flex-grow-1">
                  <div class="fw-semibold">{{ recipe.label }}</div>
                  <div class="text-muted">{{ recipe.summary }}</div>
                  <div v-if="reason" class="text-warning-emphasis mt-1">{{ reason }}</div>
                  <div v-else-if="missingRecipeTags(recipe, tags).length" class="text-muted mt-1">
                    Creates tag{{ missingRecipeTags(recipe, tags).length === 1 ? '' : 's' }}:
                    {{ missingRecipeTags(recipe, tags).join(', ') }}
                  </div>
                </div>
                <span v-if="installedRecipes.has(recipe.id)" class="badge text-bg-success flex-shrink-0">Added</span>
                <button
                  v-else
                  type="button"
                  class="btn btn-sm btn-outline-primary flex-shrink-0"
                  :disabled="!!reason || archived || atLimit || addingRecipe !== null"
                  @click="addRecipe(recipe)"
                >
                  {{ addingRecipe === recipe.id ? 'Adding…' : recipe.needsChoice ? 'Set up' : 'Add' }}
                </button>
              </div>
            </div>
          </div>
        </div>
      </details>

      <div class="d-flex flex-wrap align-items-center gap-2 mb-2">
        <h5 class="h6 mb-0">Recent runs</h5>
        <select v-if="rules.length" v-model.number="runFilter" class="form-select form-select-sm w-auto" aria-label="Filter runs by rule">
          <option :value="0">All rules</option>
          <option v-for="r in rules" :key="r.id" :value="r.id">{{ r.name }}</option>
        </select>
        <button type="button" class="btn btn-sm btn-link ms-auto" @click="refreshRuns">
          <i class="bi bi-arrow-clockwise me-1" />Refresh
        </button>
      </div>
      <p class="small text-muted mb-2">
        Each time a rule changes a task, it's listed here so you can tell why something moved.
      </p>
      <ul v-if="filteredRuns.length" class="list-group list-group-flush small automation-runs">
        <li v-for="run in filteredRuns" :key="run.id" class="list-group-item px-0">
          <div class="d-flex flex-wrap gap-2 align-items-baseline">
            <span
              class="badge"
              :class="run.outcome === 'error' ? 'text-bg-danger' : 'text-bg-success'"
            >{{ run.outcome === 'error' ? 'Error' : 'Applied' }}</span>
            <span class="fw-semibold">{{ run.rule_name }}</span>
            <span class="text-muted">on</span>
            <RouterLink v-if="run.task_id" :to="`/tasks/${run.task_id}`">#{{ run.task_id }} {{ run.task_title }}</RouterLink>
            <span v-else class="text-muted">a deleted task</span>
            <span class="text-muted ms-auto">{{ formatTime(run.created_at) }}</span>
          </div>
          <ul class="mb-0 ps-3 mt-1">
            <li v-for="(c, ci) in run.changes" :key="ci" :class="{ 'text-danger': !!c.error }">{{ changeSummary(c) }}</li>
          </ul>
        </li>
      </ul>
      <p v-else class="small text-muted">No runs yet.</p>
    </template>
  </div>
</template>

<style scoped>
.rule-drag-handle {
  cursor: grab;
}
.rule-drag-handle:active {
  cursor: grabbing;
}
.automation-rule--off {
  opacity: 0.7;
}
.automation-runs {
  max-height: 24rem;
  overflow-y: auto;
}
</style>
