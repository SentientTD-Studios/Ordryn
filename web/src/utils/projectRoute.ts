import type { RouteLocationNamedRaw } from 'vue-router'

/**
 * Route to a project's board: /p/{slug}, or /p/{id} when the slug isn't known
 * (the board resolves ids and former slugs and redirects to the current slug).
 * '' is the home list and '0' the "no project" list.
 */
export function projectRoute(project: { id: number; slug?: string } | number | string): RouteLocationNamedRaw {
  if (typeof project === 'object') {
    return { name: 'project', params: { ref: project.slug || String(project.id) } }
  }
  const id = String(project)
  if (!id) return { name: 'tasks' }
  if (id === '0') return { name: 'tasks', query: { project: '0' } }
  return { name: 'project', params: { ref: id } }
}

/**
 * Route to a task's full page: /p/{project}/tasks/{id}, inside its project, or
 * /tasks/{id} when the project isn't known (the router redirects it when it has one).
 */
export function taskPageRoute(taskId: number, project?: { id: number; slug?: string } | null): RouteLocationNamedRaw {
  if (project) {
    return { name: 'project-task', params: { ref: project.slug || String(project.id), id: String(taskId) } }
  }
  return { name: 'task', params: { id: String(taskId) } }
}
