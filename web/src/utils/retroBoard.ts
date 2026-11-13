import type { RetroCard, RetroColumn, RetroPhase } from '@/api/types'

export const RETRO_PHASES: { id: RetroPhase; label: string; hint: string }[] = [
  { id: 'collect', label: 'Collect', hint: 'Add cards. Others’ cards stay hidden until grouping.' },
  { id: 'group', label: 'Group', hint: 'Cards are revealed. Drag a card onto another to group them.' },
  { id: 'vote', label: 'Vote', hint: 'Vote for the cards that matter most.' },
  { id: 'act', label: 'Act', hint: 'Agree on action items and turn them into tasks.' },
]

export const RETRO_TEMPLATE_LABELS: Record<string, string> = {
  went_well: 'Went well / To improve / Ideas',
  start_stop_continue: 'Start / Stop / Continue',
  four_ls: '4Ls: Liked / Learned / Lacked / Longed for',
  mad_sad_glad: 'Mad / Sad / Glad',
}

export function phaseIndex(phase: RetroPhase): number {
  return RETRO_PHASES.findIndex((p) => p.id === phase)
}

export function nextPhase(phase: RetroPhase): RetroPhase | null {
  const i = phaseIndex(phase)
  return i >= 0 && i < RETRO_PHASES.length - 1 ? RETRO_PHASES[i + 1].id : null
}

export function prevPhase(phase: RetroPhase): RetroPhase | null {
  const i = phaseIndex(phase)
  return i > 0 ? RETRO_PHASES[i - 1].id : null
}

export type CardGroup = { top: RetroCard; children: RetroCard[] }

export type RetroColumnView = {
  column: RetroColumn
  groups: CardGroup[]
  /** Others' cards hidden while collecting. */
  hiddenCount: number
}

/**
 * Lays cards out by column: visible top-level cards with the cards grouped under them, plus a count of
 * hidden cards. Cards whose group top is missing (deleted) show at the top level.
 */
export function layoutRetroBoard(cards: RetroCard[], columns: RetroColumn[]): RetroColumnView[] {
  const ids = new Set(cards.map((c) => c.id))
  const childrenOf = new Map<number, RetroCard[]>()
  for (const c of cards) {
    if (c.parent_id && ids.has(c.parent_id) && !c.hidden) {
      const list = childrenOf.get(c.parent_id) ?? []
      list.push(c)
      childrenOf.set(c.parent_id, list)
    }
  }
  return columns.map((column) => {
    const inCol = cards.filter((c) => c.column === column.key)
    const groups = inCol
      .filter((c) => !c.hidden && !(c.parent_id && ids.has(c.parent_id)))
      .map((top) => ({ top, children: childrenOf.get(top.id) ?? [] }))
    return { column, groups, hiddenCount: inCol.filter((c) => c.hidden).length }
  })
}

/** Top-level cards by votes (most first), for the Act phase. Ties keep board order. */
export function rankedCards(cards: RetroCard[]): RetroCard[] {
  const ids = new Set(cards.map((c) => c.id))
  return cards
    .filter((c) => !c.hidden && !(c.parent_id && ids.has(c.parent_id)))
    .map((c, i) => ({ c, i }))
    .sort((a, b) => b.c.votes - a.c.votes || a.i - b.i)
    .map(({ c }) => c)
}

export function votesLeft(maxVotes: number, used: number): number | null {
  return maxVotes > 0 ? Math.max(0, maxVotes - used) : null
}
