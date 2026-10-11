<template>
  <div ref="dashboardCard" data-testid="sakura-welcome-card">
    <SakuraCoverSurface :cover="saved" :eyebrow="words.eyebrow" :greeting="greeting" :subtitle="words.subtitle">
      <button type="button" class="cover-change" :disabled="loading" @click="loadError ? load() : openEditor()" data-testid="change-cover">
        <Icon name="edit" size="sm" /> {{ loadError ? words.retry : words.change }}
      </button>
      <span v-if="loadError" class="cover-message" role="status">{{ words.loadError }}</span>
    </SakuraCoverSurface>

    <BaseDialog :show="editing" :title="words.title" width="wide" :close-on-escape="!busy" :show-close-button="!busy" @close="cancel">
      <div class="cover-editor">
        <p class="cover-hint">{{ words.preview }}</p>
        <div ref="previewFrame" class="cover-preview-frame" data-testid="cover-preview-frame"
          :style="{ height: (previewLayout?.displayHeight ?? 180) + 'px' }">
          <SakuraCoverSurface v-if="previewLayout" class="cover-preview-canvas"
            :style="{ width: previewLayout.width + 'px', height: previewLayout.height + 'px', transform: `scale(${previewLayout.scale})` }"
            :cover="draft" :eyebrow="words.eyebrow" :greeting="greeting" :subtitle="words.subtitle"
            editable :disabled="busy" :drag-label="words.dragHint" @position-change="Object.assign(draft, $event)">
            <span class="cover-change" aria-hidden="true"><Icon name="edit" size="sm" /> {{ words.change }}</span>
          </SakuraCoverSurface>
        </div>
        <p v-if="draft.image_data" class="cover-hint cover-drag-hint">{{ words.dragHint }}</p>
        <div class="cover-presets" role="group" :aria-label="words.presets">
          <span class="cover-presets-label">{{ words.presets }}</span>
          <div class="cover-preset-list">
            <button v-for="item in presetCards" :key="item.id" type="button" class="cover-preset"
              :class="{ selected: selectedPreset === item.id }" :disabled="busy" :aria-pressed="selectedPreset === item.id" @click="choosePreset(item)">
              <img :src="item.image" alt="" /><span>{{ item.label }}</span>
            </button>
          </div>
        </div>
        <label class="cover-upload">
          <span>{{ words.upload }}</span>
          <input ref="fileInput" type="file" accept="image/jpeg,image/png,image/webp" :disabled="busy" @change="selectImage" data-testid="cover-file" />
        </label>
        <p class="cover-hint">{{ words.hint }}</p>
        <div class="cover-controls">
          <label>{{ words.mask }} <output>{{ draft.mask }}%</output><input v-model.number="draft.mask" type="range" min="60" max="95" :disabled="busy" data-testid="cover-mask" /></label>
        </div>
        <p v-if="error" class="cover-error" role="alert">{{ error }}</p>
        <p class="cover-hint" aria-live="polite">{{ processing ? words.processing : words.private }}</p>
      </div>
      <template #footer>
        <div class="cover-footer">
          <button type="button" class="btn btn-secondary cover-reset" :disabled="busy" @click="restoreDefault">{{ words.reset }}</button>
          <button type="button" class="btn btn-secondary" :disabled="busy" @click="cancel">{{ words.cancel }}</button>
          <button type="button" class="btn btn-primary" :disabled="busy" @click="save" data-testid="save-cover">{{ saving ? words.saving : words.save }}</button>
        </div>
      </template>
    </BaseDialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { defaultDashboardCover, getDashboardCover, prepareCoverImage, saveDashboardCover } from '@/api/sakuraCover'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import SakuraCoverSurface from './SakuraCoverSurface.vue'
import { fitCoverPreview } from './coverPreview'
import { coverPresets, renderCoverPreset } from './coverPresets'
import { useAppStore } from '@/stores/app'

const props = defineProps<{ displayName: string }>()
const { locale } = useI18n()
const app = useAppStore()
// Extension-local translations keep the upstream locale files untouched.
const messages = {
  zh: {
    eyebrow: '我的工作台', hello: '你好，', fallback: '朋友', subtitle: '欢迎回来，今天也按照自己的节奏前进。',
    change: '更换封面', title: '自定义仪表盘封面', preview: '按当前仪表盘尺寸等比预览，保存后构图一致。', upload: '选择图片', presets: '内置背景',
    hint: 'JPG / PNG / WebP，最大 5 MB、1600 万像素。横图和竖图均可；拖动让主体出现在右侧。',
    dragHint: '按住图片拖动以调整位置，也可用方向键微调；到边缘自动停止。',
    mask: '文字遮罩', private: '仅改变你自己的仪表盘；保存后可跨设备使用。',
    reset: '恢复默认', cancel: '取消', save: '保存封面', saving: '保存中…', processing: '正在处理图片…',
    saved: '封面已保存', retry: '重试', loadError: '封面暂时加载失败', saveError: '保存未确认，请重试或刷新后查看。',
    imageError: '请使用有效的 JPG、PNG 或 WebP 图片（5 MB / 1600 万像素以内，单边不超过 8192 像素）。'
  },
  en: {
    eyebrow: 'MY WORKSPACE', hello: 'Hello, ', fallback: 'there', subtitle: 'Welcome back. Make room for your next idea.',
    change: 'Change cover', title: 'Personal dashboard cover', preview: 'Scaled from your current dashboard. The saved crop matches this preview.', upload: 'Choose an image', presets: 'Built-in backgrounds',
    hint: 'JPG / PNG / WebP, up to 5 MB and 16 megapixels. Landscape or portrait; drag to place the subject on the right.',
    dragHint: 'Drag the image to reposition, or use arrow keys to fine-tune. Movement stops at the edges.',
    mask: 'Text overlay', private: 'Only changes your dashboard. Your saved cover follows your account across devices.',
    reset: 'Use default', cancel: 'Cancel', save: 'Save cover', saving: 'Saving…', processing: 'Processing image…',
    saved: 'Cover saved', retry: 'Retry', loadError: 'Cover could not be loaded', saveError: 'Save was not confirmed. Retry or reload to check.',
    imageError: 'Choose a valid JPG, PNG or WebP (up to 5 MB / 16 megapixels, max 8192 pixels per side).'
  }
}
const words = computed(() => locale.value.startsWith('zh') ? messages.zh : messages.en)
const greeting = computed(() => words.value.hello + (props.displayName || words.value.fallback))
const saved = ref(defaultDashboardCover()), draft = ref(defaultDashboardCover())
const presetCards = ref<{ id: string; label: string; image: string }[]>([])
const selectedPreset = ref('')
const editing = ref(false), loading = ref(true), loadError = ref(false), saving = ref(false), processing = ref(false), error = ref('')
const busy = computed(() => saving.value || processing.value)
const fileInput = ref<HTMLInputElement | null>(null)
const dashboardCard = ref<HTMLDivElement | null>(null), previewFrame = ref<HTMLDivElement | null>(null)
const cardSize = ref({ width: 0, height: 0 }), previewWidth = ref(0)
const previewLayout = computed(() => fitCoverPreview(cardSize.value, previewWidth.value))
let resizeObserver: ResizeObserver | null = null
watch(previewFrame, (element, previous) => {
  if (previous) resizeObserver?.unobserve(previous)
  previewWidth.value = 0
  if (element) resizeObserver?.observe(element)
})
const abort = new AbortController()
let disposed = false

async function load() {
  loading.value = true; loadError.value = false
  try { saved.value = await getDashboardCover(abort.signal) }
  catch { if (!disposed) loadError.value = true }
  finally { loading.value = false }
}
function openEditor() { draft.value = { ...saved.value }; selectedPreset.value = ''; error.value = ''; editing.value = true }
function cancel() { if (!busy.value) { editing.value = false; error.value = '' } }
function restoreDefault() { draft.value = defaultDashboardCover(); selectedPreset.value = ''; error.value = ''; if (fileInput.value) fileInput.value.value = '' }
function choosePreset(item: { id: string; image: string }) {
  draft.value = { ...draft.value, image_data: item.image, position_x: 70, position_y: 50 }
  selectedPreset.value = item.id
  error.value = ''
}
async function selectImage(event: Event) {
  const input = event.target as HTMLInputElement, file = input.files?.[0]
  if (!file) return
  processing.value = true; error.value = ''
  try { const data = await prepareCoverImage(file); if (!disposed) { draft.value.image_data = data; selectedPreset.value = '' } }
  catch { error.value = words.value.imageError }
  finally { processing.value = false; input.value = '' }
}
async function save() {
  if (busy.value) return
  saving.value = true; error.value = ''
  try {
    const result = await saveDashboardCover(draft.value, saved.value.image_data)
    if (!disposed) { saved.value = result; editing.value = false; app.showToast('success', words.value.saved, 3000) }
  } catch { error.value = words.value.saveError }
  finally { saving.value = false }
}
onMounted(() => {
  presetCards.value = coverPresets.map(preset => ({ id: preset.id, label: preset.label, image: renderCoverPreset(preset) }))
  resizeObserver = new ResizeObserver(entries => {
    for (const entry of entries) {
      const { width, height } = entry.contentRect
      if (entry.target === dashboardCard.value && (width !== cardSize.value.width || height !== cardSize.value.height)) {
        cardSize.value = { width, height }
      }
      if (entry.target === previewFrame.value && width !== previewWidth.value) previewWidth.value = width
    }
  })
  if (dashboardCard.value) resizeObserver.observe(dashboardCard.value)
  void load()
})
onBeforeUnmount(() => { disposed = true; abort.abort(); resizeObserver?.disconnect() })
</script>

<style scoped>
.cover-change { display: inline-flex; align-items: center; gap: 6px; padding: 6px 10px; min-height: 32px; font-size: 12px; border: 1px solid #e3d7f4; border-radius: 9px; background: #faf7fe; color: #6e4d9f; }
.cover-change:hover { background: #f0eafa; }
.cover-change:disabled { opacity: .55; cursor: wait; }
.dark .cover-change { background: #362b46; color: #e3d7f4; border-color: #59486f; }
.cover-message,.cover-hint { color: #75667f; font-size: 12px; }
.dark .cover-message,.dark .cover-hint { color: #bdb1cb; }
.cover-editor { display: grid; gap: 14px; }
.cover-preview-frame { position: relative; min-width: 0; overflow: hidden; }
.cover-preview-canvas { position: absolute; left: 0; top: 0; transform-origin: top left; }
.cover-upload { display: grid; gap: 8px; font-size: 14px; }
.cover-presets { display: grid; gap: 8px; }
.cover-presets-label { font-size: 13px; color: #50435d; }
.cover-preset-list { display: grid; grid-template-columns: repeat(3, minmax(0,1fr)); gap: 10px; }
.cover-preset { overflow: hidden; min-width: 0; padding: 0; border: 2px solid #e0d7e9; border-radius: 8px; background: #faf7fe; color: #50435d; text-align: left; }
.cover-preset.selected,.cover-preset:focus-visible { border-color: #8565bc; outline: none; }
.cover-preset img { display: block; width: 100%; height: 62px; object-fit: cover; }
.cover-preset span { display: block; padding: 6px 8px; font-size: 12px; }
.cover-preset:disabled { opacity: .6; }
.dark .cover-preset { background: #2c2538; border-color: #59486f; color: #e4ddec; }
.dark .cover-preset.selected { border-color: #c4a5ed; }
.cover-upload input { width: 100%; min-width: 0; padding: 12px; border: 1px dashed #d7cede; border-radius: 12px; background: #faf7fe; color: #50435d; font-size: 13px; }
.dark .cover-upload input { background: #2c2538; border-color: #59486f; color: #e4ddec; }
.cover-controls { display: grid; grid-template-columns: minmax(0,1fr); gap: 20px; }
.cover-controls label { font-size: 13px; }
.cover-controls output { float: right; font-variant-numeric: tabular-nums; }
.cover-controls input { display: block; width: 100%; margin-top: 12px; accent-color: #8565bc; }
.cover-error { color: #dc2626; font-size: 13px; }
.dark .cover-error { color: #fca5a5; }
.cover-footer { display: flex; gap: 10px; flex-wrap: wrap; width: 100%; }
.cover-reset { margin-right: auto; }
@media(max-width:640px) { .cover-controls { grid-template-columns: 1fr; gap: 16px; } }
@media(pointer:coarse) { .cover-change { min-height: 44px; } .cover-controls input { min-height: 28px; } }
</style>
