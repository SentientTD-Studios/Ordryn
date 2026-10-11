<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import { api } from '@/api/client'
import type { Project, ProjectInsights, SavedView } from '@/api/types'
import { APIError } from '@/api/types'
import ModernSidebar from '@/components/modern/ModernSidebar.vue'
import AppFooter from '@/components/AppFooter.vue'
import { useAuth } from '@/composables/useAuth'
import { useSidebarState } from '@/composables/useSidebarState'
import { useLiveUpdates, isOwnFocusedLiveEvent } from '@/composables/useLiveUpdates'

const LAST_PROJECT_KEY = 'ordryn.teamDashboardProject'

const router = useRouter()
const route = useRoute()
const { user } = useAuth()
const { sidebarCollapsed, toggleSidebar } = useSidebarState()

const projects = ref<Project[]>([])
const savedViews = ref<SavedView[]>([])
const insights = ref<ProjectInsights | null>(null)
const loading = ref(true)
const error = ref('')
const projectId = ref<number | null>(null)
const sprintId = ref<number | null>(null)
const unit = ref<'tasks' | 'points'>('tasks')

const activeProjects = computed(() => projects.value.filter((p) => !p.archived))

function selectHome() {
  void router.push('/')
}
function selectProject(id: string) {
  void router.push({ path: '/', query: { project: id } })
}
function selectView(id: string) {
  void router.push({ path: '/', query: { view: id } })
}

function rememberProject(id: number) {
  try {
    localStorage.setItem(LAST_PROJECT_KEY, String(id))
  } catch {
    // Remembering the project is a convenience only.
  }
}

function initialProjectId(): number | null {
  const fromQuery = Number(route.query.project)
  if (fromQuery && activeProjects.value.some((p) => p.id === fromQuery)) return fromQuery
  try {
    const saved = Number(localStorage.getItem(LAST_PROJECT_KEY))
    if (saved && activeProjects.value.some((p) => p.id === saved)) return saved
  } catch {
    // Fall through to the first project.
  }
  const kanban = activeProjects.value.find((p) => p.workflow_mode === 'kanban')
  return (kanban ?? activeProjects.value[0])?.id ?? null
}

async function loadInsights() {
  if (projectId.value == null) {
    insights.value = null
    return
  }
  error.value = ''
  try {
    insights.value = await api.projectInsights(projectId.value, sprintId.value ?? undefined)
    if (!sprintId.value && insights.value.burndown) sprintId.value = insights.value.burndown.sprint.id
  } catch (err) {
    error.value = err instanceof APIError ? err.message : 'Could not load the team dashboard'
    insights.value = null
  }
}

async function onProjectChange() {
  if (projectId.value == null) return
  rememberProject(projectId.value)
  sprintId.value = null
  void router.replace({ query: { ...route.query, project: String(projectId.value) } })
  await loadInsights()
}

onMounted(async () => {
  try {
    const [projList, viewList] = await Promise.all([api.listProjects(), api.listSavedViews()])
    projects.value = projList
    savedViews.value = viewList
    projectId.value = initialProjectId()
    await loadInsights()
  } catch (err) {
    error.value = err instanceof APIError ? err.message : 'Could not load projects'
  } finally {
    loading.value = false
  }
})

watch(sprintId, (next, prev) => {
  // The first load fills sprintId from the server's choice; only reload on a user pick.
  if (prev != null && next != null && next !== insights.value?.burndown?.sprint.id) void loadInsights()
})

useLiveUpdates((event, batch) => {
  if (batch.every((e) => e.type === 'task.commented')) return
  if (event.type === 'notification.created' || event.type === 'extension.store') return
  if (isOwnFocusedLiveEvent(event, user.value?.id)) return
  void loadInsights()
}, 1500)

// ---- Throughput ----
const throughputMax = computed(() => {
  const vals = (insights.value?.throughput ?? []).map((w) => (unit.value === 'points' ? w.points : w.tasks))
  return Math.max(1, ...vals)
})
const throughputAvg = computed(() => {
  const weeks = insights.value?.throughput ?? []
  // Leave out the current, unfinished week.
  const full = weeks.slice(0, -1)
  if (!full.length) return 0
  const sum = full.reduce((n, w) => n + (unit.value === 'points' ? w.points : w.tasks), 0)
  return Math.round((sum / full.length) * 10) / 10
})

function weekLabel(iso: string): string {
  const [y, m, d] = iso.split('-').map(Number)
  return new Date(y, m - 1, d).toLocaleDateString(undefined, { month: 'short', day: 'numeric' })
}

// ---- Members ----
const memberMax = computed(() =>
  Math.max(1, ...(insights.value?.members ?? []).map((m) => Math.max(m.claimed_open, m.completed))),
)
const visibleMembers = computed(() =>
  (insights.value?.members ?? []).filter((m) => m.is_member || m.claimed_open || m.completed),
)

// ---- Burndown chart (inline SVG) ----
const CHART_W = 640
const CHART_H = 240
const PAD = { top: 12, right: 12, bottom: 28, left: 36 }

const burndown = computed(() => insights.value?.burndown ?? null)
const burndownTotal = computed(() =>
  burndown.value ? (unit.value === 'points' ? burndown.value.total_points : burndown.value.total_tasks) : 0,
)
const usePointsFallback = computed(() => unit.value === 'points' && burndown.value != null && burndown.value.total_points === 0)

const chart = computed(() => {
  const bd = burndown.value
  if (!bd || !bd.days.length) return null
  const yMax = Math.max(1, burndownTotal.value)
  const n = bd.days.length
  const plotW = CHART_W - PAD.left - PAD.right
  const plotH = CHART_H - PAD.top - PAD.bottom
  const x = (i: number) => PAD.left + (n === 1 ? plotW / 2 : (i / (n - 1)) * plotW)
  const y = (v: number) => PAD.top + plotH - (v / yMax) * plotH
  const ideal = bd.days
    .map((d, i) => `${x(i)},${y(unit.value === 'points' ? d.ideal_points : d.ideal_tasks)}`)
    .join(' ')
  const actualPts: { x: number; y: number; date: string; value: number }[] = []
  bd.days.forEach((d, i) => {
    const v = unit.value === 'points' ? d.remaining_points : d.remaining_tasks
    if (v != null) actualPts.push({ x: x(i), y: y(v), date: d.date, value: v })
  })
  const ticks = [0, Math.round(yMax / 2), yMax].filter((v, i, a) => a.indexOf(v) === i)
  const labelEvery = Math.max(1, Math.ceil(n / 7))
  const xLabels = bd.days
    .map((d, i) => ({ x: x(i), label: weekLabel(d.date), i }))
    .filter((l) => l.i % labelEvery === 0 || l.i === n - 1)
  return {
    ideal,
    actual: actualPts.map((p) => `${p.x},${p.y}`).join(' '),
    points: actualPts,
    yTicks: ticks.map((v) => ({ v, y: y(v) })),
    xLabels,
    baseline: PAD.top + plotH,
  }
})

const burndownStatus = computed(() => {
  const bd = burndown.value
  if (!bd) return ''
  const lastActual = [...bd.days].reverse().find((d) => d.remaining_tasks != null)
  if (!lastActual) return 'Sprint has not started yet.'
  const rem = unit.value === 'points' ? lastActual.remaining_points! : lastActual.remaining_tasks!
  const ideal = unit.value === 'points' ? lastActual.ideal_points : lastActual.ideal_tasks
  const label = unit.value === 'points' ? 'points' : rem === 1 ? 'task' : 'tasks'
  const delta = Math.round((rem - ideal) * 10) / 10
  const pace = delta > 0.5 ? `${delta} behind the ideal line` : delta < -0.5 ? `${-delta} ahead of the ideal line` : 'on the ideal line'
  return `${rem} ${label} left, ${pace}.`
})
</script>

<template>
  <div class="ordryn-main-layout">
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

    <div class="flex-grow-1 p-3 p-md-4 overflow-auto d-flex flex-column justify-content-between">
      <div data-page="team">
        <div class="d-flex flex-wrap justify-content-between align-items-center gap-3 mb-3">
          <div>
            <h1 class="h3 fw-bold mb-1">Team dashboard</h1>
            <p class="text-muted small mb-0">How a project's work is moving and who is carrying it.</p>
          </div>
          <div class="d-flex flex-wrap align-items-center gap-2">
            <label class="visually-hidden" for="team-project">Project</label>
            <select
              id="team-project"
              v-model.number="projectId"
              class="form-select form-select-sm"
              style="min-width: 12rem;"
              :disabled="!activeProjects.length"
              @change="onProjectChange"
            >
              <option v-for="p in activeProjects" :key="p.id" :value="p.id">{{ p.name }}</option>
            </select>
            <div class="btn-group btn-group-sm" role="group" aria-label="Measure in">
              <button
                type="button"
                class="btn"
                :class="unit === 'tasks' ? 'btn-primary' : 'btn-outline-secondary'"
                :aria-pressed="unit === 'tasks'"
                @click="unit = 'tasks'"
              >
                Tasks
              </button>
              <button
                type="button"
                class="btn"
                :class="unit === 'points' ? 'btn-primary' : 'btn-outline-secondary'"
                :aria-pressed="unit === 'points'"
                @click="unit = 'points'"
              >
                Points
              </button>
            </div>
            <RouterLink to="/dashboard" class="btn btn-sm btn-outline-secondary">My dashboard</RouterLink>
          </div>
        </div>

        <p v-if="loading" class="text-muted">Loading…</p>
        <p v-else-if="error" class="text-danger">{{ error }}</p>
        <div v-else-if="!activeProjects.length" class="card border-0 shadow-xs">
          <div class="card-body text-muted">
            The team dashboard covers one project at a time. <RouterLink to="/projects">Create or join a project</RouterLink> to see it.
          </div>
        </div>

        <template v-else-if="insights">
          <div class="row g-3 mb-4">
            <div class="col-6 col-lg">
              <div class="card text-center border-0 shadow-xs h-100">
                <div class="card-body py-3">
                  <div class="text-muted small fw-medium">Open</div>
                  <div class="display-6 fw-bold">{{ insights.summary.open }}</div>
                  <div v-if="insights.summary.open_points" class="small text-muted">{{ insights.summary.open_points }} pts</div>
                </div>
              </div>
            </div>
            <div class="col-6 col-lg">
              <div class="card text-center border-0 shadow-xs h-100">
                <div class="card-body py-3">
                  <div class="text-muted small fw-medium">Done</div>
                  <div class="display-6 fw-bold text-success">{{ insights.summary.done }}</div>
                </div>
              </div>
            </div>
            <div class="col-6 col-lg">
              <div class="card text-center border-0 shadow-xs h-100">
                <div class="card-body py-3">
                  <div class="text-muted small fw-medium">Overdue</div>
                  <div class="display-6 fw-bold text-danger">{{ insights.summary.overdue }}</div>
                </div>
              </div>
            </div>
            <div v-if="insights.workflow_mode === 'kanban'" class="col-6 col-lg">
              <div class="card text-center border-0 shadow-xs h-100">
                <div class="card-body py-3">
                  <div class="text-muted small fw-medium">Unclaimed</div>
                  <div class="display-6 fw-bold text-warning">{{ insights.summary.unclaimed }}</div>
                </div>
              </div>
            </div>
            <div class="col-6 col-lg">
              <div class="card text-center border-0 shadow-xs h-100">
                <div class="card-body py-3">
                  <div class="text-muted small fw-medium">Weekly average</div>
                  <div class="display-6 fw-bold text-primary">{{ throughputAvg }}</div>
                  <div class="small text-muted">{{ unit }} done, last 7 weeks</div>
                </div>
              </div>
            </div>
          </div>

          <div v-if="insights.workflow_mode === 'kanban'" class="card border-0 shadow-xs mb-4">
            <div class="card-header bg-transparent border-0 pt-3 pb-0 d-flex flex-wrap justify-content-between align-items-center gap-2">
              <h2 class="h5 fw-bold mb-0">Sprint burndown</h2>
              <select
                v-if="insights.sprints.length"
                v-model.number="sprintId"
                class="form-select form-select-sm w-auto"
                aria-label="Sprint"
              >
                <option v-for="s in insights.sprints" :key="s.id" :value="s.id">
                  {{ s.name }} ({{ weekLabel(s.start_date) }} – {{ weekLabel(s.end_date) }})
                </option>
              </select>
            </div>
            <div class="card-body">
              <p v-if="!burndown" class="text-muted mb-0">
                Add start and end dates to a sprint in project settings to see its burndown.
              </p>
              <template v-else>
                <p class="small mb-2">
                  <strong>{{ burndown.sprint.name }}</strong>:
                  {{ burndownTotal }} {{ unit }} in scope. {{ burndownStatus }}
                </p>
                <p v-if="usePointsFallback" class="small text-muted">No tasks in this sprint have estimates, so there are no points to chart.</p>
                <svg
                  v-if="chart"
                  class="burndown-chart w-100"
                  :viewBox="`0 0 ${CHART_W} ${CHART_H}`"
                  role="img"
                  :aria-label="`Burndown for ${burndown.sprint.name}. ${burndownStatus}`"
                >
                  <g class="burndown-grid">
                    <template v-for="t in chart.yTicks" :key="t.v">
                      <line :x1="PAD.left" :x2="CHART_W - PAD.right" :y1="t.y" :y2="t.y" />
                      <text :x="PAD.left - 6" :y="t.y + 4" text-anchor="end">{{ t.v }}</text>
                    </template>
                    <text
                      v-for="l in chart.xLabels"
                      :key="l.i"
                      :x="l.x"
                      :y="chart.baseline + 18"
                      text-anchor="middle"
                    >{{ l.label }}</text>
                  </g>
                  <polyline class="burndown-ideal" :points="chart.ideal" />
                  <polyline v-if="chart.points.length > 1" class="burndown-actual" :points="chart.actual" />
                  <circle v-for="p in chart.points" :key="p.date" class="burndown-dot" :cx="p.x" :cy="p.y" r="3.5">
                    <title>{{ weekLabel(p.date) }}: {{ p.value }} {{ unit }} left</title>
                  </circle>
                </svg>
                <div class="small text-muted d-flex flex-wrap gap-3 mt-1">
                  <span><span class="burndown-key burndown-key-actual" /> Remaining</span>
                  <span><span class="burndown-key burndown-key-ideal" /> Ideal</span>
                  <span>Uses the sprint's current tasks, so work added mid-sprint counts from day one.</span>
                </div>
              </template>
            </div>
          </div>

          <div class="row g-4 mb-2">
            <div :class="insights.can_view_members ? 'col-lg-6' : 'col-12'">
              <div class="card border-0 shadow-xs h-100">
                <div class="card-header bg-transparent border-0 pt-3 pb-0">
                  <h2 class="h5 fw-bold mb-0">Throughput</h2>
                  <p class="small text-muted mb-0">{{ unit === 'points' ? 'Points' : 'Tasks' }} completed per week</p>
                </div>
                <div class="card-body">
                  <div class="throughput-bars" role="list">
                    <div
                      v-for="(w, i) in insights.throughput"
                      :key="w.week_start"
                      class="throughput-col"
                      role="listitem"
                      :aria-label="`Week of ${weekLabel(w.week_start)}: ${unit === 'points' ? w.points : w.tasks} ${unit}`"
                    >
                      <span class="throughput-value">{{ unit === 'points' ? w.points : w.tasks }}</span>
                      <div class="throughput-track">
                        <div
                          class="throughput-bar"
                          :class="{ 'is-current': i === insights.throughput.length - 1 }"
                          :style="{ height: `${((unit === 'points' ? w.points : w.tasks) / throughputMax) * 100}%` }"
                        />
                      </div>
                      <span class="throughput-label">{{ weekLabel(w.week_start) }}</span>
                    </div>
                  </div>
                  <p class="small text-muted mb-0 mt-2">The last bar is the current week so far.</p>
                </div>
              </div>
            </div>
            <!-- Hidden unless the viewer's role has insights:people. -->
            <div v-if="insights.can_view_members" class="col-lg-6">
              <div class="card border-0 shadow-xs h-100">
                <div class="card-header bg-transparent border-0 pt-3 pb-0">
                  <h2 class="h5 fw-bold mb-0">People</h2>
                  <p class="small text-muted mb-0">
                    <template v-if="insights.workflow_mode === 'kanban'">Open tasks claimed now, and</template>
                    <template v-else>Tasks</template>
                    completed in the last {{ insights.completed_days }} days
                  </p>
                </div>
                <ul class="list-group list-group-flush">
                  <li v-for="m in visibleMembers" :key="m.user_id" class="list-group-item">
                    <div class="d-flex justify-content-between align-items-center gap-2 mb-1">
                      <span class="fw-medium text-truncate">
                        {{ m.name }}
                        <i v-if="m.is_agent" class="bi bi-robot text-muted ms-1" title="Agent" />
                        <span v-if="!m.is_member" class="badge text-bg-light border ms-1">Left project</span>
                      </span>
                      <span class="small text-muted text-nowrap">
                        <template v-if="insights.workflow_mode === 'kanban'">
                          {{ m.claimed_open }} claimed<template v-if="m.claimed_points"> ({{ m.claimed_points }} pts)</template> ·
                        </template>
                        {{ m.completed }} done
                      </span>
                    </div>
                    <div v-if="insights.workflow_mode === 'kanban'" class="member-bar" aria-hidden="true">
                      <div class="member-bar-fill member-bar-claimed" :style="{ width: `${(m.claimed_open / memberMax) * 100}%` }" />
                    </div>
                    <div class="member-bar mt-1" aria-hidden="true">
                      <div class="member-bar-fill member-bar-done" :style="{ width: `${(m.completed / memberMax) * 100}%` }" />
                    </div>
                  </li>
                  <li v-if="!visibleMembers.length" class="list-group-item text-muted">No members yet.</li>
                </ul>
              </div>
            </div>
          </div>
        </template>
      </div>

      <AppFooter />
    </div>
  </div>
</template>

<style scoped>
.burndown-chart {
  max-height: 280px;
  overflow: visible;
}
.burndown-grid line {
  stroke: var(--bs-border-color);
  stroke-width: 1;
}
.burndown-grid text {
  fill: var(--bs-secondary-color);
  font-size: 11px;
}
.burndown-ideal {
  fill: none;
  stroke: var(--bs-secondary-color);
  stroke-width: 1.5;
  stroke-dasharray: 5 4;
}
.burndown-actual {
  fill: none;
  stroke: var(--bs-primary);
  stroke-width: 2.5;
  stroke-linejoin: round;
}
.burndown-dot {
  fill: var(--bs-primary);
}
.burndown-key {
  display: inline-block;
  width: 1.25rem;
  vertical-align: middle;
  border-top: 2.5px solid var(--bs-primary);
}
.burndown-key-ideal {
  border-top: 1.5px dashed var(--bs-secondary-color);
}
.throughput-bars {
  display: flex;
  align-items: stretch;
  gap: 0.5rem;
  height: 180px;
}
.throughput-col {
  flex: 1 1 0;
  min-width: 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 0.25rem;
}
.throughput-track {
  flex: 1 1 auto;
  width: 100%;
  display: flex;
  align-items: flex-end;
}
.throughput-bar {
  width: 100%;
  min-height: 2px;
  border-radius: 0.25rem 0.25rem 0 0;
  background: var(--bs-success);
  opacity: 0.85;
}
.throughput-bar.is-current {
  opacity: 0.45;
}
.throughput-value {
  font-size: 0.75rem;
  font-weight: 600;
}
.throughput-label {
  font-size: 0.7rem;
  color: var(--bs-secondary-color);
  white-space: nowrap;
}
.member-bar {
  height: 6px;
  border-radius: 3px;
  background: var(--bs-tertiary-bg);
  overflow: hidden;
}
.member-bar-fill {
  height: 100%;
  border-radius: 3px;
}
.member-bar-claimed {
  background: var(--bs-warning);
}
.member-bar-done {
  background: var(--bs-success);
}
</style>
