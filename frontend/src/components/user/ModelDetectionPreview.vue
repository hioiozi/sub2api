<template>
  <div ref="host" class="work-preview" :style="{ height: `${700 * scale}px` }">
    <iframe :srcdoc="sandboxDocument(html)" sandbox="" title="模型生成的 HTML 预览" loading="lazy" :style="{ transform: `scale(${scale})` }" />
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useResizeObserver } from '@vueuse/core'
import { sandboxDocument } from '@/utils/modelDetection'
defineProps<{ html: string }>()
const host = ref<HTMLElement>()
const scale = ref(0.5)
useResizeObserver(host, (entries) => { scale.value = Math.max(0.1, entries[0].contentRect.width / 1000) })
</script>

<style scoped>
.work-preview { position: relative; overflow: hidden; background: #f7f9fa; }
.work-preview iframe { display: block; width: 1000px; height: 700px; max-width: none; border: 0; transform-origin: top left; }
</style>
