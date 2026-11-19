<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { api } from '@/api/client'
import type { ChatServer, ChatServerWithSecret } from '@/api/types'
import { APIError } from '@/api/types'
import { pathPrefix } from '@/base'
import AdminSubnav from '@/components/AdminSubnav.vue'
import { useConfirm } from '@/composables/useConfirm'
import { useToast } from '@/composables/useToast'

const toast = useToast()
const { askConfirm } = useConfirm()

const signinEnabled = ref(false)
const servers = ref<ChatServer[]>([])
const loaded = ref(false)
const saving = ref(false)

const editingId = ref<number | null>(null)
const formName = ref('')
const formURL = ref('')
const formSync = ref(false)
const formEvents = ref(false)
const formCreate = ref(false)
const editing = computed(() => servers.value.find((s) => s.id === editingId.value) || null)

/** Credentials to hand to the chat server's admin; shown once after add or rotate. */
const issued = ref<ChatServerWithSecret | null>(null)
const ordrynURL = `${window.location.origin}${pathPrefix()}`
const envLines = computed(() =>
  issued.value
    ? `ORDRYN_URL=${ordrynURL}\nORDRYN_CLIENT_ID=${issued.value.client_id}\nORDRYN_CLIENT_SECRET=${issued.value.client_secret}`
    : '',
)

function fail(err: unknown, fallback: string) {
  toast.push(err instanceof APIError ? err.message : fallback, 'error')
}

async function load() {
  try {
    const data = await api.getAdminChat()
    signinEnabled.value = data.signin_enabled
    servers.value = data.servers || []
  } catch (err) {
    fail(err, 'Failed to load chat servers')
  } finally {
    loaded.value = true
  }
}

async function toggleSignIn() {
  const next = !signinEnabled.value
  try {
    const data = await api.setChatSignIn(next)
    signinEnabled.value = data.signin_enabled
    servers.value = data.servers || []
    toast.push(next ? 'Chat sign-in turned on' : 'Chat sign-in turned off', 'success')
  } catch (err) {
    fail(err, 'Could not save')
  }
}

function resetForm() {
  editingId.value = null
  formName.value = ''
  formURL.value = ''
  formSync.value = false
  formEvents.value = false
  formCreate.value = false
}

function startEdit(s: ChatServer) {
  editingId.value = s.id
  formName.value = s.name
  formURL.value = s.url
  formSync.value = s.directory_sync
  formEvents.value = s.task_events
  formCreate.value = s.task_create
}

async function save() {
  const name = formName.value.trim()
  const url = formURL.value.trim()
  if (!name || !url) return
  saving.value = true
  try {
    if (editing.value) {
      await api.updateChatServer(editing.value.id, {
        name,
        url,
        directory_sync: formSync.value,
        task_events: formEvents.value,
        task_create: formEvents.value && formCreate.value,
      })
      toast.push('Chat server updated', 'success')
    } else {
      issued.value = await api.addChatServer({
        name,
        url,
        directory_sync: formSync.value,
        task_events: formEvents.value,
        task_create: formEvents.value && formCreate.value,
      })
      toast.push('Chat server added', 'success')
    }
    resetForm()
    await load()
  } catch (err) {
    fail(err, 'Could not save chat server')
  } finally {
    saving.value = false
  }
}

async function setFlag(s: ChatServer, patch: { enabled?: boolean; directory_sync?: boolean; task_events?: boolean; task_create?: boolean }) {
  try {
    await api.updateChatServer(s.id, patch)
    await load()
  } catch (err) {
    fail(err, 'Could not update chat server')
  }
}

async function rotate(s: ChatServer) {
  const ok = await askConfirm({
    title: `New secret for ${s.name}?`,
    message: 'The current secret stops working right away. Sign-in on that chat server fails until its admin sets the new one.',
    confirmLabel: 'Issue new secret',
  })
  if (!ok) return
  try {
    issued.value = await api.rotateChatServerSecret(s.id)
    await load()
  } catch (err) {
    fail(err, 'Could not issue a new secret')
  }
}

async function remove(s: ChatServer) {
  const ok = await askConfirm({
    title: `Remove ${s.name}?`,
    message: `${s.url} will no longer be able to use Sign in with Ordryn.`,
    confirmLabel: 'Remove',
    danger: true,
  })
  if (!ok) return
  try {
    await api.deleteChatServer(s.id)
    if (editingId.value === s.id) resetForm()
    if (issued.value?.id === s.id) issued.value = null
    await load()
  } catch (err) {
    fail(err, 'Could not remove chat server')
  }
}

async function copyEnv() {
  try {
    await navigator.clipboard.writeText(envLines.value)
    toast.push('Copied', 'success')
  } catch {
    toast.push('Copy failed; select the text instead', 'error')
  }
}

function fmt(d?: string | null) {
  return d ? new Date(d).toLocaleString() : 'never'
}

onMounted(load)
</script>

<template>
  <div class="container mt-3">
    <AdminSubnav />
    <h1>Chat servers</h1>
    <p class="text-muted">
      Ordryn Chat servers listed here can offer <strong>Sign in with Ordryn</strong>. Each one gets a client ID and
      secret, which its admin sets on the chat server. Any other server is refused, and people approving a sign-in
      see which chat server is asking. Sign-in only shares someone's profile and organizations, never their projects
      or tasks.
    </p>
    <p class="text-muted">
      With <strong>Directory sync</strong> allowed, a chat server can also read the user list (names, emails, ban
      status, site roles) and organization memberships with the same credentials. It uses them to disable people who
      are banned or removed here and to keep organization channels current. Nothing else is reachable with them.
    </p>
    <p class="text-muted">
      With <strong>Project updates</strong> allowed, people on that chat server can link a private channel they own
      to a project they can manage here, and the project's task activity is posted into it. Each link is made on
      behalf of one person and pauses on its own if they can no longer manage the project.
    </p>

    <div class="card card-body mb-4">
      <div class="form-check form-switch">
        <input
          id="chat-signin"
          class="form-check-input"
          type="checkbox"
          role="switch"
          :checked="signinEnabled"
          :disabled="!loaded"
          @change="toggleSignIn"
        />
        <label class="form-check-label" for="chat-signin">
          <strong>Allow chat servers to use Sign in with Ordryn</strong>
        </label>
      </div>
      <div class="form-text">Turning this off stops sign-in, directory sync and project updates on every chat server below, without removing them.</div>
    </div>

    <div v-if="issued" class="alert alert-warning">
      <h2 class="h6">Credentials for {{ issued.name }}</h2>
      <p class="small mb-2">
        Copy these now: the secret won't be shown again. Put them in the chat server's environment file
        (<code>/etc/ordryn-chat/chat.env</code>) and restart it.
      </p>
      <pre class="bg-body border rounded p-2 small mb-2"><code>{{ envLines }}</code></pre>
      <div class="d-flex gap-2">
        <button class="btn btn-sm btn-outline-secondary" type="button" @click="copyEnv">Copy</button>
        <button class="btn btn-sm btn-outline-secondary" type="button" @click="issued = null">Done</button>
      </div>
    </div>

    <div class="row g-4">
      <div class="col-lg-7">
        <div class="card">
          <ul class="list-group list-group-flush">
            <li v-for="s in servers" :key="s.id" class="list-group-item">
              <div class="d-flex flex-wrap justify-content-between align-items-start gap-2">
                <div>
                  <strong>{{ s.name }}</strong>
                  <span v-if="!s.enabled" class="badge text-bg-secondary ms-1">disabled</span>
                  <span v-if="s.directory_sync" class="badge text-bg-info ms-1">directory sync</span>
                  <span v-if="s.task_events" class="badge text-bg-info ms-1">project updates</span>
                  <span v-if="s.task_events && s.task_create" class="badge text-bg-info ms-1">task creation</span>
                  <div class="small"><code>{{ s.url }}</code></div>
                  <div class="small text-muted">
                    Client ID <code>{{ s.client_id }}</code> · secret {{ s.secret_hint }}
                  </div>
                  <div class="small text-muted">Last sign-in {{ fmt(s.last_used_at) }}</div>
                </div>
                <div class="d-flex flex-wrap gap-2">
                  <button class="btn btn-sm btn-outline-secondary" type="button" @click="startEdit(s)">Edit</button>
                  <button class="btn btn-sm btn-outline-secondary" type="button" @click="setFlag(s, { enabled: !s.enabled })">
                    {{ s.enabled ? 'Disable' : 'Enable' }}
                  </button>
                  <button
                    class="btn btn-sm btn-outline-secondary"
                    type="button"
                    @click="setFlag(s, { directory_sync: !s.directory_sync })"
                  >
                    {{ s.directory_sync ? 'Stop directory sync' : 'Allow directory sync' }}
                  </button>
                  <button
                    class="btn btn-sm btn-outline-secondary"
                    type="button"
                    @click="setFlag(s, { task_events: !s.task_events })"
                  >
                    {{ s.task_events ? 'Stop project updates' : 'Allow project updates' }}
                  </button>
                  <button
                    v-if="s.task_events"
                    class="btn btn-sm btn-outline-secondary"
                    type="button"
                    @click="setFlag(s, { task_create: !s.task_create })"
                  >
                    {{ s.task_create ? 'Stop task creation' : 'Allow task creation' }}
                  </button>
                  <button class="btn btn-sm btn-outline-secondary" type="button" @click="rotate(s)">New secret</button>
                  <button class="btn btn-sm btn-outline-danger" type="button" @click="remove(s)">Remove</button>
                </div>
              </div>
            </li>
            <li v-if="loaded && !servers.length" class="list-group-item text-muted">No chat servers yet.</li>
          </ul>
        </div>
      </div>

      <div class="col-lg-5">
        <form class="card card-body" @submit.prevent="save">
          <h2 class="h5">{{ editing ? `Edit ${editing.name}` : 'Add a chat server' }}</h2>
          <div class="mb-3">
            <label class="form-label small mb-0" for="chat-server-name">Name</label>
            <input
              id="chat-server-name"
              v-model="formName"
              type="text"
              class="form-control"
              maxlength="60"
              required
              placeholder="e.g. Company chat"
            />
            <div class="form-text">Shown to people when they approve a sign-in.</div>
          </div>
          <div class="mb-3">
            <label class="form-label small mb-0" for="chat-server-url">Address</label>
            <input
              id="chat-server-url"
              v-model="formURL"
              type="url"
              class="form-control"
              required
              placeholder="https://chat.ordryn.com"
            />
            <div class="form-text">The chat server's https address, without a path.</div>
          </div>
          <div class="form-check mb-3">
            <input id="chat-server-sync" v-model="formSync" class="form-check-input" type="checkbox" />
            <label class="form-check-label" for="chat-server-sync">Allow directory sync</label>
            <div class="form-text">Lets it read users, site roles and organization memberships.</div>
          </div>
          <div class="form-check mb-3">
            <input id="chat-server-events" v-model="formEvents" class="form-check-input" type="checkbox" />
            <label class="form-check-label" for="chat-server-events">Allow project updates</label>
            <div class="form-text">
              Lets its users link private channels to projects they manage, and posts those projects' task activity there.
            </div>
          </div>
          <div class="form-check mb-3">
            <input
              id="chat-server-create"
              v-model="formCreate"
              class="form-check-input"
              type="checkbox"
              :disabled="!formEvents"
            />
            <label class="form-check-label" for="chat-server-create">Allow task creation</label>
            <div class="form-text">
              Lets its users turn a chat message into a task in a linked project. Ordryn still checks that they can add
              tasks to that project. Needs project updates.
            </div>
          </div>
          <div class="d-flex gap-2">
            <button class="btn btn-primary" type="submit" :disabled="saving || !formName.trim() || !formURL.trim()">
              {{ editing ? 'Save' : 'Add and create credentials' }}
            </button>
            <button v-if="editing" class="btn btn-outline-secondary" type="button" @click="resetForm">Cancel</button>
          </div>
        </form>
      </div>
    </div>
  </div>
</template>
