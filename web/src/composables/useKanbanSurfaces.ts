import { computed, ref, type Ref } from 'vue'
import { api } from '@/api/client'
import type { Project, ProjectExtension } from '@/api/types'
import { withBase } from '@/base'

export type SurfaceTab = { viewKey: string; label: string; icon?: string }

/**
 * Extension tabs (manifest surfaces at kanban.tab) for a kanban project, and
 * which one a view mode like `ext:<extension>:<surface>` points at.
 */
export function useKanbanSurfaces(project: Readonly<Ref<Project | null>>, isKanban: Readonly<Ref<boolean>>, viewMode: Readonly<Ref<string>>) {
  const extensions = ref<ProjectExtension[]>([])

  async function load() {
    const p = project.value
    if (!p || !isKanban.value) {
      extensions.value = []
      return
    }
    try {
      extensions.value = (await api.listProjectExtensions(p.id)).extensions || []
    } catch {
      extensions.value = []
    }
  }

  const tabs = computed(() => {
    const out: SurfaceTab[] = []
    for (const ext of extensions.value) {
      if (!ext.site_enabled || !ext.settings?.enabled) continue
      const surfaces = ext.manifest.surfaces || []
      const ui = ext.manifest.ui
      const resolved = [...surfaces]
      if (!surfaces.some((s) => s.at === 'project.extensions') && ui) {
        resolved.unshift({ id: 'settings', file: ui, at: 'project.extensions', label: ext.name })
      }
      for (const s of resolved) {
        if (s.at !== 'kanban.tab') continue
        out.push({
          viewKey: `ext:${ext.id}:${s.id}`,
          label: s.label || ext.name,
          icon: ext.manifest.icon ? withBase(`/api/v2/extensions/${ext.id}/icon`) : undefined,
        })
      }
    }
    return out
  })

  const active = computed(() => {
    const m = /^ext:([^:]+):(.+)$/.exec(viewMode.value)
    if (!m) return null
    const ext = extensions.value.find((e) => e.id === m[1])
    return ext ? { extensionId: m[1], surfaceId: m[2], ext } : null
  })

  return { extensions, tabs, active, load }
}
