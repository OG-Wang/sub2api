/**
 * 供应商大厅（Provider Hall）API
 *
 * 页面数据来自三个已有接口，按 group_id 在前端 join，不新增后端聚合接口：
 *   - /provider-hall/monitors        V1 主动探测：状态、探测延迟、探测用量、时间线
 *   - /channel-monitor-v2/matrix     V2 被动统计：用户真实 TTFT、缓存命中率、成功率
 *   - /groups/available + /groups/rates  分组倍率（用户专属）
 */

import { apiClient } from './client'
import {
  repeatedArrayParamsSerializer,
  type MonitorFilter,
  type MonitorMatrixResponse,
  type MonitorMatrixRow,
  type MonitorRange,
} from './channelMonitorV2'
import { userGroupsAPI } from './groups'
import type { Group } from '@/types'

/**
 * 探测时间线上的一个点，即**一次**探测。
 *
 * 刻意不做任何分桶/平均：桶内取平均会把一次真实故障抹进周围的正常样本里，
 * 而这页存在的意义就是让人看见故障。按默认 300s 间隔，最长的 7d 窗口
 * 每个监控也就 2016 个点，压缩后过网只有几百 KB，付得起。
 */
export interface HallTimelinePoint {
  checked_at: string
  status: string
  latency_ms: number | null
  ttft_ms: number | null
  input_tokens: number | null
  cached_input_tokens: number | null
  output_tokens: number | null
}

/**
 * 曲线的横轴定义，由后端按 range 算出（与 V2 matrix 用的是同一套窗口）。
 * 两条线都按时间戳在 [start, end) 中的相对位置定横坐标，同一个 x 才是同一时刻。
 *
 * bucket_seconds 是 V2 用户线的桶粒度——探测线不分桶，它只用于判断
 * 用户线上哪里断了。
 */
export interface HallWindow {
  range: MonitorRange
  start: string
  end: string
  bucket_seconds: number
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
  /**
   * 主模型在**当前窗口**内的探测可用率（0-100），窗口内没有探测时为 null。
   *
   * 跟着 range 走：曲线画的是哪段时间，这个数就是那段时间的。
   * 曾经这里是写死 7 天的 availability_7d，切窗口纹丝不动——
   * 一个此刻正挂着的渠道照样显示 60%。
   */
  availability: number | null
  extra_models: HallExtraModelStatus[]
  last_checked_at: string | null
  timeline: HallTimelinePoint[]
  /** 探测间隔（秒），用于判断曲线上哪里是真的没探测。 */
  interval_seconds: number
  primary_ttft_ms: number | null
  /** 上游报告的总输入，含缓存命中 */
  input_tokens: number | null
  /** 总输入里命中缓存的部分；总输入减去它 = 网关用量记录的 input */
  cached_input_tokens: number | null
  output_tokens: number | null
  expected_input_tokens: number | null
  /** 上游报的输入 token 与本地算出的真值对不上（多报少报都算）。 */
  input_tokens_deviated: boolean
  /** 相对真值的偏离量，正数为多报、负数为少报。 */
  input_tokens_excess: number
}

export interface HallMonitorResponse {
  items: HallMonitorItem[]
  generated_at: string
  window: HallWindow
  /**
   * V2 被动统计，与 items 用的是同一个时间窗口。
   *
   * 刻意由这个接口一并返回，而不是前端再打一次 /channel-monitor-v2/matrix：
   * 两个接口各自按 now 对齐窗口，请求跨过桶边界时会拿到相邻的两个区间，
   * 曲线就会错开一个桶。V2 不可用时为 null。
   */
  passive: MonitorMatrixResponse | null
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

export async function listHallMonitors(
  range: MonitorRange,
  platforms: string[],
  signal?: AbortSignal,
): Promise<HallMonitorResponse> {
  const { data } = await apiClient.get<HallMonitorResponse>('/provider-hall/monitors', {
    params: { range, platform: platforms.length ? platforms : undefined },
    paramsSerializer: { serialize: repeatedArrayParamsSerializer },
    signal,
  })
  return data
}

/**
 * 拉取并 join 数据。
 *
 * 探测 + 被动统计由 /provider-hall/monitors 一次返回（同一个时间窗口，见 HallMonitorResponse.passive），
 * 只有分组倍率还要单独取——它是用户专属的，跟监控数据不同源。
 * 倍率失败不阻断页面，只是不显示价格。
 */
export async function loadHallRows(
  filter: MonitorFilter,
  signal?: AbortSignal,
): Promise<{
  rows: HallRow[]
  generatedAt: string
  passiveAvailable: boolean
  window: HallWindow | null
}> {
  const monitorsPromise = listHallMonitors(filter.range, filter.platforms, signal)
  const groupsPromise = Promise.all([
    userGroupsAPI.getAvailable(),
    userGroupsAPI.getUserGroupRates(),
  ]).catch(() => null)

  const [monitors, groupData] = await Promise.all([monitorsPromise, groupsPromise])

  const matrix = monitors.passive
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
    passiveAvailable: matrix != null,
    window: monitors.window ?? null,
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
 * 倍率展示：有几位就显示几位，不补零也不截断。
 * 0.1 → "0.1"，0.045 → "0.045"，0.0425 → "0.0425"，1 → "1"。
 *
 * 先四舍五入到 6 位小数再转回数字，只为抹掉浮点噪声
 * （0.30000000000000004 这种），不是精度上限——后台倍率输入
 * 的步进是 0.001，6 位远远够用。
 */
export function formatRateMultiplier(value: number): string {
  if (!Number.isFinite(value)) return '—'
  return String(Number(value.toFixed(6)))
}

/**
 * 净输入 = 总输入 - 缓存命中，即网关用量记录里的 input 口径。
 * 缺任一项时返回 null，不猜。
 */
export function netInputTokens(
  inputTokens: number | null,
  cachedInputTokens: number | null,
): number | null {
  if (inputTokens == null) return null
  if (cachedInputTokens == null) return inputTokens
  return Math.max(0, inputTokens - cachedInputTokens)
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
