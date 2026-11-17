<script setup lang="ts">
import { computed, ref } from 'vue'
import { api } from '@/api/client'
import type { CommentReactionCode, TaskComment } from '@/api/types'
import { APIError } from '@/api/types'
import { useToast } from '@/composables/useToast'

// Under each discussion post: reactions, and "Mark resolved" for questions.
const props = defineProps<{ taskId: number; comment: TaskComment; canResolve: boolean }>()
const emit = defineEmits<{ updated: [comment: TaskComment] }>()
const { push } = useToast()

const EMOJI: Record<CommentReactionCode, { glyph: string; label: string }> = {
  '+1': { glyph: '👍', label: 'Thumbs up' },
  '-1': { glyph: '👎', label: 'Thumbs down' },
  tada: { glyph: '🎉', label: 'Hooray' },
  heart: { glyph: '❤️', label: 'Heart' },
  smile: { glyph: '😄', label: 'Smile' },
  eyes: { glyph: '👀', label: 'Looking' },
  rocket: { glyph: '🚀', label: 'Rocket' },
  check: { glyph: '✅', label: 'Done' },
}
const CODES = Object.keys(EMOJI) as CommentReactionCode[]

const pickerOpen = ref(false)
const busy = ref(false)
const reactions = computed(() => props.comment.reactions ?? [])

async function toggle(code: CommentReactionCode) {
  if (busy.value) return
  pickerOpen.value = false
  const mine = reactions.value.find((r) => r.code === code)?.mine ?? false
  busy.value = true
  try {
    emit('updated', await api.setCommentReaction(props.taskId, props.comment.id, code, !mine))
  } catch (err) {
    push(err instanceof APIError ? err.message : 'Reaction failed', 'error')
  } finally {
    busy.value = false
  }
}

async function setResolved(on: boolean) {
  busy.value = true
  try {
    emit('updated', await api.setCommentResolved(props.taskId, props.comment.id, on))
  } catch (err) {
    push(err instanceof APIError ? err.message : 'Update failed', 'error')
  } finally {
    busy.value = false
  }
}

function tooltip(names: string[], count: number) {
  const more = count - names.length
  return names.join(', ') + (more > 0 ? ` and ${more} more` : '')
}
</script>

<template>
  <div class="d-flex flex-wrap align-items-center gap-1 mt-1 small">
    <button
      v-for="r in reactions"
      :key="r.code"
      type="button"
      class="btn btn-sm py-0 px-2 rounded-pill"
      :class="r.mine ? 'btn-primary' : 'btn-outline-secondary'"
      :title="tooltip(r.names, r.count)"
      :aria-pressed="r.mine"
      :aria-label="`${EMOJI[r.code]?.label ?? r.code}: ${r.count}. ${r.mine ? 'Remove your reaction' : 'Add yours'}`"
      :disabled="busy"
      @click="toggle(r.code)"
    >
      {{ EMOJI[r.code]?.glyph ?? r.code }} {{ r.count }}
    </button>
    <div class="position-relative">
      <button
        type="button"
        class="btn btn-sm btn-link text-muted py-0 px-1"
        aria-label="Add a reaction"
        :aria-expanded="pickerOpen"
        @click="pickerOpen = !pickerOpen"
      >
        <i class="bi bi-emoji-smile" aria-hidden="true" />
      </button>
      <div v-if="pickerOpen" class="position-absolute start-0 bottom-100 mb-1 card shadow-sm p-1 d-flex flex-row gap-1" style="z-index: 20" role="menu">
        <button
          v-for="code in CODES"
          :key="code"
          type="button"
          class="btn btn-sm btn-light py-0 px-1"
          role="menuitem"
          :title="EMOJI[code].label"
          :aria-label="EMOJI[code].label"
          @click="toggle(code)"
        >
          {{ EMOJI[code].glyph }}
        </button>
      </div>
    </div>
    <template v-if="canResolve">
      <button v-if="!comment.resolved_at" type="button" class="btn btn-sm btn-link text-muted py-0 px-1" :disabled="busy" @click="setResolved(true)">
        <i class="bi bi-check2-circle me-1" aria-hidden="true" />Mark resolved
      </button>
      <button v-else type="button" class="btn btn-sm btn-link text-muted py-0 px-1" :disabled="busy" @click="setResolved(false)">
        Reopen
      </button>
    </template>
  </div>
</template>
