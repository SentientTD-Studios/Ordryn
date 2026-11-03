<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { api } from '@/api/client'
import type { CommentAuditList } from '@/api/types'
import { APIError } from '@/api/types'
import { useAuth } from '@/composables/useAuth'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import AdminSubnav from '@/components/AdminSubnav.vue'
import RichBody from '@/components/RichBody.vue'

const toast = useToast()
// Audit viewers can read the history; restoring changes a live comment, so it stays with admins.
const canRestore = computed(() => useAuth().hasPermission('admin'))
const { askConfirm } = useConfirm()
const loading = ref(false)
const restoringId = ref<number | null>(null)
const result = ref<CommentAuditList>({ items: [], total: 0, limit: 50, offset: 0 })

const kind = ref('')
const query = ref('')
const page = ref(0)
const pageSize = 50

const kindOptions = [
  { value: '', label: 'All changes' },
  { value: 'edit', label: 'Edits' },
  { value: 'delete', label: 'Deletes' },
  { value: 'restore', label: 'Restores' },
] as const

const totalPages = computed(() => Math.max(1, Math.ceil(result.value.total / pageSize)))
const pageLabel = computed(() => {
  if (result.value.total === 0) return 'No rows'
  const start = result.value.offset + 1
  const end = result.value.offset + result.value.items.length
  return `${start}–${end} of ${result.value.total}`
})

function kindLabel(value: string) {
  return kindOptions.find((opt) => opt.value === value)?.label || value
}

function formatWhen(iso: string) {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return d.toLocaleString()
}

async function load() {
  loading.value = true
  try {
    result.value = await api.listAdminCommentAudit({
      kind: kind.value,
      q: query.value.trim(),
      limit: pageSize,
      offset: page.value * pageSize,
    })
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Failed to load comment history', 'error')
  } finally {
    loading.value = false
  }
}

function applyFilters() {
  page.value = 0
  void load()
}

function prevPage() {
  if (page.value <= 0) return
  page.value -= 1
  void load()
}

function nextPage() {
  if (page.value + 1 >= totalPages.value) return
  page.value += 1
  void load()
}

async function restore(revisionId: number) {
  const ok = await askConfirm({
    title: 'Restore comment',
    message: 'Replace the live comment with this previous version? The current text is saved to the audit log.',
    confirmLabel: 'Restore',
  })
  if (!ok) return
  restoringId.value = revisionId
  try {
    await api.restoreAdminCommentRevision(revisionId)
    toast.push('Comment restored', 'success')
    await load()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Could not restore comment', 'error')
  } finally {
    restoringId.value = null
  }
}

watch(kind, applyFilters)

onMounted(load)
</script>

<template>
  <div class="container mt-3">
    <AdminSubnav />
    <h1>Comment history</h1>
    <p class="text-muted">
      Previous discussion comment content captured when someone edited, deleted, or restored a post.
      Restore puts that text back on the live comment.
    </p>

    <form class="row g-3 align-items-end mb-3" @submit.prevent="applyFilters">
      <div class="col-sm-6 col-md-3">
        <label class="form-label" for="comment-audit-kind">Change</label>
        <select id="comment-audit-kind" v-model="kind" class="form-select">
          <option v-for="opt in kindOptions" :key="opt.value || 'all'" :value="opt.value">
            {{ opt.label }}
          </option>
        </select>
      </div>
      <div class="col-sm-6 col-md-5">
        <label class="form-label" for="comment-audit-q">Search</label>
        <input
          id="comment-audit-q"
          v-model="query"
          type="search"
          class="form-control"
          placeholder="body, task, project, or username"
          autocomplete="off"
        />
      </div>
      <div class="col-sm-6 col-md-3">
        <button type="submit" class="btn btn-primary" :disabled="loading">
          {{ loading ? 'Loading…' : 'Search' }}
        </button>
      </div>
    </form>

    <div class="card">
      <div class="table-responsive">
        <table class="table table-hover mb-0">
          <thead>
            <tr>
              <th>When</th>
              <th>Change</th>
              <th>Task</th>
              <th>Author</th>
              <th>Changed by</th>
              <th>Previous content</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="!loading && result.items.length === 0">
              <td colspan="7" class="text-muted py-4 text-center">No matching comment history.</td>
            </tr>
            <tr v-for="row in result.items" :key="row.id">
              <td class="text-nowrap small">{{ formatWhen(row.created_at) }}</td>
              <td>{{ kindLabel(row.kind) }}</td>
              <td>
                <div>{{ row.task_title || `Task #${row.task_id}` }}</div>
                <div v-if="row.project_name" class="text-muted small">{{ row.project_name }}</div>
                <div v-if="row.comment_deleted" class="small text-warning">Currently deleted</div>
              </td>
              <td>{{ row.author_user_name || '—' }}</td>
              <td>{{ row.edited_by_user_name || '—' }}</td>
              <td class="small" style="max-width: 22rem; word-break: break-word">
                <RichBody v-if="row.body.trim()" compact :body="row.body" />
                <span v-else>—</span>
              </td>
              <td class="text-nowrap">
                <button
                  v-if="canRestore && row.body.trim()"
                  type="button"
                  class="btn btn-sm btn-outline-secondary"
                  :disabled="restoringId === row.id"
                  @click="restore(row.id)"
                >
                  {{ restoringId === row.id ? 'Restoring…' : 'Restore' }}
                </button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <div class="card-footer d-flex justify-content-between align-items-center gap-2">
        <span class="text-muted small">{{ pageLabel }}</span>
        <div class="btn-group btn-group-sm">
          <button type="button" class="btn btn-outline-secondary" :disabled="page <= 0 || loading" @click="prevPage">
            Previous
          </button>
          <button
            type="button"
            class="btn btn-outline-secondary"
            :disabled="page + 1 >= totalPages || loading"
            @click="nextPage"
          >
            Next
          </button>
        </div>
      </div>
    </div>
  </div>
</template>
