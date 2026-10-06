<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { api } from '@/api/client'
import type { ProjectRoleDef } from '@/api/types'
import { APIError } from '@/api/types'
import AdminSubnav from '@/components/AdminSubnav.vue'
import { useToast } from '@/composables/useToast'

const toast = useToast()
const roles = ref<ProjectRoleDef[]>([])
const saving = ref(false)
const editing = ref(false)
const formName = ref('')
const formDescription = ref('')

const owner = computed(() => roles.value.find((r) => r.slug === 'owner') || null)

async function load() {
  try {
    const data = await api.listAdminProjectRoles()
    roles.value = data.roles || []
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Failed to load roles', 'error')
  }
}

function startEdit() {
  if (!owner.value) return
  formName.value = owner.value.name
  formDescription.value = owner.value.description || ''
  editing.value = true
}

async function saveOwner() {
  const name = formName.value.trim()
  if (!owner.value || !name) return
  saving.value = true
  try {
    await api.updateAdminProjectRole(owner.value.id, {
      name,
      description: formDescription.value.trim(),
    })
    toast.push('Owner role updated', 'success')
    editing.value = false
    await load()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Could not save role', 'error')
  } finally {
    saving.value = false
  }
}

onMounted(load)
</script>

<template>
  <div class="container mt-3">
    <AdminSubnav />
    <h1>Project roles</h1>
    <p class="text-muted">
      Owner is the only site-wide role. It always has every permission, including any permission
      added in a future release. Organizations and projects create every other role themselves, and
      can rename Owner for their own members.
    </p>

    <div v-if="owner" class="card mb-4">
      <form v-if="editing" class="card-body" @submit.prevent="saveOwner">
        <h2 class="h5">Edit {{ owner.name }}</h2>
        <div class="mb-2">
          <label class="form-label small mb-0" for="admin-role-name">Name</label>
          <input
            id="admin-role-name"
            v-model="formName"
            type="text"
            class="form-control"
            maxlength="80"
            required
          />
        </div>
        <div class="mb-2">
          <label class="form-label small mb-0" for="admin-role-desc">Description</label>
          <input
            id="admin-role-desc"
            v-model="formDescription"
            type="text"
            class="form-control"
            maxlength="200"
          />
        </div>
        <div class="d-flex gap-2">
          <button class="btn btn-primary" type="submit" :disabled="saving || !formName.trim()">Save role</button>
          <button class="btn btn-outline-secondary" type="button" @click="editing = false">Cancel</button>
        </div>
      </form>
      <div v-else class="card-body py-3 d-flex flex-wrap justify-content-between gap-2">
        <div>
          <strong>{{ owner.name }}</strong>
          <span class="badge text-bg-secondary ms-1">{{ owner.slug }}</span>
          <span class="badge text-bg-info ms-1">built-in</span>
          <div class="small text-muted">{{ owner.description || 'No description' }}</div>
          <div class="small text-muted">All permissions</div>
        </div>
        <div>
          <button class="btn btn-sm btn-outline-secondary" type="button" @click="startEdit">Edit</button>
        </div>
      </div>
    </div>
    <p v-else class="text-muted">The Owner role has not been created yet. Restart the server to run migrations.</p>
  </div>
</template>
