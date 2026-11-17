<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { api } from '@/api/client'
import {
  APIError,
  type Project,
  type RequestForm,
  type RequestFormField,
  type RequestFormFieldType,
  type RequestFormInput,
  type Tag,
} from '@/api/types'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'

const props = defineProps<{ project: Project }>()
const emit = defineEmits<{ changed: [] }>()

const toast = useToast()
const { askConfirm } = useConfirm()

const FIELD_TYPES: { id: RequestFormFieldType; label: string }[] = [
  { id: 'text', label: 'Short text' },
  { id: 'textarea', label: 'Paragraph' },
  { id: 'select', label: 'Choice' },
  { id: 'date', label: 'Date' },
  { id: 'number', label: 'Number' },
]
const PRIORITIES = ['None', 'Low', 'Medium', 'High']

const forms = ref<RequestForm[]>([])
const tags = ref<Tag[]>([])
const loading = ref(true)
const saving = ref(false)
/** Form being edited: an id, 'new', or null. */
const editing = ref<number | 'new' | null>(null)

type DraftField = RequestFormField & { optionsText: string }
type Draft = Omit<RequestFormInput, 'fields'> & { fields: DraftField[] }
const draft = ref<Draft>(emptyDraft())

function emptyDraft(): Draft {
  return { title: '', intro: '', fields: [] as DraftField[], priority: 0, tag_ids: [] as number[], enabled: true }
}

function toDraftField(f: RequestFormField): DraftField {
  return { ...f, optionsText: (f.options ?? []).join('\n') }
}

async function load() {
  loading.value = true
  try {
    const [f, t] = await Promise.all([
      api.listRequestForms(props.project.id),
      api.listTags({ project_id: props.project.id }).catch(() => [] as Tag[]),
    ])
    forms.value = f
    tags.value = t.filter((x) => x.project_id === props.project.id)
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Could not load forms', 'error')
  } finally {
    loading.value = false
  }
}

onMounted(load)

function linkFor(f: RequestForm) {
  return `${window.location.origin}${f.path}`
}

async function copyLink(f: RequestForm) {
  try {
    await navigator.clipboard.writeText(linkFor(f))
    toast.push('Link copied', 'success')
  } catch {
    toast.push('Could not copy; select the link and copy it instead', 'error')
  }
}

function startNew() {
  draft.value = emptyDraft()
  draft.value.title = `${props.project.name} requests`
  draft.value.fields = [toDraftField({ id: '', label: 'Details', type: 'textarea', required: true })]
  editing.value = 'new'
}

function startEdit(f: RequestForm) {
  draft.value = {
    title: f.title,
    intro: f.intro,
    fields: f.fields.map(toDraftField),
    priority: f.priority,
    tag_ids: [...f.tag_ids],
    enabled: f.enabled,
  }
  editing.value = f.id
}

function addField() {
  draft.value.fields.push(toDraftField({ id: '', label: '', type: 'text' }))
}

function moveField(i: number, delta: number) {
  const list = draft.value.fields
  const j = i + delta
  if (j < 0 || j >= list.length) return
  ;[list[i], list[j]] = [list[j], list[i]]
}

function removeField(i: number) {
  draft.value.fields.splice(i, 1)
}

function toggleTag(id: number) {
  const ids = draft.value.tag_ids
  const at = ids.indexOf(id)
  if (at >= 0) ids.splice(at, 1)
  else ids.push(id)
}

const canSave = computed(() => draft.value.title.trim() !== '' && draft.value.fields.every((f) => f.label.trim() !== ''))

async function save() {
  if (!canSave.value || editing.value == null) return
  const input: RequestFormInput = {
    title: draft.value.title,
    intro: draft.value.intro,
    priority: draft.value.priority,
    tag_ids: draft.value.tag_ids,
    enabled: draft.value.enabled,
    fields: draft.value.fields.map(({ optionsText, ...f }) => ({
      ...f,
      options: f.type === 'select' ? optionsText.split('\n').map((o) => o.trim()).filter(Boolean) : undefined,
    })),
  }
  saving.value = true
  try {
    if (editing.value === 'new') await api.createRequestForm(props.project.id, input)
    else await api.updateRequestForm(props.project.id, editing.value, input)
    toast.push('Form saved', 'success')
    editing.value = null
    await load()
    emit('changed')
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Could not save the form', 'error')
  } finally {
    saving.value = false
  }
}

async function rotate(f: RequestForm) {
  const ok = await askConfirm({
    title: 'Make a new link?',
    message: 'The current link stops working. Anyone who should keep filing requests needs the new one.',
    confirmLabel: 'New link',
    warning: true,
  })
  if (!ok) return
  try {
    await api.rotateRequestForm(props.project.id, f.id)
    toast.push('New link ready', 'success')
    await load()
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Could not change the link', 'error')
  }
}

async function remove(f: RequestForm) {
  const ok = await askConfirm({
    title: `Delete “${f.title}”?`,
    message: 'Its link stops working. Tasks it already created stay.',
    confirmLabel: 'Delete',
    warning: true,
  })
  if (!ok) return
  try {
    await api.deleteRequestForm(props.project.id, f.id)
    toast.push('Form deleted', 'success')
    if (editing.value === f.id) editing.value = null
    await load()
    emit('changed')
  } catch (err) {
    toast.push(err instanceof APIError ? err.message : 'Could not delete the form', 'error')
  }
}
</script>

<template>
  <div class="request-forms-panel">
    <h4 class="h6">Request forms</h4>
    <p class="small text-muted">
      Share a form link with anyone who has an account on this site. Each request they file becomes a task here, with
      their answers in the description. They don't need to be on the project, and they won't see the task.
    </p>

    <p v-if="loading" class="small text-muted">Loading…</p>

    <template v-else-if="editing == null">
      <ul v-if="forms.length" class="list-group mb-3">
        <li v-for="f in forms" :key="f.id" class="list-group-item">
          <div class="d-flex flex-wrap align-items-center gap-2">
            <strong>{{ f.title }}</strong>
            <span class="badge" :class="f.enabled ? 'text-bg-success' : 'text-bg-secondary'">
              {{ f.enabled ? 'On' : 'Off' }}
            </span>
            <span class="small text-muted">{{ f.submissions }} {{ f.submissions === 1 ? 'request' : 'requests' }}</span>
            <span class="ms-auto d-flex gap-1">
              <button type="button" class="btn btn-sm btn-outline-secondary" @click="startEdit(f)">Edit</button>
              <button type="button" class="btn btn-sm btn-outline-secondary" @click="rotate(f)">New link</button>
              <button type="button" class="btn btn-sm btn-outline-danger" @click="remove(f)">Delete</button>
            </span>
          </div>
          <div class="input-group input-group-sm mt-2">
            <input class="form-control" :value="linkFor(f)" readonly :aria-label="`Link for ${f.title}`" @focus="($event.target as HTMLInputElement).select()" />
            <button type="button" class="btn btn-outline-secondary" @click="copyLink(f)">Copy</button>
          </div>
        </li>
      </ul>
      <p v-else class="small text-muted">No forms yet.</p>
      <button type="button" class="btn btn-sm btn-primary" @click="startNew">New form</button>
    </template>

    <form v-else class="request-form-editor" @submit.prevent="save">
      <div class="row g-2">
        <div class="col-sm-8">
          <label class="form-label small mb-0" for="rf-title">Title</label>
          <input id="rf-title" v-model="draft.title" class="form-control form-control-sm" maxlength="120" required />
        </div>
        <div class="col-sm-4">
          <label class="form-label small mb-0" for="rf-priority">Priority of new tasks</label>
          <select id="rf-priority" v-model.number="draft.priority" class="form-select form-select-sm">
            <option v-for="(p, i) in PRIORITIES" :key="p" :value="i">{{ p }}</option>
          </select>
        </div>
        <div class="col-12">
          <label class="form-label small mb-0" for="rf-intro">Intro (optional)</label>
          <textarea id="rf-intro" v-model="draft.intro" class="form-control form-control-sm" rows="2" maxlength="2000" />
        </div>
        <div v-if="tags.length" class="col-12">
          <span class="form-label small mb-0 d-block">Tags for new tasks</span>
          <div class="d-flex flex-wrap gap-1">
            <button
              v-for="t in tags"
              :key="t.id"
              type="button"
              class="btn btn-sm"
              :class="draft.tag_ids.includes(t.id) ? 'btn-primary' : 'btn-outline-secondary'"
              :aria-pressed="draft.tag_ids.includes(t.id)"
              @click="toggleTag(t.id)"
            >
              {{ t.name }}
            </button>
          </div>
        </div>
        <div class="col-12 form-check form-switch ms-2">
          <input id="rf-enabled" v-model="draft.enabled" class="form-check-input" type="checkbox" />
          <label class="form-check-label small" for="rf-enabled">Accepting requests</label>
        </div>
      </div>

      <h5 class="h6 mt-3 mb-1">Questions</h5>
      <p class="small text-muted mb-2">Every request also has a required one-line summary, which becomes the task title.</p>
      <div v-for="(f, i) in draft.fields" :key="i" class="card card-body p-2 mb-2">
        <div class="row g-2 align-items-end">
          <div class="col-sm-5">
            <label class="form-label small mb-0" :for="`rf-q-${i}`">Question</label>
            <input :id="`rf-q-${i}`" v-model="f.label" class="form-control form-control-sm" maxlength="120" required />
          </div>
          <div class="col-sm-3">
            <label class="form-label small mb-0" :for="`rf-t-${i}`">Type</label>
            <select :id="`rf-t-${i}`" v-model="f.type" class="form-select form-select-sm">
              <option v-for="t in FIELD_TYPES" :key="t.id" :value="t.id">{{ t.label }}</option>
            </select>
          </div>
          <div class="col-sm-2 form-check ms-2 mb-1">
            <input :id="`rf-r-${i}`" v-model="f.required" class="form-check-input" type="checkbox" />
            <label class="form-check-label small" :for="`rf-r-${i}`">Required</label>
          </div>
          <div class="col-sm d-flex gap-1 justify-content-end">
            <button type="button" class="btn btn-sm btn-outline-secondary" :disabled="i === 0" aria-label="Move up" @click="moveField(i, -1)">
              <i class="bi bi-arrow-up" />
            </button>
            <button type="button" class="btn btn-sm btn-outline-secondary" :disabled="i === draft.fields.length - 1" aria-label="Move down" @click="moveField(i, 1)">
              <i class="bi bi-arrow-down" />
            </button>
            <button type="button" class="btn btn-sm btn-outline-danger" aria-label="Remove question" @click="removeField(i)">
              <i class="bi bi-x-lg" />
            </button>
          </div>
          <div class="col-12">
            <input v-model="f.help" class="form-control form-control-sm" maxlength="240" placeholder="Help text (optional)" :aria-label="`Help text for question ${i + 1}`" />
          </div>
          <div v-if="f.type === 'select'" class="col-12">
            <label class="form-label small mb-0" :for="`rf-o-${i}`">Choices, one per line</label>
            <textarea :id="`rf-o-${i}`" v-model="f.optionsText" class="form-control form-control-sm" rows="3" />
          </div>
        </div>
      </div>
      <button type="button" class="btn btn-sm btn-outline-secondary" :disabled="draft.fields.length >= 20" @click="addField">
        <i class="bi bi-plus-lg me-1" />Add question
      </button>

      <div class="d-flex gap-2 mt-3">
        <button type="submit" class="btn btn-sm btn-primary" :disabled="saving || !canSave">Save form</button>
        <button type="button" class="btn btn-sm btn-outline-secondary" :disabled="saving" @click="editing = null">Cancel</button>
      </div>
    </form>
  </div>
</template>
