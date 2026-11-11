<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { RouterLink } from 'vue-router'
import { api } from '@/api/client'
import type { Onboarding } from '@/api/types'

const props = defineProps<{
  /** Changes when the user's tasks change, so finished steps tick off. */
  refreshKey?: string | number
}>()

const data = ref<Onboarding | null>(null)
const hiding = ref(false)

async function load() {
  try {
    data.value = await api.getOnboarding()
  } catch {
    data.value = null
  }
}

onMounted(load)
watch(() => props.refreshKey, () => {
  if (data.value && !data.value.dismissed && !data.value.complete) void load()
})

const visible = computed(() => !!data.value && !data.value.dismissed && !data.value.complete)
const doneCount = computed(() => data.value?.steps.filter((s) => s.done).length ?? 0)
const total = computed(() => data.value?.steps.length ?? 0)
const nextStep = computed(() => data.value?.steps.find((s) => !s.done)?.id)

async function dismiss() {
  hiding.value = true
  try {
    data.value = await api.setOnboardingDismissed(true)
  } catch {
    if (data.value) data.value = { ...data.value, dismissed: true }
  } finally {
    hiding.value = false
  }
}
</script>

<template>
  <section v-if="visible && data" class="onboarding card mb-3" aria-labelledby="onboarding-title">
    <div class="card-body">
      <div class="d-flex align-items-start gap-2 mb-2">
        <div>
          <h2 id="onboarding-title" class="h6 fw-bold mb-0">Getting started</h2>
          <p class="small text-muted mb-0">{{ doneCount }} of {{ total }} done</p>
        </div>
        <button
          type="button"
          class="btn btn-sm btn-link text-muted ms-auto"
          :disabled="hiding"
          @click="dismiss"
        >
          Hide
        </button>
      </div>
      <div
        class="progress mb-3"
        role="progressbar"
        :aria-valuenow="doneCount"
        aria-valuemin="0"
        :aria-valuemax="total"
        aria-label="Getting started progress"
        style="height: 6px"
      >
        <div class="progress-bar" :style="{ width: `${total ? (doneCount / total) * 100 : 0}%` }" />
      </div>
      <ol class="list-unstyled mb-0 onboarding-steps">
        <li
          v-for="s in data.steps"
          :key="s.id"
          class="d-flex gap-2 py-1"
          :class="{ 'is-next': s.id === nextStep }"
        >
          <i
            class="bi mt-1"
            :class="s.done ? 'bi-check-circle-fill text-success' : 'bi-circle text-muted'"
            :aria-label="s.done ? 'Done' : 'Not done'"
          />
          <div class="flex-grow-1">
            <RouterLink v-if="!s.done" :to="s.href" class="fw-semibold">{{ s.label }}</RouterLink>
            <span v-else class="text-muted text-decoration-line-through">{{ s.label }}</span>
            <span v-if="s.admin_only" class="badge text-bg-light border ms-1">Admin</span>
            <div v-if="!s.done" class="small text-muted">{{ s.description }}</div>
          </div>
        </li>
      </ol>
    </div>
  </section>
</template>

<style scoped>
.onboarding {
  background: var(--ordryn-card-bg);
  border-color: var(--ordryn-card-border);
}
.onboarding-steps .is-next {
  background: var(--ordryn-muted-bg);
  border-radius: 6px;
  padding-left: 6px;
  padding-right: 6px;
}
</style>
