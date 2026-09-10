<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { api } from '@/api/client'
import type { TaskComment, TaskCommentRevision } from '@/api/types'
import WysiwygEditor from '@/components/WysiwygEditor.vue'
import RichBody from '@/components/RichBody.vue'
import { useAuth } from '@/composables/useAuth'
import { useConfirm } from '@/composables/useConfirm'
import { useTaskSidebar } from '@/composables/useTaskSidebar'
import { useToast } from '@/composables/useToast'
import { USER_SEARCH_MIN_QUERY, useUserSearch } from '@/composables/useUserSearch'
import { useImageUpload } from '@/composables/useImageUpload'
import {
  extractTaskRefIDs,
  extractTaskRefQueries,
  insertMarkdownAtCursor,
  insertMention,
  insertTaskRef,
  isInsertedTaskRef,
  mentionTokenAtCursor,
  type MentionToken,
} from '@/utils/taskCommentBody'
import { dropHasFiles, imageFileFromClipboard, imageFileFromDrop } from '@/utils/imageUpload'

const props = defineProps<{
  taskId: number
  projectId?: number | null
  currentUserId: number | null
  isOwner: boolean
  /** Grow with the parent column instead of capping the thread at 360px. */
  fillHeight?: boolean
}>()

const toast = useToast()
const { askConfirm } = useConfirm()
const { openEdit } = useTaskSidebar()
const { hasPermission } = useAuth()
const { enabled: imageEnabled, uploadImageFile } = useImageUpload()

const comments = ref<TaskComment[]>([])
const loading = ref(false)
const posting = ref(false)
const draft = ref('')
const draftEditor = ref<InstanceType<typeof WysiwygEditor> | null>(null)
const draftEl = computed(() => draftEditor.value?.textarea ?? null)
const editEditor = ref<InstanceType<typeof WysiwygEditor> | null>(null)
const editEl = computed(() => editEditor.value?.textarea ?? null)
const mentionListEl = ref<HTMLElement | null>(null)
const bottomEl = ref<HTMLElement | null>(null)
const MAX_BODY = 2000
const editingId = ref<number | null>(null)
const editDraft = ref('')
const savingEdit = ref(false)
const historyId = ref<number | null>(null)
const historyByComment = ref<Record<number, TaskCommentRevision[]>>({})
const historyLoading = ref(false)
const restoringId = ref<number | null>(null)
const isAdmin = computed(() => hasPermission('admin'))

type DraftLink = {
  id: number
  title: string
  inserted: boolean
  query?: string
}

const draftLinks = ref<DraftLink[]>([])
const titleCache = new Map<number, string | null>()
const queryCache = new Map<string, DraftLink[]>()
let lookupTimer: ReturnType<typeof setTimeout> | null = null

const mentionOpen = ref(false)
const mentionToken = ref<MentionToken | null>(null)
const mentionMenuStyle = ref<Record<string, string>>({})

const { filtered: mentionHits, loading: mentionLoading, highlight: mentionHighlight, scheduleSearch, cancelPending, applyLocal, cachedNames } =
  useUserSearch({
    projectId: () => props.projectId,
  })

const showMentionMenu = computed(
  () =>
    mentionOpen.value &&
    !!props.projectId &&
    (mentionToken.value?.query.length ?? 0) >= USER_SEARCH_MIN_QUERY,
)

async function reload(scroll = false) {
  if (!props.taskId) return
  const keepEditing = editingId.value
  const keepDraft = editDraft.value
  const keepHistory = historyId.value
  loading.value = true
  try {
    comments.value = await api.listTaskComments(props.taskId)
    await nextTick()
    if (scroll) {
      bottomEl.value?.scrollIntoView({ block: 'nearest' })
    }
    if (keepEditing && comments.value.some((c) => c.id === keepEditing && !c.deleted)) {
      editingId.value = keepEditing
      editDraft.value = keepDraft
    }
    if (keepHistory && comments.value.some((c) => c.id === keepHistory)) {
      historyId.value = keepHistory
    }
  } catch (err) {
    toast.push(err instanceof Error ? err.message : 'Could not load discussion', 'error')
  } finally {
    loading.value = false
  }
}

function authorLabel(c: TaskComment) {
  if (props.currentUserId && c.user_id === props.currentUserId) return 'You'
  return c.user_name || `User #${c.user_id}`
}

function postedByName(c: TaskComment) {
  return c.user_name || `User #${c.user_id}`
}

function editorName(c: TaskComment) {
  if (props.currentUserId && c.edited_by_user_id === props.currentUserId) return 'You'
  return c.edited_by_user_name || (c.edited_by_user_id ? `User #${c.edited_by_user_id}` : '')
}

function initials(c: TaskComment) {
  const name = authorLabel(c).trim()
  if (name === 'You') return 'YO'
  const parts = name.split(/\s+/).filter(Boolean)
  if (parts.length >= 2) return (parts[0][0] + parts[1][0]).toUpperCase()
  return name.slice(0, 2).toUpperCase() || '?'
}

function formatWhen(iso: string) {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return d.toLocaleString(undefined, {
    month: 'short',
    day: 'numeric',
    year: 'numeric',
    hour: 'numeric',
    minute: '2-digit',
  })
}

function editedHover(c: TaskComment) {
  const posted = postedByName(c)
  const editor = editorName(c)
  if (editor && c.edited_by_user_id && c.edited_by_user_id !== c.user_id) {
    return `Posted by ${posted} | Edited by ${editor}.`
  }
  return `Edited by ${posted}`
}

function tombstone(c: TaskComment) {
  if (c.deleted_by_kind === 'owner') return 'Message deleted by project owner'
  return 'Message deleted by user'
}

function canDelete(c: TaskComment) {
  if (c.deleted) return false
  if (props.currentUserId && c.user_id === props.currentUserId) return true
  return props.isOwner
}

function canEdit(c: TaskComment) {
  if (c.deleted) return false
  if (props.currentUserId && c.user_id === props.currentUserId) return true
  return props.isOwner
}

function canViewHistory(c: TaskComment) {
  if (!props.isOwner && !isAdmin.value) return false
  return !!c.edited_at || c.deleted
}

function revisionKindLabel(kind: string) {
  if (kind === 'delete') return 'Before delete'
  if (kind === 'restore') return 'Before restore'
  return 'Before edit'
}

function linkTitle(c: TaskComment, id: number) {
  return c.links?.find((l) => l.id === id)?.title
}

function linkTitleById(id: number) {
  return titleCache.get(id) || undefined
}

function taskLinkLabel(id: number, title?: string) {
  return title ? `Task #${id} - ${title}` : `Task #${id}`
}

function openLinkedTask(id: number) {
  if (id === props.taskId) return
  openEdit(id)
}

async function lookupDraftLinks() {
  const ids = extractTaskRefIDs(draft.value)
  const queries = extractTaskRefQueries(draft.value)
  const next: DraftLink[] = []
  const seen = new Set<number>()

  for (const id of ids) {
    if (id === props.taskId) continue
    if (seen.has(id)) continue
    seen.add(id)

    if (!titleCache.has(id)) {
      try {
        const task = await api.getTask(id)
        if (props.projectId && task.project_id !== props.projectId) {
          titleCache.set(id, null)
        } else {
          titleCache.set(id, task.title || `Task #${id}`)
        }
      } catch {
        titleCache.set(id, null)
      }
    }

    const title = titleCache.get(id)
    if (!title) continue

    next.push({
      id,
      title,
      inserted: isInsertedTaskRef(draft.value, id),
    })
  }

  for (const query of queries) {
    const normalized = query.trim()
    if (!normalized || normalized.length < 2) continue
    if (!/^[A-Za-z0-9][A-Za-z0-9_-]*$/.test(normalized)) continue

    const key = normalized.toLowerCase()
    if (queryCache.has(key)) {
      for (const link of queryCache.get(key)!) {
        if (link.id === props.taskId) continue
        if (seen.has(link.id)) continue
        seen.add(link.id)
        next.push({
          ...link,
          inserted: isInsertedTaskRef(draft.value, link.id),
        })
      }
      continue
    }

    try {
      const list = await api.listTasks({
        search: normalized,
        page: 1,
        per_page: 5,
        ...(props.projectId ? { project: props.projectId } : {}),
      })

      const filtered = list.tasks.filter((task) => {
        if (task.id === props.taskId) return false
        if (props.projectId && task.project_id !== props.projectId) return false
        return true
      })

      const matches = filtered.map((task) => ({
        id: task.id,
        title: task.title || `Task #${task.id}`,
        inserted: isInsertedTaskRef(draft.value, task.id),
        query: normalized,
      }))

      queryCache.set(key, matches)
      for (const link of matches) {
        if (seen.has(link.id)) continue
        seen.add(link.id)
        next.push(link)
      }
    } catch {
      queryCache.set(key, [])
    }
  }

  draftLinks.value = next
}

function scheduleDraftLookup() {
  if (lookupTimer) clearTimeout(lookupTimer)
  lookupTimer = setTimeout(() => {
    void lookupDraftLinks()
  }, 250)
}

function insertLink(id: number, query?: string) {
  draft.value = insertTaskRef(draft.value, id, query)
  void lookupDraftLinks()
}

function updateMentionMenuPosition() {
  const el = draftEl.value
  if (!el) return
  const r = el.getBoundingClientRect()
  const gap = 2
  const maxHeight = 220
  const spaceBelow = window.innerHeight - r.bottom - gap
  const openAbove = spaceBelow < 120 && r.top > spaceBelow
  if (openAbove) {
    mentionMenuStyle.value = {
      position: 'fixed',
      top: 'auto',
      bottom: `${window.innerHeight - r.top + gap}px`,
      left: `${r.left}px`,
      width: `${Math.max(r.width, 160)}px`,
      maxHeight: `${Math.min(maxHeight, r.top - gap)}px`,
      zIndex: '2000',
    }
  } else {
    mentionMenuStyle.value = {
      position: 'fixed',
      top: `${r.bottom + gap}px`,
      bottom: 'auto',
      left: `${r.left}px`,
      width: `${Math.max(r.width, 160)}px`,
      maxHeight: `${Math.min(maxHeight, Math.max(spaceBelow, 80))}px`,
      zIndex: '2000',
    }
  }
}

function closeMentionMenu() {
  mentionOpen.value = false
  mentionToken.value = null
  cancelPending()
}

function syncMentionFromEl() {
  const el = draftEl.value
  if (!el || !props.projectId) {
    closeMentionMenu()
    return
  }
  const token = mentionTokenAtCursor(el.value, el.selectionStart ?? 0)
  mentionToken.value = token
  if (!token || token.query.length < USER_SEARCH_MIN_QUERY) {
    cancelPending()
    mentionOpen.value = false
    return
  }
  mentionOpen.value = true
  const local = cachedNames(token.query)
  if (local) {
    cancelPending()
    applyLocal(local)
  } else {
    scheduleSearch(token.query)
  }
  void nextTick(() => updateMentionMenuPosition())
}

function selectMention(name: string) {
  const token = mentionToken.value
  if (!token) return
  const inserted = insertMention(draft.value, token, name)
  draft.value = inserted.body
  closeMentionMenu()
  void nextTick(() => {
    const el = draftEl.value
    if (el) {
      el.focus()
      el.setSelectionRange(inserted.cursor, inserted.cursor)
    }
    scheduleDraftLookup()
  })
}

async function scrollMentionHighlight() {
  await nextTick()
  const active = mentionListEl.value?.querySelector<HTMLElement>('[data-active="true"]')
  active?.scrollIntoView({ block: 'nearest' })
}

function onDraftInput(e: Event) {
  const el = e.target as HTMLTextAreaElement
  draft.value = el.value
  syncMentionFromEl()
}

function insertCommentImage(markdown: string, target: 'draft' | 'edit' = 'draft') {
  const el = target === 'edit' ? editEl.value : draftEl.value
  const current = target === 'edit' ? editDraft.value : draft.value
  const start = el?.selectionStart ?? current.length
  const end = el?.selectionEnd ?? start
  const next = insertMarkdownAtCursor(current, markdown, start, end)
  if (next.body.length > MAX_BODY) {
    toast.push('Comment would exceed 2000 characters', 'error')
    return
  }
  if (target === 'edit') editDraft.value = next.body
  else draft.value = next.body
  void nextTick(() => {
    if (!el) return
    el.focus()
    el.setSelectionRange(next.cursor, next.cursor)
    if (target === 'draft') syncMentionFromEl()
  })
}

async function ingestImageFile(file: File, target: 'draft' | 'edit' = 'draft') {
  const markdown = await uploadImageFile(file)
  if (markdown) insertCommentImage(markdown, target)
}

function onDraftPaste(e: ClipboardEvent) {
  const file = imageFileFromClipboard(e)
  if (!file || !imageEnabled.value) return
  e.preventDefault()
  void ingestImageFile(file, 'draft')
}

function onEditPaste(e: ClipboardEvent) {
  const file = imageFileFromClipboard(e)
  if (!file || !imageEnabled.value) return
  e.preventDefault()
  void ingestImageFile(file, 'edit')
}

function onDraftDragOver(e: DragEvent) {
  if (!imageEnabled.value || !dropHasFiles(e)) return
  e.preventDefault()
}

function onDraftDrop(e: DragEvent) {
  const file = imageFileFromDrop(e)
  if (!file || !imageEnabled.value) return
  e.preventDefault()
  void ingestImageFile(file, 'draft')
}

function onEditDragOver(e: DragEvent) {
  if (!imageEnabled.value || !dropHasFiles(e)) return
  e.preventDefault()
}

function onEditDrop(e: DragEvent) {
  const file = imageFileFromDrop(e)
  if (!file || !imageEnabled.value) return
  e.preventDefault()
  void ingestImageFile(file, 'edit')
}

function onDraftKeydown(e: KeyboardEvent) {
  if (!showMentionMenu.value) return
  if (e.key === 'Escape') {
    closeMentionMenu()
    e.preventDefault()
    return
  }
  if (e.key === 'ArrowDown') {
    if (!mentionHits.value.length) return
    mentionHighlight.value = Math.min(mentionHighlight.value + 1, mentionHits.value.length - 1)
    void scrollMentionHighlight()
    e.preventDefault()
    return
  }
  if (e.key === 'ArrowUp') {
    mentionHighlight.value = Math.max(mentionHighlight.value - 1, 0)
    void scrollMentionHighlight()
    e.preventDefault()
    return
  }
  if (e.key === 'Enter' || e.key === 'Tab') {
    const name = mentionHits.value[mentionHighlight.value]
    if (name) {
      selectMention(name)
      e.preventDefault()
    }
  }
}

function onMentionReposition() {
  if (showMentionMenu.value) updateMentionMenuPosition()
}

const charCount = computed(() => draft.value.length)

async function post() {
  const body = draft.value.trim()
  if (!body || posting.value) return
  posting.value = true
  try {
    const created = await api.addTaskComment(props.taskId, body)
    comments.value = [...comments.value, created]
    draft.value = ''
    draftLinks.value = []
    closeMentionMenu()
    await nextTick()
    bottomEl.value?.scrollIntoView({ block: 'nearest' })
  } catch (err) {
    toast.push(err instanceof Error ? err.message : 'Could not post comment', 'error')
  } finally {
    posting.value = false
  }
}

async function remove(c: TaskComment) {
  const ok = await askConfirm({
    title: 'Delete comment',
    message: 'Delete this comment? Others will see that it was removed.',
    confirmLabel: 'Delete',
    danger: true,
  })
  if (!ok) return
  try {
    await api.deleteTaskComment(props.taskId, c.id)
    if (editingId.value === c.id) {
      editingId.value = null
      editDraft.value = ''
    }
    await reload()
  } catch (err) {
    toast.push(err instanceof Error ? err.message : 'Could not delete comment', 'error')
  }
}

function onEditInput(e: Event) {
  editDraft.value = (e.target as HTMLTextAreaElement).value
}

function startEdit(c: TaskComment) {
  editingId.value = c.id
  editDraft.value = c.body
}

function cancelEdit() {
  editingId.value = null
  editDraft.value = ''
}

async function saveEdit(c: TaskComment) {
  const body = editDraft.value.trim()
  if (!body || savingEdit.value) return
  savingEdit.value = true
  try {
    const updated = await api.editTaskComment(props.taskId, c.id, body)
    comments.value = comments.value.map((row) => (row.id === updated.id ? updated : row))
    editingId.value = null
    editDraft.value = ''
    if (historyId.value === c.id) {
      await loadHistory(c.id)
    }
  } catch (err) {
    toast.push(err instanceof Error ? err.message : 'Could not save comment', 'error')
  } finally {
    savingEdit.value = false
  }
}

async function loadHistory(commentId: number) {
  historyLoading.value = true
  try {
    historyByComment.value = {
      ...historyByComment.value,
      [commentId]: await api.listTaskCommentRevisions(props.taskId, commentId),
    }
  } catch (err) {
    toast.push(err instanceof Error ? err.message : 'Could not load comment history', 'error')
  } finally {
    historyLoading.value = false
  }
}

async function toggleHistory(c: TaskComment) {
  if (historyId.value === c.id) {
    historyId.value = null
    return
  }
  historyId.value = c.id
  await loadHistory(c.id)
}

async function restoreRevision(c: TaskComment, rev: TaskCommentRevision) {
  const ok = await askConfirm({
    title: 'Restore comment',
    message: 'Replace the current comment with this previous version?',
    confirmLabel: 'Restore',
  })
  if (!ok) return
  restoringId.value = rev.id
  try {
    const updated = await api.restoreTaskComment(props.taskId, c.id, rev.id)
    comments.value = comments.value.map((row) => (row.id === updated.id ? updated : row))
    editingId.value = null
    editDraft.value = ''
    await loadHistory(c.id)
  } catch (err) {
    toast.push(err instanceof Error ? err.message : 'Could not restore comment', 'error')
  } finally {
    restoringId.value = null
  }
}

watch(
  () => props.taskId,
  () => {
    editingId.value = null
    editDraft.value = ''
    historyId.value = null
    historyByComment.value = {}
    void reload()
  },
)

watch(draft, () => {
  scheduleDraftLookup()
})

watch(showMentionMenu, (visible) => {
  if (visible) void nextTick(() => updateMentionMenuPosition())
})

onMounted(() => {
  void reload()
  window.addEventListener('scroll', onMentionReposition, true)
  window.addEventListener('resize', onMentionReposition)
})

onUnmounted(() => {
  if (lookupTimer) clearTimeout(lookupTimer)
  cancelPending()
  window.removeEventListener('scroll', onMentionReposition, true)
  window.removeEventListener('resize', onMentionReposition)
})

defineExpose({ reload })
</script>

<template>
  <div class="task-discussion" :class="fillHeight ? 'task-discussion--fill' : 'mt-3'">
    <h3 class="h6 mb-2">Discussion</h3>
    <p v-if="loading && !comments.length" class="text-muted small mb-2">Loading discussion…</p>
    <div v-else class="task-discussion-thread mb-2">
      <ul v-if="comments.length" class="list-unstyled mb-0">
        <li v-for="c in comments" :key="c.id" class="task-discussion-post">
          <article class="task-post-card" :class="{ 'task-post-card--deleted': c.deleted }">
            <header class="task-post-header">
              <img
                v-if="c.avatar_url"
                :src="c.avatar_url"
                alt=""
                class="task-post-avatar-img"
                aria-hidden="true"
              />
              <div v-else class="task-post-avatar" aria-hidden="true">{{ initials(c) }}</div>
              <div class="task-post-meta">
                <div class="task-post-author text-truncate">{{ authorLabel(c) }}</div>
                <div class="task-post-times">
                  <time class="task-post-time" :datetime="c.created_at">Posted {{ formatWhen(c.created_at) }}</time>
                  <time
                    v-if="c.edited_at && !c.deleted"
                    class="task-post-time"
                    :datetime="c.edited_at"
                    :title="editedHover(c)"
                  >Edited {{ formatWhen(c.edited_at) }}</time>
                </div>
              </div>
              <div class="task-post-actions ms-auto">
                <button
                  v-if="canEdit(c) && editingId !== c.id"
                  class="btn btn-sm btn-link p-0"
                  type="button"
                  @click="startEdit(c)"
                >
                  Edit
                </button>
                <button
                  v-if="canViewHistory(c)"
                  class="btn btn-sm btn-link p-0"
                  type="button"
                  @click="toggleHistory(c)"
                >
                  {{ historyId === c.id ? 'Hide history' : 'History' }}
                </button>
                <button
                  v-if="canDelete(c) && editingId !== c.id"
                  class="btn btn-sm btn-link text-danger p-0"
                  type="button"
                  @click="remove(c)"
                >
                  Delete
                </button>
              </div>
            </header>
            <div v-if="c.deleted" class="task-post-body fst-italic text-muted">
              {{ tombstone(c) }}
            </div>
            <div v-else-if="editingId === c.id" class="task-post-body">
              <WysiwygEditor
                ref="editEditor"
                v-model="editDraft"
                :rows="3"
                :maxlength="MAX_BODY"
                :task-title="(id) => linkTitle(c, id) || linkTitleById(id)"
                placeholder="Edit comment… Format with markdown, click 'Upload image' or paste/drop."
                @input="onEditInput"
                @paste="onEditPaste"
                @dragover="onEditDragOver"
                @drop="onEditDrop"
                @open-task="openLinkedTask"
              />
              <div class="d-flex justify-content-between align-items-center mt-2">
                <small class="text-muted">{{ editDraft.length }}/{{ MAX_BODY }}</small>
                <div class="d-flex gap-2">
                  <button type="button" class="btn btn-sm btn-outline-secondary" :disabled="savingEdit" @click="cancelEdit">
                    Cancel
                  </button>
                  <button
                    type="button"
                    class="btn btn-sm btn-primary"
                    :disabled="savingEdit || !editDraft.trim()"
                    @click="saveEdit(c)"
                  >
                    {{ savingEdit ? 'Saving…' : 'Save' }}
                  </button>
                </div>
              </div>
            </div>
            <div v-else class="task-post-body">
              <RichBody :body="c.body" :task-title="(id) => linkTitle(c, id)" @open-task="openLinkedTask" />
            </div>
            <div v-if="historyId === c.id" class="task-post-history">
              <p v-if="historyLoading && !historyByComment[c.id]" class="small text-muted mb-0">Loading history…</p>
              <p
                v-else-if="!(historyByComment[c.id] && historyByComment[c.id].length)"
                class="small text-muted mb-0"
              >
                No previous versions.
              </p>
              <ul v-else class="list-unstyled mb-0">
                <li v-for="rev in historyByComment[c.id]" :key="rev.id" class="task-post-revision">
                  <div class="d-flex justify-content-between align-items-start gap-2">
                    <div>
                      <div class="small fw-semibold">{{ revisionKindLabel(rev.kind) }}</div>
                      <div class="small text-muted">
                        {{ formatWhen(rev.created_at) }}
                        <span v-if="rev.edited_by_user_name"> · {{ rev.edited_by_user_name }}</span>
                      </div>
                    </div>
                    <button
                      v-if="rev.body.trim()"
                      type="button"
                      class="btn btn-sm btn-outline-secondary"
                      :disabled="restoringId === rev.id"
                      @click="restoreRevision(c, rev)"
                    >
                      {{ restoringId === rev.id ? 'Restoring…' : 'Restore' }}
                    </button>
                  </div>
                  <div class="task-post-revision-body">
                    <RichBody v-if="rev.body.trim()" :body="rev.body" />
                    <span v-else>(empty)</span>
                  </div>
                </li>
              </ul>
            </div>
          </article>
        </li>
      </ul>
      <p v-else class="small text-muted mb-0">No comments yet. Start the discussion.</p>
      <div ref="bottomEl" />
    </div>
    <label class="form-label small mb-1" for="task-comment-body">Add a comment</label>
    <WysiwygEditor
      id="task-comment-body"
      ref="draftEditor"
      v-model="draft"
      :rows="3"
      :maxlength="MAX_BODY"
      :task-title="linkTitleById"
      placeholder="Write a comment… Format with markdown, click 'Upload image' or paste/drop, type @ to mention a member, or paste #123 to link a task."
      @input="onDraftInput"
      @keydown="onDraftKeydown"
      @paste="onDraftPaste"
      @dragover="onDraftDragOver"
      @drop="onDraftDrop"
      @click="syncMentionFromEl"
      @keyup="syncMentionFromEl"
      @open-task="openLinkedTask"
    />
    <Teleport to="body">
      <ul
        v-if="showMentionMenu"
        id="task-comment-mention-listbox"
        ref="mentionListEl"
        class="user-search-menu list-unstyled mb-0"
        role="listbox"
        :style="mentionMenuStyle"
      >
        <li
          v-for="(name, idx) in mentionHits"
          :key="name"
          role="option"
          class="user-search-option"
          :class="{ active: mentionHighlight === idx }"
          :data-active="mentionHighlight === idx ? 'true' : undefined"
          :aria-selected="mentionToken?.query.toLowerCase() === name.toLowerCase()"
          @mousedown.prevent
          @click="selectMention(name)"
        >
          {{ name }}
        </li>
        <li
          v-if="mentionLoading && !mentionHits.length"
          class="user-search-option text-muted"
          aria-disabled="true"
        >
          Searching…
        </li>
        <li
          v-else-if="!mentionHits.length"
          class="user-search-option text-muted"
          aria-disabled="true"
        >
          No matching project members
        </li>
      </ul>
    </Teleport>
    <div v-if="draftLinks.length" class="task-discussion-suggestions mt-2">
      <div v-for="link in draftLinks" :key="link.id" class="task-discussion-suggestion">
        <span class="small">{{ taskLinkLabel(link.id, link.title) }}</span>
        <button
          v-if="!link.inserted"
          type="button"
          class="btn btn-sm btn-outline-primary"
          @click="insertLink(link.id, link.query)"
        >
          Insert link
        </button>
        <span v-else class="badge text-bg-secondary">Linked</span>
      </div>
    </div>
    <div class="d-flex justify-content-between align-items-center mt-1">
      <small class="text-muted">{{ charCount }}/{{ MAX_BODY }}</small>
      <button
        type="button"
        class="btn btn-sm btn-primary"
        :disabled="posting || !draft.trim()"
        @click="post"
      >
        {{ posting ? 'Posting…' : 'Post' }}
      </button>
    </div>
  </div>
</template>

<style scoped>
.task-discussion--fill {
  margin-top: 1.25rem;
}
.task-discussion-thread {
  max-height: 360px;
  overflow-y: auto;
  padding: 0.5rem;
  border-radius: 0.5rem;
  background: var(--ordryn-muted-bg, #f8f6ee);
  border: 1px solid var(--ordryn-card-border, #dee2e6);
}
.task-discussion--fill .task-discussion-thread {
  max-height: none;
  overflow: visible;
  min-height: 8rem;
}
.task-discussion-post + .task-discussion-post {
  margin-top: 0.75rem;
}
.task-post-card {
  background: var(--ordryn-card-bg, #fff);
  color: var(--ordryn-text);
  border: 1px solid var(--ordryn-card-border, #dee2e6);
  border-radius: 0.5rem;
  box-shadow: var(--ordryn-card-shadow, 0 2px 8px rgba(0, 0, 0, 0.06));
  overflow: hidden;
}
.task-post-card--deleted {
  opacity: 0.85;
}
.task-post-header {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.5rem 0.65rem;
  padding: 0.65rem 0.75rem;
  background: color-mix(in srgb, var(--ordryn-muted-bg, #f1f5f9) 80%, var(--ordryn-card-bg, #fff));
  border-bottom: 1px solid var(--ordryn-card-border, #dee2e6);
}
.task-post-avatar {
  width: 2rem;
  height: 2rem;
  border-radius: 999px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  font-size: 0.7rem;
  font-weight: 700;
  letter-spacing: 0.02em;
  color: var(--ordryn-filter-active-text, #fff);
  background: var(--ordryn-accent, #2563eb);
  flex-shrink: 0;
}
.task-post-avatar-img {
  width: 2rem;
  height: 2rem;
  border-radius: 999px;
  object-fit: cover;
  flex-shrink: 0;
}
.task-post-meta {
  min-width: 0;
  flex: 1;
}
.task-post-author {
  font-weight: 600;
  font-size: 0.875rem;
  line-height: 1.2;
}
.task-post-times {
  display: flex;
  flex-wrap: wrap;
  gap: 0.15rem 0.65rem;
}
.task-post-time {
  display: block;
  font-size: 0.75rem;
  color: var(--ordryn-muted, #64748b);
}
.task-post-actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: flex-end;
  gap: 0.55rem;
  flex-shrink: 0;
}
.task-post-body {
  padding: 0.75rem;
  font-size: 0.9rem;
  line-height: 1.45;
  word-break: break-word;
  min-width: 0;
  overflow: hidden;
}
.task-post-history {
  padding: 0 0.75rem 0.75rem;
  border-top: 1px dashed var(--ordryn-card-border, #dee2e6);
  padding-top: 0.65rem;
}
.task-post-revision + .task-post-revision {
  margin-top: 0.55rem;
}
.task-post-revision-body {
  margin-top: 0.3rem;
  padding: 0.45rem 0.55rem;
  border-radius: 0.375rem;
  background: var(--ordryn-muted-bg, #f8f6ee);
  font-size: 0.85rem;
  word-break: break-word;
  min-width: 0;
  overflow: hidden;
}
.task-post-task-link {
  display: inline;
  padding: 0;
  margin: 0;
  border: 0;
  background: none;
  color: var(--ordryn-accent, #2563eb);
  font-weight: 600;
  text-decoration: underline;
  text-underline-offset: 2px;
  cursor: pointer;
}
.task-post-task-link:hover {
  filter: brightness(1.1);
}
.task-post-mention {
  font-weight: 600;
  color: var(--ordryn-accent, #2563eb);
}
.task-discussion-suggestions {
  display: flex;
  flex-direction: column;
  gap: 0.4rem;
}
.task-discussion-suggestion {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.5rem;
  padding: 0.4rem 0.55rem;
  border: 1px solid var(--ordryn-card-border, #dee2e6);
  border-radius: 0.375rem;
  background: var(--ordryn-muted-bg, #f8f6ee);
}
.user-search-menu {
  overflow-y: auto;
  border: 1px solid var(--ordryn-card-border, #dee2e6);
  border-radius: 0.375rem;
  background: var(--ordryn-card-bg, #fff);
  box-shadow: 0 8px 24px rgba(0, 0, 0, 0.12);
}
.user-search-option {
  padding: 0.45rem 0.75rem;
  cursor: pointer;
  color: var(--ordryn-text, inherit);
  font-weight: 600;
  font-size: 0.92rem;
}
.user-search-option[aria-disabled='true'] {
  cursor: default;
  font-weight: 400;
}
.user-search-option.active,
.user-search-option:hover:not([aria-disabled='true']) {
  background: color-mix(in srgb, var(--ordryn-accent, #0d6efd) 14%, transparent);
}
</style>
