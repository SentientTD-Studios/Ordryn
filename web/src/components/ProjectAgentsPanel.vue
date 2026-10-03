<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { api } from '@/api/client'
import type { AgentRun, Project, ProjectAgent, ProjectMember, ProjectRoleDef, ProjectStatus } from '@/api/types'
import { APIError } from '@/api/types'
import { RouterLink } from 'vue-router'
import { useToast } from '@/composables/useToast'
import ProjectAgentEditor from '@/components/ProjectAgentEditor.vue'
import {
  AGENT_RECIPES,
  handleError,
  recipeSettings,
  reviewStatusId,
  runStatusBadge,
  suggestHandle,
  triggerLabel,
} from '@/utils/projectAgents'

const props = defineProps<{
  project: Project
}>()

const emit = defineEmits<{ changed: [] }>()

const MAX_AGENTS = 10

const toast = useToast()
const agents = ref<ProjectAgent[]>([])
const statuses = ref<ProjectStatus[]>([])
const roles = ref<ProjectRoleDef[]>([])
const humanMembers = ref<ProjectMember[]>([])
const runs = ref<AgentRun[]>([])
const loading = ref(false)
const apiEnabled = ref(true)
const selectedId = ref<number | null>(null)

const showCreate = ref(false)
const creating = ref(false)
const newName = ref('')
const newHandle = ref('')
const handleTouched = ref(false)
const newRole = ref('editor')
const recipeId = ref('triage')
const repoLinked = ref(false)
const recipe = computed(() => AGENT_RECIPES.find((r) => r.id === recipeId.value) || null)
const reviewColumnName = computed(() => {
  const id = reviewStatusId(statuses.value)
  return statuses.value.find((s) => s.id === id)?.name || ''
})

const isKanban = computed(() => (props.project.workflow_mode || 'classic') === 'kanban')
const selected = computed(() => agents.value.find((a) => a.id === selectedId.value) || null)
const newHandleError = computed(() => (handleTouched.value || newHandle.value ? handleError(newHandle.value) : ''))
const canCreate = computed(
  () => !creating.value && !!newName.value.trim() && !handleError(newHandle.value) && agents.value.length < MAX_AGENTS,
)

/** Roles an agent may hold: anything except owner and roles that manage the project. */
const agentRoles = computed(() =>
  roles.value.filter((r) => r.slug !== 'owner' && !r.permissions.includes('project:manage')),
)

watch(newName, (name) => {
  if (!handleTouched.value) newHandle.value = suggestHandle(name)
})

function formatTime(iso: string) {
  try {
    return new Date(iso).toLocaleString()
  } catch {
    return iso
  }
}

async function load() {
  loading.value = true
  try {
    const [list, roleList, health, runList, memberList] = await Promise.all([
      api.listProjectAgents(props.project.id),
      api.listProjectRoles(props.project.id).catch(() => ({ catalog: [], roles: [] })),
      api.health().catch(() => null),
      api.listProjectAgentRuns(props.project.id).catch(() => [] as AgentRun[]),
      api.listProjectMembers(props.project.id).catch(() => [] as ProjectMember[]),
    ])
    agents.value = list
    roles.value = roleList.roles || []
    apiEnabled.value = health ? health.api_enabled : true
    runs.value = runList
    humanMembers.value = memberList.filter((m) => !m.is_agent)
    statuses.value = isKanban.value ? await api.listProjectStatuses(props.project.id).catch(() => []) : []
    repoLinked.value = await api
      .getProjectGitHub(props.project.id)
      .then((r) => !!r?.linked)
      .catch(() => false)
    if (selectedId.value && !list.some((a) => a.id === selectedId.value)) selectedId.value = null
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Failed to load AI agents', 'error')
  } finally {
    loading.value = false
  }
}

watch(
  () => props.project.id,
  () => {
    selectedId.value = null
    showCreate.value = false
    void load()
  },
  { immediate: true },
)

function resetCreate() {
  newName.value = ''
  newHandle.value = ''
  handleTouched.value = false
  recipeId.value = 'triage'
  newRole.value = agentRoles.value.some((r) => r.slug === 'editor') ? 'editor' : agentRoles.value[0]?.slug || 'editor'
}

function openCreate() {
  resetCreate()
  showCreate.value = true
}

async function createAgent() {
  if (!canCreate.value) return
  creating.value = true
  try {
    const created = await api.createProjectAgent(props.project.id, {
      handle: newHandle.value.trim(),
      name: newName.value.trim(),
      role: newRole.value,
      ...(recipe.value ? recipeSettings(recipe.value, statuses.value) : {}),
    })
    toast.push(`${created.name} added — review its settings, then create a key to connect a model`, 'success')
    showCreate.value = false
    await load()
    selectedId.value = created.id
    emit('changed')
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Could not add agent', 'error')
  } finally {
    creating.value = false
  }
}

function onAgentSaved(updated: ProjectAgent) {
  agents.value = agents.value.map((a) => (a.id === updated.id ? updated : a))
  emit('changed')
}

async function onAgentRemoved() {
  selectedId.value = null
  await load()
  emit('changed')
}

function agentRuns(agentId: number) {
  return runs.value.filter((r) => r.agent_id === agentId)
}

function openRunCount(agentId: number) {
  return agentRuns(agentId).filter((r) => r.status === 'queued' || r.status === 'running').length
}
</script>

<template>
  <div>
    <template v-if="selected">
      <button type="button" class="btn btn-sm btn-link px-0 mb-2" @click="selectedId = null">
        <i class="bi bi-arrow-left me-1" />All agents
      </button>
      <ProjectAgentEditor
        :key="selected.id"
        :project="project"
        :agent="selected"
        :statuses="statuses"
        :roles="agentRoles"
        :caller-roles="roles"
        :members="humanMembers"
        :runs="agentRuns(selected.id)"
        :api-enabled="apiEnabled"
        @saved="onAgentSaved"
        @removed="onAgentRemoved"
        @runs-changed="load"
      />
    </template>

    <template v-else>
      <h4 class="h6 mb-2">AI agents</h4>
      <p class="small text-muted mb-2">
        Hand tasks to an AI model and get the work back on the task, as a comment, an updated card, or new subtasks,
        within limits you set. Each agent is a project member with its own <code>@handle</code>.
      </p>

      <details class="card card-body small mb-3" :open="!agents.length">
        <summary class="fw-semibold">How AI agents work</summary>
        <ol class="mt-2 mb-2 ps-3">
          <li class="mb-1">
            <strong>Add an agent</strong> and pick a starting setup. It fills in the instructions and guardrails
            (which fields it may change, which column it moves cards to). Adjust them later.
          </li>
          <li class="mb-1">
            <strong>Connect a model.</strong> Create a key on the agent's settings and give it to whatever runs the
            model: Claude Code (MCP), a local model with Ollama (see <code>examples/agents/ollama</code>), or your own
            service (webhook or polling). Nothing happens until something is connected.
          </li>
          <li class="mb-1">
            <strong>Send it work.</strong> <code>@mention</code> it in a task comment, drag a card into a trigger column,
            or use <strong>Send to agent</strong> on a task. Only the people you allow can do this; anyone else is ignored.
          </li>
          <li>
            <strong>Review the result.</strong> The agent claims the card, posts what it did, moves it (to Review by
            default), and the run appears on the task with a summary. It never completes tasks unless you allow it.
          </li>
        </ol>
        <div class="fw-semibold mb-1">What the agent sees</div>
        <p class="mb-2">
          The task's title, description, status, priority, due date, tags, custom fields, its discussion, the note
          sent with the run, and your standing instructions. If the project has a linked GitHub repository (GitHub tab)
          or the task has a linked issue, those are included too. <strong>The agent knows nothing else</strong>, so
          the clearer the task, the better the result: say what you want back and how you'll judge it done.
        </p>
        <div class="fw-semibold mb-1">What it can actually do depends on where it runs</div>
        <ul class="mb-0 ps-3">
          <li>
            <strong>Any model</strong> (including a local one) can do text work: clarify and rewrite tasks, break
            them into subtasks, draft docs or emails, write checklists, review a plan.
          </li>
          <li>
            <strong>Reading code</strong> needs an agent with access to a checkout. GoTodo never reads your files.
            The bundled local agent can be given read-only access to one folder (<code>REPO_DIR</code>) to explain bugs
            and propose patches.
          </li>
          <li>
            <strong>Code changes</strong> need an agent that runs where the code is, such as Claude Code on a machine
            with the repository checked out, connected over MCP, or the bundled local agent with
            <code>REPO_WRITE=true</code> (edits on its own git branch, never commits). Linking a GitHub repository tells
            it which repo to use; it doesn't give the agent access by itself. How much an agent may do on that machine
            is up to whoever runs it; the guardrails here only cover GoTodo.
          </li>
        </ul>
      </details>
      <div v-if="!apiEnabled" class="alert alert-warning small py-2">
        The REST API is turned off for this site, so agents can't connect until an administrator enables it.
      </div>

      <p v-if="loading && !agents.length" class="small text-muted">Loading agents…</p>
      <ul v-else class="list-group mb-3">
        <li v-for="a in agents" :key="a.id" class="list-group-item d-flex flex-wrap align-items-center gap-2">
          <i class="bi bi-robot text-muted" aria-hidden="true" />
          <div class="flex-grow-1">
            <div class="fw-semibold">
              {{ a.name }} <span class="text-muted fw-normal">@{{ a.handle }}</span>
            </div>
            <div class="small text-muted">
              {{ a.role_name || a.role }}
              · {{ a.webhook_url ? 'Webhook' : 'Queue / MCP' }}
              · {{ a.trigger_on_mention ? '@mentions' : 'no mentions' }}<template v-if="a.trigger_status_ids.length">
                · {{ a.trigger_status_ids.length }} trigger column{{ a.trigger_status_ids.length === 1 ? '' : 's' }}</template>
              <template v-if="openRunCount(a.id)"> · {{ openRunCount(a.id) }} open run{{ openRunCount(a.id) === 1 ? '' : 's' }}</template>
            </div>
          </div>
          <span v-if="!a.enabled" class="badge text-bg-warning">Paused</span>
          <span v-if="a.last_delivery_error" class="badge text-bg-danger" :title="a.last_delivery_error">Delivery failing</span>
          <button type="button" class="btn btn-sm btn-outline-primary" @click="selectedId = a.id">Configure</button>
        </li>
        <li v-if="!agents.length && !loading" class="list-group-item text-muted">No AI agents on this project yet.</li>
      </ul>

      <button
        v-if="!showCreate"
        type="button"
        class="btn btn-sm btn-primary"
        :disabled="agents.length >= MAX_AGENTS || project.archived"
        @click="openCreate"
      >
        <i class="bi bi-plus-lg me-1" />Add AI agent
      </button>
      <p v-if="agents.length >= MAX_AGENTS" class="small text-muted mt-1 mb-0">A project can have up to {{ MAX_AGENTS }} agents.</p>

      <form v-if="showCreate" class="card card-body mb-3" @submit.prevent="createAgent">
        <h5 class="h6">New AI agent</h5>
        <fieldset class="mb-3">
          <legend class="form-label small mb-1">What should this agent do?</legend>
          <div class="list-group">
            <label
              v-for="r in AGENT_RECIPES"
              :key="r.id"
              class="list-group-item list-group-item-action d-flex gap-2"
              :class="{ active: recipeId === r.id }"
            >
              <input v-model="recipeId" class="form-check-input mt-1 flex-shrink-0" type="radio" name="agent-recipe" :value="r.id" />
              <span>
                <span class="fw-semibold d-block">{{ r.label }}</span>
                <span class="small d-block">{{ r.summary }}</span>
                <span class="small d-block opacity-75">Good for: {{ r.goodFor }}</span>
              </span>
            </label>
          </div>
          <div v-if="recipe?.runner === 'read'" class="alert alert-info small py-2 mt-2 mb-0">
            This setup needs an agent that can <strong>read</strong> your code. GoTodo never reads files on your
            machine; the agent does. With the bundled local agent, set <code>REPO_DIR</code> to your checkout
            (<code>examples/agents/ollama</code>): it gets read-only, sandboxed access to that folder, skips secrets like
            <code>.env</code>, and can't change anything. It explains and proposes a patch for a person to apply.
          </div>
          <div v-else-if="recipe?.runner === 'repo'" class="alert alert-info small py-2 mt-2 mb-0">
            This setup needs an agent that runs where your code is and can change it: Claude Code on a machine with the
            repository checked out (over MCP), or the bundled local agent (<code>examples/agents/ollama</code>) with
            <code>REPO_DIR</code> and <code>REPO_WRITE=true</code>. The local agent edits only on a new git branch,
            never touches main or secrets, and never commits or runs commands, so a person reviews the diff and commits.
            <template v-if="!repoLinked">Link the repository on the <strong>GitHub</strong> tab so the agent knows which one to use.</template>
          </div>
          <div v-else-if="recipe && recipe.reviewOnly" class="form-text">
            Starts with: may edit {{ (recipe.settings.editable_fields || []).join(', ') }}<template v-if="recipe.settings.can_create_tasks">, create subtasks</template>,
            moves cards to {{ reviewColumnName || 'any open column' }}, can't complete tasks. You can change all of this afterwards.
          </div>
        </fieldset>
        <div class="row g-2 mb-2">
          <div class="col-sm-6">
            <label class="form-label small mb-1" for="agent-new-name">Name</label>
            <input
              id="agent-new-name"
              v-model="newName"
              type="text"
              class="form-control form-control-sm"
              maxlength="80"
              placeholder="e.g. Release Bot"
              required
            />
          </div>
          <div class="col-sm-6">
            <label class="form-label small mb-1" for="agent-new-handle">Handle</label>
            <div class="input-group input-group-sm">
              <span class="input-group-text">@</span>
              <input
                id="agent-new-handle"
                v-model="newHandle"
                type="text"
                class="form-control"
                :class="{ 'is-invalid': !!newHandleError }"
                maxlength="32"
                placeholder="release_bot"
                required
                @input="handleTouched = true"
              />
            </div>
            <div v-if="newHandleError" class="small text-danger mt-1">{{ newHandleError }}</div>
            <div v-else class="form-text">Used to @mention the agent. Can't be changed later.</div>
          </div>
        </div>
        <div class="mb-2">
          <label class="form-label small mb-1" for="agent-new-role">Project role</label>
          <select id="agent-new-role" v-model="newRole" class="form-select form-select-sm">
            <option v-for="r in agentRoles" :key="r.slug" :value="r.slug">{{ r.name }}</option>
          </select>
          <div class="form-text">
            The role sets the agent's base permissions and status gates. Guardrails on the next screen narrow it further.
          </div>
        </div>
        <div class="d-flex gap-2">
          <button type="submit" class="btn btn-sm btn-primary" :disabled="!canCreate">Add agent</button>
          <button type="button" class="btn btn-sm btn-outline-secondary" @click="showCreate = false">Cancel</button>
        </div>
      </form>

      <template v-if="runs.length">
        <h5 class="h6 mt-4">Recent runs</h5>
        <ul class="list-group list-group-flush small">
          <li v-for="r in runs.slice(0, 10)" :key="r.id" class="list-group-item px-0">
            <span class="badge me-1" :class="runStatusBadge(r.status).cls">{{ runStatusBadge(r.status).label }}</span>
            <strong>{{ r.agent_name }}</strong> on
            <RouterLink :to="`/tasks/${r.task_id}`">{{ r.task_title || `Task #${r.task_id}` }}</RouterLink>
            <span class="text-muted"> · {{ triggerLabel(r.trigger) }}<template v-if="r.triggered_by"> by {{ r.triggered_by }}</template>
              · {{ formatTime(r.created_at) }}</span>
          </li>
        </ul>
      </template>
    </template>
  </div>
</template>
