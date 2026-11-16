<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { RouterLink } from 'vue-router'
import { api } from '@/api/client'
import type { TaskTimeEntry, TimeBlock } from '@/api/types'
import { APIError } from '@/api/types'
import { useAuth } from '@/composables/useAuth'
import { useConfirm } from '@/composables/useConfirm'
import { useToast } from '@/composables/useToast'
import {
  BLOCK_STEP_MINUTES,
  formatClock,
  formatDuration,
  zonedDay,
  zonedMinutes,
  zonedToInstant,
} from '@/utils/timeBlocks'

const props = defineProps<{
  taskId: number
  /** Logged total from the task row, shown until the entries load. */
  initialMinutes: number
  canLog: boolean
}>()

const toast = useToast()
const { askConfirm } = useConfirm()
const { user } = useAuth()

const tz = computed(() => user.value?.timezone || 'UTC')
const today = () => zonedDay(new Date(), tz.value)

const entries = ref<TaskTimeEntry[]>([])
const totalMinutes = ref(props.initialMinutes)
const newMinutes = ref<number | ''>('')
const newNote = ref('')
const newDay = ref(today())
const adding = ref(false)

const totalLabel = computed(() => formatDuration(totalMinutes.value))

function dayLabel(iso: string) {
  const day = zonedDay(new Date(iso), tz.value)
  if (day === today()) return 'today'
  const [y, m, d] = day.split('-').map(Number)
  return new Date(y, m - 1, d).toLocaleDateString(undefined, { month: 'short', day: 'numeric' })
}

async function loadEntries() {
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

async function add() {
  if (!props.canLog) return
  const minutes = Number(newMinutes.value)
  if (!Number.isFinite(minutes) || minutes <= 0) {
    toast.push('Enter minutes greater than 0', 'error')
    return
  }
  // Back-dated work lands at midday on that day in your time zone.
  const spentAt = newDay.value && newDay.value !== today() ? zonedToInstant(newDay.value, 12 * 60, tz.value).toISOString() : undefined
  adding.value = true
  try {
    await api.addTimeEntry(props.taskId, minutes, newNote.value.trim(), spentAt)
    newMinutes.value = ''
    newNote.value = ''
    newDay.value = today()
    toast.push('Time logged', 'success')
    await loadEntries()
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
    await Promise.all([loadEntries(), loadBlocks()])
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Could not delete time entry', 'error')
  }
}

// --- My time blocks on this task -----------------------------------------

const blocks = ref<TimeBlock[]>([])
const blockDay = ref(today())
const blockStart = ref('09:00')
const blockMinutes = ref(60)
const blocking = ref(false)
const showAllBlocks = ref(false)

const DURATIONS = [15, 30, 45, 60, 90, 120, 180, 240, 300, 360, 480]

const visibleBlocks = computed(() => {
  // Upcoming and unlogged blocks first matter most; logged ones are history.
  const now = Date.now()
  const open = blocks.value.filter((b) => !b.logged || new Date(b.end_at).getTime() > now)
  return showAllBlocks.value ? blocks.value : open
})
const hiddenBlockCount = computed(() => blocks.value.length - visibleBlocks.value.length)
const plannedMinutes = computed(() => blocks.value.reduce((sum, b) => sum + b.minutes, 0))

function blockLabel(b: TimeBlock) {
  const s = new Date(b.start_at)
  const e = new Date(b.end_at)
  return `${dayLabel(b.start_at)}, ${formatClock(zonedMinutes(s, tz.value))} – ${formatClock(zonedMinutes(e, tz.value))}`
}

function started(b: TimeBlock) {
  return new Date(b.start_at).getTime() <= Date.now()
}

async function loadBlocks() {
  const id = props.taskId
  try {
    const list = await api.listTaskTimeBlocks(id)
    if (id === props.taskId) blocks.value = list
  } catch {
    blocks.value = []
  }
}

async function addBlock() {
  const [h, m] = blockStart.value.split(':').map(Number)
  if (!blockDay.value || !Number.isFinite(h) || !Number.isFinite(m)) {
    toast.push('Choose a day and start time', 'error')
    return
  }
  const startMin = h * 60 + m
  if (startMin % BLOCK_STEP_MINUTES !== 0) {
    toast.push(`Start on a ${BLOCK_STEP_MINUTES}-minute mark (e.g. 9:00, 9:15)`, 'error')
    return
  }
  const start = zonedToInstant(blockDay.value, startMin, tz.value)
  const end = new Date(start.getTime() + blockMinutes.value * 60000)
  blocking.value = true
  try {
    await api.createTimeBlock(props.taskId, start.toISOString(), end.toISOString())
    toast.push(`Blocked ${formatDuration(blockMinutes.value)} on your calendar`, 'success')
    await loadBlocks()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Could not block time', 'error')
  } finally {
    blocking.value = false
  }
}

async function logBlock(b: TimeBlock) {
  try {
    await api.logTimeBlock(b.id)
    toast.push(`Logged ${formatDuration(b.minutes)}`, 'success')
    await Promise.all([loadEntries(), loadBlocks()])
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Could not log time', 'error')
  }
}

async function removeBlock(b: TimeBlock) {
  try {
    await api.deleteTimeBlock(b.id)
    blocks.value = blocks.value.filter((x) => x.id !== b.id)
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Could not remove the block', 'error')
  }
}

watch(
  () => props.taskId,
  () => {
    entries.value = []
    blocks.value = []
    totalMinutes.value = props.initialMinutes
    newMinutes.value = ''
    newNote.value = ''
    newDay.value = today()
    blockDay.value = today()
    showAllBlocks.value = false
    void loadEntries()
    void loadBlocks()
  },
  { immediate: true },
)
</script>

<template>
  <div>
    <label class="d-block">Time tracking</label>
    <p class="small text-muted mb-2">
      Total logged: <strong>{{ totalLabel }}</strong>
      <span v-if="totalMinutes">({{ totalMinutes }} min)</span>
    </p>
    <form v-if="canLog" class="row g-2 align-items-end mb-2" @submit.prevent="add">
      <div class="col-6 col-sm-3">
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
      <div class="col-6 col-sm-3">
        <label class="form-label small mb-0" for="time_day">When</label>
        <input id="time_day" v-model="newDay" type="date" class="form-control form-control-sm" :max="today()" />
      </div>
      <div class="col-8 col-sm-4">
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
      <div class="col-4 col-sm-2">
        <button class="btn btn-sm btn-outline-primary w-100" type="submit" :disabled="adding">Add</button>
      </div>
    </form>
    <ul v-if="entries.length" class="list-unstyled small mb-0">
      <li v-for="entry in entries" :key="entry.id" class="d-flex justify-content-between align-items-start gap-2 mb-1">
        <span>
          <strong>{{ formatDuration(entry.minutes) }}</strong>
          <span class="text-muted"> {{ dayLabel(entry.spent_at || entry.created_at) }}</span>
          <span v-if="entry.note"> — {{ entry.note }}</span>
          <span class="text-muted"> · {{ entry.user_name || entry.user_email || 'user' }}</span>
        </span>
        <button v-if="canLog" class="btn btn-sm btn-link text-danger p-0" type="button" @click="remove(entry.id)">
          Delete
        </button>
      </li>
    </ul>
    <p v-else class="small text-muted mb-0">No time entries yet.</p>

    <div class="mt-3">
      <div class="d-flex justify-content-between align-items-baseline gap-2">
        <span class="small fw-semibold">My time blocks</span>
        <RouterLink class="small" :to="{ path: '/calendar', query: { view: 'week' } }">Open calendar</RouterLink>
      </div>
      <p class="small text-muted mb-2">
        Set aside time for this task on your hourly calendar. Only you see your blocks.
        <template v-if="plannedMinutes"> {{ formatDuration(plannedMinutes) }} blocked in total.</template>
      </p>
      <form class="row g-2 align-items-end mb-2" @submit.prevent="addBlock">
        <div class="col-6 col-sm-4">
          <label class="form-label small mb-0" for="block_day">Day</label>
          <input id="block_day" v-model="blockDay" type="date" class="form-control form-control-sm" required />
        </div>
        <div class="col-6 col-sm-3">
          <label class="form-label small mb-0" for="block_start">Start</label>
          <input
            id="block_start"
            v-model="blockStart"
            type="time"
            step="900"
            class="form-control form-control-sm"
            required
          />
        </div>
        <div class="col-8 col-sm-3">
          <label class="form-label small mb-0" for="block_minutes">Length</label>
          <select id="block_minutes" v-model.number="blockMinutes" class="form-select form-select-sm">
            <option v-for="d in DURATIONS" :key="d" :value="d">{{ formatDuration(d) }}</option>
          </select>
        </div>
        <div class="col-4 col-sm-2">
          <button class="btn btn-sm btn-outline-primary w-100" type="submit" :disabled="blocking">Block</button>
        </div>
      </form>
      <ul v-if="visibleBlocks.length" class="list-unstyled small mb-0">
        <li v-for="b in visibleBlocks" :key="b.id" class="d-flex justify-content-between align-items-start gap-2 mb-1">
          <span>
            <i
              class="bi me-1"
              :class="b.logged ? 'bi-check2-circle text-success' : 'bi-calendar-event text-muted'"
              aria-hidden="true"
            />
            {{ blockLabel(b) }} · {{ formatDuration(b.minutes) }}
            <span v-if="b.logged" class="text-muted">(logged)</span>
          </span>
          <span class="d-flex gap-2 flex-shrink-0">
            <button
              v-if="canLog && !b.logged && started(b)"
              class="btn btn-sm btn-link p-0"
              type="button"
              @click="logBlock(b)"
            >
              Log
            </button>
            <button class="btn btn-sm btn-link text-danger p-0" type="button" @click="removeBlock(b)">Remove</button>
          </span>
        </li>
      </ul>
      <button
        v-if="hiddenBlockCount > 0"
        type="button"
        class="btn btn-sm btn-link p-0 small"
        @click="showAllBlocks = true"
      >
        Show {{ hiddenBlockCount }} past logged block{{ hiddenBlockCount === 1 ? '' : 's' }}
      </button>
    </div>
  </div>
</template>
