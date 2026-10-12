<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { api } from '@/api/client'
import type { TaskTimeEntry } from '@/api/types'
import { APIError } from '@/api/types'
import { useConfirm } from '@/composables/useConfirm'
import { useToast } from '@/composables/useToast'

const props = defineProps<{
  taskId: number
  /** Logged total from the task row, shown until the entries load. */
  initialMinutes: number
  canLog: boolean
}>()

const toast = useToast()
const { askConfirm } = useConfirm()

const entries = ref<TaskTimeEntry[]>([])
const totalMinutes = ref(props.initialMinutes)
const newMinutes = ref<number | ''>('')
const newNote = ref('')
const adding = ref(false)

function formatMinutes(total: number) {
  if (!total) return '0m'
  const h = Math.floor(total / 60)
  const m = total % 60
  if (h && m) return `${h}h ${m}m`
  if (h) return `${h}h`
  return `${m}m`
}

const totalLabel = computed(() => formatMinutes(totalMinutes.value))

async function load() {
  const id = props.taskId
  try {
    const list = await api.listTimeEntries(id)
    if (id !== props.taskId) return
    entries.value = list
    totalMinutes.value = list.reduce((sum, e) => sum + e.minutes, 0)
  } catch {
    entries.value = []
  }
}

watch(
  () => props.taskId,
  () => {
    entries.value = []
    totalMinutes.value = props.initialMinutes
    newMinutes.value = ''
    newNote.value = ''
    void load()
  },
  { immediate: true },
)

async function add() {
  if (!props.canLog) return
  const minutes = Number(newMinutes.value)
  if (!Number.isFinite(minutes) || minutes <= 0) {
    toast.push('Enter minutes greater than 0', 'error')
    return
  }
  adding.value = true
  try {
    await api.addTimeEntry(props.taskId, minutes, newNote.value.trim())
    newMinutes.value = ''
    newNote.value = ''
    toast.push('Time logged', 'success')
    await load()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Could not add time entry', 'error')
  } finally {
    adding.value = false
  }
}

async function remove(entryId: number) {
  if (!props.canLog) return
  const ok = await askConfirm({
    title: 'Delete time entry?',
    message: 'Remove this time entry?',
    confirmLabel: 'Delete',
    danger: true,
  })
  if (!ok) return
  try {
    await api.deleteTimeEntry(props.taskId, entryId)
    toast.push('Time entry deleted', 'info')
    await load()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Could not delete time entry', 'error')
  }
}
</script>

<template>
  <div>
    <label class="d-block">Time tracking</label>
    <p class="small text-muted mb-2">
      Total logged: <strong>{{ totalLabel }}</strong>
      <span v-if="totalMinutes">({{ totalMinutes }} min)</span>
    </p>
    <form v-if="canLog" class="row g-2 align-items-end mb-2" @submit.prevent="add">
      <div class="col-4">
        <label class="form-label small mb-0" for="time_minutes">Minutes</label>
        <input
          id="time_minutes"
          v-model="newMinutes"
          type="number"
          class="form-control form-control-sm"
          min="1"
          max="1440"
          required
        />
      </div>
      <div class="col-5">
        <label class="form-label small mb-0" for="time_note">Note</label>
        <input
          id="time_note"
          v-model="newNote"
          type="text"
          class="form-control form-control-sm"
          maxlength="200"
          placeholder="Optional"
        />
      </div>
      <div class="col-3">
        <button class="btn btn-sm btn-outline-primary w-100" type="submit" :disabled="adding">Add</button>
      </div>
    </form>
    <ul v-if="entries.length" class="list-unstyled small mb-0">
      <li v-for="entry in entries" :key="entry.id" class="d-flex justify-content-between align-items-start gap-2 mb-1">
        <span>
          <strong>{{ entry.minutes }}m</strong>
          <span v-if="entry.note"> — {{ entry.note }}</span>
          <span class="text-muted"> · {{ entry.user_name || entry.user_email || 'user' }}</span>
        </span>
        <button v-if="canLog" class="btn btn-sm btn-link text-danger p-0" type="button" @click="remove(entry.id)">
          Delete
        </button>
      </li>
    </ul>
    <p v-else class="small text-muted mb-0">No time entries yet.</p>
  </div>
</template>
