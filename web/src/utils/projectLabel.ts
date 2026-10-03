import type { Project } from '@/api/types'

export function isArchivedProject(project: Project): boolean {
  return !!project.archived
}

/** Sidebar/nav label: shared projects include role. Does not append (archived). */
export function projectNavLabel(project: Project): string {
  if (!project.role || project.role === 'owner') return project.name
  const role = project.role_name || project.role
  return `${project.name} (${role})`
}

/** Label for project selects: shared projects include role, e.g. "Shared One (viewer)". */
export function projectOptionLabel(project: Project): string {
  const base = projectNavLabel(project)
  return isArchivedProject(project) ? `${base} (archived)` : base
}

export function activeProjects(projects: Project[]): Project[] {
  return projects.filter((p) => !isArchivedProject(p))
}

export function isProjectOwner(project: Project): boolean {
  return !project.role || project.role === 'owner'
}
