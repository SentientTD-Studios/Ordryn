<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { api } from '@/api/client'
import type { TaskAttachment } from '@/api/types'
import { APIError } from '@/api/types'
import { useAuth } from '@/composables/useAuth'
import { useConfirm } from '@/composables/useConfirm'
import { useSite } from '@/composables/useSite'
import { useToast } from '@/composables/useToast'

const props = defineProps<{
  taskId: number
  /** The viewer may edit this task (tasks:edit). */
  canEdit: boolean
  /** The viewer manages the task's project, so may remove anyone's files. */
  canManage: boolean
}>()

const MAX_FILES = 20

const toast = useToast()
const { askConfirm } = useConfirm()
const { user } = useAuth()
const { siteInfo: site } = useSite()

const files = ref<TaskAttachment[]>([])
const loaded = ref(false)
const uploading = ref(false)
const dragOver = ref(false)
const input = ref<HTMLInputElement | null>(null)

const uploadsOn = computed(() => !!site.value?.attachments_enabled)
const maxBytes = computed(() => site.value?.attachment_max_bytes || 5 * 1024 * 1024)
const canUpload = computed(() => props.canEdit && uploadsOn.value && files.value.length < MAX_FILES)

function formatSize(n: number) {
  if (n >= 1024 * 1024) return `${(n / (1024 * 1024)).toFixed(n >= 10 * 1024 * 1024 ? 0 : 1)} MB`
  if (n >= 1024) return `${Math.round(n / 1024)} KB`
  return `${n} B`
}

function iconFor(a: TaskAttachment) {
  const t = a.content_type
  if (t.startsWith('image/')) return 'bi-file-earmark-image'
  if (t === 'application/pdf') return 'bi-file-earmark-pdf'
  if (t.startsWith('text/')) return 'bi-file-earmark-text'
  if (t.includes('zip')) return 'bi-file-earmark-zip'
  return 'bi-file-earmark'
}

async function load() {
  const id = props.taskId
  try {
    const list = await api.listTaskAttachments(id)
    if (id === props.taskId) files.value = list
  } catch {
    files.value = []
  } finally {
    loaded.value = true
  }
}

watch(
  () => props.taskId,
  () => {
    files.value = []
    loaded.value = false
    void load()
  },
  { immediate: true },
)

async function uploadFiles(list: FileList | File[]) {
  for (const file of Array.from(list)) {
    if (!canUpload.value) break
    if (file.size > maxBytes.value) {
      toast.push(`${file.name} is larger than the ${formatSize(maxBytes.value)} limit`, 'error')
      continue
    }
    uploading.value = true
    try {
      files.value = [...files.value, await api.uploadTaskAttachment(props.taskId, file)]
    } catch (err) {
      toast.push(err instanceof APIError ? err.message : `Could not upload ${file.name}`, 'error')
    } finally {
      uploading.value = false
    }
  }
}

function onPick(e: Event) {
  const el = e.target as HTMLInputElement
  if (el.files?.length) void uploadFiles(el.files)
  el.value = ''
}

function onDrop(e: DragEvent) {
  dragOver.value = false
  if (!canUpload.value || !e.dataTransfer?.files.length) return
  e.preventDefault()
  void uploadFiles(e.dataTransfer.files)
}

function canRemove(a: TaskAttachment) {
  return props.canEdit && (a.user_id === user.value?.id || props.canManage)
}

async function remove(a: TaskAttachment) {
  const ok = await askConfirm({
    title: 'Remove file?',
    message: `Remove “${a.file_name}” from this task? This can't be undone.`,
    confirmLabel: 'Remove',
    danger: true,
  })
  if (!ok) return
  try {
    await api.deleteTaskAttachment(props.taskId, a.id)
    files.value = files.value.filter((f) => f.id !== a.id)
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Could not remove the file', 'error')
  }
}
</script>

<template>
  <div
    v-if="files.length || canUpload"
    class="form-group mt-3 task-attachments"
    :class="{ 'is-drag-over': dragOver }"
    @dragover.prevent="dragOver = canUpload"
    @dragleave="dragOver = false"
    @drop="onDrop"
  >
    <div class="d-flex align-items-center justify-content-between gap-2 mb-1">
      <label class="mb-0">Files <span v-if="files.length" class="text-muted fw-normal">({{ files.length }})</span></label>
      <template v-if="canUpload">
        <button type="button" class="btn btn-sm btn-outline-secondary" :disabled="uploading" @click="input?.click()">
          <span v-if="uploading" class="spinner-border spinner-border-sm me-1" />
          <i v-else class="bi bi-paperclip me-1" />Attach
        </button>
        <input ref="input" type="file" class="d-none" multiple @change="onPick" />
      </template>
    </div>
    <ul v-if="files.length" class="list-unstyled small mb-1">
      <li v-for="a in files" :key="a.id" class="d-flex align-items-center gap-2 py-1 border-bottom">
        <i class="bi" :class="iconFor(a)" aria-hidden="true" />
        <a :href="api.taskAttachmentUrl(taskId, a.id)" target="_blank" rel="noopener" class="text-truncate flex-grow-1" :title="a.file_name">
          {{ a.file_name }}
        </a>
        <span class="text-muted text-nowrap">{{ formatSize(a.size_bytes) }}</span>
        <a :href="api.taskAttachmentUrl(taskId, a.id, true)" class="text-muted" :aria-label="`Download ${a.file_name}`" title="Download">
          <i class="bi bi-download" />
        </a>
        <button
          v-if="canRemove(a)"
          type="button"
          class="btn btn-link btn-sm text-danger p-0"
          :aria-label="`Remove ${a.file_name}`"
          title="Remove"
          @click="remove(a)"
        >
          <i class="bi bi-trash" />
        </button>
      </li>
    </ul>
    <div v-if="canUpload" class="form-hint mb-0">
      Drop files here or use Attach. Up to {{ formatSize(maxBytes) }} each, {{ MAX_FILES }} per task.
    </div>
    <div v-else-if="canEdit && loaded && files.length >= MAX_FILES" class="form-hint mb-0">This task has the maximum of {{ MAX_FILES }} files.</div>
  </div>
</template>

<style scoped>
.task-attachments.is-drag-over {
  outline: 2px dashed var(--bs-primary);
  outline-offset: 4px;
  border-radius: 0.375rem;
}
</style>
