export type ProjectSettingsSection =
  | 'details'
  | 'sharing'
  | 'roles'
  | 'board'
  | 'sprints'
  | 'tags'
  | 'rules'
  | 'forms'
  | 'feedback'
  | 'github'
  | 'extensions'
  | 'api-keys'
  | 'agents'
  | 'estimates'
  | 'refinement'
  | 'retro'
  | 'standup'

export interface ProjectSettingsItem {
  id: ProjectSettingsSection
  label: string
  icon: string
}

export interface ProjectSettingsGroup {
  id: string
  label: string
  items: ProjectSettingsItem[]
}

export interface ProjectSettingsContext {
  isKanban: boolean
  canManage: boolean
  isCompanyMode: boolean
}

interface SectionDef extends ProjectSettingsItem {
  visible?: (ctx: ProjectSettingsContext) => boolean
}

const manage = (ctx: ProjectSettingsContext) => ctx.canManage

const GROUPS: { id: string; label: string; items: SectionDef[] }[] = [
  {
    id: 'general',
    label: 'General',
    items: [
      { id: 'details', label: 'Details', icon: 'bi-info-circle' },
      { id: 'sharing', label: 'Sharing', icon: 'bi-people' },
      {
        id: 'roles',
        label: 'Roles',
        icon: 'bi-shield-lock',
        visible: (ctx) => ctx.canManage && !ctx.isCompanyMode,
      },
    ],
  },
  {
    id: 'workflow',
    label: 'Workflow',
    items: [
      { id: 'board', label: 'Board', icon: 'bi-kanban' },
      { id: 'sprints', label: 'Sprints', icon: 'bi-flag', visible: (ctx) => ctx.isKanban },
      { id: 'estimates', label: 'Estimates', icon: 'bi-hourglass-split', visible: (ctx) => ctx.isKanban },
      { id: 'tags', label: 'Tags', icon: 'bi-tags' },
      { id: 'rules', label: 'Rules', icon: 'bi-lightning-charge', visible: manage },
    ],
  },
  {
    id: 'intake',
    label: 'Intake',
    items: [
      { id: 'forms', label: 'Request forms', icon: 'bi-ui-checks', visible: manage },
      { id: 'feedback', label: 'Feedback', icon: 'bi-chat-square-text', visible: manage },
    ],
  },
  {
    // Kanban-only team ceremonies.
    id: 'ceremonies',
    label: 'Ceremonies',
    items: [
      { id: 'standup', label: 'Standups', icon: 'bi-people', visible: (ctx) => ctx.isKanban },
      { id: 'refinement', label: 'Refinement', icon: 'bi-list-check', visible: (ctx) => ctx.isKanban },
      { id: 'retro', label: 'Retrospectives', icon: 'bi-arrow-repeat', visible: (ctx) => ctx.isKanban },
    ],
  },
  {
    id: 'integrations',
    label: 'Integrations',
    items: [
      { id: 'github', label: 'GitHub', icon: 'bi-github' },
      { id: 'extensions', label: 'Extensions', icon: 'bi-puzzle' },
      { id: 'api-keys', label: 'API keys', icon: 'bi-key', visible: manage },
      { id: 'agents', label: 'AI agents', icon: 'bi-robot', visible: manage },
    ],
  },
]

/** Settings sections visible for this project and viewer, grouped for the sidebar. Empty groups are dropped. */
export function projectSettingsGroups(ctx: ProjectSettingsContext): ProjectSettingsGroup[] {
  return GROUPS.map((g) => ({
    id: g.id,
    label: g.label,
    items: g.items
      .filter((item) => !item.visible || item.visible(ctx))
      .map(({ id, label, icon }) => ({ id, label, icon })),
  })).filter((g) => g.items.length > 0)
}

/** Returns `section` if it is visible, otherwise the always-present Details section. */
export function resolveProjectSettingsSection(
  section: ProjectSettingsSection,
  groups: ProjectSettingsGroup[],
): ProjectSettingsSection {
  return groups.some((g) => g.items.some((i) => i.id === section)) ? section : 'details'
}
