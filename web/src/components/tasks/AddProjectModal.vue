<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { api } from '@/api/client'
import type { Organization, OrgImportMember, OrgImportMode, ProjectTemplate } from '@/api/types'
import { APIError } from '@/api/types'
import OrgImportFields from '@/components/OrgImportFields.vue'
import { useToast } from '@/composables/useToast'
import { useSite } from '@/composables/useSite'

const props = defineProps<{
  open: boolean
  organizations: Organization[]
}>()

const emit = defineEmits<{
  close: []
  created: []
}>()

const toast = useToast()
// Company projects are shared with teams after they are created.
const { isCompanyMode } = useSite()
const name = ref('')
const description = ref('')
const orgId = ref(0)
const importMode = ref<OrgImportMode>('copy')
const importMembers = ref<OrgImportMember[]>([])
const saving = ref(false)

// Starting from a template copies its columns, roles, tags, rules, and optionally its open tasks.
const templates = ref<ProjectTemplate[]>([])
const templateId = ref(0)
const includeTasks = ref(true)
const chosenTemplate = computed(() => templates.value.find((t) => t.id === templateId.value) || null)

async function loadTemplates() {
  try {
    templates.value = (await api.listProjectTemplates()).templates
  } catch {
    templates.value = []
  }
}

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
    templateId.value = 0
    includeTasks.value = true
    void loadTemplates()
  },
)

async function create() {
  if (!canSubmit.value) return
  saving.value = true
  try {
    if (chosenTemplate.value) {
      const res = await api.createProjectFromTemplate(chosenTemplate.value.id, {
        name: name.value.trim(),
        ...(description.value.trim() ? { description: description.value.trim() } : {}),
        include_tasks: includeTasks.value,
      })
      const notes = [`Project created from ${chosenTemplate.value.name}`]
      if (res.tasks_copied) notes.push(`${res.tasks_copied} task(s) copied`)
      if (res.rules_skipped) notes.push(`${res.rules_skipped} rule(s) skipped: they name a specific person, sprint, or agent`)
      toast.push(notes.join(' · '), res.warnings.length ? 'info' : 'success')
      emit('created')
      return
    }
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
          <div v-if="templates.length" class="mb-3">
            <label for="new-project-template" class="form-label small fw-bold">Start from</label>
            <select id="new-project-template" v-model.number="templateId" class="form-select">
              <option :value="0">Blank project</option>
              <option v-for="t in templates" :key="t.id" :value="t.id">Template: {{ t.name }}</option>
            </select>
            <template v-if="chosenTemplate">
              <div class="form-text">
                Copies its board columns, roles, tags, sprint settings, and automation rules{{ chosenTemplate.description ? `. ${chosenTemplate.description}` : '' }}
              </div>
              <div v-if="chosenTemplate.open_tasks" class="form-check mt-2">
                <input id="new-project-include-tasks" v-model="includeTasks" type="checkbox" class="form-check-input" />
                <label class="form-check-label small" for="new-project-include-tasks">
                  Copy its {{ chosenTemplate.open_tasks }} open task{{ chosenTemplate.open_tasks === 1 ? '' : 's' }}
                </label>
              </div>
            </template>
          </div>
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
          <div v-if="manageableOrgs.length && !isCompanyMode && !chosenTemplate" class="mb-3">
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
