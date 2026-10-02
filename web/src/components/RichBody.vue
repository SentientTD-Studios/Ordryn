<script setup lang="ts">
import { computed } from 'vue'
import { renderMarkdown } from '@/utils/markdown'

const props = withDefaults(
  defineProps<{
    body: string
    compact?: boolean
    taskTitle?: (id: number) => string | undefined
  }>(),
  { compact: false },
)

const emit = defineEmits<{
  'open-task': [id: number]
}>()

const renderedHtml = computed(() => {
  return renderMarkdown(props.body || '', {
    taskTitle: props.taskTitle,
  })
})

function onContainerClick(e: MouseEvent) {
  const target = (e.target as HTMLElement).closest<HTMLElement>('.rich-body-task-link')
  if (target) {
    const rawId = target.getAttribute('data-task-id')
    const id = rawId ? Number(rawId) : 0
    if (id) {
      e.preventDefault()
      e.stopPropagation()
      emit('open-task', id)
    }
  }
}
</script>

<template>
  <div
    class="rich-body"
    :class="{ 'rich-body--compact': compact }"
    @click="onContainerClick"
    v-html="renderedHtml"
  />
</template>

<style scoped>
.rich-body {
  display: block;
  min-width: 0;
  max-width: 100%;
  overflow-wrap: anywhere;
  word-break: break-word;
  line-height: 1.5;
}
.rich-body :deep(p) {
  margin-bottom: 0.5rem;
}
.rich-body :deep(p:last-child) {
  margin-bottom: 0;
}
.rich-body :deep(ul),
.rich-body :deep(ol) {
  margin-top: 0.25rem;
  margin-bottom: 0.5rem;
  padding-left: 1.4rem;
}
.rich-body :deep(li) {
  margin-bottom: 0.15rem;
}
.rich-body :deep(strong) {
  font-weight: 700;
}
.rich-body :deep(em) {
  font-style: italic;
}
.rich-body :deep(u),
.rich-body :deep(ins) {
  text-decoration: underline;
  text-underline-offset: 2px;
}
.rich-body :deep(.rich-body-mention) {
  font-weight: 600;
  color: var(--ordryn-accent, #2563eb);
  background: color-mix(in srgb, var(--ordryn-accent, #2563eb) 12%, transparent);
  padding: 0.1rem 0.35rem;
  border-radius: 0.25rem;
  font-size: 0.9em;
}
.rich-body :deep(.rich-body-link) {
  color: var(--ordryn-accent, #2563eb);
  text-decoration: underline;
  text-underline-offset: 2px;
}
.rich-body :deep(.rich-body-link:hover) {
  filter: brightness(1.15);
}
.rich-body :deep(.rich-body-image-link) {
  display: block;
  max-width: 100%;
  margin: 0.5rem 0;
}
.rich-body :deep(.rich-body-image) {
  display: block;
  max-width: 100%;
  width: auto;
  height: auto;
  max-height: min(240px, 40vh);
  object-fit: contain;
  border-radius: 0.375rem;
  border: 1px solid var(--ordryn-card-border, #dee2e6);
  background: var(--ordryn-muted-bg, #f8f6ee);
}
.rich-body--compact :deep(.rich-body-image-link) {
  margin: 0.25rem 0;
}
.rich-body--compact :deep(.rich-body-image) {
  max-height: 6rem;
}
.rich-body :deep(.rich-body-task-link) {
  display: inline;
  padding: 0;
  margin: 0;
  border: 0;
  background: none;
  color: var(--ordryn-accent, #2563eb);
  font-weight: 600;
  text-decoration: underline;
  text-underline-offset: 2px;
  cursor: pointer;
}
.rich-body :deep(.rich-body-task-link:hover) {
  filter: brightness(1.15);
}

/* Syntax highlighting styles for code blocks */
.rich-body :deep(pre) {
  background: var(--ordryn-muted-bg, #f8f6ee);
  border: 1px solid var(--ordryn-card-border, #dee2e6);
  border-radius: 0.375rem;
  padding: 1rem;
  overflow-x: auto;
  margin: 0.75rem 0;
}
.rich-body :deep(pre code) {
  font-family: 'Consolas', 'Monaco', 'Andale Mono', 'DejaVu Sans Mono', monospace;
  font-size: 0.9em;
  line-height: 1.4;
  white-space: pre;
}

/* Highlight.js theme styles - Monokai-like dark theme */
.rich-body :deep(pre.hljs) {
  background: #272822;
  color: #f8f8f2;
}

/* Generic token styles for highlight.js */
.rich-body :deep(pre.hljs .hljs-comment),
.rich-body :deep(pre.hljs .hljs-quote) {
  color: #75715e;
  font-style: italic;
}

.rich-body :deep(pre.hljs .hljs-keyword),
.rich-body :deep(pre.hljs .hljs-selector-tag),
.rich-body :deep(pre.hljs .hljs-section),
.rich-body :deep(pre.hljs .hljs-subst) {
  color: #f92672;
}

.rich-body :deep(pre.hljs .hljs-string),
.rich-body :deep(pre.hljs .hljs-title),
.rich-body :deep(pre.hljs .hljs-name),
.rich-body :deep(pre.hljs .hljs-type),
.rich-body :deep(pre.hljs .hljs-symbol),
.rich-body :deep(pre.hljs .hljs-bullet),
.rich-body :deep(pre.hljs .hljs-variable),
.rich-body :deep(pre.hljs .hljs-template-variable),
.rich-body :deep(pre.hljs .hljs-link) {
  color: #a6e22e;
}

.rich-body :deep(pre.hljs .hljs-number),
.rich-body :deep(pre.hljs .hljs-built_in),
.rich-body :deep(pre.hljs .hljs-builtin-name),
.rich-body :deep(pre.hljs .hljs-literal),
.rich-body :deep(pre.hljs .hljs-params),
.rich-body :deep(pre.hljs .hljs-meta),
.rich-body :deep(pre.hljs .hljs-regexp),
.rich-body :deep(pre.hljs .hljs-title.class_) {
  color: #ae81ff;
}

.rich-body :deep(pre.hljs .hljs-attr) {
  color: #66d9ef;
}

.rich-body :deep(pre.hljs .hljs-class .hljs-title) {
  color: #f8f8f2;
}

/* Scrollbar styling for code blocks */
.rich-body :deep(pre)::-webkit-scrollbar {
  height: 8px;
}
.rich-body :deep(pre)::-webkit-scrollbar-track {
  background: var(--ordryn-muted-bg, #f8f6ee);
  border-radius: 0.375rem;
}
.rich-body :deep(pre)::-webkit-scrollbar-thumb {
  background: var(--ordryn-card-border, #dee2e6);
  border-radius: 4px;
}
.rich-body :deep(pre)::-webkit-scrollbar-thumb:hover {
  background: color-mix(in srgb, var(--ordryn-accent, #2563eb) 30%, transparent);
}

/* Compact mode adjustments */
.rich-body--compact :deep(pre) {
  padding: 0.75rem;
  margin: 0.5rem 0;
}
.rich-body--compact :deep(pre code) {
  font-size: 0.85em;
}
</style>
