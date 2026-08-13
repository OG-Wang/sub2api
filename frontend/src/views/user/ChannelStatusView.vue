<template>
  <ProviderHallView v-if="isHybrid" />
  <ChannelStatusV1View v-else-if="isV1" />
  <ChannelStatusV2View v-else />
</template>

<script setup lang="ts">
/**
 * 「渠道状态」按监控模式三选一：
 *   V1        → 主动探测卡片
 *   V2        → 被动聚合矩阵
 *   V1 + V2   → 合并对比视图（原「供应商大厅」，两套数据都有时才有意义）
 *
 * hybrid 必须排在 V1 前面判断：isChannelMonitorV1Mode() 对 hybrid 也返回 true，
 * 顺序反了 hybrid 会落回 V1 界面。
 */
import { computed } from 'vue'
import { isChannelMonitorV1Mode, isProviderHallEnabled } from '@/utils/featureFlags'
import ChannelStatusV1View from './ChannelStatusV1View.vue'
import ChannelStatusV2View from './ChannelStatusV2View.vue'
import ProviderHallView from './ProviderHallView.vue'

const isHybrid = computed(() => isProviderHallEnabled())
const isV1 = computed(() => isChannelMonitorV1Mode())
</script>
