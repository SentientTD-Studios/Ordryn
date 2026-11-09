<script setup lang="ts">
import { computed } from 'vue'
import type { QueryField, QueryNode, QueryOperator, QueryOptions, QueryValue } from '@/api/types'
import {
  PRIORITY_LABELS,
  fixClause,
  isListOperator,
  isRangeOperator,
  isValidDateValue,
  newClause,
  operatorLabel,
  parseNumberList,
  takesValue,
} from '@/utils/queryDefinition'

const props = defineProps<{
  node: QueryNode
  fields: QueryField[]
  options: QueryOptions
  readonly?: boolean
}>()

const emit = defineEmits<{
  'update:node': [node: QueryNode]
  remove: []
}>()

const uid = `qc-${Math.random().toString(36).slice(2, 9)}`

const field = computed(() => props.fields.find((f) => f.id === props.node.field))
const type = computed(() => field.value?.type)
const listOp = computed(() => isListOperator(props.node.operator))
const fieldMissing = computed(() => !!props.node.field && !field.value)


function setField(id: string) {
  emit('update:node', newClause(props.fields.find((f) => f.id === id)))
}

function setOperator(op: QueryOperator) {
  emit('update:node', fixClause({ ...props.node, operator: op }, field.value))
}

function setValue(value: QueryValue) {
  emit('update:node', { ...props.node, value })
}

/** Choices for list-style fields: value plus label. */
type Choice = { value: string | number; label: string; group?: string }

const projectNames = computed(() => new Map(props.options.projects.map((p) => [p.id, p.name])))

const choices = computed<Choice[] | null>(() => {
  switch (type.value) {
    case 'project':
      return props.options.projects.map((p) => ({ value: p.id, label: p.archived ? `${p.name} (archived)` : p.name }))
    case 'user':
      return [{ value: '@me', label: 'Me' }, ...props.options.people.map((p) => ({ value: p.id, label: p.name }))]
    case 'sprint':
      return [
        { value: '@current_sprint', label: 'Current sprint' },
        ...props.options.sprints.map((s) => ({
          value: s.id,
          label: s.active ? `${s.name} (active)` : s.name,
          group: projectNames.value.get(s.project_id) ?? '',
        })),
      ]
    case 'status':
      return props.options.statuses.map((s) => ({ value: s.toLowerCase(), label: s }))
    case 'tags':
      return props.options.tags.map((s) => ({ value: s.toLowerCase(), label: s }))
    case 'enum':
      return (field.value?.options ?? []).map((o) => ({ value: o.value, label: o.label }))
  }
  if (field.value?.id === 'priority') return PRIORITY_LABELS.map((label, i) => ({ value: i, label }))
  return null
})

/** Selected values that are not offered any more (deleted, or access lost). */
const missingChoices = computed<Choice[]>(() => {
  if (!choices.value) return []
  const vals = Array.isArray(props.node.value) ? props.node.value : props.node.value == null ? [] : [props.node.value]
  const known = new Set(choices.value.map((c) => String(c.value)))
  return vals
    .filter((v) => v !== '' && !known.has(String(v)))
    .map((v) => ({ value: v as string | number, label: typeof v === 'number' ? 'Unavailable' : `${v} (not found)` }))
})

const allChoices = computed(() => [...(choices.value ?? []), ...missingChoices.value])

const choiceGroups = computed(() => {
  const groups = new Map<string, Choice[]>()
  for (const c of allChoices.value) {
    const g = c.group ?? ''
    if (!groups.has(g)) groups.set(g, [])
    groups.get(g)!.push(c)
  }
  return [...groups.entries()]
})

function decodeChoice(raw: string): string | number {
  const match = allChoices.value.find((c) => String(c.value) === raw)
  return match ? match.value : raw
}

function onSingleChoice(e: Event) {
  setValue(decodeChoice((e.target as HTMLSelectElement).value))
}

function onMultiChoice(e: Event) {
  const sel = e.target as HTMLSelectElement
  setValue(Array.from(sel.selectedOptions).map((o) => decodeChoice(o.value)))
}

function isSelected(v: string | number): boolean {
  const cur = props.node.value
  if (Array.isArray(cur)) return cur.some((x) => String(x) === String(v))
  return String(cur) === String(v)
}

const textValue = computed(() => (typeof props.node.value === 'string' ? props.node.value : ''))
const numberListText = computed(() => (Array.isArray(props.node.value) ? props.node.value.join(', ') : ''))
const dateInvalid = computed(() => type.value === 'date' && textValue.value !== '' && !isValidDateValue(textValue.value))

const rangeOp = computed(() => isRangeOperator(props.node.operator))
const rangeValue = computed<[string, string]>(() => {
  const v = props.node.value
  return Array.isArray(v) ? [String(v[0] ?? ''), String(v[1] ?? '')] : ['', '']
})

function setRangeEnd(i: 0 | 1, s: string) {
  const next: [string, string] = [...rangeValue.value]
  next[i] = s.trim()
  setValue(next)
}

const rangeInvalid = (s: string) => s !== '' && !isValidDateValue(s)
</script>

<template>
  <div class="query-clause d-flex flex-wrap align-items-start gap-2">
    <label :for="`${uid}-field`" class="visually-hidden">Field</label>
    <select
      :id="`${uid}-field`"
      class="form-select form-select-sm query-field"
      :value="node.field"
      :disabled="readonly"
      @change="setField(($event.target as HTMLSelectElement).value)"
    >
      <option v-if="fieldMissing" :value="node.field">{{ node.field }} (unavailable)</option>
      <option v-for="f in fields" :key="f.id" :value="f.id">{{ f.label }}</option>
    </select>

    <label :for="`${uid}-op`" class="visually-hidden">Operator</label>
    <select
      :id="`${uid}-op`"
      class="form-select form-select-sm query-op"
      :value="node.operator"
      :disabled="readonly || fieldMissing"
      @change="setOperator(($event.target as HTMLSelectElement).value as QueryOperator)"
    >
      <option v-for="op in field?.operators ?? [node.operator]" :key="op" :value="op">
        {{ operatorLabel(op as QueryOperator, type) }}
      </option>
    </select>

    <div v-if="takesValue(node.operator) && !fieldMissing" class="query-value flex-grow-1">
      <label :for="`${uid}-val`" class="visually-hidden">Value</label>

      <!-- Fixed choices: projects, people, sprints, statuses, tags, enum options, priority. -->
      <template v-if="choices">
        <select
          v-if="listOp"
          :id="`${uid}-val`"
          class="form-select form-select-sm"
          multiple
          :size="Math.min(5, Math.max(3, allChoices.length))"
          :disabled="readonly"
          @change="onMultiChoice"
        >
          <template v-for="[group, items] in choiceGroups" :key="group">
            <optgroup v-if="group" :label="group">
              <option v-for="c in items" :key="String(c.value)" :value="String(c.value)" :selected="isSelected(c.value)">
                {{ c.label }}
              </option>
            </optgroup>
            <template v-else>
              <option v-for="c in items" :key="String(c.value)" :value="String(c.value)" :selected="isSelected(c.value)">
                {{ c.label }}
              </option>
            </template>
          </template>
        </select>
        <select
          v-else
          :id="`${uid}-val`"
          class="form-select form-select-sm"
          :disabled="readonly"
          @change="onSingleChoice"
        >
          <option value="" disabled :selected="node.value == null || node.value === ''">Choose…</option>
          <template v-for="[group, items] in choiceGroups" :key="group">
            <optgroup v-if="group" :label="group">
              <option v-for="c in items" :key="String(c.value)" :value="String(c.value)" :selected="isSelected(c.value)">
                {{ c.label }}
              </option>
            </optgroup>
            <template v-else>
              <option v-for="c in items" :key="String(c.value)" :value="String(c.value)" :selected="isSelected(c.value)">
                {{ c.label }}
              </option>
            </template>
          </template>
        </select>
        <div v-if="listOp && !readonly" class="form-text small mt-0">Ctrl/⌘-click to pick several.</div>
      </template>

      <select
        v-else-if="type === 'boolean'"
        :id="`${uid}-val`"
        class="form-select form-select-sm"
        :value="String(node.value)"
        :disabled="readonly"
        @change="setValue(($event.target as HTMLSelectElement).value === 'true')"
      >
        <option value="true">Yes</option>
        <option value="false">No</option>
      </select>

      <template v-else-if="type === 'date' && rangeOp">
        <div class="d-flex flex-wrap align-items-center gap-2">
          <label :for="`${uid}-from`" class="visually-hidden">From</label>
          <input
            :id="`${uid}-from`"
            type="text"
            class="form-control form-control-sm query-date"
            :class="{ 'is-invalid': rangeInvalid(rangeValue[0]) }"
            :value="rangeValue[0]"
            :list="`${uid}-macros`"
            placeholder="From: YYYY-MM-DD or @start_of_week"
            :disabled="readonly"
            @input="setRangeEnd(0, ($event.target as HTMLInputElement).value)"
          />
          <span class="small text-muted">and</span>
          <label :for="`${uid}-to`" class="visually-hidden">To</label>
          <input
            :id="`${uid}-to`"
            type="text"
            class="form-control form-control-sm query-date"
            :class="{ 'is-invalid': rangeInvalid(rangeValue[1]) }"
            :value="rangeValue[1]"
            :list="`${uid}-macros`"
            placeholder="To: YYYY-MM-DD or @end_of_week"
            :disabled="readonly"
            @input="setRangeEnd(1, ($event.target as HTMLInputElement).value)"
          />
        </div>
        <datalist :id="`${uid}-macros`">
          <option v-for="m in options.date_macros" :key="m" :value="m" />
        </datalist>
        <div class="form-text small mt-0">Both dates are included.</div>
      </template>

      <template v-else-if="type === 'date'">
        <input
          :id="`${uid}-val`"
          type="text"
          class="form-control form-control-sm"
          :class="{ 'is-invalid': dateInvalid }"
          :value="textValue"
          :list="`${uid}-macros`"
          placeholder="YYYY-MM-DD or @today-7"
          :disabled="readonly"
          :aria-describedby="`${uid}-date-help`"
          @input="setValue(($event.target as HTMLInputElement).value.trim())"
        />
        <datalist :id="`${uid}-macros`">
          <option v-for="m in options.date_macros" :key="m" :value="m" />
        </datalist>
        <div :id="`${uid}-date-help`" class="invalid-feedback">Use a date like 2026-01-31 or a macro like @today-7.</div>
      </template>

      <input
        v-else-if="type === 'number' && listOp"
        :id="`${uid}-val`"
        type="text"
        inputmode="numeric"
        class="form-control form-control-sm"
        :value="numberListText"
        placeholder="e.g. 3, 5, 8"
        :disabled="readonly"
        @change="setValue(parseNumberList(($event.target as HTMLInputElement).value))"
      />
      <input
        v-else-if="type === 'number'"
        :id="`${uid}-val`"
        type="number"
        class="form-control form-control-sm"
        :value="node.value as number"
        :disabled="readonly"
        @input="setValue(Number(($event.target as HTMLInputElement).value))"
      />

      <input
        v-else
        :id="`${uid}-val`"
        type="text"
        class="form-control form-control-sm"
        :value="textValue"
        maxlength="200"
        :disabled="readonly"
        @input="setValue(($event.target as HTMLInputElement).value)"
      />
    </div>
    <div v-else-if="fieldMissing" class="small text-warning align-self-center flex-grow-1">
      This field is no longer available and matches nothing.
    </div>
    <div v-else class="flex-grow-1" />

    <button
      v-if="!readonly"
      type="button"
      class="btn btn-sm btn-outline-secondary border-0"
      title="Remove clause"
      aria-label="Remove clause"
      @click="emit('remove')"
    >
      <i class="bi bi-x-lg" />
    </button>
  </div>
</template>

<style scoped>
.query-field {
  width: 12rem;
  max-width: 100%;
}
.query-op {
  width: 10rem;
  max-width: 100%;
}
.query-value {
  min-width: 12rem;
}
.query-date {
  width: 11rem;
  max-width: 100%;
}
@media (max-width: 576px) {
  .query-field,
  .query-op,
  .query-value {
    width: 100%;
    min-width: 0;
  }
}
</style>
