import { watch } from 'vue'
import { useRoute, useRouter, type LocationQuery } from 'vue-router'
import { api } from '@/api/client'
import type { Task } from '@/api/types'
import { useAuth } from '@/composables/useAuth'
import { allowTaskRouteNavigation, useTaskSidebar } from '@/composables/useTaskSidebar'

/**
 * Keep the open task modal in the URL, so reloading, bookmarking or sharing the
 * address reopens it over the same page:
 * - on a project board: /p/{slug}/tasks/{id} (the task's own link);
 * - on any other page: ?task={id}.
 * Loading either form opens the task. Opening and closing a task replaces the
 * history entry rather than adding one.
 *
 * Call once, from the app shell.
 */
export function useTaskUrlSync() {
  const route = useRoute()
  const router = useRouter()
  const { isAuthenticated } = useAuth()
  const { open, mode, taskId, openTask } = useTaskSidebar()

  function isProjectRoute() {
    return route.name === 'project' || route.name === 'project-task'
  }

  // The modal isn't mounted on public share pages; /tasks/:id redirects on its own.
  function routeTakesTask() {
    return (
      isAuthenticated.value &&
      route.matched.length > 0 &&
      route.name !== 'share' &&
      route.name !== 'task' &&
      !route.meta.guest
    )
  }

  function openTaskId(): number | null {
    return open.value && (mode.value === 'edit' || mode.value === 'view') && taskId.value ? taskId.value : null
  }

  function positiveId(raw: unknown): number | null {
    const id = Number(Array.isArray(raw) ? raw[0] : raw)
    return Number.isSafeInteger(id) && id > 0 ? id : null
  }

  /** The task the URL names, in either form. */
  function urlTaskId(): number | null {
    if (route.name === 'project-task') return positiveId(route.params.id)
    return positiveId(route.query.task)
  }

  function queryWithoutTask(): LocationQuery {
    const query: LocationQuery = { ...route.query }
    delete query.task
    return query
  }

  function writeUrl() {
    if (!routeTakesTask()) return
    const want = openTaskId()
    if (isProjectRoute()) {
      if (want === urlTaskId() && route.query.task === undefined) return
      const ref = String(route.params.ref || '')
      if (want) {
        allowTaskRouteNavigation(want)
        void router.replace({ name: 'project-task', params: { ref, id: String(want) }, query: queryWithoutTask(), hash: route.hash })
      } else {
        void router.replace({ name: 'project', params: { ref }, query: queryWithoutTask(), hash: route.hash })
      }
      return
    }
    if (want === urlTaskId() && (want !== null || route.query.task === undefined)) return
    const query = queryWithoutTask()
    if (want) query.task = String(want)
    void router.replace({ query, hash: route.hash })
  }

  /** A task link loaded under the wrong project (the task moved): switch to its project. */
  async function correctProject(task: Task | null) {
    if (!task || route.name !== 'project-task' || positiveId(route.params.id) !== task.id) return
    if (task.project_id == null) {
      void router.replace({ name: 'tasks', query: { ...queryWithoutTask(), task: String(task.id) } })
      return
    }
    const ref = String(route.params.ref || '')
    if (ref === String(task.project_id)) return
    const project = await api.getProject(task.project_id).catch(() => null)
    if (!project || ref === project.slug || route.name !== 'project-task') return
    allowTaskRouteNavigation(task.id)
    void router.replace({
      name: 'project-task',
      params: { ref: project.slug || String(project.id), id: String(task.id) },
      query: route.query,
      hash: route.hash,
    })
  }

  // URL -> modal: a page loaded (or navigated) with a task in the URL opens it.
  watch(
    () => [route.fullPath, isAuthenticated.value] as const,
    async () => {
      if (!routeTakesTask()) return
      const id = urlTaskId()
      if (id !== null && id !== openTaskId()) {
        const task = await openTask(id)
        await correctProject(task)
        return
      }
      // Moved to another page with a task still open: carry it along.
      if (id === null && openTaskId() !== null) writeUrl()
    },
    { immediate: true },
  )

  // Modal -> URL: opening, switching or closing a task updates the URL.
  watch([open, mode, taskId], writeUrl)
}
