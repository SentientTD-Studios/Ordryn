<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { api } from '@/api/client'
import type { CeremonySessionSummary, CeremonySettings, CeremonySettingsPatch, Project, RetroTemplateId } from '@/api/types'
import { APIError } from '@/api/types'
import { useToast } from '@/composables/useToast'
import { primeProjectCeremonies } from '@/composables/useProjectCeremonies'
import { RETRO_TEMPLATE_LABELS } from '@/utils/retroBoard'

const props = defineProps<{ project: Project }>()
const emit = defineEmits<{ changed: [] }>()

const toast = useToast()
const settings = ref<CeremonySettings | null>(null)
const history = ref<CeremonySessionSummary[]>([])
const loading = ref(true)
const saving = ref(false)
const form = reactive({ template: 'went_well' as RetroTemplateId, maxVotes: 5, anonymous: false })

const dirty = computed(() => {
  const r = settings.value?.retro
  return !!r && (r.template !== form.template || r.max_votes !== form.maxVotes || r.anonymous !== form.anonymous)
})
const votesValid = computed(() => Number.isInteger(form.maxVotes) && form.maxVotes >= 0 && form.maxVotes <= 20)
const columnsPreview = computed(
  () => settings.value?.retro_templates.find((t) => t.id === form.template)?.columns.map((c) => c.title).join(' · ') ?? '',
)

function syncForm() {
  const r = settings.value?.retro
  if (!r) return
  form.template = r.template
  form.maxVotes = r.max_votes
  form.anonymous = r.anonymous
}

async function load() {
  loading.value = true
  try {
    settings.value = await api.getCeremonySettings(props.project.id)
    syncForm()
    history.value = settings.value.active.retro ? await api.listCeremonySessions(props.project.id, 'retro', 10).catch(() => []) : []
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Could not load retrospective settings', 'error')
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
    if (settings.value.active.retro && !history.value.length) {
      history.value = await api.listCeremonySessions(props.project.id, 'retro', 10).catch(() => [])
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
  void update({ ceremonies: { retro: on } }, on ? 'Retrospectives turned on' : 'Retrospectives turned off')
}

function saveDefaults() {
  if (!votesValid.value) return
  void update({ retro: { template: form.template, max_votes: form.maxVotes, anonymous: form.anonymous } }, 'Retro defaults saved')
}

function sessionDate(iso: string) {
  return new Date(iso).toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric' })
}

watch(() => props.project.id, () => void load(), { immediate: true })
</script>

<template>
  <div>
    <h4 class="h6 mb-2">Retrospectives</h4>
    <p class="small text-muted">
      Live retros from the board. The facilitator moves the team through four phases: <strong>Collect</strong>
      (cards stay hidden from others), <strong>Group</strong> (cards are revealed and stacked),
      <strong>Vote</strong>, and <strong>Act</strong> (agree action items and turn them into tasks).
    </p>

    <p v-if="loading" class="small text-muted">Loading…</p>
    <template v-else-if="settings">
      <div v-if="!settings.site.retro" class="alert alert-secondary small py-2" role="status">
        A site admin hasn't turned on retrospectives for this site yet (Admin → Settings → Team ceremonies).
      </div>
      <div class="form-check form-switch mb-3">
        <input
          id="retro-enabled"
          class="form-check-input"
          type="checkbox"
          role="switch"
          :checked="settings.project.retro"
          :disabled="!settings.can_manage || !settings.site.retro || saving"
          @change="onToggle"
        />
        <label class="form-check-label" for="retro-enabled">Use retrospectives on this board</label>
      </div>

      <form class="row g-3 retro-defaults" @submit.prevent="saveDefaults">
        <div class="col-12">
          <div class="small fw-bold">Defaults for new retros</div>
          <div class="small text-muted">The facilitator can change these when starting one.</div>
        </div>
        <div class="col-md-7">
          <label for="retro-default-template" class="form-label small mb-1">Columns</label>
          <select id="retro-default-template" v-model="form.template" class="form-select form-select-sm" :disabled="!settings.can_manage || saving">
            <option v-for="(label, id) in RETRO_TEMPLATE_LABELS" :key="id" :value="id">{{ label }}</option>
          </select>
          <small class="form-hint">{{ columnsPreview }}</small>
        </div>
        <div class="col-md-5">
          <label for="retro-default-votes" class="form-label small mb-1">Votes per person</label>
          <input
            id="retro-default-votes"
            v-model.number="form.maxVotes"
            type="number"
            class="form-control form-control-sm"
            :class="{ 'is-invalid': !votesValid }"
            min="0"
            max="20"
            :disabled="!settings.can_manage || saving"
            aria-describedby="retro-default-votes-help"
          />
          <small id="retro-default-votes-help" class="form-hint">0–20; 0 = unlimited</small>
        </div>
        <div class="col-12">
          <div class="form-check">
            <input id="retro-default-anon" v-model="form.anonymous" class="form-check-input" type="checkbox" :disabled="!settings.can_manage || saving" />
            <label class="form-check-label small" for="retro-default-anon">Anonymous cards (authors only see their own name)</label>
          </div>
        </div>
        <div v-if="settings.can_manage" class="col-12">
          <button type="submit" class="btn btn-sm btn-primary" :disabled="saving || !dirty || !votesValid">Save defaults</button>
        </div>
      </form>

      <p class="small text-muted mt-3">
        Anyone on the project can add cards and vote. Running a retro needs the <strong>Run retrospectives</strong>
        permission (owners have it; grant it to other roles on the Roles tab). Turning action items into tasks needs
        <strong>Create tasks</strong>.
      </p>
      <p v-if="!settings.can_manage" class="small text-muted">Only project managers can change these settings.</p>

      <template v-if="settings.active.retro">
        <h5 class="h6 mt-4 mb-2">Recent retrospectives</h5>
        <p v-if="!history.length" class="small text-muted mb-0">None yet. Start one with <strong>Retro</strong> on the board.</p>
        <ul v-else class="list-group list-group-flush small ceremony-history">
          <li v-for="s in history" :key="s.id" class="list-group-item px-0 d-flex flex-wrap gap-2 align-items-baseline">
            <span class="fw-semibold">{{ s.title }}</span>
            <span v-if="s.status === 'active'" class="badge text-bg-success">Live</span>
            <span class="text-muted">{{ sessionDate(s.created_at) }} · {{ s.facilitator_name || 'No facilitator' }}</span>
            <span class="ms-auto text-nowrap">{{ s.counts.card ?? 0 }} cards · {{ s.counts.action ?? 0 }} action items</span>
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
