<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { api } from '@/api/client'
import type { CeremonySessionSummary, CeremonySettings, CeremonySettingsPatch, Project } from '@/api/types'
import { APIError } from '@/api/types'
import { useToast } from '@/composables/useToast'
import { primeProjectCeremonies } from '@/composables/useProjectCeremonies'
import { formatTimerSetting } from '@/utils/standupTimer'

const props = defineProps<{ project: Project }>()
const emit = defineEmits<{ changed: [] }>()

const TIMER_CHOICES = [0, 60, 90, 120, 180, 300]

const toast = useToast()
const settings = ref<CeremonySettings | null>(null)
const history = ref<CeremonySessionSummary[]>([])
const loading = ref(true)
const saving = ref(false)
const form = reactive({ speakerSeconds: 120, shuffle: false })

const timerChoices = computed(() =>
  TIMER_CHOICES.includes(form.speakerSeconds) ? TIMER_CHOICES : [...TIMER_CHOICES, form.speakerSeconds].sort((a, b) => a - b),
)
const dirty = computed(() => {
  const s = settings.value?.standup
  return !!s && (s.speaker_seconds !== form.speakerSeconds || s.shuffle !== form.shuffle)
})

function syncForm() {
  const s = settings.value?.standup
  if (!s) return
  form.speakerSeconds = s.speaker_seconds
  form.shuffle = s.shuffle
}

async function load() {
  loading.value = true
  try {
    settings.value = await api.getCeremonySettings(props.project.id)
    syncForm()
    history.value = settings.value.active.standup ? await api.listCeremonySessions(props.project.id, 'standup', 10).catch(() => []) : []
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Could not load standup settings', 'error')
  } finally {
    loading.value = false
  }
}

async function update(patch: CeremonySettingsPatch, message: string) {
  saving.value = true
  try {
    settings.value = await api.updateCeremonySettings(props.project.id, patch)
    primeProjectCeremonies(props.project.id, settings.value)
    syncForm()
    if (settings.value.active.standup && !history.value.length) {
      history.value = await api.listCeremonySessions(props.project.id, 'standup', 10).catch(() => [])
    }
    toast.push(message, 'success')
    emit('changed')
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Save failed', 'error')
    await load()
  } finally {
    saving.value = false
  }
}

function onToggle(e: Event) {
  const on = (e.target as HTMLInputElement).checked
  void update({ ceremonies: { standup: on } }, on ? 'Standups turned on' : 'Standups turned off')
}

function saveDefaults() {
  void update({ standup: { speaker_seconds: form.speakerSeconds, shuffle: form.shuffle } }, 'Standup defaults saved')
}

function sessionDate(iso: string) {
  return new Date(iso).toLocaleDateString(undefined, { weekday: 'short', month: 'short', day: 'numeric' })
}

watch(() => props.project.id, () => void load(), { immediate: true })
</script>

<template>
  <div>
    <h4 class="h6 mb-2">Standups</h4>
    <p class="small text-muted">
      Run the daily standup from the board. The facilitator steps through the team one person at a time with an
      optional timer. Each person fills in what they did, what's next and what's blocking them, next to the work they
      have claimed. Blockers can link to tasks or create new ones.
    </p>

    <p v-if="loading" class="small text-muted">Loading…</p>
    <template v-else-if="settings">
      <div v-if="!settings.site.standup" class="alert alert-secondary small py-2" role="status">
        A site admin hasn't turned on standups for this site yet (Admin → Settings → Team ceremonies).
      </div>
      <div class="form-check form-switch mb-3">
        <input
          id="standup-enabled"
          class="form-check-input"
          type="checkbox"
          role="switch"
          :checked="settings.project.standup"
          :disabled="!settings.can_manage || !settings.site.standup || saving"
          @change="onToggle"
        />
        <label class="form-check-label" for="standup-enabled">Use standups on this board</label>
      </div>

      <form class="row g-3" @submit.prevent="saveDefaults">
        <div class="col-12">
          <div class="small fw-bold">Defaults for new standups</div>
          <div class="small text-muted">The facilitator can change these when starting one.</div>
        </div>
        <div class="col-md-6">
          <label for="standup-default-timer" class="form-label small mb-1">Time per person</label>
          <select id="standup-default-timer" v-model.number="form.speakerSeconds" class="form-select form-select-sm" :disabled="!settings.can_manage || saving">
            <option v-for="s in timerChoices" :key="s" :value="s">{{ formatTimerSetting(s) }}</option>
          </select>
        </div>
        <div class="col-md-6 d-flex align-items-end">
          <div class="form-check">
            <input id="standup-default-shuffle" v-model="form.shuffle" class="form-check-input" type="checkbox" :disabled="!settings.can_manage || saving" />
            <label class="form-check-label small" for="standup-default-shuffle">Random speaking order</label>
          </div>
        </div>
        <div v-if="settings.can_manage" class="col-12">
          <button type="submit" class="btn btn-sm btn-primary" :disabled="saving || !dirty">Save defaults</button>
        </div>
      </form>

      <p class="small text-muted mt-3">
        Everyone on the project is added when a standup starts, and others can join. Running one needs the
        <strong>Run standups</strong> permission (owners have it; grant it to other roles on the Roles tab).
      </p>
      <p v-if="!settings.can_manage" class="small text-muted">Only project managers can change these settings.</p>

      <template v-if="settings.active.standup">
        <h5 class="h6 mt-4 mb-2">Recent standups</h5>
        <p v-if="!history.length" class="small text-muted mb-0">None yet. Start one with <strong>Standup</strong> on the board.</p>
        <ul v-else class="list-group list-group-flush small ceremony-history">
          <li v-for="s in history" :key="s.id" class="list-group-item px-0 d-flex flex-wrap gap-2 align-items-baseline">
            <span class="fw-semibold">{{ s.title }}</span>
            <span v-if="s.status === 'active'" class="badge text-bg-success">Live</span>
            <span class="text-muted">{{ sessionDate(s.created_at) }} · {{ s.facilitator_name || 'No facilitator' }}</span>
            <span class="ms-auto text-nowrap">{{ s.counts.entry ?? 0 }} check-ins · {{ s.counts.blocked ?? 0 }} blocked</span>
          </li>
        </ul>
      </template>
    </template>
  </div>
</template>

<style scoped>
.ceremony-history .list-group-item {
  background: transparent;
  color: inherit;
}
</style>
