<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { api } from '@/api/client'
import { useToast } from '@/composables/useToast'
import { APIError } from '@/api/types'

type PreviewRow = { title: string; project: string; due_date: string; tags: string }

const file = ref<File | null>(null)
const preview = ref<PreviewRow[]>([])
const wouldImport = ref(0)
const wouldSkip = ref(0)
const totalRows = ref(0)
const staged = ref(false)
const busy = ref(false)
const { push } = useToast()
const router = useRouter()

function onFileChange(event: Event) {
  const input = event.target as HTMLInputElement
  file.value = input.files?.[0] ?? null
  preview.value = []
  staged.value = false
}

async function runPreview() {
  if (!file.value) return
  busy.value = true
  try {
    const result = await api.importPreview(file.value)
    preview.value = result.preview
    wouldImport.value = result.would_import
    wouldSkip.value = result.would_skip
    totalRows.value = result.total_rows
    staged.value = true
    push('Import preview ready', 'success')
  } catch (err) {
    push(err instanceof APIError ? err.message : 'Preview failed', 'error')
  } finally {
    busy.value = false
  }
}

async function confirmImport() {
  busy.value = true
  try {
    const result = await api.importConfirm()
    push(`Imported ${result.imported} tasks (${result.skipped} skipped)`, 'success')
    await router.push({ name: 'tasks' })
  } catch (err) {
    push(err instanceof APIError ? err.message : 'Import failed', 'error')
  } finally {
    busy.value = false
  }
}

async function cancelImport() {
  try {
    await api.importCancel()
    staged.value = false
    preview.value = []
    push('Import cancelled', 'info')
  } catch (err) {
    push(err instanceof APIError ? err.message : 'Cancel failed', 'error')
  }
}
</script>

<template>
  <div class="container mt-3">
    <div class="card">
      <div class="card-header"><h1 class="h4 mb-0">Import tasks</h1></div>
      <div class="card-body">
        <p class="text-muted">Upload a CSV export to add tasks in bulk. Preview before confirming.</p>
        <form @submit.prevent="runPreview">
          <div class="mb-3">
            <label class="form-label">CSV file</label>
            <input type="file" class="form-control" accept=".csv,text/csv" required @change="onFileChange" />
          </div>
          <p class="text-muted small">Max 5 MB, 5000 rows. Use the same columns as export.</p>
          <button type="submit" class="btn btn-primary" :disabled="busy || !file">
            {{ busy ? 'Previewing…' : 'Preview import' }}
          </button>
        </form>

        <div v-if="staged" class="mt-4">
          <h2 class="h5">Preview</h2>
          <p class="text-muted">
            {{ totalRows }} rows — {{ wouldImport }} would import, {{ wouldSkip }} would skip
          </p>
          <table v-if="preview.length" class="table table-striped table-bordered">
            <thead>
              <tr>
                <th>Title</th>
                <th>Project</th>
                <th>Due</th>
                <th>Tags</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="(row, i) in preview" :key="i">
                <td>{{ row.title }}</td>
                <td>{{ row.project }}</td>
                <td>{{ row.due_date }}</td>
                <td>{{ row.tags }}</td>
              </tr>
            </tbody>
          </table>
          <div class="d-flex gap-2">
            <button type="button" class="btn btn-primary" :disabled="busy" @click="confirmImport">
              {{ busy ? 'Importing…' : 'Confirm import' }}
            </button>
            <button type="button" class="btn btn-secondary" :disabled="busy" @click="cancelImport">Cancel</button>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
