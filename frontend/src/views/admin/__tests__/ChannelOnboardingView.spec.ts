import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

const { create } = vi.hoisted(() => ({
  create: vi.fn(),
}))
const showSuccess = vi.hoisted(() => vi.fn())
const showError = vi.hoisted(() => vi.fn())

vi.mock('@/api/admin', () => ({
  adminAPI: { channelOnboarding: { create } },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showSuccess, showError }),
}))

vi.mock('@/components/layout/AppLayout.vue', () => ({
  default: { template: '<div><slot /></div>' },
}))

vi.mock('@/components/icons/Icon.vue', () => ({
  default: { template: '<span />' },
}))

vi.mock('vue-i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('vue-i18n')>()),
  useI18n: () => ({ t: (key: string) => key }),
}))

import ChannelOnboardingView from '../ChannelOnboardingView.vue'
import { CHANNEL_ONBOARDING_PLATFORMS } from '@/api/admin/channelOnboarding'

describe('ChannelOnboardingView', () => {
  beforeEach(() => {
    create.mockResolvedValue({
      group_id: 11,
      account_id: 12,
      api_key_id: 13,
      monitor_id: 14,
      api_key_masked: 'sk-a***',
      group_name: 'OpenAI primary',
      account_name: 'OpenAI primary',
      monitor_name: 'OpenAI primary',
      platform: 'openai',
      rate_multiplier: 1.2,
      interval_seconds: 900,
      public_visible: true,
    })
  })

  afterEach(() => {
    vi.clearAllMocks()
  })

  it('submits the compact onboarding payload and renders only the masked key', async () => {
    const wrapper = mount(ChannelOnboardingView)

    await wrapper.find('#channel-onboarding-name').setValue('OpenAI primary')
    await wrapper.find('#channel-onboarding-rate').setValue('1.2')
    await wrapper.find('#channel-onboarding-base-url').setValue('https://api.example.com')
    await wrapper.find('#channel-onboarding-api-key').setValue('upstream-secret')
    await wrapper.find('#channel-onboarding-model').setValue('gpt-4o-mini')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(create).toHaveBeenCalledWith({
      name: 'OpenAI primary',
      platform: 'openai',
      rate_multiplier: 1.2,
      upstream_base_url: 'https://api.example.com',
      upstream_api_key: 'upstream-secret',
      primary_model: 'gpt-4o-mini',
      concurrency: 10,
      interval_seconds: 900,
    })
    expect(wrapper.text()).toContain('sk-a***')
    expect(wrapper.text()).not.toContain('upstream-secret')
    expect(showSuccess).toHaveBeenCalledWith('admin.channelOnboarding.success.toast')
  })

  it('allows arbitrary positive multiplier precision in the browser input', () => {
    const wrapper = mount(ChannelOnboardingView)
    const input = wrapper.find('#channel-onboarding-rate')

    expect(input.attributes('step')).toBe('any')
    expect(input.attributes('min')).toBe('0')
  })

  it('renders exactly the shared platform list, with no hardcoded second copy', () => {
    const wrapper = mount(ChannelOnboardingView)
    const values = wrapper
      .find('#channel-onboarding-platform')
      .findAll('option')
      .map(option => option.element.value)

    expect(values).toEqual([...CHANNEL_ONBOARDING_PLATFORMS])
    // antigravity has no probe adapter upstream, so it can never back the
    // active monitor this page creates.
    expect(values).not.toContain('antigravity')
  })

  it('sends the concurrency typed by the admin and clamps it to at least 1', async () => {
    const wrapper = mount(ChannelOnboardingView)
    const input = wrapper.find('#channel-onboarding-concurrency')
    expect(input.attributes('min')).toBe('1')
    expect(input.attributes('step')).toBe('1')

    await wrapper.find('#channel-onboarding-name').setValue('OpenAI concurrency')
    await wrapper.find('#channel-onboarding-base-url').setValue('https://api.example.com')
    await wrapper.find('#channel-onboarding-api-key').setValue('upstream-secret')
    await wrapper.find('#channel-onboarding-model').setValue('gpt-4o-mini')
    await input.setValue('48')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(create).toHaveBeenCalledWith(expect.objectContaining({ concurrency: 48 }))

    // 0 / empty is clamped in the browser rather than sent to the backend.
    await input.setValue('0')
    expect((wrapper.vm as unknown as { form: { concurrency: number } }).form.concurrency).toBe(1)
  })

  it('allows a custom monitor interval', async () => {
    const wrapper = mount(ChannelOnboardingView)

    await wrapper.find('#channel-onboarding-name').setValue('OpenAI custom interval')
    await wrapper.find('#channel-onboarding-base-url').setValue('https://api.example.com')
    await wrapper.find('#channel-onboarding-api-key').setValue('upstream-secret')
    await wrapper.find('#channel-onboarding-model').setValue('gpt-4o-mini')
    await wrapper.find('#channel-onboarding-interval').setValue('120')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(create).toHaveBeenCalledWith(expect.objectContaining({ interval_seconds: 120 }))
  })
})
