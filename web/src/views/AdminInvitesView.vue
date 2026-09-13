<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { api } from '@/api/client'
import type { Invite } from '@/api/types'
import { APIError } from '@/api/types'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import { useSite } from '@/composables/useSite'
import AdminSubnav from '@/components/AdminSubnav.vue'

const invites = ref<Invite[]>([])
const email = ref('')
const expiresAt = ref('')
const neverExpires = ref(false)
const revealed = ref<Record<number, boolean>>({})
const busy = ref(false)
const toast = useToast()
const { askConfirm } = useConfirm()
const { siteInfo } = useSite()

function defaultExpirationDate(): string {
  const days = siteInfo.value?.invite_expiration_days ?? 7
  if (days <= 0) return ''
  const d = new Date()
  d.setDate(d.getDate() + days)
  return d.toISOString().split('T')[0]
}

async function load() {
  try {
    invites.value = await api.listAdminInvites()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Failed to load invites', 'error')
  }
}

async function create() {
  if (!email.value.trim()) return
  busy.value = true
  try {
    const to = email.value.trim()
    const exp = !neverExpires.value ? expiresAt.value || undefined : undefined
    const bypass = neverExpires.value
    await api.createAdminInvite(to, exp, bypass)
    email.value = ''
    expiresAt.value = defaultExpirationDate()
    neverExpires.value = false
    toast.push(`Invite sent to ${to}`, 'success')
    await load()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Create failed', 'error')
  } finally {
    busy.value = false
  }
}

async function remove(inv: Invite) {
  const ok = await askConfirm({
    title: 'Delete invite?',
    message: `Delete invite for ${inv.email}?`,
    confirmLabel: 'Delete',
    danger: true,
  })
  if (!ok) return
  try {
    await api.deleteAdminInvite(inv.id)
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

function formatDate(val?: string | null): string {
  if (!val) return 'Never'
  const d = new Date(val)
  if (isNaN(d.getTime())) return val
  return d.toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric' })
}

onMounted(() => {
  expiresAt.value = defaultExpirationDate()
  void load()
})
</script>

<template>
  <div class="container mt-3">
    <AdminSubnav />
    <h1>Site Invites</h1>
    <p class="text-muted">
      Manage all invitation tokens sent to join this site. Registration requests are managed separately under Requests.
    </p>

    <div class="card mb-4">
      <div class="card-body">
        <h2 class="card-title h5">Create Invite</h2>
        <form id="admin-create-invite-form" @submit.prevent="create">
          <div class="row g-3 align-items-end">
            <div class="col-md-6">
              <label for="admin-invite-email" class="form-label">Email</label>
              <input
                id="admin-invite-email"
                v-model="email"
                type="email"
                class="form-control"
                name="email"
                required
                placeholder="user@example.com"
              />
            </div>
            <div class="col-md-4">
              <label for="admin-invite-expires" class="form-label d-flex justify-content-between align-items-center mb-1">
                <span>Expires on</span>
                <span class="form-check form-switch m-0 d-inline-flex align-items-center gap-1">
                  <input
                    id="admin-no-expiry"
                    v-model="neverExpires"
                    type="checkbox"
                    class="form-check-input"
                  />
                  <label for="admin-no-expiry" class="form-check-label small text-muted">Never expires</label>
                </span>
              </label>
              <input
                id="admin-invite-expires"
                v-model="expiresAt"
                type="date"
                class="form-control"
                :disabled="neverExpires || busy"
              />
            </div>
            <div class="col-md-2">
              <button type="submit" class="btn btn-primary w-100" :disabled="busy">
                <i class="bi bi-plus-lg" /> Send
              </button>
            </div>
          </div>
        </form>
      </div>
    </div>

    <div class="card">
      <div class="card-body">
        <h2 class="card-title h5 mb-3">All Existing Invites</h2>
        <div class="table-responsive">
          <table class="table table-striped align-middle">
            <thead>
              <tr>
                <th>Email</th>
                <th>Token</th>
                <th>Sent By</th>
                <th>Created</th>
                <th>Expires</th>
                <th>Status</th>
                <th style="width: 100px;">Actions</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="inv in invites" :key="inv.id" :id="`admin-invite-row-${inv.id}`">
                <td data-label="Email">
                  <strong>{{ inv.email }}</strong>
                </td>
                <td data-label="Token">
                  <code class="token-masked me-1">{{ revealed[inv.id] ? inv.token : '••••••••' }}</code>
                  <button
                    type="button"
                    class="btn btn-sm btn-link p-0 me-1"
                    :aria-label="revealed[inv.id] ? 'Hide token' : 'Show token'"
                    @click="toggleToken(inv.id)"
                  >
                    {{ revealed[inv.id] ? 'Hide' : 'Show' }}
                  </button>
                  <button
                    type="button"
                    class="btn btn-sm btn-link p-0"
                    aria-label="Copy token"
                    @click="copyToken(inv)"
                  >
                    Copy
                  </button>
                </td>
                <td data-label="Sent By">
                  <span v-if="inv.creator_user_name || inv.creator_email">
                    {{ inv.creator_user_name || inv.creator_email }}
                  </span>
                  <span v-else class="text-muted">Admin / System</span>
                </td>
                <td data-label="Created" class="small text-muted">
                  {{ formatDate(inv.created_at) }}
                </td>
                <td data-label="Expires" class="small text-muted">
                  {{ formatDate(inv.expires_at) }}
                </td>
                <td data-label="Status">
                  <span v-if="inv.status === 'used' || inv.used" class="badge bg-success">Used</span>
                  <span v-else-if="inv.status === 'expired'" class="badge bg-danger">Expired</span>
                  <span v-else class="badge bg-warning text-dark">Pending</span>
                </td>
                <td data-label="Actions">
                  <button
                    v-if="!inv.used"
                    class="btn btn-sm btn-outline-danger"
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
                <td colspan="7" class="text-muted text-center py-4">No invites found.</td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </div>
  </div>
</template>
