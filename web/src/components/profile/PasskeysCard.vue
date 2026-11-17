<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { api } from '@/api/client'
import { APIError, type Passkey } from '@/api/types'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import {
  isPasskeyCancel,
  passkeysSupported,
  registrationToJSON,
  toCreationOptions,
} from '@/utils/webauthn'

const { push } = useToast()
const { askConfirm } = useConfirm()

const supported = passkeysSupported()
const keys = ref<Passkey[]>([])
const loading = ref(true)
const adding = ref(false)
const showAdd = ref(false)
const password = ref('')
const name = ref('')
const renamingId = ref<number | null>(null)
const renameDraft = ref('')

async function load() {
  loading.value = true
  try {
    keys.value = await api.listPasskeys()
  } catch (err) {
    push(err instanceof APIError ? err.message : 'Failed to load passkeys', 'error')
  } finally {
    loading.value = false
  }
}

onMounted(load)

function defaultName() {
  const ua = navigator.userAgent
  if (/iPhone|iPad/.test(ua)) return 'iPhone or iPad'
  if (/Android/.test(ua)) return 'Android'
  if (/Mac/.test(ua)) return 'Mac'
  if (/Windows/.test(ua)) return 'Windows'
  return 'Passkey'
}

function openAdd() {
  showAdd.value = true
  password.value = ''
  name.value = defaultName()
}

async function add() {
  adding.value = true
  try {
    const { token, options } = await api.passkeyRegisterBegin(password.value)
    const cred = (await navigator.credentials.create(toCreationOptions(options))) as PublicKeyCredential | null
    if (!cred) return
    await api.passkeyRegisterFinish(token, name.value, registrationToJSON(cred))
    push('Passkey added. You can now sign in with it.', 'success')
    showAdd.value = false
    password.value = ''
    await load()
  } catch (err) {
    if (isPasskeyCancel(err)) return
    if (err instanceof DOMException && err.name === 'InvalidStateError') {
      push('This device already has a passkey for your account.', 'error')
      return
    }
    push(err instanceof APIError ? err.message : 'Could not add the passkey', 'error')
  } finally {
    adding.value = false
  }
}

function startRename(k: Passkey) {
  renamingId.value = k.id
  renameDraft.value = k.name
}

async function saveRename(k: Passkey) {
  try {
    await api.renamePasskey(k.id, renameDraft.value)
    renamingId.value = null
    await load()
  } catch (err) {
    push(err instanceof APIError ? err.message : 'Could not rename the passkey', 'error')
  }
}

async function remove(k: Passkey) {
  const ok = await askConfirm({
    title: `Remove “${k.name}”?`,
    message: 'It can no longer be used to sign in here. You may also want to delete it from your device or password manager.',
    confirmLabel: 'Remove',
    warning: true,
  })
  if (!ok) return
  try {
    await api.deletePasskey(k.id)
    push('Passkey removed', 'success')
    await load()
  } catch (err) {
    push(err instanceof APIError ? err.message : 'Could not remove the passkey', 'error')
  }
}

function when(iso: string | null) {
  if (!iso) return 'never'
  return new Date(iso).toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric' })
}
</script>

<template>
  <div id="passkeys-section" class="card mb-4">
    <div class="card-header"><h3 class="card-title mb-0">Passkeys</h3></div>
    <div class="card-body">
      <p class="small text-muted">
        Sign in with your fingerprint, face, or device PIN instead of a password. A passkey counts as two-factor
        authentication on its own.
      </p>
      <p v-if="!supported" class="small text-warning mb-0">This browser doesn't support passkeys.</p>
      <template v-else>
        <p v-if="loading" class="small text-muted">Loading…</p>
        <ul v-else-if="keys.length" class="list-group mb-3">
          <li v-for="k in keys" :key="k.id" class="list-group-item d-flex flex-wrap align-items-center gap-2">
            <i class="bi bi-fingerprint text-muted" aria-hidden="true" />
            <form v-if="renamingId === k.id" class="d-flex gap-1" @submit.prevent="saveRename(k)">
              <input v-model="renameDraft" class="form-control form-control-sm" maxlength="60" aria-label="Passkey name" />
              <button type="submit" class="btn btn-sm btn-primary">Save</button>
              <button type="button" class="btn btn-sm btn-outline-secondary" @click="renamingId = null">Cancel</button>
            </form>
            <template v-else>
              <strong>{{ k.name }}</strong>
              <span class="small text-muted">added {{ when(k.created_at) }} · last used {{ when(k.last_used_at) }}</span>
              <span class="ms-auto d-flex gap-1">
                <button type="button" class="btn btn-sm btn-outline-secondary" @click="startRename(k)">Rename</button>
                <button type="button" class="btn btn-sm btn-outline-danger" @click="remove(k)">Remove</button>
              </span>
            </template>
          </li>
        </ul>
        <p v-else class="small text-muted">No passkeys yet.</p>

        <button v-if="!showAdd" type="button" class="btn btn-sm btn-primary" @click="openAdd">Add a passkey</button>
        <form v-else class="row g-2 align-items-end" @submit.prevent="add">
          <div class="col-sm-5">
            <label class="form-label small mb-0" for="pk-name">Name</label>
            <input id="pk-name" v-model="name" class="form-control form-control-sm" maxlength="60" />
          </div>
          <div class="col-sm-4">
            <label class="form-label small mb-0" for="pk-password">Current password</label>
            <input
              id="pk-password"
              v-model="password"
              type="password"
              class="form-control form-control-sm"
              autocomplete="current-password"
            />
          </div>
          <div class="col-sm-3 d-flex gap-1">
            <button type="submit" class="btn btn-sm btn-primary" :disabled="adding">
              {{ adding ? 'Waiting…' : 'Continue' }}
            </button>
            <button type="button" class="btn btn-sm btn-outline-secondary" :disabled="adding" @click="showAdd = false">
              Cancel
            </button>
          </div>
          <div class="col-12 form-text">
            Signed in with single sign-on? Leave the password empty.
          </div>
        </form>
      </template>
    </div>
  </div>
</template>
