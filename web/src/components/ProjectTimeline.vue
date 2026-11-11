<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { api } from '@/api/client'
import { APIError, type ProjectTimeline, type ProjectTimelineTask } from '@/api/types'
import { useToast } from '@/composables/useToast'
import {
  isoFromDay,
  monthBands,
  shiftDates,
  taskSpan,
  timelineWindow,
  weekday,
  type TimelineSpan,
} from '@/utils/timeline'

const props = defineProps<{
  projectId: number
  canEdit: boolean
  /** Bumped by the parent when tasks change elsewhere, to reload. */
  revision?: number | string
}>()

const emit = defineEmits<{
  'open-task': [id: number]
}>()

const toast = useToast()

const ROW_H = 34
const BAR_H = 20
const ZOOMS = [
  { key: 'day', label: 'Days', px: 32 },
  { key: 'week', label: 'Weeks', px: 14 },
  { key: 'month', label: 'Months', px: 5 },
] as const
type ZoomKey = (typeof ZOOMS)[number]['key']
const ZOOM_KEY = 'gotodo.timelineZoom'

function readZoom(): ZoomKey {
  try {
    const v = localStorage.getItem(ZOOM_KEY)
    if (v === 'day' || v === 'week' || v === 'month') return v
  } catch {
    /* ignore */
  }
  return 'week'
}

const zoom = ref<ZoomKey>(readZoom())
const px = computed(() => ZOOMS.find((z) => z.key === zoom.value)!.px)
const showDone = ref(true)

function setZoom(k: ZoomKey) {
  zoom.value = k
  try {
    localStorage.setItem(ZOOM_KEY, k)
  } catch {
    /* ignore */
  }
}

const data = ref<ProjectTimeline | null>(null)
const loading = ref(false)
const error = ref('')

async function load() {
  loading.value = true
  error.value = ''
  try {
    data.value = await api.projectTimeline(props.projectId)
  } catch (err) {
    error.value = err instanceof APIError ? err.message : 'Could not load the timeline.'
  } finally {
    loading.value = false
  }
}

watch(() => [props.projectId, props.revision], load, { immediate: true })

function localToday(): number {
  const now = new Date()
  return Math.round(Date.UTC(now.getFullYear(), now.getMonth(), now.getDate()) / 86_400_000)
}
const today = localToday()

type Row = { task: ProjectTimelineTask; span: TimelineSpan; index: number; depth: number }

const rows = computed<Row[]>(() => {
  const tasks = (data.value?.tasks ?? []).filter((t) => showDone.value || !t.completed)
  const byId = new Map(tasks.map((t) => [t.id, t]))
  // Subtasks follow their parent when the parent is on the timeline.
  const children = new Map<number, ProjectTimelineTask[]>()
  const roots: ProjectTimelineTask[] = []
  for (const t of tasks) {
    if (t.parent_id && byId.has(t.parent_id)) {
      const list = children.get(t.parent_id) ?? []
      list.push(t)
      children.set(t.parent_id, list)
    } else {
      roots.push(t)
    }
  }
  const out: Row[] = []
  const push = (t: ProjectTimelineTask, depth: number) => {
    const span = taskSpan(t.start_date, t.due_date)
    if (span) out.push({ task: t, span, index: out.length, depth })
    for (const c of children.get(t.id) ?? []) push(c, depth + 1)
  }
  roots.forEach((t) => push(t, 0))
  return out
})

const range = computed(() => timelineWindow(rows.value.map((r) => r.span), today))
const totalDays = computed(() => range.value.end - range.value.start + 1)
const gridWidth = computed(() => totalDays.value * px.value)
const gridHeight = computed(() => Math.max(rows.value.length, 1) * ROW_H)
const months = computed(() => monthBands(range.value.start, range.value.end))
const days = computed(() => {
  const out: number[] = []
  for (let d = range.value.start; d <= range.value.end; d++) out.push(d)
  return out
})

function x(day: number) {
  return (day - range.value.start) * px.value
}

const rowById = computed(() => new Map(rows.value.map((r) => [r.task.id, r])))

function barStyle(r: Row) {
  const offset = dragging.value?.id === r.task.id ? dragging.value.delta : 0
  const left = x(r.span.start + offset)
  const width = (r.span.end - r.span.start + 1) * px.value
  if (r.span.milestone) {
    const size = BAR_H - 4
    return {
      left: `${left + px.value / 2 - size / 2}px`,
      top: `${r.index * ROW_H + (ROW_H - size) / 2}px`,
      width: `${size}px`,
      height: `${size}px`,
    }
  }
  return {
    left: `${left + 1}px`,
    top: `${r.index * ROW_H + (ROW_H - BAR_H) / 2}px`,
    width: `${Math.max(width - 2, 4)}px`,
    height: `${BAR_H}px`,
  }
}

function isOverdue(r: Row) {
  return !r.task.completed && !!r.task.due_date && r.span.end < today
}

function barTitle(r: Row) {
  const t = r.task
  const parts = [t.title]
  if (t.start_date) parts.push(`Start ${t.start_date}`)
  if (t.due_date) parts.push(`Due ${t.due_date}`)
  if (t.status_name) parts.push(t.status_name)
  if (t.claimed_by_name) parts.push(`Claimed by ${t.claimed_by_name}`)
  if (t.completed) parts.push('Done')
  return parts.join(' · ')
}

/** Elbow paths from the end of each blocker to the start of what it blocks. */
const linkPaths = computed(() => {
  const out: { key: string; d: string; late: boolean }[] = []
  for (const l of data.value?.links ?? []) {
    const from = rowById.value.get(l.from_task_id)
    const to = rowById.value.get(l.to_task_id)
    if (!from || !to) continue
    const x1 = x(from.span.end + 1)
    const y1 = from.index * ROW_H + ROW_H / 2
    const x2 = x(to.span.start)
    const y2 = to.index * ROW_H + ROW_H / 2
    const mid = Math.max(x1 + 6, Math.min(x2 - 6, x1 + 12))
    out.push({
      key: `${l.from_task_id}-${l.to_task_id}`,
      d: `M ${x1} ${y1} H ${mid} V ${y2} H ${x2}`,
      // The blocked task starts before its blocker ends.
      late: to.span.start <= from.span.end,
    })
  }
  return out
})

// Drag a bar sideways to move both dates; snaps to whole days.
const dragging = ref<{ id: number; startX: number; delta: number; moved: boolean } | null>(null)

function onBarPointerDown(e: PointerEvent, r: Row) {
  if (!props.canEdit || e.button !== 0) return
  dragging.value = { id: r.task.id, startX: e.clientX, delta: 0, moved: false }
  ;(e.currentTarget as HTMLElement).setPointerCapture(e.pointerId)
}

function onBarPointerMove(e: PointerEvent) {
  const d = dragging.value
  if (!d) return
  const dx = e.clientX - d.startX
  if (Math.abs(dx) > 3) d.moved = true
  d.delta = Math.round(dx / px.value)
}

async function onBarPointerUp(r: Row) {
  const d = dragging.value
  if (!d) return
  dragging.value = null
  if (!d.moved) {
    emit('open-task', r.task.id)
    return
  }
  if (d.delta === 0) return
  const next = shiftDates(r.task.start_date, r.task.due_date, d.delta)
  const prev = { start_date: r.task.start_date, due_date: r.task.due_date }
  r.task.start_date = next.start_date ?? undefined
  r.task.due_date = next.due_date ?? undefined
  try {
    await api.patchTask(r.task.id, {
      ...(next.start_date ? { start_date: next.start_date } : {}),
      ...(next.due_date ? { due_date: next.due_date } : {}),
    })
  } catch (err) {
    r.task.start_date = prev.start_date
    r.task.due_date = prev.due_date
    toast.push(err instanceof APIError ? err.message : 'Could not move the task', 'error')
  }
}

function onBarKey(e: KeyboardEvent, r: Row) {
  if (e.key === 'Enter' || e.key === ' ') {
    e.preventDefault()
    emit('open-task', r.task.id)
  }
}

onBeforeUnmount(() => {
  dragging.value = null
})

const scroller = ref<HTMLElement | null>(null)
function scrollToToday() {
  const el = scroller.value
  if (!el) return
  el.scrollLeft = Math.max(0, x(today) - el.clientWidth / 3)
}
watch([() => data.value, zoom], () => requestAnimationFrame(scrollToToday))

const dragHint = computed(() => (props.canEdit ? ' Drag a bar to move its dates.' : ''))
</script>

<template>
  <section class="project-timeline" aria-label="Project timeline">
    <div class="d-flex flex-wrap align-items-center gap-2 mb-2">
      <div class="btn-group btn-group-sm" role="group" aria-label="Timeline zoom">
        <button
          v-for="z in ZOOMS"
          :key="z.key"
          type="button"
          class="btn btn-outline-secondary"
          :class="{ active: zoom === z.key }"
          :aria-pressed="zoom === z.key"
          @click="setZoom(z.key)"
        >
          {{ z.label }}
        </button>
      </div>
      <button type="button" class="btn btn-sm btn-outline-secondary" @click="scrollToToday">Today</button>
      <div class="form-check form-switch mb-0 ms-1">
        <input id="timeline-show-done" v-model="showDone" class="form-check-input" type="checkbox" />
        <label class="form-check-label small" for="timeline-show-done">Show completed</label>
      </div>
      <span class="small text-muted ms-auto">
        <template v-if="data?.undated_count">
          {{ data.undated_count }} open {{ data.undated_count === 1 ? 'task has' : 'tasks have' }} no dates.
        </template>
        Give tasks a start and due date to see them here.{{ dragHint }}
      </span>
    </div>

    <div v-if="error" class="alert alert-danger py-2">{{ error }}</div>
    <div v-else-if="loading && !data" class="text-center py-4 text-muted">
      <span class="spinner-border spinner-border-sm me-2" role="status" />Loading timeline…
    </div>
    <div v-else-if="!rows.length" class="text-center text-muted py-5 border rounded">
      No tasks with dates yet.
    </div>
    <div v-else class="timeline-frame border rounded">
      <div class="timeline-labels">
        <div class="timeline-head" />
        <button
          v-for="r in rows"
          :key="r.task.id"
          type="button"
          class="timeline-label text-truncate"
          :class="{ 'text-muted text-decoration-line-through': r.task.completed }"
          :style="{ height: `${ROW_H}px`, paddingLeft: `${8 + r.depth * 14}px` }"
          :title="r.task.title"
          @click="emit('open-task', r.task.id)"
        >
          {{ r.task.title }}
        </button>
      </div>
      <div ref="scroller" class="timeline-scroll">
        <div :style="{ width: `${gridWidth}px` }">
          <div class="timeline-head">
            <div class="timeline-months">
              <div
                v-for="m in months"
                :key="m.start"
                class="timeline-month text-truncate"
                :style="{ left: `${x(m.start)}px`, width: `${m.days * px}px` }"
              >
                {{ m.label }}
              </div>
            </div>
            <div v-if="zoom === 'day'" class="timeline-days">
              <div
                v-for="d in days"
                :key="d"
                class="timeline-day"
                :class="{ 'is-today': d === today }"
                :style="{ left: `${x(d)}px`, width: `${px}px` }"
              >
                {{ isoFromDay(d).slice(8) }}
              </div>
            </div>
          </div>
          <div class="timeline-grid" :style="{ height: `${gridHeight}px` }">
            <template v-if="zoom !== 'month'">
              <div
                v-for="d in days"
                v-show="weekday(d) === 0 || weekday(d) === 6"
                :key="`w${d}`"
                class="timeline-weekend"
                :style="{ left: `${x(d)}px`, width: `${px}px` }"
              />
            </template>
            <div
              v-for="r in rows"
              :key="`row${r.task.id}`"
              class="timeline-row-line"
              :style="{ top: `${(r.index + 1) * ROW_H - 1}px` }"
            />
            <div class="timeline-today" :style="{ left: `${x(today) + px / 2}px` }" title="Today" />
            <svg class="timeline-links" :width="gridWidth" :height="gridHeight" aria-hidden="true">
              <defs>
                <marker id="timeline-arrow" viewBox="0 0 8 8" refX="7" refY="4" markerWidth="6" markerHeight="6" orient="auto">
                  <path d="M0,0 L8,4 L0,8 z" fill="currentColor" />
                </marker>
              </defs>
              <path
                v-for="p in linkPaths"
                :key="p.key"
                :d="p.d"
                :class="{ late: p.late }"
                fill="none"
                marker-end="url(#timeline-arrow)"
              />
            </svg>
            <div
              v-for="r in rows"
              :key="`bar${r.task.id}`"
              class="timeline-bar"
              :class="{
                milestone: r.span.milestone,
                done: r.task.completed,
                overdue: isOverdue(r),
                draggable: canEdit,
                dragging: dragging?.id === r.task.id,
              }"
              :style="barStyle(r)"
              role="button"
              tabindex="0"
              :title="barTitle(r)"
              :aria-label="barTitle(r)"
              @pointerdown="onBarPointerDown($event, r)"
              @pointermove="onBarPointerMove"
              @pointerup="onBarPointerUp(r)"
              @pointercancel="dragging = null"
              @keydown="onBarKey($event, r)"
            >
              <span v-if="!r.span.milestone && (r.span.end - r.span.start + 1) * px > 60" class="timeline-bar-text">
                {{ r.task.title }}
              </span>
            </div>
          </div>
        </div>
      </div>
    </div>
    <p v-if="data?.truncated" class="small text-muted mt-2 mb-0">
      Showing the first 2000 dated tasks.
    </p>
  </section>
</template>

<style scoped>
.timeline-frame {
  display: flex;
  overflow: hidden;
  background: var(--ordryn-card-bg);
}
.timeline-labels {
  flex: 0 0 auto;
  width: clamp(120px, 28%, 260px);
  border-right: 1px solid var(--ordryn-card-border);
  display: flex;
  flex-direction: column;
}
.timeline-label {
  display: block;
  width: 100%;
  border: 0;
  border-bottom: 1px solid var(--ordryn-card-border);
  background: transparent;
  color: var(--ordryn-text);
  text-align: left;
  font-size: 0.85rem;
  line-height: 1;
  padding-right: 8px;
}
.timeline-label:hover,
.timeline-label:focus-visible {
  background: var(--ordryn-muted-bg);
}
.timeline-scroll {
  flex: 1 1 auto;
  overflow-x: auto;
  overflow-y: hidden;
}
.timeline-head {
  position: relative;
  height: 44px;
  border-bottom: 1px solid var(--ordryn-card-border);
  background: var(--ordryn-muted-bg);
}
.timeline-months,
.timeline-days {
  position: relative;
  height: 22px;
}
.timeline-month {
  position: absolute;
  top: 0;
  height: 22px;
  padding: 3px 6px;
  font-size: 0.75rem;
  font-weight: 600;
  border-left: 1px solid var(--ordryn-card-border);
  color: var(--ordryn-text);
}
.timeline-day {
  position: absolute;
  top: 0;
  height: 22px;
  font-size: 0.7rem;
  text-align: center;
  line-height: 22px;
  color: var(--ordryn-muted);
}
.timeline-day.is-today {
  color: var(--ordryn-accent);
  font-weight: 700;
}
.timeline-grid {
  position: relative;
}
.timeline-weekend {
  position: absolute;
  top: 0;
  bottom: 0;
  background: var(--ordryn-muted-bg);
  opacity: 0.6;
}
.timeline-row-line {
  position: absolute;
  left: 0;
  right: 0;
  border-top: 1px solid var(--ordryn-card-border);
  opacity: 0.6;
}
.timeline-today {
  position: absolute;
  top: 0;
  bottom: 0;
  width: 2px;
  background: var(--ordryn-accent);
  opacity: 0.7;
}
.timeline-links {
  position: absolute;
  top: 0;
  left: 0;
  pointer-events: none;
  color: var(--ordryn-muted);
}
.timeline-links path {
  stroke: currentColor;
  stroke-width: 1.5;
}
.timeline-links path.late {
  stroke: var(--bs-danger);
  color: var(--bs-danger);
}
.timeline-bar {
  position: absolute;
  border-radius: 4px;
  background: var(--ordryn-accent);
  color: #fff;
  font-size: 0.72rem;
  line-height: 20px;
  overflow: hidden;
  white-space: nowrap;
  cursor: pointer;
  touch-action: none;
  box-shadow: 0 1px 2px rgba(0, 0, 0, 0.15);
}
.timeline-bar.draggable {
  cursor: grab;
}
.timeline-bar.dragging {
  cursor: grabbing;
  opacity: 0.85;
  z-index: 2;
}
.timeline-bar.milestone {
  border-radius: 2px;
  transform: rotate(45deg);
}
.timeline-bar.done {
  background: var(--ordryn-muted);
  opacity: 0.6;
}
.timeline-bar.overdue {
  background: var(--bs-danger);
}
.timeline-bar:focus-visible {
  outline: 2px solid var(--ordryn-text);
  outline-offset: 1px;
}
.timeline-bar-text {
  padding: 0 6px;
}
</style>
