/**
 * 供应商大厅（Provider Hall）API
 *
 * 页面数据来自三个已有接口，按 group_id 在前端 join，不新增后端聚合接口：
 *   - /provider-hall/monitors        V1 主动探测：状态、探测延迟、探测用量、时间线
 *   - /channel-monitor-v2/matrix     V2 被动统计：用户真实 TTFT、缓存命中率、成功率
 *   - /groups/available + /groups/rates  分组倍率（用户专属）
 */

import { apiClient } from './client'
import { getMatrix, type MonitorFilter, type MonitorMatrixRow } from './channelMonitorV2'
import { userGroupsAPI } from './groups'
import type { Group } from '@/types'

/** 探测时间线单点。 */
export interface HallTimelinePoint {
  status: string
  latency_ms: number | null
  ping_latency_ms: number | null
  checked_at: string
  ttft_ms: number | null
  input_tokens: number | null
  output_tokens: number | null
}

export interface HallExtraModelStatus {
  model: string
  status: string
  latency_ms: number | null
}

/** /provider-hall/monitors 返回的原始行。 */
export interface HallMonitorItem {
  id: number
  name: string
  provider: string
  group_name: string
  group_id: number | null
  platform: string
  public_note: string
  report_url: string
  primary_model: string
  primary_status: string
  primary_latency_ms: number | null
  availability_7d: number
  extra_models: HallExtraModelStatus[]
  last_checked_at: string | null
  timeline: HallTimelinePoint[]
  primary_ttft_ms: number | null
  input_tokens: number | null
  output_tokens: number | null
  expected_input_tokens: number | null
  input_tokens_inflated: boolean
}

export interface HallMonitorResponse {
  items: HallMonitorItem[]
  generated_at: string
}

/** 三方数据 join 后的一行，供表格直接渲染。 */
export interface HallRow extends HallMonitorItem {
  /** 该用户在此分组上的实际倍率，未关联分组或无权访问时为 null。 */
  rateMultiplier: number | null
  /** V2 被动统计，匹配不到（该分组近期无真实流量）时为 null。 */
  passive: MonitorMatrixRow | null
}

/** 平台分页签。「其他」聚合 openai / anthropic 之外的全部平台。 */
export type HallPlatformTab = 'openai' | 'anthropic' | 'other'

/** 「其他」页签覆盖的平台。 */
export const HALL_OTHER_PLATFORMS = ['gemini', 'grok', 'antigravity'] as const

/** 把页签映射成 v2 matrix 的 platforms 过滤值。 */
export function platformsForTab(tab: HallPlatformTab): string[] {
  if (tab === 'other') return [...HALL_OTHER_PLATFORMS]
  return [tab]
}

/** 判断一行是否属于某个页签。 */
export function rowMatchesTab(row: { platform: string }, tab: HallPlatformTab): boolean {
  const platform = (row.platform || '').toLowerCase()
  if (tab === 'other') return platform !== 'openai' && platform !== 'anthropic'
  return platform === tab
}

export async function listHallMonitors(signal?: AbortSignal): Promise<HallMonitorResponse> {
  const { data } = await apiClient.get<HallMonitorResponse>('/provider-hall/monitors', { signal })
  return data
}

/**
 * 拉取并 join 三方数据。
 *
 * V2 与分组倍率任一失败都不阻断页面：探测数据是主体，
 * 缺了被动指标只是少几列，缺了倍率只是不显示价格。
 */
export async function loadHallRows(
  filter: MonitorFilter,
  signal?: AbortSignal,
): Promise<{ rows: HallRow[]; generatedAt: string; passiveAvailable: boolean }> {
  const monitorsPromise = listHallMonitors(signal)
  const matrixPromise = getMatrix(filter, 'platform_group', false, signal).catch(() => null)
  const groupsPromise = Promise.all([
    userGroupsAPI.getAvailable(),
    userGroupsAPI.getUserGroupRates(),
  ]).catch(() => null)

  const [monitors, matrix, groupData] = await Promise.all([
    monitorsPromise,
    matrixPromise,
    groupsPromise,
  ])

  const passiveByGroup = new Map<number, MonitorMatrixRow>()
  for (const item of matrix?.items ?? []) {
    if (typeof item.group_id === 'number') passiveByGroup.set(item.group_id, item)
  }

  const rateByGroup = buildRateMap(groupData)

  const rows: HallRow[] = monitors.items.map((item) => ({
    ...item,
    rateMultiplier: item.group_id == null ? null : (rateByGroup.get(item.group_id) ?? null),
    passive: item.group_id == null ? null : (passiveByGroup.get(item.group_id) ?? null),
  }))

  return {
    rows,
    generatedAt: monitors.generated_at,
    passiveAvailable: matrix !== null,
  }
}

/**
 * 构造 group_id → 实际倍率 的映射。
 *
 * 用户专属倍率（/groups/rates）优先于分组默认倍率：同一个分组，
 * 管理员可能给某个用户单独配了价格，展示默认值会让用户看到错的数。
 */
function buildRateMap(
  groupData: [Group[], Record<number, number>] | null,
): Map<number, number> {
  const map = new Map<number, number>()
  if (!groupData) return map
  const [groups, userRates] = groupData
  for (const g of groups) {
    map.set(g.id, g.rate_multiplier)
  }
  for (const [groupId, rate] of Object.entries(userRates)) {
    map.set(Number(groupId), rate)
  }
  return map
}

/**
 * 由探测数据估算 TPS（token / 秒）。
 *
 * 公式：output_tokens / (总耗时 - 首 token 耗时)。分母是「开始出字到出完」
 * 的时间，扣掉首 token 等待才是真实的生成速度。
 *
 * 缺任一输入、或分母非正（TTFT 还没接入时 ttft 为 null）都返回 null，
 * 不猜、不填 0——0 会被误读成「这个渠道极慢」。
 */
export function computeTps(
  outputTokens: number | null,
  latencyMs: number | null,
  ttftMs: number | null,
): number | null {
  if (outputTokens == null || outputTokens <= 0) return null
  if (latencyMs == null || ttftMs == null) return null
  const generationMs = latencyMs - ttftMs
  if (generationMs <= 0) return null
  return (outputTokens / generationMs) * 1000
}
