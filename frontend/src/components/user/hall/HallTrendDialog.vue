<template>
  <BaseDialog
    :show="show"
    :title="dialogTitle"
    width="full"
    @close="emit('close')"
  >
    <div v-if="row" class="space-y-4">
      <!-- 指标切换 + 密度（密度就是这里的"缩放"：每个探测点分到多少像素） -->
      <div class="flex flex-wrap items-center justify-between gap-3">
        <div class="flex items-center gap-2">
          <span class="text-xs text-gray-500 dark:text-dark-400">
            {{ t('providerHall.trendDialog.metricLabel') }}
          </span>
          <Select v-model="metricLocal" :options="metricOptions" class="w-36" />
        </div>

        <div class="flex items-center gap-2">
          <span class="text-xs text-gray-500 dark:text-dark-400">
            {{ t('providerHall.trendDialog.densityLabel') }}
          </span>
          <div class="inline-flex rounded-lg border border-gray-200 bg-gray-50 p-0.5 dark:border-dark-600 dark:bg-dark-900/40">
            <button
              v-for="level in DENSITY_LEVELS"
              :key="level.key"
              type="button"
              class="rounded-md px-3 py-1 text-xs font-medium transition"
              :class="
                density === level.key
                  ? 'bg-white text-primary-700 shadow-sm dark:bg-dark-800 dark:text-primary-300'
                  : 'text-gray-600 hover:text-gray-900 dark:text-dark-300 dark:hover:text-white'
              "
              @click="density = level.key"
            >
              {{ t(`providerHall.trendDialog.density.${level.key}`) }}
            </button>
          </div>
        </div>
      </div>

      <!-- 图例 -->
      <div class="flex flex-wrap items-center gap-4 text-xs text-gray-500 dark:text-dark-400">
        <span class="inline-flex items-center gap-1.5">
          <svg class="h-2 w-6" viewBox="0 0 24 8" aria-hidden="true">
            <line x1="0" y1="4" x2="24" y2="4" stroke="currentColor" stroke-width="2" class="text-primary-500" />
          </svg>
          {{ t('providerHall.trendDialog.legendProbe') }}
        </span>
        <span class="inline-flex items-center gap-1.5">
          <svg class="h-2 w-6" viewBox="0 0 24 8" aria-hidden="true">
            <line
              x1="0" y1="4" x2="24" y2="4"
              stroke="currentColor" stroke-width="2" stroke-dasharray="5 3"
              class="text-emerald-500"
            />
          </svg>
          {{ t('providerHall.trendDialog.legendUser') }}
        </span>
        <span class="inline-flex items-center gap-1.5">
          <span class="h-2 w-2 rounded-full bg-red-500"></span>
          {{ t('providerHall.trendDialog.legendError') }}
        </span>
      </div>

      <!--
        横向滚动容器：点数再多也不压缩，宽度不够就滚动。
        这是"每个点都要看得见"与"格子只有 112px"之间唯一诚实的解法。
      -->
      <div
        class="overflow-x-auto overflow-y-hidden rounded-xl border border-gray-200 bg-gray-50/60 p-3 dark:border-dark-700 dark:bg-dark-900/30"
      >
        <div :style="{ width: `${canvasWidth}px`, minWidth: '100%' }">
          <HallSparkline
            :row="row"
            :metric="metricLocal"
            :window="window"
            :width="canvasWidth"
            :height="CANVAS_HEIGHT"
            :point-radius="3"
            svg-class="h-64 w-full"
          />
        </div>
      </div>

      <!-- 说清楚画了多少点：这页承诺的是"一个不漏" -->
      <div class="flex flex-wrap items-center justify-between gap-2 text-xs text-gray-500 dark:text-dark-400">
        <span>{{ t('providerHall.trendDialog.pointsNote', { count: probeCount }) }}</span>
        <span v-if="scrollable">{{ t('providerHall.trendDialog.scrollHint') }}</span>
      </div>
    </div>

    <div v-else class="py-10 text-center text-sm text-gray-500 dark:text-dark-400">
      {{ t('providerHall.trendDialog.empty') }}
    </div>
  </BaseDialog>
</template>

<script setup lang="ts">
/**
 * 「曲线」列的放大视图。
 *
 * 表格格子里放不下精确曲线（7 天窗口 ~2016 个探测点抢 112px），
 * 但用户要的恰恰是**每个点都画出来**，不聚合、不降采样。
 * 唯一诚实的解法就是给它足够的横向像素，不够就滚动——这就是本弹窗。
 *
 * 曲线本体直接复用 HallSparkline（同一套时间轴与命中逻辑），
 * 只是把画布尺寸调大：viewBox 宽度跟着渲染像素一起放大，
 * 否则 preserveAspectRatio="none" 会把红点抻成横向椭圆。
 */
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { HallRow, HallWindow } from '@/api/providerHall'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select from '@/components/common/Select.vue'
import HallSparkline, { type HallSparklineMetric } from '@/components/user/hall/HallSparkline.vue'

const props = defineProps<{
  show: boolean
  row: HallRow | null
  metric: HallSparklineMetric
  window: HallWindow | null
  /** 当前时间窗口档位，用于标题上标明看的是哪一段。 */
  rangeLabel: string
}>()

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'update:metric', value: HallSparklineMetric): void
}>()

const { t } = useI18n()

const CANVAS_HEIGHT = 256
/** 画布宽度上限：再宽浏览器渲染会开始吃力，且已远超"看清每个点"所需。 */
const MAX_CANVAS_WIDTH = 24000
const MIN_CANVAS_WIDTH = 640

/** 每个探测点分到多少像素。standard 下 7 天 ~2016 点 ≈ 12000px，滚动可达。 */
const DENSITY_LEVELS = [
  { key: 'compact' as const, px: 2 },
  { key: 'standard' as const, px: 6 },
  { key: 'wide' as const, px: 16 },
]
type DensityKey = (typeof DENSITY_LEVELS)[number]['key']

const density = ref<DensityKey>('standard')

/** 本地副本：弹窗里换指标不该影响表格列，关掉再开也不该被重置成别的。 */
const metricLocal = ref<HallSparklineMetric>(props.metric)

watch(
  () => props.metric,
  (value) => {
    metricLocal.value = value
  },
)

watch(metricLocal, (value) => {
  emit('update:metric', value)
})

const metricOptions = computed(() => [
  { value: 'ttft', label: t('providerHall.metrics.ttft') },
  { value: 'tps', label: t('providerHall.metrics.tps') },
  { value: 'inputTokens', label: t('providerHall.metrics.inputTokens') },
])

const probeCount = computed(() => props.row?.timeline?.length ?? 0)
const userCount = computed(() => props.row?.passive?.buckets?.length ?? 0)

const pxPerPoint = computed(
  () => DENSITY_LEVELS.find((l) => l.key === density.value)?.px ?? 6,
)

const canvasWidth = computed(() => {
  const points = Math.max(probeCount.value, userCount.value)
  const wanted = points * pxPerPoint.value
  return Math.round(Math.min(MAX_CANVAS_WIDTH, Math.max(MIN_CANVAS_WIDTH, wanted)))
})

/** 宽度超过 MIN 就说明大概率要横向滚动，提示一下滚动条的存在。 */
const scrollable = computed(() => canvasWidth.value > MIN_CANVAS_WIDTH)

const dialogTitle = computed(() => {
  const name = props.row?.group_name || props.row?.name || ''
  return t('providerHall.trendDialog.title', { group: name, range: props.rangeLabel })
})
</script>
