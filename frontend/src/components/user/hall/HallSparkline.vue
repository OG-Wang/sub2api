<template>
  <div class="flex flex-col gap-1">
    <svg
      v-if="hasData"
      :viewBox="`0 0 ${VIEW_WIDTH} ${VIEW_HEIGHT}`"
      class="h-8 w-28"
      preserveAspectRatio="none"
      role="img"
      :aria-label="ariaLabel"
    >
      <!-- 探测线（V1 主动探测） -->
      <polyline
        v-if="probePoints"
        :points="probePoints"
        fill="none"
        stroke="currentColor"
        stroke-width="1.5"
        class="text-primary-500"
        vector-effect="non-scaling-stroke"
      />
      <!-- 用户线（V2 被动统计），与探测线对比才能看出探测是否代表真实体验 -->
      <polyline
        v-if="userPoints"
        :points="userPoints"
        fill="none"
        stroke="currentColor"
        stroke-width="1.5"
        stroke-dasharray="3 2"
        class="text-emerald-500"
        vector-effect="non-scaling-stroke"
      />
    </svg>
    <span v-else class="text-xs text-gray-400 dark:text-dark-500">—</span>
  </div>
</template>

<script setup lang="ts">
/**
 * 大厅曲线：探测线（实线）+ 用户线（虚线）。
 *
 * 两条线各自独立归一化到同一画布高度——它们量纲相同（都是毫秒或都是 token），
 * 但基线可能差一个数量级，共用一套刻度会把其中一条压成直线。
 * 这里只用于看趋势，不看绝对值，绝对值在第 4 列。
 */
import { computed } from 'vue'
import type { HallRow } from '@/api/providerHall'
import { computeTps } from '@/api/providerHall'

/** 曲线可切换的指标。 */
export type HallSparklineMetric = 'ttft' | 'tps' | 'inputTokens'

const props = defineProps<{
  row: HallRow
  metric: HallSparklineMetric
}>()

const VIEW_WIDTH = 100
const VIEW_HEIGHT = 24

/** 从探测时间线取指标序列。timeline 是最新在前，绘图要按时间正序。 */
const probeSeries = computed<number[]>(() => {
  const points = [...props.row.timeline].reverse()
  const values: number[] = []
  for (const p of points) {
    const v = pickProbeMetric(p.ttft_ms, p.latency_ms, p.input_tokens, p.output_tokens)
    if (v != null) values.push(v)
  }
  return values
})

function pickProbeMetric(
  ttftMs: number | null,
  latencyMs: number | null,
  inputTokens: number | null,
  outputTokens: number | null,
): number | null {
  switch (props.metric) {
    case 'ttft':
      // TTFT 未接入时退回总耗时，保持曲线不空白。
      return ttftMs ?? latencyMs
    case 'tps':
      return computeTps(outputTokens, latencyMs, ttftMs)
    case 'inputTokens':
      return inputTokens
    default:
      return null
  }
}

/** 用户线只有 TTFT 一个维度可用；其余指标 V2 没有对应数据，留空。 */
const userSeries = computed<number[]>(() => {
  if (props.metric !== 'ttft') return []
  const buckets = props.row.passive?.buckets ?? []
  const values: number[] = []
  for (const b of buckets) {
    const avg = b.metrics.ttft.avg_ms
    if (avg != null) values.push(avg)
  }
  return values
})

const probePoints = computed(() => toPolyline(probeSeries.value))
const userPoints = computed(() => toPolyline(userSeries.value))
const hasData = computed(() => probePoints.value !== null || userPoints.value !== null)

/**
 * 序列归一化成 polyline 坐标。
 * 少于 2 个点画不出线，返回 null 由上层显示占位符。
 */
function toPolyline(series: number[]): string | null {
  if (series.length < 2) return null
  let min = series[0]
  let max = series[0]
  for (const v of series) {
    if (v < min) min = v
    if (v > max) max = v
  }
  const span = max - min
  const stepX = VIEW_WIDTH / (series.length - 1)
  return series
    .map((v, i) => {
      // 全平的序列（span=0）画在中线，避免除零。
      const ratio = span === 0 ? 0.5 : (v - min) / span
      const y = VIEW_HEIGHT - ratio * VIEW_HEIGHT
      return `${(i * stepX).toFixed(2)},${y.toFixed(2)}`
    })
    .join(' ')
}

const ariaLabel = computed(() => `${props.metric} trend for ${props.row.name}`)
</script>
