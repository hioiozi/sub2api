<template>
  <section class="sakura-cover-surface" :class="{ 'is-editable': canDrag, 'is-dragging': dragging }" :style="{ '--cover-mask': cover.mask + '%' }" :aria-label="greeting">
    <div ref="art" class="sakura-cover-art" :class="{ 'has-image': cover.image_data }" aria-hidden="true">
      <img v-if="cover.image_data" ref="picture" :src="cover.image_data" alt="" :style="{ objectPosition: `${cover.position_x}% ${cover.position_y}%` }" draggable="false" @load="imageReady = true" @error="imageReady = false" />
    </div>
    <button v-if="editable && cover.image_data" ref="dragHandle" type="button" class="sakura-cover-drag"
      :disabled="!canDrag" :aria-label="dragLabel" data-testid="cover-drag"
      @pointerdown="startDrag" @pointermove="moveDrag" @pointerup="endDrag"
      @pointercancel="cancelDrag" @lostpointercapture="cancelDrag" @keydown="moveWithKeys" />
    <span v-if="editable" class="sr-only" role="status" aria-live="polite">{{ cover.position_x }}%, {{ cover.position_y }}%</span>
    <div class="sakura-cover-veil" aria-hidden="true"></div>
    <div class="sakura-cover-copy">
      <div class="sakura-cover-eyebrow">{{ eyebrow }}</div>
      <h2 :title="greeting">{{ greeting }}</h2>
      <p>{{ subtitle }}</p>
      <div v-if="$slots.default" class="sakura-cover-actions"><slot /></div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import type { DashboardCover } from '@/api/sakuraCover'
import { coverOverflow, draggedCoverPosition, type CoverOverflow, type CoverPosition } from './coverDrag'
const props = defineProps<{ cover: DashboardCover; eyebrow: string; greeting: string; subtitle: string; editable?: boolean; disabled?: boolean; dragLabel?: string }>()
const emit = defineEmits<{ (e: 'position-change', position: CoverPosition): void }>()
const art = ref<HTMLDivElement | null>(null), picture = ref<HTMLImageElement | null>(null), dragHandle = ref<HTMLButtonElement | null>(null)
const imageReady = ref(false), dragging = ref(false)
const canDrag = computed(() => props.editable && !props.disabled && imageReady.value && !!props.cover.image_data)
let active: { id: number; x: number; y: number; start: CoverPosition; overflow: CoverOverflow } | null = null

function geometry() {
  const frame = art.value?.getBoundingClientRect(), img = picture.value
  return frame && img ? coverOverflow(frame.width, frame.height, img.naturalWidth, img.naturalHeight) : { x: 0, y: 0 }
}
function position() { return { position_x: props.cover.position_x, position_y: props.cover.position_y } }
function startDrag(event: PointerEvent) {
  // Touch scrolling is unchanged in this desktop-first iteration.
  if (!canDrag.value || active || event.button !== 0 || !event.isPrimary || event.pointerType === 'touch') return
  event.preventDefault()
  dragHandle.value?.focus({ preventScroll: true })
  active = { id: event.pointerId, x: event.clientX, y: event.clientY, start: position(), overflow: geometry() }
  dragHandle.value?.setPointerCapture(event.pointerId)
  dragging.value = true
}
function moveDrag(event: PointerEvent) {
  if (!active || event.pointerId !== active.id) return
  if ((event.buttons & 1) === 0) { finishDrag(); return }
  emit('position-change', draggedCoverPosition(active.start, event.clientX - active.x, event.clientY - active.y, active.overflow))
}
function finishDrag(revert = false) {
  const previous = active
  active = null; dragging.value = false
  if (!previous) return
  if (revert) emit('position-change', previous.start)
  if (dragHandle.value?.hasPointerCapture(previous.id)) dragHandle.value.releasePointerCapture(previous.id)
}
function endDrag(event: PointerEvent) { if (active?.id === event.pointerId) { moveDrag(event); finishDrag() } }
function cancelDrag() { finishDrag(true) }
function moveWithKeys(event: KeyboardEvent) {
  if (event.key === 'Escape' && active) { event.preventDefault(); event.stopPropagation(); cancelDrag(); return }
  if (!canDrag.value || active) return
  const step = event.shiftKey ? 20 : 4
  const overflow = geometry()
  // Stored positions use whole percentages; every key press must survive rounding.
  const xStep = Math.max(step, overflow.x / 100), yStep = Math.max(step, overflow.y / 100)
  const deltas: Record<string, [number, number]> = { ArrowLeft: [-xStep, 0], ArrowRight: [xStep, 0], ArrowUp: [0, -yStep], ArrowDown: [0, yStep] }
  const delta = deltas[event.key]
  if (!delta) return
  event.preventDefault()
  emit('position-change', draggedCoverPosition(position(), delta[0], delta[1], overflow))
}
watch(() => props.cover.image_data, () => { finishDrag(); imageReady.value = false })
watch(canDrag, value => { if (!value) finishDrag() })
onMounted(() => {
  imageReady.value = !!picture.value?.complete && !!picture.value?.naturalWidth
  window.addEventListener('blur', cancelDrag)
  window.addEventListener('resize', cancelDrag)
})
onBeforeUnmount(() => { finishDrag(); window.removeEventListener('blur', cancelDrag); window.removeEventListener('resize', cancelDrag) })
</script>

<style scoped>
.sakura-cover-surface {
  --cover-paper: #fff; --cover-ink: #30263f; --cover-muted: #776785; --cover-border: #e9e3f1;
  position: relative; isolation: isolate; height: 180px; min-height: 180px; overflow: hidden;
  border: 1px solid var(--cover-border); border-radius: 18px; background: var(--cover-paper);
  box-shadow: 0 2px 3px #31214108; display: flex; align-items: center;
}
.dark .sakura-cover-surface { --cover-paper: #211c2b; --cover-ink: #f3edf8; --cover-muted: #bdb1cb; --cover-border: #493d59; }
.sakura-cover-art { position: absolute; z-index: -3; inset: 0 0 0 31%; background: radial-gradient(ellipse at 82% 34%,#f7d6c4,transparent 27%),radial-gradient(ellipse at 58% 85%,#b7addc,transparent 40%),linear-gradient(125deg,#8075ad,#c1b1dd 46%,#f0cee0); }
.sakura-cover-art::before { content: ''; position: absolute; width: 104px; height: 104px; border-radius: 50%; right: 18%; top: 24px; background: linear-gradient(#fff5e6bb,#fff0d522); box-shadow: 0 0 42px #fae6d044; }
.sakura-cover-art::after { content: ''; position: absolute; width: 100%; height: 150px; bottom: -108px; right: -14%; border-radius: 50%; background: #796b9c22; transform: rotate(-9deg); box-shadow: -80px -22px 0 10px #e3d2ed55; }
.sakura-cover-art.has-image::before,.sakura-cover-art.has-image::after { display: none; }
.sakura-cover-art img { display: block; width: 100%; height: 100%; object-fit: cover; }
.sakura-cover-veil { position: absolute; inset: 0; z-index: -1; background: linear-gradient(90deg,var(--cover-paper) 0%,var(--cover-paper) 32%,color-mix(in srgb,var(--cover-paper) var(--cover-mask),transparent) 62%,transparent 100%); }
.sakura-cover-copy { padding: 20px 26px; width: min(62%,560px); min-width: 0; }
.sakura-cover-eyebrow { font-size: 12px; font-weight: 500; color: #8565bc; margin-bottom: 5px; }
.dark .sakura-cover-eyebrow { color: #c4a5ed; }
.sakura-cover-copy h2 { font-size: 23px; font-weight: 600; line-height: 1.35; letter-spacing: -.3px; color: var(--cover-ink); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.sakura-cover-copy p { margin-top: 7px; color: var(--cover-muted); font-size: 13px; }
.sakura-cover-actions { margin-top: 13px; display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.sakura-cover-drag { position: absolute; z-index: 1; inset: 0; width: 100%; height: 100%; padding: 0; border: 0; border-radius: inherit; background: transparent; cursor: grab; touch-action: pan-y pinch-zoom; }
.sakura-cover-drag:disabled { cursor: default; }
.sakura-cover-drag:focus-visible { outline: 2px solid #8565bc; outline-offset: -4px; }
.is-editable { user-select: none; }
.is-dragging .sakura-cover-drag { cursor: grabbing; }
@media(max-width:640px) {
  .sakura-cover-surface { height: 180px; }
  .sakura-cover-copy { padding: 20px; width: 88%; }
  .sakura-cover-copy h2 { font-size: 20px; }
  .sakura-cover-art { left: 15%; }
  .sakura-cover-veil { background: linear-gradient(90deg,var(--cover-paper),color-mix(in srgb,var(--cover-paper) 92%,transparent) 45%,color-mix(in srgb,var(--cover-paper) 35%,transparent)); }
}
</style>
