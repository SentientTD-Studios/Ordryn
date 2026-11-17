<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { RouterLink } from 'vue-router'
import { api } from '@/api/client'
import type { CompanyHomeSummary } from '@/api/types'
import RichBody from '@/components/RichBody.vue'
import { useSite } from '@/composables/useSite'
import { useAuth } from '@/composables/useAuth'
import { projectRoute } from '@/utils/projectRoute'

// Company mode home page: the company's welcome (set in site settings) and the
// signed-in person's own work, projects and teams.
const { siteName } = useSite()
const { user } = useAuth()
const content = ref('')
const canEdit = ref(false)
const summary = ref<CompanyHomeSummary | null>(null)
const loaded = ref(false)

onMounted(async () => {
  try {
    const home = await api.getCompanyHome()
    content.value = home.content
    canEdit.value = home.can_edit
    summary.value = home.summary ?? null
  } catch {
    // The note below still explains what to do.
  } finally {
    loaded.value = true
  }
})

function greeting() {
  const h = new Date().getHours()
  const part = h < 12 ? 'Good morning' : h < 18 ? 'Good afternoon' : 'Good evening'
  const name = user.value?.first_name || user.value?.user_name
  return name ? `${part}, ${name}` : part
}
</script>

<template>
  <div class="company-home mx-auto py-3" style="max-width: 64rem">
    <p class="text-muted small mb-1">{{ siteName }}</p>
    <h1 class="h3 fw-bold mb-3">{{ greeting() }}</h1>

    <div v-if="content" class="card card-body mb-3">
      <RichBody :body="content" />
    </div>

    <template v-if="summary">
      <section class="mb-4" aria-labelledby="ch-work">
        <h2 id="ch-work" class="h6 text-uppercase text-muted small mb-2">Your work</h2>
        <div class="row g-2">
          <div class="col-6 col-md-3">
            <RouterLink to="/" class="ch-stat card card-body text-decoration-none">
              <span class="ch-stat-num">{{ summary.my_work.open }}</span>
              <span class="small text-muted">open</span>
            </RouterLink>
          </div>
          <div class="col-6 col-md-3">
            <RouterLink to="/" class="ch-stat card card-body text-decoration-none" :class="{ 'is-alert': summary.my_work.overdue }">
              <span class="ch-stat-num">{{ summary.my_work.overdue }}</span>
              <span class="small text-muted">overdue</span>
            </RouterLink>
          </div>
          <div class="col-6 col-md-3">
            <RouterLink to="/calendar" class="ch-stat card card-body text-decoration-none">
              <span class="ch-stat-num">{{ summary.my_work.due_today }}</span>
              <span class="small text-muted">due today</span>
            </RouterLink>
          </div>
          <div class="col-6 col-md-3">
            <RouterLink to="/calendar" class="ch-stat card card-body text-decoration-none">
              <span class="ch-stat-num">{{ summary.my_work.due_week }}</span>
              <span class="small text-muted">due this week</span>
            </RouterLink>
          </div>
        </div>
        <p class="small text-muted mt-1 mb-0">Tasks you claimed, plus tasks you created that nobody has claimed.</p>
      </section>

      <div class="row g-3">
        <section class="col-md-8" aria-labelledby="ch-projects">
          <h2 id="ch-projects" class="h6 text-uppercase text-muted small mb-2">
            Your projects
            <span v-if="summary.total_projects > summary.projects.length" class="text-lowercase">
              ({{ summary.projects.length }} of {{ summary.total_projects }})
            </span>
          </h2>
          <div v-if="summary.projects.length" class="list-group">
            <RouterLink
              v-for="p in summary.projects"
              :key="p.id"
              :to="projectRoute(p)"
              class="list-group-item list-group-item-action d-flex align-items-center gap-2"
            >
              <i class="bi bi-folder2 text-muted" aria-hidden="true" />
              <span class="text-truncate">{{ p.name }}</span>
              <span class="ms-auto small text-muted text-nowrap">{{ p.open }} open</span>
              <span v-if="p.overdue" class="badge text-bg-danger">{{ p.overdue }} overdue</span>
            </RouterLink>
          </div>
          <div v-else class="alert alert-info d-flex gap-2 align-items-start mb-0" role="status">
            <i class="bi bi-info-circle mt-1" aria-hidden="true" />
            <div>
              <strong>You're not on any projects yet.</strong>
              Every task here belongs to a project. Once a project is shared with you or one of your teams, it shows up
              here and in the sidebar.
            </div>
          </div>
        </section>

        <section class="col-md-4" aria-labelledby="ch-teams">
          <h2 id="ch-teams" class="h6 text-uppercase text-muted small mb-2">Your teams</h2>
          <ul v-if="summary.teams.length" class="list-group">
            <li v-for="t in summary.teams" :key="t.id" class="list-group-item d-flex align-items-center gap-2">
              <i class="bi bi-people text-muted" aria-hidden="true" />
              <span class="text-truncate">{{ t.name }}</span>
              <span class="ms-auto small text-muted">{{ t.members }}</span>
            </li>
          </ul>
          <p v-else class="small text-muted">You're not on a team yet.</p>
          <RouterLink to="/organizations" class="small">Company &amp; teams</RouterLink>
        </section>
      </div>
    </template>

    <div v-else-if="loaded" class="alert alert-info d-flex gap-2 align-items-start" role="status">
      <i class="bi bi-info-circle mt-1" aria-hidden="true" />
      <div>
        Once a project is shared with you or one of your teams, it shows up in the sidebar and you can start adding tasks.
      </div>
    </div>

    <p v-if="loaded && canEdit" class="small text-muted mt-3 mb-0">
      <template v-if="!content">You can add a welcome message to the top of this page. </template>
      Edit it in <RouterLink to="/admin">Site settings</RouterLink>.
    </p>
  </div>
</template>

<style scoped>
.ch-stat {
  color: var(--ordryn-text);
  background: var(--ordryn-card-bg);
  border-color: var(--ordryn-card-border);
  padding: 0.75rem 1rem;
}
.ch-stat:hover {
  border-color: var(--ordryn-accent);
}
.ch-stat-num {
  font-size: 1.6rem;
  font-weight: 700;
  line-height: 1.1;
}
.ch-stat.is-alert .ch-stat-num {
  color: var(--bs-danger);
}
</style>
