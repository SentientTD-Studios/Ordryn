<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { api } from '@/api/client'
import type { DashboardStats } from '@/api/types'
import { APIError } from '@/api/types'
import { useToast } from '@/composables/useToast'

const stats = ref<DashboardStats | null>(null)
const loading = ref(true)
const toast = useToast()

onMounted(async () => {
  try {
    stats.value = await api.dashboard()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Failed to load dashboard', 'error')
  } finally {
    loading.value = false
  }
})
</script>

<template>
  <section class="page">
    <header class="page-head">
      <div>
        <h1>Dashboard</h1>
        <p class="lede">Task rhythm from `/api/v1/dashboard`.</p>
      </div>
    </header>

    <p v-if="loading" class="muted">Loading…</p>
    <template v-else-if="stats">
      <div class="stat-grid">
        <div class="stat">
          <span class="stat-label">Overdue</span>
          <strong>{{ stats.overdue_count }}</strong>
        </div>
        <div class="stat">
          <span class="stat-label">Due today</span>
          <strong>{{ stats.due_today_count }}</strong>
        </div>
        <div class="stat">
          <span class="stat-label">Done this week</span>
          <strong>{{ stats.completed_this_week }}</strong>
        </div>
        <div class="stat">
          <span class="stat-label">Streak</span>
          <strong>{{ stats.streak_days }}d</strong>
        </div>
      </div>

      <div class="split">
        <div>
          <h2>By project</h2>
          <ul class="plain-list">
            <li v-for="row in stats.by_project" :key="row.name" class="row">
              <span>{{ row.name || 'No project' }}</span>
              <span class="muted">{{ row.count }}</span>
            </li>
            <li v-if="!stats.by_project.length" class="muted empty">No project breakdown.</li>
          </ul>
        </div>
        <div>
          <h2>Last 7 days</h2>
          <ul class="plain-list">
            <li v-for="day in stats.completions_last_7_days" :key="day.date" class="row">
              <span>{{ day.date }}</span>
              <span class="muted">{{ day.count }}</span>
            </li>
          </ul>
        </div>
      </div>
    </template>
  </section>
</template>
