<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { api } from '@/api/client'
import type { Invite } from '@/api/types'
import { APIError } from '@/api/types'
import { useToast } from '@/composables/useToast'

const invites = ref<Invite[]>([])
const email = ref('')
const revealed = ref<Record<number, boolean>>({})
const toast = useToast()

async function load() {
  try {
    invites.value = await api.listInvites()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Failed to load invites', 'error')
  }
}

async function create() {
  if (!email.value.trim()) return
  try {
    await api.createInvite(email.value.trim())
    email.value = ''
    toast.push('Invite created', 'success')
    await load()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Create failed', 'error')
  }
}

async function remove(inv: Invite) {
  if (!confirm(`Delete invite for ${inv.email}?`)) return
  try {
    await api.deleteInvite(inv.id)
    toast.push('Invite deleted', 'info')
    await load()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Delete failed', 'error')
  }
}

function toggleToken(id: number) {
  revealed.value = { ...revealed.value, [id]: !revealed.value[id] }
}

async function copyToken(inv: Invite) {
  try {
    await navigator.clipboard.writeText(inv.token)
    toast.push('Token copied', 'success')
  } catch {
    toast.push('Could not copy token', 'error')
  }
}

onMounted(() => {
  document.body.classList.add('create-invite-page')
  void load()
})

onUnmounted(() => {
  document.body.classList.remove('create-invite-page')
})
</script>

<template>
  <div class="container mt-3">
    <div class="card">
      <div class="card-body">
        <h1 class="card-title">Create Invite</h1>

        <form id="create-invite-form" @submit.prevent="create">
          <div class="mb-3">
            <label for="email" class="form-label">Email</label>
            <input
              id="email"
              v-model="email"
              type="email"
              class="form-control"
              name="email"
              required
              placeholder="user@example.com"
            />
          </div>
          <button type="submit" class="btn btn-primary">
            <i class="bi bi-plus-lg" /> Create Invite
          </button>
        </form>
      </div>
    </div>

    <div class="card mt-4 invite-list-card">
      <div class="card-body">
        <h2 class="card-title">Existing Invites</h2>
        <div class="table-responsive">
          <table class="table table-striped">
            <thead>
              <tr>
                <th>Email</th>
                <th>Token</th>
                <th>Status</th>
                <th>Actions</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="inv in invites" :key="inv.id" :id="`invite-row-${inv.id}`">
                <td class="title-column" data-label="Email">{{ inv.email }}</td>
                <td class="desc-column" data-label="Token">
                  <code class="token-masked">{{ revealed[inv.id] ? inv.token : '••••••••' }}</code>
                  <button
                    type="button"
                    class="btn btn-sm btn-link p-0 ms-1 reveal-token-btn"
                    :aria-label="revealed[inv.id] ? 'Hide invite token' : 'Show invite token'"
                    @click="toggleToken(inv.id)"
                  >
                    {{ revealed[inv.id] ? 'Hide' : 'Show' }}
                  </button>
                  <button
                    type="button"
                    class="btn btn-sm btn-link p-0 ms-1"
                    aria-label="Copy invite token"
                    @click="copyToken(inv)"
                  >
                    Copy
                  </button>
                </td>
                <td class="status-column" data-label="Status">
                  <span v-if="inv.used" class="badge bg-success">Active</span>
                  <span v-else class="badge bg-warning">Pending</span>
                </td>
                <td class="delete-column" data-label="Actions">
                  <button
                    v-if="!inv.used"
                    class="btn btn-sm btn-danger"
                    type="button"
                    title="Delete invite"
                    :aria-label="`Delete invite ${inv.id}`"
                    @click="remove(inv)"
                  >
                    <i class="bi bi-trash" />
                  </button>
                  <span v-else class="text-muted">—</span>
                </td>
              </tr>
              <tr v-if="!invites.length">
                <td colspan="4" class="text-muted">No invites yet.</td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </div>
  </div>
</template>
