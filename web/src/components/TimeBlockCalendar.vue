<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { api } from '@/api/client'
import type { Task, TimeBlock } from '@/api/types'
import { APIError } from '@/api/types'
import { useTaskSidebar } from '@/composables/useTaskSidebar'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import {
  BLOCK_STEP_MINUTES,
  DAY_MINUTES,
  MAX_BLOCK_MINUTES,
  addDays,
  clamp,
  daySegments,
  formatClock,
  formatDuration,
  minutesOnDay,
  snapMinutes,
  weekStart,
  zonedDay,
  zonedMinutes,
  zonedToInstant,
  type DaySegment,
} from '@/utils/timeBlocks'

const props = defineProps<{
  timezone: string
  /** 'week' shows Sunday–Saturday; 'day' one day. */
  mode: 'week' | 'day'
}>()

const { openEdit, lastSavedTask } = useTaskSidebar()
const { push } = useToast()
const { askConfirm } = useConfirm()

const HOUR_PX = 48
const PX_PER_MIN = HOUR_PX / 60
const DEFAULT_BLOCK_MINUTES = 60
const TASK_MIME = 'application/x-ordryn-task'

const today = ref(zonedDay(new Date(), props.timezone))
const anchor = ref(today.value)
const blocks = ref<TimeBlock[]>([])
const busy = ref(false)
const error = ref('')
const nowMin = ref(zonedMinutes(new Date(), props.timezone))

const days = computed(() => {
  if (props.mode === 'day') return [anchor.value]
  const start = weekStart(anchor.value)
  return Array.from({ length: 7 }, (_, i) => addDays(start, i))
})

const rangeLabel = computed(() => {
  const fmt = (d: string, opts: Intl.DateTimeFormatOptions) => {
    const [y, m, dd] = d.split('-').map(Number)
    return new Date(y, m - 1, dd).toLocaleDateString(undefined, opts)
  }
  const first = days.value[0]
  const last = days.value[days.value.length - 1]
  if (first === last) return fmt(first, { weekday: 'long', month: 'long', day: 'numeric', year: 'numeric' })
  return `${fmt(first, { month: 'short', day: 'numeric' })} – ${fmt(last, { month: 'short', day: 'numeric', year: 'numeric' })}`
})

function dayHeader(day: string) {
  const [y, m, d] = day.split('-').map(Number)
  const date = new Date(y, m - 1, d)
  return {
    weekday: date.toLocaleDateString(undefined, { weekday: 'short' }),
    num: d,
  }
}

const segmentsByDay = computed(() => {
  const out: Record<string, DaySegment<TimeBlock>[]> = {}
  for (const day of days.value) out[day] = daySegments(blocks.value, day, props.timezone)
  return out
})

const plannedMinutes = computed(() => blocks.value.reduce((sum, b) => sum + inRangeMinutes(b), 0))
const loggedMinutes = computed(() => blocks.value.filter((b) => b.logged).reduce((sum, b) => sum + inRangeMinutes(b), 0))

function inRangeMinutes(b: TimeBlock) {
  return days.value.reduce((sum, d) => sum + minutesOnDay([b], d, props.timezone), 0)
}

function dayPlanned(day: string) {
  return (segmentsByDay.value[day] ?? []).reduce((sum, s) => sum + (s.endMin - s.startMin), 0)
}

async function load() {
  busy.value = true
  error.value = ''
  const from = zonedToInstant(days.value[0], 0, props.timezone).toISOString()
  const to = zonedToInstant(addDays(days.value[days.value.length - 1], 1), 0, props.timezone).toISOString()
  try {
    blocks.value = await api.listTimeBlocks(from, to)
  } catch (err) {
    error.value = err instanceof APIError ? err.message : 'Could not load time blocks'
  } finally {
    busy.value = false
  }
}

function step(dir: -1 | 1) {
  anchor.value = addDays(anchor.value, dir * (props.mode === 'day' ? 1 : 7))
}

function goToday() {
  today.value = zonedDay(new Date(), props.timezone)
  anchor.value = today.value
}

watch(() => [days.value[0], days.value.length, props.timezone], () => void load())

// --- Tasks to schedule ---------------------------------------------------

const trayTasks = ref<Task[]>([])
const plannerTasks = ref<Task[]>([])
const trayQuery = ref('')
const traySearching = ref(false)
let searchTimer: ReturnType<typeof setTimeout> | null = null

async function loadTray() {
  try {
    const p = await api.planner(14)
    const seen = new Set<number>()
    const list: Task[] = []
    for (const t of [...p.overdue, ...p.due_today, ...p.woken, ...p.upcoming.flatMap((d) => d.tasks)]) {
      if (seen.has(t.id)) continue
      seen.add(t.id)
      list.push(t)
    }
    plannerTasks.value = list
    if (!trayQuery.value.trim()) trayTasks.value = list
  } catch {
    plannerTasks.value = []
  }
}
watch(trayQuery, (q) => {
  if (searchTimer) clearTimeout(searchTimer)
  const text = q.trim()
  if (!text) {
    trayTasks.value = plannerTasks.value
    return
  }
  searchTimer = setTimeout(async () => {
    traySearching.value = true
    try {
      const res = await api.search({ q: text, scope: 'titles', per_page: 20 })
      if (trayQuery.value.trim() === text) trayTasks.value = res.tasks.filter((t) => !t.completed)
    } catch {
      /* keep the last list */
    } finally {
      traySearching.value = false
    }
  }, 250)
})

function onTrayDragStart(e: DragEvent, task: Task) {
  if (!e.dataTransfer) return
  e.dataTransfer.effectAllowed = 'copy'
  e.dataTransfer.setData(TASK_MIME, String(task.id))
  e.dataTransfer.setData('text/plain', task.title)
}

// --- Grid geometry ----------------------------------------------------------

const scroller = ref<HTMLElement | null>(null)
const columns = ref<HTMLElement | null>(null)

function minuteAt(clientY: number, col: HTMLElement) {
  const rect = col.getBoundingClientRect()
  return clamp((clientY - rect.top) / PX_PER_MIN, 0, DAY_MINUTES)
}

function dayIndexAt(clientX: number) {
  const el = columns.value
  if (!el) return 0
  const rect = el.getBoundingClientRect()
  const w = rect.width / days.value.length
  return clamp(Math.floor((clientX - rect.left) / w), 0, days.value.length - 1)
}

function iso(day: string, minutes: number) {
  // Minutes may run past midnight (a block ending at 24:00 or later).
  const extraDays = Math.floor(minutes / DAY_MINUTES)
  return zonedToInstant(addDays(day, extraDays), minutes - extraDays * DAY_MINUTES, props.timezone).toISOString()
}

async function createBlock(taskId: number, day: string, startMin: number, endMin: number) {
  try {
    const b = await api.createTimeBlock(taskId, iso(day, startMin), iso(day, endMin))
    blocks.value = [...blocks.value, b]
    push(`Blocked ${formatDuration(b.minutes)} for "${b.task_title}"`, 'success')
  } catch (err) {
    push(err instanceof APIError ? err.message : 'Could not block time', 'error')
  }
}

// Drop a tray task on a column: a one-hour block at the drop point.
const dropHint = ref<{ day: string; min: number } | null>(null)

function onColDragOver(e: DragEvent, day: string) {
  if (!e.dataTransfer?.types.includes(TASK_MIME)) return
  e.preventDefault()
  e.dataTransfer.dropEffect = 'copy'
  const min = clamp(snapMinutes(minuteAt(e.clientY, e.currentTarget as HTMLElement) - 30), 0, DAY_MINUTES - BLOCK_STEP_MINUTES)
  dropHint.value = { day, min }
}

function onColDragLeave(e: DragEvent) {
  const to = e.relatedTarget as Node | null
  if (to && (e.currentTarget as HTMLElement).contains(to)) return
  dropHint.value = null
}

async function onColDrop(e: DragEvent, day: string) {
  const id = Number(e.dataTransfer?.getData(TASK_MIME))
  const hint = dropHint.value
  dropHint.value = null
  if (!id || !hint) return
  e.preventDefault()
  await createBlock(id, day, hint.min, Math.min(DAY_MINUTES, hint.min + DEFAULT_BLOCK_MINUTES))
}

// --- Pointer: select a range, move or resize blocks ---------------------------

type Drag =
  | { kind: 'select'; day: string; anchorMin: number; curMin: number; moved: boolean }
  | {
      kind: 'move' | 'resize'
      block: TimeBlock
      seg: DaySegment<TimeBlock>
      day: string
      startY: number
      startX: number
      dayIdx: number
      deltaMin: number
      deltaDays: number
      moved: boolean
    }

const drag = ref<Drag | null>(null)

const selection = computed(() => {
  const d = drag.value
  if (!d || d.kind !== 'select') return null
  const a = Math.min(d.anchorMin, d.curMin)
  const b = Math.max(d.anchorMin, d.curMin)
  return { day: d.day, start: a, end: Math.max(b, a + BLOCK_STEP_MINUTES) }
})

function onColPointerDown(e: PointerEvent, day: string) {
  if (e.button !== 0 || (e.target as HTMLElement).closest('.tb-block')) return
  const min = Math.floor(minuteAt(e.clientY, e.currentTarget as HTMLElement) / BLOCK_STEP_MINUTES) * BLOCK_STEP_MINUTES
  drag.value = { kind: 'select', day, anchorMin: min, curMin: min + BLOCK_STEP_MINUTES, moved: false }
  ;(e.currentTarget as HTMLElement).setPointerCapture(e.pointerId)
}

function onBlockPointerDown(e: PointerEvent, seg: DaySegment<TimeBlock>, day: string, resize: boolean) {
  if (e.button !== 0) return
  e.stopPropagation()
  drag.value = {
    kind: resize ? 'resize' : 'move',
    block: seg.block,
    seg,
    day,
    startY: e.clientY,
    startX: e.clientX,
    dayIdx: days.value.indexOf(day),
    deltaMin: 0,
    deltaDays: 0,
    moved: false,
  }
  ;(e.currentTarget as HTMLElement).setPointerCapture(e.pointerId)
}

function onPointerMove(e: PointerEvent) {
  const d = drag.value
  if (!d) return
  if (d.kind === 'select') {
    const col = columns.value?.querySelectorAll<HTMLElement>('.tb-col')[days.value.indexOf(d.day)]
    if (!col) return
    const min = clamp(Math.ceil(minuteAt(e.clientY, col) / BLOCK_STEP_MINUTES) * BLOCK_STEP_MINUTES, 0, DAY_MINUTES)
    if (min !== d.curMin) d.moved = true
    d.curMin = min <= d.anchorMin ? Math.max(0, min - BLOCK_STEP_MINUTES) : min
    return
  }
  if (d.block.logged) return
  const dy = e.clientY - d.startY
  if (!d.moved && Math.abs(dy) < 4 && Math.abs(e.clientX - d.startX) < 4) return
  d.moved = true
  d.deltaMin = snapMinutes(dy / PX_PER_MIN)
  if (d.kind === 'move') d.deltaDays = dayIndexAt(e.clientX) - d.dayIdx
}

async function onPointerUp() {
  const d = drag.value
  drag.value = null
  if (!d) return
  if (d.kind === 'select') {
    const a = Math.min(d.anchorMin, d.curMin)
    const b = Math.max(d.anchorMin, d.curMin)
    // A click (no drag) offers a one-hour block from that slot.
    const end = d.moved ? Math.max(b, a + BLOCK_STEP_MINUTES) : Math.min(DAY_MINUTES, a + DEFAULT_BLOCK_MINUTES)
    openPicker(d.day, a, end)
    return
  }
  if (!d.moved) {
    selectedId.value = d.block.id
    return
  }
  const span = previewSpan(d)
  if (!span) return
  const old = d.block
  const startAt = new Date(span.startAt)
  const endAt = new Date(span.endAt)
  if (startAt.getTime() === new Date(old.start_at).getTime() && endAt.getTime() === new Date(old.end_at).getTime()) return
  blocks.value = blocks.value.map((b) =>
    b.id === old.id ? { ...b, start_at: span.startAt, end_at: span.endAt, minutes: span.minutes } : b,
  )
  try {
    const updated = await api.moveTimeBlock(old.id, span.startAt, span.endAt)
    blocks.value = blocks.value.map((b) => (b.id === updated.id ? updated : b))
  } catch (err) {
    blocks.value = blocks.value.map((b) => (b.id === old.id ? old : b))
    push(err instanceof APIError ? err.message : 'Could not move the block', 'error')
  }
}

/** The span a move/resize drag would produce. */
function previewSpan(d: Extract<Drag, { kind: 'move' | 'resize' }>) {
  const start = new Date(d.block.start_at).getTime()
  const end = new Date(d.block.end_at).getTime()
  const minutes = Math.round((end - start) / 60000)
  if (d.kind === 'move') {
    const shift = d.deltaMin * 60000 + d.deltaDays * DAY_MINUTES * 60000
    return { startAt: new Date(start + shift).toISOString(), endAt: new Date(end + shift).toISOString(), minutes }
  }
  const newMinutes = clamp(minutes + d.deltaMin, BLOCK_STEP_MINUTES, MAX_BLOCK_MINUTES)
  return { startAt: d.block.start_at, endAt: new Date(start + newMinutes * 60000).toISOString(), minutes: newMinutes }
}

/** Blocks as drawn, with any in-progress drag applied. */
const displayBlocks = computed(() => {
  const d = drag.value
  if (!d || d.kind === 'select' || !d.moved) return null
  const span = previewSpan(d)
  return blocks.value.map((b) => (b.id === d.block.id ? { ...b, start_at: span.startAt, end_at: span.endAt } : b))
})

const drawnSegments = computed(() => {
  const list = displayBlocks.value
  if (!list) return segmentsByDay.value
  const out: Record<string, DaySegment<TimeBlock>[]> = {}
  for (const day of days.value) out[day] = daySegments(list, day, props.timezone)
  return out
})

function segStyle(seg: DaySegment<TimeBlock>) {
  const width = 100 / seg.lanes
  return {
    top: `${seg.startMin * PX_PER_MIN}px`,
    height: `${Math.max(seg.endMin - seg.startMin, BLOCK_STEP_MINUTES) * PX_PER_MIN - 2}px`,
    left: `calc(${seg.lane * width}% + 2px)`,
    width: `calc(${width}% - 4px)`,
  }
}

function blockTimeLabel(b: TimeBlock) {
  const s = new Date(b.start_at)
  const e = new Date(b.end_at)
  return `${formatClock(zonedMinutes(s, props.timezone))} – ${formatClock(zonedMinutes(e, props.timezone))}`
}

function isDragging(id: number) {
  const d = drag.value
  return !!d && d.kind !== 'select' && d.block.id === id && d.moved
}

// --- Task picker for an empty range -----------------------------------------

const picker = ref<{ day: string; start: number; end: number } | null>(null)

function openPicker(day: string, start: number, end: number) {
  picker.value = { day, start, end }
  trayQuery.value = ''
  void nextTick(() => document.getElementById('tb-picker-search')?.focus())
}

async function pickTask(task: Task) {
  const p = picker.value
  picker.value = null
  if (!p) return
  await createBlock(task.id, p.day, p.start, p.end)
}

function pickerLabel() {
  const p = picker.value
  if (!p) return ''
  const [y, m, d] = p.day.split('-').map(Number)
  const date = new Date(y, m - 1, d).toLocaleDateString(undefined, { weekday: 'short', month: 'short', day: 'numeric' })
  return `${date}, ${formatClock(p.start)} – ${formatClock(p.end)} (${formatDuration(p.end - p.start)})`
}

// --- Selected block -----------------------------------------------------------

const selectedId = ref<number | null>(null)
const selected = computed(() => blocks.value.find((b) => b.id === selectedId.value) ?? null)
const logNote = ref('')
const acting = ref(false)

watch(selectedId, () => {
  logNote.value = ''
})

function hasStarted(b: TimeBlock) {
  return new Date(b.start_at).getTime() <= Date.now()
}

async function logSelected() {
  const b = selected.value
  if (!b || acting.value) return
  acting.value = true
  try {
    const res = await api.logTimeBlock(b.id, logNote.value.trim())
    blocks.value = blocks.value.map((x) => (x.id === b.id ? res.block : x))
    push(`Logged ${formatDuration(res.time_entry.minutes)} on "${b.task_title}"`, 'success')
    selectedId.value = null
  } catch (err) {
    push(err instanceof APIError ? err.message : 'Could not log time', 'error')
  } finally {
    acting.value = false
  }
}

async function deleteSelected() {
  const b = selected.value
  if (!b || acting.value) return
  const ok = await askConfirm({
    title: 'Remove time block?',
    message: b.logged
      ? 'The block comes off your calendar. Time already logged from it stays on the task.'
      : 'The block comes off your calendar.',
    confirmLabel: 'Remove',
    danger: true,
  })
  if (!ok) return
  acting.value = true
  try {
    await api.deleteTimeBlock(b.id)
    blocks.value = blocks.value.filter((x) => x.id !== b.id)
    selectedId.value = null
  } catch (err) {
    push(err instanceof APIError ? err.message : 'Could not remove the block', 'error')
  } finally {
    acting.value = false
  }
}

function openSelectedTask() {
  const b = selected.value
  if (!b) return
  selectedId.value = null
  openEdit(b.task_id)
}

function onKey(e: KeyboardEvent) {
  if (e.key !== 'Escape') return
  picker.value = null
  selectedId.value = null
}

// --- Lifecycle ----------------------------------------------------------------

let clock: ReturnType<typeof setInterval> | null = null

onMounted(async () => {
  window.addEventListener('keydown', onKey)
  clock = setInterval(() => {
    nowMin.value = zonedMinutes(new Date(), props.timezone)
    today.value = zonedDay(new Date(), props.timezone)
  }, 60000)
  await Promise.all([load(), loadTray()])
  await nextTick()
  // Start the view at 7 AM (or an hour before now on today).
  const target = days.value.includes(today.value) ? Math.max(0, nowMin.value - 60) : 7 * 60
  if (scroller.value) scroller.value.scrollTop = Math.min(target, 7 * 60) * PX_PER_MIN
})

onBeforeUnmount(() => {
  window.removeEventListener('keydown', onKey)
  if (clock) clearInterval(clock)
  if (searchTimer) clearTimeout(searchTimer)
})

// A saved task can change titles or completion shown on blocks.
watch(lastSavedTask, () => {
  void load()
  void loadTray()
})

watch(
  () => props.timezone,
  (tz) => {
    today.value = zonedDay(new Date(), tz)
    nowMin.value = zonedMinutes(new Date(), tz)
  },
)

defineExpose({ reload: load })

const hours = Array.from({ length: 24 }, (_, h) => h)
</script>

<template>
  <div class="tb-wrap">
    <div class="d-flex flex-wrap align-items-center gap-2 mb-2">
      <div class="btn-group btn-group-sm" role="group" :aria-label="mode === 'day' ? 'Day navigation' : 'Week navigation'">
        <button type="button" class="btn btn-outline-secondary" :aria-label="mode === 'day' ? 'Previous day' : 'Previous week'" @click="step(-1)">
          <i class="bi bi-chevron-left" />
        </button>
        <button type="button" class="btn btn-outline-secondary" @click="goToday">Today</button>
        <button type="button" class="btn btn-outline-secondary" :aria-label="mode === 'day' ? 'Next day' : 'Next week'" @click="step(1)">
          <i class="bi bi-chevron-right" />
        </button>
      </div>
      <span class="fw-semibold">{{ rangeLabel }}</span>
      <span class="small text-muted ms-auto" aria-live="polite">
        {{ formatDuration(plannedMinutes) }} blocked · {{ formatDuration(loggedMinutes) }} logged from blocks
      </span>
    </div>

    <p v-if="error" class="text-danger small">{{ error }}</p>

    <div class="tb-layout">
      <aside class="tb-tray" aria-label="Tasks to schedule">
        <label class="small fw-bold mb-1 d-block" for="tb-tray-search">Tasks to schedule</label>
        <input
          id="tb-tray-search"
          v-model="trayQuery"
          type="search"
          class="form-control form-control-sm mb-2"
          placeholder="Search tasks…"
          autocomplete="off"
        />
        <p class="small text-muted mb-2">Drag a task onto the calendar, or click an empty slot.</p>
        <ul class="list-unstyled mb-0 tb-tray-list">
          <li
            v-for="t in trayTasks"
            :key="t.id"
            class="tb-tray-item"
            draggable="true"
            :title="t.title"
            @dragstart="onTrayDragStart($event, t)"
          >
            <i class="bi bi-grip-vertical text-muted" aria-hidden="true" />
            <span class="tb-tray-title">{{ t.title }}</span>
            <span v-if="t.project" class="tb-tray-project">{{ t.project }}</span>
          </li>
          <li v-if="!trayTasks.length" class="small text-muted">
            {{ traySearching ? 'Searching…' : trayQuery.trim() ? 'No open tasks match.' : 'Nothing due in the next two weeks. Search to find a task.' }}
          </li>
        </ul>
      </aside>

      <div class="tb-calendar" :class="{ 'tb-calendar--busy': busy }">
        <div class="tb-head" :style="{ gridTemplateColumns: `3.5rem repeat(${days.length}, minmax(0, 1fr))` }">
          <span />
          <div
            v-for="day in days"
            :key="day"
            class="tb-head-day"
            :class="{ 'tb-head-day--today': day === today }"
          >
            <span class="tb-head-weekday">{{ dayHeader(day).weekday }}</span>
            <span class="tb-head-num">{{ dayHeader(day).num }}</span>
            <span v-if="dayPlanned(day)" class="tb-head-total">{{ formatDuration(dayPlanned(day)) }}</span>
          </div>
        </div>

        <div ref="scroller" class="tb-scroll">
          <div class="tb-body" :style="{ gridTemplateColumns: `3.5rem minmax(0, 1fr)`, height: `${24 * HOUR_PX}px` }">
            <div class="tb-gutter" aria-hidden="true">
              <span v-for="h in hours" :key="h" class="tb-hour-label" :style="{ top: `${h * HOUR_PX}px` }">
                {{ h === 0 ? '' : formatClock(h * 60) }}
              </span>
            </div>
            <div
              ref="columns"
              class="tb-cols"
              :style="{ gridTemplateColumns: `repeat(${days.length}, minmax(0, 1fr))` }"
              @pointermove="onPointerMove"
              @pointerup="onPointerUp"
              @pointercancel="drag = null"
            >
              <div
                v-for="day in days"
                :key="day"
                class="tb-col"
                :class="{ 'tb-col--today': day === today }"
                :data-day="day"
                role="group"
                :aria-label="`Time blocks on ${day}`"
                @pointerdown="onColPointerDown($event, day)"
                @dragover="onColDragOver($event, day)"
                @dragleave="onColDragLeave"
                @drop="onColDrop($event, day)"
              >
                <div v-for="h in hours" :key="h" class="tb-hour-line" :style="{ top: `${h * HOUR_PX}px` }" />

                <div
                  v-if="dropHint && dropHint.day === day"
                  class="tb-ghost"
                  :style="{ top: `${dropHint.min * PX_PER_MIN}px`, height: `${DEFAULT_BLOCK_MINUTES * PX_PER_MIN}px` }"
                />
                <div
                  v-if="selection && selection.day === day"
                  class="tb-ghost"
                  :style="{ top: `${selection.start * PX_PER_MIN}px`, height: `${(selection.end - selection.start) * PX_PER_MIN}px` }"
                >
                  <span class="tb-ghost-label">{{ formatClock(selection.start) }} – {{ formatClock(selection.end) }}</span>
                </div>
                <div
                  v-if="picker && picker.day === day"
                  class="tb-ghost tb-ghost--picked"
                  :style="{ top: `${picker.start * PX_PER_MIN}px`, height: `${(picker.end - picker.start) * PX_PER_MIN}px` }"
                />

                <div
                  v-if="day === today"
                  class="tb-now"
                  :style="{ top: `${nowMin * PX_PER_MIN}px` }"
                  aria-hidden="true"
                />

                <div
                  v-for="seg in drawnSegments[day]"
                  :key="`${seg.block.id}-${day}`"
                  class="tb-block"
                  :class="{
                    'tb-block--logged': seg.block.logged,
                    'tb-block--done': seg.block.task_completed,
                    'tb-block--dragging': isDragging(seg.block.id),
                    'tb-block--selected': selectedId === seg.block.id,
                    'tb-block--short': seg.endMin - seg.startMin < 45,
                  }"
                  :style="segStyle(seg)"
                  role="button"
                  tabindex="0"
                  :aria-label="`${seg.block.task_title}, ${blockTimeLabel(seg.block)}${seg.block.logged ? ', logged' : ''}`"
                  @pointerdown="onBlockPointerDown($event, seg, day, false)"
                  @keydown.enter.prevent="selectedId = seg.block.id"
                  @keydown.space.prevent="selectedId = seg.block.id"
                >
                  <div class="tb-block-title">
                    <i v-if="seg.block.logged" class="bi bi-check2-circle me-1" aria-hidden="true" />
                    {{ seg.block.task_title }}
                  </div>
                  <div class="tb-block-meta">
                    {{ blockTimeLabel(seg.block) }}
                    <template v-if="seg.block.project_name"> · {{ seg.block.project_name }}</template>
                  </div>
                  <div
                    v-if="!seg.block.logged && !seg.continuesAfter"
                    class="tb-block-resize"
                    aria-hidden="true"
                    @pointerdown="onBlockPointerDown($event, seg, day, true)"
                  />
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- Pick a task for an empty range -->
    <div v-if="picker" class="tb-dialog-backdrop" @click.self="picker = null">
      <div class="tb-dialog" role="dialog" aria-modal="true" aria-labelledby="tb-picker-title">
        <div class="d-flex justify-content-between align-items-start gap-2 mb-2">
          <div>
            <h2 id="tb-picker-title" class="h6 fw-bold mb-0">Block time for a task</h2>
            <p class="small text-muted mb-0">{{ pickerLabel() }}</p>
          </div>
          <button type="button" class="btn-close" aria-label="Close" @click="picker = null" />
        </div>
        <input
          id="tb-picker-search"
          v-model="trayQuery"
          type="search"
          class="form-control form-control-sm mb-2"
          placeholder="Search tasks…"
          aria-label="Search tasks"
          autocomplete="off"
        />
        <ul class="list-unstyled mb-0 tb-picker-list">
          <li v-for="t in trayTasks" :key="t.id">
            <button type="button" class="tb-picker-item" @click="pickTask(t)">
              <span class="tb-tray-title">{{ t.title }}</span>
              <span v-if="t.project" class="tb-tray-project">{{ t.project }}</span>
            </button>
          </li>
          <li v-if="!trayTasks.length" class="small text-muted p-2">
            {{ traySearching ? 'Searching…' : 'Search for the task to block time for.' }}
          </li>
        </ul>
      </div>
    </div>

    <!-- Selected block -->
    <div v-if="selected" class="tb-dialog-backdrop" @click.self="selectedId = null">
      <div class="tb-dialog" role="dialog" aria-modal="true" aria-labelledby="tb-block-title">
        <div class="d-flex justify-content-between align-items-start gap-2 mb-2">
          <div class="min-w-0">
            <h2 id="tb-block-title" class="h6 fw-bold mb-0 text-break">{{ selected.task_title }}</h2>
            <p class="small text-muted mb-0">
              {{ blockTimeLabel(selected) }} · {{ formatDuration(selected.minutes) }}
              <template v-if="selected.project_name"> · {{ selected.project_name }}</template>
            </p>
          </div>
          <button type="button" class="btn-close" aria-label="Close" @click="selectedId = null" />
        </div>

        <p v-if="selected.logged" class="small mb-2">
          <i class="bi bi-check2-circle text-success me-1" aria-hidden="true" />
          Logged as time spent. Delete the time entry on the task to change this block.
        </p>
        <form v-else-if="hasStarted(selected)" class="mb-2" @submit.prevent="logSelected">
          <label class="form-label small mb-1" for="tb-log-note">
            Log {{ formatDuration(selected.minutes) }} as time spent
          </label>
          <div class="d-flex gap-2">
            <input
              id="tb-log-note"
              v-model="logNote"
              type="text"
              class="form-control form-control-sm"
              maxlength="200"
              placeholder="Note (optional)"
            />
            <button type="submit" class="btn btn-sm btn-primary text-nowrap" :disabled="acting">Log time</button>
          </div>
          <p class="small text-muted mt-1 mb-0">Didn't take the full block? Resize it first.</p>
        </form>
        <p v-else class="small text-muted mb-2">You can log this block once it starts.</p>

        <div class="d-flex flex-wrap gap-2 justify-content-between">
          <button type="button" class="btn btn-sm btn-outline-secondary" @click="openSelectedTask">
            <i class="bi bi-box-arrow-up-right me-1" aria-hidden="true" />Open task
          </button>
          <button type="button" class="btn btn-sm btn-outline-danger" :disabled="acting" @click="deleteSelected">
            Remove block
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.tb-layout {
  display: grid;
  grid-template-columns: 15rem minmax(0, 1fr);
  gap: 1rem;
  align-items: start;
}
@media (max-width: 767.98px) {
  .tb-layout {
    grid-template-columns: minmax(0, 1fr);
  }
  .tb-tray-list {
    max-height: 9rem;
  }
}
.tb-tray {
  background: var(--ordryn-card-bg);
  border: 1px solid var(--ordryn-card-border);
  border-radius: 0.75rem;
  padding: 0.75rem;
}
.tb-tray-list {
  max-height: 60vh;
  overflow-y: auto;
}
.tb-tray-item {
  display: flex;
  align-items: center;
  gap: 0.35rem;
  padding: 0.35rem 0.4rem;
  border-radius: 0.5rem;
  cursor: grab;
  font-size: 0.85rem;
  border: 1px solid transparent;
}
.tb-tray-item:hover {
  background: var(--ordryn-muted-bg);
  border-color: var(--ordryn-card-border);
}
.tb-tray-title {
  flex: 1 1 auto;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--ordryn-text);
}
.tb-tray-project {
  flex: 0 0 auto;
  font-size: 0.72rem;
  color: var(--ordryn-muted);
  max-width: 40%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.tb-calendar {
  background: var(--ordryn-card-bg);
  border: 1px solid var(--ordryn-card-border);
  border-radius: 0.75rem;
  overflow: hidden;
  min-width: 0;
}
.tb-calendar--busy {
  opacity: 0.85;
}
.tb-head {
  display: grid;
  border-bottom: 1px solid var(--ordryn-card-border);
}
.tb-head-day {
  display: flex;
  flex-direction: column;
  align-items: center;
  padding: 0.35rem 0.1rem;
  font-size: 0.8rem;
  color: var(--ordryn-muted);
  border-left: 1px solid var(--ordryn-card-border);
  min-width: 0;
}
.tb-head-num {
  font-size: 1.05rem;
  font-weight: 700;
  color: var(--ordryn-text);
}
.tb-head-day--today .tb-head-num {
  color: var(--ordryn-accent);
}
.tb-head-total {
  font-size: 0.7rem;
}
.tb-scroll {
  max-height: 70vh;
  overflow-y: auto;
  overflow-x: hidden;
}
.tb-body {
  display: grid;
  position: relative;
}
.tb-gutter {
  position: relative;
}
.tb-hour-label {
  position: absolute;
  right: 0.35rem;
  transform: translateY(-0.55em);
  font-size: 0.68rem;
  color: var(--ordryn-muted);
  white-space: nowrap;
}
.tb-cols {
  display: grid;
  position: relative;
  touch-action: pan-y;
}
.tb-col {
  position: relative;
  border-left: 1px solid var(--ordryn-card-border);
  cursor: crosshair;
  user-select: none;
}
.tb-col--today {
  background: color-mix(in srgb, var(--ordryn-accent) 4%, transparent);
}
.tb-hour-line {
  position: absolute;
  left: 0;
  right: 0;
  border-top: 1px solid var(--ordryn-card-border);
  opacity: 0.6;
  pointer-events: none;
}
.tb-now {
  position: absolute;
  left: 0;
  right: 0;
  border-top: 2px solid #dc3545;
  z-index: 3;
  pointer-events: none;
}
.tb-ghost {
  position: absolute;
  left: 2px;
  right: 2px;
  border-radius: 0.4rem;
  background: color-mix(in srgb, var(--ordryn-accent) 18%, transparent);
  border: 1px dashed var(--ordryn-accent);
  pointer-events: none;
  z-index: 1;
}
.tb-ghost--picked {
  border-style: solid;
}
.tb-ghost-label {
  font-size: 0.7rem;
  padding: 0.1rem 0.3rem;
  color: var(--ordryn-text);
}
.tb-block {
  position: absolute;
  z-index: 2;
  border-radius: 0.4rem;
  padding: 0.15rem 0.35rem;
  background: color-mix(in srgb, var(--ordryn-accent) 22%, var(--ordryn-card-bg));
  border-left: 3px solid var(--ordryn-accent);
  color: var(--ordryn-text);
  font-size: 0.75rem;
  overflow: hidden;
  cursor: grab;
  touch-action: none;
  box-shadow: var(--ordryn-card-shadow);
}
.tb-block:focus-visible,
.tb-block--selected {
  outline: 2px solid var(--ordryn-accent);
  outline-offset: 1px;
}
.tb-block--logged {
  background: color-mix(in srgb, #198754 18%, var(--ordryn-card-bg));
  border-left-color: #198754;
  cursor: pointer;
}
.tb-block--done .tb-block-title {
  text-decoration: line-through;
  opacity: 0.75;
}
.tb-block--dragging {
  opacity: 0.8;
  cursor: grabbing;
  z-index: 4;
}
.tb-block-title {
  font-weight: 600;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.tb-block-meta {
  font-size: 0.68rem;
  color: var(--ordryn-muted);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.tb-block--short .tb-block-meta {
  display: none;
}
.tb-block-resize {
  position: absolute;
  left: 0;
  right: 0;
  bottom: 0;
  height: 7px;
  cursor: ns-resize;
}

.tb-dialog-backdrop {
  position: fixed;
  inset: 0;
  z-index: 1060;
  background: rgba(0, 0, 0, 0.35);
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 1rem;
}
.tb-dialog {
  width: 100%;
  max-width: 26rem;
  background: var(--ordryn-card-bg);
  color: var(--ordryn-text);
  border: 1px solid var(--ordryn-card-border);
  border-radius: 0.75rem;
  padding: 1rem;
  box-shadow: var(--ordryn-card-hover-shadow);
}
.tb-picker-list {
  max-height: 50vh;
  overflow-y: auto;
}
.tb-picker-item {
  display: flex;
  width: 100%;
  gap: 0.5rem;
  align-items: center;
  text-align: left;
  border: 0;
  background: transparent;
  padding: 0.45rem 0.5rem;
  border-radius: 0.5rem;
  font-size: 0.875rem;
}
.tb-picker-item:hover,
.tb-picker-item:focus-visible {
  background: var(--ordryn-muted-bg);
}
.min-w-0 {
  min-width: 0;
}
</style>
