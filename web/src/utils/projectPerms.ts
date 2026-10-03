import type { Project, ProjectStatus, User } from '@/api/types'

export const PROJECT_PERMS = {
  TASKS_CREATE: 'tasks:create',
  TASKS_EDIT: 'tasks:edit',
  TASKS_DELETE: 'tasks:delete',
  TASKS_ARCHIVE: 'tasks:archive',
  TASKS_RESTORE: 'tasks:restore',
  TASKS_COMPLETE: 'tasks:complete',
  TASKS_CLAIM: 'tasks:claim',
  TASKS_REORDER: 'tasks:reorder',
  TASKS_STATUS: 'tasks:status',
  TASKS_SPRINT: 'tasks:sprint',
  PROJECT_MANAGE: 'project:manage',
  PROJECT_TAGS: 'project:tags',
  TIME_WRITE: 'time:write',
  EXTENSIONS_WRITE: 'extensions:write',
  COMMENTS_MODERATE: 'comments:moderate',
} as const

export const WRITE_TASK_PERMS = [
  PROJECT_PERMS.TASKS_CREATE,
  PROJECT_PERMS.TASKS_EDIT,
  PROJECT_PERMS.TASKS_DELETE,
  PROJECT_PERMS.TASKS_ARCHIVE,
  PROJECT_PERMS.TASKS_RESTORE,
  PROJECT_PERMS.TASKS_COMPLETE,
  PROJECT_PERMS.TASKS_CLAIM,
  PROJECT_PERMS.TASKS_REORDER,
  PROJECT_PERMS.TASKS_STATUS,
  PROJECT_PERMS.TASKS_SPRINT,
  PROJECT_PERMS.TIME_WRITE,
] as const

export function hasProjectPerm(project: Project | null | undefined, perm: string): boolean {
  if (!project) return true
  const role = project.role
  if (!role || role === 'owner') return true
  return (project.permissions || []).includes(perm)
}

export function hasAnyProjectWrite(project: Project | null | undefined): boolean {
  if (!project) return true
  const role = project.role
  if (!role || role === 'owner') return true
  const perms = project.permissions || []
  return WRITE_TASK_PERMS.some((p) => perms.includes(p))
}

export function canManageProject(project: Project | null | undefined): boolean {
  return hasProjectPerm(project, PROJECT_PERMS.PROJECT_MANAGE)
}

export function isSiteAdmin(user?: Pick<User, 'permissions'> | null): boolean {
  return !!(user?.permissions || []).includes('admin')
}

export function bypassesStatusGates(
  project: Project | null | undefined,
  user?: Pick<User, 'permissions'> | null,
): boolean {
  return canManageProject(project) || isSiteAdmin(user)
}

function gateAllows(slugs: string[] | undefined, role: string): boolean {
  if (!slugs || slugs.length === 0) return true
  return slugs.includes(role)
}

export function canMoveTaskStatus(
  project: Project | null | undefined,
  from: ProjectStatus | null | undefined,
  to: ProjectStatus | null | undefined,
  user?: Pick<User, 'permissions'> | null,
): boolean {
  if (!hasProjectPerm(project, PROJECT_PERMS.TASKS_STATUS)) return false
  if (!to || (from && from.id === to.id)) return true
  if (bypassesStatusGates(project, user)) return true
  const role = project?.role || ''
  return gateAllows(from?.leave_role_slugs, role) && gateAllows(to.enter_role_slugs, role)
}

export function statusHasGates(status: ProjectStatus | null | undefined): boolean {
  return !!((status?.enter_role_slugs && status.enter_role_slugs.length) || (status?.leave_role_slugs && status.leave_role_slugs.length))
}

export function discussionAuthorLabel(
  userName: string,
  roleName?: string | null,
  isCurrentUser?: boolean,
): string {
  const name = isCurrentUser ? 'You' : userName
  const role = roleName && roleName.trim()
  return role ? `${name} - ${role}` : name
}

export function slugifyRoleName(name: string): string {
  const slug = name
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, 40)
  if (!slug) return ''
  if (!/^[a-z]/.test(slug)) return `r-${slug}`.slice(0, 40)
  return slug
}
