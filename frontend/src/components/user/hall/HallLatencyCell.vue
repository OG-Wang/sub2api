<template>
  <div class="flex flex-col gap-0.5">
    <!-- 主值：探测首 Token。TTFT 尚未接入时退回探测总耗时，并在 tooltip 里说明。 -->
    <div class="flex items-center gap-1.5">
      <span class="font-medium tabular-nums text-gray-900 dark:text-white">
        {{ primaryDisplay }}
      </span>
      <HelpTooltip v-if="hasAnyDetail" :content="tooltipText" />
      <span
        v-if="row.input_tokens_inflated"
        class="inline-flex items-center rounded px-1 py-0.5 text-[10px] font-medium text-amber-700 ring-1 ring-amber-300 dark:text-amber-300 dark:ring-amber-600"
        :title="inflationTitle"
      >
        {{ t('providerHall.inflated') }}
      </span>
    </div>

    <!-- ↑输入 ↓输出 -->
    <div
      v-if="row.input_tokens != null || row.output_tokens != null"
      class="flex items-center gap-2 text-[11px] tabular-nums text-gray-500 dark:text-dark-400"
    >
      <span v-if="row.input_tokens != null">↑{{ row.input_tokens }}</span>
      <span v-if="row.output_tokens != null">↓{{ row.output_tokens }}</span>
    </div>
  </div>
</template>

<script setup lang="ts">
/**
 * 大厅表格「最新首Token」单元格。
 *
 * 这一格同时承载三个来源的数据，tooltip 里必须标清楚哪个是探测、
 * 哪个是真实用户流量，否则两个数字对不上时无从判断。
 */
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { computeTps, type HallRow } from '@/api/providerHall'
import HelpTooltip from '@/components/common/HelpTooltip.vue'

const props = defineProps<{ row: HallRow }>()

const { t } = useI18n()

/** 探测首 Token；P3 接入流式前为 null，此时退回探测总耗时。 */
const probeTtft = computed(() => props.row.primary_ttft_ms)
const probeLatency = computed(() => props.row.primary_latency_ms)

const primaryDisplay = computed(() => {
  const value = probeTtft.value ?? probeLatency.value
  return value == null ? '—' : `${value} ms`
})

/** 用户平均首 Token，来自 V2 被动统计。 */
const userAvgTtft = computed(() => {
  const avg = props.row.passive?.metrics.ttft.avg_ms
  return avg == null ? null : Math.round(avg)
})

const tps = computed(() =>
  computeTps(props.row.output_tokens, probeLatency.value, probeTtft.value),
)

const hasAnyDetail = computed(
  () => probeLatency.value != null || userAvgTtft.value != null || tps.value != null,
)

const tooltipText = computed(() => {
  const lines: string[] = []
  if (probeTtft.value != null) {
    lines.push(`${t('providerHall.tooltip.probeTtft')}: ${probeTtft.value} ms`)
  }
  if (probeLatency.value != null) {
    lines.push(`${t('providerHall.tooltip.probeTotal')}: ${probeLatency.value} ms`)
  }
  if (probeTtft.value == null) {
    // 说清主值现在是什么，避免误读成真实 TTFT。
    lines.push(t('providerHall.tooltip.ttftUnavailable'))
  }
  if (userAvgTtft.value != null) {
    lines.push(`${t('providerHall.tooltip.userAvgTtft')}: ${userAvgTtft.value} ms`)
  }
  if (tps.value != null) {
    lines.push(`${t('providerHall.tooltip.tps')}: ${tps.value.toFixed(1)}`)
  }
  return lines.join('\n')
})

const inflationTitle = computed(() => {
  const expected = props.row.expected_input_tokens
  const actual = props.row.input_tokens
  if (expected == null || actual == null) return t('providerHall.inflatedHint')
  return t('providerHall.inflatedDetail', { expected, actual })
})
</script>
