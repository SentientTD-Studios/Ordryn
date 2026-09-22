<script setup lang="ts">
import { computed } from 'vue'
import { RouterLink } from 'vue-router'
import { useSite } from '@/composables/useSite'
import AppFooter from '@/components/AppFooter.vue'
import ModernTaskCard from '@/components/modern/ModernTaskCard.vue'
import type { Task } from '@/api/types'

const { siteName, metaDescription, openJoin, enableJoinRequests } = useSite()

const fallbackDescription = computed(
  () =>
    `${siteName.value} helps you track tasks, organize projects, and plan work with a calendar and live collaboration.`,
)

const aboutText = computed(() => metaDescription.value || fallbackDescription.value)

function isoDaysFromToday(days: number) {
  const d = new Date()
  d.setDate(d.getDate() + days)
  return d.toISOString().slice(0, 10)
}

const previewTasks: Task[] = [
  {
    id: -1,
    title: 'Ship the launch checklist',
    description: 'Confirm docs, invites, and the public homepage before go-live.',
    completed: false,
    due_date: isoDaysFromToday(0),
    project: 'Launch',
    priority: 3,
    position: 0,
    child_count: 2,
    children_completed: 1,
    tags: [
      { id: -11, name: 'launch', color: '#7c3aed' },
    ],
    created_at: '',
    modified_at: '',
    children: [
      {
        id: -11,
        title: 'Write the weekly digest',
        description: '',
        completed: true,
        due_date: isoDaysFromToday(-1),
        priority: 1,
        position: 0,
        parent_id: -1,
        tags: [],
        created_at: '',
        modified_at: '',
      },
      {
        id: -12,
        title: 'Review the project board with the team',
        description: '',
        completed: false,
        due_date: isoDaysFromToday(1),
        priority: 2,
        position: 1,
        parent_id: -1,
        tags: [{ id: -12, name: 'planning', color: '#2563eb' }],
        created_at: '',
        modified_at: '',
      },
    ],
  },
]
</script>

<template>
  <div class="guest-home">
    <section class="guest-hero py-5">
      <div class="container">
        <div class="row align-items-center g-4">
          <div class="col-lg-6">
            <p class="text-uppercase small fw-semibold mb-2 guest-kicker">Welcome</p>
            <h1 class="display-5 fw-bold mb-3">{{ siteName }}</h1>
            <p class="lead mb-4" style="color: var(--ordryn-muted);">{{ aboutText }}</p>
            <div class="d-flex flex-wrap gap-2">
              <RouterLink v-if="openJoin" to="/register" class="btn btn-primary btn-lg">
                Join Today!
              </RouterLink>
              <RouterLink v-if="enableJoinRequests" to="/join" class="btn btn-lg" :class="openJoin ? 'btn-outline-primary' : 'btn-primary'">
                Request to Join!
              </RouterLink>
              <RouterLink to="/login" class="btn btn-lg btn-outline-secondary">Login</RouterLink>
            </div>
          </div>
          <div class="col-lg-6">
            <div class="guest-preview" aria-hidden="true">
              <div
                v-for="task in previewTasks"
                :key="task.id"
                class="task-tree-root"
              >
                <ModernTaskCard
                  :task="task"
                  :selected="false"
                  density="comfortable"
                  :can-write="false"
                  :expanded="true"
                />
                <div v-if="task.children?.length" class="task-children">
                  <ModernTaskCard
                    v-for="child in task.children"
                    :key="child.id"
                    :task="child"
                    :depth="1"
                    :selected="false"
                    density="comfortable"
                    :can-write="false"
                    :show-project-pill="false"
                  />
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>
    </section>

    <section class="py-5" style="background: var(--ordryn-muted-bg);">
      <div class="container">
        <h2 class="h4 mb-4">What you can do</h2>
        <div class="row g-3">
          <div class="col-md-6 col-lg-3">
            <div class="card h-100 border-0 shadow-sm">
              <div class="card-body">
                <i class="bi bi-check2-square fs-3 mb-2 d-block" style="color: var(--ordryn-accent);" />
                <h3 class="h6">Tasks</h3>
                <p class="small mb-0" style="color: var(--ordryn-muted);">
                  Capture work, nest subtasks, and mark items done.
                </p>
              </div>
            </div>
          </div>
          <div class="col-md-6 col-lg-3">
            <div class="card h-100 border-0 shadow-sm">
              <div class="card-body">
                <i class="bi bi-folder2 fs-3 mb-2 d-block" style="color: var(--ordryn-accent);" />
                <h3 class="h6">Projects &amp; views</h3>
                <p class="small mb-0" style="color: var(--ordryn-muted);">
                  Group work into projects, tag it, and save filters you reuse.
                </p>
              </div>
            </div>
          </div>
          <div class="col-md-6 col-lg-3">
            <div class="card h-100 border-0 shadow-sm">
              <div class="card-body">
                <i class="bi bi-calendar3 fs-3 mb-2 d-block" style="color: var(--ordryn-accent);" />
                <h3 class="h6">Calendar</h3>
                <p class="small mb-0" style="color: var(--ordryn-muted);">
                  See due dates by month and keep upcoming work in view.
                </p>
              </div>
            </div>
          </div>
          <div class="col-md-6 col-lg-3">
            <div class="card h-100 border-0 shadow-sm">
              <div class="card-body">
                <i class="bi bi-people fs-3 mb-2 d-block" style="color: var(--ordryn-accent);" />
                <h3 class="h6">Live collaboration</h3>
                <p class="small mb-0" style="color: var(--ordryn-muted);">
                  Share projects, comment on tasks, and stay in sync as others edit.
                </p>
              </div>
            </div>
          </div>
        </div>
        <p class="mt-4 mb-0">
          <RouterLink to="/docs/guide">How to use {{ siteName }}</RouterLink>
        </p>
      </div>
    </section>

    <section class="py-5">
      <div class="container">
        <h2 class="h4 mb-3">About {{ siteName }}</h2>
        <p class="mb-0" style="color: var(--ordryn-muted); max-width: 40rem;">{{ aboutText }}</p>
      </div>
    </section>

    <AppFooter class="container" />
  </div>
</template>

<style scoped>
.guest-kicker {
  letter-spacing: 0.08em;
  color: var(--ordryn-accent);
}
.guest-preview {
  pointer-events: none;
  user-select: none;
}
.guest-preview :deep(.ordryn-task-card:last-child) {
  margin-bottom: 0;
}
</style>
