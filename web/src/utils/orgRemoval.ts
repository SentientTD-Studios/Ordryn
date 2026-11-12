import type { OrgRemovalProject } from '@/api/types'

/** How a person got onto a project, for the removal dialog. */
export function orgRemovalModeLabel(mode: OrgRemovalProject['mode']): string {
  switch (mode) {
    case 'lock':
      return 'roster locked to the organization'
    case 'copy':
      return 'copied from the organization'
    case 'select':
      return 'picked by hand'
    case 'share':
      return "through this team's share"
    case 'personal':
      return 'personal share'
  }
}

/**
 * Splits a member's organization projects for the removal dialog: the ones they
 * leave with the organization, and hand-assigned ones the remover may also take
 * them off. Projects copied from the organization start ticked (the organization
 * put them there); ones picked by hand or shared personally start unticked.
 */
export function splitOrgRemovalProjects(projects: OrgRemovalProject[]) {
  const automatic = projects.filter((p) => p.automatic)
  const optional = projects.filter((p) => !p.automatic)
  const preselected = optional.filter((p) => p.mode === 'copy').map((p) => p.id)
  return { automatic, optional, preselected }
}
