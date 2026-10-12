<script setup lang="ts">
import { ref, watch } from 'vue'
import { api } from '@/api/client'
import type { TaskEvent } from '@/api/types'

const props = defineProps<{ taskId: number }>()

const events = ref<TaskEvent[]>([])
const loaded = ref(false)
const loading = ref(false)

// Activity loads when the section is first opened.
async function load(force = false) {
  if (loading.value || (loaded.value && !force)) return
  const id = props.taskId
  loading.value = true
  try {
    const list = await api.listTaskEvents(id)
    if (id !== props.taskId) return
    events.value = list
    loaded.value = true
  } catch {
    events.value = []
  } finally {
    loading.value = false
  }
}

watch(
  () => props.taskId,
  () => {
    events.value = []
    loaded.value = false
  },
)

/** Reload after a change, but only if the user has already opened the log. */
async function refresh() {
  if (loaded.value) await load(true)
}

defineExpose({ refresh })
</script>

<template>
  <details class="task-timeline mt-3" @toggle="(e) => { if ((e.target as HTMLDetailsElement).open) void load() }">
    <summary class="mb-2">Activity</summary>
    <div class="task-timeline-body">
      <p v-if="loading" class="text-muted small mb-0">Loading activity…</p>
      <ul v-else-if="events.length" class="list-unstyled small mb-0">
        <li v-for="ev in events" :key="ev.id" class="mb-1">
          <span class="text-muted">{{ ev.created_at }}</span> — {{ ev.label }}
          <span v-if="ev.actor_user_name || ev.actor_email"> · {{ ev.actor_user_name || ev.actor_email }}</span>
        </li>
      </ul>
      <p v-else class="text-muted small mb-0">No activity recorded.</p>
    </div>
  </details>
</template>
