<template>
  <div class="container mt-3">
    <h1>Dashboard</h1>
    <p v-if="loading" class="text-muted">Loading…</p>
    <template v-else-if="stats">
      <div class="row g-3 mb-4">
        <div class="col-sm-6 col-lg-3">
          <div class="card text-center">
            <div class="card-body">
              <div class="text-muted small">Overdue</div>
              <div class="display-6">{{ stats.overdue_count }}</div>
            </div>
          </div>
        </div>
        <div class="col-sm-6 col-lg-3">
          <div class="card text-center">
            <div class="card-body">
              <div class="text-muted small">Due today</div>
              <div class="display-6">{{ stats.due_today_count }}</div>
            </div>
          </div>
        </div>
        <div class="col-sm-6 col-lg-3">
          <div class="card text-center">
            <div class="card-body">
              <div class="text-muted small">Done this week</div>
              <div class="display-6">{{ stats.completed_this_week }}</div>
            </div>
          </div>
        </div>
        <div class="col-sm-6 col-lg-3">
          <div class="card text-center">
            <div class="card-body">
              <div class="text-muted small">Streak</div>
              <div class="display-6">{{ stats.streak_days }}d</div>
            </div>
          </div>
        </div>
      </div>

      <div class="row g-4">
        <div class="col-lg-6">
          <div class="card">
            <div class="card-header"><h2 class="h5 mb-0">By project</h2></div>
            <ul class="list-group list-group-flush">
              <li v-for="row in stats.by_project" :key="row.name" class="list-group-item d-flex justify-content-between">
                <span>{{ row.name || 'No project' }}</span>
                <span class="text-muted">{{ row.count }}</span>
              </li>
              <li v-if="!stats.by_project.length" class="list-group-item text-muted">No project breakdown.</li>
            </ul>
          </div>
        </div>
        <div class="col-lg-6">
          <div class="card">
            <div class="card-header"><h2 class="h5 mb-0">Last 7 days</h2></div>
            <ul class="list-group list-group-flush">
              <li v-for="day in stats.completions_last_7_days" :key="day.date" class="list-group-item d-flex justify-content-between">
                <span>{{ day.date }}</span>
                <span class="text-muted">{{ day.count }}</span>
              </li>
            </ul>
          </div>
        </div>
      </div>
    </template>
  </div>
</template>

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
