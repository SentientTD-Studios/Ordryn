<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { api } from '@/api/client'
import type { Tag } from '@/api/types'
import { APIError } from '@/api/types'
import TimezoneSelect from '@/components/TimezoneSelect.vue'
import { useConfirm } from '@/composables/useConfirm'
import { useToast } from '@/composables/useToast'
import { isProtectedTag } from '@/utils/tags'

defineProps<{
  timezone: string
  itemsPerPage: number
  allowProjectInvites: boolean
  homeSplitView: boolean
  busy: boolean
  dirty: boolean
}>()

const emit = defineEmits<{
  'update:timezone': [value: string]
  'update:itemsPerPage': [value: number]
  'update:allowProjectInvites': [value: boolean]
  'update:homeSplitView': [value: boolean]
  save: []
}>()

const { push } = useToast()

const showingOnboarding = ref(false)
async function showOnboarding() {
  showingOnboarding.value = true
  try {
    await api.setOnboardingDismissed(false)
    push('The checklist is back on Home', 'success')
  } catch (err) {
    push(err instanceof APIError ? err.message : 'Could not update', 'error')
  } finally {
    showingOnboarding.value = false
  }
}
const { askConfirm } = useConfirm()
const tags = ref<Tag[]>([])
const tagName = ref('')
const renameTagId = ref<number | null>(null)
const renameTagValue = ref('')

async function loadTags() {
  try {
    tags.value = await api.listTags({ project_id: 0 })
  } catch (err) {
    push(err instanceof APIError ? err.message : 'Failed to load tags', 'error')
  }
}

async function createTag() {
  if (!tagName.value.trim()) return
  try {
    await api.createTag(tagName.value.trim())
    tagName.value = ''
    tags.value = await api.listTags({ project_id: 0 })
    push('Tag created', 'success')
  } catch (err) {
    push(err instanceof APIError ? err.message : 'Create failed', 'error')
  }
}

function beginRenameTag(tag: Tag) {
  renameTagId.value = tag.id
  renameTagValue.value = tag.name
}

async function saveRenameTag() {
  if (renameTagId.value == null || !renameTagValue.value.trim()) return
  try {
    await api.renameTag(renameTagId.value, renameTagValue.value.trim())
    renameTagId.value = null
    tags.value = await api.listTags({ project_id: 0 })
    push('Tag renamed', 'success')
  } catch (err) {
    push(err instanceof APIError ? err.message : 'Rename failed', 'error')
  }
}

async function removeTag(tag: Tag) {
  const ok = await askConfirm({
    title: 'Delete tag?',
    message: `Delete tag “${tag.name}”? It will be removed from inbox tasks that use it.`,
    confirmLabel: 'Delete',
    danger: true,
  })
  if (!ok) return
  try {
    await api.deleteTag(tag.id)
    tags.value = await api.listTags({ project_id: 0 })
    push('Tag deleted', 'info')
  } catch (err) {
    push(err instanceof APIError ? err.message : 'Delete failed', 'error')
  }
}

onMounted(() => {
  void loadTags()
})
</script>

<template>
  <div class="card mb-4">
    <div class="card-header">
      <h3 class="card-title mb-0">Preferences</h3>
    </div>
    <div class="card-body">
      <form @submit.prevent="emit('save')">
        <div class="mb-3">
          <label for="profile-timezone" class="form-label">Timezone</label>
          <TimezoneSelect
            id="profile-timezone"
            :model-value="timezone"
            required
            @update:model-value="emit('update:timezone', $event)"
          />
        </div>
        <div class="mb-3">
          <label for="profile-per-page" class="form-label">Tasks per page</label>
          <select
            id="profile-per-page"
            class="form-select"
            :value="itemsPerPage"
            @change="emit('update:itemsPerPage', Number(($event.target as HTMLSelectElement).value))"
          >
            <option :value="10">10</option>
            <option :value="15">15</option>
            <option :value="25">25</option>
            <option :value="50">50</option>
          </select>
        </div>
        <div class="form-check mb-3">
          <input
            id="profile-allow-invites"
            class="form-check-input"
            type="checkbox"
            :checked="allowProjectInvites"
            @change="emit('update:allowProjectInvites', ($event.target as HTMLInputElement).checked)"
          />
          <label class="form-check-label" for="profile-allow-invites">
            Allow project invites
          </label>
          <div class="form-text">
            When off, other users cannot invite you to shared projects. Existing memberships are unchanged.
          </div>
        </div>
        <div class="form-check mb-3">
          <input
            id="profile-home-split"
            class="form-check-input"
            type="checkbox"
            :checked="homeSplitView"
            @change="emit('update:homeSplitView', ($event.target as HTMLInputElement).checked)"
          />
          <label class="form-check-label" for="profile-home-split">
            Show projects above tasks on Home
          </label>
          <div class="form-text">
            Adds a grid of your projects to the top of the Home page, above your task list.
          </div>
        </div>
        <button type="submit" class="btn btn-primary" :disabled="busy || !dirty">
          {{ busy ? 'Saving…' : 'Save preferences' }}
        </button>
      </form>
      <hr />
      <div class="d-flex flex-wrap align-items-center gap-2">
        <span class="small text-muted">Hid the getting-started checklist?</span>
        <button type="button" class="btn btn-sm btn-outline-secondary" :disabled="showingOnboarding" @click="showOnboarding">
          Show it on Home again
        </button>
      </div>
    </div>
  </div>

  <div class="card mb-4">
    <div class="card-header">
      <h3 class="card-title mb-0">Personal tags</h3>
    </div>
    <div class="card-body">
      <p class="text-muted small mb-3">
        These tags apply to inbox tasks (tasks not in a project). Project tags are managed in project settings.
      </p>
      <form class="row g-2 mb-3" @submit.prevent="createTag">
        <div class="col-sm-8">
          <input v-model="tagName" type="text" class="form-control" placeholder="New tag" required maxlength="50" />
        </div>
        <div class="col-sm-4">
          <button type="submit" class="btn btn-primary w-100">Add tag</button>
        </div>
      </form>
      <ul class="list-group">
        <li v-for="tag in tags" :key="tag.id" class="list-group-item">
          <div class="d-flex flex-wrap gap-2 align-items-center">
            <template v-if="renameTagId === tag.id">
              <input v-model="renameTagValue" type="text" class="form-control form-control-sm" maxlength="50" />
              <button type="button" class="btn btn-sm btn-primary" @click="saveRenameTag">Save</button>
              <button type="button" class="btn btn-sm btn-secondary" @click="renameTagId = null">Cancel</button>
            </template>
            <template v-else>
              <span class="flex-grow-1">{{ tag.name }}</span>
              <span v-if="isProtectedTag(tag)" class="badge text-bg-secondary">System</span>
              <button
                v-if="!isProtectedTag(tag)"
                type="button"
                class="btn btn-sm btn-outline-secondary"
                @click="beginRenameTag(tag)"
              >
                Rename
              </button>
              <button
                v-if="!isProtectedTag(tag)"
                type="button"
                class="btn btn-sm btn-outline-danger"
                @click="removeTag(tag)"
              >
                Delete
              </button>
            </template>
          </div>
        </li>
        <li v-if="!tags.length" class="list-group-item text-muted">No personal tags yet.</li>
      </ul>
    </div>
  </div>
</template>
