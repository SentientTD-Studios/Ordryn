/** Query helpers for kanban project task lists (board and list views). */

/** Kanban project views include unclaimed work; the global/home list stays "mine". */
export function kanbanWorkflowClaimScope(isKanbanProject: boolean): 'all' | 'mine' {
  return isKanbanProject ? 'all' : 'mine'
}

/** Sprint filter for a kanban project view. `backlog` maps to `none` (unassigned). */
export function kanbanSprintQueryValue(
  isKanbanProject: boolean,
  sprintKey: string,
): string | undefined {
  if (!isKanbanProject) return undefined
  if (!sprintKey || sprintKey === 'backlog') return 'none'
  return sprintKey
}
