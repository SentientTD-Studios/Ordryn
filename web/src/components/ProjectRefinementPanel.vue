<script setup lang="ts">
import { ref, watch } from 'vue'
import { api } from '@/api/client'
import type { CeremonySettings, Project, RefinementSessionSummary } from '@/api/types'
import { APIError } from '@/api/types'
import { useToast } from '@/composables/useToast'
import { primeProjectCeremonies } from '@/composables/useProjectCeremonies'

const props = defineProps<{ project: Project }>()
const emit = defineEmits<{ changed: [] }>()

const toast = useToast()
const settings = ref<CeremonySettings | null>(null)
const history = ref<RefinementSessionSummary[]>([])
const loading = ref(true)
const saving = ref(false)

async function loadHistory() {
  if (!settings.value?.active.refinement) {
    history.value = []
    return
  }
  try {
    history.value = await api.listRefinementSessions(props.project.id, 10)
  } catch {
    history.value = []
  }
}

async function load() {
  loading.value = true
  try {
    settings.value = await api.getCeremonySettings(props.project.id)
    await loadHistory()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Could not load refinement settings', 'error')
  } finally {
    loading.value = false
  }
}

async function update(patch: Parameters<typeof api.updateCeremonySettings>[1], message: string) {
  saving.value = true
  try {
    const next = await api.updateCeremonySettings(props.project.id, patch)
    settings.value = next
    primeProjectCeremonies(props.project.id, next)
    await loadHistory()
    toast.push(message, 'success')
    emit('changed')
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Save failed', 'error')
    await load()
  } finally {
    saving.value = false
  }
}

function onToggleEnabled(e: Event) {
  const on = (e.target as HTMLInputElement).checked
  void update({ ceremonies: { refinement: on } }, on ? 'Refinement turned on' : 'Refinement turned off')
}

function onToggleDefault(e: Event) {
  const on = (e.target as HTMLInputElement).checked
  void update({ new_tasks_need_refinement: on }, 'Refinement default saved')
}

function sessionDate(iso: string) {
  return new Date(iso).toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric' })
}

watch(() => props.project.id, () => void load(), { immediate: true })
</script>

<template>
  <div>
    <h4 class="h6 mb-2">Backlog refinement</h4>
    <p class="small text-muted">
      Run live refinement sessions from the board: a facilitator walks the team through tasks flagged
      <strong>Needs Refinement</strong>, everyone sees the same task as it updates, and the team settles the details
      and the estimate before the facilitator marks it <strong>Refined</strong>.
    </p>

    <p v-if="loading" class="small text-muted">Loading…</p>
    <template v-else-if="settings">
      <div v-if="!settings.site.refinement" class="alert alert-secondary small py-2" role="status">
        A site admin hasn't turned on refinement for this site yet (Admin → Settings → Team ceremonies).
      </div>

      <div class="form-check form-switch mb-2">
        <input
          id="refinement-enabled"
          class="form-check-input"
          type="checkbox"
          role="switch"
          :checked="settings.project.refinement"
          :disabled="!settings.can_manage || !settings.site.refinement || saving"
          @change="onToggleEnabled"
        />
        <label class="form-check-label" for="refinement-enabled">Use refinement on this board</label>
      </div>

      <div class="form-check mb-1 ms-1">
        <input
          id="refinement-default"
          class="form-check-input"
          type="checkbox"
          :checked="settings.new_tasks_need_refinement"
          :disabled="!settings.can_manage || !settings.active.refinement || saving"
          @change="onToggleDefault"
        />
        <label class="form-check-label" for="refinement-default">New tasks start as Needs Refinement</label>
      </div>
      <p class="small text-muted ms-1">
        Anyone who can edit a task can change its flag. Running a session needs the
        <strong>Run refinement sessions</strong> permission, which owners have; grant it to other roles on the
        Roles tab.
      </p>
      <p v-if="!settings.can_manage" class="small text-muted">Only project managers can change these settings.</p>

      <template v-if="settings.active.refinement">
        <h5 class="h6 mt-4 mb-2">Recent sessions</h5>
        <p v-if="!history.length" class="small text-muted mb-0">
          No sessions yet. Start one with <strong>Refinement</strong> on the board.
        </p>
        <ul v-else class="list-group list-group-flush small refinement-history">
          <li v-for="s in history" :key="s.id" class="list-group-item px-0 d-flex flex-wrap gap-2 align-items-baseline">
            <span class="fw-semibold">{{ s.title }}</span>
            <span v-if="s.status === 'active'" class="badge text-bg-success">Live</span>
            <span class="text-muted">{{ sessionDate(s.created_at) }}</span>
            <span class="text-muted">· {{ s.facilitator_name || 'No facilitator' }}</span>
            <span class="ms-auto text-nowrap">
              {{ s.refined }} refined · {{ s.skipped }} skipped · {{ s.queued }} queued
            </span>
          </li>
        </ul>
      </template>
    </template>
  </div>
</template>

<style scoped>
.refinement-history .list-group-item {
  background: transparent;
  color: inherit;
}
</style>
