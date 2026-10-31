// Labels and helpers for public feedback boards. Kept free of Vue/API imports
// so it runs under the node test runner.

export type FeedbackStatusKey = 'under_review' | 'planned' | 'in_progress' | 'done'

const LABELS: Record<FeedbackStatusKey, string> = {
  under_review: 'Under review',
  planned: 'Planned',
  in_progress: 'In progress',
  done: 'Done',
}

const BADGES: Record<FeedbackStatusKey, string> = {
  under_review: 'text-bg-secondary',
  planned: 'text-bg-info',
  in_progress: 'text-bg-primary',
  done: 'text-bg-success',
}

export const FEEDBACK_STATUS_ORDER: FeedbackStatusKey[] = ['under_review', 'planned', 'in_progress', 'done']

export function feedbackStatusLabel(status: string): string {
  return LABELS[status as FeedbackStatusKey] ?? LABELS.under_review
}

export function feedbackStatusBadge(status: string): string {
  return BADGES[status as FeedbackStatusKey] ?? BADGES.under_review
}

/** Keeps posts whose status matches the filter ('' = all). */
export function filterFeedbackByStatus<T extends { status: string }>(posts: T[], status: string): T[] {
  if (!status) return posts
  return posts.filter((p) => p.status === status)
}

/** Counts posts per status for filter chips. */
export function countFeedbackByStatus(posts: { status: string }[]): Record<FeedbackStatusKey, number> {
  const out: Record<FeedbackStatusKey, number> = { under_review: 0, planned: 0, in_progress: 0, done: 0 }
  for (const p of posts) {
    const key = (p.status in out ? p.status : 'under_review') as FeedbackStatusKey
    out[key]++
  }
  return out
}

/** Applies a vote result to a post list without refetching. */
export function applyFeedbackVote<T extends { id: number; vote_count: number; voted: boolean }>(
  posts: T[],
  postId: number,
  result: { vote_count: number; voted: boolean },
): T[] {
  return posts.map((p) => (p.id === postId ? { ...p, vote_count: result.vote_count, voted: result.voted } : p))
}
