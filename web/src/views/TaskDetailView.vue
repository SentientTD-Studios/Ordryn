<script setup lang="ts">
import { onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useTaskSidebar } from '@/composables/useTaskSidebar'

const route = useRoute()
const router = useRouter()
const { openEdit } = useTaskSidebar()

onMounted(async () => {
  const id = Number(route.params.id)
  if (id > 0) openEdit(id)
  await router.replace({ name: 'tasks' })
})
</script>

<template>
  <p class="text-muted p-3">Opening task…</p>
</template>
