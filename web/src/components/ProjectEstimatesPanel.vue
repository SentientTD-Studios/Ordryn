<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { api } from '@/api/client'
import type { CeremonySettings, Project } from '@/api/types'
import { APIError } from '@/api/types'
import { useToast } from '@/composables/useToast'
import { primeProjectCeremonies } from '@/composables/useProjectCeremonies'
import { formatHours } from '@/utils/estimates'

const props = defineProps<{ project: Project }>()
const emit = defineEmits<{ changed: [] }>()

const toast = useToast()
const settings = ref<CeremonySettings | null>(null)
const loading = ref(true)
const saving = ref(false)
const rate = ref<number | ''>('')

const rateValid = computed(() => {
  const n = Number(rate.value)
  return rate.value !== '' && n >= 0.25 && n <= 100 && Number.isInteger(n * 4)
})
const dirty = computed(() => settings.value != null && Number(rate.value) !== settings.value.hours_per_point)

async function load() {
  loading.value = true
  try {
    settings.value = await api.getCeremonySettings(props.project.id)
    rate.value = settings.value.hours_per_point
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Could not load estimate settings', 'error')
  } finally {
    loading.value = false
  }
}

async function save() {
  if (!rateValid.value || !dirty.value) return
  saving.value = true
  try {
    const next = await api.updateCeremonySettings(props.project.id, { hours_per_point: Number(rate.value) })
    settings.value = next
    rate.value = next.hours_per_point
    primeProjectCeremonies(props.project.id, next)
    toast.push('Estimate settings saved', 'success')
    emit('changed')
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Save failed', 'error')
  } finally {
    saving.value = false
  }
}

watch(() => props.project.id, () => void load(), { immediate: true })
</script>

<template>
  <div>
    <h4 class="h6 mb-2">Estimates</h4>
    <p class="small text-muted">
      Tasks are estimated in hours, in quarter-hour steps. Estimates entered as story points before the switch to
      hours are converted at the rate below, and follow it if you change it later. An estimate someone re-enters in
      hours stays as entered.
    </p>

    <p v-if="loading" class="small text-muted">Loading…</p>
    <form v-else-if="settings" class="estimate-rate-form" @submit.prevent="save">
      <label for="estimate-hours-per-point" class="form-label small fw-bold mb-1">One story point equals</label>
      <div class="d-flex align-items-center gap-2 flex-wrap">
        <div class="input-group input-group-sm estimate-rate-input">
          <input
            id="estimate-hours-per-point"
            v-model.number="rate"
            type="number"
            class="form-control"
            :class="{ 'is-invalid': rate !== '' && !rateValid }"
            min="0.25"
            max="100"
            step="0.25"
            inputmode="decimal"
            :disabled="!settings.can_manage || saving"
            aria-describedby="estimate-rate-help"
          />
          <span class="input-group-text">hours</span>
        </div>
        <button
          v-if="settings.can_manage"
          type="submit"
          class="btn btn-sm btn-primary"
          :disabled="saving || !rateValid || !dirty"
        >
          Save
        </button>
      </div>
      <small id="estimate-rate-help" class="form-hint d-block mt-1">
        0.25–100 hours. With {{ formatHours(settings.hours_per_point) }} per point, a 3-point task shows as
        {{ formatHours(settings.hours_per_point * 3) }}.
      </small>
      <p v-if="!settings.can_manage" class="small text-muted mt-2 mb-0">Only project managers can change this.</p>
    </form>
  </div>
</template>

<style scoped>
.estimate-rate-input {
  max-width: 11rem;
}
</style>
