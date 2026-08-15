<template>
  <BaseDialog
    :show="show"
    :title="t('providerHall.useGroupDialog.title', { group: groupName })"
    width="extra-wide"
    @close="handleClose"
  >
    <div class="space-y-4">
      <!-- 目标分组 + 新建入口 -->
      <div class="flex flex-wrap items-start justify-between gap-3">
        <div>
          <p class="text-base font-medium text-gray-900 dark:text-white">{{ groupName }}</p>
          <p v-if="rateMultiplier != null" class="mt-0.5 text-sm text-gray-500 dark:text-dark-400">
            {{ t('providerHall.useGroupDialog.rate', { rate: formatRate(rateMultiplier) }) }}
          </p>
        </div>
        <button type="button" class="btn btn-secondary" @click="goCreateKey">
          <span class="mr-1">＋</span>{{ t('providerHall.useGroupDialog.createNew') }}
        </button>
      </div>

      <SearchInput
        v-model="search"
        :placeholder="t('providerHall.useGroupDialog.searchPlaceholder')"
        class="w-full"
      />

      <!-- 已有 Key 列表，单选 -->
      <div class="overflow-hidden rounded-lg border border-gray-200 dark:border-dark-600">
        <div v-if="loading" class="p-6 text-center text-sm text-gray-500 dark:text-dark-400">
          {{ t('common.loading') }}
        </div>
        <div v-else-if="filteredKeys.length === 0" class="p-6">
          <EmptyState
            :title="t('providerHall.useGroupDialog.emptyTitle')"
            :description="t('providerHall.useGroupDialog.emptyHint')"
          />
        </div>
        <table v-else class="min-w-full divide-y divide-gray-200 dark:divide-dark-600">
          <thead class="bg-gray-50 dark:bg-dark-900/40">
            <tr class="text-left text-xs font-medium uppercase tracking-wider text-gray-500 dark:text-dark-400">
              <th class="w-10 px-4 py-3"></th>
              <th class="px-4 py-3">{{ t('providerHall.useGroupDialog.columns.name') }}</th>
              <th class="px-4 py-3">{{ t('providerHall.useGroupDialog.columns.key') }}</th>
              <th class="px-4 py-3">{{ t('providerHall.useGroupDialog.columns.status') }}</th>
              <th class="px-4 py-3">{{ t('providerHall.useGroupDialog.columns.currentGroup') }}</th>
              <th class="px-4 py-3">{{ t('providerHall.useGroupDialog.columns.lastUsed') }}</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
            <tr
              v-for="k in filteredKeys"
              :key="k.id"
              class="cursor-pointer text-sm transition-colors hover:bg-gray-50 dark:hover:bg-dark-800"
              :class="selectedId === k.id && 'bg-primary-50/60 dark:bg-primary-900/20'"
              @click="selectKey(k)"
            >
              <td class="px-4 py-3">
                <input
                  type="radio"
                  name="hall-use-group-key"
                  class="h-4 w-4 text-primary-500 focus:ring-primary-500"
                  :checked="selectedId === k.id"
                  :disabled="k.group_id === groupId"
                  @change="selectKey(k)"
                />
              </td>
              <td class="px-4 py-3 font-medium text-gray-900 dark:text-white">{{ k.name }}</td>
              <td class="px-4 py-3">
                <code class="font-mono text-xs text-gray-500 dark:text-dark-400">{{ maskApiKey(k.key) }}</code>
              </td>
              <td class="px-4 py-3">
                <span
                  class="inline-flex items-center rounded-full px-2 py-0.5 text-[11px]"
                  :class="statusClass(k.status)"
                >
                  {{ t(`providerHall.useGroupDialog.status.${k.status}`, k.status) }}
                </span>
              </td>
              <td class="px-4 py-3">
                <GroupBadge
                  v-if="k.group"
                  :name="k.group.name"
                  :platform="k.group.platform"
                  :subscription-type="k.group.subscription_type"
                  :rate-multiplier="k.group.rate_multiplier"
                />
                <span v-else class="text-xs text-gray-400 dark:text-dark-500">
                  {{ t('providerHall.useGroupDialog.noGroup') }}
                </span>
              </td>
              <td class="px-4 py-3 text-xs text-gray-500 dark:text-dark-400">
                {{ formatLastUsed(k.last_used_at) }}
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <p v-if="alreadyInGroupCount > 0" class="text-xs text-gray-400 dark:text-dark-500">
        {{ t('providerHall.useGroupDialog.alreadyInGroup', { count: alreadyInGroupCount }) }}
      </p>
    </div>

    <template #footer>
      <div class="flex justify-end gap-3">
        <button type="button" class="btn btn-secondary" @click="handleClose">
          {{ t('common.cancel') }}
        </button>
        <button
          type="button"
          class="btn btn-primary"
          :disabled="selectedId == null || switching"
          @click="handleSwitch"
        >
          {{ switching ? t('common.saving') : t('providerHall.useGroupDialog.submit') }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
/**
 * 「使用此分组」弹窗：把一把已有的 API Key 切换到该分组。
 *
 * 新建走跳转而不是在这里再嵌一个创建表单——创建有名称、有效期、配额、
 * IP 限制等一堆字段，那是 /keys 页面的职责，在弹窗里重做一遍只会两处漂移。
 */
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import { maskApiKey } from '@/utils/maskApiKey'
import { keysAPI } from '@/api'
import { formatRateMultiplier } from '@/api/providerHall'
import type { ApiKey } from '@/types'
import BaseDialog from '@/components/common/BaseDialog.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import GroupBadge from '@/components/common/GroupBadge.vue'
import SearchInput from '@/components/common/SearchInput.vue'

const props = defineProps<{
  show: boolean
  groupId: number | null
  groupName: string
  /** 该用户在此分组上的实际倍率，用于弹窗顶部展示。 */
  rateMultiplier: number | null
}>()

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'switched'): void
}>()

const { t } = useI18n()
const router = useRouter()
const appStore = useAppStore()

const keys = ref<ApiKey[]>([])
const loading = ref(false)
const switching = ref(false)
const search = ref('')
const selectedId = ref<number | null>(null)

/** 已经在目标分组里的 Key 无需切换，计数展示但不可选。 */
const alreadyInGroupCount = computed(
  () => keys.value.filter((k) => k.group_id === props.groupId).length,
)

const filteredKeys = computed(() => {
  const q = search.value.trim().toLowerCase()
  if (!q) return keys.value
  return keys.value.filter(
    (k) => k.name.toLowerCase().includes(q) || k.key.toLowerCase().includes(q),
  )
})

watch(
  () => props.show,
  (show) => {
    if (!show) return
    selectedId.value = null
    search.value = ''
    void loadKeys()
  },
)

async function loadKeys() {
  loading.value = true
  try {
    // 一次拉够：普通用户的 Key 数量不会到需要分页的量级，
    // 弹窗里再做分页只会让「找到那把 Key」变得更麻烦。
    const res = await keysAPI.list(1, 200)
    keys.value = res.items || []
  } catch (err: unknown) {
    appStore.showError(extractApiErrorMessage(err, t('providerHall.useGroupDialog.loadFailed')))
    keys.value = []
  } finally {
    loading.value = false
  }
}

function selectKey(k: ApiKey) {
  if (k.group_id === props.groupId) return
  selectedId.value = k.id
}

async function handleSwitch() {
  if (selectedId.value == null || props.groupId == null || switching.value) return
  switching.value = true
  try {
    await keysAPI.update(selectedId.value, { group_id: props.groupId })
    appStore.showSuccess(t('providerHall.useGroupDialog.success', { group: props.groupName }))
    emit('switched')
    emit('close')
  } catch (err: unknown) {
    appStore.showError(extractApiErrorMessage(err, t('providerHall.useGroupDialog.failed')))
  } finally {
    switching.value = false
  }
}

function handleClose() {
  emit('close')
}

/** 新建走 /keys 页面：创建表单的字段远比这里多，不在弹窗里重做一遍。 */
function goCreateKey() {
  if (props.groupId == null) return
  void router.push({
    path: '/keys',
    query: { create: '1', group_id: String(props.groupId) },
  })
}

function statusClass(status: string): string {
  switch (status) {
    case 'active':
      return 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300'
    case 'inactive':
      return 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-dark-300'
    default:
      return 'bg-amber-100 text-amber-700 dark:bg-amber-900/40 dark:text-amber-300'
  }
}

function formatRate(rate: number): string {
  return `${formatRateMultiplier(rate)}x`
}

function formatLastUsed(value: string | null): string {
  if (!value) return t('providerHall.useGroupDialog.neverUsed')
  const d = new Date(value)
  return Number.isNaN(d.getTime()) ? '—' : d.toLocaleString()
}
</script>
