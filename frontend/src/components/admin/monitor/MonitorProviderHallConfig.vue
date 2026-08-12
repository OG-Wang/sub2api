<template>
  <details class="rounded-lg border border-gray-200 bg-gray-50/50 p-3 dark:border-dark-700 dark:bg-dark-900/30">
    <summary class="cursor-pointer text-sm font-medium text-gray-700 dark:text-gray-300">
      {{ t('admin.channelMonitor.providerHall.section') }}
    </summary>
    <p class="mt-1 text-xs text-gray-400">{{ t('admin.channelMonitor.providerHall.sectionHint') }}</p>

    <div class="mt-4 space-y-4">
      <!-- 在大厅展示 -->
      <div class="flex items-start justify-between gap-4">
        <div class="min-w-0">
          <p class="text-sm font-medium text-gray-900 dark:text-white">
            {{ t('admin.channelMonitor.providerHall.publicVisible') }}
          </p>
          <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
            {{ t('admin.channelMonitor.providerHall.publicVisibleHint') }}
          </p>
        </div>
        <Toggle
          :model-value="publicVisible"
          @update:model-value="emit('update:publicVisible', $event)"
        />
      </div>

      <!-- 关联分组 -->
      <div>
        <label class="input-label">{{ t('admin.channelMonitor.providerHall.group') }}</label>
        <Select
          :model-value="groupSelectValue"
          :options="groupOptions"
          searchable
          clearable
          :disabled="groupsLoading"
          :placeholder="
            groupsLoading
              ? t('common.loading')
              : t('admin.channelMonitor.providerHall.groupPlaceholder')
          "
          @update:model-value="handleGroupChange"
        />
        <p class="mt-1 text-xs text-gray-400">
          {{ t('admin.channelMonitor.providerHall.groupHint') }}
        </p>
        <div v-if="selectedGroup" class="mt-2">
          <GroupBadge
            :name="selectedGroup.name"
            :platform="selectedGroup.platform"
            :subscription-type="selectedGroup.subscription_type"
            :rate-multiplier="selectedGroup.rate_multiplier"
          />
        </div>
        <p
          v-else-if="groupId != null"
          class="mt-2 text-xs text-amber-600 dark:text-amber-400"
        >
          {{ t('admin.channelMonitor.providerHall.groupMissing', { id: groupId }) }}
        </p>
      </div>

      <!-- 分组备注 -->
      <div>
        <label class="input-label">{{ t('admin.channelMonitor.providerHall.publicNote') }}</label>
        <input
          :value="publicNote"
          type="text"
          maxlength="200"
          class="input"
          :placeholder="t('admin.channelMonitor.providerHall.publicNotePlaceholder')"
          @input="emit('update:publicNote', ($event.target as HTMLInputElement).value)"
        />
        <p class="mt-1 text-xs text-gray-400">
          {{ t('admin.channelMonitor.providerHall.publicNoteHint') }}
        </p>
      </div>

      <!-- 检测报告链接 -->
      <div>
        <label class="input-label">{{ t('admin.channelMonitor.providerHall.reportUrl') }}</label>
        <input
          :value="reportUrl"
          type="url"
          maxlength="500"
          class="input"
          :class="reportUrlInvalid && 'border-red-400 dark:border-red-500'"
          placeholder="https://example.com/report/123"
          @input="emit('update:reportUrl', ($event.target as HTMLInputElement).value)"
        />
        <p v-if="reportUrlInvalid" class="mt-1 text-xs text-red-500">
          {{ t('admin.channelMonitor.providerHall.reportUrlInvalid') }}
        </p>
        <p v-else class="mt-1 text-xs text-gray-400">
          {{ t('admin.channelMonitor.providerHall.reportUrlHint') }}
        </p>
      </div>

      <!-- 期望输入 token -->
      <div>
        <label class="input-label">
          {{ t('admin.channelMonitor.providerHall.expectedInputTokens') }}
        </label>
        <input
          :value="expectedInputTokens ?? ''"
          type="number"
          min="1"
          class="input"
          :placeholder="t('admin.channelMonitor.providerHall.expectedInputTokensPlaceholder')"
          @input="handleExpectedTokensInput"
        />
        <p class="mt-1 text-xs text-gray-400">
          {{ t('admin.channelMonitor.providerHall.expectedInputTokensHint') }}
        </p>
      </div>
    </div>
  </details>
</template>

<script setup lang="ts">
/**
 * 渠道监控的「供应商大厅」配置区。
 *
 * 单独成组件（对齐 MonitorAdvancedRequestConfig 的做法），
 * 让 MonitorFormDialog 只需加一行调用，跟上游 rebase 时冲突面最小。
 */
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type { AdminGroup } from '@/types'
import Select from '@/components/common/Select.vue'
import Toggle from '@/components/common/Toggle.vue'
import GroupBadge from '@/components/common/GroupBadge.vue'

const props = defineProps<{
  /** dialog 打开时才拉分组列表，避免关闭状态下的无谓请求 */
  active: boolean
  groupId: number | null
  publicVisible: boolean
  publicNote: string
  reportUrl: string
  expectedInputTokens: number | null
}>()

const emit = defineEmits<{
  (e: 'update:groupId', value: number | null): void
  (e: 'update:publicVisible', value: boolean): void
  (e: 'update:publicNote', value: string): void
  (e: 'update:reportUrl', value: string): void
  (e: 'update:expectedInputTokens', value: number | null): void
}>()

const { t } = useI18n()

const groups = ref<AdminGroup[]>([])
const groupsLoading = ref(false)
let loaded = false

async function loadGroups() {
  if (loaded || groupsLoading.value) return
  groupsLoading.value = true
  try {
    // 含停用分组：监控可能仍绑在一个临时停用的分组上，
    // 不带出来会导致编辑时选择被静默清空。
    groups.value = await adminAPI.groups.getAllIncludingInactive()
    loaded = true
  } catch {
    // 拉取失败不阻塞表单其他字段，保持已有 group_id 不变。
    groups.value = []
  } finally {
    groupsLoading.value = false
  }
}

watch(
  () => props.active,
  (active) => {
    if (active) void loadGroups()
  },
  { immediate: true },
)

const groupOptions = computed(() =>
  groups.value.map((g) => ({
    value: String(g.id),
    label: g.status === 'inactive' ? `${g.name}（${t('common.disabled')}）` : g.name,
  })),
)

const groupSelectValue = computed(() => (props.groupId == null ? '' : String(props.groupId)))

const selectedGroup = computed(
  () => groups.value.find((g) => g.id === props.groupId) ?? null,
)

function handleGroupChange(value: string | number | boolean | null) {
  if (value === null || value === '') {
    emit('update:groupId', null)
    return
  }
  const parsed = Number(value)
  emit('update:groupId', Number.isFinite(parsed) && parsed > 0 ? parsed : null)
}

function handleExpectedTokensInput(event: Event) {
  const raw = (event.target as HTMLInputElement).value.trim()
  if (raw === '') {
    emit('update:expectedInputTokens', null)
    return
  }
  const parsed = Number(raw)
  emit('update:expectedInputTokens', Number.isFinite(parsed) && parsed > 0 ? parsed : null)
}

// 与后端 validateMonitorReportURL 保持一致：只放行 http/https。
const reportUrlInvalid = computed(() => {
  const raw = props.reportUrl.trim()
  if (raw === '') return false
  try {
    const parsed = new URL(raw)
    return parsed.protocol !== 'http:' && parsed.protocol !== 'https:'
  } catch {
    return true
  }
})
</script>
