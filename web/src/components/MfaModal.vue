<script setup lang="ts">
import { onMounted, onUnmounted, ref, watch } from 'vue'
import { api } from '@/api/client'
import { APIError } from '@/api/types'
import { useToast } from '@/composables/useToast'
import { useBodyScrollLock } from '@/composables/useBodyScrollLock'
import QRCode from 'qrcode'

const open = defineModel<boolean>({ default: false })
const emit = defineEmits<{
  updated: [status: { enabled: boolean; recovery_codes_remaining: number }]
}>()

const { push } = useToast()

type ConfirmAction = 'disable' | 'regenerate' | null
const confirmAction = ref<ConfirmAction>(null)

const enabled = ref(false)
const recoveryRemaining = ref(0)
const busy = ref(false)
const setupSecret = ref('')
const qrDataUrl = ref('')
const setupCode = ref('')
const disableCode = ref('')
const regenerateCode = ref('')
const recoveryCodes = ref<string[] | null>(null)

const showingRecoveryCodes = () => !!recoveryCodes.value?.length

async function refreshStatus() {
  const status = await api.getMFA()
  enabled.value = status.enabled
  recoveryRemaining.value = status.recovery_codes_remaining
  emit('updated', status)
}

function resetTransient() {
  setupSecret.value = ''
  qrDataUrl.value = ''
  setupCode.value = ''
  disableCode.value = ''
  regenerateCode.value = ''
  confirmAction.value = null
}

async function onOpen() {
  resetTransient()
  recoveryCodes.value = null
  busy.value = true
  try {
    await refreshStatus()
  } catch (err) {
    push(err instanceof APIError ? err.message : 'Could not load MFA status', 'error')
    open.value = false
  } finally {
    busy.value = false
  }
}

function requestClose() {
  if (showingRecoveryCodes()) {
    push('Save your recovery codes before closing', 'error')
    return
  }
  open.value = false
}

async function startSetup() {
  busy.value = true
  try {
    const setup = await api.setupMFA()
    setupSecret.value = setup.secret
    setupCode.value = ''
    qrDataUrl.value = await QRCode.toDataURL(setup.otpauth_url, { width: 192, margin: 1 })
  } catch (err) {
    push(err instanceof APIError ? err.message : 'Could not start MFA setup', 'error')
  } finally {
    busy.value = false
  }
}

function cancelSetup() {
  setupSecret.value = ''
  qrDataUrl.value = ''
  setupCode.value = ''
}

async function enableMFA() {
  if (!setupCode.value.trim()) return
  busy.value = true
  try {
    const result = await api.enableMFA(setupCode.value.trim())
    recoveryCodes.value = result.recovery_codes
    cancelSetup()
    await refreshStatus()
    push('Two-factor authentication enabled', 'success')
  } catch (err) {
    push(err instanceof APIError ? err.message : 'Could not enable MFA', 'error')
  } finally {
    busy.value = false
  }
}

function requestDisable() {
  if (!disableCode.value.trim()) return
  confirmAction.value = 'disable'
}

function requestRegenerate() {
  if (!regenerateCode.value.trim()) return
  confirmAction.value = 'regenerate'
}

function cancelConfirm() {
  confirmAction.value = null
}

async function disableMFA() {
  if (!disableCode.value.trim()) return
  busy.value = true
  try {
    await api.disableMFA(disableCode.value.trim())
    disableCode.value = ''
    regenerateCode.value = ''
    recoveryCodes.value = null
    confirmAction.value = null
    await refreshStatus()
    push('Two-factor authentication turned off', 'info')
  } catch (err) {
    push(err instanceof APIError ? err.message : 'Could not turn off MFA', 'error')
  } finally {
    busy.value = false
  }
}

async function regenerateRecoveryCodes() {
  if (!regenerateCode.value.trim()) return
  busy.value = true
  try {
    const result = await api.regenerateMFARecoveryCodes(regenerateCode.value.trim())
    recoveryCodes.value = result.recovery_codes
    regenerateCode.value = ''
    confirmAction.value = null
    await refreshStatus()
    push('New recovery codes generated', 'success')
  } catch (err) {
    push(err instanceof APIError ? err.message : 'Could not regenerate recovery codes', 'error')
  } finally {
    busy.value = false
  }
}

async function copyRecoveryCodes() {
  if (!recoveryCodes.value?.length) return
  try {
    await navigator.clipboard.writeText(recoveryCodes.value.join('\n'))
    push('Recovery codes copied', 'success')
  } catch {
    push('Could not copy recovery codes', 'error')
  }
}

function downloadRecoveryCodes() {
  if (!recoveryCodes.value?.length) return
  const blob = new Blob([recoveryCodes.value.join('\n') + '\n'], { type: 'text/plain' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = 'gotodo-recovery-codes.txt'
  a.click()
  URL.revokeObjectURL(url)
}

function dismissRecoveryCodes() {
  recoveryCodes.value = null
}

function onKeydown(e: KeyboardEvent) {
  if (!open.value) return
  if (e.key === 'Escape') {
    e.preventDefault()
    if (confirmAction.value) {
      cancelConfirm()
      return
    }
    requestClose()
  }
}

useBodyScrollLock(open)

watch(open, (isOpen) => {
  if (isOpen) void onOpen()
})

onMounted(() => {
  window.addEventListener('keydown', onKeydown)
})

onUnmounted(() => {
  window.removeEventListener('keydown', onKeydown)
})
</script>

<template>
  <Teleport to="body">
    <div
      v-if="open"
      id="mfaModal"
      class="modal fade show d-block"
      tabindex="-1"
      role="dialog"
      aria-modal="true"
      aria-labelledby="mfaModalLabel"
      @click.self="requestClose"
    >
      <div class="modal-dialog modal-dialog-centered modal-dialog-scrollable" @click.stop>
        <div class="modal-content">
          <div class="modal-header">
            <h5 id="mfaModalLabel" class="modal-title">Two-factor authentication</h5>
            <button type="button" class="btn-close" aria-label="Close" @click="requestClose" />
          </div>
          <div class="modal-body">
            <p class="text-muted small">
              Authenticator-app codes (TOTP) plus recovery codes. Password reset does not turn this off.
            </p>

            <div v-if="recoveryCodes?.length" class="alert alert-warning">
              <p class="mb-2">
                Save these recovery codes now. They will not be shown again. Each code can be used once if you lose
                access to your authenticator.
              </p>
              <pre class="mb-3 bg-body-secondary p-2 rounded small">{{ recoveryCodes.join('\n') }}</pre>
              <div class="d-flex flex-wrap gap-2">
                <button type="button" class="btn btn-sm btn-outline-primary" @click="copyRecoveryCodes">Copy</button>
                <button type="button" class="btn btn-sm btn-outline-primary" @click="downloadRecoveryCodes">
                  Download
                </button>
                <button type="button" class="btn btn-sm btn-outline-secondary" @click="dismissRecoveryCodes">
                  I’ve saved them
                </button>
              </div>
            </div>

            <template v-else-if="enabled">
              <p class="mb-3">
                <span class="badge text-bg-success">Enabled</span>
                <span class="ms-2">
                  {{ recoveryRemaining }} unused recovery code{{ recoveryRemaining === 1 ? '' : 's' }} remaining.
                </span>
              </p>

              <div v-if="confirmAction === 'disable'" class="alert alert-danger">
                <p class="mb-2">
                  Turn off two-factor authentication? You will only need your password to sign in. Recovery codes will
                  be deleted.
                </p>
                <div class="d-flex flex-wrap gap-2">
                  <button type="button" class="btn btn-danger" :disabled="busy" @click="disableMFA">Turn off</button>
                  <button type="button" class="btn btn-outline-secondary" :disabled="busy" @click="cancelConfirm">
                    Cancel
                  </button>
                </div>
              </div>
              <div v-else-if="confirmAction === 'regenerate'" class="alert alert-warning">
                <p class="mb-2">
                  Replace recovery codes? Your existing unused codes will stop working. Save the new codes somewhere
                  safe.
                </p>
                <div class="d-flex flex-wrap gap-2">
                  <button type="button" class="btn btn-primary" :disabled="busy" @click="regenerateRecoveryCodes">
                    Replace codes
                  </button>
                  <button type="button" class="btn btn-outline-secondary" :disabled="busy" @click="cancelConfirm">
                    Cancel
                  </button>
                </div>
              </div>
              <template v-else>
                <form class="mb-4" @submit.prevent="requestDisable">
                  <label class="form-label" for="mfa-disable-code">Turn off with authenticator or recovery code</label>
                  <div class="row g-2">
                    <div class="col-sm-8">
                      <input
                        id="mfa-disable-code"
                        v-model="disableCode"
                        type="text"
                        class="form-control"
                        autocomplete="one-time-code"
                        :disabled="busy"
                      />
                    </div>
                    <div class="col-sm-4">
                      <button
                        type="submit"
                        class="btn btn-outline-danger w-100"
                        :disabled="busy || !disableCode.trim()"
                      >
                        Turn off MFA
                      </button>
                    </div>
                  </div>
                </form>
                <form @submit.prevent="requestRegenerate">
                  <label class="form-label" for="mfa-regen-code">Replace recovery codes</label>
                  <div class="row g-2">
                    <div class="col-sm-8">
                      <input
                        id="mfa-regen-code"
                        v-model="regenerateCode"
                        type="text"
                        class="form-control"
                        autocomplete="one-time-code"
                        :disabled="busy"
                      />
                    </div>
                    <div class="col-sm-4">
                      <button
                        type="submit"
                        class="btn btn-outline-primary w-100"
                        :disabled="busy || !regenerateCode.trim()"
                      >
                        Generate new codes
                      </button>
                    </div>
                  </div>
                </form>
              </template>
            </template>

            <template v-else-if="setupSecret">
              <p>Scan this QR code with your authenticator app, or enter the secret manually.</p>
              <div class="mb-3 text-center">
                <img v-if="qrDataUrl" :src="qrDataUrl" width="192" height="192" alt="Authenticator QR code" />
              </div>
              <div class="mb-3">
                <label class="form-label" for="mfa-secret">Secret</label>
                <input id="mfa-secret" class="form-control font-monospace" :value="setupSecret" readonly />
              </div>
              <form @submit.prevent="enableMFA">
                <label class="form-label" for="mfa-setup-code">Enter the 6-digit code to confirm</label>
                <div class="row g-2">
                  <div class="col-sm-8">
                    <input
                      id="mfa-setup-code"
                      v-model="setupCode"
                      type="text"
                      class="form-control"
                      inputmode="numeric"
                      autocomplete="one-time-code"
                      required
                      :disabled="busy"
                    />
                  </div>
                  <div class="col-sm-4 d-flex gap-2">
                    <button type="submit" class="btn btn-primary flex-grow-1" :disabled="busy">Enable</button>
                    <button type="button" class="btn btn-outline-secondary" :disabled="busy" @click="cancelSetup">
                      Cancel
                    </button>
                  </div>
                </div>
              </form>
            </template>

            <template v-else>
              <button type="button" class="btn btn-primary" :disabled="busy" @click="startSetup">
                {{ busy ? 'Starting…' : 'Set up authenticator' }}
              </button>
            </template>
          </div>
        </div>
      </div>
    </div>
    <div v-if="open" class="modal-backdrop fade show" @click="requestClose" />
  </Teleport>
</template>
