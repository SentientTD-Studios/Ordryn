import { computed, ref, type Ref } from 'vue'
import { api } from '@/api/client'
import type { Project, ProjectSprint, ProjectStatus, Task } from '@/api/types'

function storageKey(projectId: number) {
  return `gotodo.boardSprint.${projectId}`
}

function pickDefaultSprintKey(projectId: number, sprints: ProjectSprint[]): string {
  try {
    const stored = localStorage.getItem(storageKey(projectId))
    if (stored === 'backlog') return 'backlog'
    if (stored && sprints.some((s) => String(s.id) === stored)) return stored
  } catch {
    /* ignore */
  }
  const active = sprints.find((s) => s.is_active)
  return active ? String(active.id) : 'backlog'
}

/**
 * Sprint picker state for a kanban project view: the project's sprints and
 * board columns, and which sprint ('backlog' or an id) the view shows.
 */
export function useBoardSprints(project: Readonly<Ref<Project | null>>, isKanban: Readonly<Ref<boolean>>) {
  const sprints = ref<ProjectSprint[]>([])
  const statuses = ref<ProjectStatus[]>([])
  const sprintKey = ref('backlog')
  let loadedFor: number | null = null

  async function ensureLoaded() {
    const p = project.value
    if (!p || !isKanban.value) {
      sprints.value = []
      statuses.value = []
      loadedFor = null
      return
    }
    if (loadedFor === p.id) return
    const [s, st] = await Promise.all([
      api.listProjectSprints(p.id).catch(() => [] as ProjectSprint[]),
      api.listProjectStatuses(p.id).catch(() => [] as ProjectStatus[]),
    ])
    sprints.value = s
    statuses.value = st
    sprintKey.value = pickDefaultSprintKey(p.id, s)
    loadedFor = p.id
  }

  /** Forget the loaded project so the next ensureLoaded refetches sprints and columns. */
  function invalidate() {
    loadedFor = null
  }

  function persist() {
    const p = project.value
    if (!p) return
    try {
      localStorage.setItem(storageKey(p.id), sprintKey.value)
    } catch {
      /* ignore */
    }
  }

  const selectedSprint = computed(() => {
    if (sprintKey.value === 'backlog') return null
    const id = parseInt(sprintKey.value, 10)
    return sprints.value.find((s) => s.id === id) ?? null
  })

  /** undefined outside kanban, null for the backlog, else the sprint id. */
  function currentSprintId(): number | null | undefined {
    if (!isKanban.value) return undefined
    if (sprintKey.value === 'backlog') return null
    const n = parseInt(sprintKey.value, 10)
    return Number.isNaN(n) ? undefined : n
  }

  function taskInSprint(task: Task): boolean {
    if (!isKanban.value) return true
    const assigned = task.sprint_id && task.sprint_id > 0 ? task.sprint_id : 0
    if (sprintKey.value === 'backlog') return assigned === 0
    return assigned === parseInt(sprintKey.value, 10)
  }

  return { sprints, statuses, sprintKey, selectedSprint, ensureLoaded, invalidate, persist, currentSprintId, taskInSprint }
}
