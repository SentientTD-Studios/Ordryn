import { ref, type Ref } from 'vue'
import type { Task } from '@/api/types'

export type TaskListMatchers = {
  /** Done/open counts cover the project and sprint only. */
  inProjectScope: (task: Task) => boolean
  /** Whether the task belongs on screen under every active filter. */
  matchesFilters: (task: Task) => boolean
}

/**
 * The loaded task tree plus its header counts, and the local edits that keep
 * them in step with server changes without a full reload. Root tasks carry
 * their loaded children; subtasks are never root rows.
 */
export function useTaskListState(matchers: TaskListMatchers, selected: Ref<number[]> = ref([])) {
  const tasks = ref<Task[]>([])
  const total = ref(0)
  const completedCount = ref(0)
  const incompleteCount = ref(0)

  function findTaskInTree(id: number): { parent: Task | null; task: Task } | null {
    for (const t of tasks.value) {
      if (t.id === id) return { parent: null, task: t }
      const child = t.children?.find((c) => c.id === id)
      if (child) return { parent: t, task: child }
    }
    return null
  }

  function refreshParentCounts(parent: Task) {
    const kids = parent.children || []
    parent.child_count = kids.length
    parent.children_completed = kids.filter((c) => c.completed).length
  }

  function bumpCount(completed: boolean, delta: 1 | -1) {
    const target = completed ? completedCount : incompleteCount
    target.value = Math.max(0, target.value + delta)
  }

  function adjustCompletionCounts(wasCompleted: boolean, isCompleted: boolean) {
    if (wasCompleted === isCompleted) return
    bumpCount(wasCompleted, -1)
    bumpCount(isCompleted, 1)
  }

  function registerTaskAdded(task: Task) {
    if (task.parent_id) {
      const parent = tasks.value.find((t) => t.id === task.parent_id)
      if (parent) {
        if (!parent.children) parent.children = []
        if (!parent.children.some((c) => c.id === task.id)) {
          parent.children = [...parent.children, task]
          refreshParentCounts(parent)
        }
      }
      // A subtask whose parent isn't loaded never becomes a root row.
      bumpCount(task.completed, 1)
      return
    }
    if (tasks.value.some((t) => t.id === task.id)) return
    if (matchers.inProjectScope(task)) bumpCount(task.completed, 1)
    if (!matchers.matchesFilters(task)) return
    total.value += 1
    tasks.value = [...tasks.value, { ...task, children: task.children || [] }]
  }

  function removeTaskLocally(task: Task) {
    const found = findTaskInTree(task.id)
    if (!found) return
    if (found.parent) {
      found.parent.children = (found.parent.children || []).filter((c) => c.id !== task.id)
      refreshParentCounts(found.parent)
      bumpCount(task.completed, -1)
      selected.value = selected.value.filter((id) => id !== task.id)
      return
    }
    const childIds = (task.children || []).map((c) => c.id)
    tasks.value = tasks.value.filter((t) => t.id !== task.id)
    total.value = Math.max(0, total.value - 1)
    bumpCount(task.completed, -1)
    for (const c of task.children || []) bumpCount(c.completed, -1)
    selected.value = selected.value.filter((id) => id !== task.id && !childIds.includes(id))
  }

  /** Nested children spread onto the existing row; omitted JSON nulls must not keep old values. */
  function mergeNestedChild(existing: Task, updated: Task): Task {
    return {
      ...existing,
      ...updated,
      claimed_by: updated.claimed_by ?? null,
      claimed_by_name: updated.claimed_by_name || '',
      children: undefined,
    }
  }

  function applyTaskUpdate(updated: Task) {
    const found = findTaskInTree(updated.id)
    const previous = found?.task ?? null

    if (updated.parent_id) {
      if (found?.parent) {
        adjustCompletionCounts(previous!.completed, updated.completed)
        found.parent.children = (found.parent.children || []).map((c) =>
          c.id === updated.id ? mergeNestedChild(c, updated) : c,
        )
        refreshParentCounts(found.parent)
        return
      }
      const parent = tasks.value.find((t) => t.id === updated.parent_id)
      if (parent) {
        // Newly nested or moved under a parent already in the list
        if (found && !found.parent) {
          tasks.value = tasks.value.filter((t) => t.id !== updated.id)
          total.value = Math.max(0, total.value - 1)
        }
        if (!parent.children) parent.children = []
        parent.children = [...parent.children.filter((c) => c.id !== updated.id), { ...updated, children: undefined }]
        refreshParentCounts(parent)
        if (previous) adjustCompletionCounts(previous.completed, updated.completed)
        return
      }
    }

    if (!matchers.matchesFilters(updated)) {
      if (found) removeTaskLocally(found.task)
      // removeTaskLocally already took the old state off the counts; add the new one back.
      if (previous && matchers.inProjectScope(updated)) bumpCount(updated.completed, 1)
      return
    }

    if (found && !found.parent) {
      adjustCompletionCounts(found.task.completed, updated.completed)
      const idx = tasks.value.findIndex((t) => t.id === updated.id)
      const existingChildren = found.task.children || updated.children || []
      tasks.value[idx] = {
        ...updated,
        children: existingChildren,
        child_count: existingChildren.length,
        children_completed: existingChildren.filter((c) => c.completed).length,
      }
      return
    }

    if (found?.parent && !updated.parent_id) {
      // Promoted to root
      found.parent.children = (found.parent.children || []).filter((c) => c.id !== updated.id)
      refreshParentCounts(found.parent)
      tasks.value = [...tasks.value, { ...updated, children: [] }]
      total.value += 1
      return
    }

    tasks.value = [...tasks.value, { ...updated, children: updated.children || [] }]
  }

  function reorderLocalTasks(orderedIds: number[], parentId?: number | null) {
    if (parentId) {
      const parent = tasks.value.find((t) => t.id === parentId)
      if (!parent?.children) return
      parent.children = orderedIds
        .map((id) => parent.children!.find((c) => c.id === id))
        .filter((t): t is Task => !!t)
      return
    }
    const reordered = orderedIds
      .map((id) => tasks.value.find((t) => t.id === id))
      .filter((t): t is Task => !!t)
    const leftover = tasks.value.filter((t) => !orderedIds.includes(t.id))
    tasks.value = [...reordered, ...leftover]
  }

  /** Keep column order + status_id in sync without a full reload after a board drag. */
  function applyBoardReorder(payload: { statusId: number; taskIds: number[] }) {
    const { statusId, taskIds } = payload
    const idSet = new Set(taskIds)
    const byId = new Map(tasks.value.map((t) => [t.id, t]))
    const columnRootIds = taskIds.filter((id) => byId.has(id))
    const columnSet = new Set(columnRootIds)

    const updatedRoots: Task[] = tasks.value.map((t) => {
      const next: Task = idSet.has(t.id) ? { ...t, status_id: statusId } : { ...t }
      const kids = (next.children || []).map((c) => (idSet.has(c.id) ? { ...c, status_id: statusId } : c))
      return { ...next, children: kids }
    })

    const orderedColumn = columnRootIds
      .map((id) => updatedRoots.find((t) => t.id === id))
      .filter((t): t is Task => t != null)

    const result: Task[] = []
    let inserted = false
    for (const t of updatedRoots) {
      if (columnSet.has(t.id)) {
        if (!inserted) {
          result.push(...orderedColumn)
          inserted = true
        }
        continue
      }
      result.push(t)
    }
    if (!inserted) result.push(...orderedColumn)
    tasks.value = result
  }

  return {
    tasks,
    total,
    completedCount,
    incompleteCount,
    selected,
    findTaskInTree,
    registerTaskAdded,
    removeTaskLocally,
    applyTaskUpdate,
    reorderLocalTasks,
    applyBoardReorder,
  }
}
