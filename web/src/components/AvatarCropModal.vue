<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useBodyScrollLock } from '@/composables/useBodyScrollLock'

const props = defineProps<{
  imageFile: File | null
}>()

const open = defineModel<boolean>({ default: false })
useBodyScrollLock(open)

const emit = defineEmits<{
  cropped: [blob: Blob]
}>()

const imageUrl = ref<string>('')
const imageEl = ref<HTMLImageElement | null>(null)
const naturalWidth = ref(0)
const naturalHeight = ref(0)

const zoom = ref(1)
const minZoom = ref(1)
const maxZoom = ref(3)

const panX = ref(0)
const panY = ref(0)

const isDragging = ref(false)
const dragStartX = ref(0)
const dragStartY = ref(0)
const initialPanX = ref(0)
const initialPanY = ref(0)

const cropSize = 280 // Viewport width and height in px

watch(
  () => props.imageFile,
  (file) => {
    imageUrl.value = ''
    if (file) {
      const reader = new FileReader()
      reader.onload = (e) => {
        imageUrl.value = (e.target?.result as string) || ''
        resetCropState()
      }
      reader.readAsDataURL(file)
    }
  },
  { immediate: true },
)

function resetCropState() {
  zoom.value = 1
  panX.value = 0
  panY.value = 0
}

function onImageLoaded(e: Event) {
  const img = e.target as HTMLImageElement
  imageEl.value = img
  naturalWidth.value = img.naturalWidth || img.width
  naturalHeight.value = img.naturalHeight || img.height

  // Compute minimum zoom so image covers the entire crop square
  const scaleX = cropSize / naturalWidth.value
  const scaleY = cropSize / naturalHeight.value
  const coverScale = Math.max(scaleX, scaleY)
  minZoom.value = coverScale
  maxZoom.value = Math.max(coverScale * 3, 2)
  zoom.value = coverScale
  panX.value = 0
  panY.value = 0
}

function onMouseDown(e: MouseEvent) {
  e.preventDefault()
  isDragging.value = true
  dragStartX.value = e.clientX
  dragStartY.value = e.clientY
  initialPanX.value = panX.value
  initialPanY.value = panY.value
  window.addEventListener('mousemove', onMouseMove)
  window.addEventListener('mouseup', onMouseUp)
}

function onMouseMove(e: MouseEvent) {
  if (!isDragging.value) return
  const dx = e.clientX - dragStartX.value
  const dy = e.clientY - dragStartY.value
  panX.value = clampPan(initialPanX.value + dx, naturalWidth.value * zoom.value, cropSize)
  panY.value = clampPan(initialPanY.value + dy, naturalHeight.value * zoom.value, cropSize)
}

function onMouseUp() {
  isDragging.value = false
  window.removeEventListener('mousemove', onMouseMove)
  window.removeEventListener('mouseup', onMouseUp)
}

function onTouchStart(e: TouchEvent) {
  if (e.touches.length === 1) {
    isDragging.value = true
    dragStartX.value = e.touches[0].clientX
    dragStartY.value = e.touches[0].clientY
    initialPanX.value = panX.value
    initialPanY.value = panY.value
  }
}

function onTouchMove(e: TouchEvent) {
  if (!isDragging.value || e.touches.length !== 1) return
  const dx = e.touches[0].clientX - dragStartX.value
  const dy = e.touches[0].clientY - dragStartY.value
  panX.value = clampPan(initialPanX.value + dx, naturalWidth.value * zoom.value, cropSize)
  panY.value = clampPan(initialPanY.value + dy, naturalHeight.value * zoom.value, cropSize)
}

function onTouchEnd() {
  isDragging.value = false
}

function onWheel(e: WheelEvent) {
  e.preventDefault()
  const delta = e.deltaY < 0 ? 0.08 : -0.08
  const nextZoom = Math.min(Math.max(zoom.value + delta, minZoom.value), maxZoom.value)
  zoom.value = nextZoom
  panX.value = clampPan(panX.value, naturalWidth.value * zoom.value, cropSize)
  panY.value = clampPan(panY.value, naturalHeight.value * zoom.value, cropSize)
}

function clampPan(current: number, scaledDim: number, viewDim: number): number {
  const maxOffset = Math.max(0, (scaledDim - viewDim) / 2)
  return Math.max(-maxOffset, Math.min(maxOffset, current))
}

const imageTransform = computed(() => {
  return `translate(${panX.value}px, ${panY.value}px) scale(${zoom.value})`
})

function onZoomInput() {
  panX.value = clampPan(panX.value, naturalWidth.value * zoom.value, cropSize)
  panY.value = clampPan(panY.value, naturalHeight.value * zoom.value, cropSize)
}

function applyCrop() {
  if (!imageEl.value || naturalWidth.value === 0 || naturalHeight.value === 0) return

  const canvas = document.createElement('canvas')
  const targetSize = 512
  canvas.width = targetSize
  canvas.height = targetSize
  const ctx = canvas.getContext('2d')
  if (!ctx) return

  const currentScale = zoom.value
  const viewCenterX = cropSize / 2
  const viewCenterY = cropSize / 2

  // Image center relative to viewport center:
  const imgCenterX = viewCenterX + panX.value
  const imgCenterY = viewCenterY + panY.value

  // Map back to natural image source coords:
  const srcCropWidth = cropSize / currentScale
  const srcCropHeight = cropSize / currentScale
  const srcX = (naturalWidth.value / 2) - ((imgCenterX - viewCenterX) / currentScale) - (srcCropWidth / 2)
  const srcY = (naturalHeight.value / 2) - ((imgCenterY - viewCenterY) / currentScale) - (srcCropHeight / 2)

  ctx.drawImage(
    imageEl.value,
    Math.max(0, srcX),
    Math.max(0, srcY),
    Math.min(naturalWidth.value, srcCropWidth),
    Math.min(naturalHeight.value, srcCropHeight),
    0,
    0,
    targetSize,
    targetSize,
  )

  const outputType = props.imageFile?.type === 'image/png' ? 'image/png' : 'image/jpeg'
  canvas.toBlob(
    (blob) => {
      if (blob) {
        emit('cropped', blob)
        open.value = false
      }
    },
    outputType,
    0.92,
  )
}

function onKeydown(e: KeyboardEvent) {
  if (!open.value) return
  if (e.key === 'Escape') {
    e.preventDefault()
    open.value = false
  }
}

watch(
  () => open.value,
  (val) => {
    if (!val) {
      imageUrl.value = ''
    }
  },
)

onMounted(() => {
  window.addEventListener('keydown', onKeydown)
})

onUnmounted(() => {
  window.removeEventListener('keydown', onKeydown)
  imageUrl.value = ''
})
</script>

<template>
  <Teleport to="body">
    <div
      v-if="open"
      class="modal fade show d-block avatar-cropper-modal"
      tabindex="-1"
      role="dialog"
      aria-modal="true"
      aria-labelledby="avatarCropModalLabel"
    >
      <div class="modal-dialog modal-dialog-centered" @click.stop>
        <div class="modal-content shadow">
          <div class="modal-header">
            <h5 id="avatarCropModalLabel" class="modal-title">Crop Profile Picture</h5>
            <button type="button" class="btn-close" aria-label="Close" @click="open = false" />
          </div>

          <div class="modal-body d-flex flex-column align-items-center">
            <p class="text-muted small mb-3 text-center">
              Drag to reposition and use the slider to zoom.
            </p>

            <!-- Cropper Viewport Container -->
            <div
              class="crop-viewport"
              :style="{ width: `${cropSize}px`, height: `${cropSize}px` }"
              @mousedown="onMouseDown"
              @touchstart="onTouchStart"
              @touchmove="onTouchMove"
              @touchend="onTouchEnd"
              @wheel="onWheel"
            >
              <img
                v-if="imageUrl"
                :src="imageUrl"
                alt="Crop preview"
                class="crop-source-image"
                :style="{ transform: imageTransform }"
                draggable="false"
                @load="onImageLoaded"
              />
              <!-- Circular mask overlay -->
              <div class="crop-circular-mask" />
            </div>

            <!-- Zoom Control Slider -->
            <div class="zoom-controls d-flex align-items-center gap-2 mt-3 w-100" style="max-width: 320px;">
              <i class="bi bi-zoom-out text-muted" />
              <input
                v-model.number="zoom"
                type="range"
                class="form-range flex-grow-1"
                :min="minZoom"
                :max="maxZoom"
                step="0.01"
                aria-label="Zoom"
                @input="onZoomInput"
              />
              <i class="bi bi-zoom-in text-muted" />
              <button
                type="button"
                class="btn btn-sm btn-outline-secondary ms-1"
                title="Reset crop"
                @click="resetCropState"
              >
                Reset
              </button>
            </div>
          </div>

          <div class="modal-footer">
            <button type="button" class="btn btn-secondary" @click="open = false">
              Cancel
            </button>
            <button type="button" class="btn btn-primary" @click="applyCrop">
              Save & Apply
            </button>
          </div>
        </div>
      </div>
    </div>
    <div v-if="open" class="modal-backdrop fade show" />
  </Teleport>
</template>

<style scoped>
.avatar-cropper-modal {
  z-index: 1060;
}
.crop-viewport {
  position: relative;
  overflow: hidden;
  user-select: none;
  background-color: #1a1a1a;
  border-radius: 8px;
  cursor: grab;
  display: flex;
  align-items: center;
  justify-content: center;
}
.crop-viewport:active {
  cursor: grabbing;
}
.crop-source-image {
  position: absolute;
  max-width: none;
  transform-origin: center center;
  pointer-events: none;
  transition: transform 0.05s ease-out;
}
.crop-circular-mask {
  position: absolute;
  inset: 0;
  pointer-events: none;
  border-radius: 50%;
  box-shadow: 0 0 0 9999px rgba(0, 0, 0, 0.55);
  border: 2px solid rgba(255, 255, 255, 0.85);
}
</style>
