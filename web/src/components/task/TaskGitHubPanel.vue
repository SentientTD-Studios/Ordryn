<script setup lang="ts">
import { ref, watch } from 'vue'
import { api } from '@/api/client'
import type { TaskGitHubIssue } from '@/api/types'
import { APIError } from '@/api/types'
import { useConfirm } from '@/composables/useConfirm'
import { useToast } from '@/composables/useToast'

const props = defineProps<{
  taskId: number
  /** The task's project; the panel stays hidden unless it is linked to a GitHub repository. */
  projectId: number | null
  /** Linked issue from the task row. */
  initialIssue: TaskGitHubIssue | null
  readOnly: boolean
}>()

const toast = useToast()
const { askConfirm } = useConfirm()

const projectLinked = ref(false)
const issue = ref<TaskGitHubIssue | null>(props.initialIssue)
const issueRef = ref('')
const busy = ref(false)

watch(
  () => props.projectId,
  async (pid) => {
    projectLinked.value = false
    if (!pid) return
    try {
      const link = await api.getProjectGitHub(pid)
      if (props.projectId === pid) projectLinked.value = !!link.linked
    } catch {
      projectLinked.value = false
    }
  },
  { immediate: true },
)

watch(
  () => [props.taskId, props.initialIssue] as const,
  () => {
    issue.value = props.initialIssue
    issueRef.value = ''
  },
)

async function run(action: () => Promise<void>, failure: string) {
  busy.value = true
  try {
    await action()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : failure, 'error')
  } finally {
    busy.value = false
  }
}

function createIssue() {
  if (props.readOnly) return
  return run(async () => {
    issue.value = await api.createTaskGitHubIssue(props.taskId)
    toast.push(`Created GitHub issue #${issue.value.issue_number}`, 'success')
  }, 'Could not create GitHub issue')
}

function linkIssue() {
  const raw = issueRef.value.trim()
  if (props.readOnly || !raw) return
  return run(async () => {
    issue.value = await api.linkTaskGitHubIssue(props.taskId, raw)
    issueRef.value = ''
    toast.push(`Linked GitHub issue #${issue.value.issue_number}`, 'success')
  }, 'Could not link GitHub issue')
}

async function unlinkIssue() {
  if (props.readOnly) return
  const ok = await askConfirm({
    title: 'Unlink GitHub issue?',
    message: 'The GitHub issue itself is not deleted.',
    confirmLabel: 'Unlink',
    danger: true,
  })
  if (!ok) return
  await run(async () => {
    await api.unlinkTaskGitHubIssue(props.taskId)
    issue.value = null
    toast.push('GitHub issue unlinked', 'info')
  }, 'Could not unlink GitHub issue')
}
</script>

<template>
  <div v-if="projectLinked" class="form-group mt-3">
    <label class="d-block">GitHub issue</label>
    <div v-if="issue" class="d-flex flex-column gap-2">
      <div class="d-flex flex-wrap align-items-center gap-2">
        <a :href="issue.issue_url" target="_blank" rel="noopener noreferrer" class="fw-semibold">
          #{{ issue.issue_number }}
          <span v-if="issue.issue_title"> — {{ issue.issue_title }}</span>
        </a>
        <span class="badge" :class="issue.issue_state === 'closed' ? 'text-bg-secondary' : 'text-bg-success'">
          {{ issue.issue_state }}
        </span>
      </div>
      <p v-if="issue.last_sync_error" class="text-danger small mb-0">Sync error: {{ issue.last_sync_error }}</p>
      <button
        v-if="!readOnly"
        type="button"
        class="btn btn-sm btn-outline-secondary align-self-start"
        :disabled="busy"
        @click="unlinkIssue"
      >
        Unlink
      </button>
    </div>
    <div v-else-if="!readOnly" class="d-flex flex-column gap-2">
      <button type="button" class="btn btn-sm btn-outline-primary align-self-start" :disabled="busy" @click="createIssue">
        {{ busy ? 'Working…' : 'Create GitHub issue' }}
      </button>
      <div class="input-group input-group-sm">
        <input v-model="issueRef" type="text" class="form-control" placeholder="Issue # or URL" :disabled="busy" />
        <button
          type="button"
          class="btn btn-outline-secondary"
          :disabled="busy || !issueRef.trim()"
          @click="linkIssue"
        >
          Link existing
        </button>
      </div>
      <div class="form-hint mb-0">
        Creates or links an issue in the project’s GitHub repository. Does not import issues as tasks.
      </div>
    </div>
    <p v-else class="text-muted small mb-0">No linked GitHub issue.</p>
  </div>
</template>
