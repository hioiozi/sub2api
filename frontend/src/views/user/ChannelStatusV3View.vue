<template>
  <AppLayout>
    <div class="space-y-6 pb-12">
      <section class="card !rounded-3xl !border-0 p-5 shadow-sm ring-1 ring-gray-900/5 dark:!bg-dark-800 dark:ring-dark-700 sm:p-6">
        <div class="flex flex-wrap items-start justify-between gap-4">
          <div class="min-w-0">
            <div class="flex items-center gap-2">
              <span class="inline-flex h-9 w-9 items-center justify-center rounded-2xl bg-emerald-50 text-emerald-600 dark:bg-emerald-500/15 dark:text-emerald-300"><Icon name="chart" size="sm" /></span>
              <h1 class="text-xl font-black text-gray-900 dark:text-white">{{ t('channelMonitorV3.title') }}</h1>
            </div>
            <div v-if="isAdmin" class="mt-3 flex flex-wrap items-center gap-2 text-xs text-gray-500 dark:text-gray-400">
              <span class="inline-flex items-center gap-1.5"><i class="h-2 w-2 rounded-full" :class="loading ? 'bg-gray-400' : 'bg-emerald-500'" />{{ statusText }}</span>
              <span v-if="coverage?.aggregation_lag_seconds != null" class="badge badge-gray">{{ t('channelMonitorV3.lag', { seconds: coverage.aggregation_lag_seconds }) }}</span>
            </div>
          </div>
          <div class="flex flex-wrap items-center gap-2">
            <div class="tabs inline-flex" role="group" :aria-label="t('channelMonitorV3.rangeAria')">
              <button v-for="option in ranges" :key="option.value" type="button" class="tab !px-2.5 !py-1.5 text-xs" :class="range === option.value ? 'tab-active' : ''" @click="setRange(option.value)">{{ option.label }}</button>
            </div>
            <button class="btn btn-secondary btn-icon h-9 w-9" type="button" :disabled="loading" :title="t('common.refresh')" @click="load"><Icon name="refresh" size="sm" :class="loading ? 'animate-spin' : ''" /></button>
          </div>
        </div>
      </section>

      <section class="grid grid-cols-2 gap-3 sm:grid-cols-4" :aria-label="t('channelMonitorV3.summaryAria')">
        <div v-for="item in summary" :key="item.label" class="card !rounded-2xl !border-0 p-4 shadow-sm ring-1 ring-gray-900/5 dark:!bg-dark-800 dark:ring-dark-700"><span class="text-xs font-semibold text-gray-500 dark:text-gray-400">{{ item.label }}</span><strong class="mt-2 block text-2xl font-black tabular-nums text-gray-900 dark:text-white">{{ item.value }}</strong></div>
      </section>

      <section class="flex flex-wrap items-center justify-between gap-3">
        <h2 class="text-sm font-bold text-gray-900 dark:text-white">{{ t('channelMonitorV3.groupsTitle') }}</h2>
        <div class="flex items-center gap-2"><input v-model="search" class="input h-9 w-44 text-xs sm:w-56" :placeholder="t('channelMonitorV3.searchPlaceholder')" /><select v-model="platform" class="input h-9 w-32 text-xs"><option value="">{{ t('channelMonitorV3.allPlatforms') }}</option><option v-for="item in platforms" :key="item" :value="item">{{ item }}</option></select></div>
      </section>
      <section v-if="loading && !cards.length" class="grid gap-4 sm:grid-cols-2 xl:grid-cols-3"><div v-for="n in 6" :key="n" class="h-[310px] animate-pulse rounded-3xl bg-gray-100 dark:bg-dark-800/60" /></section>
      <section v-else-if="filteredCards.length" class="grid gap-4 sm:grid-cols-2 xl:grid-cols-3"><GroupHealthCard v-for="card in filteredCards" :key="card.key" :row="card.row" :group-name="card.name" :platform="card.platform" :effective-rate="card.rate" /></section>
      <section v-else class="card !rounded-3xl !border-0 py-16 text-center shadow-sm ring-1 ring-gray-900/5 dark:!bg-dark-800 dark:ring-dark-700"><Icon name="chart" size="lg" class="mx-auto text-gray-300 dark:text-dark-500" /><h2 class="mt-4 text-base font-bold text-gray-800 dark:text-gray-100">{{ t('channelMonitorV3.emptyTitle') }}</h2><p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('channelMonitorV3.emptyDescription') }}</p></section>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import GroupHealthCard from '@/components/user/monitor/GroupHealthCard.vue'
import * as monitorApi from '@/api/channelMonitorV2'
import type { MonitorCoverage, MonitorMatrixRow, MonitorRange } from '@/api/channelMonitorV2'
import userGroupsAPI from '@/api/groups'
import { adminAPI } from '@/api/admin'
import { useAuthStore } from '@/stores/auth'
import { extractApiErrorMessage } from '@/utils/apiError'

const { t } = useI18n()
const authStore = useAuthStore()
const isAdmin = computed(() => authStore.isAdmin)
const range = ref<MonitorRange>('90m')
const search = ref('')
const platform = ref('')
const rows = ref<MonitorMatrixRow[]>([])
const groups = ref<Array<{ id: number; name: string; platform?: string; rate_multiplier?: number }>>([])
const rates = ref<Record<number, number>>({})
const coverage = ref<MonitorCoverage | null>(null)
const loading = ref(false)
let controller: AbortController | null = null
let timer: number | null = null
const ranges = computed(() => (['90m', '24h', '7d', '30d'] as MonitorRange[]).map(value => ({ value, label: t(`channelMonitorV3.ranges.${value}`) })))
const platforms = computed(() => [...new Set(rows.value.map(row => row.platform).filter(Boolean))].sort())
const cards = computed(() => {
  const best = new Map<number, MonitorMatrixRow>()
  for (const row of rows.value) {
    if (row.group_id == null) continue
    const current = best.get(row.group_id)
    if (!current || row.metrics.request_count > current.metrics.request_count) best.set(row.group_id, row)
  }
  return [...best.values()].map(row => {
    const group = groups.value.find(item => item.id === row.group_id)
    return { key: String(row.group_id), row, name: group?.name || row.group_name || `#${row.group_id}`, platform: group?.platform || row.platform, rate: rates.value[row.group_id!] ?? group?.rate_multiplier ?? null }
  })
})
const filteredCards = computed(() => {
  const query = search.value.trim().toLowerCase()
  return cards.value.filter(card => (!platform.value || card.platform === platform.value) && (!query || `${card.name} ${card.platform}`.toLowerCase().includes(query)))
})
const summary = computed(() => [
  { label: t('channelMonitorV3.summary.groups'), value: cards.value.length },
  { label: t('channelMonitorV3.summary.healthy'), value: cards.value.filter(card => card.row.health.overall === 'healthy').length },
  { label: t('channelMonitorV3.summary.watch'), value: cards.value.filter(card => card.row.health.overall === 'warning').length },
  { label: t('channelMonitorV3.summary.critical'), value: cards.value.filter(card => card.row.health.overall === 'critical').length },
])
const statusText = computed(() => coverage.value?.data_through ? t('channelMonitorV3.updatedTo', { time: new Date(coverage.value.data_through).toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' }) }) : t('channelMonitorV3.waiting'))

async function load() {
  controller?.abort()
  controller = new AbortController()
  loading.value = true
  try {
    const filter: monitorApi.MonitorFilter = { range: range.value, platforms: [], groupIds: [], models: [] }
    const [matrix, availableGroups, userRates] = await Promise.all([
      monitorApi.getMatrix(filter, 'platform_group', isAdmin.value, controller.signal),
      (isAdmin.value ? adminAPI.groups.getAllIncludingInactive() : userGroupsAPI.getAvailable()).catch(() => []),
      userGroupsAPI.getUserGroupRates().catch(() => ({})),
    ])
    if (controller.signal.aborted) return
    rows.value = matrix.items || []
    coverage.value = matrix.coverage
    groups.value = availableGroups
    rates.value = userRates
  } catch (error) {
    if (!controller.signal.aborted) console.error(extractApiErrorMessage(error, t('channelMonitorV3.loadFailed')))
  } finally {
    if (!controller.signal.aborted) loading.value = false
  }
}
function setRange(value: MonitorRange) { if (range.value !== value) { range.value = value; void load() } }
onMounted(() => { void load(); timer = window.setInterval(() => { if (!document.hidden) void load() }, 60_000) })
onBeforeUnmount(() => { controller?.abort(); if (timer != null) window.clearInterval(timer) })
</script>
