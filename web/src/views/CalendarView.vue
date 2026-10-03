<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import { api } from '@/api/client'
import type { CalendarMonth, Project, SavedView } from '@/api/types'
import { APIError } from '@/api/types'
import ModernSidebar from '@/components/modern/ModernSidebar.vue'
import AppFooter from '@/components/AppFooter.vue'
import { useAuth } from '@/composables/useAuth'
import { useTaskSidebar } from '@/composables/useTaskSidebar'
import { useLiveUpdates, isOwnFocusedLiveEvent } from '@/composables/useLiveUpdates'
import { useToast } from '@/composables/useToast'
import { useSidebarState } from '@/composables/useSidebarState'

const MONTH_LABELS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec']

const router = useRouter()
const route = useRoute()
const { user } = useAuth()
const { openAdd, openEdit, lastSavedTask, lastDeletedTask } = useTaskSidebar()
const { push } = useToast()
const { sidebarCollapsed, toggleSidebar } = useSidebarState()

const month = ref('')
const view = ref<CalendarMonth | null>(null)
const projects = ref<Project[]>([])
const savedViews = ref<SavedView[]>([])
const busy = ref(false)
const error = ref('')
const jumpYear = ref(new Date().getFullYear())
const completing = ref<Record<number, boolean>>({})

const timezone = computed(() => user.value?.timezone || 'UTC')

function selectHome() {
  void router.push('/')
}

function selectProject(id: string) {
  void router.push({ path: '/', query: { project: id } })
}

function selectView(id: string) {
  void router.push({ path: '/', query: { view: id } })
}

function todayInTZ(): string {
  try {
    return new Intl.DateTimeFormat('en-CA', {
      timeZone: timezone.value,
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
    }).format(new Date())
  } catch {
    return new Date().toISOString().slice(0, 10)
  }
}

function dueClass(due: string, completed: boolean): string {
  if (completed) return ''
  const today = todayInTZ()
  if (due < today) return 'due-overdue'
  if (due === today) return 'due-today'
  return ''
}

async function loadMonth(ym?: string) {
  busy.value = true
  error.value = ''
  try {
    const [calMonth, projList, viewList] = await Promise.all([
      api.calendarMonth(ym),
      api.listProjects(),
      api.listSavedViews(),
    ])
    view.value = calMonth
    month.value = calMonth.year_month
    jumpYear.value = calMonth.year
    projects.value = projList
    savedViews.value = viewList
  } catch (err) {
    error.value = err instanceof APIError ? err.message : 'Failed to load calendar'
    view.value = null
  } finally {
    busy.value = false
  }
}

function go(ym: string) {
  void loadMonth(ym)
}

async function toggleComplete(id: number, completed: boolean) {
  if (completing.value[id]) return
  completing.value = { ...completing.value, [id]: true }
  try {
    await api.patchTask(id, { completed: !completed })
    push(completed ? 'Marked incomplete' : 'Task completed', 'success')
    await loadMonth(month.value)
  } catch (err) {
    push(err instanceof APIError ? err.message : 'Could not update task', 'error')
  } finally {
    const next = { ...completing.value }
    delete next[id]
    completing.value = next
  }
}

onMounted(() => {
  const q = typeof route.query.month === 'string' ? route.query.month : undefined
  void loadMonth(q)
})

watch(lastSavedTask, () => {
  if (month.value) void loadMonth(month.value)
})

watch(lastDeletedTask, (payload) => {
  if (!payload) return
  lastDeletedTask.value = null
  if (month.value) void loadMonth(month.value)
})

useLiveUpdates((event, batch) => {
  // Comments alone don't change this view; skip only when nothing else arrived.
  if (batch.every((e) => e.type === 'task.commented')) return
  if (isOwnFocusedLiveEvent(event, user.value?.id)) return
  if (month.value) void loadMonth(month.value)
}, 500)
</script>

<template>
  <div class="ordryn-main-layout">
    <!-- Collapsible & Responsive Warm Sidebar -->
    <ModernSidebar
      :collapsed="sidebarCollapsed"
      :projects="projects"
      :saved-views="savedViews"
      @toggle-collapse="toggleSidebar"
      @select-home="selectHome"
      @select-project="selectProject"
      @select-view="selectView"
      @add-project="router.push('/projects')"
      @edit-project="() => router.push('/projects')"
      @add-view="router.push('/views')"
    />

    <!-- Main Content Area -->
    <div class="flex-grow-1 p-3 p-md-4 overflow-auto d-flex flex-column justify-content-between">
      <div class="calendar-page-inner" data-page="calendar">
        <div class="calendar-toolbar d-flex flex-wrap justify-content-between align-items-center gap-3 mb-3">
          <div>
            <h1 class="h3 fw-bold mb-1">Task calendar</h1>
            <p class="text-muted mb-0 small">Tasks with due dates — open and completed</p>
          </div>
          <div class="d-flex flex-wrap align-items-center gap-2">
            <div v-if="view" class="btn-group btn-group-sm" role="group" aria-label="Month navigation">
              <button
                type="button"
                class="btn btn-outline-secondary"
                aria-label="Previous month"
                :disabled="busy"
                @click="go(view.prev_month)"
              >
                <i class="bi bi-chevron-left" />
              </button>
              <button
                type="button"
                class="btn btn-outline-secondary"
                :disabled="busy"
                @click="go(view.today_month)"
              >
                Today
              </button>
              <button
                type="button"
                class="btn btn-outline-secondary"
                aria-label="Next month"
                :disabled="busy"
                @click="go(view.next_month)"
              >
                <i class="bi bi-chevron-right" />
              </button>
            </div>

            <div v-if="view" class="dropdown calendar-jump">
              <button
                id="calendar-jump-toggle"
                type="button"
                class="btn btn-outline-secondary btn-sm dropdown-toggle calendar-jump-toggle"
                data-bs-toggle="dropdown"
                data-bs-auto-close="outside"
                aria-expanded="false"
                aria-haspopup="dialog"
              >
                <span class="calendar-month-label">{{ view.month_label }}</span>
              </button>
              <div
                class="dropdown-menu calendar-jump-menu shadow"
                role="dialog"
                aria-label="Choose month"
              >
                <div class="calendar-jump-header">
                  <button
                    type="button"
                    class="btn btn-link btn-sm calendar-jump-year-btn p-0"
                    aria-label="Previous year"
                    @click.stop="jumpYear -= 1"
                  >
                    <i class="bi bi-chevron-left" />
                  </button>
                  <span class="calendar-jump-year">{{ jumpYear }}</span>
                  <button
                    type="button"
                    class="btn btn-link btn-sm calendar-jump-year-btn p-0"
                    aria-label="Next year"
                    @click.stop="jumpYear += 1"
                  >
                    <i class="bi bi-chevron-right" />
                  </button>
                </div>
                <div class="calendar-jump-months" role="grid">
                  <button
                    v-for="(label, idx) in MONTH_LABELS"
                    :key="label"
                    type="button"
                    class="calendar-jump-month"
                    :class="{
                      'calendar-jump-month--active':
                        view.year_month === `${jumpYear}-${String(idx + 1).padStart(2, '0')}`,
                    }"
                    @click="go(`${jumpYear}-${String(idx + 1).padStart(2, '0')}`)"
                  >
                    {{ label }}
                  </button>
                </div>
              </div>
            </div>

            <button type="button" class="btn btn-sm btn-success rounded-pill px-3 shadow-xs" @click="openAdd()">
              <i class="bi bi-plus-lg me-1" /> Add task
            </button>
          </div>
        </div>

        <p v-if="error" class="text-danger">{{ error }}</p>
        <p v-else-if="busy && !view" class="text-muted">Loading calendar…</p>

        <div v-if="view" id="calendar-grid" class="calendar-grid-wrap" :data-month="view.year_month">
          <div class="calendar-weekdays" aria-hidden="true">
            <span title="Sunday">S<span class="d-none d-sm-inline">un</span></span>
            <span title="Monday">M<span class="d-none d-sm-inline">on</span></span>
            <span title="Tuesday">T<span class="d-none d-sm-inline">ue</span></span>
            <span title="Wednesday">W<span class="d-none d-sm-inline">ed</span></span>
            <span title="Thursday">T<span class="d-none d-sm-inline">hu</span></span>
            <span title="Friday">F<span class="d-none d-sm-inline">ri</span></span>
            <span title="Saturday">S<span class="d-none d-sm-inline">at</span></span>
          </div>
          <div class="calendar-grid" role="grid" :aria-label="`Task calendar for ${view.month_label}`">
            <div v-for="(week, wIdx) in view.weeks" :key="wIdx" class="calendar-week" role="row">
              <div
                v-for="cell in week"
                :key="cell.date"
                class="calendar-cell"
                :class="{
                  'calendar-cell--muted': !cell.in_month,
                  'calendar-cell--today': cell.is_today,
                }"
                role="gridcell"
                :data-date="cell.date"
                :aria-label="cell.date"
              >
                <div class="calendar-cell-header">
                  <span class="calendar-day-num">{{ cell.day }}</span>
                  <button
                    v-if="cell.in_month"
                    type="button"
                    class="btn btn-link btn-sm calendar-add-btn p-0"
                    :title="`Add task on ${cell.date}`"
                    :aria-label="`Add task on ${cell.date}`"
                    @click="openAdd(cell.date)"
                  >
                    <i class="bi bi-plus-lg" />
                  </button>
                </div>
                <ul v-if="cell.tasks.length" class="calendar-task-list">
                  <li
                    v-for="task in cell.tasks"
                    :key="task.id"
                    class="calendar-task-item"
                    :class="[
                      dueClass(task.due, task.completed),
                      task.completed ? 'calendar-task--completed' : '',
                      !task.completed && task.priority === 3 ? 'calendar-task--high' : '',
                      !task.completed && task.priority === 2 ? 'calendar-task--med' : '',
                    ]"
                  >
                    <button
                      type="button"
                      class="calendar-task-complete"
                      :class="{ 'calendar-task-complete--done': task.completed }"
                      :title="task.completed ? 'Mark incomplete' : 'Mark complete'"
                      :aria-label="task.completed ? `Mark ${task.title} incomplete` : `Mark ${task.title} complete`"
                      :disabled="!!completing[task.id]"
                      @click="toggleComplete(task.id, task.completed)"
                    >
                      <i
                        class="bi"
                        :class="task.completed ? 'bi-check-circle-fill' : 'bi-circle'"
                        aria-hidden="true"
                      />
                    </button>
                    <button
                      type="button"
                      class="calendar-task-title"
                      :title="task.title"
                      @click="openEdit(task.id)"
                    >
                      <i
                        v-if="dueClass(task.due, task.completed) === 'due-overdue'"
                        class="bi bi-exclamation-circle"
                        aria-hidden="true"
                      />
                      {{ task.title }}
                    </button>
                    <span v-if="task.project_name" class="calendar-task-project">{{ task.project_name }}</span>
                  </li>
                </ul>
              </div>
            </div>
          </div>
        </div>

        <div class="calendar-footer mt-4 d-flex flex-wrap justify-content-between align-items-center gap-2 text-muted small">
          <span>
            <i class="bi bi-circle-fill calendar-legend-overdue" /> Overdue &nbsp;
            <i class="bi bi-circle-fill calendar-legend-today" /> Due today &nbsp;
            <i class="bi bi-check-circle-fill calendar-legend-completed" /> Completed
          </span>
          <RouterLink to="/settings#integrations">Subscribe or sync via ICS on Profile</RouterLink>
        </div>
      </div>

      <!-- Clean Reusable Footer inside right content area -->
      <AppFooter />
    </div>
  </div>
</template>
