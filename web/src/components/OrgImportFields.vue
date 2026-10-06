<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { api } from '@/api/client'
import type { Organization, OrganizationMember, ProjectRoleDef } from '@/api/types'

export type OrgImportMode = 'copy' | 'lock' | 'select'
export type OrgImportMemberPayload = { user_id: number; role: string }

withDefaults(
  defineProps<{
    orgs: Organization[]
    selectId?: string
    noneLabel?: string
  }>(),
  {
    selectId: 'org-import-org',
    noneLabel: 'None — manage members on this project',
  },
)

const organizationId = defineModel<number>('organizationId', { default: 0 })
const importMode = defineModel<OrgImportMode>('importMode', { default: 'copy' })
const membersOut = defineModel<OrgImportMemberPayload[]>('members', { default: () => [] })

const members = ref<OrganizationMember[]>([])
const roles = ref<ProjectRoleDef[]>([])
const selected = ref<Record<number, { on: boolean; role: string }>>({})
const loading = ref(false)

const assignableRoles = computed(() => roles.value.filter((r) => r.slug !== 'owner'))
const importableMembers = computed(() => members.value.filter((m) => m.role !== 'owner'))

watch(
  organizationId,
  async (id) => {
    selected.value = {}
    members.value = []
    roles.value = []
    membersOut.value = []
    if (!id) {
      importMode.value = 'copy'
      return
    }
    loading.value = true
    try {
      const [m, roleData] = await Promise.all([
        api.listOrganizationMembers(id),
        api.listOrganizationRoles(id),
      ])
      members.value = m
      roles.value = roleData.roles || []
      // Members start with their organization role. There is no fallback role: anyone without
      // one must be given a role before they are imported.
      const next: Record<number, { on: boolean; role: string }> = {}
      for (const mem of m) {
        if (mem.role === 'owner') continue
        next[mem.user_id] = { on: false, role: mem.role || '' }
      }
      selected.value = next
    } finally {
      loading.value = false
    }
  },
)

watch(
  [importMode, selected],
  () => {
    if (importMode.value !== 'select') {
      membersOut.value = []
      return
    }
    membersOut.value = Object.entries(selected.value)
      .filter(([, v]) => v.on && v.role)
      .map(([id, v]) => ({ user_id: Number(id), role: v.role }))
  },
  { deep: true },
)

const selectValid = computed(() => {
  if (!organizationId.value || importMode.value !== 'select') return true
  return membersOut.value.length > 0 && membersOut.value.every((m) => !!m.role)
})

defineExpose({ selectValid })
</script>

<template>
  <div class="org-import-fields">
    <label class="form-label small fw-bold" :for="selectId">Organization</label>
    <select :id="selectId" v-model.number="organizationId" class="form-select form-select-sm">
      <option :value="0">{{ noneLabel }}</option>
      <option v-for="o in orgs" :key="o.id" :value="o.id">
        Import members from {{ o.name }}
      </option>
    </select>

    <div v-if="organizationId" class="mt-3">
      <p class="small fw-bold mb-2">How should members be imported?</p>
      <div class="form-check">
        <input
          :id="`${selectId}-copy`"
          v-model="importMode"
          class="form-check-input"
          type="radio"
          value="copy"
        />
        <label class="form-check-label small" :for="`${selectId}-copy`">
          Import all members, keep roles editable on this project
        </label>
      </div>
      <div class="form-check">
        <input
          :id="`${selectId}-lock`"
          v-model="importMode"
          class="form-check-input"
          type="radio"
          value="lock"
        />
        <label class="form-check-label small" :for="`${selectId}-lock`">
          Import all members and lock roles to the organization
        </label>
      </div>
      <div class="form-check">
        <input
          :id="`${selectId}-select`"
          v-model="importMode"
          class="form-check-input"
          type="radio"
          value="select"
        />
        <label class="form-check-label small" :for="`${selectId}-select`">
          Import only selected members (a role is required for each)
        </label>
      </div>
      <p v-if="importMode === 'lock'" class="small text-muted mt-2 mb-0">
        Organization role changes will update this board. Sharing and project roles cannot be edited here.
      </p>
      <p v-else-if="importMode === 'copy'" class="small text-muted mt-2 mb-0">
        Later organization role changes will not overwrite this board.
      </p>
    </div>

    <div v-if="organizationId && importMode === 'select'" class="mt-3">
      <p v-if="loading" class="small text-muted mb-0">Loading members…</p>
      <p v-else-if="!importableMembers.length" class="small text-muted mb-0">
        This organization has no other members to import yet.
      </p>
      <ul v-else class="list-unstyled mb-0">
        <li
          v-for="m in importableMembers"
          :key="m.user_id"
          class="d-flex flex-wrap align-items-center gap-2 mb-2"
        >
          <template v-if="selected[m.user_id]">
          <div class="form-check mb-0">
            <input
              :id="`${selectId}-member-${m.user_id}`"
              v-model="selected[m.user_id].on"
              class="form-check-input"
              type="checkbox"
            />
            <label class="form-check-label small" :for="`${selectId}-member-${m.user_id}`">
              {{ m.user_name || m.email }}
            </label>
          </div>
          <select
            v-model="selected[m.user_id].role"
            class="form-select form-select-sm w-auto"
            :required="selected[m.user_id].on"
            :disabled="!selected[m.user_id].on"
          >
            <option value="" disabled>Select a role</option>
            <option v-for="r in assignableRoles" :key="r.slug" :value="r.slug">{{ r.name }}</option>
          </select>
          </template>
        </li>
      </ul>
      <p v-if="!selectValid" class="small text-danger mt-2 mb-0">
        Select at least one member and choose a role for each.
      </p>
    </div>
  </div>
</template>
