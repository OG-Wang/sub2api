import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const { post } = vi.hoisted(() => ({
  post: vi.fn(),
}))

vi.mock('@/api/client', () => ({
  apiClient: { post },
}))

import { create } from '@/api/admin/channelOnboarding'

describe('admin channel onboarding API', () => {
  beforeEach(() => {
    localStorage.clear()
    sessionStorage.clear()
    post.mockReset()
    post.mockResolvedValue({ data: { group_id: 1, account_id: 2, api_key_id: 3, monitor_id: 4 } })
    localStorage.setItem('auth_user', JSON.stringify({ id: 7 }))
    vi.spyOn(globalThis.crypto, 'randomUUID').mockReturnValue(
      '11111111-1111-4111-8111-111111111111'
    )
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('sends the onboarding payload and an idempotency key', async () => {
    await create({
      name: 'OpenAI primary',
      platform: 'openai',
      rate_multiplier: 1.2,
      upstream_base_url: 'https://api.example.com/',
      upstream_api_key: 'secret-is-never-logged',
      primary_model: 'gpt-4o-mini',
      interval_seconds: 900,
    })

    expect(post).toHaveBeenCalledWith(
      '/admin/channel-onboardings',
      expect.objectContaining({
        name: 'OpenAI primary',
        platform: 'openai',
        rate_multiplier: 1.2,
        primary_model: 'gpt-4o-mini',
      }),
      {
        headers: {
          'Idempotency-Key': 'channel-onboarding-7-11111111-1111-4111-8111-111111111111',
        },
      }
    )
    expect(sessionStorage.length).toBe(0)
  })

  it('reuses the same key after an ambiguous failure and clears it after success', async () => {
    post.mockRejectedValueOnce(new Error('network timeout'))
    const params = {
      name: 'Anthropic primary' as const,
      platform: 'anthropic' as const,
      rate_multiplier: 1,
      upstream_base_url: 'https://api.example.com',
      upstream_api_key: 'secret',
      primary_model: 'claude-sonnet',
      interval_seconds: 900,
    }

    await expect(create(params)).rejects.toThrow('network timeout')
    expect(Array.from({ length: sessionStorage.length }, (_, index) => sessionStorage.key(index))).toHaveLength(1)

    post.mockResolvedValueOnce({ data: { monitor_id: 9 } })
    await create(params)

    expect(post).toHaveBeenCalledTimes(2)
    expect(post.mock.calls[1][2]).toEqual(post.mock.calls[0][2])
    expect(sessionStorage.length).toBe(0)
  })
})
