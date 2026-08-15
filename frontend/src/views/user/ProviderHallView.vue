<template>
  <AppLayout>
    <TablePageLayout>
      <template #actions>
        <div class="flex flex-wrap items-center justify-between gap-3">
          <div>
            <h1 class="text-xl font-semibold text-gray-900 dark:text-white">
              {{ t('nav.channelStatus') }}
            </h1>
            <p class="mt-0.5 text-sm text-gray-500 dark:text-dark-400">
              {{ t('providerHall.description') }}
            </p>
          </div>
          <div class="flex flex-wrap items-center gap-2">
            <span v-if="generatedAt" class="text-xs text-gray-400 dark:text-dark-500">
              {{ t('providerHall.generatedAt', { time: formattedGeneratedAt }) }}
            </span>
            <button
              type="button"
              class="btn btn-secondary"
              :disabled="loading"
              @click="reload"
            >
              {{ t('common.refresh') }}
            </button>
          </div>
        </div>
      </template>

      <template #filters>
        <!-- 平台分页签 + 时间窗口 -->
        <div class="flex flex-wrap items-center justify-between gap-3">
          <div class="inline-flex rounded-lg border border-gray-200 bg-gray-50 p-1 dark:border-dark-600 dark:bg-dark-900/40">
            <button
              v-for="tab in PLATFORM_TABS"
              :key="tab"
              type="button"
              class="rounded-md px-4 py-1.5 text-sm font-medium transition"
              :class="
                activeTab === tab
                  ? 'bg-white text-primary-700 shadow-sm dark:bg-dark-800 dark:text-primary-300'
                  : 'text-gray-600 hover:text-gray-900 dark:text-dark-300 dark:hover:text-white'
              "
              @click="switchTab(tab)"
            >
              {{ t(`providerHall.tabs.${tab}`) }}
            </button>
          </div>

          <div class="flex items-center gap-2">
            <span class="text-xs text-gray-500 dark:text-dark-400">
              {{ t('providerHall.window') }}
            </span>
            <Select v-model="range" :options="rangeOptions" class="w-32" @update:model-value="reload" />
          </div>
        </div>
      </template>

      <template #table>
        <p v-if="!passiveAvailable" class="px-4 pt-3 text-xs text-amber-600 dark:text-amber-400">
          {{ t('providerHall.passiveUnavailable') }}
        </p>

        <DataTable
          :columns="columns"
          :data="visibleRows"
          :loading="loading"
          row-key="id"
          default-sort-key="availability"
          default-sort-order="desc"
        >
        <!-- 分组 -->
        <template #cell-group="{ row }">
          <div class="flex flex-col gap-0.5">
            <span class="font-medium text-gray-900 dark:text-white">{{ row.group_name || row.name }}</span>
            <span v-if="row.public_note" class="max-w-[16rem] text-xs text-gray-500 dark:text-dark-400">
              {{ row.public_note }}
            </span>
          </div>
        </template>

        <!-- 倍率 -->
        <template #cell-rate="{ row }">
          <span v-if="row.rateMultiplier != null" class="tabular-nums">
            {{ formatRateMultiplier(row.rateMultiplier) }}
          </span>
          <span v-else class="text-gray-400 dark:text-dark-500">—</span>
        </template>

        <!-- 最新状态 -->
        <template #cell-status="{ row }">
          <div class="flex flex-col items-start gap-1">
            <span
              class="inline-flex items-center rounded-full px-2 py-0.5 text-[11px]"
              :class="statusBadgeClass(row.primary_status)"
            >
              {{ statusLabel(row.primary_status) }}
            </span>
            <span
              v-for="m in modelStatuses(row)"
              :key="m.model"
              class="inline-flex items-center gap-1 whitespace-nowrap rounded-full px-1.5 py-0.5 text-[10px] leading-tight"
              :class="statusBadgeClass(m.status)"
              :title="`${m.model}: ${statusLabel(m.status)}`"
            >
              {{ m.model }}
              <span
                class="h-1.5 w-1.5 shrink-0 rounded-full"
                :class="statusDotClass(m.status)"
                aria-hidden="true"
              />
            </span>
          </div>
        </template>

        <!-- 最新首 Token -->
        <template #cell-latency="{ row }">
          <HallLatencyCell :row="row" />
        </template>

        <!-- 缓存命中率 -->
        <template #cell-cache="{ row }">
          <span v-if="row.passive" class="tabular-nums">
            {{ formatPercent(row.passive.metrics.cache_rate) }}
          </span>
          <span v-else class="text-gray-400 dark:text-dark-500">—</span>
        </template>

        <!-- 可用率 -->
        <template #cell-availability="{ row }">
          <span class="tabular-nums">{{ formatPercent(availabilityOf(row)) }}</span>
        </template>

        <!-- 曲线 -->
        <template #header-chart>
          <div class="flex items-center gap-2">
            <span>{{ t('providerHall.columns.chart') }}</span>
            <Select v-model="chartMetric" :options="metricOptions" class="w-28" @click.stop />
          </div>
        </template>
        <template #cell-chart="{ row }">
          <HallSparkline :row="row" :metric="chartMetric" />
        </template>

        <!-- 最近监测 -->
        <template #cell-checked="{ row }">
          <span class="text-xs text-gray-500 dark:text-dark-400">
            {{ formatCheckedAt(row.last_checked_at) }}
          </span>
        </template>

        <!-- 使用此分组 -->
        <template #cell-actions="{ row }">
          <div class="flex items-center gap-2">
            <button
              type="button"
              class="btn btn-primary btn-sm"
              :disabled="row.group_id == null"
              :title="row.group_id == null ? t('providerHall.noGroupLinked') : ''"
              @click="useGroup(row)"
            >
              {{ t('providerHall.useGroup') }}
            </button>
            <a
              v-if="row.report_url"
              :href="row.report_url"
              target="_blank"
              rel="noopener noreferrer"
              class="text-xs text-primary-600 hover:underline dark:text-primary-400"
            >
              {{ t('providerHall.report') }}
            </a>
          </div>
        </template>

        <template #empty>
          <EmptyState
            :title="t('providerHall.emptyTitle')"
            :description="t('providerHall.emptyHint')"
          />
        </template>
        </DataTable>
      </template>
    </TablePageLayout>

    <HallUseGroupDialog
      :show="showUseGroup"
      :group-id="useGroupTarget?.group_id ?? null"
      :group-name="useGroupTarget?.group_name ?? ''"
      :rate-multiplier="useGroupTarget?.rateMultiplier ?? null"
      @close="showUseGroup = false"
      @switched="showUseGroup = false"
    />
  </AppLayout>
</template>

<script setup lang="ts">
/**
 * 「渠道状态」的 V1 + V2 并存视图（原独立页面「供应商大厅」）。
 *
 * 由 ChannelStatusView 在 hybrid 模式下挂载，没有独立路由——
 * 所以这里不再自查模式，能渲染就说明两套数据都在。
 *
 * 三份数据在前端按 group_id join（见 api/providerHall.ts），
 * 不新增后端聚合接口。
 */
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import {
  loadHallRows,
  rowMatchesTab,
  platformsForTab,
  formatRateMultiplier,
  type HallPlatformTab,
  type HallRow,
} from '@/api/providerHall'
import type { MonitorRange } from '@/api/channelMonitorV2'
import type { Column } from '@/components/common/types'
import AppLayout from '@/components/layout/AppLayout.vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import Select from '@/components/common/Select.vue'
import HallLatencyCell from '@/components/user/hall/HallLatencyCell.vue'
import HallSparkline, { type HallSparklineMetric } from '@/components/user/hall/HallSparkline.vue'
import HallUseGroupDialog from '@/components/user/hall/HallUseGroupDialog.vue'

const { t } = useI18n()
const appStore = useAppStore()

const PLATFORM_TABS: HallPlatformTab[] = ['openai', 'anthropic', 'other']

const loading = ref(false)
const rows = ref<HallRow[]>([])
const generatedAt = ref('')
const passiveAvailable = ref(true)
// 默认落在第一个标签页，跟着 PLATFORM_TABS 的顺序走，改名/换序都不用动这里。
const activeTab = ref<HallPlatformTab>(PLATFORM_TABS[0])
const chartMetric = ref<HallSparklineMetric>('ttft')
const showUseGroup = ref(false)
const useGroupTarget = ref<HallRow | null>(null)

// V2 支持的窗口就是这四个；规格里写的 6h 并不存在，用 90m 代替最短档。
const range = ref<MonitorRange>('24h')
const rangeOptions = computed(() => [
  { value: '90m', label: t('providerHall.ranges.90m') },
  { value: '24h', label: t('providerHall.ranges.24h') },
  { value: '7d', label: t('providerHall.ranges.7d') },
  { value: '30d', label: t('providerHall.ranges.30d') },
])

const metricOptions = computed(() => [
  { value: 'ttft', label: t('providerHall.metrics.ttft') },
  { value: 'tps', label: t('providerHall.metrics.tps') },
  { value: 'inputTokens', label: t('providerHall.metrics.inputTokens') },
])

const columns = computed<Column[]>(() => [
  { key: 'group', label: t('providerHall.columns.group') },
  { key: 'rate', label: t('providerHall.columns.rate'), sortable: true },
  { key: 'status', label: t('providerHall.columns.status') },
  { key: 'latency', label: t('providerHall.columns.latency'), sortable: true },
  { key: 'cache', label: t('providerHall.columns.cache') },
  { key: 'availability', label: t('providerHall.columns.availability'), sortable: true },
  { key: 'chart', label: t('providerHall.columns.chart') },
  { key: 'checked', label: t('providerHall.columns.checked') },
  { key: 'actions', label: t('providerHall.columns.actions') },
])

const visibleRows = computed(() =>
  rows.value
    .filter((r) => rowMatchesTab(r, activeTab.value))
    // DataTable 的排序读的是 row[key]，所以把可排序列的值摊平上去。
    .map((r) => ({
      ...r,
      rate: r.rateMultiplier,
      latency: r.primary_ttft_ms ?? r.primary_latency_ms,
      availability: availabilityOf(r),
    })),
)

/**
 * 可用率优先取 V2 的真实成功率——那是用户实际请求的结果。
 * 没有被动数据时退回 V1 探测的 7 天可用率。
 */
function availabilityOf(row: HallRow): number {
  const successRate = row.passive?.metrics
    ? 100 - row.passive.metrics.error_rate * 100
    : null
  return successRate ?? row.availability_7d
}

function formatPercent(value: number | null | undefined): string {
  if (value == null || Number.isNaN(value)) return '—'
  return `${value.toFixed(2)}%`
}

function statusLabel(status: string): string {
  if (!status) return t('providerHall.status.unknown')
  return t(`providerHall.status.${status}`, status)
}

/**
 * 「最新状态」列的模型胶囊：主模型排第一，附加模型按配置顺序跟随。
 *
 * 主模型的状态在响应里是平铺的（primary_model / primary_status），
 * 附加模型在 extra_models 里，这里拼成同构的一串，模板只管画。
 * 模型名直接用上游原始 ID，不做截短。
 */
function modelStatuses(row: HallRow): Array<{ model: string; status: string }> {
  const out: Array<{ model: string; status: string }> = []
  const seen = new Set<string>()
  const push = (model: string, status: string) => {
    // 附加模型里重复填了主模型时会拿到两条同名记录，去重避免 key 冲突。
    if (!model || seen.has(model)) return
    seen.add(model)
    out.push({ model, status })
  }
  push(row.primary_model, row.primary_status)
  for (const m of row.extra_models) push(m.model, m.status)
  return out
}

function statusBadgeClass(status: string): string {
  switch (status) {
    case 'operational':
      return 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300'
    case 'degraded':
      return 'bg-amber-100 text-amber-700 dark:bg-amber-900/40 dark:text-amber-300'
    case 'failed':
    case 'error':
      return 'bg-red-100 text-red-700 dark:bg-red-900/40 dark:text-red-300'
    default:
      return 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-dark-300'
  }
}

function statusDotClass(status: string): string {
  switch (status) {
    case 'operational':
      return 'bg-emerald-500'
    case 'degraded':
      return 'bg-amber-500'
    case 'failed':
    case 'error':
      return 'bg-red-500'
    default:
      return 'bg-gray-300 dark:bg-dark-600'
  }
}

function formatCheckedAt(value: string | null): string {
  if (!value) return '—'
  const d = new Date(value)
  if (Number.isNaN(d.getTime())) return '—'
  return d.toLocaleString()
}

const formattedGeneratedAt = computed(() => {
  if (!generatedAt.value) return ''
  const d = new Date(generatedAt.value)
  return Number.isNaN(d.getTime()) ? '' : d.toLocaleTimeString()
})

function switchTab(tab: HallPlatformTab) {
  if (activeTab.value === tab) return
  activeTab.value = tab
  void reload()
}

/** 弹出「使用此分组」窗口：把已有 Key 切到该分组，或跳去新建。 */
function useGroup(row: HallRow) {
  if (row.group_id == null) return
  useGroupTarget.value = row
  showUseGroup.value = true
}

async function reload() {
  if (loading.value) return
  loading.value = true
  try {
    const result = await loadHallRows({
      range: range.value,
      platforms: platformsForTab(activeTab.value),
      groupIds: [],
      models: [],
    })
    rows.value = result.rows
    generatedAt.value = result.generatedAt
    passiveAvailable.value = result.passiveAvailable
  } catch (err: unknown) {
    appStore.showError(extractApiErrorMessage(err, t('providerHall.loadError')))
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  void reload()
})
</script>
