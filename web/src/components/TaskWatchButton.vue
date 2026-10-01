<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { api } from '@/api/client'
import type { TaskWatch } from '@/api/types'
import { useToast } from '@/composables/useToast'

const props = defineProps<{ taskId: number }>()
const toast = useToast()
const state = ref<TaskWatch | null>(null)
const busy = ref(false)

const label = computed(() => (state.value?.watching ? 'Watching' : 'Watch'))
const title = computed(() => {
  const s = state.value
  if (!s) return 'Watch this task'
  const names = s.watchers.map((w) => w.name).filter(Boolean)
  const who = names.length ? `Watchers: ${names.join(', ')}` : 'No direct watchers'
  if (s.via_project) return `Watching through the project. ${who}`
  return `${s.watching ? 'Stop watching' : 'Get notified about changes'}. ${who}`
})

async function load() {
  state.value = null
  if (!props.taskId) return
  try {
    state.value = await api.getTaskWatch(props.taskId)
  } catch {
    state.value = null
  }
}

async function toggle() {
  if (!state.value || busy.value || state.value.via_project) return
  busy.value = true
  try {
    state.value = state.value.watching ? await api.unwatchTask(props.taskId) : await api.watchTask(props.taskId)
  } catch (err) {
    toast.push(err instanceof Error ? err.message : 'Could not update watch', 'error')
  } finally {
    busy.value = false
  }
}

watch(() => props.taskId, load, { immediate: true })
</script>

<template>
  <button
    v-if="state"
    type="button"
    class="btn btn-sm"
    :class="state.watching ? 'btn-primary' : 'btn-outline-secondary'"
    :title="title"
    :aria-pressed="state.watching"
    :disabled="busy || state.via_project"
    @click="toggle"
  >
    <i class="bi" :class="state.watching ? 'bi-eye-fill' : 'bi-eye'" /> {{ label }}
    <span v-if="state.watchers.length" class="badge text-bg-light ms-1">{{ state.watchers.length }}</span>
  </button>
</template>
