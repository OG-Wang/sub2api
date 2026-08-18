<template>
  <div class="flex flex-col gap-1">
    <svg
      v-if="hasData"
      :viewBox="`0 0 ${VIEW_WIDTH} ${VIEW_HEIGHT}`"
      class="h-8 w-28 cursor-crosshair"
      preserveAspectRatio="none"
      role="img"
      :aria-label="ariaLabel"
      @mousemove="onMove"
      @mouseleave="onLeave"
      @click.stop="onClick"
    >
      <!-- 探测线（V1 主动探测）。按数据空洞拆成多段，见 toSegments -->
      <polyline
        v-for="(seg, idx) in probeSegments"
        :key="`probe-${idx}`"
        :points="seg"
        fill="none"
        stroke="currentColor"
        stroke-width="1.5"
        class="text-primary-500"
        vector-effect="non-scaling-stroke"
      />
      <!-- 用户线（V2 被动统计），与探测线对比才能看出探测是否代表真实体验 -->
      <polyline
        v-for="(seg, idx) in userSegments"
        :key="`user-${idx}`"
        :points="seg"
        fill="none"
        stroke="currentColor"
        stroke-width="1.5"
        stroke-dasharray="3 2"
        class="text-emerald-500"
        vector-effect="non-scaling-stroke"
      />
      <!--
        透明命中层：SVG 只在图元上触发鼠标事件，线之间的空白处不会触发。
        没有它就只有正好压在线上才出 tooltip，这条线只有 1.5 宽，等于点不中。
      -->
      <rect :width="VIEW_WIDTH" :height="VIEW_HEIGHT" fill="transparent" />
      <!-- 异常点标记：探测到 degraded/failed/error 时显示红点 -->
      <circle
        v-for="(point, idx) in errorPoints"
        :key="`error-${idx}`"
        :cx="point.x"
        :cy="point.y"
        r="2"
        fill="#ef4444"
        class="error-marker"
        vector-effect="non-scaling-stroke"
      />
      <!-- 命中点高亮 -->
      <circle
        v-if="active"
        :cx="active.point.x"
        :cy="active.point.y"
        r="2.2"
        fill="currentColor"
        stroke="white"
        stroke-width="1"
        :class="active.point.series === 'user' ? 'text-emerald-500' : 'text-primary-500'"
        vector-effect="non-scaling-stroke"
      />
    </svg>
    <span v-else class="text-xs text-gray-400 dark:text-dark-500">—</span>

    <!-- 传送到 body：表格是横向滚动容器，留在原地会被 overflow 裁掉 -->
    <Teleport to="body">
      <div
        v-if="active"
        role="tooltip"
        class="pointer-events-none fixed z-[99999] rounded-lg bg-gray-900 px-3 py-2 text-xs leading-[18px] text-white shadow-xl ring-1 ring-white/10 dark:bg-gray-800"
        :style="tooltipStyle"
      >
        <div class="text-gray-300 dark:text-dark-300">{{ activeLines.time }}</div>
        <div class="mt-0.5 font-medium tabular-nums">{{ activeLines.detail }}</div>
        <div v-if="activeLines.note" class="mt-0.5 text-gray-300 dark:text-dark-300">
          {{ activeLines.note }}
        </div>
      </div>
    </Teleport>
  </div>
</template>

<script setup lang="ts">
/**
 * 大厅曲线：探测线（实线）+ 用户线（虚线），鼠标悬停显示该点的时间与取值。
 *
 * 两条线各自独立归一化到同一画布高度——它们量纲相同（都是毫秒或都是 token），
 * 但基线可能差一个数量级，共用一套刻度会把其中一条压成直线。
 * 曲线本身只用于看趋势，绝对值靠 tooltip 与第 4 列读。
 *
 * 横轴则**必须**共用：x 由点的时间戳在 window [start, end) 中的相对位置算出，
 * 而不是「第 i 个点 / 总点数」。后者在两条线点数不同时会让同一个 x
 * 对应不同时刻，「探测 vs 真实用户」的对比就成了假的。
 *
 * 命中判定在屏幕坐标里做而不是 viewBox 坐标：viewBox 是 100x24 但渲染成
 * 112x32（preserveAspectRatio="none"），两轴缩放比不同，
 * 直接用 viewBox 距离会让纵向偏差被低估，指哪儿命中哪儿就不准。
 */
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { HallRow, HallWindow } from '@/api/providerHall'
import { computeTps, netInputTokens } from '@/api/providerHall'

/** 曲线可切换的指标。 */
export type HallSparklineMetric = 'ttft' | 'tps' | 'inputTokens'

const props = defineProps<{
  row: HallRow
  metric: HallSparklineMetric
  /** 横轴窗口，与 row.passive 的桶同源。取不到就整格显示占位符，不画错轴的线。 */
  window: HallWindow | null
}>()

const { t } = useI18n()

const VIEW_WIDTH = 100
const VIEW_HEIGHT = 24

/** tooltip 固定宽度，用于水平夹取，省掉「渲染完再量一次」的抖动。 */
const TOOLTIP_WIDTH = 224
/** 估算高度（3 行 + 内边距），只用来决定往上还是往下弹。 */
const TOOLTIP_EST_HEIGHT = 74
const VIEWPORT_MARGIN = 16
const ANCHOR_GAP = 8

type SeriesKind = 'probe' | 'user'

/** 曲线上的一个点。带上原始时间与状态，tooltip 才能说清「这是哪一刻的哪个数」。 */
interface ChartPoint {
  series: SeriesKind
  /** 探测线是该次探测的时刻，用户线是该桶的起点（ISO 时间戳）。 */
  at: string
  value: number
  /** 探测状态；用户线为空。 */
  status: string
  /** 用户线该桶聚合了几条请求；探测线一点就是一次探测，为 null。 */
  samples?: number | null
  /** 该点画的是探测总耗时而非真正的首 Token（TTFT 尚未采集到）。 */
  ttftFallback: boolean
  x: number
  y: number
}

interface ActiveState {
  point: ChartPoint
  /** 命中点在视口中的横坐标。 */
  anchorX: number
  /** 图表在视口中的上下边界，用于决定 tooltip 弹在哪一侧。 */
  chartTop: number
  chartBottom: number
}

/** 未归一化的原始点。 */
interface RawPoint {
  at: string
  value: number
  status: string
  samples?: number | null
  ttftFallback: boolean
}

/**
 * 从探测时间线取指标序列。
 * 后端已按 checked_at 升序返回窗口内的**每一次**探测，不做分桶。
 */
const probePoints = computed<ChartPoint[]>(() => {
  const raw: RawPoint[] = []
  let lastValidValue: number | null = null

  for (const p of props.row.timeline) {
    const value = pickProbeMetric(
      p.ttft_ms,
      p.latency_ms,
      netInputTokens(p.input_tokens, p.cached_input_tokens),
      p.output_tokens,
    )

    // 这次探测没采到该指标时（多为失败），用上一个有效值占位，让曲线保持连续
    const displayValue = value ?? lastValidValue ?? 0

    raw.push({
      at: p.checked_at,
      value: displayValue,
      status: p.status,
      ttftFallback: props.metric === 'ttft' && p.ttft_ms == null,
    })

    if (value !== null) lastValidValue = value
  }
  return layout(raw, 'probe')
})

function pickProbeMetric(
  ttftMs: number | null,
  latencyMs: number | null,
  netInput: number | null,
  outputTokens: number | null,
): number | null {
  switch (props.metric) {
    case 'ttft':
      // TTFT 未接入时退回总耗时，保持曲线不空白。
      return ttftMs ?? latencyMs
    case 'tps':
      return computeTps(outputTokens, latencyMs, ttftMs)
    case 'inputTokens':
      // 与第 4 列口径一致：净输入（扣掉缓存命中）。
      return netInput
    default:
      return null
  }
}

/** 用户线只有 TTFT 一个维度可用；其余指标 V2 没有对应数据，留空。 */
const userPoints = computed<ChartPoint[]>(() => {
  if (props.metric !== 'ttft') return []
  const raw: RawPoint[] = []
  for (const b of props.row.passive?.buckets ?? []) {
    const avg = b.metrics.ttft.avg_ms
    if (avg == null) continue
    raw.push({
      at: b.bucket_start,
      value: avg,
      status: '',
      samples: b.metrics.request_count,
      ttftFallback: false,
    })
  }
  return layout(raw, 'user')
})

/**
 * 横轴：把窗口 [start, end) 线性映射到画布宽度。
 *
 * 算不出窗口就返回 null，layout() 随之交出空序列、整格显示占位符。
 * 刻意不退回「按点数均分」：那会画出一条看着正常、但横轴是错的曲线——
 * 两条线点数不同时同一个 x 不是同一时刻，而这正是本组件要解决的问题。
 * 宁可什么都不画，也不画一条骗人的线。
 */
const timeAxis = computed<{ start: number; span: number; bucketMs: number } | null>(() => {
  const start = Date.parse(props.window?.start ?? '')
  const end = Date.parse(props.window?.end ?? '')
  const bucketSeconds = props.window?.bucket_seconds ?? 0
  if (Number.isNaN(start) || Number.isNaN(end) || end <= start || bucketSeconds <= 0) return null
  return { start, span: end - start, bucketMs: bucketSeconds * 1000 }
})

/**
 * 序列归一化成画布坐标：y 按本序列自身的极值归一，x 按绝对时间。
 * 少于 2 个点画不出线，返回空数组由上层显示占位符。
 */
function layout(raw: RawPoint[], series: SeriesKind): ChartPoint[] {
  const axis = timeAxis.value
  if (axis == null || raw.length < 2) return []
  let min = raw[0].value
  let max = raw[0].value
  for (const p of raw) {
    if (p.value < min) min = p.value
    if (p.value > max) max = p.value
  }
  const span = max - min

  // 留出上下边距，避免曲线端点贴边
  const PADDING_RATIO = 0.1
  const plotHeight = VIEW_HEIGHT * (1 - 2 * PADDING_RATIO)
  const offsetY = VIEW_HEIGHT * PADDING_RATIO

  return raw.map((p) => {
    // 全平的序列（span=0）画在中线，避免除零。
    const ratio = span === 0 ? 0.5 : (p.value - min) / span
    return {
      ...p,
      series,
      x: xOnAxis(axis, p.at),
      y: VIEW_HEIGHT - offsetY - ratio * plotHeight,
    }
  })
}

/** 时间戳 → 横坐标。 */
function xOnAxis(axis: { start: number; span: number }, at: string): number {
  const t = Date.parse(at)
  if (Number.isNaN(t)) return 0
  const ratio = (t - axis.start) / axis.span
  return Math.min(VIEW_WIDTH, Math.max(0, ratio * VIEW_WIDTH))
}

/**
 * 把一条序列按数据空洞拆成多段 polyline。
 *
 * 缺的时间段后端不会返回。若照旧把相邻点首尾相连，一段「监控停了三天」
 * 会被画成一条平稳跨越三天的直线——看不出这里没有数据，反而像是稳定。
 *
 * gapMs 由调用方按各自的采样节奏给：超过它就认为中间那段真的没采到。
 */
function toSegments(points: ChartPoint[], gapMs: number): string[] {
  if (points.length < 2 || gapMs <= 0) return []

  const segments: string[] = []
  let current: ChartPoint[] = []
  const flush = () => {
    if (current.length >= 2) {
      segments.push(current.map((p) => `${p.x.toFixed(2)},${p.y.toFixed(2)}`).join(' '))
    }
    current = []
  }

  for (const p of points) {
    const prev = current[current.length - 1]
    if (prev && Date.parse(p.at) - Date.parse(prev.at) > gapMs) flush()
    current.push(p)
  }
  flush()
  return segments
}

/**
 * 探测线的断点阈值：2.5 倍探测间隔。
 *
 * 用 2.5 而不是 1.5，是为了容忍调度抖动（monitor 可以配 jitter）加偶尔漏一拍——
 * 这两种情况连起来是对的。真正的停摆动辄几十上百拍，照样会被断开。
 * 拿不到间隔时退回窗口的十分之一，只求别把整条线连成一根。
 */
const probeGapMs = computed(() => {
  const interval = props.row.interval_seconds
  if (interval > 0) return interval * 2500
  const axis = timeAxis.value
  return axis ? axis.span / 10 : 0
})

const probeSegments = computed(() => toSegments(probePoints.value, probeGapMs.value))
/** 用户线是 V2 按桶聚合的，相邻桶的间隔就是桶长，超过 1.5 倍即缺桶。 */
const userSegments = computed(() =>
  toSegments(userPoints.value, (timeAxis.value?.bucketMs ?? 0) * 1.5),
)
/**
 * 有没有东西可画。
 *
 * layout() 已经把「不足 2 个点」的序列清空了（画不出趋势），
 * 所以这里非空就等于至少有一条线。但不能改用 segments 判断：
 * 一条全是空洞、每段都不足 2 点的序列 segments 为空，
 * 此时仍有异常红点要画。
 */
const hasData = computed(() => probePoints.value.length > 0 || userPoints.value.length > 0)

/**
 * 异常点：桶内出现过 failed/error 的时间桶，在探测线上标红。
 * degraded（缓慢）不算异常，不标。
 *
 * 直接从 probePoints 里筛，坐标天然与线一致：切换指标时红点跟着线上下走，
 * 不会飘在空处，也不会因为时间轴换算方式不同而横向错位。
 */
const errorPoints = computed<ChartPoint[]>(() =>
  probePoints.value.filter((p) => p.status === 'failed' || p.status === 'error'),
)

// ---------------------------------------------------------------- 悬停与命中

const hovered = ref<ActiveState | null>(null)
/** 点击钉住：触屏没有 hover，不钉住就永远看不到数值。 */
const pinned = ref<ActiveState | null>(null)
const active = computed(() => pinned.value ?? hovered.value)

function onMove(event: MouseEvent) {
  hovered.value = locate(event)
}

function onLeave() {
  hovered.value = null
}

function onClick(event: MouseEvent) {
  const next = locate(event)
  // 再点同一个点＝取消钉住。
  pinned.value = pinned.value && next && pinned.value.point.at === next.point.at ? null : next
}

/** 找出离鼠标最近的点，并算出 tooltip 的锚点。 */
function locate(event: MouseEvent): ActiveState | null {
  const all = [...probePoints.value, ...userPoints.value]
  if (all.length === 0) return null

  const rect = (event.currentTarget as SVGSVGElement).getBoundingClientRect()
  if (rect.width <= 0 || rect.height <= 0) return null

  const scaleX = rect.width / VIEW_WIDTH
  const scaleY = rect.height / VIEW_HEIGHT
  const localX = event.clientX - rect.left
  const localY = event.clientY - rect.top

  let best = all[0]
  let bestDistance = Number.POSITIVE_INFINITY
  for (const p of all) {
    const distance = Math.hypot(p.x * scaleX - localX, p.y * scaleY - localY)
    if (distance < bestDistance) {
      bestDistance = distance
      best = p
    }
  }

  return {
    point: best,
    anchorX: rect.left + best.x * scaleX,
    chartTop: rect.top,
    chartBottom: rect.bottom,
  }
}

// 页面一滚动，已算好的视口坐标就失效了；与其重算不如直接收起来。
watch(active, (value) => {
  if (value) attachDismissListeners()
  else detachDismissListeners()
})

onBeforeUnmount(detachDismissListeners)

let dismissAttached = false

function attachDismissListeners() {
  if (dismissAttached) return
  dismissAttached = true
  window.addEventListener('scroll', dismiss, true)
  window.addEventListener('resize', dismiss)
  document.addEventListener('keydown', onKeydown)
  document.addEventListener('click', onDocumentClick, true)
}

function detachDismissListeners() {
  if (!dismissAttached) return
  dismissAttached = false
  window.removeEventListener('scroll', dismiss, true)
  window.removeEventListener('resize', dismiss)
  document.removeEventListener('keydown', onKeydown)
  document.removeEventListener('click', onDocumentClick, true)
}

function dismiss() {
  hovered.value = null
  pinned.value = null
}

function onKeydown(event: KeyboardEvent) {
  if (event.key === 'Escape') dismiss()
}

/** 点到图表外面就取消钉住（图表自己的点击已 stop propagation，不会走到这里）。 */
function onDocumentClick() {
  pinned.value = null
}

// ------------------------------------------------------------------ tooltip

const tooltipStyle = computed(() => {
  const state = active.value
  if (!state) return {}

  const half = TOOLTIP_WIDTH / 2
  const minLeft = VIEWPORT_MARGIN + half
  const maxLeft = window.innerWidth - VIEWPORT_MARGIN - half
  // 视口比 tooltip 还窄时上面两个界会翻过来，取中间兜底。
  const left =
    maxLeft < minLeft
      ? window.innerWidth / 2
      : Math.min(Math.max(state.anchorX, minLeft), maxLeft)

  // 默认弹在图表上方；上方装不下就翻到下方。
  const fitsAbove = state.chartTop - ANCHOR_GAP - TOOLTIP_EST_HEIGHT >= VIEWPORT_MARGIN
  return {
    width: `${TOOLTIP_WIDTH}px`,
    left: `${Math.round(left)}px`,
    top: `${Math.round(fitsAbove ? state.chartTop - ANCHOR_GAP : state.chartBottom + ANCHOR_GAP)}px`,
    transform: fitsAbove ? 'translate(-50%, -100%)' : 'translate(-50%, 0)',
  }
})

/** tooltip 的三行：时间 / 指标名 + 值 / 补充说明。 */
const activeLines = computed(() => {
  const point = active.value?.point
  if (!point) return { time: '', detail: '', note: '' }
  return {
    time: formatTime(point.at),
    detail: `${metricLabel(point)} ${formatValue(point)}`,
    note: noteFor(point),
  }
})

function formatTime(at: string): string {
  const date = new Date(at)
  return Number.isNaN(date.getTime()) ? '—' : date.toLocaleString()
}

function metricLabel(point: ChartPoint): string {
  if (point.series === 'user') return t('providerHall.tooltip.userAvgTtft')
  switch (props.metric) {
    case 'ttft':
      // 画的是总耗时就别叫首 Token，否则两个口径混在一起没法对账。
      return point.ttftFallback
        ? t('providerHall.tooltip.probeTotal')
        : t('providerHall.tooltip.probeTtft')
    case 'tps':
      return t('providerHall.tooltip.tps')
    case 'inputTokens':
      return t('providerHall.metrics.inputTokens')
    default:
      return ''
  }
}

function formatValue(point: ChartPoint): string {
  if (point.series === 'user') return `${Math.round(point.value)} ms`
  switch (props.metric) {
    case 'ttft':
      return `${Math.round(point.value)} ms`
    case 'tps':
      return point.value.toFixed(2)
    case 'inputTokens':
      return String(Math.round(point.value))
    default:
      return ''
  }
}

/**
 * 补充说明行：用户线补样本数（几条请求平均出来的），探测线补非正常状态。
 * 探测线一个点就是一次探测，没有「几次平均」可说。
 */
function noteFor(point: ChartPoint): string {
  if (point.series === 'user') {
    return point.samples == null ? '' : t('providerHall.chart.samples', { count: point.samples })
  }
  if (!point.status || point.status === 'operational') return ''
  return t(`providerHall.status.${point.status}`, point.status)
}

const ariaLabel = computed(() => `${props.metric} trend for ${props.row.name}`)
</script>
