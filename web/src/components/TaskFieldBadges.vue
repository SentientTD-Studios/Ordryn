<script setup lang="ts">
import { computed } from 'vue'
import type { CustomFieldDef, Task } from '@/api/types'
import { useCustomFieldDefs } from '@/composables/useCustomFieldDefs'

const props = defineProps<{
  task: Task
  defs?: CustomFieldDef[]
}>()

const { defByKey: loadedByKey } = useCustomFieldDefs(() => props.defs ? null : props.task.project_id)

const byKey = computed(() => {
  if (props.defs?.length) {
    const map: Record<string, CustomFieldDef> = {}
    for (const d of props.defs) map[d.field_key] = d
    return map
  }
  return loadedByKey.value
})

type Badge = {
  key: string
  label: string
  title: string
  href?: string
  color?: string
}

function optionFor(def: CustomFieldDef | undefined, value: unknown) {
  if (!def || def.type !== 'enum') return undefined
  const raw = String(value)
  return (def.options || []).find((o) => o.value === raw)
}

function displayValue(def: CustomFieldDef | undefined, value: unknown): string {
  if (value && typeof value === 'object' && 'user_name' in (value as object)) {
    const u = value as { user_name?: string; id?: number }
    return u.user_name || (u.id != null ? String(u.id) : '')
  }
  const opt = optionFor(def, value)
  if (opt) return opt.label || opt.value
  if (typeof value === 'boolean') return def?.label || 'Yes'
  if (value == null) return ''
  return String(value)
}

function shouldShow(def: CustomFieldDef | undefined, value: unknown): boolean {
  if (value == null || value === '') return false
  if (typeof value === 'boolean') return value
  if (typeof value === 'object' && value && 'id' in (value as object) && !(value as { id?: number }).id) {
    return false
  }
  return displayValue(def, value) !== ''
}

const badges = computed<Badge[]>(() => {
  const fields = props.task.fields || {}
  const out: Badge[] = []
  for (const [key, value] of Object.entries(fields)) {
    const def = byKey.value[key]
    if (!shouldShow(def, value)) continue
    const opt = optionFor(def, value)
    const label = displayValue(def, value)
    const title = def?.label || key
    const href = def?.type === 'url' && typeof value === 'string' ? value : undefined
    out.push({ key, label, title, href, color: opt?.color })
  }
  return out
})
</script>

<template>
  <template v-for="b in badges" :key="b.key">
    <a
      v-if="b.href"
      class="ordryn-badge text-nowrap text-decoration-none"
      :href="b.href"
      target="_blank"
      rel="noopener noreferrer"
      :title="b.title"
      :style="b.color ? { background: b.color, color: '#fff' } : { background: 'var(--ordryn-muted-bg)', color: 'var(--ordryn-muted)' }"
      @click.stop
    >{{ b.label }}</a>
    <span
      v-else
      class="ordryn-badge text-nowrap"
      :title="b.title"
      :style="b.color ? { background: b.color, color: '#fff' } : { background: 'var(--ordryn-muted-bg)', color: 'var(--ordryn-muted)' }"
    >{{ b.label }}</span>
  </template>
</template>
