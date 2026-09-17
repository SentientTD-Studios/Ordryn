<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { api } from '@/api/client'
import type { Project, ProjectGitHubRepo } from '@/api/types'
import { APIError } from '@/api/types'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import { withBase } from '@/base'

const props = defineProps<{
  project: Project
}>()

const emit = defineEmits<{
  changed: []
}>()

const toast = useToast()
const { askConfirm } = useConfirm()

const loading = ref(false)
const saving = ref(false)
const link = ref<ProjectGitHubRepo | null>(null)
const repository = ref('')
const isOwner = computed(() => (props.project.role || 'owner') === 'owner')

const webhookURL = computed(() => {
  if (typeof window === 'undefined') return ''
  return `${window.location.origin}${withBase('/api/v2/webhooks/github')}`
})

async function load() {
  loading.value = true
  try {
    link.value = await api.getProjectGitHub(props.project.id)
    if (link.value.linked && link.value.full_name) {
      repository.value = link.value.full_name
    }
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Failed to load GitHub link', 'error')
  } finally {
    loading.value = false
  }
}

async function linkRepo() {
  if (!repository.value.trim()) return
  saving.value = true
  try {
    link.value = await api.linkProjectGitHub(props.project.id, repository.value.trim())
    toast.push('GitHub repository linked', 'success')
    emit('changed')
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Failed to link repository', 'error')
  } finally {
    saving.value = false
  }
}

async function unlinkRepo() {
  const ok = await askConfirm({
    title: 'Unlink GitHub repository?',
    message:
      'This removes the repo link and clears GitHub issue links on tasks. Issues on GitHub are not deleted.',
    confirmLabel: 'Unlink',
    danger: true,
  })
  if (!ok) return
  saving.value = true
  try {
    await api.unlinkProjectGitHub(props.project.id)
    link.value = { linked: false }
    repository.value = ''
    toast.push('GitHub repository unlinked', 'info')
    emit('changed')
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Failed to unlink repository', 'error')
  } finally {
    saving.value = false
  }
}

async function copyText(value: string, label: string) {
  try {
    await navigator.clipboard.writeText(value)
    toast.push(`${label} copied`, 'success')
  } catch {
    toast.push(`Could not copy ${label}`, 'error')
  }
}

watch(
  () => props.project.id,
  () => {
    void load()
  },
  { immediate: true },
)
</script>

<template>
  <div>
    <h6 class="fw-bold mb-2">GitHub repository</h6>
    <p class="form-hint small mb-3">
      Link a repository you administer so board tasks can create or attach GitHub issues.
      Ordryn never creates tasks from GitHub issues.
    </p>

    <div v-if="loading" class="text-muted small">Loading…</div>

    <template v-else>
      <div v-if="link?.linked" class="mb-3">
        <div class="d-flex flex-wrap align-items-center gap-2 mb-2">
          <a
            v-if="link.html_url"
            :href="link.html_url"
            target="_blank"
            rel="noopener noreferrer"
            class="fw-semibold"
          >
            {{ link.full_name }}
          </a>
          <span v-else class="fw-semibold">{{ link.full_name }}</span>
          <span class="badge text-bg-success">Linked</span>
        </div>

        <div v-if="isOwner && link.webhook_secret" class="small border rounded p-2 mb-2">
          <div class="fw-semibold mb-1">Optional webhook (status sync from GitHub)</div>
          <div class="mb-1 text-break">
            <span class="text-muted">URL:</span>
            <code class="ms-1">{{ webhookURL }}</code>
            <button type="button" class="btn btn-link btn-sm py-0" @click="copyText(webhookURL, 'Webhook URL')">
              Copy
            </button>
          </div>
          <div class="text-break">
            <span class="text-muted">Secret:</span>
            <code class="ms-1">{{ link.webhook_secret }}</code>
            <button
              type="button"
              class="btn btn-link btn-sm py-0"
              @click="copyText(link.webhook_secret || '', 'Webhook secret')"
            >
              Copy
            </button>
          </div>
          <div class="form-hint mt-1 mb-0">
            Configure a GitHub webhook for Issues events. Use the secret as the webhook secret
            (HMAC) or send header <code>X-Ordryn-Webhook-Secret</code>.
          </div>
        </div>

        <button
          v-if="isOwner"
          type="button"
          class="btn btn-sm btn-outline-danger"
          :disabled="saving"
          @click="unlinkRepo"
        >
          Unlink repository
        </button>
      </div>

      <div v-else-if="isOwner" class="row g-2 align-items-end">
        <div class="col-sm-8">
          <label class="form-label small mb-1" for="github-repo-input">Repository</label>
          <input
            id="github-repo-input"
            v-model="repository"
            type="text"
            class="form-control form-control-sm"
            placeholder="owner/repo"
            :disabled="saving"
          />
        </div>
        <div class="col-sm-4">
          <button
            type="button"
            class="btn btn-sm btn-primary w-100"
            :disabled="saving || !repository.trim()"
            @click="linkRepo"
          >
            {{ saving ? 'Linking…' : 'Link repository' }}
          </button>
        </div>
        <div class="col-12">
          <div class="form-hint mb-0">
            You must be connected to GitHub in Settings and have admin access on the repository.
          </div>
        </div>
      </div>

      <p v-else class="text-muted small mb-0">No GitHub repository linked.</p>
    </template>
  </div>
</template>
