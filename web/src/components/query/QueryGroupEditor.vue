<script setup lang="ts">
import type { QueryField, QueryNode, QueryOptions } from '@/api/types'
import { isGroup, newClause, rowJoin, withJoin } from '@/utils/queryDefinition'
import QueryClauseRow from './QueryClauseRow.vue'

defineOptions({ name: 'QueryGroupEditor' })

const props = defineProps<{
  node: QueryNode
  fields: QueryField[]
  options: QueryOptions
  readonly?: boolean
  depth?: number
  maxDepth?: number
}>()

const emit = defineEmits<{
  'update:node': [node: QueryNode]
  remove: []
}>()

const uid = `qg-${Math.random().toString(36).slice(2, 9)}`
const depth = () => props.depth ?? 1

function clauses(): QueryNode[] {
  return props.node.clauses ?? []
}

/** Emit the group with its rows; every row but the first states its connector. */
function update(next: QueryNode[]) {
  const fixed = next.map((c, i) => withJoin(c, i === 0 ? undefined : (c.join ?? 'and')))
  const out: QueryNode = { op: props.node.op ?? 'and', clauses: fixed }
  if (props.node.join) out.join = props.node.join
  emit('update:node', out)
}

/** Rows with every connector made explicit (older queries rely on the group's op). */
function explicitRows(): QueryNode[] {
  return clauses().map((c, i) => withJoin(c, rowJoin(props.node, i)))
}

function setJoin(i: number, join: 'and' | 'or') {
  const next = explicitRows()
  next[i] = withJoin(next[i], join)
  update(next)
}

function replaceAt(i: number, n: QueryNode) {
  const next = explicitRows()
  // Child editors don't know their connector; keep the one this row had.
  next[i] = withJoin(n, rowJoin(props.node, i))
  update(next)
}

function removeAt(i: number) {
  const next = explicitRows()
  next.splice(i, 1)
  update(next)
}

function firstField() {
  return props.fields.find((f) => f.id === 'title') ?? props.fields[0]
}

function addClause() {
  update([...explicitRows(), { join: 'and', ...newClause(firstField()) }])
}

function addGroup() {
  update([...explicitRows(), { join: 'and', op: 'and', clauses: [newClause(firstField())] }])
}

const hasOr = () => clauses().some((_, i) => rowJoin(props.node, i) === 'or')
</script>

<template>
  <div class="query-group" :class="{ 'query-group-nested': depth() > 1 }">
    <div v-if="depth() > 1" class="d-flex align-items-center mb-2">
      <span class="small text-muted fw-semibold">Group</span>
      <button
        v-if="!readonly"
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
      <li v-for="(child, i) in clauses()" :key="i" class="d-flex align-items-start gap-2">
        <div class="query-join flex-shrink-0">
          <span v-if="i === 0" class="small text-muted d-inline-block pt-1">Where</span>
          <template v-else>
            <label :for="`${uid}-join-${i}`" class="visually-hidden">Connect row {{ i + 1 }} with</label>
            <select
              :id="`${uid}-join-${i}`"
              class="form-select form-select-sm"
              :class="rowJoin(node, i) === 'or' ? 'query-join-or' : ''"
              :value="rowJoin(node, i)"
              :disabled="readonly"
              @change="setJoin(i, ($event.target as HTMLSelectElement).value as 'and' | 'or')"
            >
              <option value="and">And</option>
              <option value="or">Or</option>
            </select>
          </template>
        </div>
        <div class="flex-grow-1 min-w-0">
          <QueryGroupEditor
            v-if="isGroup(child)"
            :node="child"
            :fields="fields"
            :options="options"
            :readonly="readonly"
            :depth="depth() + 1"
            :max-depth="maxDepth"
            @update:node="replaceAt(i, $event)"
            @remove="removeAt(i)"
          />
          <QueryClauseRow
            v-else
            :node="child"
            :fields="fields"
            :options="options"
            :readonly="readonly"
            @update:node="replaceAt(i, $event)"
            @remove="removeAt(i)"
          />
        </div>
      </li>
    </ul>

    <p v-if="hasOr() && clauses().length > 2" class="small text-muted mb-2">
      <i class="bi bi-info-circle" aria-hidden="true" />
      “And” is applied before “Or”: <em>A and B or C</em> means <em>(A and B) or C</em>. Add a group to change the order.
    </p>

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
}
.query-join {
  width: 5rem;
}
.query-join-or {
  font-weight: 600;
  color: var(--bs-warning-text-emphasis, #997404);
}
.min-w-0 {
  min-width: 0;
}
</style>
