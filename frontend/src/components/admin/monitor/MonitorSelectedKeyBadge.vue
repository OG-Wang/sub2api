<template>
  <div v-if="selectedKey" class="mt-2 flex items-center gap-2" data-testid="monitor-selected-key">
    <span class="text-xs text-gray-500 dark:text-gray-400">
      {{ t('admin.channelMonitor.form.selectedKey') }}
    </span>
    <GroupBadge
      v-if="selectedKey.group"
      :name="selectedKey.group.name"
      :platform="selectedKey.group.platform"
      :subscription-type="selectedKey.group.subscription_type"
      :rate-multiplier="selectedKey.group.rate_multiplier"
      :user-rate-multiplier="userGroupRates[selectedKey.group.id]"
    />
    <span v-else class="text-xs text-gray-400">—</span>
  </div>
</template>

<script setup lang="ts">
/**
 * 「使用我的 Key」选中后的分组标识。
 *
 * 只显示分组徽章，不显示 Key 名称与 Key 值：管理员一个分组固定放一把 Key，
 * 分组本身就是唯一有意义的身份，同组内选哪把都能完成监控任务。
 *
 * 只在「本次表单里选过 Key」时出现——此时明文 Key 就在内存里，判定是实测的。
 * 打开已保存的监控时后端只回一个 4 位掩码（sk-a***），不足以反查是哪把 Key，
 * 这里宁可什么都不显示，也不给一个可能骗人的标记。
 *
 * 单独成组件（对齐 MonitorProviderHallConfig 的做法），
 * 让 MonitorFormDialog 只需加一行调用，跟上游 rebase 时冲突面最小。
 */
import { useI18n } from 'vue-i18n'
import type { ApiKey } from '@/types'
import GroupBadge from '@/components/common/GroupBadge.vue'

withDefaults(
  defineProps<{
    selectedKey: ApiKey | null
    userGroupRates?: Record<number, number>
  }>(),
  {
    userGroupRates: () => ({}),
  },
)

const { t } = useI18n()
</script>
