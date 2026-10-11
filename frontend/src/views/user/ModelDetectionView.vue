<template>
  <AppLayout>
    <main class="detection-page">
      <header class="detection-heading">
        <div><span class="eyebrow">MODEL QUALITY LAB</span><h1>模型检测</h1><p>相同任务、相同提示词，对比不同上游的实际输出。</p></div>
        <button class="run-button" type="button" :disabled="running || submitting || !selectedKeyId || !model || !prompt || !hasBalance" @click="runDetection"><Icon name="play" size="sm" />{{ running || submitting ? '检测中…' : '开始检测' }}</button>
      </header>
      <p class="detection-note">每次选择一个分组与模型，按该分组实际用量计费。使用相同提示词重复检测，比较画面、推理与回答质量。提示词与原始回答对站内用户可见，最近保留 20 条。</p>
      <section class="detection-config" aria-label="检测配置">
        <div class="config-field"><label for="key-select">检测分组</label><Select id="key-select" v-model="selectedKeyId" :options="groupOptions" placeholder="选择检测分组" :searchable="true" :disabled="running || submitting" aria-label="检测分组" /><p v-if="!groupKeys.length" class="field-note">暂无可用分组，请先在 <a href="/keys">API 密钥页面</a>创建对应分组的检测密钥。</p></div>
        <div class="config-field"><label for="model-select">检测模型</label><Select id="model-select" v-model="model" :options="modelOptions" :loading="modelsLoading" search-placeholder="搜索模型或输入模型 ID" :searchable="true" :creatable="true" creatable-prefix="使用模型 ID" :placeholder="modelsLoading ? '正在读取模型…' : '选择或输入模型 ID'" :disabled="running || submitting || !selectedKeyId" aria-label="检测模型" /><p class="field-note" role="status">{{ modelsLoading ? '正在读取当前分组的模型' : modelListError ? '模型列表读取失败，可搜索并输入模型 ID。' : availableModels.length ? `当前分组列出 ${availableModels.length} 个模型；如列表不全，可搜索并输入模型 ID。` : '当前分组没有列出模型，可搜索并输入模型 ID。' }}</p></div>
        <div class="config-field"><label for="topic-select">检测主题</label><Select id="topic-select" v-model="topic" :options="topicOptions" :disabled="running || submitting" aria-label="检测主题" /></div>
        <div class="config-field balance-field"><span>我的余额</span><strong>${{ (auth.user?.balance || 0).toFixed(2) }}</strong><p>{{ hasBalance ? "检测费用从自己的余额扣除，管理员同样计费。" : "请充值后再开始检测，可先浏览已有作品。" }}</p></div>
      </section>
      <p v-if="error" class="detection-error" role="alert">{{ error }}</p>
      <section class="detection-stats" aria-label="检测统计"><div><span>检测完成</span><strong>{{ state.generated }}</strong><small>全站累计完成检测</small></div><div><span>生成失败</span><strong>{{ state.failed }}</strong><small>全站累计失败记录</small></div><div><span>全站已保存</span><strong>{{ state.items.length }}</strong><small>最近保留的作品</small></div><div><span>记录保留上限</span><strong>20</strong><small>超出后自动清理最旧作品</small></div></section>
      <div class="detection-toolbar"><div><strong class="gallery-title">共享作品区</strong><p class="gallery-note">展示所有分组的历史检测；作品标注的是当时请求的模型 ID，不代表当前分组可用模型。</p><div class="topic-tabs"><button v-for="option in topics" :key="option.name" type="button" :class="{ active: topic === option.name }" :disabled="running || submitting" @click="topic = option.name">{{ option.name }}</button></div></div><div class="filters"><input v-model.trim="search" aria-label="搜索模型或分组" placeholder="搜索模型或分组" /><Select v-model="filterStatus" :options="statusOptions" aria-label="筛选状态" /><button type="button" title="刷新" :disabled="loading" @click="load()"><Icon name="refresh" size="sm" /></button></div></div>
      <div class="prompt-display"><label for="prompt-input">本次提示词 · {{ topic }}</label><textarea v-if="topic === '自定义'" id="prompt-input" v-model="prompt" :disabled="running || submitting" rows="4" maxlength="4000" placeholder="输入要比较的任务，原始回答将对站内用户可见" /><p v-else>{{ prompt }}</p></div>
      <div v-if="running" class="detection-progress" role="status">正在后台检测所选分组与模型，最长 8 分钟。可离开或刷新页面，查询进度不会重复调用模型。</div>
      <div v-if="loading" role="status">加载中…</div>
      <div v-else-if="!filteredItems.length" class="detection-empty">当前没有匹配的检测记录。</div>
      <section v-else class="detection-grid" aria-label="检测作品">
        <article v-for="item in filteredItems" :key="item.id" class="detection-result"><header><div><small>{{ item.platform?.toUpperCase() }} · {{ item.key_name }}</small><h2>{{ item.model }}</h2><p>{{ formatDate(item.created_at) }} · {{ item.topic }}</p></div><span :class="item.status">{{ item.status === 'generated' ? '已生成' : item.status === 'running' ? '检测中' : '失败' }}</span></header><div v-if="item.status === 'generated' && item.html" class="preview"><iframe :srcdoc="sandboxDocument(item.html)" sandbox="" title="模型生成的 HTML 预览" loading="lazy" /><div class="preview-actions"><span>SVG · HTML 动画</span><button type="button" @click="preview = item">放大观看 <Icon name="externalLink" size="xs" /></button><button type="button" @click="download(item)">下载 HTML</button></div></div><div v-else-if="item.status === 'generated'" class="text-answer"><small>模型原始回答</small><pre>{{ item.text }}</pre><button type="button" @click="download(item)">下载回答</button></div><div v-else-if="item.status === 'running'" class="detection-progress">模型正在生成作品…</div><div v-else class="failure">{{ item.error || '检测未完成' }}</div><footer><div><b>主题</b><span>{{ item.topic }}</span></div><div><b>耗时</b><span>{{ (item.duration_ms / 1000).toFixed(1) }} 秒</span></div><div><b>作品</b><span>全站共享</span></div></footer><details><summary>查看生成提示词</summary><p>{{ item.prompt }}</p></details><button v-if="item.status !== 'running' && (item.user_id === auth.user?.id || auth.isAdmin)" type="button" class="delete-button" @click="remove(item)">删除记录</button></article>
      </section>
      <div v-if="preview" class="preview-modal" role="dialog" aria-modal="true" aria-label="放大预览" @click.self="preview = null"><div class="modal-panel"><div class="modal-heading"><strong>{{ preview.model }} · {{ preview.key_name }}</strong><button type="button" aria-label="关闭预览" @click="preview = null"><Icon name="x" size="md" /></button></div><iframe :srcdoc="sandboxDocument(preview.html)" sandbox="" title="放大预览" /></div></div>
    </main>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import Select from '@/components/common/Select.vue'
import { keysAPI } from '@/api/keys'
import { buildGatewayUrl } from '@/api/client'
import { deleteDetection, listDetections, startDetection, type DetectionRecord, type DetectionState } from '@/api/modelDetection'
import { sandboxDocument } from '@/utils/modelDetection'
import { useAuthStore } from '@/stores/auth'
import type { ApiKey } from '@/types'

const auth = useAuthStore()
const topics = [
  { name: '鹈鹕骑自行车', prompt: '创建一个完整的单文件 HTML，内容是 SVG 绘制的鹈鹕骑自行车的海边 2D 动画。画面要有明确的角色、海岸背景和连续的骑行动作。CSS 和 SVG 必须内嵌，不引用外部资源。使用 CSS 或 SVG 原生动画，不使用 JavaScript。只输出完整 HTML。' },
  { name: '糖果问题', prompt: `在一个黑色的袋子里放有三种口味的糖果，每种糖果有两种不同的形状（圆形和五角星形，不同的形状靠手感可以分辨）。现已知不同口味的糖和不同形状的数量统计如下表。参赛者需要在活动前决定摸出的糖果数目，那么，最少取出多少个糖果才能保证手中同时拥有不同形状的苹果味和桃子味的糖？（同时手中有圆形苹果味匹配五角星桃子味糖果，或者有圆形桃子味匹配五角星苹果味糖果都满足要求）

        苹果味 桃子味 西瓜味
圆形       7     9     8
五角星形   7     6     4

只返回结果数值。` },
  { name: '自定义', prompt: '' },
]
const topic = ref(topics[0].name)
const topicOptions = topics.map(item => ({ value: item.name, label: item.name }))
const statusOptions = [{ value: 'all', label: '全部状态' }, { value: 'generated', label: '已生成' }, { value: 'failed', label: '失败' }, { value: 'running', label: '检测中' }]
const prompt = ref(topics[0].prompt)
const customPrompt = ref('')
const hasBalance = computed(() => (auth.user?.balance || 0) > 0)
watch(topic, (value, previous) => {
  if (previous === '自定义') customPrompt.value = prompt.value
  prompt.value = value === '自定义' ? customPrompt.value : topics.find(item => item.name === value)!.prompt
})
const model = ref('')
const keys = ref<ApiKey[]>([])
const selectedKeyId = ref('')
const groupKeys = computed(() => {
  const byGroup = new Map<number, ApiKey>()
  for (const key of keys.value) {
    if (key.group_id && !key.ip_whitelist?.length && !key.ip_blacklist?.length && !byGroup.has(key.group_id)) byGroup.set(key.group_id, key)
  }
  return [...byGroup.values()]
})
const groupOptions = computed(() => groupKeys.value.map(key => ({ value: String(key.id), label: key.group?.name || '默认分组' })))
const availableModels = ref<string[]>([])
const modelOptions = computed(() => availableModels.value.map(name => ({ value: name, label: name })))
const modelsLoading = ref(false)
const modelListError = ref(false)
const state = ref<DetectionState>({ generated: 0, failed: 0, items: [] })
const search = ref('')
const filterStatus = ref('all')
const loading = ref(false)
const submitting = ref(false)
const running = computed(() => state.value.items.some(item => item.status === 'running' && item.user_id === auth.user?.id))
let poll: ReturnType<typeof setTimeout> | undefined
let disposed = false
const error = ref('')
const preview = ref<DetectionRecord | null>(null)
const filteredItems = computed(() => (state.value.items || []).filter((item) => (item.topic === topic.value) && (filterStatus.value === 'all' || item.status === filterStatus.value) && (!search.value || `${item.model} ${item.key_name} ${item.topic}`.toLowerCase().includes(search.value.toLowerCase()))))

async function load(quiet = false) {
  if (!quiet) loading.value = true
  try { state.value = await listDetections(); state.value.items ||= [] }
  catch { error.value = '检测记录加载失败，请重试。' }
  finally {
    loading.value = false
    clearTimeout(poll)
    if (!disposed && state.value.items.some(item => item.status === 'running')) poll = setTimeout(() => { if (!disposed) void load(true) }, 5000)
  }
}
async function loadKeys() {
  try {
    let page = 1, total = 1
    const entries: ApiKey[] = []
    while (entries.length < total) {
      const result = await keysAPI.list(page++, 100, { status: 'active' })
      entries.push(...result.items); total = result.total
      if (!result.items.length) break
    }
    keys.value = entries.filter(key => key.status === 'active' && (!key.expires_at || Date.parse(key.expires_at) > Date.now()) && (!key.quota || key.quota_used < key.quota))
    if (!selectedKeyId.value && groupKeys.value.length) selectedKeyId.value = String(groupKeys.value[0].id)
  }
  catch { error.value = '密钥列表加载失败。' }
}
watch(selectedKeyId, async (id, _, cleanup) => {
  const controller = new AbortController()
  let stale = false
  const timeout = setTimeout(() => controller.abort(), 15000)
  cleanup(() => { stale = true; clearTimeout(timeout); controller.abort() })
  availableModels.value = []
  model.value = ''
  modelsLoading.value = !!id
  modelListError.value = false
  const key = keys.value.find((entry) => String(entry.id) === id)
  if (!key) { clearTimeout(timeout); modelsLoading.value = false; return }
  try {
    const response = await fetch(buildGatewayUrl('/v1/models'), { headers: { Authorization: `Bearer ${key.key}` }, signal: controller.signal })
    if (!response.ok) throw new Error('models')
    const data = await response.json() as { data?: Array<{ id: string }> }
    if (stale) return
    availableModels.value = [...new Set((data.data || []).map((item) => item.id).filter((id): id is string => typeof id === 'string' && id.trim().length > 0))].sort()
  } catch { if (!stale) modelListError.value = true }
  finally { clearTimeout(timeout); if (!stale) modelsLoading.value = false }
})
async function runDetection() {
  if (running.value || submitting.value || !model.value?.trim() || !prompt.value || !selectedKeyId.value || !hasBalance.value) return
  submitting.value = true; error.value = ''
  try {
    const item = await startDetection({ key_id: Number(selectedKeyId.value), model: model.value.trim(), topic: topic.value, prompt: prompt.value })
    state.value.items = [item, ...state.value.items.filter(entry => entry.id !== item.id)].slice(0, 20)
    await load(true)
  } catch (cause: any) { error.value = cause?.response?.data?.message || cause?.message || '检测任务提交失败' }
  finally { submitting.value = false }
}
function formatDate(value: string) { return new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value)) }
function download(item: DetectionRecord) { const url = URL.createObjectURL(new Blob([item.html ? sandboxDocument(item.html) : item.text], { type: item.html ? 'text/html;charset=utf-8' : 'text/plain;charset=utf-8' })); const link = document.createElement('a'); link.href = url; link.download = `model-detection-${item.id}.${item.html ? 'html' : 'txt'}`; link.click(); setTimeout(() => URL.revokeObjectURL(url), 1000) }
async function remove(item: DetectionRecord) { if (!window.confirm('删除这条检测记录？')) return; try { await deleteDetection(item.id); state.value.items = state.value.items.filter((entry) => entry.id !== item.id) } catch { error.value = '删除失败，请重试。' } }
onMounted(() => { void load(); void loadKeys() })
onUnmounted(() => { disposed = true; clearTimeout(poll) })
</script>

<style scoped>
.detection-page{max-width:1440px;margin:auto;padding:8px 0 56px;color:#172238}.detection-heading{display:flex;align-items:end;justify-content:space-between;gap:24px;padding:24px 28px 22px;background:#f6f0fc;border:1px solid #e3d7f4;border-radius:16px}.eyebrow{font:700 11px ui-monospace,monospace;color:#7655aa}.detection-heading h1{font-size:34px;font-weight:700;margin:12px 0}.detection-heading p{font-size:14px;color:#627184}.run-button{display:flex;align-items:center;gap:9px;background:#7655aa;color:white;border:0;border-radius:7px;padding:12px 18px;font-weight:700;white-space:nowrap}.run-button:disabled{opacity:.5}.detection-note{margin:16px 0;padding:13px 16px;border:1px solid #e3d7f4;border-radius:16px;background:#faf7fe;color:#6e4d9f;font-size:13px}.detection-config{display:grid;grid-template-columns:1fr 1fr;gap:16px;padding:18px;border:1px solid #dce4e9;border-radius:16px;background:#fff}.config-field label{display:block;font-size:12px;color:#647287;margin-bottom:7px}.config-field label span{font-size:11px;color:#8894a4}.config-field input:not([type=checkbox]),.config-field select,.config-field textarea,.filters input,.filters select{width:100%;border:1px solid #dce3e9;border-radius:6px;background:#fff;padding:10px 12px;color:#182234;font-size:13px}.config-field textarea{resize:vertical}.key-options{display:flex;flex-wrap:wrap;gap:8px;max-height:110px;overflow:auto}.key-option{display:flex!important;align-items:center;gap:8px;border:1px solid #dce3e9;border-radius:6px;padding:9px 11px;margin:0!important;color:#253447!important}.key-option.chosen{border-color:#0d9187;background:#e9f8f5}.key-option small{color:#647287}.detection-error{margin:12px 0;color:#b43e42}.detection-stats{display:grid;grid-template-columns:repeat(4,1fr);gap:12px;margin:20px 0}.detection-stats>div{border:1px solid #dce3e9;background:#fff;border-radius:16px;padding:18px}.detection-stats span,.detection-stats small{display:block;color:#647287;font-size:12px}.detection-stats strong{display:block;margin:10px 0 4px;font-size:28px}.detection-toolbar{display:flex;justify-content:space-between;gap:16px;align-items:center;margin:24px 0 12px}.topic-tabs,.filters{display:flex;gap:8px;flex-wrap:wrap}.topic-tabs button,.filters button{border:1px solid #dce3e9;background:#fff;border-radius:6px;padding:9px 12px;color:#506174;font-size:13px}.topic-tabs button.active{background:#f0eafa;border-color:#c4a5ed;color:#7655aa}.filters input{width:180px}.filters select{width:135px}.filters button{display:grid;place-items:center}.prompt-display{white-space:pre-wrap;border:1px solid #dce3e9;background:#fff;border-radius:16px;padding:14px 18px;font-size:13px;margin-bottom:18px}.prompt-display label{display:block;color:#788797;margin-bottom:10px}.prompt-display textarea{width:100%;min-height:110px;background:transparent;border:1px solid #dce3e9;border-radius:6px;padding:12px}.text-answer{margin-top:16px;min-height:200px;padding:20px;background:#f7f9fa;border-radius:8px}.text-answer small{color:#647287}.text-answer pre{white-space:pre-wrap;overflow-wrap:anywhere;font-family:inherit;font-size:15px;line-height:1.8;margin:14px 0;max-height:480px;overflow:auto}.text-answer button{color:#7655aa;font-size:12px}.balance-field strong{display:block;font-size:24px;margin:6px 0}.balance-field p{font-size:12px;color:#647287}.dark .text-answer{background:#312a3c}.prompt-display small{display:block;color:#788797;margin-bottom:7px}.detection-progress,.detection-empty{padding:22px;text-align:center;color:#617186}.detection-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:14px}.detection-result{min-width:0;border:1px solid #dce3e9;background:#fff;border-radius:16px;padding:18px}.detection-result header{display:flex;justify-content:space-between;gap:10px;align-items:start}.detection-result header small{font:700 10px ui-monospace,monospace;color:#7655aa}.detection-result h2{font-size:18px;margin:9px 0}.detection-result header p{font-size:12px;color:#79889b}.detection-result header>span{border-radius:5px;padding:5px 8px;font-size:11px;background:#e7f7ef;color:#138052}.detection-result header>span.running{background:#fff4d6;color:#966d1b}.detection-result header>span.failed{background:#fbeaea;color:#b7444a}.preview{margin-top:16px;border:1px solid #e1e7ed;border-radius:7px;overflow:hidden}.preview iframe{display:block;width:100%;height:320px;border:0;background:#f7f9fa}.preview-actions{display:flex;align-items:center;gap:8px;padding:10px 12px;font-size:11px;color:#6d7c8d}.preview-actions span{margin-right:auto}.preview-actions button{display:flex;align-items:center;gap:4px;color:#7655aa}.failure{padding:50px 16px;color:#a23c42;background:#fff6f6;margin-top:16px;border-radius:7px}.detection-result footer{display:grid;grid-template-columns:repeat(3,1fr);gap:8px;margin-top:14px;border-top:1px solid #e4e9ee;padding-top:13px}.detection-result footer b,.detection-result footer span{display:block;font-size:11px}.detection-result footer b{color:#273445}.detection-result footer span{color:#788797;margin-top:5px}.detection-result details{margin-top:12px;font-size:12px;color:#7655aa}.detection-result details p{white-space:pre-wrap;color:#566678;margin-top:8px}.delete-button{margin-top:10px;color:#b7444a;font-size:11px}.preview-modal{position:fixed;inset:0;z-index:100;background:#121c2abc;display:grid;place-items:center;padding:24px}.modal-panel{width:min(1100px,100%);height:min(780px,95vh);background:#fff;display:flex;flex-direction:column;border-radius:16px;overflow:hidden}.modal-heading{display:flex;justify-content:space-between;align-items:center;padding:12px 16px}.modal-panel iframe{flex:1;border:0}.dark .detection-page{color:#e9edf2}.dark .detection-heading,.dark .detection-note{background:#30253e;border-color:#59486f}.dark .detection-config,.dark .detection-stats>div,.dark .topic-tabs button,.dark .filters button,.dark .prompt-display,.dark .detection-result{background:#251f2e;border-color:#4a4055}.dark .config-field input:not([type=checkbox]),.dark .config-field select,.dark .config-field textarea,.dark .filters input,.dark .filters select{background:#312a3c;color:#fff;border-color:#574d62}@media(max-width:900px){.detection-config,.detection-grid{grid-template-columns:1fr}.detection-stats{grid-template-columns:repeat(2,1fr)}.detection-toolbar{align-items:stretch;flex-direction:column}}@media(max-width:600px){.detection-heading{align-items:start;flex-direction:column}.detection-stats{gap:7px}.detection-stats>div{padding:12px}}
.config-field :deep(.select-trigger), .filters :deep(.select-trigger) { min-height: 40px; border-radius: 12px; border-color: #e3d7f4; background: #fff; color: #182234; font-size: 13px; }
.config-field :deep(.select-trigger:focus-visible), .filters :deep(.select-trigger:focus-visible) { border-color: #8565bc; box-shadow: 0 0 0 3px #8565bc33; }
.field-note { margin-top: 7px; color: #758296; font-size: 11px; line-height: 1.5; }
.field-note a { color: #7655aa; text-decoration: underline; }
.gallery-note { margin-bottom: 10px; color: #758296; font-size: 12px; line-height: 1.5; }
.gallery-title { display: block; margin-bottom: 10px; color: #30263f; font-size: 16px; }
.filters :deep(.select-trigger) { width: 135px; }
.dark .config-field :deep(.select-trigger), .dark .filters :deep(.select-trigger) { background: #312a3c; color: #fff; border-color: #574d62; }
.dark .gallery-title { color: #f3edf8; }
</style>
