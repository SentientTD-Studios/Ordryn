import type { Project } from '@/api/types'

/** How many projects to show in the sidebar before anything is pinned. */
export const SIDEBAR_AUTOFILL_COUNT = 5
/** How many recently opened (unpinned) projects the sidebar lists. */
export const SIDEBAR_RECENT_COUNT = 5
/** How many recent project ids are remembered. */
export const RECENT_PROJECTS_MAX = 10

export type SidebarProjectSections = {
  /** Projects the user pinned, in pin order. */
  pinned: Project[]
  /** Top projects shown only while nothing is pinned. */
  suggested: Project[]
  /** Recently opened projects that are not already listed above. */
  recent: Project[]
}

/** Pin order from the server's `pinned` / `pin_position` fields. */
export function pinIdsFromProjects(projects: Project[]): number[] {
  return projects
    .filter((p) => p.pinned)
    .sort((a, b) => (a.pin_position ?? 0) - (b.pin_position ?? 0) || a.id - b.id)
    .map((p) => p.id)
}

function pickById(projects: Project[], ids: number[]): Project[] {
  const byId = new Map(projects.map((p) => [p.id, p]))
  const out: Project[] = []
  for (const id of ids) {
    const p = byId.get(id)
    if (p) out.push(p)
  }
  return out
}

/**
 * Splits the project list into what the sidebar shows. Archived projects never appear here;
 * they stay reachable from the project switcher. `projects` is assumed to be in server order
 * (owned by position, then shared), which is what auto-fill uses.
 */
export function sidebarProjectSections(
  projects: Project[],
  pinIds: number[],
  recentIds: number[],
  opts: { autoFill?: number; recentLimit?: number } = {},
): SidebarProjectSections {
  const autoFill = opts.autoFill ?? SIDEBAR_AUTOFILL_COUNT
  const recentLimit = opts.recentLimit ?? SIDEBAR_RECENT_COUNT
  const active = projects.filter((p) => !p.archived)

  const pinned = pickById(active, pinIds)
  const suggested = pinned.length === 0 ? active.slice(0, autoFill) : []
  const shown = new Set([...pinned, ...suggested].map((p) => p.id))
  const recent = pickById(active, recentIds)
    .filter((p) => !shown.has(p.id))
    .slice(0, recentLimit)

  return { pinned, suggested, recent }
}

/** Moves `id` to the front of the recent list, keeping at most `max` ids. */
export function pushRecentId(recentIds: number[], id: number, max = RECENT_PROJECTS_MAX): number[] {
  return [id, ...recentIds.filter((r) => r !== id)].slice(0, max)
}

/** Adds or removes `id` from the pin list (new pins go to the end). */
export function togglePinId(pinIds: number[], id: number): number[] {
  return pinIds.includes(id) ? pinIds.filter((p) => p !== id) : [...pinIds, id]
}

function searchText(p: Project): string[] {
  return [p.name, p.organization_name || '', p.owner_user_name || '', p.description || '']
}

/**
 * Filters projects by a free-text query (name, organization, owner, description) and ranks
 * name-prefix matches first, then word-prefix matches, then any substring match. An empty
 * query returns the list unchanged.
 */
export function searchProjects(projects: Project[], query: string): Project[] {
  const q = query.trim().toLowerCase()
  if (!q) return projects
  const ranked: { p: Project; rank: number; idx: number }[] = []
  projects.forEach((p, idx) => {
    const name = p.name.toLowerCase()
    let rank = -1
    if (name.startsWith(q)) rank = 0
    else if (name.split(/[\s\-_/.]+/).some((w) => w.startsWith(q))) rank = 1
    else if (name.includes(q)) rank = 2
    else if (searchText(p).slice(1).some((t) => t.toLowerCase().includes(q))) rank = 3
    if (rank >= 0) ranked.push({ p, rank, idx })
  })
  ranked.sort((a, b) => a.rank - b.rank || Number(!!a.p.archived) - Number(!!b.p.archived) || a.idx - b.idx)
  return ranked.map((r) => r.p)
}

/**
 * Card order for the Home projects grid: pinned, then recently opened, then the rest in
 * server order (owned before shared). Archived projects are left out unless a filter query
 * matches them, in which case results are ranked by searchProjects.
 */
export function homeProjectOrder(
  projects: Project[],
  pinIds: number[],
  recentIds: number[],
  query = '',
): Project[] {
  if (query.trim()) return searchProjects(projects, query)
  const active = projects.filter((p) => !p.archived)
  const pinned = pickById(active, pinIds)
  const seen = new Set(pinned.map((p) => p.id))
  const recent = pickById(active, recentIds).filter((p) => !seen.has(p.id))
  recent.forEach((p) => seen.add(p.id))
  return [...pinned, ...recent, ...active.filter((p) => !seen.has(p.id))]
}

/** One or two letters for a project tile, e.g. "Website redesign" → "WR", "api" → "AP". */
export function projectInitials(name: string): string {
  const words = name.trim().split(/[\s\-_/.]+/).filter(Boolean)
  if (words.length === 0) return '?'
  const first = [...words[0]!]
  if (words.length === 1) return first.slice(0, 2).join('').toUpperCase()
  return (first[0]! + [...words[1]!][0]!).toUpperCase()
}

/** Stable hue (0–359) for a project's tile color, spread so neighbouring ids differ. */
export function projectTileHue(id: number): number {
  return Math.abs(Math.imul(id, 2654435761) >> 0) % 360
}
