<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { api } from '@/api/client'
import type { Project, Tag } from '@/api/types'
import { APIError } from '@/api/types'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import { isProtectedTag } from '@/utils/tags'
import { hasProjectPerm, PROJECT_PERMS } from '@/utils/projectPerms'

const props = defineProps<{
  project: Project
}>()

const emit = defineEmits<{
  changed: []
}>()

const toast = useToast()
const { askConfirm } = useConfirm()
const tags = ref<Tag[]>([])
const tagName = ref('')
const renameTagId = ref<number | null>(null)
const renameTagValue = ref('')
const renameTagColor = ref('#6c757d')
const loading = ref(false)

const canManage = computed(() => hasProjectPerm(props.project, PROJECT_PERMS.PROJECT_TAGS))

async function load() {
  loading.value = true
  try {
    tags.value = await api.listTags({ project_id: props.project.id })
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Failed to load tags', 'error')
  } finally {
    loading.value = false
  }
}

watch(
  () => props.project.id,
  () => {
    void load()
  },
  { immediate: true },
)

async function createTag() {
  if (!tagName.value.trim() || !canManage.value) return
  try {
    await api.createTag(tagName.value.trim(), props.project.id)
    tagName.value = ''
    toast.push('Tag created', 'success')
    await load()
    emit('changed')
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Create failed', 'error')
  }
}

function beginRenameTag(tag: Tag) {
  renameTagId.value = tag.id
  renameTagValue.value = tag.name
  renameTagColor.value = tag.color || '#6c757d'
}

async function saveRenameTag() {
  if (renameTagId.value == null || !renameTagValue.value.trim()) return
  try {
    await api.updateTag(renameTagId.value, {
      name: renameTagValue.value.trim(),
      color: renameTagColor.value
    })

    renameTagId.value = null
    toast.push('Tag updated', 'success')
    await load()
    emit('changed')
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Rename failed', 'error')
  }
}

async function removeTag(tag: Tag) {
  const ok = await askConfirm({
    title: 'Delete tag?',
    message: `Delete tag “${tag.name}”? It will be removed from all tasks in this project.`,
    confirmLabel: 'Delete',
    danger: true,
  })
  if (!ok) return
  try {
    await api.deleteTag(tag.id)
    toast.push('Tag deleted', 'info')
    await load()
    emit('changed')
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Delete failed', 'error')
  }
}
</script>

<template>
  <div>
    <h4 class="h6 mb-2">Tags</h4>
    <p class="small text-muted mb-3">
      Tags on this project are shared with members. Owners and editors can add, edit the name or color, or delete them.
    </p>
    <form v-if="canManage" class="row g-2 mb-3" @submit.prevent="createTag">
      <div class="col-sm-8">
        <input v-model="tagName" type="text" class="form-control form-control-sm" placeholder="New tag" maxlength="50" required />
      </div>
      <div class="col-sm-4">
        <button type="submit" class="btn btn-sm btn-primary w-100">Add tag</button>
      </div>
    </form>
    <p v-if="loading" class="small text-muted mb-0">Loading tags…</p>
    <ul v-else class="list-group">
      <li v-for="tag in tags" :key="tag.id" class="list-group-item">
        <div class="d-flex flex-wrap gap-2 align-items-center">
          <template v-if="renameTagId === tag.id">
            <input v-model="renameTagValue" type="text" class="form-control form-control-sm" maxlength="50" />
            <input v-model="renameTagColor" type="color" class="form-control form-control-sm form-control-color" title="Choose tag color" />
            <button type="button" class="btn btn-sm btn-primary" @click="saveRenameTag">Save</button>
            <button type="button" class="btn btn-sm btn-secondary" @click="renameTagId = null">Cancel</button>
          </template>
          <template v-else>
            <span class="tag-chip" :style="{ backgroundColor: tag.color || '#6c757d' }">{{ tag.name }}</span>
            <span v-if="isProtectedTag(tag)" class="badge text-bg-secondary">System</span>
            <span class="flex-grow-1" />
            <button
              v-if="canManage && !isProtectedTag(tag)"
              type="button"
              class="btn btn-sm btn-outline-secondary"
              @click="beginRenameTag(tag)"
            >
              Edit
            </button>
            <button
              v-if="canManage && !isProtectedTag(tag)"
              type="button"
              class="btn btn-sm btn-outline-danger"
              @click="removeTag(tag)"
            >
              Delete
            </button>
          </template>
        </div>
      </li>
      <li v-if="!tags.length" class="list-group-item text-muted">No tags on this project yet.</li>
    </ul>
  </div>
</template>
