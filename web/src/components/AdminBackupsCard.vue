<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { api } from '@/api/client'
import type { BackupStatus } from '@/api/types'
import { APIError } from '@/api/types'
import { useToast } from '@/composables/useToast'

// Admin → Settings: download a backup now, and see scheduled ones.
const { push } = useToast()
const status = ref<BackupStatus | null>(null)
const downloading = ref(false)

function size(bytes: number) {
  if (bytes >= 1 << 30) return `${(bytes / (1 << 30)).toFixed(1)} GB`
  if (bytes >= 1 << 20) return `${(bytes / (1 << 20)).toFixed(1)} MB`
  return `${Math.max(1, Math.round(bytes / 1024))} KB`
}

onMounted(async () => {
  try {
    status.value = await api.getBackupStatus()
  } catch {
    status.value = null
  }
})

async function downloadNow() {
  downloading.value = true
  try {
    await api.downloadBackup()
  } catch (err) {
    push(err instanceof APIError ? err.message : 'Backup failed', 'error')
  } finally {
    downloading.value = false
  }
}

async function downloadStored(name: string) {
  try {
    await api.downloadStoredBackup(name)
  } catch (err) {
    push(err instanceof APIError ? err.message : 'Download failed', 'error')
  }
}
</script>

<template>
  <div v-if="status" class="card mb-4">
    <div class="card-header"><h2 class="h5 mb-0">Backups</h2></div>
    <div class="card-body">
      <p class="small text-muted">
        A backup is a <code>.tar.gz</code> with the whole database (<code>pg_dump</code> custom format), uploaded images
        when they're stored on this server, and a <code>manifest.json</code> with restore steps. It includes secrets
        such as webhook and API settings, so keep it somewhere safe.
      </p>
      <div v-if="!status.pg_dump_available" class="alert alert-warning small py-2">
        <code>pg_dump</code> isn't installed on the server. Install the PostgreSQL client tools (the same or a newer
        major version than your database) or set <code>PG_DUMP_PATH</code>.
      </div>
      <button type="button" class="btn btn-sm btn-primary mb-3" :disabled="downloading || !status.pg_dump_available" @click="downloadNow">
        <i class="bi bi-download me-1" aria-hidden="true" />{{ downloading ? 'Preparing…' : 'Download a backup now' }}
      </button>
      <p v-if="status.pg_dump_version" class="small text-muted">{{ status.pg_dump_version }}</p>

      <h3 class="h6">Scheduled backups</h3>
      <p v-if="!status.scheduled" class="small text-muted mb-0">
        Off. Set <code>BACKUP_DIR</code> and <code>BACKUP_INTERVAL_HOURS</code> (and optionally <code>BACKUP_KEEP</code>,
        default 7) in the server's environment to write backups to a folder automatically.
      </p>
      <template v-else>
        <p class="small text-muted">
          Every {{ status.schedule.interval_hours }} hour{{ status.schedule.interval_hours === 1 ? '' : 's' }} into
          <code>{{ status.schedule.dir }}</code>, keeping the newest {{ status.schedule.keep }}.
        </p>
        <ul class="list-group list-group-flush small">
          <li v-for="f in status.files" :key="f.name" class="list-group-item d-flex align-items-center gap-2 px-0">
            <span class="flex-grow-1">{{ new Date(f.created_at).toLocaleString() }} · {{ size(f.size) }}</span>
            <button type="button" class="btn btn-sm btn-outline-secondary" @click="downloadStored(f.name)">Download</button>
          </li>
          <li v-if="!status.files.length" class="list-group-item px-0 text-muted">No backups yet.</li>
        </ul>
      </template>

      <details class="small mt-3">
        <summary>How to restore</summary>
        <ol class="mt-2 mb-0">
          <li>Turn on maintenance mode (or stop the server).</li>
          <li>Unpack the archive: <code>tar -xzf ordryn-backup-….tar.gz</code></li>
          <li><code>pg_restore --clean --if-exists --no-owner -d &lt;database&gt; database.dump</code></li>
          <li>Copy <code>uploads/</code> back into the image folder.</li>
          <li>Start the server (same or newer version) and turn maintenance mode off.</li>
        </ol>
      </details>
    </div>
  </div>
</template>
