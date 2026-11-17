<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { api } from '@/api/client'
import type { OffboardInput, OffboardingPreview, Person } from '@/api/types'
import { APIError } from '@/api/types'
import { useToast } from '@/composables/useToast'
import { personLabel } from '@/utils/personLabel'

// Deactivating someone who's leaving: shows what they still hold and hands it off.
const props = defineProps<{
  person: Person
  /** Active people the work can go to. */
  candidates: Person[]
}>()
const emit = defineEmits<{ close: []; done: [] }>()

const { push } = useToast()
const preview = ref<OffboardingPreview | null>(null)
const busy = ref(false)
const form = reactive<Omit<OffboardInput, 'successor_id'> & { successor_id: number | null }>({
  successor_id: null,
  claims: 'keep',
  transfer_projects: false,
  revoke_keys: true,
})

const who = computed(() => personLabel(props.person))
const others = computed(() => props.candidates.filter((p) => p.id !== props.person.id && p.active))
const needsSuccessor = computed(() => form.claims === 'reassign' || form.transfer_projects)
const keyCount = computed(() => (preview.value ? preview.value.personal_keys + preview.value.project_keys : 0))

onMounted(async () => {
  try {
    preview.value = await api.getOffboardingPreview(props.person.id)
    if (preview.value.claimed_tasks > 0) form.claims = 'release'
  } catch (err) {
    push(err instanceof APIError ? err.message : 'Could not load what they hold', 'error')
  }
})

async function submit() {
  if (needsSuccessor.value && !form.successor_id) {
    push('Choose who takes over their work', 'error')
    return
  }
  busy.value = true
  try {
    const res = await api.offboardPerson(props.person.id, {
      successor_id: form.successor_id ?? undefined,
      claims: form.claims,
      transfer_projects: form.transfer_projects,
      revoke_keys: form.revoke_keys,
    })
    const parts = [`${who.value} deactivated`]
    if (res.claims_reassigned) parts.push(`${res.claims_reassigned} task(s) reassigned`)
    if (res.claims_released) parts.push(`${res.claims_released} claim(s) released`)
    if (res.projects_transferred) parts.push(`${res.projects_transferred} project(s) handed over`)
    if (res.keys_revoked) parts.push(`${res.keys_revoked} key(s) revoked`)
    push(parts.join(' · '), 'info')
    emit('done')
  } catch (err) {
    push(err instanceof APIError ? err.message : 'Deactivation failed', 'error')
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <Teleport to="body">
    <div class="modal fade show d-block" tabindex="-1" role="dialog" aria-modal="true" aria-labelledby="offboard-title" @click.self="emit('close')">
      <div class="modal-dialog modal-dialog-centered" @click.stop>
        <form class="modal-content" @submit.prevent="submit">
          <div class="modal-header">
            <h5 id="offboard-title" class="modal-title">Deactivate {{ who }}</h5>
            <button type="button" class="btn-close" aria-label="Close" @click="emit('close')" />
          </div>
          <div class="modal-body">
            <p class="small">
              {{ who }} will be signed out everywhere and can't sign in. Their tasks, comments, and history stay, and you
              can reactivate them later.
            </p>
            <p v-if="!preview" class="text-muted small">Checking what they hold…</p>
            <ul v-else class="small mb-3">
              <li>{{ preview.claimed_tasks }} open claimed task{{ preview.claimed_tasks === 1 ? '' : 's' }}</li>
              <li>{{ preview.owned_projects }} project{{ preview.owned_projects === 1 ? '' : 's' }} they own</li>
              <li>{{ keyCount }} active API key{{ keyCount === 1 ? '' : 's' }}</li>
              <li v-if="preview.agents_created">
                {{ preview.agents_created }} AI agent{{ preview.agents_created === 1 ? '' : 's' }} they added (these keep
                working; project managers can pause them)
              </li>
            </ul>

            <div class="mb-3">
              <label class="form-label" for="offboard-successor">Hand their work to</label>
              <select id="offboard-successor" v-model="form.successor_id" class="form-select">
                <option :value="null">Nobody</option>
                <option v-for="p in others" :key="p.id" :value="p.id">{{ personLabel(p) }}</option>
              </select>
            </div>

            <fieldset v-if="preview?.claimed_tasks" class="mb-3">
              <legend class="form-label fs-6">Their claimed tasks</legend>
              <div class="form-check">
                <input id="claims-release" v-model="form.claims" type="radio" value="release" class="form-check-input" />
                <label class="form-check-label" for="claims-release">Release them so anyone can pick them up</label>
              </div>
              <div class="form-check">
                <input id="claims-reassign" v-model="form.claims" type="radio" value="reassign" class="form-check-input" />
                <label class="form-check-label" for="claims-reassign">
                  Reassign to the person above (released where they can't claim)
                </label>
              </div>
              <div class="form-check">
                <input id="claims-keep" v-model="form.claims" type="radio" value="keep" class="form-check-input" />
                <label class="form-check-label" for="claims-keep">Leave them as they are</label>
              </div>
            </fieldset>

            <div v-if="preview?.owned_projects" class="form-check mb-2">
              <input id="offboard-projects" v-model="form.transfer_projects" type="checkbox" class="form-check-input" />
              <label class="form-check-label" for="offboard-projects">Make the person above an owner of their projects</label>
            </div>
            <div class="form-check">
              <input id="offboard-keys" v-model="form.revoke_keys" type="checkbox" class="form-check-input" />
              <label class="form-check-label" for="offboard-keys">Revoke their API keys for good</label>
              <div class="form-text">Keys stop working while the account is deactivated either way; revoked keys stay dead if it's reactivated.</div>
            </div>
          </div>
          <div class="modal-footer">
            <button type="button" class="btn btn-secondary" @click="emit('close')">Cancel</button>
            <button type="submit" class="btn btn-danger" :disabled="busy || !preview">
              {{ busy ? 'Deactivating…' : 'Deactivate' }}
            </button>
          </div>
        </form>
      </div>
    </div>
    <div class="modal-backdrop fade show" />
  </Teleport>
</template>
