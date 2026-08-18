<template>
  <button
    v-if="hasWindow"
    type="button"
    class="group flex w-32 flex-col gap-1 rounded-md p-1 text-left transition hover:bg-gray-100 focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500/40 dark:hover:bg-dark-800"
    :title="t('providerHall.band.openHint')"
    :aria-label="ariaLabel"
    @click.stop="emit('open')"
  >
    <!-- 固定段数的状态条：窗口越长每段越粗，段数不变，所以 90m / 24h / 7d 看起来一样清楚 -->
    <span class="flex h-4 w-full items-stretch gap-[2px]" aria-hidden="true">
      <span
        v-for="seg in segments"
        :key="seg.index"
        class="flex-1 rounded-[2px] transition-transform group-hover:scale-y-110"
        :class="segmentClass(seg.state)"
        :title="segmentTitle(seg)"
      />
    </span>

    <!-- 一行小字说清这段时间发生了什么，避免只靠颜色传达（色盲友好） -->
    <span class="flex items-center gap-1 text-[11px] leading-none" :class="summaryClass">
      {{ summaryText }}
    </span>
  </button>

  <span v-else class="text-xs text-gray-400 dark:text-dark-500">—</span>
</template>

<script setup lang="ts">
/**
 * 大厅表格「曲线」列的缩略图：状态条（statuspage 那种 uptime bar）。
 *
 * 为什么不在这一格里画精确曲线：格子只有约 112px 宽，而 7 天窗口按默认
 * 5 分钟间隔有 ~2016 个探测点——平均 18 个点抢 1 个像素，红点必然叠成一团，
 * 「全窗口不可用」和「偶发几次失败」画出来长得一模一样，反而误导。
 *
 * 所以这里只回答「这段时间大致怎么样、什么时候坏过」，
 * 精确到每一个点的曲线放进弹窗（HallTrendDialog），一个点都不省。
 *
 * 段数固定：窗口拉长时每段覆盖的时间变长，但**段数与视觉密度不变**，
 * 于是 90m / 24h / 7d 三档看起来一样清楚——这正是原本最难受的地方。
 */
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { HallRow, HallWindow } from '@/api/providerHall'

const props = defineProps<{
  row: HallRow
  /** 与曲线共用的时间窗口，取不到就显示占位符。 */
  window: HallWindow | null
}>()

const emit = defineEmits<{ (e: 'open'): void }>()

const { t } = useI18n()

/** 段数。24 段在 ~112px 宽里每段约 3.5px，既数得清也不会细成线。 */
const SEGMENT_COUNT = 24

type SegState = 'ok' | 'degraded' | 'failed' | 'empty'

interface Segment {
  index: number
  start: number
  end: number
  ok: number
  degraded: number
  failed: number
  state: SegState
}

const axis = computed<{ start: number; span: number } | null>(() => {
  const start = Date.parse(props.window?.start ?? '')
  const end = Date.parse(props.window?.end ?? '')
  if (Number.isNaN(start) || Number.isNaN(end) || end <= start) return null
  return { start, span: end - start }
})

const hasWindow = computed(() => axis.value != null)

/**
 * 把窗口切成等长的若干段，每段按段内**最坏**的一次探测定色。
 *
 * 取最坏而不是取平均：这页存在的意义是让人看见故障，
 * 一段里 10 次正常 1 次失败，平均下来还是绿的，那次失败就被抹掉了。
 */
const segments = computed<Segment[]>(() => {
  const a = axis.value
  if (!a) return []

  const out: Segment[] = Array.from({ length: SEGMENT_COUNT }, (_, index) => ({
    index,
    start: a.start + (a.span * index) / SEGMENT_COUNT,
    end: a.start + (a.span * (index + 1)) / SEGMENT_COUNT,
    ok: 0,
    degraded: 0,
    failed: 0,
    state: 'empty' as SegState,
  }))

  for (const point of props.row.timeline || []) {
    const at = Date.parse(point.checked_at)
    if (Number.isNaN(at)) continue
    const ratio = (at - a.start) / a.span
    if (ratio < 0 || ratio >= 1) continue
    const seg = out[Math.min(SEGMENT_COUNT - 1, Math.floor(ratio * SEGMENT_COUNT))]
    if (point.status === 'failed' || point.status === 'error') seg.failed += 1
    else if (point.status === 'degraded') seg.degraded += 1
    else seg.ok += 1
  }

  for (const seg of out) {
    if (seg.failed > 0) seg.state = 'failed'
    else if (seg.degraded > 0) seg.state = 'degraded'
    else if (seg.ok > 0) seg.state = 'ok'
    else seg.state = 'empty'
  }
  return out
})

const totals = computed(() => {
  let ok = 0
  let degraded = 0
  let failed = 0
  for (const seg of segments.value) {
    ok += seg.ok
    degraded += seg.degraded
    failed += seg.failed
  }
  return { ok, degraded, failed, probes: ok + degraded + failed }
})

function segmentClass(state: SegState): string {
  switch (state) {
    case 'ok':
      return 'bg-emerald-500'
    case 'degraded':
      return 'bg-amber-400'
    case 'failed':
      return 'bg-red-500'
    default:
      // 无数据要明显区别于「正常」，又不能抢眼到像故障。
      return 'bg-gray-200 dark:bg-dark-600'
  }
}

/** 概要行：优先说异常，其次说缓慢，都没有才说正常。 */
const summaryText = computed(() => {
  const { failed, degraded, probes } = totals.value
  if (probes === 0) return t('providerHall.band.noProbe')
  if (failed > 0) return t('providerHall.band.failures', { count: failed })
  if (degraded > 0) return t('providerHall.band.degraded', { count: degraded })
  return t('providerHall.band.allOk')
})

const summaryClass = computed(() => {
  const { failed, degraded, probes } = totals.value
  if (probes === 0) return 'text-gray-400 dark:text-dark-500'
  if (failed > 0) return 'text-red-600 dark:text-red-400'
  if (degraded > 0) return 'text-amber-600 dark:text-amber-400'
  return 'text-emerald-600 dark:text-emerald-400'
})

function formatClock(ms: number): string {
  const d = new Date(ms)
  return Number.isNaN(d.getTime())
    ? '—'
    : d.toLocaleString(undefined, {
        month: '2-digit',
        day: '2-digit',
        hour: '2-digit',
        minute: '2-digit',
      })
}

function segmentTitle(seg: Segment): string {
  const range = `${formatClock(seg.start)} - ${formatClock(seg.end)}`
  if (seg.state === 'empty') return `${range} · ${t('providerHall.band.noProbe')}`
  const parts: string[] = []
  if (seg.failed > 0) parts.push(t('providerHall.band.failures', { count: seg.failed }))
  if (seg.degraded > 0) parts.push(t('providerHall.band.degraded', { count: seg.degraded }))
  if (seg.ok > 0) parts.push(t('providerHall.band.okCount', { count: seg.ok }))
  return `${range} · ${parts.join(' / ')}`
}

const ariaLabel = computed(() => `${summaryText.value} — ${t('providerHall.band.openHint')}`)
</script>
