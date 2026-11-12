import type { Project, Task } from '@/api/types'
import { searchTaskId } from './search.ts'

/** Live results under the header search box, in display order. */
export type HeaderSearchItem =
  | { kind: 'open-id'; key: string; taskId: number }
  | { kind: 'task'; key: string; task: Task }
  | { kind: 'project'; key: string; project: Project }
  | { kind: 'all'; key: string; query: string }

export const HEADER_SEARCH_TASKS = 6
export const HEADER_SEARCH_PROJECTS = 3

/** Live results need two characters, or a task id ("7", "#7"). */
export function wantsLiveResults(raw: string): boolean {
  const q = raw.trim()
  return q.length >= 2 || searchTaskId(q) !== null
}

/**
 * Items for the header dropdown: "Open task #n" when the query is an id that the
 * results don't already hold, then title matches, then projects, then a last
 * item that runs the full search (descriptions, tags and comments too).
 */
export function headerSearchItems(raw: string, tasks: Task[], projects: Project[]): HeaderSearchItem[] {
  const q = raw.trim()
  if (!q) return []
  const out: HeaderSearchItem[] = []
  const n = searchTaskId(q)
  if (n !== null && !tasks.some((t) => t.id === n)) out.push({ kind: 'open-id', key: `open-${n}`, taskId: n })
  for (const task of tasks.slice(0, HEADER_SEARCH_TASKS)) out.push({ kind: 'task', key: `task-${task.id}`, task })
  for (const project of projects.slice(0, HEADER_SEARCH_PROJECTS)) {
    out.push({ kind: 'project', key: `project-${project.id}`, project })
  }
  out.push({ kind: 'all', key: 'all', query: q })
  return out
}
