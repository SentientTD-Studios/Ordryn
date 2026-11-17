<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import { api } from '@/api/client'
import { APIError, type PublicRequestForm } from '@/api/types'

const route = useRoute()
const token = String(route.params.token || '')

const form = ref<PublicRequestForm | null>(null)
const loading = ref(true)
const loadError = ref('')
const submitting = ref(false)
const submitError = ref('')
const filedId = ref<number | null>(null)
const summary = ref('')
const answers = reactive<Record<string, string>>({})

async function load() {
  loading.value = true
  loadError.value = ''
  try {
    form.value = await api.getRequestForm(token)
    document.title = form.value.title
  } catch (err) {
    loadError.value =
      err instanceof APIError && err.status === 404
        ? 'This form link is not valid. It may have been replaced or deleted.'
        : err instanceof APIError
          ? err.message
          : 'Could not load the form.'
  } finally {
    loading.value = false
  }
}

function reset() {
  summary.value = ''
  for (const k of Object.keys(answers)) delete answers[k]
  filedId.value = null
  submitError.value = ''
}

async function submit() {
  if (!form.value || submitting.value) return
  submitting.value = true
  submitError.value = ''
  try {
    const res = await api.submitRequestForm(token, summary.value, { ...answers })
    filedId.value = res.task_id
  } catch (err) {
    submitError.value = err instanceof APIError ? err.message : 'Could not send your request.'
  } finally {
    submitting.value = false
  }
}

onMounted(load)
</script>

<template>
  <div class="container py-4 request-form-page">
    <div v-if="loading" class="text-center text-muted py-5">
      <span class="spinner-border spinner-border-sm me-2" role="status" />Loading form…
    </div>
    <div v-else-if="loadError" class="alert alert-warning">
      {{ loadError }}
      <div class="mt-2"><RouterLink to="/">Go to your tasks</RouterLink></div>
    </div>

    <template v-else-if="form">
      <p class="small text-muted mb-1">{{ form.project_name }}</p>
      <h1 class="h3 mb-2">{{ form.title }}</h1>
      <p v-if="form.intro" class="text-muted request-form-intro">{{ form.intro }}</p>

      <div v-if="filedId" class="alert alert-success" role="status">
        <strong>Request sent.</strong> It's been filed as task #{{ filedId }} for the {{ form.project_name }} team.
        <div class="mt-2 d-flex gap-2">
          <button type="button" class="btn btn-sm btn-success" @click="reset">File another</button>
          <RouterLink to="/" class="btn btn-sm btn-outline-secondary">Back to your tasks</RouterLink>
        </div>
      </div>

      <form v-else class="card card-body" @submit.prevent="submit">
        <div class="mb-3">
          <label class="form-label fw-semibold" for="rf-summary">Summary <span class="text-danger" aria-hidden="true">*</span></label>
          <input
            id="rf-summary"
            v-model="summary"
            class="form-control"
            maxlength="140"
            required
            placeholder="A short title for your request"
          />
        </div>

        <div v-for="q in form.fields" :key="q.id" class="mb-3">
          <label class="form-label fw-semibold" :for="`rf-a-${q.id}`">
            {{ q.label }} <span v-if="q.required" class="text-danger" aria-hidden="true">*</span>
          </label>
          <textarea
            v-if="q.type === 'textarea'"
            :id="`rf-a-${q.id}`"
            v-model="answers[q.id]"
            class="form-control"
            rows="4"
            maxlength="4000"
            :required="q.required"
            :aria-describedby="q.help ? `rf-h-${q.id}` : undefined"
          />
          <select
            v-else-if="q.type === 'select'"
            :id="`rf-a-${q.id}`"
            v-model="answers[q.id]"
            class="form-select"
            :required="q.required"
            :aria-describedby="q.help ? `rf-h-${q.id}` : undefined"
          >
            <option value="">Choose…</option>
            <option v-for="o in q.options" :key="o" :value="o">{{ o }}</option>
          </select>
          <input
            v-else
            :id="`rf-a-${q.id}`"
            v-model="answers[q.id]"
            class="form-control"
            :type="q.type === 'date' ? 'date' : q.type === 'number' ? 'number' : 'text'"
            :step="q.type === 'number' ? 'any' : undefined"
            maxlength="4000"
            :required="q.required"
            :aria-describedby="q.help ? `rf-h-${q.id}` : undefined"
          />
          <div v-if="q.help" :id="`rf-h-${q.id}`" class="form-text">{{ q.help }}</div>
        </div>

        <div v-if="submitError" class="alert alert-danger py-2" role="alert">{{ submitError }}</div>
        <div>
          <button type="submit" class="btn btn-primary" :disabled="submitting || !summary.trim()">
            {{ submitting ? 'Sending…' : 'Send request' }}
          </button>
        </div>
      </form>
    </template>
  </div>
</template>

<style scoped>
.request-form-page {
  max-width: 720px;
}
.request-form-intro {
  white-space: pre-line;
}
</style>
