<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { api } from '@/api/client'
import type { Organization, OrgImportMember, OrgImportMode } from '@/api/types'
import { APIError } from '@/api/types'
import OrgImportFields from '@/components/OrgImportFields.vue'
import { useToast } from '@/composables/useToast'

const props = defineProps<{
  open: boolean
  organizations: Organization[]
}>()

const emit = defineEmits<{
  close: []
  created: []
}>()

const toast = useToast()
const name = ref('')
const description = ref('')
const orgId = ref(0)
const importMode = ref<OrgImportMode>('copy')
const importMembers = ref<OrgImportMember[]>([])
const saving = ref(false)

const manageableOrgs = computed(() => props.organizations.filter((o) => o.can_manage))
const canSubmit = computed(
  () => !!name.value.trim() && !saving.value && !(orgId.value > 0 && importMode.value === 'select' && !importMembers.value.length),
)

watch(
  () => props.open,
  (open) => {
    if (!open) return
    name.value = ''
    description.value = ''
    orgId.value = 0
    importMode.value = 'copy'
    importMembers.value = []
  },
)

async function create() {
  if (!canSubmit.value) return
  saving.value = true
  try {
    await api.createProject(name.value.trim(), description.value.trim(), {
      organization_id: Number(orgId.value) || null,
      org_import: importMode.value,
      org_import_members: importMembers.value,
    })
    toast.push('Project created!', 'success')
    emit('created')
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Failed to create project', 'error')
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <div v-if="open" class="modal fade show d-block" style="background: rgba(0,0,0,0.5);" tabindex="-1">
    <div class="modal-dialog modal-dialog-centered">
      <div class="modal-content border-0 shadow" style="background: var(--ordryn-card-bg); color: var(--ordryn-text);">
        <div class="modal-header border-0 pb-0">
          <h5 class="modal-title fw-bold">Create New Project</h5>
          <button type="button" class="btn-close" @click="emit('close')" />
        </div>
        <div class="modal-body py-3">
          <div class="mb-3">
            <label for="new-project-name" class="form-label small fw-bold">Project Name</label>
            <input
              id="new-project-name"
              v-model="name"
              type="text"
              class="form-control"
              maxlength="50"
              placeholder="e.g., Marketing Campaign"
              @keyup.enter="create"
            />
          </div>
          <div class="mb-3">
            <label for="new-project-description" class="form-label small fw-bold">Description</label>
            <textarea
              id="new-project-description"
              v-model="description"
              class="form-control"
              rows="3"
              maxlength="1000"
              placeholder="Optional details about this project"
            />
          </div>
          <div v-if="manageableOrgs.length" class="mb-3">
            <OrgImportFields
              v-model:organization-id="orgId"
              v-model:import-mode="importMode"
              v-model:members="importMembers"
              :orgs="manageableOrgs"
              select-id="new-project-org"
            />
          </div>
        </div>
        <div class="modal-footer border-0 pt-0 justify-content-end gap-2">
          <button type="button" class="btn btn-sm btn-outline-secondary" @click="emit('close')">Cancel</button>
          <button type="button" class="btn btn-sm btn-success px-3" :disabled="!canSubmit" @click="create">Create Project</button>
        </div>
      </div>
    </div>
  </div>
</template>
