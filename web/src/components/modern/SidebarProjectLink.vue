<script setup lang="ts">
import { computed } from 'vue'
import { useRouter } from 'vue-router'
import type { Project } from '@/api/types'
import { projectNavLabel, projectOptionLabel } from '@/utils/projectLabel'
import { projectRoute } from '@/utils/projectRoute'

const props = defineProps<{
  project: Project
  active: boolean
  pinned: boolean
  /** Show a drag handle (pinned list only). */
  draggable?: boolean
  /** Hide the hover actions (mobile drawer keeps rows simple). */
  compact?: boolean
}>()

const emit = defineEmits<{
  select: []
  'toggle-pin': []
  edit: []
}>()

const router = useRouter()
const href = computed(() => router.resolve(projectRoute(props.project)).href)

function onClick(e: MouseEvent) {
  // Let the browser handle new-tab/window clicks on the real link.
  if (e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return
  e.preventDefault()
  emit('select')
}

const tooltip = computed(() =>
  props.project.description
    ? `${projectOptionLabel(props.project)} — ${props.project.description}`
    : projectOptionLabel(props.project),
)
</script>

<template>
  <li
    class="sidebar-nav-item position-relative"
    :class="{ 'project-reorder-item': draggable }"
    :data-project-id="project.id"
  >
    <div class="d-flex align-items-center justify-content-between w-100 min-w-0">
      <span
        v-if="draggable && !compact"
        class="project-drag-handle sidebar-row-action text-muted d-none d-md-inline-flex me-1 hover-reveal"
        title="Drag to reorder"
        aria-label="Drag to reorder pinned project"
        @click.stop
      >
        <i class="bi bi-grip-vertical" style="font-size: 0.85rem;" />
      </span>
      <a
        :href="href"
        class="sidebar-nav-link flex-grow-1 min-w-0"
        :class="{ active }"
        :data-tooltip="tooltip"
        :title="tooltip"
        :aria-current="active ? 'page' : undefined"
        @click="onClick"
      >
        <i class="bi bi-folder2-open" />
        <span class="sidebar-text text-truncate">{{ projectNavLabel(project) }}</span>
      </a>
      <template v-if="!compact">
        <button
          type="button"
          class="ordryn-pin-btn sidebar-row-action hover-reveal d-none d-md-inline-block"
          :class="{ 'is-pinned': pinned }"
          :title="pinned ? 'Unpin from sidebar' : 'Pin to sidebar'"
          :aria-label="pinned ? `Unpin ${project.name}` : `Pin ${project.name}`"
          @click.stop="emit('toggle-pin')"
        >
          <i :class="pinned ? 'bi bi-pin-angle-fill' : 'bi bi-pin-angle'" style="font-size: 0.85rem;" />
        </button>
        <button
          type="button"
          class="btn btn-sm text-muted p-0 border-0 sidebar-row-action hover-reveal d-none d-md-inline-block flex-shrink-0 ms-1 me-2"
          title="Project settings"
          :aria-label="`Settings for ${project.name}`"
          @click.stop="emit('edit')"
        >
          <i class="bi bi-pencil" style="font-size: 0.85rem;" />
        </button>
      </template>
    </div>
  </li>
</template>
