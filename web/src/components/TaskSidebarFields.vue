<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import type { CustomFieldDef, ProjectMember } from '@/api/types'
import { api } from '@/api/client'
import { useCustomFieldDefs } from '@/composables/useCustomFieldDefs'

const props = defineProps<{
  projectId: number | '' | null
  modelValue: Record<string, unknown>
  readOnly?: boolean
  /**
   * Compact rail mode: empty fields hide behind an "Add field" menu and
   * descriptions move into tooltips.
   */
  compact?: boolean
  /** Changing this (e.g. the task id) re-hides fields revealed for the previous task. */
  revealKey?: unknown
}>()

const emit = defineEmits<{
  'update:modelValue': [value: Record<string, unknown>]
}>()

const { defs } = useCustomFieldDefs(() => (typeof props.projectId === 'number' ? props.projectId : null))
const members = ref<ProjectMember[]>([])

watch(
  () => props.projectId,
  async (id) => {
    if (typeof id !== 'number' || id <= 0) {
      members.value = []
      return
    }
    try {
      members.value = await api.listProjectMembers(id)
    } catch {
      members.value = []
    }
  },
  { immediate: true },
)

const visible = computed(() => defs.value.filter((d) => (d.show_on || ['sidebar']).includes('sidebar')))

// Fields revealed by "Add field" or edited stay put, even if cleared again.
const revealed = ref(new Set<string>())
watch(
  () => [props.projectId, props.revealKey],
  () => {
    revealed.value = new Set()
  },
)

function hasValue(def: CustomFieldDef): boolean {
  if (def.type === 'boolean') return boolValue(def)
  if (def.type === 'user') return userValue(def) !== ''
  return stringValue(def).trim() !== ''
}

const shown = computed(() =>
  props.compact ? visible.value.filter((d) => hasValue(d) || revealed.value.has(d.field_key)) : visible.value,
)
const addable = computed(() => (props.compact && !props.readOnly ? visible.value.filter((d) => !shown.value.includes(d)) : []))

async function reveal(def: CustomFieldDef) {
  revealed.value = new Set(revealed.value).add(def.field_key)
  await nextTick()
  document.getElementById(`cf-${def.field_key}`)?.focus()
}

function current(key: string): unknown {
  return props.modelValue[key]
}

function setField(def: CustomFieldDef, value: unknown) {
  if (props.compact && !revealed.value.has(def.field_key)) {
    revealed.value = new Set(revealed.value).add(def.field_key)
  }
  emit('update:modelValue', { ...props.modelValue, [def.field_key]: value })
}

function stringValue(def: CustomFieldDef): string {
  const v = current(def.field_key)
  return v == null ? '' : String(v)
}

function numberValue(def: CustomFieldDef): number | '' {
  const v = current(def.field_key)
  if (v == null || v === '') return ''
  const n = Number(v)
  return Number.isFinite(n) ? n : ''
}

function boolValue(def: CustomFieldDef): boolean {
  return current(def.field_key) === true
}

function userValue(def: CustomFieldDef): number | '' {
  const v = current(def.field_key)
  if (v == null || v === '') return ''
  if (typeof v === 'number') return v
  if (typeof v === 'object' && v && 'id' in v) {
    const id = Number((v as { id?: number }).id)
    return Number.isFinite(id) && id > 0 ? id : ''
  }
  const n = Number(v)
  return Number.isFinite(n) && n > 0 ? n : ''
}

function memberLabel(m: ProjectMember): string {
  return m.user_name || m.email || String(m.user_id)
}
</script>

<template>
  <div v-if="shown.length || addable.length" class="task-sidebar-fields" :class="{ 'is-compact': compact }">
    <div v-for="def in shown" :key="def.field_key" class="form-group mt-2" :class="`cf-type-${def.type}`">
      <label
        v-if="def.type !== 'boolean'"
        class="form-label"
        :for="`cf-${def.field_key}`"
        :title="compact ? def.description || undefined : undefined"
      >{{ def.label }}</label>

      <select
        v-if="def.type === 'enum'"
        :id="`cf-${def.field_key}`"
        class="form-select"
        :disabled="readOnly"
        :value="stringValue(def)"
        @change="setField(def, ($event.target as HTMLSelectElement).value || null)"
      >
        <option value="">None</option>
        <option v-for="opt in def.options || []" :key="opt.value" :value="opt.value">
          {{ opt.label || opt.value }}
        </option>
      </select>

      <input
        v-else-if="def.type === 'number'"
        :id="`cf-${def.field_key}`"
        type="number"
        class="form-control"
        :disabled="readOnly"
        :readonly="readOnly"
        :value="numberValue(def)"
        @input="setField(def, ($event.target as HTMLInputElement).value === '' ? null : Number(($event.target as HTMLInputElement).value))"
      />

      <input
        v-else-if="def.type === 'url'"
        :id="`cf-${def.field_key}`"
        type="url"
        class="form-control"
        :disabled="readOnly"
        :readonly="readOnly"
        :value="stringValue(def)"
        placeholder="https://"
        @input="setField(def, ($event.target as HTMLInputElement).value)"
      />

      <select
        v-else-if="def.type === 'user'"
        :id="`cf-${def.field_key}`"
        class="form-select"
        :disabled="readOnly"
        :value="userValue(def)"
        @change="setField(def, ($event.target as HTMLSelectElement).value ? Number(($event.target as HTMLSelectElement).value) : null)"
      >
        <option value="">None</option>
        <option v-for="m in members" :key="m.user_id" :value="m.user_id">{{ memberLabel(m) }}</option>
      </select>

      <input
        v-else-if="def.type === 'date'"
        :id="`cf-${def.field_key}`"
        type="date"
        class="form-control"
        :disabled="readOnly"
        :readonly="readOnly"
        :value="stringValue(def)"
        @input="setField(def, ($event.target as HTMLInputElement).value || null)"
      />

      <textarea
        v-else-if="def.type === 'markdown'"
        :id="`cf-${def.field_key}`"
        class="form-control"
        :rows="compact ? 3 : 4"
        :disabled="readOnly"
        :readonly="readOnly"
        :value="stringValue(def)"
        @input="setField(def, ($event.target as HTMLTextAreaElement).value)"
      />

      <div v-else-if="def.type === 'boolean'" class="form-check">
        <input
          :id="`cf-${def.field_key}`"
          class="form-check-input"
          type="checkbox"
          :disabled="readOnly"
          :checked="boolValue(def)"
          @change="setField(def, ($event.target as HTMLInputElement).checked)"
        />
        <label
          class="form-check-label"
          :for="`cf-${def.field_key}`"
          :title="compact ? def.description || undefined : undefined"
        >{{ def.label }}</label>
      </div>

      <input
        v-else
        :id="`cf-${def.field_key}`"
        type="text"
        class="form-control"
        :disabled="readOnly"
        :readonly="readOnly"
        :value="stringValue(def)"
        @input="setField(def, ($event.target as HTMLInputElement).value)"
      />

      <div v-if="def.description" :class="compact ? 'visually-hidden' : 'form-text'">{{ def.description }}</div>
    </div>
    <div v-if="addable.length" class="dropdown task-sidebar-fields-add">
      <button
        type="button"
        class="btn btn-sm btn-link text-decoration-none p-0"
        data-bs-toggle="dropdown"
        aria-expanded="false"
      >
        <i class="bi bi-plus-lg me-1" />Add field
      </button>
      <ul class="dropdown-menu shadow-sm">
        <li v-for="def in addable" :key="def.field_key">
          <button type="button" class="dropdown-item small" :title="def.description || undefined" @click="reveal(def)">
            {{ def.label }}
          </button>
        </li>
      </ul>
    </div>
  </div>
</template>
