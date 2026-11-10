<script setup lang="ts">
import { nextTick, ref } from 'vue'
import { applyFormat, handleListEnter, type FormatAction } from '@/utils/markdown'
import { insertMarkdownAtCursor } from '@/utils/taskCommentBody'
import { useImageUpload } from '@/composables/useImageUpload'
import { IMAGE_ACCEPT, armTaskOverlayFileGuard, dropHasFiles, imageFileFromClipboard, imageFileFromDrop } from '@/utils/imageUpload'
import RichBody from '@/components/RichBody.vue'
import { useToast } from '@/composables/useToast'
import { useBodyScrollLock } from '@/composables/useBodyScrollLock'

const props = withDefaults(
  defineProps<{
    modelValue: string
    placeholder?: string
    maxlength?: number
    rows?: number
    disabled?: boolean
    compact?: boolean
    minHeight?: string
    taskTitle?: (id: number) => string | undefined
  }>(),
  {
    modelValue: '',
    placeholder: "Write in markdown… Click 'Upload image' or paste/drop an image directly.",
    rows: 4,
    disabled: false,
    compact: false,
  },
)

const emit = defineEmits<{
  'update:modelValue': [value: string]
  'open-task': [id: number]
  input: [event: Event]
  keydown: [event: KeyboardEvent]
  paste: [event: ClipboardEvent]
  dragover: [event: DragEvent]
  drop: [event: DragEvent]
  click: [event: MouseEvent]
  keyup: [event: KeyboardEvent]
}>()

const toast = useToast()
const { busy: imageUploading, uploadImageFile } = useImageUpload()

const textareaEl = ref<HTMLTextAreaElement | null>(null)
const fileInputEl = ref<HTMLInputElement | null>(null)
const isPreview = ref(false)
const isFocused = ref(false)

// Link modal state
const showLinkModal = ref(false)
const linkUrl = ref('')
const linkTitle = ref('')
const linkUrlInputEl = ref<HTMLInputElement | null>(null)
const linkTitleInputEl = ref<HTMLInputElement | null>(null)
const savedSelectionStart = ref(0)
const savedSelectionEnd = ref(0)

useBodyScrollLock(showLinkModal)

function updateValue(next: string, cursorStart?: number, cursorEnd?: number) {
  if (props.maxlength && next.length > props.maxlength) {
    toast.push(`Cannot exceed ${props.maxlength} characters`, 'error')
    return
  }
  if (textareaEl.value) {
    textareaEl.value.value = next
  }
  emit('update:modelValue', next)
  if (cursorStart != null) {
    void nextTick(() => {
      if (textareaEl.value) {
        textareaEl.value.focus()
        textareaEl.value.setSelectionRange(cursorStart, cursorEnd ?? cursorStart)
      }
    })
  }
}

function handleFormat(action: FormatAction) {
  if (props.disabled) return
  if (isPreview.value) {
    isPreview.value = false
  }
  if (action === 'link') {
    openLinkModal()
    return
  }
  const el = textareaEl.value
  const text = props.modelValue
  const start = el?.selectionStart ?? text.length
  const end = el?.selectionEnd ?? start

  const result = applyFormat(text, start, end, action)
  updateValue(result.text, result.selectionStart, result.selectionEnd)
}

function openLinkModal() {
  const el = textareaEl.value
  const text = props.modelValue
  const start = el?.selectionStart ?? text.length
  const end = el?.selectionEnd ?? start
  const selected = text.slice(start, end).trim()

  savedSelectionStart.value = start
  savedSelectionEnd.value = end

  if (/^(https?:\/\/|mailto:|\/uploads\/)/i.test(selected)) {
    linkUrl.value = selected
    linkTitle.value = ''
  } else {
    linkUrl.value = ''
    linkTitle.value = selected
  }

  showLinkModal.value = true
  void nextTick(() => {
    if (linkUrl.value) {
      linkTitleInputEl.value?.focus()
      linkTitleInputEl.value?.select()
    } else {
      linkUrlInputEl.value?.focus()
      linkUrlInputEl.value?.select()
    }
  })
}

function closeLinkModal() {
  showLinkModal.value = false
  linkUrl.value = ''
  linkTitle.value = ''
  void nextTick(() => {
    textareaEl.value?.focus()
  })
}

function insertLink() {
  const rawUrl = linkUrl.value.trim()
  if (!rawUrl) {
    toast.push('Please enter a URL', 'error')
    linkUrlInputEl.value?.focus()
    return
  }

  let url = rawUrl
  if (
    !/^https?:\/\//i.test(url) &&
    !/^mailto:/i.test(url) &&
    !url.startsWith('/') &&
    !url.startsWith('#')
  ) {
    url = `https://${url}`
  }

  const title = linkTitle.value.trim()
  const text = props.modelValue
  const start = savedSelectionStart.value
  const end = savedSelectionEnd.value

  const result = applyFormat(text, start, end, 'link', { url, title: title || url })
  updateValue(result.text, result.selectionStart, result.selectionEnd)
  closeLinkModal()
}

function triggerImagePicker() {
  if (props.disabled || imageUploading.value) return
  if (isPreview.value) {
    isPreview.value = false
  }
  armTaskOverlayFileGuard()
  fileInputEl.value?.click()
}

async function onImageFileChange(e: Event) {
  armTaskOverlayFileGuard()
  const input = e.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (!file) return
  await uploadAndInsertImage(file)
}

async function uploadAndInsertImage(file: File) {
  const markdown = await uploadImageFile(file)
  if (!markdown) return
  const el = textareaEl.value
  const current = props.modelValue
  const start = el?.selectionStart ?? current.length
  const end = el?.selectionEnd ?? start
  const next = insertMarkdownAtCursor(current, markdown, start, end)
  updateValue(next.body, next.cursor, next.cursor)
}

function onTextareaInput(e: Event) {
  const el = e.target as HTMLTextAreaElement
  emit('update:modelValue', el.value)
  emit('input', e)
}

function onTextareaKeydown(e: KeyboardEvent) {
  emit('keydown', e)
  if (e.defaultPrevented) return

  // Keyboard shortcuts
  if ((e.ctrlKey || e.metaKey) && !e.altKey && !e.shiftKey) {
    if (e.key === 'b' || e.key === 'B') {
      e.preventDefault()
      handleFormat('bold')
      return
    } else if (e.key === 'i' || e.key === 'I') {
      e.preventDefault()
      handleFormat('italic')
      return
    } else if (e.key === 'u' || e.key === 'U') {
      e.preventDefault()
      handleFormat('underline')
      return
    } else if (e.key === 'k' || e.key === 'K') {
      e.preventDefault()
      handleFormat('link')
      return
    }
  }

  // List continuation on Enter
  if (e.key === 'Enter' && !e.shiftKey && !e.ctrlKey && !e.metaKey && !e.altKey && !e.isComposing) {
    const el = textareaEl.value
    if (el) {
      const result = handleListEnter(props.modelValue, el.selectionStart, el.selectionEnd)
      if (result) {
        e.preventDefault()
        updateValue(result.text, result.cursor, result.cursor)
      }
    }
  }
}

function onTextareaPaste(e: ClipboardEvent) {
  emit('paste', e)
  if (e.defaultPrevented) return
  const file = imageFileFromClipboard(e)
  if (file) {
    e.preventDefault()
    void uploadAndInsertImage(file)
  }
}

function onTextareaDragOver(e: DragEvent) {
  emit('dragover', e)
  if (e.defaultPrevented) return
  if (dropHasFiles(e)) {
    e.preventDefault()
  }
}

function onTextareaDrop(e: DragEvent) {
  emit('drop', e)
  if (e.defaultPrevented) return
  const file = imageFileFromDrop(e)
  if (file) {
    e.preventDefault()
    void uploadAndInsertImage(file)
  }
}

defineExpose({
  textarea: textareaEl,
  focus: () => textareaEl.value?.focus(),
  setSelectionRange: (start: number, end: number) => {
    textareaEl.value?.setSelectionRange(start, end)
  },
})
</script>

<template>
  <div
    class="wysiwyg-editor"
    :class="{
      'wysiwyg-editor--focused': isFocused,
      'wysiwyg-editor--compact': compact,
      'wysiwyg-editor--disabled': disabled,
    }"
  >
    <!-- Hidden file input for image upload -->
    <input
      ref="fileInputEl"
      type="file"
      class="d-none"
      :accept="IMAGE_ACCEPT"
      @change="onImageFileChange"
    />

    <!-- Formatting Toolbar -->
    <div class="wysiwyg-toolbar">
      <div class="wysiwyg-toolbar-group">
        <button
          type="button"
          class="wysiwyg-btn"
          :disabled="disabled || isPreview"
          title="Bold (Ctrl+B)"
          aria-label="Bold"
          @click="handleFormat('bold')"
        >
          <i class="bi bi-type-bold" />
        </button>
        <button
          type="button"
          class="wysiwyg-btn"
          :disabled="disabled || isPreview"
          title="Italic (Ctrl+I)"
          aria-label="Italic"
          @click="handleFormat('italic')"
        >
          <i class="bi bi-type-italic" />
        </button>
        <button
          type="button"
          class="wysiwyg-btn"
          :disabled="disabled || isPreview"
          title="Underline (Ctrl+U)"
          aria-label="Underline"
          @click="handleFormat('underline')"
        >
          <i class="bi bi-type-underline" />
        </button>

        <span class="wysiwyg-toolbar-divider" />

        <button
          type="button"
          class="wysiwyg-btn"
          :disabled="disabled || isPreview"
          title="Bulleted list"
          aria-label="Bulleted list"
          @click="handleFormat('ul')"
        >
          <i class="bi bi-list-ul" />
        </button>
        <button
          type="button"
          class="wysiwyg-btn"
          :disabled="disabled || isPreview"
          title="Numbered list"
          aria-label="Numbered list"
          @click="handleFormat('ol')"
        >
          <i class="bi bi-list-ol" />
        </button>

        <span class="wysiwyg-toolbar-divider" />

        <button
          type="button"
          class="wysiwyg-btn"
          :disabled="disabled || isPreview"
          title="Insert link (Ctrl+K)"
          aria-label="Insert link"
          @click="handleFormat('link')"
        >
          <i class="bi bi-link-45deg" />
        </button>

        <button
          type="button"
          class="wysiwyg-btn wysiwyg-btn--image"
          :class="{ 'wysiwyg-btn--uploading': imageUploading }"
          :disabled="disabled || imageUploading || isPreview"
          title="Upload image (or paste/drop an image)"
          aria-label="Upload image"
          @click="triggerImagePicker"
        >
          <span v-if="imageUploading" class="spinner-border spinner-border-sm me-1" role="status" aria-hidden="true" />
          <i v-else class="bi bi-image me-1" />
          <span class="wysiwyg-btn-label">{{ imageUploading ? 'Uploading…' : 'Upload image' }}</span>
        </button>
      </div>

      <!-- Mode Toggle: Write / Preview -->
      <div class="wysiwyg-mode-tabs" role="tablist">
        <button
          type="button"
          role="tab"
          class="wysiwyg-mode-btn"
          :class="{ active: !isPreview }"
          :aria-selected="!isPreview"
          @click="isPreview = false"
        >
          <i class="bi bi-pencil me-1" />Write
        </button>
        <button
          type="button"
          role="tab"
          class="wysiwyg-mode-btn"
          :class="{ active: isPreview }"
          :aria-selected="isPreview"
          @click="isPreview = true"
        >
          <i class="bi bi-eye me-1" />Preview
        </button>
      </div>
    </div>

    <!-- Insert Link Modal -->
    <Teleport to="body">
      <div
        v-if="showLinkModal"
        id="wysiwygLinkModal"
        class="modal fade show d-block wysiwyg-link-modal"
        tabindex="-1"
        role="dialog"
        aria-modal="true"
        aria-labelledby="wysiwygLinkModalLabel"
        @click="closeLinkModal"
      >
        <div class="modal-dialog modal-dialog-centered" @click.stop>
          <div class="modal-content shadow">
            <div class="modal-header">
              <h5 id="wysiwygLinkModalLabel" class="modal-title d-flex align-items-center gap-2">
                <i class="bi bi-link-45deg fs-4 text-primary" />
                Insert Link
              </h5>
              <button
                type="button"
                class="btn-close"
                aria-label="Close"
                @click="closeLinkModal"
              />
            </div>
            <form @submit.prevent="insertLink">
              <div class="modal-body">
                <div class="mb-3">
                  <label for="wysiwyg-link-url" class="form-label small fw-semibold">
                    URL <span class="text-danger">*</span>
                  </label>
                  <div class="input-group">
                    <span class="input-group-text"><i class="bi bi-globe" /></span>
                    <input
                      id="wysiwyg-link-url"
                      ref="linkUrlInputEl"
                      v-model="linkUrl"
                      type="text"
                      class="form-control"
                      placeholder="https://example.com"
                      required
                      @keydown.esc.prevent="closeLinkModal"
                    />
                  </div>
                  <div class="form-text small text-muted">
                    Paste a web address (http/https), mailto link, or relative path.
                  </div>
                </div>
                <div class="mb-2">
                  <label for="wysiwyg-link-title" class="form-label small fw-semibold">
                    Link Text <span class="text-muted fw-normal">(optional)</span>
                  </label>
                  <div class="input-group">
                    <span class="input-group-text"><i class="bi bi-fonts" /></span>
                    <input
                      id="wysiwyg-link-title"
                      ref="linkTitleInputEl"
                      v-model="linkTitle"
                      type="text"
                      class="form-control"
                      placeholder="Display text for the link"
                      @keydown.esc.prevent="closeLinkModal"
                    />
                  </div>
                  <div class="form-text small text-muted">
                    If empty, the URL itself will be displayed.
                  </div>
                </div>
              </div>
              <div class="modal-footer">
                <button
                  type="button"
                  class="btn btn-secondary"
                  @click="closeLinkModal"
                >
                  Cancel
                </button>
                <button
                  type="submit"
                  class="btn btn-primary d-flex align-items-center gap-1"
                >
                  <i class="bi bi-check2" />
                  Insert Link
                </button>
              </div>
            </form>
          </div>
        </div>
      </div>
      <div v-if="showLinkModal" class="modal-backdrop fade show wysiwyg-link-backdrop" />
    </Teleport>

    <!-- Content Area -->
    <div class="wysiwyg-content">
      <!-- Write (Textarea) Mode -->
      <textarea
        v-show="!isPreview"
        ref="textareaEl"
        class="wysiwyg-textarea form-control"
        :value="modelValue"
        :rows="rows"
        :placeholder="placeholder"
        :maxlength="maxlength"
        :disabled="disabled"
        :style="minHeight ? { minHeight } : undefined"
        @input="onTextareaInput"
        @keydown="onTextareaKeydown"
        @paste="onTextareaPaste"
        @dragover="onTextareaDragOver"
        @drop="onTextareaDrop"
        @click="emit('click', $event)"
        @keyup="emit('keyup', $event)"
        @focus="isFocused = true"
        @blur="isFocused = false"
      />

      <!-- Preview Mode -->
      <div
        v-show="isPreview"
        class="wysiwyg-preview"
        :style="minHeight ? { minHeight } : undefined"
      >
        <div class="wysiwyg-preview-header small text-muted mb-2 d-flex align-items-center gap-1">
          <i class="bi bi-eye" />
          <span>Preview of formatted markdown:</span>
        </div>
        <RichBody
          v-if="modelValue.trim()"
          :body="modelValue"
          :task-title="taskTitle"
          @open-task="(id) => emit('open-task', id)"
        />
        <p v-else class="text-muted fst-italic mb-0 small">Nothing to preview</p>
      </div>
    </div>
  </div>
</template>

<style scoped>
.wysiwyg-editor {
  border: 1px solid var(--ordryn-card-border, #dee2e6);
  border-radius: 0.5rem;
  background: var(--ordryn-card-bg, #fff);
  display: flex;
  flex-direction: column;
  transition: border-color 0.15s ease-in-out, box-shadow 0.15s ease-in-out;
  overflow: hidden;
}

.wysiwyg-editor--focused {
  border-color: var(--ordryn-accent, #86b7fe);
  box-shadow: 0 0 0 0.2rem rgba(37, 99, 235, 0.15);
}

.wysiwyg-editor--disabled {
  opacity: 0.7;
  pointer-events: none;
}

.wysiwyg-toolbar {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 0.35rem;
  padding: 0.35rem 0.5rem;
  background: color-mix(in srgb, var(--ordryn-muted-bg, #f8f6ee) 75%, var(--ordryn-card-bg, #fff));
  border-bottom: 1px solid var(--ordryn-card-border, #dee2e6);
}

.wysiwyg-toolbar-group {
  display: flex;
  align-items: center;
  gap: 0.2rem;
  flex-wrap: wrap;
}

.wysiwyg-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  padding: 0.25rem 0.45rem;
  min-width: 2rem;
  height: 1.85rem;
  font-size: 0.875rem;
  border-radius: 0.25rem;
  border: 1px solid transparent;
  background: transparent;
  color: var(--ordryn-text, inherit);
  cursor: pointer;
  transition: background-color 0.15s ease, border-color 0.15s ease, color 0.15s ease;
}

.wysiwyg-btn:hover:not(:disabled) {
  background: color-mix(in srgb, var(--ordryn-accent, #2563eb) 12%, transparent);
  color: var(--ordryn-accent, #2563eb);
}

.wysiwyg-btn:disabled {
  opacity: 0.45;
  cursor: not-allowed;
}

.wysiwyg-btn-label {
  font-size: 0.8rem;
  font-weight: 500;
}

.wysiwyg-toolbar-divider {
  display: inline-block;
  width: 1px;
  height: 1.15rem;
  margin: 0 0.25rem;
  background: var(--ordryn-card-border, #dee2e6);
}

.wysiwyg-mode-tabs {
  display: inline-flex;
  align-items: center;
  padding: 0.15rem;
  background: color-mix(in srgb, var(--ordryn-card-border, #dee2e6) 40%, transparent);
  border-radius: 0.35rem;
}

.wysiwyg-mode-btn {
  display: inline-flex;
  align-items: center;
  padding: 0.2rem 0.55rem;
  font-size: 0.775rem;
  font-weight: 600;
  border: 0;
  border-radius: 0.25rem;
  background: transparent;
  color: var(--ordryn-muted, #6c757d);
  cursor: pointer;
  transition: all 0.15s ease;
}

.wysiwyg-mode-btn.active {
  background: var(--ordryn-card-bg, #fff);
  color: var(--ordryn-accent, #2563eb);
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.08);
}

.wysiwyg-link-modal {
  z-index: 2060;
  background-color: rgba(0, 0, 0, 0.45);
}

.wysiwyg-link-backdrop {
  z-index: 2055;
}

.wysiwyg-content {
  position: relative;
  min-height: 80px;
}

.wysiwyg-textarea {
  border: 0;
  border-radius: 0;
  box-shadow: none !important;
  resize: vertical;
  background: transparent;
  color: var(--ordryn-text, inherit);
  padding: 0.5rem 0.75rem;
  font-size: 0.9rem;
  line-height: 1.5;
}

.wysiwyg-textarea:focus {
  background: transparent;
  color: var(--ordryn-text, inherit);
}

.wysiwyg-preview {
  padding: 0.65rem 0.75rem;
  background: var(--ordryn-card-bg, #fff);
  min-height: 80px;
  max-height: 420px;
  overflow-y: auto;
  font-size: 0.9rem;
  line-height: 1.5;
}

.wysiwyg-preview-header {
  padding-bottom: 0.35rem;
  border-bottom: 1px dashed var(--ordryn-card-border, #dee2e6);
  font-size: 0.75rem;
}
</style>
