<template>
  <div class="feedback-panel">
    <p v-if="loading" class="small text-muted">Loading…</p>

    <!-- No board yet -->
    <template v-else-if="!board?.exists">
      <h4 class="h6">Public feedback board</h4>
      <p class="small text-muted">
        A password-protected page where people outside the project, such as playtesters, can send feedback and vote
        on it without an account. New submissions wait in an inbox here until you approve them, and nothing reaches
        the board until you turn it into a task.
      </p>
      <form class="row g-2" @submit.prevent="createBoard">
        <div class="col-sm-6">
          <label class="form-label small mb-0" for="fb-new-title">Title</label>
          <input
            id="fb-new-title"
            v-model="newTitle"
            class="form-control form-control-sm"
            maxlength="120"
            :placeholder="`${project.name} feedback`"
          />
        </div>
        <div class="col-sm-6">
          <label class="form-label small mb-0" for="fb-new-password">Password</label>
          <input
            id="fb-new-password"
            v-model="newPassword"
            type="text"
            class="form-control form-control-sm"
            minlength="6"
            maxlength="128"
            autocomplete="off"
            required
            placeholder="At least 6 characters"
          />
        </div>
        <div class="col-12">
          <button class="btn btn-sm btn-primary" type="submit" :disabled="saving || newPassword.length < 6">
            Create feedback board
          </button>
        </div>
      </form>
    </template>

    <!-- Board settings + inbox -->
    <template v-else>
      <div class="d-flex flex-wrap align-items-center gap-2 mb-2">
        <h4 class="h6 mb-0">Public feedback board</h4>
        <span v-if="!board.enabled" class="badge text-bg-secondary">Off</span>
        <span v-else-if="!board.submissions_open" class="badge text-bg-warning">Not taking submissions</span>
        <span v-else class="badge text-bg-success">Live</span>
      </div>
      <div class="d-flex flex-wrap align-items-center gap-2 mb-1">
        <code class="small text-truncate fb-url">{{ board.url }}</code>
        <button class="btn btn-sm btn-outline-secondary" type="button" @click="copy(board.url || '')">Copy link</button>
      </div>
      <p class="small text-muted mb-3">Share the link and the password with your testers. Both are needed.</p>

      <form class="row g-2 mb-2" @submit.prevent="saveSettings">
        <div class="col-12">
          <label class="form-label small mb-0" for="fb-title">Title</label>
          <input id="fb-title" v-model="edit.title" class="form-control form-control-sm" maxlength="120" required />
        </div>
        <div class="col-12">
          <label class="form-label small mb-0" for="fb-intro">Intro <span class="text-muted">(shown at the top of the page)</span></label>
          <textarea id="fb-intro" v-model="edit.intro" class="form-control form-control-sm" rows="2" maxlength="2000" />
        </div>
        <div class="col-sm-6">
          <label class="form-label small mb-0" for="fb-password">New password</label>
          <input
            id="fb-password"
            v-model="edit.password"
            type="text"
            class="form-control form-control-sm"
            maxlength="128"
            autocomplete="off"
            placeholder="Leave blank to keep"
            aria-describedby="fb-password-help"
          />
          <div id="fb-password-help" class="form-text">Changing it signs every visitor out.</div>
        </div>
        <div class="col-sm-6 d-flex flex-column justify-content-center gap-1">
          <div class="form-check form-switch">
            <input id="fb-enabled" v-model="edit.enabled" class="form-check-input" type="checkbox" role="switch" />
            <label class="form-check-label small" for="fb-enabled">Board is on</label>
          </div>
          <div class="form-check form-switch">
            <input id="fb-open" v-model="edit.submissions_open" class="form-check-input" type="checkbox" role="switch" />
            <label class="form-check-label small" for="fb-open">Accept new submissions</label>
          </div>
        </div>
        <div class="col-12 d-flex flex-wrap gap-2">
          <button class="btn btn-sm btn-primary" type="submit" :disabled="saving">Save</button>
          <button class="btn btn-sm btn-outline-secondary" type="button" :disabled="saving" @click="rotateLink">
            Replace link
          </button>
          <button class="btn btn-sm btn-outline-danger ms-auto" type="button" :disabled="saving" @click="deleteBoard">
            Delete board
          </button>
        </div>
      </form>

      <hr />

      <ul class="nav nav-tabs nav-sm mb-2" role="tablist">
        <li v-for="s in STATES" :key="s.id" class="nav-item">
          <button
            type="button"
            class="nav-link py-1 px-2 small"
            :class="{ active: state === s.id }"
            role="tab"
            :aria-selected="state === s.id"
            @click="state = s.id"
          >
            {{ s.label }}
            <span v-if="s.id === 'pending' && board.pending_count" class="badge text-bg-danger ms-1">{{ board.pending_count }}</span>
          </button>
        </li>
      </ul>

      <p v-if="postsLoading" class="small text-muted">Loading…</p>
      <p v-else-if="!posts.length" class="small text-muted">{{ emptyText }}</p>
      <ul v-else class="list-unstyled mb-0">
        <li v-for="p in posts" :key="p.id" class="border rounded p-2 mb-2">
          <div class="d-flex flex-wrap align-items-center gap-2">
            <strong class="text-break">{{ p.title }}</strong>
            <span v-if="p.state === 'approved'" class="badge" :class="feedbackStatusBadge(p.status)">
              {{ feedbackStatusLabel(p.status) }}
            </span>
            <span v-if="p.state === 'approved'" class="small text-muted">
              <i class="bi bi-caret-up-fill" aria-hidden="true" />{{ p.vote_count }}
              <span class="visually-hidden">votes</span>
            </span>
          </div>
          <p v-if="p.body" class="small mb-1 fb-text">{{ p.body }}</p>
          <div class="small text-muted mb-2">
            <span v-if="p.author_name">{{ p.author_name }} · </span>{{ formatTime(p.created_at) }}
          </div>
          <div class="d-flex flex-wrap gap-1">
            <template v-if="p.state === 'pending'">
              <button class="btn btn-sm btn-success" type="button" :disabled="busyId === p.id" @click="moderate(p, 'approve')">
                Approve
              </button>
              <button class="btn btn-sm btn-outline-primary" type="button" :disabled="busyId === p.id" @click="promote(p)">
                Approve + create task
              </button>
              <button class="btn btn-sm btn-outline-secondary" type="button" :disabled="busyId === p.id" @click="moderate(p, 'reject')">
                Reject
              </button>
            </template>
            <template v-else-if="p.state === 'approved'">
              <RouterLink v-if="p.task_id" class="btn btn-sm btn-outline-secondary" :to="{ name: 'task', params: { id: p.task_id } }">
                Open task #{{ p.task_id }}
              </RouterLink>
              <template v-else>
                <button class="btn btn-sm btn-outline-primary" type="button" :disabled="busyId === p.id" @click="promote(p)">
                  Create task
                </button>
                <form class="d-flex gap-1" @submit.prevent="linkTask(p)">
                  <label class="visually-hidden" :for="`fb-link-${p.id}`">Existing task id</label>
                  <input
                    :id="`fb-link-${p.id}`"
                    v-model.number="linkIds[p.id]"
                    type="number"
                    min="1"
                    class="form-control form-control-sm fb-link-input"
                    placeholder="Task #"
                  />
                  <button class="btn btn-sm btn-outline-secondary" type="submit" :disabled="busyId === p.id || !linkIds[p.id]">
                    Link
                  </button>
                </form>
              </template>
              <button class="btn btn-sm btn-outline-secondary" type="button" :disabled="busyId === p.id" @click="moderate(p, 'reject')">
                Hide
              </button>
            </template>
            <template v-else>
              <button class="btn btn-sm btn-outline-secondary" type="button" :disabled="busyId === p.id" @click="moderate(p, 'reopen')">
                Move back to inbox
              </button>
            </template>
            <button class="btn btn-sm btn-link text-danger ms-auto" type="button" :disabled="busyId === p.id" @click="removePost(p)">
              Delete
            </button>
          </div>
        </li>
      </ul>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { RouterLink } from 'vue-router'
import { api } from '@/api/client'
import type { FeedbackBoard, FeedbackPost, FeedbackPostState, Project } from '@/api/types'
import { APIError } from '@/api/types'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import { feedbackStatusBadge, feedbackStatusLabel } from '@/utils/feedbackStatus'

const props = defineProps<{ project: Project }>()
const emit = defineEmits<{ changed: [] }>()

const STATES: { id: FeedbackPostState; label: string }[] = [
  { id: 'pending', label: 'Inbox' },
  { id: 'approved', label: 'Public' },
  { id: 'rejected', label: 'Rejected' },
]

const toast = useToast()
const { askConfirm } = useConfirm()

const board = ref<FeedbackBoard | null>(null)
const loading = ref(true)
const saving = ref(false)
const newTitle = ref('')
const newPassword = ref('')
const edit = reactive({ title: '', intro: '', password: '', enabled: true, submissions_open: true })

const state = ref<FeedbackPostState>('pending')
const posts = ref<FeedbackPost[]>([])
const postsLoading = ref(false)
const busyId = ref<number | null>(null)
const linkIds = reactive<Record<number, number | undefined>>({})

const emptyText = computed(() => {
  if (state.value === 'pending') return 'The inbox is empty.'
  if (state.value === 'approved') return 'Nothing is public yet. Approve submissions from the inbox.'
  return 'No rejected submissions.'
})

function errMessage(err: unknown, fallback: string) {
  return err instanceof APIError ? err.message : fallback
}

function applyBoard(b: FeedbackBoard) {
  board.value = b
  edit.title = b.title || ''
  edit.intro = b.intro || ''
  edit.password = ''
  edit.enabled = b.enabled
  edit.submissions_open = b.submissions_open
}

async function loadBoard() {
  loading.value = true
  try {
    applyBoard(await api.getFeedbackBoard(props.project.id))
    if (board.value?.exists) await loadPosts()
  } catch (err) {
    board.value = null
    toast.push(errMessage(err, 'Failed to load feedback board'), 'error')
  } finally {
    loading.value = false
  }
}

async function loadPosts() {
  postsLoading.value = true
  try {
    posts.value = await api.listFeedbackPosts(props.project.id, state.value)
  } catch (err) {
    posts.value = []
    toast.push(errMessage(err, 'Failed to load submissions'), 'error')
  } finally {
    postsLoading.value = false
  }
}

async function refreshCounts() {
  try {
    const b = await api.getFeedbackBoard(props.project.id)
    if (board.value) board.value.pending_count = b.pending_count
  } catch {
    // Count is cosmetic; ignore.
  }
}

async function createBoard() {
  saving.value = true
  try {
    applyBoard(
      await api.saveFeedbackBoard(props.project.id, {
        title: newTitle.value.trim() || undefined,
        password: newPassword.value,
      }),
    )
    newPassword.value = ''
    newTitle.value = ''
    state.value = 'pending'
    await loadPosts()
    toast.push('Feedback board created', 'success')
    emit('changed')
  } catch (err) {
    toast.push(errMessage(err, 'Failed to create board'), 'error')
  } finally {
    saving.value = false
  }
}

async function saveSettings() {
  if (edit.password && edit.password.length < 6) {
    toast.push('Password must be at least 6 characters', 'error')
    return
  }
  if (edit.password) {
    const ok = await askConfirm({
      title: 'Change password?',
      message: 'Everyone currently on the board will be signed out and need the new password.',
      confirmLabel: 'Change password',
    })
    if (!ok) return
  }
  saving.value = true
  try {
    applyBoard(
      await api.saveFeedbackBoard(props.project.id, {
        title: edit.title,
        intro: edit.intro,
        enabled: edit.enabled,
        submissions_open: edit.submissions_open,
        ...(edit.password ? { password: edit.password } : {}),
      }),
    )
    toast.push('Feedback board saved', 'success')
  } catch (err) {
    toast.push(errMessage(err, 'Failed to save'), 'error')
  } finally {
    saving.value = false
  }
}

async function rotateLink() {
  const ok = await askConfirm({
    title: 'Replace link?',
    message: 'The current link stops working and everyone is signed out. You will need to share the new link.',
    confirmLabel: 'Replace link',
    danger: true,
  })
  if (!ok) return
  saving.value = true
  try {
    applyBoard(await api.rotateFeedbackBoardLink(props.project.id))
    toast.push('New link created', 'success')
  } catch (err) {
    toast.push(errMessage(err, 'Failed to replace link'), 'error')
  } finally {
    saving.value = false
  }
}

async function deleteBoard() {
  const ok = await askConfirm({
    title: 'Delete feedback board?',
    message: 'This deletes the board and every submission and vote on it. Tasks already created from feedback are kept.',
    confirmLabel: 'Delete board',
    danger: true,
  })
  if (!ok) return
  saving.value = true
  try {
    await api.deleteFeedbackBoard(props.project.id)
    board.value = { exists: false, intro: '', enabled: false, submissions_open: false, pending_count: 0 }
    posts.value = []
    toast.push('Feedback board deleted', 'info')
    emit('changed')
  } catch (err) {
    toast.push(errMessage(err, 'Failed to delete board'), 'error')
  } finally {
    saving.value = false
  }
}

async function runPostAction(p: FeedbackPost, action: () => Promise<unknown>, success: string) {
  busyId.value = p.id
  try {
    await action()
    toast.push(success, 'success')
    await Promise.all([loadPosts(), refreshCounts()])
  } catch (err) {
    toast.push(errMessage(err, 'Action failed'), 'error')
  } finally {
    busyId.value = null
  }
}

function moderate(p: FeedbackPost, action: 'approve' | 'reject' | 'reopen') {
  const msg = action === 'approve' ? 'Approved and now public' : action === 'reject' ? 'Hidden from the board' : 'Moved back to inbox'
  return runPostAction(p, () => api.moderateFeedbackPost(props.project.id, p.id, action), msg)
}

function promote(p: FeedbackPost) {
  return runPostAction(p, () => api.promoteFeedbackPost(props.project.id, p.id), 'Task created and linked')
}

function linkTask(p: FeedbackPost) {
  const taskId = linkIds[p.id]
  if (!taskId) return
  return runPostAction(p, () => api.linkFeedbackPost(props.project.id, p.id, taskId), 'Linked to task')
}

async function removePost(p: FeedbackPost) {
  const ok = await askConfirm({
    title: 'Delete submission?',
    message: 'Delete this submission and its votes permanently?',
    confirmLabel: 'Delete',
    danger: true,
  })
  if (!ok) return
  await runPostAction(p, () => api.deleteFeedbackPost(props.project.id, p.id), 'Submission deleted')
}

async function copy(text: string) {
  try {
    await navigator.clipboard.writeText(text)
    toast.push('Copied', 'success')
  } catch {
    toast.push(text, 'info')
  }
}

function formatTime(iso: string) {
  try {
    return new Date(iso).toLocaleString()
  } catch {
    return iso
  }
}

watch(state, () => {
  if (board.value?.exists) void loadPosts()
})

watch(
  () => props.project.id,
  () => {
    state.value = 'pending'
    void loadBoard()
  },
)

onMounted(loadBoard)
</script>

<style scoped>
.fb-url {
  max-width: 22rem;
}
.fb-text {
  white-space: pre-line;
  overflow-wrap: anywhere;
}
.fb-link-input {
  width: 6.5rem;
}
</style>
