<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { api } from '@/api/client'
import { APIError, type Project, type Tag, type Task } from '@/api/types'
import { useToast } from '@/composables/useToast'
import { hasProjectPerm, PROJECT_PERMS } from '@/utils/projectPerms'
import { parseQuickAdd } from '@/utils/quickAdd'
import QuickAddChips from '@/components/QuickAddChips.vue'

const props = defineProps<{
  projects: Project[]
  /** Project new tasks go to unless the text names one with +project; null = personal. */
  defaultProjectId: number | null
  /** Sprint to place tasks in when they land in the default project. */
  sprintId?: number | null
}>()

const emit = defineEmits<{ created: [task: Task] }>()

const toast = useToast()
const text = ref('')
const busy = ref(false)
const focused = ref(false)
const inputEl = ref<HTMLInputElement | null>(null)
const tagCache = ref(new Map<string, Tag[]>())

const writableProjects = computed(() =>
  props.projects.filter((p) => !p.archived && hasProjectPerm(p, PROJECT_PERMS.TASKS_CREATE)),
)

const targetProjectId = computed<number | null>(() => {
  const first = parseQuickAdd(text.value, { projects: writableProjects.value })
  return first.projectId ?? props.defaultProjectId
})
const targetProject = computed(() =>
  targetProjectId.value == null ? null : props.projects.find((p) => p.id === targetProjectId.value) ?? null,
)
const tagKey = computed(() => String(targetProjectId.value ?? 0))

const parsed = computed(() => {
  const project = targetProject.value
  return parseQuickAdd(text.value, {
    projects: writableProjects.value,
    tags: tagCache.value.get(tagKey.value) ?? [],
    allowClaim:
      project?.workflow_mode === 'kanban' && hasProjectPerm(project, PROJECT_PERMS.TASKS_CLAIM),
  })
})

async function loadTags(key: string): Promise<void> {
  if (tagCache.value.has(key)) return
  try {
    const list = await api.listTags({ project_id: Number(key) })
    const next = new Map(tagCache.value)
    next.set(key, list.filter((t) => !t.protected))
    tagCache.value = next
  } catch {
    // Tags are optional; "#name" just stays in the title.
  }
}

watch(
  [tagKey, () => text.value.includes('#')],
  ([key, wantsTags]) => {
    if (wantsTags) void loadTags(key)
  },
)

async function submit() {
  if (busy.value || !text.value.trim()) return
  busy.value = true
  try {
    if (text.value.includes('#')) await loadTags(tagKey.value)
    const p = parsed.value
    const projectId = targetProjectId.value
    const canSprint = projectId != null && hasProjectPerm(targetProject.value, PROJECT_PERMS.TASKS_SPRINT)
    let task = await api.createTask({
      title: p.title,
      project_id: projectId,
      due_date: p.dueDate,
      priority: p.priority,
      tag_ids: p.tagIds.length ? p.tagIds : undefined,
      sprint_id: projectId === props.defaultProjectId && props.sprintId && canSprint ? props.sprintId : undefined,
    })
    if (p.claim) {
      try {
        task = await api.claimTask(task.id)
      } catch (err) {
        toast.push(err instanceof APIError ? `Added, but could not claim: ${err.message}` : 'Added, but could not claim', 'info')
      }
    }
    text.value = ''
    emit('created', task)
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Could not add task', 'error')
  } finally {
    busy.value = false
    inputEl.value?.focus()
  }
}

function clear() {
  text.value = ''
  inputEl.value?.blur()
}

defineExpose({ focus: () => inputEl.value?.focus() })
</script>

<template>
  <form class="quick-add-bar mb-3" @submit.prevent="submit">
    <div class="input-group input-group-sm">
      <span class="input-group-text"><i class="bi bi-plus-lg" aria-hidden="true" /></span>
      <input
        ref="inputEl"
        v-model="text"
        type="text"
        class="form-control"
        maxlength="500"
        placeholder="Quick add: Fix login #bug !high tomorrow"
        aria-label="Quick add task"
        aria-describedby="quick-add-help"
        :disabled="busy"
        @focus="focused = true"
        @blur="focused = false"
        @keydown.esc.prevent="clear"
      />
      <button type="submit" class="btn btn-success" :disabled="busy || !text.trim()">
        <span v-if="busy" class="spinner-border spinner-border-sm" aria-hidden="true" />
        <span v-else>Add</span>
      </button>
    </div>
    <div class="d-flex flex-wrap align-items-center gap-2 mt-1 small text-muted">
      <QuickAddChips :chips="parsed.chips" />
      <span v-if="text.trim() && parsed.title !== text.trim()" class="text-truncate">
        Title: <strong>{{ parsed.title }}</strong>
      </span>
      <span v-else-if="focused && !text.trim()" id="quick-add-help">
        <code>#tag</code> <code>!low</code>/<code>!med</code>/<code>!high</code> <code>+project</code>
        <code>@me</code> and dates like <code>tomorrow</code>, <code>friday</code>, <code>next week</code>,
        <code>in 3 days</code>, <code>oct 20</code>
      </span>
    </div>
  </form>
</template>
