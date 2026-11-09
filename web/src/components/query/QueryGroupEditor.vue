<script setup lang="ts">
import type { QueryField, QueryNode, QueryOptions } from '@/api/types'
import { isGroup, newClause } from '@/utils/queryDefinition'
import QueryClauseRow from './QueryClauseRow.vue'

defineOptions({ name: 'QueryGroupEditor' })

const props = defineProps<{
  node: QueryNode
  fields: QueryField[]
  options: QueryOptions
  readonly?: boolean
  depth?: number
  maxDepth?: number
  hideProjectField?: boolean
}>()

const emit = defineEmits<{
  'update:node': [node: QueryNode]
  remove: []
}>()

const depth = () => props.depth ?? 1

function clauses(): QueryNode[] {
  return props.node.clauses ?? []
}

function update(clauses: QueryNode[]) {
  emit('update:node', { op: props.node.op ?? 'and', clauses })
}

function setOp(op: 'and' | 'or') {
  emit('update:node', { op, clauses: clauses() })
}

function replaceAt(i: number, n: QueryNode) {
  const next = [...clauses()]
  next[i] = n
  update(next)
}

function removeAt(i: number) {
  const next = [...clauses()]
  next.splice(i, 1)
  update(next)
}

function addClause() {
  const firstField = props.fields.find((f) => f.id === 'title') ?? props.fields[0]
  update([...clauses(), newClause(firstField)])
}

function addGroup() {
  const firstField = props.fields.find((f) => f.id === 'title') ?? props.fields[0]
  // A new group starts with the opposite join so it is useful straight away.
  update([...clauses(), { op: props.node.op === 'or' ? 'and' : 'or', clauses: [newClause(firstField)] }])
}
</script>

<template>
  <div class="query-group" :class="{ 'query-group-nested': depth() > 1 }">
    <div class="d-flex align-items-center gap-2 mb-2 flex-wrap">
      <div class="btn-group btn-group-sm" role="group" :aria-label="'Join clauses with'">
        <button
          type="button"
          class="btn"
          :class="node.op !== 'or' ? 'btn-primary' : 'btn-outline-primary'"
          :disabled="readonly"
          :aria-pressed="node.op !== 'or'"
          @click="setOp('and')"
        >
          All (AND)
        </button>
        <button
          type="button"
          class="btn"
          :class="node.op === 'or' ? 'btn-primary' : 'btn-outline-primary'"
          :disabled="readonly"
          :aria-pressed="node.op === 'or'"
          @click="setOp('or')"
        >
          Any (OR)
        </button>
      </div>
      <span class="small text-muted">
        {{ node.op === 'or' ? 'Match tasks where any of these is true' : 'Match tasks where all of these are true' }}
      </span>
      <button
        v-if="depth() > 1 && !readonly"
        type="button"
        class="btn btn-sm btn-outline-secondary border-0 ms-auto"
        title="Remove group"
        aria-label="Remove group"
        @click="emit('remove')"
      >
        <i class="bi bi-x-lg" />
      </button>
    </div>

    <p v-if="clauses().length === 0" class="small text-muted mb-2">
      No clauses yet: every task in scope matches.
    </p>

    <ul class="list-unstyled mb-2 d-flex flex-column gap-2">
      <li v-for="(child, i) in clauses()" :key="i">
        <QueryGroupEditor
          v-if="isGroup(child)"
          :node="child"
          :fields="fields"
          :options="options"
          :readonly="readonly"
          :depth="depth() + 1"
          :max-depth="maxDepth"
          :hide-project-field="hideProjectField"
          @update:node="replaceAt(i, $event)"
          @remove="removeAt(i)"
        />
        <QueryClauseRow
          v-else
          :node="child"
          :fields="fields"
          :options="options"
          :readonly="readonly"
          :hide-project-field="hideProjectField"
          @update:node="replaceAt(i, $event)"
          @remove="removeAt(i)"
        />
      </li>
    </ul>

    <div v-if="!readonly" class="d-flex gap-2">
      <button type="button" class="btn btn-sm btn-outline-primary" @click="addClause">
        <i class="bi bi-plus-lg" /> Clause
      </button>
      <button
        v-if="depth() < (maxDepth ?? 5)"
        type="button"
        class="btn btn-sm btn-outline-secondary"
        @click="addGroup"
      >
        <i class="bi bi-diagram-2" /> Group
      </button>
    </div>
  </div>
</template>

<style scoped>
.query-group-nested {
  border-left: 3px solid var(--bs-primary-border-subtle, #9ec5fe);
  padding-left: 0.75rem;
  margin-left: 0.25rem;
}
</style>
