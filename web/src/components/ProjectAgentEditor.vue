<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { RouterLink } from 'vue-router'
import { api } from '@/api/client'
import type {
  AgentEditableField,
  AgentRun,
  AgentTriggerBy,
  Project,
  ProjectAgent,
  ProjectAPIKey,
  ProjectMember,
  ProjectRoleDef,
  ProjectStatus,
} from '@/api/types'
import { APIError } from '@/api/types'
import { pathPrefix } from '@/base'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import { EXPIRY_PRESETS, expiresAtFromPreset, keyExpiryLabel, type ExpiryPreset } from '@/utils/projectApiKeys'
import {
  AGENT_FIELDS,
  apiRoot,
  claudeCodeMcpCommand,
  isOpenRun,
  mcpJsonConfig,
  mcpServerName,
  runStatusBadge,
  triggerLabel,
} from '@/utils/projectAgents'

const props = defineProps<{
  project: Project
  agent: ProjectAgent
  statuses: ProjectStatus[]
  roles: ProjectRoleDef[]
  /** Every project role, for picking who may call the agent. */
  callerRoles: ProjectRoleDef[]
  /** Human project members, for picking who may call the agent. */
  members: ProjectMember[]
  runs: AgentRun[]
  apiEnabled: boolean
}>()

const emit = defineEmits<{
  saved: [agent: ProjectAgent]
  removed: []
  'runs-changed': []
}>()

const toast = useToast()
const { askConfirm } = useConfirm()

type Form = {
  name: string
  description: string
  instructions: string
  role: string
  enabled: boolean
  webhook_url: string
  trigger_on_mention: boolean
  trigger_status_ids: number[]
  trigger_by: AgentTriggerBy
  trigger_role_slugs: string[]
  trigger_user_ids: number[]
  claim_on_dispatch: boolean
  allowed_status_ids: number[]
  editable_fields: AgentEditableField[]
  can_complete: boolean
  can_create_tasks: boolean
  can_comment: boolean
  max_runs_per_hour: number
}

function formFrom(a: ProjectAgent): Form {
  return {
    name: a.name,
    description: a.description,
    instructions: a.instructions,
    role: a.role,
    enabled: a.enabled,
    webhook_url: a.webhook_url,
    trigger_on_mention: a.trigger_on_mention,
    trigger_status_ids: [...a.trigger_status_ids],
    trigger_by: a.trigger_by,
    trigger_role_slugs: [...a.trigger_role_slugs],
    // Drop members who have since left so saving does not fail.
    trigger_user_ids: a.trigger_user_ids.filter((id) => props.members.some((m) => m.user_id === id)),
    claim_on_dispatch: a.claim_on_dispatch,
    allowed_status_ids: [...a.allowed_status_ids],
    editable_fields: [...a.editable_fields],
    can_complete: a.can_complete,
    can_create_tasks: a.can_create_tasks,
    can_comment: a.can_comment,
    max_runs_per_hour: a.max_runs_per_hour,
  }
}

const form = reactive<Form>(formFrom(props.agent))
const saving = ref(false)
const dirty = computed(() => JSON.stringify(form) !== JSON.stringify(formFrom(props.agent)))
const isKanban = computed(() => (props.project.workflow_mode || 'classic') === 'kanban')
const statusEditable = computed(() => form.editable_fields.includes('status'))
const doneStatuses = computed(() => props.statuses.filter((s) => s.is_done))
const callerSelectionValid = computed(
  () => form.trigger_by !== 'selected' || form.trigger_role_slugs.length > 0 || form.trigger_user_ids.length > 0,
)

watch(
  () => props.agent,
  (a) => Object.assign(form, formFrom(a)),
)

async function save() {
  if (!form.name.trim()) {
    toast.push('Name is required', 'error')
    return
  }
  saving.value = true
  try {
    const updated = await api.updateProjectAgent(props.project.id, props.agent.id, {
      ...form,
      name: form.name.trim(),
      webhook_url: form.webhook_url.trim(),
      max_runs_per_hour: Number(form.max_runs_per_hour) || 1,
    })
    toast.push('Agent saved', 'success')
    emit('saved', updated)
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Save failed', 'error')
  } finally {
    saving.value = false
  }
}

async function togglePaused() {
  const enabled = !props.agent.enabled
  try {
    const updated = await api.updateProjectAgent(props.project.id, props.agent.id, { enabled })
    toast.push(enabled ? `${updated.name} resumed` : `${updated.name} paused`, 'info')
    emit('saved', updated)
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Update failed', 'error')
  }
}

async function removeAgent() {
  const ok = await askConfirm({
    title: 'Remove AI agent?',
    message: `Remove “${props.agent.name}” (@${props.agent.handle})? Its keys stop working immediately, open runs are cancelled, and it leaves the project. Its past comments and activity stay.`,
    confirmLabel: 'Remove agent',
    danger: true,
  })
  if (!ok) return
  try {
    await api.removeProjectAgent(props.project.id, props.agent.id)
    toast.push('Agent removed', 'info')
    emit('removed')
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Remove failed', 'error')
  }
}

// --- Keys ---
const keys = ref<ProjectAPIKey[]>([])
const keyName = ref('')
const keyExpiry = ref<ExpiryPreset>('90')
const mintingKey = ref(false)
const minted = ref<{ name: string; key: string } | null>(null)

async function loadKeys() {
  try {
    keys.value = await api.listAgentKeys(props.project.id, props.agent.id)
  } catch {
    keys.value = []
  }
}

async function mintKey() {
  mintingKey.value = true
  try {
    const created = await api.createAgentKey(props.project.id, props.agent.id, {
      name: keyName.value.trim() || undefined,
      expires_at: expiresAtFromPreset(keyExpiry.value),
    })
    minted.value = { name: created.name, key: created.key }
    keyName.value = ''
    toast.push('Agent key created — copy it now', 'success')
    await loadKeys()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Could not create key', 'error')
  } finally {
    mintingKey.value = false
  }
}

async function revokeKey(key: ProjectAPIKey) {
  const ok = await askConfirm({
    title: 'Revoke agent key?',
    message: `Revoke “${key.name}”? Anything using it as ${props.agent.name} stops working.`,
    confirmLabel: 'Revoke',
    danger: true,
  })
  if (!ok) return
  try {
    await api.revokeAgentKey(props.project.id, props.agent.id, key.id)
    if (minted.value?.name === key.name) minted.value = null
    toast.push('Key revoked', 'info')
    await loadKeys()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Revoke failed', 'error')
  }
}

// --- Webhook ---
const rotatedSecret = ref('')
const testing = ref(false)

async function rotateSecret() {
  if (props.agent.webhook_secret_set) {
    const ok = await askConfirm({
      title: 'Rotate signing secret?',
      message: 'The old secret stops validating deliveries immediately.',
      confirmLabel: 'Rotate',
    })
    if (!ok) return
  }
  try {
    const res = await api.rotateAgentWebhookSecret(props.project.id, props.agent.id)
    rotatedSecret.value = res.secret
    emit('saved', { ...props.agent, webhook_secret_set: true })
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Could not rotate secret', 'error')
  }
}

async function testWebhook() {
  testing.value = true
  try {
    await api.testAgentWebhook(props.project.id, props.agent.id)
    toast.push('Test delivery accepted', 'success')
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Test delivery failed', 'error')
  } finally {
    testing.value = false
  }
}

// --- Connection snippets ---
const root = computed(() => apiRoot(window.location.origin, pathPrefix()))
const serverName = computed(() => mcpServerName(props.agent.handle))
const snippetKey = computed(() => minted.value?.key || '<AGENT_KEY>')
const cliCommand = computed(() => claudeCodeMcpCommand(root.value, serverName.value, snippetKey.value))
const jsonConfig = computed(() => mcpJsonConfig(root.value, serverName.value, snippetKey.value))

async function copy(text: string) {
  try {
    await navigator.clipboard.writeText(text)
    toast.push('Copied', 'success')
  } catch {
    toast.push('Copy failed — select the text and copy it manually', 'error')
  }
}

// --- Runs ---
async function cancelRun(run: AgentRun) {
  try {
    await api.cancelAgentRun(run.task_id, run.id)
    toast.push('Run cancelled', 'info')
    emit('runs-changed')
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Cancel failed', 'error')
  }
}

function formatTime(iso: string | null) {
  if (!iso) return ''
  try {
    return new Date(iso).toLocaleString()
  } catch {
    return iso
  }
}

watch(
  () => props.agent.id,
  () => {
    minted.value = null
    rotatedSecret.value = ''
    void loadKeys()
  },
  { immediate: true },
)
</script>

<template>
  <div class="agent-editor">
    <div class="d-flex flex-wrap align-items-center gap-2 mb-3">
      <i class="bi bi-robot fs-4 text-muted" aria-hidden="true" />
      <div class="flex-grow-1">
        <div class="fw-semibold">{{ agent.name }}</div>
        <div class="small text-muted">@{{ agent.handle }} · {{ agent.role_name || agent.role }}</div>
      </div>
      <span v-if="!agent.enabled" class="badge text-bg-warning">Paused</span>
      <button type="button" class="btn btn-sm btn-outline-secondary" @click="togglePaused">
        <i :class="agent.enabled ? 'bi bi-pause-fill' : 'bi bi-play-fill'" class="me-1" />
        {{ agent.enabled ? 'Pause' : 'Resume' }}
      </button>
      <button type="button" class="btn btn-sm btn-outline-danger" @click="removeAgent">Remove</button>
    </div>
    <div v-if="!agent.enabled" class="alert alert-warning small py-2">
      Paused: no new runs start and this agent's keys are refused until you resume it.
    </div>

    <form @submit.prevent="save">
      <!-- Profile -->
      <section class="mb-4">
        <h5 class="h6">Profile</h5>
        <div class="row g-2">
          <div class="col-sm-6">
            <label class="form-label small mb-1" :for="`agent-name-${agent.id}`">Name</label>
            <input :id="`agent-name-${agent.id}`" v-model="form.name" type="text" class="form-control form-control-sm" maxlength="80" required />
          </div>
          <div class="col-sm-6">
            <label class="form-label small mb-1" :for="`agent-role-${agent.id}`">Project role</label>
            <select :id="`agent-role-${agent.id}`" v-model="form.role" class="form-select form-select-sm">
              <option v-for="r in roles" :key="r.slug" :value="r.slug">{{ r.name }}</option>
            </select>
          </div>
          <div class="col-12">
            <label class="form-label small mb-1" :for="`agent-desc-${agent.id}`">Description</label>
            <input
              :id="`agent-desc-${agent.id}`"
              v-model="form.description"
              type="text"
              class="form-control form-control-sm"
              maxlength="500"
              placeholder="What this agent is for (shown to managers)"
            />
          </div>
        </div>
      </section>

      <!-- Instructions -->
      <section class="mb-4">
        <h5 class="h6">Standing instructions</h5>
        <p class="small text-muted mb-1">
          Sent with every run, alongside the task and any note from the person who sent it. Describe how to do the work,
          what "done" means, and when to stop and ask in a comment instead.
        </p>
        <textarea
          :id="`agent-instructions-${agent.id}`"
          v-model="form.instructions"
          class="form-control form-control-sm font-monospace"
          rows="6"
          maxlength="8000"
          placeholder="e.g. Fix the bug described in the task. Add a regression test. When tests pass, comment with a summary and move the card to Review. If the task is unclear, comment with your questions and finish the run as failed."
          aria-label="Standing instructions"
        />
        <div class="d-flex justify-content-end"><small class="text-muted">{{ form.instructions.length }}/8000</small></div>
      </section>

      <!-- Triggers -->
      <section class="mb-4">
        <h5 class="h6">Triggers</h5>
        <p class="small text-muted mb-2">How work reaches this agent. <strong>Send to agent</strong> on a task always works for the people below.</p>
        <div class="form-check">
          <input :id="`agent-mention-${agent.id}`" v-model="form.trigger_on_mention" class="form-check-input" type="checkbox" />
          <label class="form-check-label small" :for="`agent-mention-${agent.id}`">
            Start a run when someone <code>@{{ agent.handle }}</code> in a task comment (the comment becomes the run's note)
          </label>
        </div>
        <fieldset v-if="isKanban && statuses.length" class="mt-2">
          <legend class="form-label small mb-1">Start a run when a card moves into</legend>
          <div class="d-flex flex-wrap gap-3">
            <div v-for="s in statuses" :key="s.id" class="form-check">
              <input :id="`agent-trig-${agent.id}-${s.id}`" v-model="form.trigger_status_ids" class="form-check-input" type="checkbox" :value="s.id" />
              <label class="form-check-label small" :for="`agent-trig-${agent.id}-${s.id}`">{{ s.name }}</label>
            </div>
          </div>
          <div class="form-text">Tip: add an "AI queue" column on the Board tab and pick it here.</div>
        </fieldset>
        <fieldset class="mt-2">
          <legend class="form-label small mb-1">Who can send work to this agent</legend>
          <div class="form-check">
            <input :id="`agent-by-m-${agent.id}`" v-model="form.trigger_by" class="form-check-input" type="radio" value="managers" />
            <label class="form-check-label small" :for="`agent-by-m-${agent.id}`">Project managers only (recommended)</label>
          </div>
          <div class="form-check">
            <input :id="`agent-by-w-${agent.id}`" v-model="form.trigger_by" class="form-check-input" type="radio" value="writers" />
            <label class="form-check-label small" :for="`agent-by-w-${agent.id}`">Anyone who can edit tasks</label>
          </div>
          <div class="form-check">
            <input :id="`agent-by-s-${agent.id}`" v-model="form.trigger_by" class="form-check-input" type="radio" value="selected" />
            <label class="form-check-label small" :for="`agent-by-s-${agent.id}`">Only the roles and members I pick</label>
          </div>
          <div v-if="form.trigger_by === 'selected'" class="border rounded p-2 mt-1">
            <div class="small fw-semibold mb-1">Roles</div>
            <div class="d-flex flex-wrap gap-3 mb-2">
              <div v-for="r in callerRoles" :key="r.slug" class="form-check">
                <input :id="`agent-caller-role-${agent.id}-${r.slug}`" v-model="form.trigger_role_slugs" class="form-check-input" type="checkbox" :value="r.slug" />
                <label class="form-check-label small" :for="`agent-caller-role-${agent.id}-${r.slug}`">{{ r.name }}</label>
              </div>
            </div>
            <div class="small fw-semibold mb-1">Members</div>
            <div class="d-flex flex-wrap gap-3">
              <div v-for="m in members" :key="m.user_id" class="form-check">
                <input :id="`agent-caller-user-${agent.id}-${m.user_id}`" v-model="form.trigger_user_ids" class="form-check-input" type="checkbox" :value="m.user_id" />
                <label class="form-check-label small" :for="`agent-caller-user-${agent.id}-${m.user_id}`">
                  {{ m.user_name || m.email }} <span class="text-muted">({{ m.role_name || m.role }})</span>
                </label>
              </div>
            </div>
            <div v-if="!callerSelectionValid" class="small text-danger mt-1">Pick at least one role or member.</div>
            <div class="form-text">
              Someone may call the agent if their role <em>or</em> their account is checked. Managers are not
              included automatically, so check your own role or name if you want to call it.
            </div>
          </div>
          <div class="form-text">
            Mentions and column moves by anyone else are ignored and nothing happens. Agents can never trigger other agents.
          </div>
        </fieldset>
        <div v-if="isKanban" class="form-check mt-2">
          <input :id="`agent-claim-${agent.id}`" v-model="form.claim_on_dispatch" class="form-check-input" type="checkbox" />
          <label class="form-check-label small" :for="`agent-claim-${agent.id}`">
            Claim the card for the agent while it works (released when the run ends)
          </label>
        </div>
      </section>

      <!-- Guardrails -->
      <section class="mb-4">
        <h5 class="h6">Guardrails</h5>
        <p class="small text-muted mb-2">
          These apply on top of the agent's role and the board's status gates. Anything outside them is refused with an
          <code>agent_guardrail</code> error, which the agent sees.
        </p>
        <fieldset class="mb-2">
          <legend class="form-label small mb-1">Task fields the agent may change</legend>
          <div class="row row-cols-1 row-cols-sm-2 g-1">
            <div v-for="f in AGENT_FIELDS" :key="f.id" class="col">
              <div class="form-check">
                <input :id="`agent-field-${agent.id}-${f.id}`" v-model="form.editable_fields" class="form-check-input" type="checkbox" :value="f.id" />
                <label class="form-check-label small" :for="`agent-field-${agent.id}-${f.id}`" :title="f.help">{{ f.label }}</label>
              </div>
            </div>
          </div>
        </fieldset>
        <fieldset v-if="isKanban && statusEditable && statuses.length" class="mb-2">
          <legend class="form-label small mb-1">Columns the agent may move cards into</legend>
          <div class="d-flex flex-wrap gap-3">
            <div v-for="s in statuses" :key="s.id" class="form-check">
              <input :id="`agent-allow-${agent.id}-${s.id}`" v-model="form.allowed_status_ids" class="form-check-input" type="checkbox" :value="s.id" />
              <label class="form-check-label small" :for="`agent-allow-${agent.id}-${s.id}`">
                {{ s.name }}<span v-if="s.is_done" class="text-muted"> (done)</span>
              </label>
            </div>
          </div>
          <div class="form-text">
            None checked means any column except done ones.
            <template v-if="doneStatuses.length">Done columns also need "complete tasks" below.</template>
          </div>
        </fieldset>
        <div class="form-check">
          <input :id="`agent-complete-${agent.id}`" v-model="form.can_complete" class="form-check-input" type="checkbox" />
          <label class="form-check-label small" :for="`agent-complete-${agent.id}`">
            May complete or reopen tasks (off keeps a human sign-off step)
          </label>
        </div>
        <div class="form-check">
          <input :id="`agent-create-${agent.id}`" v-model="form.can_create_tasks" class="form-check-input" type="checkbox" />
          <label class="form-check-label small" :for="`agent-create-${agent.id}`">May create tasks and subtasks</label>
        </div>
        <div class="form-check">
          <input :id="`agent-comment-${agent.id}`" v-model="form.can_comment" class="form-check-input" type="checkbox" />
          <label class="form-check-label small" :for="`agent-comment-${agent.id}`">May post comments (recommended, so it can report back)</label>
        </div>
        <div class="mt-2" style="max-width: 14rem;">
          <label class="form-label small mb-1" :for="`agent-rate-${agent.id}`">Max runs per hour</label>
          <input :id="`agent-rate-${agent.id}`" v-model.number="form.max_runs_per_hour" type="number" min="1" max="500" class="form-control form-control-sm" />
        </div>
      </section>

      <!-- Webhook -->
      <section class="mb-3">
        <h5 class="h6">Webhook (optional)</h5>
        <p class="small text-muted mb-1">
          GoTodo POSTs each new run to this HTTPS URL as JSON, signed with <code>X-Ordryn-Signature: sha256=…</code>.
          Leave it empty if the agent polls its queue or connects over MCP.
        </p>
        <input
          :id="`agent-webhook-${agent.id}`"
          v-model="form.webhook_url"
          type="url"
          class="form-control form-control-sm"
          placeholder="https://agents.example.com/gotodo"
          aria-label="Webhook URL"
        />
      </section>

      <div class="d-flex align-items-center gap-2 mb-4">
        <button type="submit" class="btn btn-sm btn-primary" :disabled="saving || !dirty || !callerSelectionValid">Save agent</button>
        <span v-if="dirty" class="small text-muted">Unsaved changes</span>
      </div>
    </form>

    <section v-if="agent.webhook_url" class="mb-4">
      <div class="d-flex flex-wrap gap-2 align-items-center mb-1">
        <button type="button" class="btn btn-sm btn-outline-secondary" @click="rotateSecret">
          {{ agent.webhook_secret_set ? 'Rotate signing secret' : 'Create signing secret' }}
        </button>
        <button type="button" class="btn btn-sm btn-outline-secondary" :disabled="testing" @click="testWebhook">Send test delivery</button>
        <span class="small text-muted">
          <template v-if="agent.last_delivery_at">
            Last delivery {{ formatTime(agent.last_delivery_at) }} —
            <span :class="agent.last_delivery_error ? 'text-danger' : 'text-success'">
              {{ agent.last_delivery_error || 'OK' }}
            </span>
          </template>
          <template v-else>No deliveries yet</template>
        </span>
      </div>
      <div v-if="rotatedSecret" class="alert alert-success small mb-0">
        <p class="fw-semibold mb-1">Signing secret — shown once</p>
        <div class="d-flex gap-2 align-items-center">
          <code class="text-break flex-grow-1">{{ rotatedSecret }}</code>
          <button type="button" class="btn btn-sm btn-outline-secondary" @click="copy(rotatedSecret)">Copy</button>
        </div>
      </div>
    </section>

    <!-- Keys & MCP -->
    <section class="mb-4">
      <h5 class="h6">Connect the agent</h5>
      <div v-if="!apiEnabled" class="alert alert-warning small py-2">
        The REST API is turned off for this site, so agent keys won't work until an administrator enables it.
      </div>
      <p class="small text-muted mb-2">
        Create a key the agent uses to act as <strong>@{{ agent.handle }}</strong>. It can read this project, work its queue,
        comment, and edit tasks within the guardrails above, and nothing else.
      </p>
      <form class="row g-2 align-items-end mb-2" @submit.prevent="mintKey">
        <div class="col-sm-6">
          <label class="form-label small mb-1" :for="`agent-key-name-${agent.id}`">Key name</label>
          <input :id="`agent-key-name-${agent.id}`" v-model="keyName" type="text" class="form-control form-control-sm" maxlength="80" :placeholder="`${agent.handle} key`" />
        </div>
        <div class="col-sm-3">
          <label class="form-label small mb-1" :for="`agent-key-exp-${agent.id}`">Expires</label>
          <select :id="`agent-key-exp-${agent.id}`" v-model="keyExpiry" class="form-select form-select-sm">
            <option v-for="p in EXPIRY_PRESETS" :key="p.id" :value="p.id">{{ p.label }}</option>
          </select>
        </div>
        <div class="col-sm-3">
          <button type="submit" class="btn btn-sm btn-primary w-100" :disabled="mintingKey">Create key</button>
        </div>
      </form>
      <div v-if="minted" class="alert alert-success small">
        <p class="fw-semibold mb-1">New key “{{ minted.name }}” — shown once</p>
        <div class="d-flex gap-2 align-items-center">
          <code class="text-break flex-grow-1">{{ minted.key }}</code>
          <button type="button" class="btn btn-sm btn-outline-secondary" @click="copy(minted.key)">Copy</button>
        </div>
      </div>
      <ul class="list-group mb-3">
        <li v-for="k in keys" :key="k.id" class="list-group-item d-flex flex-wrap align-items-center gap-2 small">
          <span class="fw-semibold">{{ k.name }}</span>
          <code>{{ k.key_prefix }}</code>
          <span v-if="k.expired" class="badge text-bg-danger">Expired</span>
          <span class="text-muted flex-grow-1">
            Last used {{ k.last_used_at ? formatTime(k.last_used_at) : 'never' }} · {{ keyExpiryLabel(k.expires_at) }}
          </span>
          <button type="button" class="btn btn-sm btn-outline-danger" @click="revokeKey(k)">Revoke</button>
        </li>
        <li v-if="!keys.length" class="list-group-item small text-muted">No keys yet.</li>
      </ul>

      <h6 class="small fw-semibold mb-1">Claude Code and other MCP clients</h6>
      <p class="small text-muted mb-1">
        Register this project as an MCP server; the agent gets tools to read its queue, read tasks, comment, and move cards.
      </p>
      <div class="d-flex gap-2 align-items-start mb-2">
        <pre class="small bg-body-tertiary p-2 rounded mb-0 flex-grow-1 text-wrap text-break"><code>{{ cliCommand }}</code></pre>
        <button type="button" class="btn btn-sm btn-outline-secondary" @click="copy(cliCommand)">Copy</button>
      </div>
      <details class="small">
        <summary>JSON config (<code>.mcp.json</code> and other clients)</summary>
        <div class="d-flex gap-2 align-items-start mt-1">
          <pre class="small bg-body-tertiary p-2 rounded mb-0 flex-grow-1"><code>{{ jsonConfig }}</code></pre>
          <button type="button" class="btn btn-sm btn-outline-secondary" @click="copy(jsonConfig)">Copy</button>
        </div>
      </details>
      <p class="small text-muted mt-2 mb-0">
        Prefer plain HTTP? Poll <code>GET /api/v2/agent/runs</code>, then <code>POST /api/v2/agent/runs/{id}/start</code>
        and <code>…/finish</code>. See the <RouterLink to="/docs/api/v2#ai-agents">API docs</RouterLink>.
      </p>
    </section>

    <!-- Runs -->
    <section>
      <h5 class="h6">Recent runs</h5>
      <ul v-if="runs.length" class="list-group list-group-flush small">
        <li v-for="r in runs" :key="r.id" class="list-group-item px-0">
          <div class="d-flex flex-wrap align-items-center gap-2">
            <span class="badge" :class="runStatusBadge(r.status).cls">{{ runStatusBadge(r.status).label }}</span>
            <RouterLink :to="`/tasks/${r.task_id}`">{{ r.task_title || `Task #${r.task_id}` }}</RouterLink>
            <span class="text-muted flex-grow-1">
              {{ triggerLabel(r.trigger) }}<template v-if="r.triggered_by"> by {{ r.triggered_by }}</template> · {{ formatTime(r.created_at) }}
            </span>
            <span v-if="agent.webhook_url && r.delivery_error" class="badge text-bg-danger" :title="r.delivery_error">Not delivered</span>
            <button v-if="isOpenRun(r.status)" type="button" class="btn btn-sm btn-outline-secondary py-0" @click="cancelRun(r)">Cancel</button>
          </div>
          <div v-if="r.summary" class="text-muted mt-1">{{ r.summary }}</div>
        </li>
      </ul>
      <p v-else class="small text-muted mb-0">No runs yet.</p>
    </section>
  </div>
</template>
