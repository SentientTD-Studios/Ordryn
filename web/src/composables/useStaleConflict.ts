import { api } from '@/api/client'
import { APIError, type Task } from '@/api/types'
import { useConfirm } from '@/composables/useConfirm'
import { refreshLiveData } from '@/composables/useLiveUpdates'

/** The server refused an action because the view it came from is out of date. */
export function isStaleBoardError(err: unknown): err is APIError {
  return err instanceof APIError && err.status === 409 && err.code === 'stale_board'
}

/**
 * The server refused a task save because the task changed after this view
 * loaded it. Returns the task as it is now (when the server sent it), else
 * null for any other error.
 */
export function staleTaskOf(err: unknown): { current: Task | null } | null {
  if (!(err instanceof APIError) || err.status !== 409 || err.code !== 'stale_task') return null
  const p = (err.payload ?? {}) as { task?: Task }
  return { current: p.task ?? null }
}

/** `expected_version` for a PATCH, or nothing when the view never saw a version. */
export function versionGuard(version: number | null | undefined): { expected_version?: number } {
  return typeof version === 'number' && version > 0 ? { expected_version: version } : {}
}

export type ClaimConflict ={ claimedBy: number; claimedByName: string; message: string }

/** The task is held by someone other than the caller expected. */
export function claimConflictOf(err: unknown): ClaimConflict | null {
  if (!(err instanceof APIError) || err.status !== 409 || err.code !== 'claim_conflict') return null
  const p = (err.payload ?? {}) as { claimed_by?: number; claimed_by_name?: string }
  return {
    claimedBy: typeof p.claimed_by === 'number' ? p.claimed_by : 0,
    claimedByName: p.claimed_by_name || 'another member',
    message: err.message,
  }
}

export function useStaleConflict() {
  const { askConfirm } = useConfirm()

  /** Tell the person their view is out of date and offer to refresh it. */
  async function promptRefresh(message: string): Promise<boolean> {
    const ok = await askConfirm({
      title: 'Out of date',
      message: `${message} Nothing was changed. Refresh to see the latest?`,
      confirmLabel: 'Refresh',
      cancelLabel: 'Not now',
      warning: true,
    })
    if (ok) refreshLiveData()
    return ok
  }

  /**
   * Handle a refused save for views without an edit form (toggles, drags,
   * inline renames): say so and offer to refresh. Returns false when `err`
   * is not a stale-task refusal, so callers can report it the usual way.
   */
  async function handleStaleTask(err: unknown): Promise<boolean> {
    if (!staleTaskOf(err)) return false
    await promptRefresh('This task was changed somewhere else since this page loaded.')
    return true
  }

  /** Ask before taking a task from whoever holds it. */
  function confirmTakeOver(holderName: string, taskTitle?: string): Promise<boolean> {
    const what = taskTitle ? `"${taskTitle}"` : 'this task'
    return askConfirm({
      title: 'Take over task?',
      message: `${holderName} has claimed ${what}. Take it over from them?`,
      confirmLabel: 'Take over',
      cancelLabel: 'Cancel',
      warning: true,
    })
  }

  /**
   * Claim a task, asking before taking it from someone else. `seenClaimedBy`
   * is who this view shows holding it; if the server knows better, the
   * person is told who holds it now and asked again. Resolves to the updated
   * task, or null when they decline. Other errors are rethrown.
   */
  async function claimTask(
    task: { id: number; title?: string; claimed_by?: number | null; claimed_by_name?: string | null },
    userId: number | null | undefined,
  ): Promise<Task | null> {
    const seen = task.claimed_by ?? 0
    if (seen && seen !== userId) {
      if (!(await confirmTakeOver(task.claimed_by_name || 'Another member', task.title))) return null
      return takeOver(task.id, seen)
    }
    try {
      return await api.claimTask(task.id, { expectedClaimedBy: seen })
    } catch (err) {
      const c = claimConflictOf(err)
      if (!c) throw err
      // This view showed it free, but someone has claimed it since.
      if (!(await confirmTakeOver(c.claimedByName, task.title))) {
        refreshLiveData()
        return null
      }
      return takeOver(task.id, c.claimedBy)
    }
  }

  async function takeOver(taskId: number, from: number): Promise<Task | null> {
    try {
      return await api.claimTask(taskId, { takeOver: true, expectedClaimedBy: from })
    } catch (err) {
      const c = claimConflictOf(err)
      if (!c) throw err
      await promptRefresh(c.message)
      return null
    }
  }

  /** Release a claim this view shows as `seenClaimedBy`'s. Null when the view was stale. */
  async function releaseTask(taskId: number, seenClaimedBy: number | null | undefined): Promise<Task | null> {
    try {
      return await api.unclaimTask(taskId, seenClaimedBy ?? 0)
    } catch (err) {
      const c = claimConflictOf(err)
      if (!c) throw err
      await promptRefresh(c.message)
      return null
    }
  }

  return { promptRefresh, handleStaleTask, confirmTakeOver, claimTask, releaseTask }
}
