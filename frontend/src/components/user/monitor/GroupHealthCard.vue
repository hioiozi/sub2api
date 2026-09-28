<template>
  <article class="group-health-card" :class="`group-health-card--${state}`">
    <header class="group-health-card__header">
      <div class="group-health-card__identity">
        <span class="group-health-card__provider-icon" aria-hidden="true">
          <Icon name="chart" size="sm" />
        </span>
        <div class="min-w-0">
          <h2 class="truncate text-base font-bold text-gray-900 dark:text-white">{{ displayName }}</h2>
          <div class="mt-1 flex flex-wrap items-center gap-1.5 text-[11px] text-gray-500 dark:text-gray-400">
            <span class="group-health-card__tag">{{ displayPlatform }}</span>
            <span v-if="effectiveRate != null" class="group-health-card__tag group-health-card__tag--rate">
              {{ t('channelMonitorV3.rate') }} ×{{ formatRate(effectiveRate) }}
            </span>
          </div>
        </div>
      </div>
      <span class="group-health-card__status" :class="`group-health-card__status--${state}`">
        <i aria-hidden="true" /> {{ t(`channelMonitorV3.health.${state}`) }}
      </span>
    </header>

    <div class="group-health-card__metrics" :aria-label="t('channelMonitorV3.metricsAria')">
      <div class="group-health-card__metric">
        <span>{{ t('channelMonitorV3.metrics.cacheRate') }}</span>
        <strong>{{ hasTraffic ? formatPercent(row.metrics.cache_rate) : '-' }}</strong>
      </div>
      <div class="group-health-card__metric">
        <span>{{ t('channelMonitorV3.metrics.availability') }}</span>
        <strong :class="valueClass(row.health.error_rate)">{{ hasTraffic ? formatPercent(1 - row.metrics.error_rate) : '-' }}</strong>
      </div>
      <div class="group-health-card__metric">
        <span>{{ t('channelMonitorV3.metrics.ttft') }}</span>
        <strong :class="valueClass(row.health.ttft)">{{ formatMs(row.metrics.ttft.p50_ms) }}</strong>
      </div>
    </div>

    <div class="group-health-card__timeline-head">
      <span>{{ t('channelMonitorV3.timeline.recent') }}</span>
      <span>{{ latestTime }}</span>
    </div>
    <div class="group-health-card__timeline" :aria-label="t('channelMonitorV3.timeline.aria')">
      <span
        v-for="(bucket, index) in timeline"
        :key="`${row.group_id ?? row.group_name}-${index}`"
        class="group-health-card__pulse"
        :class="bucket ? pulseClass(bucket) : 'group-health-card__pulse--empty'"
        :title="bucket ? bucketTitle(bucket) : t('channelMonitorV3.timeline.noTraffic')"
      />
    </div>
    <footer class="group-health-card__footer">
      <span>{{ t('channelMonitorV3.timeline.past') }}</span>
      <span>{{ t('channelMonitorV3.timeline.now') }}</span>
    </footer>
  </article>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import type { MonitorHealth, MonitorMatrixBucket, MonitorMatrixRow } from '@/api/channelMonitorV2'

const props = defineProps<{
  row: MonitorMatrixRow
  groupName?: string
  platform?: string
  effectiveRate?: number | null
}>()

const { t } = useI18n()
const displayName = computed(() => props.groupName || props.row.group_name || `#${props.row.group_id ?? '-'}`)
const displayPlatform = computed(() => props.platform || props.row.platform || t('channelMonitorV3.platformUnknown'))
const hasTraffic = computed(() => props.row.metrics.request_count > 0)
const state = computed<'unknown' | 'healthy' | 'warning' | 'critical'>(() => {
  if (!hasTraffic.value || props.row.health.score == null) return 'unknown'
  if (props.row.health.overall === 'healthy') return 'healthy'
  if (props.row.health.overall === 'critical') return 'critical'
  return 'warning'
})
const timeline = computed<Array<MonitorMatrixBucket | undefined>>(() => {
  const buckets = [...(props.row.buckets || [])]
    .sort((a, b) => new Date(a.bucket_start).getTime() - new Date(b.bucket_start).getTime())
    .slice(-18)
  return buckets.length >= 18 ? buckets : [...new Array(18 - buckets.length), ...buckets]
})
const latestTime = computed(() => {
  const latest = props.row.buckets?.[props.row.buckets.length - 1]?.bucket_start
  return latest ? formatTime(latest) : t('channelMonitorV3.timeline.waiting')
})

function formatPercent(value: number | null | undefined): string {
  if (value == null || Number.isNaN(value)) return '-'
  return new Intl.NumberFormat(undefined, { minimumFractionDigits: value < 0.01 ? 2 : 1, maximumFractionDigits: value < 0.01 ? 2 : 1 }).format((value || 0) * 100) + '%'
}
function formatMs(value: number | null | undefined): string {
  if (value == null || Number.isNaN(value)) return '-'
  return value >= 1000 ? `${(value / 1000).toFixed(1)}s` : `${Math.round(value)}ms`
}
function formatRate(value: number): string {
  return Number(value).toFixed(2).replace(/0+$/, '').replace(/\.$/, '')
}
function formatTime(value: string): string {
  return new Date(value).toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' })
}
function valueClass(value: MonitorHealth['error_rate']): string {
  return value === 'critical' ? 'group-health-card__value--critical' : value === 'warning' ? 'group-health-card__value--warning' : 'group-health-card__value--healthy'
}
function pulseClass(bucket: MonitorMatrixBucket): string {
  const score = bucket.health.score
  if (score == null || bucket.metrics.request_count <= 0) return 'group-health-card__pulse--empty'
  return score < 40 ? 'group-health-card__pulse--critical' : score < 70 ? 'group-health-card__pulse--warning' : 'group-health-card__pulse--healthy'
}
function bucketTitle(bucket: MonitorMatrixBucket): string {
  return `${formatTime(bucket.bucket_start)} · ${formatPercent(1 - bucket.metrics.error_rate)} · ${formatMs(bucket.metrics.ttft.p50_ms)}`
}
</script>

<style scoped>
.group-health-card { --card-line: rgba(100, 116, 139, .16); min-width: 0; padding: 22px; border: 1px solid var(--card-line); border-radius: 24px; background: linear-gradient(145deg,#fffffffa,#f8fafceb); box-shadow: 0 10px 30px #0f172a0f; transition: transform .18s ease, box-shadow .18s ease, border-color .18s ease; }
.group-health-card:hover { transform: translateY(-2px); box-shadow: 0 16px 34px #0f172a1a; }
.dark .group-health-card { border-color: #94a3b829; background: linear-gradient(145deg,#1e293bf5,#0f172af5); }
.group-health-card__header, .group-health-card__identity, .group-health-card__timeline-head, .group-health-card__footer { display: flex; align-items: center; }
.group-health-card__header { justify-content: space-between; gap: 14px; }
.group-health-card__identity { min-width: 0; gap: 10px; }
.group-health-card__provider-icon { display: inline-grid; width: 36px; height: 36px; flex: 0 0 auto; place-items: center; border-radius: 12px; background: #d1fae5; color: #047857; }
.dark .group-health-card__provider-icon { background: #10b98129; color: #6ee7b7; }
.group-health-card__tag { display: inline-flex; align-items: center; padding: 3px 7px; border-radius: 6px; background: #dcfce7; color: #047857; font-size: 10px; font-weight: 700; }
.group-health-card__tag--rate { background: #ecfeff; color: #0f766e; }
.dark .group-health-card__tag { background: #10b98124; color: #a7f3d0; }
.dark .group-health-card__tag--rate { background: #14b8a624; color: #99f6e4; }
.group-health-card__status { display: inline-flex; flex: 0 0 auto; align-items: center; gap: 6px; padding: 5px 9px; border-radius: 999px; font-size: 11px; font-weight: 700; }
.group-health-card__status i { width: 7px; height: 7px; border-radius: 50%; background: currentColor; }
.group-health-card__status--healthy { color: #047857; background: #d1fae5; }
.group-health-card__status--warning { color: #b45309; background: #fef3c7; }
.group-health-card__status--critical { color: #b91c1c; background: #fee2e2; }
.group-health-card__status--unknown { color: #64748b; background: #e2e8f0; }
.dark .group-health-card__status--healthy { color: #6ee7b7; background: #10b98129; }
.dark .group-health-card__status--warning { color: #fcd34d; background: #f59e0b29; }
.dark .group-health-card__status--critical { color: #fca5a5; background: #ef444429; }
.dark .group-health-card__status--unknown { color: #cbd5e1; background: #64748b33; }
.group-health-card__metrics { display: grid; grid-template-columns: repeat(3,minmax(0,1fr)); gap: 9px; margin-top: 22px; }
.group-health-card__metric { min-width: 0; padding: 14px 12px 12px; border: 1px solid rgba(148,163,184,.18); border-radius: 16px; background: #f8fafcb3; }
.dark .group-health-card__metric { border-color: #94a3b826; background: #0f172a57; }
.group-health-card__metric span { display: block; color: #94a3b8; font-size: 10px; font-weight: 700; text-transform: uppercase; }
.group-health-card__metric strong { display: block; margin-top: 8px; color: #1e293b; font-size: 20px; line-height: 1; font-variant-numeric: tabular-nums; }
.dark .group-health-card__metric strong { color: #f8fafc; }
.group-health-card__value--healthy { color: #047857 !important; }
.group-health-card__value--warning { color: #d97706 !important; }
.group-health-card__value--critical { color: #dc2626 !important; }
.group-health-card__timeline-head { justify-content: space-between; gap: 8px; margin-top: 21px; color: #94a3b8; font-size: 11px; font-weight: 700; }
.group-health-card__timeline { display: grid; grid-template-columns: repeat(18,minmax(0,1fr)); gap: 3px; height: 18px; margin-top: 10px; }
.group-health-card__pulse { min-width: 0; border-radius: 4px; background: #cbd5e1; }
.group-health-card__pulse--healthy { background: #10b981; }
.group-health-card__pulse--warning { background: #f59e0b; }
.group-health-card__pulse--critical { background: #ef4444; }
.group-health-card__pulse--empty { background: #cbd5e1; opacity: .5; }
.dark .group-health-card__pulse--empty { background: #475569; }
.group-health-card__footer { justify-content: space-between; margin-top: 5px; color: #94a3b8; font-size: 9px; font-weight: 700; text-transform: uppercase; }
@media (max-width: 420px) { .group-health-card { padding: 18px; border-radius: 20px; } .group-health-card__metric strong { font-size: 17px; } }
</style>
