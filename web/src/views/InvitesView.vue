<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { api } from '@/api/client'
import type { Invite } from '@/api/types'
import { APIError } from '@/api/types'
import { useToast } from '@/composables/useToast'

const invites = ref<Invite[]>([])
const email = ref('')
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
  try {
    await api.deleteInvite(inv.id)
    toast.push('Invite deleted', 'info')
    await load()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Delete failed', 'error')
  }
}

async function copyToken(inv: Invite) {
  try {
    await navigator.clipboard.writeText(inv.token)
    toast.push('Token copied', 'success')
  } catch {
    toast.push('Could not copy token', 'error')
  }
}

onMounted(load)
</script>

<template>
  <section class="page">
    <header class="page-head">
      <div>
        <h1>Invites</h1>
        <p class="lede">Create registration tokens when invite-only mode is on.</p>
      </div>
    </header>

    <form class="composer" @submit.prevent="create">
      <input v-model="email" type="email" placeholder="invitee@example.com" required />
      <button class="primary" type="submit">Create invite</button>
    </form>

    <ul class="plain-list">
      <li v-for="inv in invites" :key="inv.id" class="row">
        <div class="task-body">
          <strong>{{ inv.email }}</strong>
          <p class="meta muted">
            {{ inv.used ? 'Used' : 'Unused' }} · {{ inv.token }}
          </p>
        </div>
        <button type="button" class="ghost" @click="copyToken(inv)">Copy</button>
        <button
          v-if="!inv.used"
          type="button"
          class="ghost danger"
          @click="remove(inv)"
        >
          Delete
        </button>
      </li>
      <li v-if="!invites.length" class="muted empty">No invites yet.</li>
    </ul>
  </section>
</template>
