<script setup lang="ts">
import { onMounted, ref } from 'vue'
import type { Tag } from '@/api/types'
import type { ViewDensity } from '@/composables/useViewDensity'

export type KanbanSurfaceTab = {
  viewKey: string
  label: string
  icon?: string
}

const props = withDefaults(
  defineProps<{
    status: string
    tag: string
    priority: string
    dueDatePreset: string
    sort: string
    search: string
    density: ViewDensity
    tags: Tag[]
    showViewMode?: boolean
    viewMode?: string
    tagByName?: boolean
    surfaceTabs?: KanbanSurfaceTab[]
    hideTaskFilters?: boolean
  }>(),
  {
    showViewMode: false,
    viewMode: 'list',
    tagByName: false,
    surfaceTabs: () => [],
    hideTaskFilters: false,
  },
)

const emit = defineEmits<{
  'update:status': [val: string]
  'update:tag': [val: string]
  'update:priority': [val: string]
  'update:dueDatePreset': [val: string]
  'update:sort': [val: string]
  'update:search': [val: string]
  'update:density': [val: ViewDensity]
  'update:viewMode': [val: string]
  'clear-filters': []
}>()

// Fold/unfold state for the filter toolbar
const showFilterPills = ref(true)

onMounted(() => {
  if (typeof window !== 'undefined' && window.matchMedia('(max-width: 767.98px)').matches) {
    showFilterPills.value = false
  }
})

function tagFilterValue(t: Tag) {
  return props.tagByName ? t.name : String(t.id)
}

function selectedTagLabel() {
  if (!props.tag) return 'ALL'
  const match = props.tags.find((t) =>
    props.tagByName ? t.name.toLowerCase() === props.tag.toLowerCase() : String(t.id) === props.tag,
  )
  return match?.name || 'Selected'
}

function getDueDateLabel(preset: string) {
  if (preset === 'today') return 'TODAY'
  if (preset === 'overdue') return 'OVERDUE'
  if (preset === 'this_week' || preset === 'week') return 'THIS WEEK'
  if (preset === 'nodate' || preset === 'none') return 'NO DATE'
  return 'ALL'
}
</script>

<template>
  <div class="ordryn-filter-bar mb-2">
    <div class="d-flex flex-column flex-md-row align-items-md-center justify-content-between gap-2">
      <!-- Search Input & Filter Fold Toggle Button -->
        <div v-if="!hideTaskFilters" class="d-flex align-items-center gap-2 flex-grow-1 oryryn-filter-search">
        <div class="position-relative flex-grow-1">
          <i class="bi bi-search position-absolute top-50 start-0 translate-middle-y ms-3 text-muted" />
          <input
            id="task-search"
            type="text"
            class="form-control form-control-sm ps-5 pe-3 py-1.5 rounded-pill border-0 shadow-xs"
            placeholder="Search tasks..."
            :value="search"
            style="background: var(--ordryn-card-bg); color: var(--ordryn-text);"
            @input="emit('update:search', ($event.target as HTMLInputElement).value)"
          />
        </div>

        <!-- Filter Fold / Toggle Button -->
        <button
          type="button"
          class="btn btn-sm d-flex align-items-center gap-1 rounded-pill px-3 py-1.5 border-0 shadow-xs fw-semibold"
          :class="showFilterPills ? 'btn-primary' : 'btn-outline-secondary'"
          style="white-space: nowrap;"
          title="Fold / Unfold filters"
          @click="showFilterPills = !showFilterPills"
        >
          <i class="bi bi-funnel" />
          <span>Filters</span>
          <i :class="showFilterPills ? 'bi bi-chevron-up' : 'bi bi-chevron-down'" class="ms-1 small" />
        </button>
      </div>

      <!-- Right: View mode + Density -->
      <div class="d-flex align-items-center gap-3 flex-wrap justify-content-md-end">
        <div
          v-if="showViewMode"
          class="d-flex align-items-center gap-2"
        >
          <span class="small fw-medium text-muted d-none d-sm-inline">View:</span>
          <div
            class="btn-group btn-group-sm p-1 rounded-pill"
            style="background: var(--ordryn-muted-bg);"
            role="group"
            aria-label="View mode"
          >
            <button
              type="button"
              class="btn btn-sm rounded-pill px-2.5 py-0.5 border-0 fw-medium transition-all"
              :class="viewMode === 'list' ? 'shadow-xs fw-bold' : 'text-muted'"
              :style="viewMode === 'list' ? 'background: var(--ordryn-card-bg); color: var(--ordryn-text);' : ''"
              @click="emit('update:viewMode', 'list')"
            >
              <i class="bi bi-list-ul me-1" />List
            </button>
            <button
              type="button"
              class="btn btn-sm rounded-pill px-2.5 py-0.5 border-0 fw-medium transition-all"
              :class="viewMode === 'board' ? 'shadow-xs fw-bold' : 'text-muted'"
              :style="viewMode === 'board' ? 'background: var(--ordryn-card-bg); color: var(--ordryn-text);' : ''"
              @click="emit('update:viewMode', 'board')"
            >
              <i class="bi bi-kanban me-1" />Board
            </button>
            <button
              v-for="tab in surfaceTabs"
              :key="tab.viewKey"
              type="button"
              class="btn btn-sm rounded-pill px-2.5 py-0.5 border-0 fw-medium transition-all"
              :class="viewMode === tab.viewKey ? 'shadow-xs fw-bold' : 'text-muted'"
              :style="viewMode === tab.viewKey ? 'background: var(--ordryn-card-bg); color: var(--ordryn-text);' : ''"
              @click="emit('update:viewMode', tab.viewKey)"
            >
              <img
                v-if="tab.icon"
                :src="tab.icon"
                alt=""
                width="14"
                height="14"
                class="me-1"
                style="object-fit: contain"
              />
              {{ tab.label }}
            </button>
          </div>
        </div>

        <div class="d-flex align-items-center gap-2">
          <span class="small fw-medium text-muted d-none d-sm-inline">Density:</span>
          <div class="btn-group btn-group-sm p-1 rounded-pill" style="background: var(--ordryn-muted-bg);">
            <button
              type="button"
              class="btn btn-sm rounded-pill px-2 px-md-2.5 py-0.5 border-0 fw-medium transition-all"
              :class="density === 'comfortable' ? 'shadow-xs fw-bold' : 'text-muted'"
              :style="density === 'comfortable' ? 'background: var(--ordryn-card-bg); color: var(--ordryn-text);' : ''"
              title="Comfortable density"
              @click="emit('update:density', 'comfortable')"
            >
              <i class="bi bi-view-list" /><span class="d-none d-md-inline ms-1">Comfortable</span>
            </button>
            <button
              type="button"
              class="btn btn-sm rounded-pill px-2 px-md-2.5 py-0.5 border-0 fw-medium transition-all"
              :class="density === 'dense' ? 'shadow-xs fw-bold' : 'text-muted'"
              :style="density === 'dense' ? 'background: var(--ordryn-card-bg); color: var(--ordryn-text);' : ''"
              title="Dense density"
              @click="emit('update:density', 'dense')"
            >
              <i class="bi bi-list-task" /><span class="d-none d-md-inline ms-1">Dense</span>
            </button>
          </div>
        </div>
      </div>
    </div>

    <!-- Foldable Filter Pills Row -->
    <div v-if="showFilterPills && !hideTaskFilters" class="filter-pills-container mt-2">
      <!-- Status Dropdown Pill (default: incomplete) -->
      <div class="dropdown d-inline-block">
        <button
          class="filter-pill-btn dropdown-toggle"
          :class="{ active: status !== 'incomplete' }"
          type="button"
          data-bs-toggle="dropdown"
        >
          <span class="d-none d-sm-inline">STATUS: </span><span class="text-uppercase fw-bold">{{ status || 'ALL' }}</span>
        </button>
        <ul class="dropdown-menu shadow-sm border-0">
          <li><button class="dropdown-item small" @click="emit('update:status', '')">All Statuses</button></li>
          <li><button class="dropdown-item small" @click="emit('update:status', 'incomplete')">Incomplete</button></li>
          <li><button class="dropdown-item small" @click="emit('update:status', 'complete')">Completed</button></li>
        </ul>
      </div>

      <!-- Tag Dropdown Pill -->
      <div class="dropdown d-inline-block">
        <button
          class="filter-pill-btn dropdown-toggle"
          :class="{ active: tag !== '' }"
          type="button"
          data-bs-toggle="dropdown"
        >
          <span class="d-none d-sm-inline">TAGS: </span><span class="fw-bold">{{ selectedTagLabel() }}</span>
        </button>
        <ul class="dropdown-menu shadow-sm border-0">
          <li><button class="dropdown-item small" @click="emit('update:tag', '')">All Tags</button></li>
          <li v-for="t in tags" :key="t.id">
            <button class="dropdown-item small" @click="emit('update:tag', tagFilterValue(t))">
              <span class="badge rounded-pill me-1" :style="{ backgroundColor: t.color || '#6c757d' }">&nbsp;</span>
              {{ t.name }}
            </button>
          </li>
        </ul>
      </div>

      <!-- Due Date Dropdown Pill -->
      <div class="dropdown d-inline-block">
        <button
          class="filter-pill-btn dropdown-toggle"
          :class="{ active: dueDatePreset !== '' }"
          type="button"
          data-bs-toggle="dropdown"
        >
          <span class="d-none d-sm-inline">DUE DATE: </span><span class="fw-bold">{{ getDueDateLabel(dueDatePreset) }}</span>
        </button>
        <ul class="dropdown-menu shadow-sm border-0">
          <li><button class="dropdown-item small" @click="emit('update:dueDatePreset', '')">ALL</button></li>
          <li><button class="dropdown-item small" @click="emit('update:dueDatePreset', 'today')">TODAY</button></li>
          <li><button class="dropdown-item small" @click="emit('update:dueDatePreset', 'overdue')">OVERDUE</button></li>
          <li><button class="dropdown-item small" @click="emit('update:dueDatePreset', 'this_week')">THIS WEEK</button></li>
          <li><button class="dropdown-item small" @click="emit('update:dueDatePreset', 'nodate')">NO DATE</button></li>
        </ul>
      </div>

      <!-- Priority Dropdown Pill -->
      <div class="dropdown d-inline-block">
        <button
          class="filter-pill-btn dropdown-toggle"
          :class="{ active: priority !== '' }"
          type="button"
          data-bs-toggle="dropdown"
        >
          <span class="d-none d-sm-inline">PRIORITY: </span><span class="fw-bold">{{ priority ? (priority === '3' ? 'HIGH' : priority === '2' ? 'MED' : 'LOW') : 'ALL' }}</span>
        </button>
        <ul class="dropdown-menu shadow-sm border-0">
          <li><button class="dropdown-item small" @click="emit('update:priority', '')">All Priorities</button></li>
          <li><button class="dropdown-item small text-danger" @click="emit('update:priority', '3')">High Priority</button></li>
          <li><button class="dropdown-item small text-warning" @click="emit('update:priority', '2')">Medium Priority</button></li>
          <li><button class="dropdown-item small text-secondary" @click="emit('update:priority', '1')">Low Priority</button></li>
        </ul>
      </div>

      <!-- Order Dropdown Pill -->
      <div class="dropdown d-inline-block">
        <button
          class="filter-pill-btn dropdown-toggle"
          :class="{ active: sort === 'priority' }"
          type="button"
          data-bs-toggle="dropdown"
        >
          <span class="d-none d-sm-inline">ORDER: </span><span class="fw-bold">{{ sort === 'priority' ? 'PRIORITY' : 'CUSTOM' }}</span>
        </button>
        <ul class="dropdown-menu shadow-sm border-0">
          <li><button class="dropdown-item small" @click="emit('update:sort', 'custom')">Custom Order</button></li>
          <li><button class="dropdown-item small" @click="emit('update:sort', 'priority')">Priority Order</button></li>
        </ul>
      </div>

      <!-- Clear Filters Pill -->
      <button
        type="button"
        class="btn btn-link btn-sm text-decoration-none p-0 text-muted ms-2 small"
        @click="emit('clear-filters')"
      >
        <i class="bi bi-x-circle me-1" />Clear
      </button>
    </div>
  </div>
</template>
