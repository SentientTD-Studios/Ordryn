<script setup lang="ts">
import { onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api } from '@/api/client'
import { useTaskSidebar } from '@/composables/useTaskSidebar'
import { hasAnyProjectWrite } from '@/utils/projectPerms'

const route = useRoute()
const router = useRouter()
const { openEdit, openView } = useTaskSidebar()

onMounted(async () => {
  const id = Number(route.params.id)
  if (id > 0) {
    try {
      const [task, projects] = await Promise.all([api.getTask(id), api.listProjects()])
      const project =
        task.project_id != null ? projects.find((p) => p.id === task.project_id) : undefined
      if (project && hasAnyProjectWrite(project)) openEdit(id)
      else openView(id)
    } catch {
      openView(id)
    }
  }
  await router.replace({ name: 'tasks' })
})
</script>

<template>
  <p class="text-muted p-3">Opening task…</p>
</template>
