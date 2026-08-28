import { apiClient } from '../client'

// Single source of truth on this side. Mirrors service.ChannelOnboardingPlatforms()
// on the backend, which derives the set from probeCapableProviders.
// antigravity is deliberately absent: it has no probe adapter, so it cannot
// back the active monitor this workflow creates.
export const CHANNEL_ONBOARDING_PLATFORMS = [
  'openai',
  'anthropic',
  'gemini',
  'grok',
  'kimi',
  'zhipu',
  'deepseek',
] as const

export type ChannelOnboardingPlatform = (typeof CHANNEL_ONBOARDING_PLATFORMS)[number]

export interface ChannelOnboardingParams {
  name: string
  platform: ChannelOnboardingPlatform
  rate_multiplier: number
  upstream_base_url: string
  upstream_api_key: string
  primary_model: string
  concurrency: number
  interval_seconds: number
  expected_input_tokens?: number
}

export interface ChannelOnboardingResult {
  group_id: number
  account_id: number
  api_key_id: number
  monitor_id: number
  api_key_masked: string
  group_name: string
  account_name: string
  monitor_name: string
  platform: ChannelOnboardingPlatform
  rate_multiplier: number
  concurrency: number
  interval_seconds: number
  public_visible: boolean
  expected_input_tokens?: number
}

const inMemoryOperationKeys = new Map<string, string>()

function getCurrentAdminID(): string {
  try {
    const rawUser = globalThis.localStorage?.getItem('auth_user')
    if (rawUser) {
      const user: unknown = JSON.parse(rawUser)
      if (typeof user === 'object' && user !== null) {
        const id = (user as { id?: unknown }).id
        if (typeof id === 'number' && Number.isSafeInteger(id) && id > 0) {
          return String(id)
        }
      }
    }
  } catch {
    // Fall through to the non-persisted unknown-admin scope.
  }
  return 'unknown-admin'
}

async function payloadFingerprint(params: ChannelOnboardingParams): Promise<string> {
  const serialized = JSON.stringify(params)
  try {
    const bytes = new TextEncoder().encode(serialized)
    const digest = await globalThis.crypto.subtle.digest('SHA-256', bytes)
    return Array.from(new Uint8Array(digest), byte => byte.toString(16).padStart(2, '0')).join('')
  } catch {
    let hash = 2166136261
    for (let i = 0; i < serialized.length; i += 1) {
      hash ^= serialized.charCodeAt(i)
      hash = Math.imul(hash, 16777619)
    }
    return (hash >>> 0).toString(16).padStart(8, '0')
  }
}

async function operationStorageKey(params: ChannelOnboardingParams): Promise<string> {
  return `sub2api:admin:channel-onboarding:create:${getCurrentAdminID()}:${await payloadFingerprint(params)}`
}

function getStoredOperationKey(storageKey: string): string | null {
  try {
    return globalThis.sessionStorage?.getItem(storageKey) ?? null
  } catch {
    return null
  }
}

function storeOperationKey(storageKey: string, key: string | null): void {
  try {
    if (key) globalThis.sessionStorage?.setItem(storageKey, key)
    else globalThis.sessionStorage?.removeItem(storageKey)
  } catch {
    // In-memory retry protection still works when browser storage is unavailable.
  }
}

async function getOperationKey(params: ChannelOnboardingParams): Promise<{ key: string; storageKey: string }> {
  const storageKey = await operationStorageKey(params)
  const existing = inMemoryOperationKeys.get(storageKey) ?? getStoredOperationKey(storageKey)
  if (existing) return { key: existing, storageKey }

  const requestID =
    globalThis.crypto?.randomUUID?.() ??
    `${Date.now()}-${Math.random().toString(36).slice(2)}`
  const key = `channel-onboarding-${getCurrentAdminID()}-${requestID}`
  inMemoryOperationKeys.set(storageKey, key)
  storeOperationKey(storageKey, key)
  return { key, storageKey }
}

export async function create(
  params: ChannelOnboardingParams
): Promise<ChannelOnboardingResult> {
  const operation = await getOperationKey(params)
  const { data } = await apiClient.post<ChannelOnboardingResult>(
    '/admin/channel-onboardings',
    params,
    { headers: { 'Idempotency-Key': operation.key } }
  )

  inMemoryOperationKeys.delete(operation.storageKey)
  storeOperationKey(operation.storageKey, null)
  return data
}

export const channelOnboardingAPI = { create }

export default channelOnboardingAPI
