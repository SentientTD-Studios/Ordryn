<template>
  <NotFoundView v-if="notFound" />
  <div v-else class="container mt-4 mb-5 feedback-board">
    <p v-if="loading" class="text-muted">Loading…</p>
    <div v-else-if="loadError" class="alert alert-warning" role="alert">{{ loadError }}</div>

    <!-- Locked: reveal nothing about the board until the password is entered. -->
    <div v-else-if="view?.locked" class="card mx-auto feedback-lock">
      <div class="card-body">
        <h1 class="h5 mb-2"><i class="bi bi-lock me-2" aria-hidden="true" />Private feedback board</h1>
        <p class="text-muted small">Enter the password you were given to view and send feedback.</p>
        <form @submit.prevent="unlock">
          <label class="form-label small mb-1" for="fb-password">Password</label>
          <input
            id="fb-password"
            v-model="password"
            type="password"
            class="form-control mb-2"
            autocomplete="current-password"
            maxlength="512"
            required
            :aria-invalid="!!unlockError"
            aria-describedby="fb-password-error"
          />
          <p v-if="unlockError" id="fb-password-error" class="text-danger small mb-2" role="alert">{{ unlockError }}</p>
          <button class="btn btn-primary w-100" type="submit" :disabled="unlocking || !password">
            {{ unlocking ? 'Checking…' : 'Unlock' }}
          </button>
        </form>
      </div>
    </div>

    <template v-else-if="view">
      <header class="d-flex flex-wrap justify-content-between align-items-start gap-2 mb-3">
        <div class="min-w-0">
          <h1 class="h3 mb-1 text-break">{{ view.title }}</h1>
          <p v-if="view.intro" class="text-muted mb-0 feedback-text">{{ view.intro }}</p>
        </div>
        <button class="btn btn-sm btn-link text-muted px-0" type="button" @click="lock">
          <i class="bi bi-box-arrow-right me-1" aria-hidden="true" />Leave board
        </button>
      </header>

      <div class="row g-4">
        <div class="col-lg-5 order-lg-2">
          <div class="card">
            <div class="card-body">
              <h2 class="h6">Send feedback</h2>
              <p v-if="!view.submissions_open" class="small text-muted mb-0">
                This board isn't taking new submissions right now. You can still vote below.
              </p>
              <form v-else @submit.prevent="submit" novalidate>
                <label class="form-label small mb-1" for="fb-title">Title</label>
                <input
                  id="fb-title"
                  v-model="form.title"
                  class="form-control form-control-sm mb-2"
                  maxlength="140"
                  required
                  placeholder="Short summary"
                />
                <label class="form-label small mb-1" for="fb-body">Details <span class="text-muted">(optional)</span></label>
                <textarea
                  id="fb-body"
                  v-model="form.body"
                  class="form-control form-control-sm mb-1"
                  rows="5"
                  maxlength="4000"
                  placeholder="What happened, what you expected, steps to reproduce…"
                />
                <div class="small text-muted text-end mb-2">{{ form.body.length }}/4000</div>
                <label class="form-label small mb-1" for="fb-name">Your name <span class="text-muted">(optional)</span></label>
                <input
                  id="fb-name"
                  v-model="form.author_name"
                  class="form-control form-control-sm mb-2"
                  maxlength="60"
                  autocomplete="nickname"
                />
                <!-- Honeypot: hidden from people and assistive tech; bots fill it in. -->
                <div class="fb-hp" aria-hidden="true">
                  <label for="fb-website">Website</label>
                  <input id="fb-website" v-model="form.website" type="text" tabindex="-1" autocomplete="off" />
                </div>
                <p v-if="submitError" class="text-danger small mb-2" role="alert">{{ submitError }}</p>
                <p v-if="submitted" class="text-success small mb-2" role="status">
                  Thanks! Your feedback will show up here once it's been reviewed.
                </p>
                <button class="btn btn-sm btn-primary" type="submit" :disabled="submitting || form.title.trim().length < 3">
                  {{ submitting ? 'Sending…' : 'Send feedback' }}
                </button>
              </form>
            </div>
          </div>

          <div v-if="(view.my_pending || []).length" class="card mt-3">
            <div class="card-body">
              <h2 class="h6 mb-2">Waiting for review</h2>
              <p class="small text-muted mb-2">Only you can see these until they're approved.</p>
              <ul class="list-unstyled small mb-0">
                <li v-for="p in view.my_pending" :key="p.id" class="mb-1 text-break">
                  <i class="bi bi-hourglass-split me-1 text-muted" aria-hidden="true" />{{ p.title }}
                </li>
              </ul>
            </div>
          </div>
        </div>

        <div class="col-lg-7 order-lg-1">
          <div class="d-flex flex-wrap gap-1 mb-3" role="group" aria-label="Filter by status">
            <button
              type="button"
              class="btn btn-sm"
              :class="statusFilter === '' ? 'btn-secondary' : 'btn-outline-secondary'"
              :aria-pressed="statusFilter === ''"
              @click="statusFilter = ''"
            >
              All <span class="opacity-75">{{ posts.length }}</span>
            </button>
            <button
              v-for="s in FEEDBACK_STATUS_ORDER"
              :key="s"
              type="button"
              class="btn btn-sm"
              :class="statusFilter === s ? 'btn-secondary' : 'btn-outline-secondary'"
              :aria-pressed="statusFilter === s"
              @click="statusFilter = s"
            >
              {{ feedbackStatusLabel(s) }} <span class="opacity-75">{{ counts[s] }}</span>
            </button>
          </div>

          <p v-if="!visiblePosts.length" class="text-muted">
            {{ posts.length ? 'Nothing with this status yet.' : 'No feedback has been shared yet. Be the first!' }}
          </p>
          <ul v-else class="list-unstyled mb-0">
            <li v-for="p in visiblePosts" :key="p.id" class="card mb-2">
              <div class="card-body d-flex gap-3">
                <button
                  type="button"
                  class="btn btn-sm vote-btn flex-shrink-0"
                  :class="p.voted ? 'btn-primary' : 'btn-outline-primary'"
                  :aria-pressed="p.voted"
                  :aria-label="(p.voted ? 'Remove vote for ' : 'Vote for ') + p.title"
                  :disabled="votingId === p.id"
                  @click="toggleVote(p)"
                >
                  <i class="bi bi-caret-up-fill d-block" aria-hidden="true" />
                  <span class="fw-semibold">{{ p.vote_count }}</span>
                </button>
                <div class="min-w-0 flex-grow-1">
                  <div class="d-flex flex-wrap align-items-center gap-2 mb-1">
                    <h3 class="h6 mb-0 text-break">{{ p.title }}</h3>
                    <span class="badge" :class="feedbackStatusBadge(p.status)">{{ feedbackStatusLabel(p.status) }}</span>
                  </div>
                  <p v-if="p.body" class="small mb-1 feedback-text">{{ p.body }}</p>
                  <div class="small text-muted">
                    <span v-if="p.author_name">{{ p.author_name }} · </span>{{ formatDate(p.created_at) }}
                  </div>
                </div>
              </div>
            </li>
          </ul>
        </div>
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useRoute } from 'vue-router'
import { api } from '@/api/client'
import type { PublicFeedbackPost, PublicFeedbackView } from '@/api/types'
import { APIError } from '@/api/types'
import NotFoundView from '@/views/NotFoundView.vue'
import {
  FEEDBACK_STATUS_ORDER,
  applyFeedbackVote,
  countFeedbackByStatus,
  feedbackStatusBadge,
  feedbackStatusLabel,
  filterFeedbackByStatus,
} from '@/utils/feedbackStatus'

const route = useRoute()
const slug = computed(() => String(route.params.slug || ''))

const view = ref<PublicFeedbackView | null>(null)
const posts = ref<PublicFeedbackPost[]>([])
const loading = ref(true)
const notFound = ref(false)
const loadError = ref('')

const password = ref('')
const unlocking = ref(false)
const unlockError = ref('')

const form = reactive({ title: '', body: '', author_name: '', website: '' })
const submitting = ref(false)
const submitError = ref('')
const submitted = ref(false)

const statusFilter = ref('')
const votingId = ref<number | null>(null)

const counts = computed(() => countFeedbackByStatus(posts.value))
const visiblePosts = computed(() => filterFeedbackByStatus(posts.value, statusFilter.value))

function errMessage(err: unknown, fallback: string) {
  return err instanceof APIError ? err.message : fallback
}

async function load() {
  if (!slug.value) {
    notFound.value = true
    loading.value = false
    return
  }
  loadError.value = ''
  try {
    const v = await api.viewPublicFeedback(slug.value)
    view.value = v
    posts.value = v.posts || []
    document.title = v.locked ? 'Feedback' : `${v.title} · Feedback`
  } catch (err) {
    if (err instanceof APIError && err.status === 404) {
      notFound.value = true
    } else {
      loadError.value = errMessage(err, 'Could not load this board. Try again in a moment.')
    }
  } finally {
    loading.value = false
  }
}

async function unlock() {
  unlockError.value = ''
  unlocking.value = true
  try {
    await api.unlockPublicFeedback(slug.value, password.value)
    password.value = ''
    await load()
  } catch (err) {
    unlockError.value = errMessage(err, 'Could not unlock the board.')
  } finally {
    unlocking.value = false
  }
}

async function lock() {
  try {
    await api.lockPublicFeedback(slug.value)
  } catch {
    // Signing out is best effort; reloading shows the lock screen either way.
  }
  posts.value = []
  await load()
}

function handleLockedError(err: unknown) {
  if (err instanceof APIError && err.status === 401) {
    // Password changed or link rotated while the page was open.
    void load()
    return true
  }
  return false
}

async function submit() {
  submitError.value = ''
  submitted.value = false
  if (form.title.trim().length < 3) {
    submitError.value = 'Add a short title (at least 3 characters).'
    return
  }
  submitting.value = true
  try {
    await api.submitPublicFeedback(slug.value, {
      title: form.title,
      body: form.body,
      author_name: form.author_name,
      website: form.website,
      form_token: view.value?.form_token || '',
    })
    form.title = ''
    form.body = ''
    submitted.value = true
    await load() // fresh form token and pending list
  } catch (err) {
    if (!handleLockedError(err)) submitError.value = errMessage(err, 'Could not send your feedback.')
  } finally {
    submitting.value = false
  }
}

async function toggleVote(p: PublicFeedbackPost) {
  votingId.value = p.id
  try {
    const res = await api.votePublicFeedback(slug.value, p.id, !p.voted)
    posts.value = applyFeedbackVote(posts.value, p.id, res)
  } catch (err) {
    if (!handleLockedError(err)) submitError.value = errMessage(err, 'Could not save your vote.')
  } finally {
    votingId.value = null
  }
}

function formatDate(iso: string) {
  try {
    return new Date(iso).toLocaleDateString()
  } catch {
    return iso
  }
}

onMounted(load)
</script>

<style scoped>
.feedback-lock {
  max-width: 24rem;
}
.feedback-text {
  white-space: pre-line;
  overflow-wrap: anywhere;
}
.vote-btn {
  min-width: 3rem;
  line-height: 1.1;
  align-self: flex-start;
}
.min-w-0 {
  min-width: 0;
}
/* Off-screen rather than display:none, which some bots detect and skip. */
.fb-hp {
  position: absolute;
  left: -10000px;
  width: 1px;
  height: 1px;
  overflow: hidden;
}
</style>
