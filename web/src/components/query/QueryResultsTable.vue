<script setup lang="ts">
import { computed, ref } from 'vue'
import { RouterLink } from 'vue-router'
import type { QueryOptions, QueryResults, QuerySortKey, QueryTask } from '@/api/types'
import { PRIORITY_LABELS } from '@/utils/queryDefinition'

const props = defineProps<{
  results: QueryResults
  options: QueryOptions | null
  sort: QuerySortKey[]
  loading?: boolean
}>()

const emit = defineEmits<{
  page: [page: number]
  sort: [sort: QuerySortKey[]]
}>()

const collapsed = ref(new Set<number>())

const columnLabels = computed(() => {
  const m = new Map<string, string>()
  for (const c of props.options?.columns ?? []) m.set(c.value, c.label)
  return m
})

const sortable = computed(() => new Set((props.options?.fields ?? []).filter((f) => f.sortable).map((f) => f.id)))

function label(col: string): string {
  return columnLabels.value.get(col) ?? (col.startsWith('cf:') ? col.slice(3) : col)
}

type Row = { task: QueryTask; depth: number; hasChildren: boolean }

const rows = computed<Row[]>(() => {
  const out: Row[] = []
  const walk = (list: QueryTask[], depth: number) => {
    for (const t of list) {
      const kids = t.children ?? []
      out.push({ task: t, depth, hasChildren: kids.length > 0 })
      if (kids.length && !collapsed.value.has(t.id)) walk(kids, depth + 1)
    }
  }
  walk(props.results.tasks, 0)
  return out
})

function toggle(id: number) {
  const next = new Set(collapsed.value)
  if (next.has(id)) next.delete(id)
  else next.add(id)
  collapsed.value = next
}

function sortDir(col: string): 'asc' | 'desc' | null {
  const k = props.sort[0]
  return k && k.field === col ? k.dir : null
}

function clickHeader(col: string) {
  if (!sortable.value.has(col)) return
  const dir = sortDir(col) === 'asc' ? 'desc' : 'asc'
  // The clicked column leads; earlier keys follow so ties stay stable.
  const rest = props.sort.filter((k) => k.field !== col).slice(0, 2)
  emit('sort', [{ field: col, dir }, ...rest])
}

function ariaSort(col: string): 'ascending' | 'descending' | 'none' | undefined {
  if (!sortable.value.has(col)) return undefined
  const d = sortDir(col)
  return d === 'asc' ? 'ascending' : d === 'desc' ? 'descending' : 'none'
}

function customValue(t: QueryTask, key: string): string {
  const v = t.fields?.[key]
  if (v === null || v === undefined) return ''
  if (typeof v === 'object' && v && 'user_name' in v) return String((v as { user_name: string }).user_name ?? '')
  if (typeof v === 'boolean') return v ? 'Yes' : 'No'
  return String(v)
}

function shortDate(s?: string): string {
  if (!s) return ''
  // "2026/11/09 11:58 AM" -> "2026-11-09"
  return s.slice(0, 10).replace(/\//g, '-')
}

function cell(t: QueryTask, col: string): string {
  switch (col) {
    case 'project':
      return t.project ?? ''
    case 'workflow_status':
      return t.status_name ?? ''
    case 'priority':
      return PRIORITY_LABELS[t.priority] ?? String(t.priority)
    case 'due_date':
      return t.due_date ?? ''
    case 'created_date':
      return shortDate(t.created_at)
    case 'modified_date':
      return shortDate(t.modified_at)
    case 'completed_date':
      return t.completed_date ?? ''
    case 'claimed_by':
      return t.claimed_by_name ?? ''
    case 'created_by':
      return t.created_by_name ?? ''
    case 'sprint':
      return t.sprint_name ?? ''
    case 'estimate':
      return t.estimate_points != null ? String(t.estimate_points) : ''
    case 'parent':
      return t.parent_title ?? ''
    case 'description':
      return (t.description ?? '').replace(/\s+/g, ' ').slice(0, 140)
    case 'is_subtask':
      return t.parent_id ? 'Yes' : 'No'
    case 'has_subtasks':
      return t.child_count ? `${t.children_completed ?? 0}/${t.child_count}` : ''
  }
  if (col.startsWith('cf:')) return customValue(t, col.slice(3))
  return ''
}

const pages = computed(() => Math.max(1, props.results.total_pages || 1))
</script>

<template>
  <div>
    <div class="table-responsive">
      <table class="table table-sm table-hover align-middle mb-2 query-results" :aria-busy="loading ? 'true' : 'false'">
        <thead>
          <tr>
            <th
              v-for="col in results.columns"
              :key="col"
              scope="col"
              :aria-sort="ariaSort(col)"
              class="text-nowrap"
            >
              <button
                v-if="sortable.has(col)"
                type="button"
                class="btn btn-link btn-sm p-0 text-reset text-decoration-none fw-semibold"
                @click="clickHeader(col)"
              >
                {{ label(col) }}
                <i
                  v-if="sortDir(col)"
                  :class="sortDir(col) === 'asc' ? 'bi bi-caret-up-fill' : 'bi bi-caret-down-fill'"
                  aria-hidden="true"
                />
              </button>
              <span v-else>{{ label(col) }}</span>
            </th>
          </tr>
        </thead>
        <tbody>
          <tr v-if="rows.length === 0">
            <td :colspan="results.columns.length" class="text-center text-muted py-4">No tasks match this query.</td>
          </tr>
          <tr v-for="row in rows" :key="row.task.id" :class="{ 'text-muted': row.task.completed }">
            <td v-for="(col, ci) in results.columns" :key="col">
              <template v-if="col === 'title'">
                <span class="d-inline-flex align-items-center" :style="{ paddingLeft: `${row.depth * 1.25}rem` }">
                  <button
                    v-if="row.hasChildren"
                    type="button"
                    class="btn btn-sm btn-link p-0 me-1 text-reset"
                    :aria-expanded="!collapsed.has(row.task.id)"
                    :aria-label="collapsed.has(row.task.id) ? 'Expand subtasks' : 'Collapse subtasks'"
                    @click="toggle(row.task.id)"
                  >
                    <i :class="collapsed.has(row.task.id) ? 'bi bi-chevron-right' : 'bi bi-chevron-down'" />
                  </button>
                  <span v-else-if="row.depth > 0" class="me-1 text-muted" aria-hidden="true">↳</span>
                  <RouterLink :to="`/tasks/${row.task.id}`" :class="{ 'text-decoration-line-through': row.task.completed }">
                    {{ row.task.title }}
                  </RouterLink>
                </span>
                <div v-if="row.depth === 0 && row.task.parent_title && !results.columns.includes('parent')" class="small text-muted">
                  in {{ row.task.parent_title }}
                </div>
              </template>
              <RouterLink v-else-if="col === 'id'" :to="`/tasks/${row.task.id}`" class="text-nowrap">#{{ row.task.id }}</RouterLink>
              <template v-else-if="col === 'tags'">
                <span
                  v-for="tag in row.task.tags"
                  :key="tag.id"
                  class="badge me-1"
                  :style="{ backgroundColor: tag.color || '#6c757d' }"
                >{{ tag.name }}</span>
              </template>
              <template v-else-if="col === 'completed'">
                <i v-if="row.task.completed" class="bi bi-check-circle-fill text-success" aria-label="Completed" />
                <span v-else class="visually-hidden">Open</span>
              </template>
              <template v-else-if="col === 'favorite'">
                <i v-if="row.task.is_favorite" class="bi bi-star-fill text-warning" aria-label="Favorite" />
              </template>
              <span v-else :class="{ 'text-nowrap': ci > 0 && col !== 'description' }">{{ cell(row.task, col) }}</span>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <nav v-if="pages > 1" class="d-flex align-items-center gap-2" aria-label="Results pages">
      <button
        type="button"
        class="btn btn-sm btn-outline-secondary"
        :disabled="results.page <= 1 || loading"
        @click="emit('page', results.page - 1)"
      >
        <i class="bi bi-chevron-left" /> Previous
      </button>
      <span class="small text-muted">Page {{ results.page }} of {{ pages }}</span>
      <button
        type="button"
        class="btn btn-sm btn-outline-secondary"
        :disabled="results.page >= pages || loading"
        @click="emit('page', results.page + 1)"
      >
        Next <i class="bi bi-chevron-right" />
      </button>
    </nav>
  </div>
</template>

<style scoped>
.query-results td {
  max-width: 28rem;
}
</style>
