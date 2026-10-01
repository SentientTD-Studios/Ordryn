<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { api } from '@/api/client'
import type { TaskLink, TaskLinkType } from '@/api/types'
import { APIError } from '@/api/types'
import { useToast } from '@/composables/useToast'
import { LINK_TYPE_LABELS, LINK_TYPE_ORDER, groupTaskLinks, parseTaskRef } from '@/utils/taskLinks'

const props = defineProps<{ taskId: number; canEdit: boolean }>()
const emit = defineEmits<{ open: [taskId: number]; changed: [] }>()
const toast = useToast()

const links = ref<TaskLink[]>([])
const loaded = ref(false)
const newType = ref<TaskLinkType>('blocked_by')
const newRef = ref('')
const busy = ref(false)

const groups = computed(() => groupTaskLinks(links.value))
const openBlockers = computed(() => links.value.filter((l) => l.type === 'blocked_by' && !l.completed).length)

async function load() {
  loaded.value = false
  links.value = []
  if (!props.taskId) return
  try {
    links.value = await api.listTaskLinks(props.taskId)
  } catch {
    links.value = []
  } finally {
    loaded.value = true
  }
}

async function add() {
  const other = parseTaskRef(newRef.value)
  if (!other) {
    toast.push('Enter a task number, e.g. #123', 'error')
    return
  }
  busy.value = true
  try {
    await api.addTaskLink(props.taskId, other, newType.value)
    newRef.value = ''
    await load()
    emit('changed')
  } catch (err) {
    toast.push(err instanceof APIError || err instanceof Error ? err.message : 'Could not add link', 'error')
  } finally {
    busy.value = false
  }
}

async function remove(link: TaskLink) {
  busy.value = true
  try {
    await api.removeTaskLink(props.taskId, link.link_id)
    await load()
    emit('changed')
  } catch (err) {
    toast.push(err instanceof Error ? err.message : 'Could not remove link', 'error')
  } finally {
    busy.value = false
  }
}

watch(() => props.taskId, load, { immediate: true })
defineExpose({ reload: load })
</script>

<template>
  <div v-if="loaded && (links.length || canEdit)" class="task-links-panel">
    <label class="d-block">
      Linked tasks
      <span v-if="openBlockers" class="badge text-bg-warning ms-1" :title="`${openBlockers} open blocker(s)`">
        <i class="bi bi-lock-fill" /> Blocked
      </span>
    </label>
    <div v-for="type in LINK_TYPE_ORDER" :key="type">
      <template v-if="groups[type].length">
        <span class="form-hint d-block">{{ LINK_TYPE_LABELS[type] }}</span>
        <ul class="list-unstyled mb-1">
          <li v-for="l in groups[type]" :key="l.link_id" class="d-flex align-items-center gap-1">
            <i class="bi" :class="l.completed ? 'bi-check-circle text-success' : 'bi-circle text-muted'" />
            <button
              type="button"
              class="btn btn-link text-start text-decoration-none p-0"
              :class="{ 'text-decoration-line-through text-muted': l.completed }"
              @click="emit('open', l.task_id)"
            >
              #{{ l.task_id }} {{ l.title }}
            </button>
            <button
              v-if="canEdit"
              type="button"
              class="btn btn-link btn-sm text-danger p-0 ms-auto"
              :aria-label="`Remove link to task ${l.task_id}`"
              :disabled="busy"
              @click="remove(l)"
            >
              <i class="bi bi-x-lg" />
            </button>
          </li>
        </ul>
      </template>
    </div>
    <form v-if="canEdit" class="d-flex gap-1 mt-1" @submit.prevent="add">
      <select v-model="newType" class="form-select form-select-sm w-auto" aria-label="Link type">
        <option v-for="type in LINK_TYPE_ORDER" :key="type" :value="type">{{ LINK_TYPE_LABELS[type] }}</option>
      </select>
      <input
        v-model="newRef"
        type="text"
        inputmode="numeric"
        class="form-control form-control-sm"
        placeholder="#task"
        aria-label="Task number to link"
        style="max-width: 7rem"
      />
      <button type="submit" class="btn btn-sm btn-outline-primary" :disabled="busy || !newRef.trim()">Link</button>
    </form>
  </div>
</template>
