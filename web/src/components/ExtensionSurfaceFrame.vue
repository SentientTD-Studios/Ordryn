<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { withBase } from '@/base'
import { attachExtensionBridge, type ExtensionBridgeContext } from '@/composables/useExtensionBridge'
import { useLiveUpdates } from '@/composables/useLiveUpdates'

const props = defineProps<{
  projectId: number
  extensionId: string
  surfaceId: string
  sprintId: number
  context: ExtensionBridgeContext
}>()

const frame = ref<HTMLIFrameElement | null>(null)
let bridge: ReturnType<typeof attachExtensionBridge> | null = null

const src = computed(() => {
  const q = new URLSearchParams({
    surface: props.surfaceId,
    project_id: String(props.projectId),
    sprint_id: String(props.sprintId),
  })
  return withBase(`/api/v2/projects/${props.projectId}/extensions/${encodeURIComponent(props.extensionId)}/ui?${q}`)
})

function bind() {
  bridge?.destroy()
  bridge = null
  const el = frame.value
  if (!el) return
  bridge = attachExtensionBridge(el, {
    extensionId: props.extensionId,
    projectId: props.projectId,
    getContext: () => props.context,
  })
}

function onLoad() {
  bind()
  bridge?.pushContext()
}

watch(
  () => [props.projectId, props.extensionId, props.surfaceId] as const,
  () => {
    bridge?.destroy()
    bridge = null
  },
)

watch(
  () => props.context,
  () => {
    bridge?.pushContext()
  },
  { deep: true },
)

useLiveUpdates((event) => {
  if (event.type !== 'extension.store') return
  if (event.project_id && event.project_id !== props.projectId) return
  if (event.extension_id && event.extension_id !== props.extensionId) return
  if (event.key) bridge?.notifyStoreChanged(event.key)
})

onBeforeUnmount(() => {
  bridge?.destroy()
  bridge = null
})
</script>

<template>
  <iframe
    ref="frame"
    class="extension-surface-frame w-100 border rounded"
    :src="src"
    sandbox="allow-scripts allow-forms allow-popups"
    referrerpolicy="no-referrer"
    title="Extension surface"
    @load="onLoad"
  />
</template>

<style scoped>
.extension-surface-frame {
  min-height: min(70vh, 40rem);
  height: calc(100vh - 16rem);
  background: var(--bs-body-bg);
}
</style>
