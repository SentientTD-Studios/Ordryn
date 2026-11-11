<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { api } from '@/api/client'
import { APIError, type Planner, type PlannerTask, type Project, type SavedView } from '@/api/types'
import ModernSidebar from '@/components/modern/ModernSidebar.vue'
import AppFooter from '@/components/AppFooter.vue'
import { useAuth } from '@/composables/useAuth'
import { useTaskSidebar } from '@/composables/useTaskSidebar'
import { useLiveUpdates, isOwnFocusedLiveEvent } from '@/composables/useLiveUpdates'
import { useToast } from '@/composables/useToast'
import { useSidebarState } from '@/composables/useSidebarState'
import { dayLabel, snoozeOptions } from '@/utils/planner'

const router = useRouter()
const { user } = useAuth()
const { openEdit, lastSavedTask, lastDeletedTask } = useTaskSidebar()
const { push } = useToast()
const { sidebarCollapsed, toggleSidebar } = useSidebarState()

const DAYS_KEY = 'gotodo.plannerDays'
function readDays(): number {
  try {
    const n = Number(localStorage.getItem(DAYS_KEY))
    if (n === 7 || n === 14 || n === 30) return n
  } catch {
    /* ignore */
  }
  return 14
}

const days = ref(readDays())
const planner = ref<Planner | null>(null)
const projects = ref<Project[]>([])
const savedViews = ref<SavedView[]>([])
const loading = ref(false)
const error = ref('')
const busy = ref<Record<number, boolean>>({})
const showSnoozed = ref(false)
const snoozeMenuFor = ref<number | null>(null)
const customSnooze = ref('')

async function load() {
  loading.value = true
  error.value = ''
  try {
    planner.value = await api.planner(days.value)
  } catch (err) {
    error.value = err instanceof APIError ? err.message : 'Failed to load your planner'
  } finally {
    loading.value = false
  }
}

async function loadNav() {
  try {
    const [p, v] = await Promise.all([api.listProjects(), api.listSavedViews()])
    projects.value = p
    savedViews.value = v
  } catch {
    /* the sidebar still works without them */
  }
}

function setDays(n: number) {
  days.value = n
  try {
    localStorage.setItem(DAYS_KEY, String(n))
  } catch {
    /* ignore */
  }
  void load()
}

const todayCount = computed(() => {
  const p = planner.value
  if (!p) return 0
  return p.overdue.length + p.due_today.length + p.woken.length
})

const options = computed(() => (planner.value ? snoozeOptions(planner.value.today) : []))

async function withBusy(id: number, fn: () => Promise<unknown>, ok: string, fail: string) {
  if (busy.value[id]) return
  busy.value = { ...busy.value, [id]: true }
  try {
    await fn()
    push(ok, 'success')
    await load()
  } catch (err) {
    push(err instanceof APIError ? err.message : fail, 'error')
  } finally {
    const next = { ...busy.value }
    delete next[id]
    busy.value = next
  }
}

function complete(t: PlannerTask) {
  void withBusy(t.id, () => api.patchTask(t.id, { completed: true }), 'Task completed', 'Could not complete the task')
}

function snooze(t: PlannerTask, until: string) {
  snoozeMenuFor.value = null
  customSnooze.value = ''
  if (!until) return
  void withBusy(t.id, () => api.snoozeTask(t.id, until), `Snoozed until ${dayLabel(until, planner.value!.today)}`, 'Could not snooze the task')
}

function unsnooze(t: PlannerTask, ok = 'Back in Today') {
  void withBusy(t.id, () => api.unsnoozeTask(t.id), ok, 'Could not update the snooze')
}

function toggleSnoozeMenu(id: number) {
  snoozeMenuFor.value = snoozeMenuFor.value === id ? null : id
  customSnooze.value = ''
}

function selectProject(id: string) {
  void router.push({ path: '/', query: { project: id } })
}

onMounted(() => {
  void load()
  void loadNav()
})

watch(lastSavedTask, () => void load())
watch(lastDeletedTask, (payload) => {
  if (!payload) return
  lastDeletedTask.value = null
  void load()
})

useLiveUpdates((event, batch) => {
  if (batch.every((e) => e.type === 'task.commented')) return
  if (isOwnFocusedLiveEvent(event, user.value?.id)) return
  void load()
}, 500)

const priorityLabel = ['', 'Low', 'Medium', 'High']
</script>

<template>
  <div class="ordryn-main-layout">
    <ModernSidebar
      :collapsed="sidebarCollapsed"
      :projects="projects"
      :saved-views="savedViews"
      @toggle-collapse="toggleSidebar"
      @select-home="router.push('/')"
      @select-project="selectProject"
      @add-project="router.push('/projects')"
      @edit-project="() => router.push('/projects')"
      @add-view="router.push('/views/new')"
    />

    <div class="flex-grow-1 p-3 p-md-4 overflow-auto d-flex flex-column justify-content-between">
      <div class="planner-page" data-page="planner">
        <div class="d-flex flex-wrap justify-content-between align-items-center gap-3 mb-3">
          <div>
            <h1 class="h3 fw-bold mb-1">Planner</h1>
            <p class="text-muted mb-0 small">
              Your open tasks by day. Snooze a task to hide it from Today until a later day; only you see snoozes.
            </p>
          </div>
          <div class="btn-group btn-group-sm" role="group" aria-label="How far ahead">
            <button
              v-for="n in [7, 14, 30]"
              :key="n"
              type="button"
              class="btn btn-outline-secondary"
              :class="{ active: days === n }"
              :aria-pressed="days === n"
              @click="setDays(n)"
            >
              {{ n }} days
            </button>
          </div>
        </div>

        <div v-if="error" class="alert alert-danger py-2">{{ error }}</div>
        <div v-else-if="loading && !planner" class="text-center py-5 text-muted">
          <span class="spinner-border spinner-border-sm me-2" role="status" />Loading your planner…
        </div>

        <template v-else-if="planner">
          <section class="mb-4" aria-labelledby="planner-today">
            <h2 id="planner-today" class="h5 fw-semibold mb-2">
              Today <span class="badge text-bg-secondary ms-1">{{ todayCount }}</span>
            </h2>
            <p v-if="!todayCount" class="text-muted small mb-0">Nothing due today. Nice.</p>

            <template v-for="group in [
              { key: 'overdue', label: 'Overdue', tasks: planner.overdue },
              { key: 'today', label: 'Due today', tasks: planner.due_today },
              { key: 'woken', label: 'Back from snooze', tasks: planner.woken },
            ]" :key="group.key">
              <div v-if="group.tasks.length" class="mb-3">
                <h3 class="h6 text-uppercase small text-muted mb-1" :class="{ 'text-danger': group.key === 'overdue' }">
                  {{ group.label }}
                </h3>
                <ul class="list-group planner-list">
                  <li v-for="t in group.tasks" :key="t.id" class="list-group-item d-flex align-items-center gap-2 flex-wrap">
                    <button
                      type="button"
                      class="btn btn-sm btn-outline-success planner-check"
                      :disabled="busy[t.id]"
                      :aria-label="`Complete ${t.title}`"
                      title="Complete"
                      @click="complete(t)"
                    >
                      <i class="bi bi-check-lg" />
                    </button>
                    <button type="button" class="planner-title btn btn-link p-0 text-start text-truncate" @click="openEdit(t.id)">
                      {{ t.title }}
                    </button>
                    <span v-if="t.project" class="badge text-bg-light border">{{ t.project }}</span>
                    <span v-if="t.priority" class="small text-muted">{{ priorityLabel[t.priority] }}</span>
                    <span v-if="t.due_date && group.key !== 'today'" class="small" :class="group.key === 'overdue' ? 'text-danger' : 'text-muted'">
                      due {{ dayLabel(t.due_date.slice(0, 10), planner.today) }}
                    </span>
                    <span class="ms-auto d-flex gap-1 position-relative">
                      <button
                        v-if="group.key === 'woken'"
                        type="button"
                        class="btn btn-sm btn-outline-secondary"
                        :disabled="busy[t.id]"
                        title="Remove from Today"
                        @click="unsnooze(t, 'Removed from Today')"
                      >
                        Dismiss
                      </button>
                      <button
                        type="button"
                        class="btn btn-sm btn-outline-secondary"
                        :disabled="busy[t.id]"
                        :aria-expanded="snoozeMenuFor === t.id"
                        @click="toggleSnoozeMenu(t.id)"
                      >
                        <i class="bi bi-alarm me-1" />Snooze
                      </button>
                      <div v-if="snoozeMenuFor === t.id" class="planner-snooze-menu card shadow-sm p-2" role="menu">
                        <button
                          v-for="o in options"
                          :key="o.label"
                          type="button"
                          class="dropdown-item small rounded"
                          role="menuitem"
                          @click="snooze(t, o.until)"
                        >
                          {{ o.label }} <span class="text-muted">· {{ dayLabel(o.until, planner.today) }}</span>
                        </button>
                        <form class="d-flex gap-1 mt-1" @submit.prevent="snooze(t, customSnooze)">
                          <input
                            v-model="customSnooze"
                            type="date"
                            class="form-control form-control-sm"
                            :min="options[0]?.until"
                            aria-label="Snooze until"
                          />
                          <button type="submit" class="btn btn-sm btn-primary" :disabled="!customSnooze">Set</button>
                        </form>
                      </div>
                    </span>
                  </li>
                </ul>
              </div>
            </template>
          </section>

          <section class="mb-4" aria-labelledby="planner-upcoming">
            <h2 id="planner-upcoming" class="h5 fw-semibold mb-2">Upcoming</h2>
            <p v-if="!planner.upcoming.length" class="text-muted small mb-0">Nothing due in the next {{ planner.days }} days.</p>
            <div v-for="d in planner.upcoming" :key="d.date" class="mb-3">
              <h3 class="h6 small fw-semibold mb-1">{{ dayLabel(d.date, planner.today) }}</h3>
              <ul class="list-group planner-list">
                <li v-for="t in d.tasks" :key="t.id" class="list-group-item d-flex align-items-center gap-2">
                  <button
                    type="button"
                    class="btn btn-sm btn-outline-success planner-check"
                    :disabled="busy[t.id]"
                    :aria-label="`Complete ${t.title}`"
                    title="Complete"
                    @click="complete(t)"
                  >
                    <i class="bi bi-check-lg" />
                  </button>
                  <button type="button" class="planner-title btn btn-link p-0 text-start text-truncate" @click="openEdit(t.id)">
                    {{ t.title }}
                  </button>
                  <span v-if="t.project" class="badge text-bg-light border">{{ t.project }}</span>
                  <span v-if="t.priority" class="small text-muted ms-auto">{{ priorityLabel[t.priority] }}</span>
                </li>
              </ul>
            </div>
          </section>

          <section v-if="planner.snoozed.length" aria-labelledby="planner-snoozed">
            <h2 id="planner-snoozed" class="h6 mb-2">
              <button
                type="button"
                class="btn btn-link p-0 text-decoration-none"
                :aria-expanded="showSnoozed"
                @click="showSnoozed = !showSnoozed"
              >
                <i class="bi" :class="showSnoozed ? 'bi-chevron-down' : 'bi-chevron-right'" />
                Snoozed ({{ planner.snoozed.length }})
              </button>
            </h2>
            <ul v-if="showSnoozed" class="list-group planner-list">
              <li v-for="t in planner.snoozed" :key="t.id" class="list-group-item d-flex align-items-center gap-2">
                <button type="button" class="planner-title btn btn-link p-0 text-start text-truncate" @click="openEdit(t.id)">
                  {{ t.title }}
                </button>
                <span class="small text-muted">until {{ dayLabel(t.snoozed_until || '', planner.today) }}</span>
                <button
                  type="button"
                  class="btn btn-sm btn-outline-secondary ms-auto"
                  :disabled="busy[t.id]"
                  @click="unsnooze(t)"
                >
                  Wake now
                </button>
              </li>
            </ul>
          </section>
        </template>
      </div>
      <AppFooter />
    </div>
  </div>
</template>

<style scoped>
.planner-page {
  max-width: 900px;
}
.planner-title {
  color: var(--ordryn-text);
  max-width: min(60ch, 100%);
}
.planner-check {
  width: 30px;
  height: 30px;
  padding: 0;
  border-radius: 50%;
}
.planner-snooze-menu {
  position: absolute;
  right: 0;
  top: 100%;
  z-index: 20;
  min-width: 240px;
  background: var(--ordryn-card-bg);
  border-color: var(--ordryn-card-border);
}
.planner-list .list-group-item {
  background: var(--ordryn-card-bg);
  border-color: var(--ordryn-card-border);
}
</style>
