<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { api } from '@/api/client'
import type { APIKey, Project } from '@/api/types'
import { APIError } from '@/api/types'
import { pathPrefix } from '@/base'
import { useToast } from '@/composables/useToast'
import { apiRoot } from '@/utils/projectAgents'
import { siteUrl } from '@/utils/ollamaAgentScript'
import { EXPIRY_PRESETS, expiresAtFromPreset, type ExpiryPreset } from '@/utils/projectApiKeys'
import {
  agentsMdSnippet,
  localAgentSnippets,
  localServerName,
  LOCAL_AGENT_CLIENTS,
  type LocalAgentClient,
} from '@/utils/localAgentSetup'

const emit = defineEmits<{ created: [] }>()
const { push } = useToast()

const projects = ref<Project[]>([])
const name = ref('')
const projectId = ref<number | null>(null)
const canWriteTasks = ref(false)
const canComment = ref(false)
const expiry = ref<ExpiryPreset>('90')
const creating = ref(false)
const minted = ref<(APIKey & { key: string }) | null>(null)
const client = ref<LocalAgentClient>('claude')

const root = computed(() => apiRoot(window.location.origin, pathPrefix()))
const site = computed(() => siteUrl(window.location.origin, pathPrefix()))
const limitName = computed(() => {
  if (minted.value) return minted.value.project_name ?? ''
  return projects.value.find((p) => p.id === projectId.value)?.name ?? ''
})
const serverName = computed(() => localServerName(limitName.value))
const snippets = computed(() =>
  localAgentSnippets(client.value, root.value, serverName.value, minted.value?.key),
)
const mintedCanWrite = computed(() =>
  minted.value
    ? minted.value.scopes.some((s) => s !== 'tasks:read')
    : canWriteTasks.value || canComment.value,
)
const agentsMd = computed(() => agentsMdSnippet(site.value, serverName.value, mintedCanWrite.value))

async function loadProjects() {
  try {
    projects.value = await api.listProjects()
  } catch {
    projects.value = [] // the key can still be made for all projects
  }
}

async function create() {
  if (!name.value.trim() || creating.value) return
  creating.value = true
  try {
    const scopes = ['tasks:read']
    if (canWriteTasks.value) scopes.push('tasks:write')
    if (canComment.value) scopes.push('comments:write')
    minted.value = await api.createLocalAgentKey({
      name: name.value.trim(),
      scopes,
      project_id: projectId.value,
      expires_at: expiresAtFromPreset(expiry.value),
    })
    name.value = ''
    emit('created')
    push('Local agent key created — copy it now', 'success')
  } catch (err) {
    push(err instanceof APIError ? err.message : 'Create failed', 'error')
  } finally {
    creating.value = false
  }
}

async function copy(text: string) {
  try {
    await navigator.clipboard.writeText(text)
    push('Copied', 'success')
  } catch {
    push('Copy failed — select the text and copy it manually', 'error')
  }
}

onMounted(() => {
  void loadProjects()
})
</script>

<template>
  <div id="local-agents-section" class="card mb-4">
    <div class="card-header">
      <h3 class="card-title mb-0">Local AI agents</h3>
    </div>
    <div class="card-body">
      <p class="text-muted small">
        Let an AI agent on your computer (Claude Code, Codex, Gemini CLI, Cursor, or your own script) read your tasks, so you can
        say “check out task #123” in your editor. The key acts as you, but can only reach tasks and comments: it can't change your
        account, create keys, or delete anything. Nothing runs on this site; the agent connects over MCP or the REST API.
      </p>

      <form class="row g-2 align-items-end mb-3" @submit.prevent="create">
        <div class="col-md-4">
          <label class="form-label small mb-1" for="local-agent-name">Key name</label>
          <input
            id="local-agent-name"
            v-model="name"
            type="text"
            class="form-control form-control-sm"
            maxlength="80"
            placeholder="Laptop – Claude Code"
            required
          />
        </div>
        <div class="col-md-4">
          <label class="form-label small mb-1" for="local-agent-project">Projects</label>
          <select id="local-agent-project" v-model="projectId" class="form-select form-select-sm">
            <option :value="null">All my projects</option>
            <option v-for="p in projects" :key="p.id" :value="p.id">Only {{ p.name }}</option>
          </select>
        </div>
        <div class="col-md-2">
          <label class="form-label small mb-1" for="local-agent-expiry">Expires</label>
          <select id="local-agent-expiry" v-model="expiry" class="form-select form-select-sm">
            <option v-for="p in EXPIRY_PRESETS" :key="p.id" :value="p.id">{{ p.label }}</option>
          </select>
        </div>
        <div class="col-md-2">
          <button type="submit" class="btn btn-sm btn-primary w-100" :disabled="creating || !name.trim()">Create key</button>
        </div>
        <div class="col-12">
          <span class="small text-muted me-3">The agent can:</span>
          <div class="form-check form-check-inline small">
            <input id="local-agent-read" class="form-check-input" type="checkbox" checked disabled />
            <label class="form-check-label" for="local-agent-read">Read tasks and comments</label>
          </div>
          <div class="form-check form-check-inline small">
            <input id="local-agent-comment" v-model="canComment" class="form-check-input" type="checkbox" />
            <label class="form-check-label" for="local-agent-comment">Post comments</label>
          </div>
          <div class="form-check form-check-inline small">
            <input id="local-agent-write" v-model="canWriteTasks" class="form-check-input" type="checkbox" />
            <label class="form-check-label" for="local-agent-write">Create, update, move and claim tasks</label>
          </div>
        </div>
      </form>

      <div v-if="minted" class="api-key-reveal-panel mb-3">
        <p class="api-key-reveal-title">New key “{{ minted.name }}” — shown once</p>
        <div class="d-flex gap-2 align-items-center">
          <code class="text-break flex-grow-1">{{ minted.key }}</code>
          <button type="button" class="btn btn-sm btn-outline-secondary" @click="copy(minted.key)">Copy</button>
        </div>
      </div>

      <h4 class="h6 mb-2">Connect your agent</h4>
      <p v-if="!minted" class="small text-muted mb-2">
        Create a key first to get these with the key filled in, or replace <code>&lt;YOUR_KEY&gt;</code> yourself.
      </p>
      <ul class="nav nav-pills nav-sm small mb-2 flex-wrap" role="tablist">
        <li v-for="c in LOCAL_AGENT_CLIENTS" :key="c.id" class="nav-item">
          <button
            type="button"
            class="nav-link py-1 px-2"
            :class="{ active: client === c.id }"
            role="tab"
            :aria-selected="client === c.id"
            @click="client = c.id"
          >
            {{ c.label }}
          </button>
        </li>
      </ul>
      <div v-for="(s, i) in snippets" :key="`${client}-${i}`" class="mb-2">
        <div class="small fw-semibold mb-1">{{ s.title }}</div>
        <div class="d-flex gap-2 align-items-start">
          <pre class="small bg-body-tertiary p-2 rounded mb-0 flex-grow-1 text-wrap text-break"><code>{{ s.code }}</code></pre>
          <button type="button" class="btn btn-sm btn-outline-secondary" @click="copy(s.code)">Copy</button>
        </div>
        <div v-if="s.note" class="small text-muted mt-1">{{ s.note }}</div>
      </div>

      <details class="small mt-3">
        <summary>Optional: tell the agent where your tasks live (AGENTS.md, CLAUDE.md, GEMINI.md)</summary>
        <div class="d-flex gap-2 align-items-start mt-2">
          <pre class="small bg-body-tertiary p-2 rounded mb-0 flex-grow-1 text-wrap text-break"><code>{{ agentsMd }}</code></pre>
          <button type="button" class="btn btn-sm btn-outline-secondary" @click="copy(agentsMd)">Copy</button>
        </div>
      </details>
    </div>
  </div>
</template>
