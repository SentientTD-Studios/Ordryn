<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { api } from '@/api/client'
import type { CeremonyKind, Project, ProjectSprint } from '@/api/types'
import RefinementSessionModal from '@/components/RefinementSessionModal.vue'
import RetroSessionModal from '@/components/RetroSessionModal.vue'
import StandupSessionModal from '@/components/StandupSessionModal.vue'
import { useProjectCeremonies } from '@/composables/useProjectCeremonies'
import { useLiveUpdates, LIVE_CEREMONY_UPDATED, LIVE_RESYNC } from '@/composables/useLiveUpdates'

const props = defineProps<{ project: Project; sprints: ProjectSprint[] }>()
const emit = defineEmits<{ 'tasks-changed': [] }>()

const { settings } = useProjectCeremonies(() => props.project.id)

const CEREMONIES: { kind: CeremonyKind; label: string; icon: string }[] = [
  { kind: 'standup', label: 'Standup', icon: 'bi-people' },
  { kind: 'refinement', label: 'Refinement', icon: 'bi-list-check' },
  { kind: 'retro', label: 'Retro', icon: 'bi-arrow-repeat' },
]

const visible = computed(() => CEREMONIES.filter((c) => settings.value?.active[c.kind]))
const live = reactive<Record<CeremonyKind, boolean>>({ refinement: false, retro: false, standup: false })
const openKind = ref<CeremonyKind | null>(null)

const activeLoaders: Record<CeremonyKind, (pid: number) => Promise<{ session: unknown | null }>> = {
  refinement: (pid) => api.getActiveRefinementSession(pid),
  retro: (pid) => api.getActiveRetro(pid),
  standup: (pid) => api.getActiveStandup(pid),
}

async function checkLive(kind: CeremonyKind) {
  const pid = props.project.id
  if (!settings.value?.active[kind]) {
    live[kind] = false
    return
  }
  try {
    const res = await activeLoaders[kind](pid)
    if (props.project.id === pid) live[kind] = !!res.session
  } catch {
    live[kind] = false
  }
}

function checkAll() {
  for (const c of CEREMONIES) void checkLive(c.kind)
}

watch([() => props.project.id, settings], () => {
  openKind.value = null
  checkAll()
}, { immediate: true })

useLiveUpdates(
  (_e, batch) => {
    const pid = props.project.id
    if (batch.some((e) => e.type === LIVE_RESYNC)) {
      checkAll()
      return
    }
    const kinds = new Set<CeremonyKind>()
    for (const e of batch) {
      if (e.type === LIVE_CEREMONY_UPDATED && e.project_id === pid && e.key) {
        const kind = e.key.split(':')[0] as CeremonyKind
        if (kind in live) kinds.add(kind)
      }
    }
    // Only start/end changes the dot, but any step may be the end; the check is one small request.
    kinds.forEach((k) => {
      if (openKind.value !== k) void checkLive(k)
    })
  },
  400,
  { ceremonies: true },
)
</script>

<template>
  <div v-if="visible.length" class="ceremony-launcher" role="group" aria-label="Team ceremonies">
    <button
      v-for="c in visible"
      :key="c.kind"
      type="button"
      class="btn btn-sm ceremony-launch"
      :class="live[c.kind] ? 'btn-success' : 'btn-outline-secondary'"
      :title="live[c.kind] ? `A ${c.label.toLowerCase()} is running. Join it.` : c.label"
      @click="openKind = c.kind"
    >
      <i class="bi me-1" :class="c.icon" aria-hidden="true" />{{ c.label }}
      <span v-if="live[c.kind]" class="ceremony-launch-live">
        <span class="ceremony-live-dot" aria-hidden="true" /><span>Live</span>
      </span>
    </button>

    <RefinementSessionModal
      v-if="settings?.active.refinement"
      :open="openKind === 'refinement'"
      :project="project"
      :sprints="sprints"
      :can-facilitate="!!settings?.can_facilitate.refinement"
      @close="openKind = null"
      @active-changed="live.refinement = $event"
      @tasks-changed="emit('tasks-changed')"
    />
    <RetroSessionModal
      v-if="settings?.active.retro"
      :open="openKind === 'retro'"
      :project="project"
      :sprints="sprints"
      :can-facilitate="!!settings?.can_facilitate.retro"
      :defaults="settings?.retro ?? null"
      @close="openKind = null"
      @active-changed="live.retro = $event"
    />
    <StandupSessionModal
      v-if="settings?.active.standup"
      :open="openKind === 'standup'"
      :project="project"
      :can-facilitate="!!settings?.can_facilitate.standup"
      :defaults="settings?.standup ?? null"
      @close="openKind = null"
      @active-changed="live.standup = $event"
    />
  </div>
</template>

<style scoped>
.ceremony-launcher {
  display: inline-flex;
  flex-wrap: wrap;
  gap: 0.35rem;
}
</style>
