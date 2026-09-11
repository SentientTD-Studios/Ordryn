import type { Tag, Task } from '@/api/types'

/** Keep the first tag for each case-insensitive name. */
export function uniqueTagsByName(tags: Tag[]): Tag[] {
  const seen = new Set<string>()
  const out: Tag[] = []
  for (const tag of tags) {
    const key = tag.name.toLowerCase()
    if (seen.has(key)) continue
    seen.add(key)
    out.push(tag)
  }
  return out
}

export function isProtectedTag(tag: Tag): boolean {
  const name = tag.name.toLowerCase()
  return !!tag.protected || name === 'removed' || name === 'archived'
}

export function isArchivedTask(task?: { tags?: Tag[] } | null): boolean {
  return (task?.tags || []).some((tag) => tag.name.toLowerCase() === 'archived')
}

export function assignableTags(tags: Tag[]): Tag[] {
  return tags.filter((t) => !isProtectedTag(t))
}

export function archiveConfirmMessage(task: Task): string {
  const kids = task.child_count ?? task.children?.length ?? 0
  const extra =
    kids > 0
      ? ` Its ${kids} subtask${kids === 1 ? '' : 's'} will be archived too.`
      : ''
  return `Archive “${task.title}”?${extra} Filter by the archived tag to find archived tasks and restore them.`
}
