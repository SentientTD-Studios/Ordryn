import { computed } from 'vue'
import { useSite } from '@/composables/useSite'

// Fallback character cap for task descriptions and comments until site info
// loads. Must match storage.DefaultTaskTextLength on the server.
export const DEFAULT_TASK_TEXT_LENGTH = 20000

/** Site-configured character caps for task descriptions and comments. */
export function useTaskTextLimits() {
  const { siteInfo } = useSite()
  const maxDescription = computed(() => siteInfo.value?.max_description_length || DEFAULT_TASK_TEXT_LENGTH)
  const maxComment = computed(() => siteInfo.value?.max_comment_length || DEFAULT_TASK_TEXT_LENGTH)
  return { maxDescription, maxComment }
}
