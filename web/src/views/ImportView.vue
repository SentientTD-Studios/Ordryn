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
  <section class="page narrow">
    <h1>Import tasks</h1>
    <p class="lede">Upload a CSV export to add tasks in bulk. Preview before confirming.</p>

    <form class="stack" @submit.prevent="runPreview">
      <label>
        CSV file
        <input type="file" accept=".csv,text/csv" required @change="onFileChange" />
      </label>
      <p class="muted">Max 5 MB, 5000 rows. Use the same columns as export.</p>
      <button class="primary" type="submit" :disabled="busy || !file">
        {{ busy ? 'Previewing…' : 'Preview import' }}
      </button>
    </form>

    <div v-if="staged" class="stack">
      <h2>Preview</h2>
      <p class="muted">
        {{ totalRows }} rows — {{ wouldImport }} would import, {{ wouldSkip }} would skip
      </p>
      <ul v-if="preview.length" class="plain-list">
        <li v-for="(row, i) in preview" :key="i" class="row">
          <div class="task-body">
            <strong>{{ row.title }}</strong>
            <p class="meta muted">
              {{ row.project || 'No project' }}
              <span v-if="row.due_date"> · due {{ row.due_date }}</span>
              <span v-if="row.tags"> · {{ row.tags }}</span>
            </p>
          </div>
        </li>
      </ul>
      <div class="actions">
        <button class="primary" type="button" :disabled="busy" @click="confirmImport">
          {{ busy ? 'Importing…' : 'Confirm import' }}
        </button>
        <button class="ghost" type="button" :disabled="busy" @click="cancelImport">Cancel</button>
      </div>
    </div>
  </section>
</template>
