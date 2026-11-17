<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { RouterLink } from 'vue-router'
import { api } from '@/api/client'
import type {
  CeremonySessionSummary,
  Project,
  ProjectSprint,
  RetroCard,
  RetroConfig,
  RetroPhase,
  RetroSession,
  RetroStep,
  RetroTemplateId,
} from '@/api/types'
import { APIError } from '@/api/types'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import { useBodyScrollLock } from '@/composables/useBodyScrollLock'
import { useLiveUpdates, LIVE_CEREMONY_UPDATED, LIVE_RESYNC, type LiveEvent } from '@/composables/useLiveUpdates'
import { taskPageRoute } from '@/utils/projectRoute'
import {
  RETRO_PHASES,
  RETRO_TEMPLATE_LABELS,
  layoutRetroBoard,
  nextPhase,
  phaseIndex,
  prevPhase,
  rankedCards,
  votesLeft,
} from '@/utils/retroBoard'

const props = defineProps<{
  open: boolean
  project: Project
  sprints: ProjectSprint[]
  canFacilitate: boolean
  defaults: RetroConfig | null
}>()
const emit = defineEmits<{ close: []; 'active-changed': [active: boolean] }>()

const toast = useToast()
const { askConfirm } = useConfirm()
useBodyScrollLock(() => props.open)

const MAX_LEN = 500
const loading = ref(false)
const busy = ref(false)
const session = ref<RetroSession | null>(null)
const history = ref<CeremonySessionSummary[]>([])

const start = reactive({
  title: '',
  sprintId: 0,
  template: 'went_well' as RetroTemplateId,
  maxVotes: 5,
  anonymous: false,
})
const drafts = reactive<Record<string, string>>({})
const actionDraft = ref('')
const editingId = ref<number | null>(null)
const editDraft = ref('')
const dragId = ref<number | null>(null)
const dropTargetId = ref<number | null>(null)

const phase = computed<RetroPhase>(() => session.value?.phase ?? 'collect')
const isFacilitator = computed(() => !!session.value?.is_facilitator)
const cardsOpen = computed(() => phase.value === 'collect' || phase.value === 'group')
const board = computed(() => (session.value ? layoutRetroBoard(session.value.cards, session.value.columns) : []))
const ranked = computed(() => (session.value ? rankedCards(session.value.cards) : []))
const remainingVotes = computed(() => (session.value ? votesLeft(session.value.max_votes, session.value.votes_used) : null))
const topCards = computed(() => board.value.flatMap((c) => c.groups.map((g) => g.top)))
const cardById = computed(() => new Map((session.value?.cards ?? []).map((c) => [c.id, c])))
const groupSize = (id: number) => (session.value?.cards ?? []).filter((c) => c.parent_id === id).length
const sprintOptions = computed(() => [...props.sprints].sort((a, b) => Number(b.is_active) - Number(a.is_active)))

function resetStartForm() {
  const d = props.defaults
  start.title = ''
  start.sprintId = props.sprints.find((s) => s.is_active)?.id ?? 0
  start.template = d?.template ?? 'went_well'
  start.maxVotes = d?.max_votes ?? 5
  start.anonymous = d?.anonymous ?? false
}

function errMessage(err: unknown, fallback: string) {
  return err instanceof APIError ? err.message : fallback
}

async function load() {
  try {
    const res = await api.getActiveRetro(props.project.id)
    session.value = res.session
    emit('active-changed', !!res.session)
    if (!res.session) history.value = await api.listCeremonySessions(props.project.id, 'retro', 5).catch(() => [])
  } catch (err) {
    toast.push(errMessage(err, 'Could not load the retrospective'), 'error')
  }
}

watch(
  () => props.open,
  async (open) => {
    if (!open) {
      editingId.value = null
      return
    }
    resetStartForm()
    loading.value = !session.value
    await load()
    loading.value = false
  },
  { immediate: true },
)

useLiveUpdates(
  (_e: LiveEvent, batch: LiveEvent[]) => {
    if (!props.open) return
    const pid = props.project.id
    if (batch.some((e) => e.type === LIVE_RESYNC || (e.type === LIVE_CEREMONY_UPDATED && e.project_id === pid && e.key?.startsWith('retro:')))) {
      void load()
    }
  },
  120,
  { ceremonies: true },
)

/** Runs a change that returns the fresh session; on a 409 shows the latest instead. */
async function run(fn: () => Promise<RetroSession>, fallback: string, done?: string) {
  if (busy.value) return false
  busy.value = true
  try {
    session.value = await fn()
    if (done) toast.push(done, 'success')
    return true
  } catch (err) {
    toast.push(err instanceof APIError && err.status === 409 ? err.message : errMessage(err, fallback), err instanceof APIError && err.status === 409 ? 'info' : 'error')
    await load()
    return false
  } finally {
    busy.value = false
  }
}

async function startRetro() {
  const ok = await run(
    () =>
      api.startRetro(props.project.id, {
        title: start.title.trim() || undefined,
        sprint_id: start.sprintId || undefined,
        template: start.template,
        max_votes: start.maxVotes,
        anonymous: start.anonymous,
      }),
    'Could not start the retrospective',
  )
  if (ok) emit('active-changed', true)
}

async function step(s: RetroStep) {
  const cur = session.value
  if (!cur) return
  const ok = await run(() => api.retroStep(props.project.id, cur.id, cur.revision, s), 'That step did not go through')
  if (ok && session.value?.status === 'ended') {
    toast.push('Retrospective ended', 'success')
    session.value = null
    emit('active-changed', false)
    await load()
  }
}

async function endRetro() {
  const withoutTasks = (session.value?.actions ?? []).filter((a) => !a.task_id).length
  const ok = await askConfirm({
    title: 'End retrospective?',
    message: withoutTasks
      ? `${withoutTasks} action item${withoutTasks === 1 ? ' has' : 's have'} no task yet. Ending locks the retro for everyone.`
      : 'Ending locks the retro for everyone. It stays in the project history.',
    confirmLabel: 'End retrospective',
  })
  if (ok) await step({ action: 'end' })
}

async function addCard(column: string) {
  const body = (drafts[column] ?? '').trim()
  const cur = session.value
  if (!body || !cur) return
  if (await run(() => api.addRetroItem(props.project.id, cur.id, { type: 'card', column, body }), 'Could not add the card')) {
    drafts[column] = ''
  }
}

function startEdit(c: RetroCard) {
  editingId.value = c.id
  editDraft.value = c.body ?? ''
}

async function saveEdit(id: number) {
  const cur = session.value
  const body = editDraft.value.trim()
  if (!cur || !body) return
  if (await run(() => api.updateRetroItem(props.project.id, cur.id, id, { body }), 'Could not save the card')) {
    editingId.value = null
  }
}

async function removeItem(id: number, what: string) {
  const cur = session.value
  if (!cur) return
  const ok = await askConfirm({ title: `Delete ${what}?`, message: 'This cannot be undone.', confirmLabel: 'Delete', danger: true })
  if (ok) await run(() => api.deleteRetroItem(props.project.id, cur.id, id), `Could not delete the ${what}`)
}

async function moveCard(id: number, column: string) {
  const cur = session.value
  if (cur) await run(() => api.updateRetroItem(props.project.id, cur.id, id, { column }), 'Could not move the card')
}

async function groupUnder(id: number, parentId: number | null) {
  const cur = session.value
  if (!cur || id === parentId) return
  await run(() => api.updateRetroItem(props.project.id, cur.id, id, { parent_id: parentId }), 'Could not group the card')
}

async function toggleVote(c: RetroCard) {
  const cur = session.value
  if (cur) await run(() => api.voteRetroCard(props.project.id, cur.id, c.id, !c.voted_by_me), 'Could not record the vote')
}

async function addAction(cardId?: number) {
  const cur = session.value
  const body = actionDraft.value.trim()
  if (!cur || !body) return
  if (await run(() => api.addRetroItem(props.project.id, cur.id, { type: 'action', body, card_id: cardId }), 'Could not add the action item')) {
    actionDraft.value = ''
  }
}

async function makeTask(id: number) {
  const cur = session.value
  if (cur) await run(() => api.retroActionToTask(props.project.id, cur.id, id), 'Could not create the task', 'Task created')
}

// Drag a card onto another to group them (Group phase).
function onDragStart(e: DragEvent, c: RetroCard) {
  dragId.value = c.id
  e.dataTransfer?.setData('text/plain', String(c.id))
  if (e.dataTransfer) e.dataTransfer.effectAllowed = 'move'
}
function onDragEnd() {
  dragId.value = null
  dropTargetId.value = null
}
function onDrop(target: RetroCard) {
  const id = dragId.value
  onDragEnd()
  if (id && id !== target.id) void groupUnder(id, target.id)
}

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape' && editingId.value == null) emit('close')
}
function sessionDate(iso: string) {
  return new Date(iso).toLocaleDateString(undefined, { month: 'short', day: 'numeric' })
}
</script>

<template>
  <div
    v-if="open"
    class="modal fade show d-block retro-modal"
    style="background: rgba(0, 0, 0, 0.5);"
    tabindex="-1"
    role="dialog"
    aria-modal="true"
    aria-labelledby="retro-modal-title"
    @click.self="emit('close')"
    @keydown="onKeydown"
  >
    <div class="modal-dialog modal-dialog-centered modal-xl retro-dialog">
      <div class="modal-content border-0 shadow" style="background: var(--ordryn-card-bg); color: var(--ordryn-text);">
        <div class="modal-header gap-3 flex-wrap">
          <div class="min-w-0 me-auto">
            <h5 id="retro-modal-title" class="modal-title fw-bold mb-0 d-flex align-items-center gap-2">
              <i class="bi bi-arrow-repeat" aria-hidden="true" />
              <span class="text-truncate">{{ session ? session.title : 'Retrospective' }}</span>
              <span v-if="session" class="badge text-bg-success ceremony-live"><span class="ceremony-live-dot" aria-hidden="true" />Live</span>
              <span v-if="session?.anonymous" class="badge text-bg-secondary">Anonymous</span>
            </h5>
            <div class="small text-muted text-truncate">
              {{ project.name }}<template v-if="session?.sprint_name"> · {{ session.sprint_name }}</template>
              <template v-if="session"> · {{ isFacilitator ? 'You are facilitating' : `Facilitated by ${session.facilitator_name || 'nobody'}` }}</template>
            </div>
          </div>
          <template v-if="session">
            <button v-if="session.can_facilitate && !isFacilitator" type="button" class="btn btn-sm btn-outline-primary" :disabled="busy" @click="step({ action: 'take_over' })">
              <i class="bi bi-hand-index me-1" aria-hidden="true" />Take over
            </button>
            <button v-if="isFacilitator" type="button" class="btn btn-sm btn-outline-danger" :disabled="busy" @click="endRetro">End retro</button>
          </template>
          <button type="button" class="btn-close" aria-label="Close" @click="emit('close')" />
        </div>

        <!-- Phase bar -->
        <div v-if="session" class="retro-phasebar">
          <ol class="retro-phases" aria-label="Retrospective phases">
            <li
              v-for="(p, i) in RETRO_PHASES"
              :key="p.id"
              class="retro-phase"
              :class="{ 'is-current': p.id === phase, 'is-done': i < phaseIndex(phase) }"
              :aria-current="p.id === phase ? 'step' : undefined"
            >
              <span class="retro-phase-num">{{ i + 1 }}</span>{{ p.label }}
            </li>
          </ol>
          <p class="small text-muted mb-0 retro-phase-hint">
            {{ RETRO_PHASES[phaseIndex(phase)]?.hint }}
            <template v-if="phase === 'vote'">
              <strong v-if="remainingVotes != null" class="ms-1">{{ remainingVotes }} of {{ session.max_votes }} votes left.</strong>
              <strong v-else class="ms-1">Unlimited votes.</strong>
            </template>
          </p>
          <div v-if="isFacilitator" class="d-flex gap-2 ms-auto">
            <button v-if="prevPhase(phase)" type="button" class="btn btn-sm btn-outline-secondary" :disabled="busy" @click="step({ action: 'phase', phase: prevPhase(phase)! })">
              <i class="bi bi-arrow-left me-1" aria-hidden="true" />Back
            </button>
            <button v-if="nextPhase(phase)" type="button" class="btn btn-sm btn-primary" :disabled="busy" @click="step({ action: 'phase', phase: nextPhase(phase)! })">
              Next: {{ RETRO_PHASES[phaseIndex(phase) + 1].label }}<i class="bi bi-arrow-right ms-1" aria-hidden="true" />
            </button>
          </div>
        </div>

        <div class="modal-body p-0 retro-body">
          <p v-if="loading" class="p-4 text-muted mb-0">Loading…</p>

          <!-- Not running -->
          <div v-else-if="!session" class="p-4 retro-empty">
            <div class="row g-4">
              <div class="col-lg-7">
                <template v-if="canFacilitate">
                  <h6 class="fw-bold">Start a retrospective</h6>
                  <p class="small text-muted">
                    You'll facilitate and move the team through Collect, Group, Vote and Act. Everyone with the window
                    open follows along live.
                  </p>
                  <form class="row g-3" @submit.prevent="startRetro">
                    <div class="col-12">
                      <label for="retro-title" class="form-label small fw-bold mb-1">Title</label>
                      <input id="retro-title" v-model="start.title" type="text" class="form-control form-control-sm" maxlength="120" placeholder="Defaults to the sprint name and date" />
                    </div>
                    <div class="col-sm-6">
                      <label for="retro-sprint" class="form-label small fw-bold mb-1">Sprint</label>
                      <select id="retro-sprint" v-model.number="start.sprintId" class="form-select form-select-sm">
                        <option :value="0">No sprint</option>
                        <option v-for="s in sprintOptions" :key="s.id" :value="s.id">{{ s.name }}{{ s.is_active ? ' (active)' : '' }}</option>
                      </select>
                    </div>
                    <div class="col-sm-6">
                      <label for="retro-template" class="form-label small fw-bold mb-1">Columns</label>
                      <select id="retro-template" v-model="start.template" class="form-select form-select-sm">
                        <option v-for="(label, id) in RETRO_TEMPLATE_LABELS" :key="id" :value="id">{{ label }}</option>
                      </select>
                    </div>
                    <div class="col-sm-6">
                      <label for="retro-votes" class="form-label small fw-bold mb-1">Votes per person</label>
                      <input id="retro-votes" v-model.number="start.maxVotes" type="number" class="form-control form-control-sm" min="0" max="20" aria-describedby="retro-votes-help" />
                      <small id="retro-votes-help" class="form-hint">0 = unlimited</small>
                    </div>
                    <div class="col-sm-6 d-flex align-items-center">
                      <div class="form-check mt-3">
                        <input id="retro-anon" v-model="start.anonymous" class="form-check-input" type="checkbox" />
                        <label class="form-check-label small" for="retro-anon">Anonymous cards</label>
                      </div>
                    </div>
                    <div class="col-12">
                      <button type="submit" class="btn btn-primary" :disabled="busy"><i class="bi bi-play-fill me-1" aria-hidden="true" />Start retrospective</button>
                    </div>
                  </form>
                </template>
                <template v-else>
                  <h6 class="fw-bold">No retrospective running</h6>
                  <p class="small text-muted mb-0">
                    When someone starts one it opens here and follows along live. Starting one needs the
                    <strong>Run retrospectives</strong> permission.
                  </p>
                </template>
              </div>
              <div v-if="history.length" class="col-lg-5">
                <h6 class="fw-bold">Recent retrospectives</h6>
                <ul class="list-unstyled small mb-0 vstack gap-2">
                  <li v-for="h in history" :key="h.id">
                    <div class="fw-semibold text-truncate">{{ h.title }}</div>
                    <div class="text-muted">{{ sessionDate(h.created_at) }} · {{ h.counts.card ?? 0 }} cards · {{ h.counts.action ?? 0 }} action items</div>
                  </li>
                </ul>
              </div>
            </div>
          </div>

          <!-- Act phase -->
          <div v-else-if="phase === 'act'" class="retro-act">
            <section class="retro-act-cards" aria-label="Cards by votes">
              <h6 class="fw-bold mb-2">Most voted</h6>
              <ol class="list-unstyled vstack gap-2 mb-0">
                <li v-for="c in ranked" :key="c.id" class="retro-card retro-card--ranked">
                  <span class="retro-votes-pill" :title="`${c.votes} vote${c.votes === 1 ? '' : 's'}`">{{ c.votes }}</span>
                  <div class="min-w-0 flex-grow-1">
                    <div class="retro-card-body">{{ c.body }}</div>
                    <div class="small text-muted">
                      {{ session.columns.find((col) => col.key === c.column)?.title }}
                      <template v-if="groupSize(c.id)"> · +{{ groupSize(c.id) }} grouped</template>
                      <template v-if="c.author_name"> · {{ c.author_name }}</template>
                    </div>
                  </div>
                  <button type="button" class="btn btn-sm btn-link" :disabled="busy" title="Use as an action item" @click="actionDraft = c.body ?? ''">
                    <i class="bi bi-arrow-right-circle" aria-hidden="true" /><span class="visually-hidden">Use as an action item</span>
                  </button>
                </li>
                <li v-if="!ranked.length" class="small text-muted">No cards were added.</li>
              </ol>
            </section>
            <section class="retro-act-actions" aria-label="Action items">
              <h6 class="fw-bold mb-2">Action items</h6>
              <form class="d-flex gap-2 mb-3" @submit.prevent="addAction()">
                <label for="retro-action-draft" class="visually-hidden">New action item</label>
                <input id="retro-action-draft" v-model="actionDraft" type="text" class="form-control form-control-sm" :maxlength="MAX_LEN" placeholder="What will the team do differently?" />
                <button type="submit" class="btn btn-sm btn-primary text-nowrap" :disabled="busy || !actionDraft.trim()">Add</button>
              </form>
              <ul class="list-unstyled vstack gap-2 mb-0">
                <li v-for="a in session.actions" :key="a.id" class="retro-action">
                  <div class="min-w-0 flex-grow-1">
                    <div>{{ a.body }}</div>
                    <div class="small text-muted">
                      {{ a.author_name || 'Someone' }}
                      <template v-if="a.card_id && cardById.get(a.card_id)?.body"> · from “{{ cardById.get(a.card_id)?.body }}”</template>
                    </div>
                    <RouterLink v-if="a.task_id" class="small" :to="taskPageRoute(a.task_id, project)" target="_blank" rel="noopener">
                      <i class="bi bi-check2-square me-1" aria-hidden="true" />#{{ a.task_id }} {{ a.task_title }}
                    </RouterLink>
                  </div>
                  <button v-if="!a.task_id && session.can_create_task" type="button" class="btn btn-sm btn-outline-primary text-nowrap" :disabled="busy" @click="makeTask(a.id)">
                    Create task
                  </button>
                  <button v-if="a.mine || isFacilitator" type="button" class="btn btn-sm btn-link text-muted" :aria-label="`Delete action item ${a.body}`" :disabled="busy" @click="removeItem(a.id, 'action item')">
                    <i class="bi bi-trash" aria-hidden="true" />
                  </button>
                </li>
                <li v-if="!session.actions.length" class="small text-muted">No action items yet.</li>
              </ul>
            </section>
          </div>

          <!-- Collect / Group / Vote board -->
          <div v-else class="retro-board" :style="{ '--retro-cols': session.columns.length }">
            <section v-for="col in board" :key="col.column.key" class="retro-column" :aria-label="col.column.title">
              <header class="retro-column-head">
                <h6 class="fw-bold mb-0">{{ col.column.title }}</h6>
                <span class="badge text-bg-light border">{{ col.groups.reduce((n, g) => n + 1 + g.children.length, 0) + col.hiddenCount }}</span>
              </header>
              <div class="retro-column-cards">
                <p v-if="col.hiddenCount" class="small text-muted retro-hidden-note">
                  <i class="bi bi-eye-slash me-1" aria-hidden="true" />{{ col.hiddenCount }} card{{ col.hiddenCount === 1 ? '' : 's' }} from others, revealed when grouping starts
                </p>
                <article
                  v-for="g in col.groups"
                  :key="g.top.id"
                  class="retro-card"
                  :class="{ 'is-drop-target': dropTargetId === g.top.id, 'is-dragging': dragId === g.top.id, 'is-mine': g.top.mine }"
                  :draggable="phase === 'group' && !busy"
                  @dragstart="onDragStart($event, g.top)"
                  @dragend="onDragEnd"
                  @dragover.prevent="phase === 'group' && dragId && dragId !== g.top.id && (dropTargetId = g.top.id)"
                  @dragleave="dropTargetId === g.top.id && (dropTargetId = null)"
                  @drop.prevent="phase === 'group' && onDrop(g.top)"
                >
                  <template v-if="editingId === g.top.id">
                    <label :for="`retro-edit-${g.top.id}`" class="visually-hidden">Edit card</label>
                    <textarea :id="`retro-edit-${g.top.id}`" v-model="editDraft" class="form-control form-control-sm" rows="3" :maxlength="MAX_LEN" @keydown.enter.exact.prevent="saveEdit(g.top.id)" />
                    <div class="d-flex justify-content-end gap-2 mt-1">
                      <button type="button" class="btn btn-sm btn-link" @click="editingId = null">Cancel</button>
                      <button type="button" class="btn btn-sm btn-primary" :disabled="busy || !editDraft.trim()" @click="saveEdit(g.top.id)">Save</button>
                    </div>
                  </template>
                  <template v-else>
                    <div class="retro-card-body">{{ g.top.body }}</div>
                    <div class="retro-card-foot">
                      <span class="small text-muted text-truncate">{{ g.top.mine ? 'You' : g.top.author_name || (session.anonymous ? 'Anonymous' : '') }}</span>
                      <div class="retro-card-tools">
                        <button
                          v-if="phase === 'vote'"
                          type="button"
                          class="btn btn-sm retro-vote"
                          :class="g.top.voted_by_me ? 'btn-primary' : 'btn-outline-secondary'"
                          :aria-pressed="g.top.voted_by_me"
                          :disabled="busy || (!g.top.voted_by_me && remainingVotes === 0)"
                          @click="toggleVote(g.top)"
                        >
                          <i class="bi" :class="g.top.voted_by_me ? 'bi-hand-thumbs-up-fill' : 'bi-hand-thumbs-up'" aria-hidden="true" />
                          <span class="visually-hidden">{{ g.top.voted_by_me ? 'Remove vote' : 'Vote' }}</span>
                        </button>
                        <template v-if="cardsOpen">
                          <button v-if="g.top.mine" type="button" class="btn btn-sm btn-link text-muted" aria-label="Edit card" @click="startEdit(g.top)">
                            <i class="bi bi-pencil" aria-hidden="true" />
                          </button>
                          <div v-if="g.top.mine || isFacilitator || phase === 'group'" class="dropdown">
                            <button type="button" class="btn btn-sm btn-link text-muted" data-bs-toggle="dropdown" aria-expanded="false" aria-label="More card actions">
                              <i class="bi bi-three-dots" aria-hidden="true" />
                            </button>
                            <ul class="dropdown-menu dropdown-menu-end">
                              <template v-if="g.top.mine || isFacilitator">
                                <li><h6 class="dropdown-header">Move to</h6></li>
                                <li v-for="c2 in session.columns.filter((c) => c.key !== g.top.column)" :key="c2.key">
                                  <button type="button" class="dropdown-item" @click="moveCard(g.top.id, c2.key)">{{ c2.title }}</button>
                                </li>
                              </template>
                              <template v-if="phase === 'group' && topCards.length > 1">
                                <li><hr class="dropdown-divider" /></li>
                                <li><h6 class="dropdown-header">Group under</h6></li>
                                <li v-for="t in topCards.filter((t) => t.id !== g.top.id)" :key="t.id">
                                  <button type="button" class="dropdown-item text-truncate retro-group-option" @click="groupUnder(g.top.id, t.id)">{{ t.body }}</button>
                                </li>
                              </template>
                              <template v-if="g.top.mine || isFacilitator">
                                <li><hr class="dropdown-divider" /></li>
                                <li><button type="button" class="dropdown-item text-danger" @click="removeItem(g.top.id, 'card')">Delete</button></li>
                              </template>
                            </ul>
                          </div>
                        </template>
                      </div>
                    </div>
                  </template>
                  <ul v-if="g.children.length" class="retro-group list-unstyled mb-0">
                    <li v-for="child in g.children" :key="child.id" class="retro-child">
                      <span class="retro-card-body">{{ child.body }}</span>
                      <button
                        v-if="phase === 'group'"
                        type="button"
                        class="btn btn-sm btn-link text-muted p-0 ms-auto"
                        :aria-label="`Ungroup ${child.body}`"
                        :disabled="busy"
                        @click="groupUnder(child.id, null)"
                      >
                        <i class="bi bi-box-arrow-up-right" aria-hidden="true" />
                      </button>
                    </li>
                  </ul>
                </article>
              </div>
              <form v-if="cardsOpen" class="retro-add" @submit.prevent="addCard(col.column.key)">
                <label :for="`retro-add-${col.column.key}`" class="visually-hidden">Add a card to {{ col.column.title }}</label>
                <textarea
                  :id="`retro-add-${col.column.key}`"
                  v-model="drafts[col.column.key]"
                  class="form-control form-control-sm"
                  rows="2"
                  :maxlength="MAX_LEN"
                  :placeholder="`Add to ${col.column.title}…`"
                  @keydown.enter.exact.prevent="addCard(col.column.key)"
                />
                <button type="submit" class="btn btn-sm btn-outline-primary w-100 mt-1" :disabled="busy || !(drafts[col.column.key] ?? '').trim()">Add card</button>
              </form>
            </section>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.retro-body {
  height: min(78vh, 800px);
  overflow: hidden;
}

.retro-empty {
  height: 100%;
  overflow-y: auto;
}

.retro-phasebar {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.5rem 1rem;
  padding: 0.6rem 1rem;
  border-bottom: 1px solid var(--bs-border-color-translucent, rgba(0, 0, 0, 0.1));
}

.retro-phases {
  display: flex;
  gap: 0.25rem;
  list-style: none;
  margin: 0;
  padding: 0;
}

.retro-phase {
  display: inline-flex;
  align-items: center;
  gap: 0.35rem;
  padding: 0.2rem 0.6rem;
  border-radius: 9999px;
  font-size: 0.8rem;
  font-weight: 600;
  color: var(--bs-secondary-color);
}

.retro-phase-num {
  display: inline-grid;
  place-items: center;
  width: 1.2rem;
  height: 1.2rem;
  border-radius: 50%;
  font-size: 0.7rem;
  border: 1px solid currentColor;
}

.retro-phase.is-current {
  color: var(--bs-primary-text-emphasis);
  background: var(--bs-primary-bg-subtle);
}

.retro-phase.is-done {
  color: var(--bs-success-text-emphasis);
}

.retro-phase-hint {
  flex: 1 1 16rem;
}

.retro-board {
  display: grid;
  grid-template-columns: repeat(var(--retro-cols), minmax(15rem, 1fr));
  gap: 0.75rem;
  height: 100%;
  padding: 0.75rem;
  overflow-x: auto;
}

.retro-column {
  display: flex;
  flex-direction: column;
  min-height: 0;
  background: var(--section-label-bg, rgba(0, 0, 0, 0.03));
  border-radius: 0.5rem;
}

.retro-column-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0.6rem 0.75rem;
}

.retro-column-cards {
  flex: 1 1 auto;
  overflow-y: auto;
  padding: 0 0.5rem;
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}

.retro-hidden-note {
  margin: 0;
  padding: 0.4rem 0.5rem;
  border: 1px dashed var(--bs-border-color);
  border-radius: 0.375rem;
}

.retro-card {
  background: var(--ordryn-card-bg, var(--bs-body-bg));
  border: 1px solid var(--bs-border-color-translucent, rgba(0, 0, 0, 0.1));
  border-radius: 0.5rem;
  padding: 0.5rem 0.6rem;
  font-size: 0.875rem;
}

.retro-card[draggable='true'] {
  cursor: grab;
}

.retro-card.is-mine {
  border-left: 3px solid var(--bs-primary);
}

.retro-card.is-drop-target {
  outline: 2px dashed var(--bs-primary);
  outline-offset: 2px;
}

.retro-card.is-dragging {
  opacity: 0.5;
}

.retro-card-body {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}

.retro-card-foot {
  display: flex;
  align-items: center;
  gap: 0.25rem;
  margin-top: 0.35rem;
}

.retro-card-tools {
  display: flex;
  align-items: center;
  margin-left: auto;
}

.retro-card-tools .btn-link {
  padding: 0.1rem 0.3rem;
}

.retro-group-option {
  max-width: 18rem;
}

.retro-group {
  margin-top: 0.5rem;
  border-top: 1px dashed var(--bs-border-color);
  padding-top: 0.35rem;
}

.retro-child {
  display: flex;
  gap: 0.35rem;
  align-items: flex-start;
  padding: 0.2rem 0;
  font-size: 0.8rem;
  color: var(--bs-secondary-color);
}

.retro-add {
  padding: 0.5rem;
}

.retro-act {
  display: grid;
  grid-template-columns: 1fr 1fr;
  height: 100%;
}

.retro-act-cards,
.retro-act-actions {
  overflow-y: auto;
  padding: 1rem 1.25rem;
}

.retro-act-cards {
  border-right: 1px solid var(--bs-border-color-translucent, rgba(0, 0, 0, 0.1));
}

.retro-card--ranked,
.retro-action {
  display: flex;
  align-items: flex-start;
  gap: 0.6rem;
}

.retro-action {
  padding: 0.5rem 0.6rem;
  border: 1px solid var(--bs-border-color-translucent, rgba(0, 0, 0, 0.1));
  border-radius: 0.5rem;
}

.retro-votes-pill {
  flex-shrink: 0;
  min-width: 2rem;
  text-align: center;
  font-weight: 700;
  border-radius: 9999px;
  padding: 0.1rem 0.5rem;
  background: var(--bs-primary-bg-subtle);
  color: var(--bs-primary-text-emphasis);
}

@media (max-width: 767.98px) {
  .retro-body {
    height: auto;
    max-height: calc(100dvh - 12rem);
    overflow-y: auto;
  }

  .retro-board {
    grid-template-columns: 1fr;
    height: auto;
    overflow-x: visible;
  }

  .retro-column-cards {
    overflow: visible;
  }

  .retro-act {
    grid-template-columns: 1fr;
    height: auto;
  }

  .retro-act-cards {
    border-right: 0;
    border-bottom: 1px solid var(--bs-border-color-translucent, rgba(0, 0, 0, 0.1));
  }

  .retro-phases {
    flex-wrap: wrap;
  }
}
</style>
