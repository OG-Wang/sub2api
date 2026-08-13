<template>
  <div class="flex flex-col gap-0.5">
    <!-- 主值：探测首 Token。TTFT 尚未接入时退回探测总耗时，并在 tooltip 里说明。 -->
    <div class="flex items-center gap-1.5">
      <span class="font-medium tabular-nums text-gray-900 dark:text-white">
        {{ primaryDisplay }}
      </span>
      <HelpTooltip v-if="detailLines.length" width-class="w-72">
        <div v-for="(line, i) in detailLines" :key="i" :class="i > 0 ? 'mt-1' : ''">
          {{ line }}
        </div>
      </HelpTooltip>
    </div>

    <!-- ↑输入 ↓输出 -->
    <div
      v-if="netInput != null || row.output_tokens != null"
      class="flex items-center gap-2 text-[11px] tabular-nums text-gray-500 dark:text-dark-400"
    >
      <!--
        输入 token 与真值对不上时直接标在数字上（多报=橙，少报=红），
        旁边一个「i」说明应该是多少——比单独挂一个「疑似注水」标签更直接：
        看到的就是那个对不上的数，不用再猜说的是哪一项。
      -->
      <span v-if="netInput != null" class="inline-flex items-center gap-[3px]" :class="deviationClass">
        ↑{{ netInput }}
        <HelpTooltip v-if="row.input_tokens_deviated" width-class="w-72">
          <template #trigger>
            <span
              class="inline-flex h-4 w-4 cursor-help items-center justify-center rounded-full text-[10px] font-extrabold leading-none text-white"
              :class="deviationIconClass"
              aria-hidden="true"
            >
              i
            </span>
          </template>
          <div v-for="(line, i) in expectedInputLines" :key="i" :class="i > 0 ? 'mt-1' : ''">
            {{ line }}
          </div>
        </HelpTooltip>
        <span v-if="row.input_tokens_deviated" class="sr-only">{{ expectedInputLines.join('；') }}</span>
      </span>
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
import { computeTps, netInputTokens, type HallRow } from '@/api/providerHall'
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

/** 净输入：扣掉缓存命中，与网关用量记录的 input 口径一致。 */
const netInput = computed(() =>
  netInputTokens(props.row.input_tokens, props.row.cached_input_tokens),
)

/**
 * 主值 tooltip 的各行。
 *
 * 逐行渲染而不是拼成一个字符串塞给 content——HelpTooltip 的容器没开
 * white-space 保留，`\n` 会被折成空格，几行说明会糊成一行。
 */
const detailLines = computed(() => {
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
  // ↑ 展示的是净输入（与用量记录口径一致）。
  // 这里补上总量，说明差额是缓存命中，省得再去猜。
  if (props.row.cached_input_tokens != null && props.row.input_tokens != null) {
    lines.push(
      `${t('providerHall.tooltip.inputBreakdown')}: ` +
        `${props.row.input_tokens} - ${props.row.cached_input_tokens} = ${netInput.value}`,
    )
  }
  return lines
})

/** 多报走警告色，少报走危险色——少报意味着 prompt 被截断或换掉了，性质更重。 */
const deviationClass = computed(() => {
  if (!props.row.input_tokens_deviated) return ''
  return props.row.input_tokens_excess < 0
    ? 'rounded bg-red-100 px-1 font-bold text-red-700 dark:bg-red-900/40 dark:text-red-300'
    : 'rounded bg-amber-100 px-1 font-bold text-amber-700 dark:bg-amber-900/40 dark:text-amber-300'
})

const deviationIconClass = computed(() =>
  props.row.input_tokens_excess < 0 ? 'bg-red-500' : 'bg-amber-500',
)

const expectedInputLines = computed(() => {
  const lines: string[] = []
  const expected = props.row.expected_input_tokens
  if (expected != null) {
    lines.push(t('providerHall.expectedInput', { expected }))
  }
  // 旁边 ↑ 显示的是净输入，判定用的却是含缓存的总输入。
  // 缓存不为 0 时两个数对不上，不写清楚这条注释看起来会自相矛盾。
  const total = props.row.input_tokens
  const cached = props.row.cached_input_tokens
  if (total != null && cached != null && cached > 0) {
    lines.push(t('providerHall.expectedInputTotal', { actual: total, cached }))
  }
  return lines
})
</script>
