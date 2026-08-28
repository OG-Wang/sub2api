import { describe, expect, it } from 'vitest'

import enAdmin from '../locales/en/admin'
import enAdminChannelOnboarding from '../locales/en/admin/channelOnboarding'
import zhAdmin from '../locales/zh/admin'
import zhAdminChannelOnboarding from '../locales/zh/admin/channelOnboarding'

// 一键接入渠道的文案独立成模块，通过 admin/index.ts 的对象展开聚合。
// 展开有两个静默失败模式：模块没被注册（页面渲染出原始 key），
// 或与既有模块顶层键撞名被覆盖。这里把两者都固化成显式断言。

function leafKeys(value: unknown, prefix = ''): string[] {
  if (typeof value !== 'object' || value === null) return [prefix]
  return Object.entries(value as Record<string, unknown>).flatMap(([key, child]) =>
    leafKeys(child, prefix ? `${prefix}.${key}` : key)
  )
}

describe('channel onboarding locales', () => {
  it('is aggregated into the admin namespace for both languages', () => {
    expect(zhAdmin.channelOnboarding).toBe(zhAdminChannelOnboarding.channelOnboarding)
    expect(enAdmin.channelOnboarding).toBe(enAdminChannelOnboarding.channelOnboarding)
  })

  it('does not collide with another admin module', () => {
    for (const [name, aggregate] of [
      ['zh', zhAdmin],
      ['en', enAdmin],
    ] as const) {
      const ownKeys = Object.keys(name === 'zh' ? zhAdminChannelOnboarding : enAdminChannelOnboarding)
      expect(ownKeys, name).toEqual(['channelOnboarding'])
      expect(aggregate, name).toHaveProperty('channelOnboarding')
    }
  })

  it('keeps the Chinese and English key trees identical', () => {
    expect(leafKeys(zhAdminChannelOnboarding).sort()).toEqual(
      leafKeys(enAdminChannelOnboarding).sort()
    )
  })

  it('does not claim the upstream key is encrypted at rest', () => {
    // accounts.credentials is plaintext JSONB upstream; only the monitor's copy
    // of the generated sub2api key goes through SecretEncryptor.
    expect(zhAdminChannelOnboarding.channelOnboarding.form.apiKeyHint).not.toContain('加密')
    expect(enAdminChannelOnboarding.channelOnboarding.form.apiKeyHint.toLowerCase()).not.toContain(
      'encrypted'
    )
  })
})
