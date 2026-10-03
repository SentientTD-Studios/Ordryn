<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { api } from '@/api/client'
import type { AgentRun, TaskAgentRuns } from '@/api/types'
import { APIError } from '@/api/types'
import { useToast } from '@/composables/useToast'
import { useLiveUpdates, type LiveEvent } from '@/composables/useLiveUpdates'
import { isOpenRun, runStatusBadge, triggerLabel } from '@/utils/projectAgents'
import { batchTouchesTask } from '@/utils/liveEvents'

const props = defineProps<{ taskId: number }>()
const toast = useToast()

const data = ref<TaskAgentRuns>({ runs: [], agents: [] })
const loaded = ref(false)
const agentId = ref(0)
const note = ref('')
const sending = ref(false)
const showForm = ref(false)

const openRuns = computed(() => data.value.runs.filter((r) => isOpenRun(r.status)))
const busyAgentIds = computed(() => new Set(openRuns.value.map((r) => r.agent_id)))
const visible = computed(() => loaded.value && (data.value.runs.length > 0 || data.value.agents.length > 0))

async function load() {
  if (!props.taskId) return
  try {
    data.value = await api.getTaskAgentRuns(props.taskId)
    if (!data.value.agents.some((a) => a.id === agentId.value)) {
      agentId.value = data.value.agents[0]?.id ?? 0
    }
  } catch {
    data.value = { runs: [], agents: [] }
  } finally {
    loaded.value = true
  }
}

watch(
  () => props.taskId,
  () => {
    loaded.value = false
    showForm.value = false
    note.value = ''
    void load()
  },
  { immediate: true },
)

useLiveUpdates(async (_last: LiveEvent, batch: LiveEvent[]) => {
  if (batchTouchesTask(batch, props.taskId)) await load()
})

async function send() {
  if (!agentId.value) return
  sending.value = true
  try {
    const run = await api.sendTaskToAgent(props.taskId, agentId.value, note.value.trim())
    toast.push(`Sent to ${run.agent_name}`, 'success')
    note.value = ''
    showForm.value = false
    await load()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Could not send to agent', 'error')
  } finally {
    sending.value = false
  }
}

async function cancel(run: AgentRun) {
  try {
    await api.cancelAgentRun(props.taskId, run.id)
    toast.push('Run cancelled', 'info')
    await load()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Cancel failed', 'error')
  }
}

function formatTime(iso: string) {
  try {
    return new Date(iso).toLocaleString()
  } catch {
    return iso
  }
}
</script>

<template>
  <div v-if="visible" class="task-agents-panel">
    <label class="d-flex align-items-center gap-2">
      <span><i class="bi bi-robot me-1" />AI agents</span>
      <button
        v-if="data.agents.length && !showForm"
        type="button"
        class="btn btn-sm btn-outline-primary py-0 ms-auto"
        @click="showForm = true"
      >
        Send to agent
      </button>
    </label>

    <form v-if="showForm" class="mb-2" @submit.prevent="send">
      <div class="d-flex gap-1 mb-1">
        <select v-model.number="agentId" class="form-select form-select-sm" aria-label="Agent">
          <option v-for="a in data.agents" :key="a.id" :value="a.id" :disabled="busyAgentIds.has(a.id)">
            {{ a.name }} (@{{ a.handle }}){{ busyAgentIds.has(a.id) ? ' — working on it' : '' }}
          </option>
        </select>
      </div>
      <textarea
        v-model="note"
        class="form-control form-control-sm mb-1"
        rows="3"
        maxlength="2000"
        placeholder="What you want back and how you'll judge it done, e.g. &quot;Draft the onboarding checklist as a comment; done when it covers accounts, laptop, and first-week meetings.&quot;"
        aria-label="Note for the agent"
      />
      <div class="form-text mb-1">
        The agent sees this task's title, description, and discussion plus this note, and nothing else.
      </div>
      <div class="d-flex gap-1">
        <button type="submit" class="btn btn-sm btn-primary" :disabled="sending || !agentId || busyAgentIds.has(agentId)">Send</button>
        <button type="button" class="btn btn-sm btn-outline-secondary" @click="showForm = false">Cancel</button>
      </div>
    </form>

    <ul v-if="data.runs.length" class="list-unstyled small mb-0">
      <li v-for="r in data.runs.slice(0, 5)" :key="r.id" class="mb-1">
        <div class="d-flex flex-wrap align-items-center gap-1">
          <span class="badge" :class="runStatusBadge(r.status).cls">{{ runStatusBadge(r.status).label }}</span>
          <strong>{{ r.agent_name }}</strong>
          <span class="text-muted">· {{ triggerLabel(r.trigger) }}<template v-if="r.triggered_by"> by {{ r.triggered_by }}</template> · {{ formatTime(r.created_at) }}</span>
          <button
            v-if="isOpenRun(r.status)"
            type="button"
            class="btn btn-link btn-sm text-danger p-0 ms-auto"
            @click="cancel(r)"
          >
            Cancel
          </button>
        </div>
        <div v-if="r.summary" class="text-muted">{{ r.summary }}</div>
      </li>
    </ul>
  </div>
</template>
