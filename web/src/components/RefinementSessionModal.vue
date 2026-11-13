<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { RouterLink } from 'vue-router'
import { api } from '@/api/client'
import type {
  Project,
  ProjectSprint,
  RefinementAction,
  RefinementSession,
  RefinementSessionSummary,
  Task,
} from '@/api/types'
import { APIError } from '@/api/types'
import RichBody from '@/components/RichBody.vue'
import WysiwygEditor from '@/components/WysiwygEditor.vue'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import { useBodyScrollLock } from '@/composables/useBodyScrollLock'
import { useLiveUpdates, LIVE_CEREMONY_UPDATED, LIVE_RESYNC, type LiveEvent } from '@/composables/useLiveUpdates'
import { hasProjectPerm, PROJECT_PERMS } from '@/utils/projectPerms'
import { taskPageRoute } from '@/utils/projectRoute'
import { useTaskTextLimits } from '@/utils/taskText'
import { ESTIMATE_STEP, MAX_ESTIMATE_HOURS, formatHours, parseEstimateInput, refinementLabel } from '@/utils/estimates'
import {
  parseTaskIdList,
  queueItemState,
  scopeOptions,
  sessionProgress,
  startInputFor,
  type RefinementScope,
} from '@/utils/refinementSession'

const props = defineProps<{
  open: boolean
  project: Project
  sprints: ProjectSprint[]
  canFacilitate: boolean
}>()

// tasks-changed: this tab's own edits arrive as live events the focused board skips, so tell it directly.
const emit = defineEmits<{ close: []; 'active-changed': [active: boolean]; 'tasks-changed': [] }>()

const toast = useToast()
const { askConfirm } = useConfirm()
const { maxDescription } = useTaskTextLimits()
useBodyScrollLock(() => props.open)

const loading = ref(false)
const busy = ref(false)
const session = ref<RefinementSession | null>(null)
const history = ref<RefinementSessionSummary[]>([])

// Start form
const startTitle = ref('')
const startScope = ref<RefinementScope>('any')
const includeUnflagged = ref(false)

// Current task detail and inline edits
const detail = ref<Task | null>(null)
const detailLoading = ref(false)
const editingDescription = ref(false)
const descriptionDraft = ref('')
const estimateDraft = ref<number | ''>('')
const savingTask = ref(false)
const addIdsDraft = ref('')

const canEditTasks = computed(() => hasProjectPerm(props.project, PROJECT_PERMS.TASKS_EDIT))
const isFacilitator = computed(() => !!session.value?.is_facilitator)
const progress = computed(() => (session.value ? sessionProgress(session.value) : null))
const scopes = computed(() => scopeOptions(props.sprints.filter((s) => !s.is_locked || s.is_active)))
const currentSummary = computed(() =>
  session.value?.tasks.find((t) => t.id === session.value?.current_task_id) ?? null,
)
const estimateInvalid = computed(() => parseEstimateInput(estimateDraft.value) === undefined)
const estimateDirty = computed(
  () => !estimateInvalid.value && parseEstimateInput(estimateDraft.value) !== (detail.value?.estimate_hours ?? null),
)

function errMessage(err: unknown, fallback: string) {
  return err instanceof APIError ? err.message : fallback
}

async function loadSession() {
  try {
    const res = await api.getActiveRefinementSession(props.project.id)
    session.value = res.session
    emit('active-changed', !!res.session)
    if (!res.session) {
      history.value = await api.listRefinementSessions(props.project.id, 5).catch(() => [])
    }
  } catch (err) {
    toast.push(errMessage(err, 'Could not load the refinement session'), 'error')
  }
}

async function loadDetail(taskId: number | null) {
  if (!taskId) {
    detail.value = null
    return
  }
  detailLoading.value = detail.value?.id !== taskId
  try {
    const t = await api.getTask(taskId)
    // The facilitator may have moved on while this loaded.
    if (session.value?.current_task_id !== taskId) return
    detail.value = t
    if (!editingDescription.value) descriptionDraft.value = t.description || ''
    estimateDraft.value = t.estimate_hours ?? ''
  } catch {
    detail.value = null
  } finally {
    detailLoading.value = false
  }
}

async function refresh() {
  loading.value = !session.value
  await loadSession()
  loading.value = false
}

watch(
  () => props.open,
  (open) => {
    if (open) void refresh()
    else {
      editingDescription.value = false
      detail.value = null
    }
  },
  { immediate: true },
)

watch(
  () => session.value?.current_task_id ?? null,
  (id, prev) => {
    if (id !== prev) editingDescription.value = false
    void loadDetail(id)
  },
)

useLiveUpdates(
  (_last: LiveEvent, batch: LiveEvent[]) => {
    if (!props.open) return
    const sid = session.value?.id
    const sessionChanged = batch.some(
      (e) =>
        e.type === LIVE_RESYNC ||
        (e.type === LIVE_CEREMONY_UPDATED &&
          e.project_id === props.project.id &&
          (!sid || e.key === `refinement:${sid}` || !session.value)),
    )
    // A new session started elsewhere arrives with a key we don't know yet.
    const newSession = batch.some(
      (e) => e.type === LIVE_CEREMONY_UPDATED && e.project_id === props.project.id && e.key?.startsWith('refinement:'),
    )
    if (sessionChanged || newSession) void loadSession()
    const cur = session.value?.current_task_id
    if (cur && batch.some((e) => e.task_id === cur || e.type === LIVE_RESYNC)) void loadDetail(cur)
    else if (batch.some((e) => e.task_id && session.value?.queue.includes(e.task_id))) void loadSession()
  },
  150,
  { ceremonies: true },
)

async function startSession() {
  busy.value = true
  try {
    session.value = await api.startRefinementSession(
      props.project.id,
      startInputFor(startScope.value, { title: startTitle.value, includeUnflagged: includeUnflagged.value }),
    )
    emit('active-changed', true)
    startTitle.value = ''
    if (!session.value.queue.length) toast.push('No tasks matched. Add some by number to get going.', 'info')
  } catch (err) {
    toast.push(errMessage(err, 'Could not start the session'), 'error')
    await loadSession()
  } finally {
    busy.value = false
  }
}

async function act(action: RefinementAction) {
  const s = session.value
  if (!s || busy.value) return
  busy.value = true
  try {
    session.value = await api.refinementAction(props.project.id, s.id, s.revision, action)
    if (action.action === 'refine') emit('tasks-changed')
    if (session.value.status === 'ended') {
      toast.push('Refinement session ended', 'success')
      session.value = null
      emit('active-changed', false)
      await loadSession()
    }
  } catch (err) {
    if (err instanceof APIError && err.status === 409) {
      toast.push('The session moved on. Showing the latest.', 'info')
    } else {
      toast.push(errMessage(err, 'That step did not go through'), 'error')
    }
    await loadSession()
  } finally {
    busy.value = false
  }
}

async function endSession() {
  const p = progress.value
  const ok = await askConfirm({
    title: 'End refinement session?',
    message: p && p.remaining > 0
      ? `${p.remaining} task${p.remaining === 1 ? '' : 's'} were not covered. They keep their current flag.`
      : 'Everyone in the session will see it end.',
    confirmLabel: 'End session',
  })
  if (ok) await act({ action: 'end' })
}

async function addTasks() {
  const ids = parseTaskIdList(addIdsDraft.value)
  if (!ids.length) return
  await act({ action: 'add', task_ids: ids })
  addIdsDraft.value = ''
}

async function saveEstimate() {
  const t = detail.value
  const next = parseEstimateInput(estimateDraft.value)
  if (!t || next === undefined || !estimateDirty.value) return
  savingTask.value = true
  try {
    detail.value = await api.patchTask(t.id, { estimate_hours: next })
    emit('tasks-changed')
    toast.push('Estimate saved', 'success')
  } catch (err) {
    toast.push(errMessage(err, 'Could not save the estimate'), 'error')
  } finally {
    savingTask.value = false
  }
}

async function saveDescription() {
  const t = detail.value
  if (!t) return
  savingTask.value = true
  try {
    detail.value = await api.patchTask(t.id, { description: descriptionDraft.value })
    emit('tasks-changed')
    editingDescription.value = false
    toast.push('Description saved', 'success')
  } catch (err) {
    toast.push(errMessage(err, 'Could not save the description'), 'error')
  } finally {
    savingTask.value = false
  }
}

function startEditingDescription() {
  descriptionDraft.value = detail.value?.description || ''
  editingDescription.value = true
}

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape' && !editingDescription.value) emit('close')
}

function sessionDate(iso: string) {
  return new Date(iso).toLocaleDateString(undefined, { month: 'short', day: 'numeric' })
}
</script>

<template>
  <div
    v-if="open"
    class="modal fade show d-block refinement-modal"
    style="background: rgba(0, 0, 0, 0.5);"
    tabindex="-1"
    role="dialog"
    aria-modal="true"
    aria-labelledby="refinement-modal-title"
    @click.self="emit('close')"
    @keydown="onKeydown"
  >
    <div class="modal-dialog modal-dialog-centered modal-xl refinement-dialog">
      <div class="modal-content border-0 shadow" style="background: var(--ordryn-card-bg); color: var(--ordryn-text);">
        <!-- Header -->
        <div class="modal-header gap-3 flex-wrap">
          <div class="min-w-0 me-auto">
            <h5 id="refinement-modal-title" class="modal-title fw-bold mb-0 d-flex align-items-center gap-2">
              <i class="bi bi-list-check" aria-hidden="true" />
              <span class="text-truncate">{{ session ? session.title : 'Refinement' }}</span>
              <span v-if="session" class="badge text-bg-success ceremony-live">
                <span class="ceremony-live-dot" aria-hidden="true" />Live
              </span>
            </h5>
            <div class="small text-muted text-truncate">
              {{ project.name }}<template v-if="session?.sprint_name"> · {{ session.sprint_name }}</template>
              <template v-if="session">
                · {{ isFacilitator ? 'You are facilitating' : `Facilitated by ${session.facilitator_name || 'nobody'}` }}
              </template>
            </div>
          </div>
          <template v-if="session">
            <button
              v-if="session.can_facilitate && !isFacilitator"
              type="button"
              class="btn btn-sm btn-outline-primary"
              :disabled="busy"
              @click="act({ action: 'take_over' })"
            >
              <i class="bi bi-hand-index me-1" aria-hidden="true" />Take over
            </button>
            <button
              v-if="isFacilitator"
              type="button"
              class="btn btn-sm btn-outline-danger"
              :disabled="busy"
              @click="endSession"
            >
              End session
            </button>
          </template>
          <button type="button" class="btn-close" aria-label="Close" @click="emit('close')" />
        </div>

        <div class="modal-body p-0 refinement-body">
          <p v-if="loading" class="p-4 text-muted mb-0">Loading…</p>

          <!-- No running session -->
          <div v-else-if="!session" class="refinement-empty p-4">
            <div class="row g-4">
              <div class="col-lg-7">
                <template v-if="canFacilitate">
                  <h6 class="fw-bold">Start a refinement session</h6>
                  <p class="small text-muted">
                    You'll facilitate: the team sees the task you're on, live. The queue starts with tasks flagged
                    <strong>Needs Refinement</strong>, in board order.
                  </p>
                  <form class="vstack gap-3" @submit.prevent="startSession">
                    <div>
                      <label for="refinement-title" class="form-label small fw-bold mb-1">Title</label>
                      <input
                        id="refinement-title"
                        v-model="startTitle"
                        type="text"
                        class="form-control form-control-sm"
                        maxlength="120"
                        placeholder="e.g. Sprint 14 grooming"
                      />
                    </div>
                    <div>
                      <label for="refinement-scope" class="form-label small fw-bold mb-1">Tasks from</label>
                      <select id="refinement-scope" v-model="startScope" class="form-select form-select-sm">
                        <option v-for="o in scopes" :key="String(o.value)" :value="o.value">{{ o.label }}</option>
                      </select>
                    </div>
                    <div class="form-check">
                      <input id="refinement-unflagged" v-model="includeUnflagged" class="form-check-input" type="checkbox" />
                      <label class="form-check-label small" for="refinement-unflagged">
                        Also include open tasks with no refinement flag
                      </label>
                    </div>
                    <div>
                      <button type="submit" class="btn btn-primary" :disabled="busy">
                        <i class="bi bi-play-fill me-1" aria-hidden="true" />Start session
                      </button>
                    </div>
                  </form>
                </template>
                <template v-else>
                  <h6 class="fw-bold">No session running</h6>
                  <p class="small text-muted mb-0">
                    When someone starts a refinement session it opens here and follows along live. Starting one needs
                    the <strong>Run refinement sessions</strong> permission.
                  </p>
                </template>
              </div>
              <div v-if="history.length" class="col-lg-5">
                <h6 class="fw-bold">Recent sessions</h6>
                <ul class="list-unstyled small mb-0 vstack gap-2">
                  <li v-for="h in history" :key="h.id" class="refinement-history-row">
                    <div class="fw-semibold text-truncate">{{ h.title }}</div>
                    <div class="text-muted">
                      {{ sessionDate(h.created_at) }} · {{ h.refined }} refined · {{ h.skipped }} skipped of {{ h.queued }}
                    </div>
                  </li>
                </ul>
              </div>
            </div>
          </div>

          <!-- Running session -->
          <div v-else class="refinement-layout">
            <aside class="refinement-queue" aria-label="Session queue">
              <div class="refinement-queue-head">
                <div class="small fw-bold">Queue</div>
                <div v-if="progress" class="small text-muted">
                  {{ progress.refined }} refined · {{ progress.skipped }} skipped · {{ progress.remaining }} left
                </div>
                <div
                  v-if="progress && progress.total"
                  class="progress mt-2"
                  role="progressbar"
                  :aria-valuenow="progress.refined + progress.skipped"
                  aria-valuemin="0"
                  :aria-valuemax="progress.total"
                  aria-label="Session progress"
                  style="height: 4px;"
                >
                  <div class="progress-bar bg-success" :style="{ width: `${(progress.refined / progress.total) * 100}%` }" />
                  <div class="progress-bar bg-secondary" :style="{ width: `${(progress.skipped / progress.total) * 100}%` }" />
                </div>
              </div>
              <ol class="refinement-queue-list">
                <li
                  v-for="(t, i) in session.tasks"
                  :key="t.id"
                  class="refinement-queue-item"
                  :class="`is-${queueItemState(session, t.id)}`"
                >
                  <button
                    type="button"
                    class="refinement-queue-btn"
                    :disabled="!isFacilitator || busy"
                    :aria-current="queueItemState(session, t.id) === 'current' ? 'step' : undefined"
                    :title="isFacilitator ? 'Jump to this task' : undefined"
                    @click="act({ action: 'goto', index: session.queue.indexOf(t.id) })"
                  >
                    <i
                      class="bi refinement-queue-icon"
                      :class="{
                        'bi-play-circle-fill': queueItemState(session, t.id) === 'current',
                        'bi-check-circle-fill': queueItemState(session, t.id) === 'refined',
                        'bi-skip-forward-circle': queueItemState(session, t.id) === 'skipped',
                        'bi-circle': queueItemState(session, t.id) === 'pending',
                      }"
                      aria-hidden="true"
                    />
                    <span class="visually-hidden">{{ queueItemState(session, t.id) }}:</span>
                    <span class="refinement-queue-title">
                      <span class="text-muted">#{{ t.id }}</span> {{ t.title }}
                    </span>
                    <span v-if="t.estimate_hours != null" class="refinement-queue-est">{{ formatHours(t.estimate_hours) }}</span>
                  </button>
                  <button
                    v-if="isFacilitator"
                    type="button"
                    class="btn btn-sm btn-link text-muted refinement-queue-remove"
                    :aria-label="`Remove #${t.id} from the queue`"
                    :disabled="busy"
                    @click="act({ action: 'remove', task_id: t.id })"
                  >
                    <i class="bi bi-x-lg" aria-hidden="true" />
                  </button>
                  <span class="visually-hidden">Position {{ i + 1 }}</span>
                </li>
              </ol>
              <form v-if="isFacilitator" class="refinement-queue-add" @submit.prevent="addTasks">
                <label for="refinement-add" class="visually-hidden">Add tasks by number</label>
                <div class="input-group input-group-sm">
                  <input
                    id="refinement-add"
                    v-model="addIdsDraft"
                    type="text"
                    class="form-control"
                    placeholder="Add #ids, e.g. 42 57"
                    inputmode="numeric"
                  />
                  <button type="submit" class="btn btn-outline-secondary" :disabled="busy || !addIdsDraft.trim()">Add</button>
                </div>
              </form>
            </aside>

            <section class="refinement-stage" aria-live="polite">
              <!-- Past the end of the queue -->
              <div v-if="!session.current_task_id" class="refinement-done text-center py-5">
                <i class="bi bi-check2-all display-5 text-success" aria-hidden="true" />
                <h6 class="fw-bold mt-3">
                  {{ session.queue.length ? 'Every task in the queue has been covered' : 'The queue is empty' }}
                </h6>
                <p class="small text-muted">
                  {{ isFacilitator ? 'Add more tasks, go back to one, or end the session.' : 'Waiting for the facilitator.' }}
                </p>
                <div v-if="isFacilitator" class="d-flex justify-content-center gap-2">
                  <button v-if="session.queue.length" type="button" class="btn btn-sm btn-outline-secondary" :disabled="busy" @click="act({ action: 'prev' })">
                    <i class="bi bi-arrow-left me-1" aria-hidden="true" />Back
                  </button>
                  <button type="button" class="btn btn-sm btn-primary" :disabled="busy" @click="endSession">End session</button>
                </div>
              </div>

              <!-- Current task -->
              <template v-else>
                <div class="refinement-stage-scroll">
                  <div class="d-flex align-items-start gap-2 flex-wrap mb-1">
                    <span class="text-muted small">#{{ session.current_task_id }}</span>
                    <span
                      v-if="currentSummary?.refinement"
                      class="badge refinement-badge"
                      :class="`refinement-badge--${currentSummary.refinement}`"
                    >
                      {{ refinementLabel(currentSummary.refinement) }}
                    </span>
                    <RouterLink
                      class="small ms-auto"
                      :to="taskPageRoute(session.current_task_id, project)"
                      target="_blank"
                      rel="noopener"
                    >
                      Open task <i class="bi bi-box-arrow-up-right" aria-hidden="true" />
                    </RouterLink>
                  </div>
                  <h4 class="h5 fw-bold mb-2">{{ detail?.title ?? currentSummary?.title }}</h4>
                  <div class="d-flex flex-wrap gap-2 small text-muted mb-3">
                    <span v-if="currentSummary?.parent_title"><i class="bi bi-diagram-2 me-1" aria-hidden="true" />{{ currentSummary.parent_title }}</span>
                    <span v-if="currentSummary?.status_name"><i class="bi bi-kanban me-1" aria-hidden="true" />{{ currentSummary.status_name }}</span>
                    <span v-if="currentSummary?.sprint_name"><i class="bi bi-flag me-1" aria-hidden="true" />{{ currentSummary.sprint_name }}</span>
                    <span v-if="currentSummary?.claimed_by_name"><i class="bi bi-person me-1" aria-hidden="true" />{{ currentSummary.claimed_by_name }}</span>
                    <span v-if="currentSummary?.deleted" class="text-danger">In trash</span>
                  </div>

                  <div class="refinement-section">
                    <div class="d-flex align-items-center mb-2">
                      <h5 class="h6 fw-bold mb-0">Details &amp; acceptance criteria</h5>
                      <button
                        v-if="canEditTasks && !editingDescription && detail"
                        type="button"
                        class="btn btn-sm btn-link ms-auto"
                        @click="startEditingDescription"
                      >
                        <i class="bi bi-pencil me-1" aria-hidden="true" />Edit
                      </button>
                    </div>
                    <p v-if="detailLoading" class="small text-muted">Loading…</p>
                    <template v-else-if="editingDescription">
                      <WysiwygEditor
                        v-model="descriptionDraft"
                        :maxlength="maxDescription"
                        min-height="12rem"
                        placeholder="What needs doing, and how will the team know it's done?"
                      />
                      <div class="d-flex gap-2 justify-content-end mt-2">
                        <button type="button" class="btn btn-sm btn-outline-secondary" :disabled="savingTask" @click="editingDescription = false">Cancel</button>
                        <button type="button" class="btn btn-sm btn-primary" :disabled="savingTask" @click="saveDescription">Save details</button>
                      </div>
                    </template>
                    <RichBody v-else-if="detail?.description" :body="detail.description" />
                    <p v-else class="small text-muted mb-0">No details yet.</p>
                  </div>
                </div>

                <div class="refinement-controls">
                  <form class="refinement-estimate" @submit.prevent="saveEstimate">
                    <label for="refinement-estimate" class="small fw-bold mb-0 text-nowrap">Estimate</label>
                    <div class="input-group input-group-sm">
                      <input
                        id="refinement-estimate"
                        v-model="estimateDraft"
                        type="number"
                        class="form-control"
                        :class="{ 'is-invalid': estimateInvalid }"
                        min="0"
                        :max="MAX_ESTIMATE_HOURS"
                        :step="ESTIMATE_STEP"
                        inputmode="decimal"
                        placeholder="hours"
                        :disabled="!canEditTasks || !detail || savingTask"
                      />
                      <span class="input-group-text">h</span>
                      <button
                        v-if="canEditTasks"
                        type="submit"
                        class="btn btn-outline-primary"
                        :disabled="savingTask || !estimateDirty"
                      >
                        Save
                      </button>
                    </div>
                  </form>

                  <div v-if="isFacilitator" class="refinement-nav">
                    <button
                      type="button"
                      class="btn btn-sm btn-outline-secondary"
                      :disabled="busy || session.current === 0"
                      aria-label="Previous task"
                      @click="act({ action: 'prev' })"
                    >
                      <i class="bi bi-arrow-left" aria-hidden="true" />
                    </button>
                    <button type="button" class="btn btn-sm btn-outline-secondary" :disabled="busy" @click="act({ action: 'skip' })">
                      Skip
                    </button>
                    <button type="button" class="btn btn-sm btn-success" :disabled="busy" @click="act({ action: 'refine' })">
                      <i class="bi bi-check2-circle me-1" aria-hidden="true" />Mark refined &amp; next
                    </button>
                  </div>
                  <p v-else class="small text-muted mb-0 refinement-nav">
                    <i class="bi bi-broadcast me-1" aria-hidden="true" />Following {{ session.facilitator_name || 'the facilitator' }}
                  </p>
                </div>
              </template>
            </section>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.refinement-body {
  height: min(82vh, 820px);
  overflow: hidden;
}

.refinement-empty {
  height: 100%;
  overflow-y: auto;
}

.refinement-layout {
  display: flex;
  height: 100%;
}

.refinement-queue {
  flex: 0 0 300px;
  display: flex;
  flex-direction: column;
  min-height: 0;
  border-right: 1px solid var(--bs-border-color-translucent, rgba(0, 0, 0, 0.1));
}

.refinement-queue-head {
  padding: 0.75rem 1rem;
  border-bottom: 1px solid var(--bs-border-color-translucent, rgba(0, 0, 0, 0.1));
}

.refinement-queue-list {
  list-style: none;
  margin: 0;
  padding: 0.25rem 0.5rem;
  overflow-y: auto;
  flex: 1 1 auto;
}

.refinement-queue-item {
  display: flex;
  align-items: center;
  border-radius: 0.375rem;
}

.refinement-queue-item.is-current {
  background-color: var(--bs-primary-bg-subtle);
}

.refinement-queue-btn {
  flex: 1 1 auto;
  min-width: 0;
  display: flex;
  align-items: center;
  gap: 0.5rem;
  padding: 0.4rem 0.5rem;
  border: 0;
  background: transparent;
  color: inherit;
  text-align: left;
  font-size: 0.875rem;
}

.refinement-queue-btn:disabled {
  opacity: 1;
  cursor: default;
}

.refinement-queue-btn:not(:disabled):hover {
  background-color: var(--section-label-bg, rgba(0, 0, 0, 0.05));
  border-radius: 0.375rem;
}

.refinement-queue-title {
  flex: 1 1 auto;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.is-refined .refinement-queue-title,
.is-skipped .refinement-queue-title {
  opacity: 0.65;
}

.refinement-queue-icon {
  flex-shrink: 0;
}

.is-current .refinement-queue-icon {
  color: var(--bs-primary);
}

.is-refined .refinement-queue-icon {
  color: var(--bs-success);
}

.is-skipped .refinement-queue-icon,
.is-pending .refinement-queue-icon {
  color: var(--bs-secondary-color);
}

.refinement-queue-est {
  flex-shrink: 0;
  font-size: 0.75rem;
  color: var(--bs-secondary-color);
}

.refinement-queue-remove {
  flex-shrink: 0;
  padding: 0.25rem 0.4rem;
  opacity: 0;
}

.refinement-queue-item:hover .refinement-queue-remove,
.refinement-queue-remove:focus-visible {
  opacity: 1;
}

.refinement-queue-add {
  padding: 0.75rem 1rem;
  border-top: 1px solid var(--bs-border-color-translucent, rgba(0, 0, 0, 0.1));
}

.refinement-stage {
  flex: 1 1 auto;
  min-width: 0;
  display: flex;
  flex-direction: column;
}

.refinement-stage-scroll {
  flex: 1 1 auto;
  overflow-y: auto;
  padding: 1.25rem 1.5rem;
}

.refinement-section {
  border-top: 1px solid var(--bs-border-color-translucent, rgba(0, 0, 0, 0.1));
  padding-top: 1rem;
}

.refinement-done {
  margin: auto;
  padding: 1rem;
}

.refinement-controls {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 0.75rem 1rem;
  padding: 0.75rem 1.5rem;
  border-top: 1px solid var(--bs-border-color-translucent, rgba(0, 0, 0, 0.1));
}

.refinement-estimate {
  display: flex;
  align-items: center;
  gap: 0.5rem;
}

.refinement-estimate .input-group {
  width: 11rem;
}

.refinement-nav {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  margin-left: auto;
}

@media (max-width: 767.98px) {
  .refinement-body {
    height: auto;
    max-height: calc(100dvh - 8rem);
    overflow-y: auto;
  }

  .refinement-layout {
    flex-direction: column;
    height: auto;
  }

  .refinement-queue {
    flex: none;
    border-right: 0;
    border-bottom: 1px solid var(--bs-border-color-translucent, rgba(0, 0, 0, 0.1));
  }

  .refinement-queue-list {
    max-height: 30vh;
  }

  .refinement-queue-remove {
    opacity: 1;
  }

  .refinement-stage-scroll,
  .refinement-controls {
    padding-left: 1rem;
    padding-right: 1rem;
  }

  .refinement-nav {
    margin-left: 0;
    width: 100%;
  }

  .refinement-nav .btn-success {
    flex: 1 1 auto;
  }
}
</style>
