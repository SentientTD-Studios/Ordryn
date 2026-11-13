<script setup lang="ts">
import { computed, onUnmounted, reactive, ref, watch } from 'vue'
import { RouterLink } from 'vue-router'
import { api } from '@/api/client'
import type {
  CeremonySessionSummary,
  Project,
  ProjectMember,
  StandupConfig,
  StandupParticipant,
  StandupSession,
  StandupStep,
} from '@/api/types'
import { APIError } from '@/api/types'
import { useAuth } from '@/composables/useAuth'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import { useBodyScrollLock } from '@/composables/useBodyScrollLock'
import { useLiveUpdates, LIVE_CEREMONY_UPDATED, LIVE_RESYNC, type LiveEvent } from '@/composables/useLiveUpdates'
import { taskPageRoute } from '@/utils/projectRoute'
import { parseTaskIdList } from '@/utils/refinementSession'
import { clockOffsetMs, formatClock, formatTimerSetting, speakerSecondsLeft } from '@/utils/standupTimer'
import { formatHours } from '@/utils/estimates'

const props = defineProps<{
  open: boolean
  project: Project
  canFacilitate: boolean
  defaults: StandupConfig | null
}>()
const emit = defineEmits<{ close: []; 'active-changed': [active: boolean] }>()

const toast = useToast()
const { askConfirm } = useConfirm()
const { user } = useAuth()
useBodyScrollLock(() => props.open)

const MAX_LEN = 2000
const loading = ref(false)
const busy = ref(false)
const savingEntry = ref(false)
const session = ref<StandupSession | null>(null)
const history = ref<CeremonySessionSummary[]>([])
const members = ref<ProjectMember[]>([])
const offsetMs = ref(0)
const nowMs = ref(Date.now())
let ticker: ReturnType<typeof setInterval> | null = null

const start = reactive({ title: '', speakerSeconds: 120, shuffle: false })
const entry = reactive({ done: '', today: '', blockers: '', linkIds: '' })
const entryDirty = ref(false)
const blockerTitle = ref('')
const addUserId = ref(0)

const isFacilitator = computed(() => !!session.value?.is_facilitator)
const me = computed(() => user.value?.id ?? 0)
const current = computed<StandupParticipant | null>(
  () => session.value?.participants.find((p) => p.user_id === session.value?.current_user_id) ?? null,
)
const allSpoken = computed(() => !!session.value && !session.value.current_user_id)
const myRow = computed(() => session.value?.participants.find((p) => p.user_id === me.value) ?? null)
const secondsLeft = computed(() =>
  session.value && session.value.current_user_id
    ? speakerSecondsLeft(session.value.speaker_since, session.value.speaker_seconds, nowMs.value, offsetMs.value)
    : null,
)
const addable = computed(() => {
  const inIt = new Set(session.value?.participants.map((p) => p.user_id) ?? [])
  return members.value.filter((m) => !m.is_agent && !inIt.has(m.user_id))
})
const blockedCount = computed(
  () => session.value?.participants.filter((p) => p.entry && (p.entry.blockers || p.entry.blocker_task_ids.length)).length ?? 0,
)

function errMessage(err: unknown, fallback: string) {
  return err instanceof APIError ? err.message : fallback
}

function applySession(s: StandupSession | null) {
  if (s) offsetMs.value = clockOffsetMs(s.server_time, Date.now())
  session.value = s
  // Keep what the person is typing; otherwise mirror the saved check-in.
  if (!entryDirty.value) {
    const e = s?.participants.find((p) => p.user_id === me.value)?.entry
    entry.done = e?.done ?? ''
    entry.today = e?.today ?? ''
    entry.blockers = e?.blockers ?? ''
    entry.linkIds = (e?.blocker_task_ids ?? []).join(' ')
  }
}

async function load() {
  try {
    const res = await api.getActiveStandup(props.project.id)
    applySession(res.session)
    emit('active-changed', !!res.session)
    if (!res.session) history.value = await api.listCeremonySessions(props.project.id, 'standup', 5).catch(() => [])
  } catch (err) {
    toast.push(errMessage(err, 'Could not load the standup'), 'error')
  }
}

watch(
  () => props.open,
  async (open) => {
    if (ticker) clearInterval(ticker)
    ticker = null
    if (!open) return
    start.title = ''
    start.speakerSeconds = props.defaults?.speaker_seconds ?? 120
    start.shuffle = props.defaults?.shuffle ?? false
    entryDirty.value = false
    ticker = setInterval(() => (nowMs.value = Date.now()), 1000)
    loading.value = !session.value
    await load()
    loading.value = false
    api.listProjectMembers(props.project.id).then((m) => (members.value = m)).catch(() => (members.value = []))
  },
  { immediate: true },
)
onUnmounted(() => {
  if (ticker) clearInterval(ticker)
})

useLiveUpdates(
  (_e: LiveEvent, batch: LiveEvent[]) => {
    if (!props.open) return
    const pid = props.project.id
    const touched = batch.some(
      (e) =>
        e.type === LIVE_RESYNC ||
        (e.type === LIVE_CEREMONY_UPDATED && e.project_id === pid && e.key?.startsWith('standup:')) ||
        // Claimed work and blocker tasks change on the board too.
        (e.task_id && e.project_id === pid),
    )
    if (touched) void load()
  },
  150,
  { ceremonies: true },
)

async function run(fn: () => Promise<StandupSession>, fallback: string, done?: string) {
  if (busy.value) return false
  busy.value = true
  try {
    applySession(await fn())
    if (done) toast.push(done, 'success')
    return true
  } catch (err) {
    const conflict = err instanceof APIError && err.status === 409
    toast.push(conflict ? (err as APIError).message : errMessage(err, fallback), conflict ? 'info' : 'error')
    await load()
    return false
  } finally {
    busy.value = false
  }
}

async function startStandup() {
  const ok = await run(
    () =>
      api.startStandup(props.project.id, {
        title: start.title.trim() || undefined,
        speaker_seconds: start.speakerSeconds,
        shuffle: start.shuffle,
      }),
    'Could not start the standup',
  )
  if (ok) emit('active-changed', true)
}

async function step(s: StandupStep) {
  const cur = session.value
  if (!cur) return
  const ok = await run(() => api.standupStep(props.project.id, cur.id, cur.revision, s), 'That step did not go through')
  if (ok && session.value?.status === 'ended') {
    toast.push('Standup ended', 'success')
    applySession(null)
    emit('active-changed', false)
    await load()
  }
}

async function endStandup() {
  const silent = session.value?.participants.filter((p) => !p.entry).length ?? 0
  const ok = await askConfirm({
    title: 'End standup?',
    message: silent
      ? `${silent} ${silent === 1 ? 'person has' : 'people have'} no check-in. The standup is saved to the project history as it is.`
      : 'The standup is saved to the project history.',
    confirmLabel: 'End standup',
  })
  if (ok) await step({ action: 'end' })
}

async function join() {
  const cur = session.value
  if (cur) await run(() => api.joinStandup(props.project.id, cur.id), 'Could not join the standup')
}

async function addParticipant() {
  if (!addUserId.value) return
  await step({ action: 'add_participant', user_id: addUserId.value })
  addUserId.value = 0
}

async function removeParticipant(p: StandupParticipant) {
  const ok = await askConfirm({
    title: `Remove ${p.name}?`,
    message: 'They leave the speaking order. Anything they already wrote stays in the standup.',
    confirmLabel: 'Remove',
  })
  if (ok) await step({ action: 'remove_participant', user_id: p.user_id })
}

async function saveEntry() {
  const cur = session.value
  if (!cur) return
  savingEntry.value = true
  try {
    applySession(
      await api.saveStandupEntry(props.project.id, cur.id, {
        done: entry.done,
        today: entry.today,
        blockers: entry.blockers,
        blocker_task_ids: parseTaskIdList(entry.linkIds),
      }),
    )
    entryDirty.value = false
    applySession(session.value)
    toast.push('Check-in saved', 'success')
  } catch (err) {
    toast.push(errMessage(err, 'Could not save your check-in'), 'error')
  } finally {
    savingEntry.value = false
  }
}

async function createBlocker() {
  const cur = session.value
  const title = blockerTitle.value.trim()
  if (!cur || !title) return
  if (entryDirty.value) await saveEntry()
  if (await run(() => api.createStandupBlocker(props.project.id, cur.id, title), 'Could not create the blocker task', 'Blocker task created')) {
    blockerTitle.value = ''
  }
}

function participantState(i: number): 'spoken' | 'current' | 'upcoming' {
  const c = session.value?.current ?? 0
  return i < c ? 'spoken' : i === c ? 'current' : 'upcoming'
}

function hasBlocker(p: StandupParticipant) {
  return !!p.entry && (!!p.entry.blockers || p.entry.blocker_task_ids.length > 0)
}

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape') emit('close')
}
function sessionDate(iso: string) {
  return new Date(iso).toLocaleDateString(undefined, { weekday: 'short', month: 'short', day: 'numeric' })
}
</script>

<template>
  <div
    v-if="open"
    class="modal fade show d-block standup-modal"
    style="background: rgba(0, 0, 0, 0.5);"
    tabindex="-1"
    role="dialog"
    aria-modal="true"
    aria-labelledby="standup-modal-title"
    @click.self="emit('close')"
    @keydown="onKeydown"
  >
    <div class="modal-dialog modal-dialog-centered modal-xl">
      <div class="modal-content border-0 shadow" style="background: var(--ordryn-card-bg); color: var(--ordryn-text);">
        <div class="modal-header gap-3 flex-wrap">
          <div class="min-w-0 me-auto">
            <h5 id="standup-modal-title" class="modal-title fw-bold mb-0 d-flex align-items-center gap-2">
              <i class="bi bi-people" aria-hidden="true" />
              <span class="text-truncate">{{ session ? session.title : 'Standup' }}</span>
              <span v-if="session" class="badge text-bg-success ceremony-live"><span class="ceremony-live-dot" aria-hidden="true" />Live</span>
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
            <button v-if="isFacilitator" type="button" class="btn btn-sm btn-outline-danger" :disabled="busy" @click="endStandup">End standup</button>
          </template>
          <button type="button" class="btn-close" aria-label="Close" @click="emit('close')" />
        </div>

        <div class="modal-body p-0 standup-body">
          <p v-if="loading" class="p-4 text-muted mb-0">Loading…</p>

          <!-- Not running -->
          <div v-else-if="!session" class="p-4 standup-empty">
            <div class="row g-4">
              <div class="col-lg-7">
                <template v-if="canFacilitate">
                  <h6 class="fw-bold">Start a standup</h6>
                  <p class="small text-muted">
                    Everyone on the project is added in order; anyone else can join from this window. Each person's
                    claimed work in the current sprint shows next to their check-in.
                  </p>
                  <form class="row g-3" @submit.prevent="startStandup">
                    <div class="col-12">
                      <label for="standup-title" class="form-label small fw-bold mb-1">Title</label>
                      <input id="standup-title" v-model="start.title" type="text" class="form-control form-control-sm" maxlength="120" placeholder="Defaults to today's date" />
                    </div>
                    <div class="col-sm-6">
                      <label for="standup-timer" class="form-label small fw-bold mb-1">Time per person</label>
                      <select id="standup-timer" v-model.number="start.speakerSeconds" class="form-select form-select-sm">
                        <option v-for="s in [0, 60, 90, 120, 180, 300]" :key="s" :value="s">{{ formatTimerSetting(s) }}</option>
                      </select>
                    </div>
                    <div class="col-sm-6 d-flex align-items-center">
                      <div class="form-check mt-3">
                        <input id="standup-shuffle" v-model="start.shuffle" class="form-check-input" type="checkbox" />
                        <label class="form-check-label small" for="standup-shuffle">Random speaking order</label>
                      </div>
                    </div>
                    <div class="col-12">
                      <button type="submit" class="btn btn-primary" :disabled="busy"><i class="bi bi-play-fill me-1" aria-hidden="true" />Start standup</button>
                    </div>
                  </form>
                </template>
                <template v-else>
                  <h6 class="fw-bold">No standup running</h6>
                  <p class="small text-muted mb-0">When someone starts one it opens here. Starting one needs the <strong>Run standups</strong> permission.</p>
                </template>
              </div>
              <div v-if="history.length" class="col-lg-5">
                <h6 class="fw-bold">Recent standups</h6>
                <ul class="list-unstyled small mb-0 vstack gap-2">
                  <li v-for="h in history" :key="h.id">
                    <div class="fw-semibold text-truncate">{{ h.title }}</div>
                    <div class="text-muted">{{ sessionDate(h.created_at) }} · {{ h.counts.entry ?? 0 }} check-ins · {{ h.counts.blocked ?? 0 }} blocked</div>
                  </li>
                </ul>
              </div>
            </div>
          </div>

          <!-- Running -->
          <div v-else class="standup-layout">
            <aside class="standup-order" aria-label="Speaking order">
              <div class="standup-order-head">
                <span class="small fw-bold">Speaking order</span>
                <button v-if="isFacilitator && !allSpoken" type="button" class="btn btn-sm btn-link p-0" :disabled="busy" @click="step({ action: 'shuffle' })">
                  <i class="bi bi-shuffle me-1" aria-hidden="true" />Shuffle rest
                </button>
              </div>
              <ol class="standup-people">
                <li v-for="(p, i) in session.participants" :key="p.user_id" class="standup-person" :class="`is-${participantState(i)}`">
                  <button
                    type="button"
                    class="standup-person-btn"
                    :disabled="!isFacilitator || busy"
                    :aria-current="participantState(i) === 'current' ? 'step' : undefined"
                    @click="step({ action: 'goto', index: i })"
                  >
                    <i
                      class="bi"
                      :class="{
                        'bi-check-circle-fill text-success': participantState(i) === 'spoken',
                        'bi-mic-fill text-primary': participantState(i) === 'current',
                        'bi-circle text-secondary': participantState(i) === 'upcoming',
                      }"
                      aria-hidden="true"
                    />
                    <span class="visually-hidden">{{ participantState(i) }}:</span>
                    <span class="text-truncate flex-grow-1">{{ p.name }}<template v-if="p.user_id === me"> (you)</template></span>
                    <i v-if="hasBlocker(p)" class="bi bi-exclamation-triangle-fill text-warning" title="Blocked" aria-label="Blocked" />
                    <i v-if="p.entry" class="bi bi-journal-check text-muted" title="Checked in" aria-label="Checked in" />
                  </button>
                  <button
                    v-if="isFacilitator"
                    type="button"
                    class="btn btn-sm btn-link text-muted standup-remove"
                    :aria-label="`Remove ${p.name}`"
                    :disabled="busy"
                    @click="removeParticipant(p)"
                  >
                    <i class="bi bi-x-lg" aria-hidden="true" />
                  </button>
                </li>
              </ol>
              <form v-if="isFacilitator && addable.length" class="standup-add" @submit.prevent="addParticipant">
                <label for="standup-add-person" class="visually-hidden">Add someone</label>
                <div class="input-group input-group-sm">
                  <select id="standup-add-person" v-model.number="addUserId" class="form-select">
                    <option :value="0">Add someone…</option>
                    <option v-for="m in addable" :key="m.user_id" :value="m.user_id">{{ m.full_name || m.user_name || m.email }}</option>
                  </select>
                  <button type="submit" class="btn btn-outline-secondary" :disabled="busy || !addUserId">Add</button>
                </div>
              </form>
              <div v-if="!session.joined" class="standup-add">
                <button type="button" class="btn btn-sm btn-primary w-100" :disabled="busy" @click="join">Join standup</button>
              </div>
            </aside>

            <section class="standup-stage" aria-live="polite">
              <div class="standup-stage-scroll">
                <!-- Everyone has spoken: summary -->
                <template v-if="allSpoken">
                  <div class="d-flex align-items-center gap-2 mb-3">
                    <i class="bi bi-check2-all fs-4 text-success" aria-hidden="true" />
                    <h6 class="fw-bold mb-0">Everyone has spoken</h6>
                    <span class="small text-muted ms-2">{{ blockedCount }} blocked</span>
                  </div>
                  <ul class="list-unstyled vstack gap-3 mb-0">
                    <li v-for="p in session.participants" :key="p.user_id" class="standup-summary-row">
                      <div class="fw-semibold">{{ p.name }}</div>
                      <template v-if="p.entry">
                        <div v-if="p.entry.done" class="small"><span class="text-muted">Done:</span> {{ p.entry.done }}</div>
                        <div v-if="p.entry.today" class="small"><span class="text-muted">Today:</span> {{ p.entry.today }}</div>
                        <div v-if="p.entry.blockers || p.blocker_tasks.length" class="small text-warning-emphasis">
                          <i class="bi bi-exclamation-triangle me-1" aria-hidden="true" />{{ p.entry.blockers }}
                          <RouterLink v-for="t in p.blocker_tasks" :key="t.id" class="ms-1" :to="taskPageRoute(t.id, project)" target="_blank" rel="noopener">#{{ t.id }}</RouterLink>
                        </div>
                      </template>
                      <div v-else class="small text-muted">No check-in.</div>
                    </li>
                  </ul>
                  <div v-if="isFacilitator" class="d-flex gap-2 mt-4">
                    <button type="button" class="btn btn-sm btn-outline-secondary" :disabled="busy" @click="step({ action: 'prev' })">
                      <i class="bi bi-arrow-left me-1" aria-hidden="true" />Back
                    </button>
                    <button type="button" class="btn btn-sm btn-primary" :disabled="busy" @click="endStandup">End standup</button>
                  </div>
                </template>

                <!-- Current speaker -->
                <template v-else-if="current">
                  <div class="standup-speaker-head">
                    <div class="min-w-0">
                      <div class="small text-muted">Now speaking</div>
                      <h4 class="h4 fw-bold mb-0 text-truncate">{{ current.name }}<template v-if="current.user_id === me"> (you)</template></h4>
                    </div>
                    <div
                      v-if="secondsLeft != null"
                      class="standup-timer"
                      :class="{ 'is-over': secondsLeft < 0, 'is-low': secondsLeft >= 0 && secondsLeft <= 15 }"
                      role="timer"
                      :aria-label="secondsLeft < 0 ? `${-secondsLeft} seconds over` : `${secondsLeft} seconds left`"
                    >
                      {{ formatClock(secondsLeft) }}
                    </div>
                  </div>

                  <div class="standup-grid">
                    <div class="standup-field">
                      <div class="standup-label">Done since last time</div>
                      <p class="mb-0 standup-text">{{ current.entry?.done || '—' }}</p>
                    </div>
                    <div class="standup-field">
                      <div class="standup-label">Today</div>
                      <p class="mb-0 standup-text">{{ current.entry?.today || '—' }}</p>
                    </div>
                    <div class="standup-field" :class="{ 'is-blocked': hasBlocker(current) }">
                      <div class="standup-label">Blockers</div>
                      <p class="mb-1 standup-text">{{ current.entry?.blockers || 'None' }}</p>
                      <ul v-if="current.blocker_tasks.length" class="list-unstyled small mb-0">
                        <li v-for="t in current.blocker_tasks" :key="t.id">
                          <RouterLink :to="taskPageRoute(t.id, project)" target="_blank" rel="noopener">#{{ t.id }} {{ t.title }}</RouterLink>
                          <span v-if="t.status_name" class="text-muted"> · {{ t.status_name }}</span>
                        </li>
                      </ul>
                    </div>
                  </div>

                  <h6 class="fw-bold mt-4 mb-2">Claimed work<template v-if="session.sprint_name"> in {{ session.sprint_name }}</template></h6>
                  <ul v-if="current.claimed.length" class="list-unstyled small vstack gap-1 mb-0">
                    <li v-for="t in current.claimed" :key="t.id" class="d-flex gap-2">
                      <RouterLink class="text-truncate" :to="taskPageRoute(t.id, project)" target="_blank" rel="noopener">#{{ t.id }} {{ t.title }}</RouterLink>
                      <span v-if="t.status_name" class="badge text-bg-light border">{{ t.status_name }}</span>
                      <span v-if="t.estimate_hours != null" class="text-muted">{{ formatHours(t.estimate_hours) }}</span>
                    </li>
                  </ul>
                  <p v-else class="small text-muted mb-0">Nothing claimed.</p>
                </template>
              </div>

              <div v-if="isFacilitator && !allSpoken" class="standup-controls">
                <button type="button" class="btn btn-sm btn-outline-secondary" :disabled="busy || session.current === 0" @click="step({ action: 'prev' })">
                  <i class="bi bi-arrow-left me-1" aria-hidden="true" />Previous
                </button>
                <button type="button" class="btn btn-sm btn-primary" :disabled="busy" @click="step({ action: 'next' })">
                  {{ session.current + 1 >= session.participants.length ? 'Finish round' : 'Next person' }}<i class="bi bi-arrow-right ms-1" aria-hidden="true" />
                </button>
              </div>
              <p v-else-if="!isFacilitator && !allSpoken" class="small text-muted mb-0 standup-controls">
                <i class="bi bi-broadcast me-1" aria-hidden="true" />Following {{ session.facilitator_name || 'the facilitator' }}
              </p>
            </section>

            <!-- Your check-in -->
            <aside v-if="session.joined" class="standup-mine" aria-labelledby="standup-mine-title">
              <h6 id="standup-mine-title" class="fw-bold mb-2">Your check-in</h6>
              <form class="vstack gap-2" @submit.prevent="saveEntry" @input="entryDirty = true">
                <div>
                  <label for="standup-done" class="form-label small mb-0">Done since last time</label>
                  <textarea id="standup-done" v-model="entry.done" class="form-control form-control-sm" rows="3" :maxlength="MAX_LEN" />
                </div>
                <div>
                  <label for="standup-today" class="form-label small mb-0">Today</label>
                  <textarea id="standup-today" v-model="entry.today" class="form-control form-control-sm" rows="3" :maxlength="MAX_LEN" />
                </div>
                <div>
                  <label for="standup-blockers" class="form-label small mb-0">Blockers</label>
                  <textarea id="standup-blockers" v-model="entry.blockers" class="form-control form-control-sm" rows="2" :maxlength="MAX_LEN" />
                </div>
                <div>
                  <label for="standup-links" class="form-label small mb-0">Blocked by tasks</label>
                  <input id="standup-links" v-model="entry.linkIds" type="text" class="form-control form-control-sm" placeholder="#ids, e.g. 42 57" aria-describedby="standup-links-help" />
                  <small id="standup-links-help" class="form-hint">Tasks in this project; up to 10.</small>
                </div>
                <button type="submit" class="btn btn-sm btn-primary" :disabled="savingEntry || !entryDirty">
                  {{ myRow?.entry && !entryDirty ? 'Saved' : 'Save check-in' }}
                </button>
              </form>
              <form v-if="session.can_create_task" class="mt-3" @submit.prevent="createBlocker">
                <label for="standup-blocker-task" class="form-label small fw-bold mb-1">New blocker task</label>
                <div class="input-group input-group-sm">
                  <input id="standup-blocker-task" v-model="blockerTitle" type="text" class="form-control" maxlength="200" placeholder="What's in the way?" />
                  <button type="submit" class="btn btn-outline-warning" :disabled="busy || !blockerTitle.trim()">Create</button>
                </div>
              </form>
            </aside>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.standup-body {
  height: min(80vh, 800px);
  overflow: hidden;
}

.standup-empty {
  height: 100%;
  overflow-y: auto;
}

.standup-layout {
  display: grid;
  grid-template-columns: 240px minmax(0, 1fr) 280px;
  height: 100%;
}

.standup-order,
.standup-mine {
  display: flex;
  flex-direction: column;
  min-height: 0;
  overflow-y: auto;
}

.standup-order {
  border-right: 1px solid var(--bs-border-color-translucent, rgba(0, 0, 0, 0.1));
}

.standup-mine {
  border-left: 1px solid var(--bs-border-color-translucent, rgba(0, 0, 0, 0.1));
  padding: 1rem;
}

.standup-order-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0.75rem 1rem 0.5rem;
}

.standup-people {
  list-style: none;
  margin: 0;
  padding: 0 0.5rem;
  flex: 1 1 auto;
}

.standup-person {
  display: flex;
  align-items: center;
  border-radius: 0.375rem;
}

.standup-person.is-current {
  background: var(--bs-primary-bg-subtle);
}

.standup-person.is-spoken .standup-person-btn {
  opacity: 0.7;
}

.standup-person-btn {
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

.standup-person-btn:disabled {
  opacity: 1;
  cursor: default;
}

.standup-remove {
  padding: 0.25rem 0.4rem;
  opacity: 0;
}

.standup-person:hover .standup-remove,
.standup-remove:focus-visible {
  opacity: 1;
}

.standup-add {
  padding: 0.75rem 1rem;
  border-top: 1px solid var(--bs-border-color-translucent, rgba(0, 0, 0, 0.1));
}

.standup-stage {
  display: flex;
  flex-direction: column;
  min-width: 0;
  min-height: 0;
}

.standup-stage-scroll {
  flex: 1 1 auto;
  overflow-y: auto;
  padding: 1.25rem 1.5rem;
}

.standup-speaker-head {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 1rem;
  margin-bottom: 1rem;
}

.standup-timer {
  font-variant-numeric: tabular-nums;
  font-size: 2rem;
  font-weight: 700;
  line-height: 1;
  padding: 0.35rem 0.75rem;
  border-radius: 0.5rem;
  background: var(--section-label-bg, rgba(0, 0, 0, 0.05));
}

.standup-timer.is-low {
  color: var(--bs-warning-text-emphasis);
  background: var(--bs-warning-bg-subtle);
}

.standup-timer.is-over {
  color: var(--bs-danger-text-emphasis);
  background: var(--bs-danger-bg-subtle);
}

.standup-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 0.75rem;
}

.standup-field {
  padding: 0.75rem;
  border: 1px solid var(--bs-border-color-translucent, rgba(0, 0, 0, 0.1));
  border-radius: 0.5rem;
}

.standup-field.is-blocked {
  border-color: var(--bs-warning-border-subtle);
  background: var(--bs-warning-bg-subtle);
}

.standup-label {
  font-size: 0.7rem;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.05em;
  color: var(--bs-secondary-color);
  margin-bottom: 0.25rem;
}

.standup-text {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}

.standup-summary-row {
  padding-bottom: 0.75rem;
  border-bottom: 1px solid var(--bs-border-color-translucent, rgba(0, 0, 0, 0.1));
}

.standup-controls {
  display: flex;
  justify-content: flex-end;
  align-items: center;
  gap: 0.5rem;
  padding: 0.75rem 1.5rem;
  border-top: 1px solid var(--bs-border-color-translucent, rgba(0, 0, 0, 0.1));
}

@media (max-width: 1199.98px) {
  .standup-layout {
    grid-template-columns: 220px minmax(0, 1fr);
  }

  .standup-mine {
    grid-column: 1 / -1;
    border-left: 0;
    border-top: 1px solid var(--bs-border-color-translucent, rgba(0, 0, 0, 0.1));
  }

  .standup-body {
    overflow-y: auto;
  }

  .standup-layout {
    height: auto;
  }
}

@media (max-width: 767.98px) {
  .standup-body {
    height: auto;
    max-height: calc(100dvh - 8rem);
  }

  .standup-layout {
    grid-template-columns: 1fr;
  }

  .standup-order {
    border-right: 0;
    border-bottom: 1px solid var(--bs-border-color-translucent, rgba(0, 0, 0, 0.1));
  }

  .standup-grid {
    grid-template-columns: 1fr;
  }

  .standup-remove {
    opacity: 1;
  }

  .standup-stage-scroll,
  .standup-controls {
    padding-left: 1rem;
    padding-right: 1rem;
  }
}
</style>
