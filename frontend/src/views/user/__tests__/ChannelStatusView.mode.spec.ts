import { describe, expect, it, vi, beforeEach } from 'vitest'
import { defineComponent, h } from 'vue'
import { mount } from '@vue/test-utils'

const isV1 = vi.fn(() => false)
const isHybrid = vi.fn(() => false)

vi.mock('@/utils/featureFlags', () => ({
  isChannelMonitorV1Mode: () => isV1(),
  isProviderHallEnabled: () => isHybrid(),
}))

vi.mock('../ChannelStatusV1View.vue', () => ({
  default: defineComponent({ name: 'ChannelStatusV1View', setup: () => () => h('div', { 'data-testid': 'v1' }) }),
}))
vi.mock('../ChannelStatusV2View.vue', () => ({
  default: defineComponent({ name: 'ChannelStatusV2View', setup: () => () => h('div', { 'data-testid': 'v2' }) }),
}))
vi.mock('../ProviderHallView.vue', () => ({
  default: defineComponent({ name: 'ProviderHallView', setup: () => () => h('div', { 'data-testid': 'hybrid' }) }),
}))

import ChannelStatusView from '../ChannelStatusView.vue'

describe('ChannelStatusView mode switch', () => {
  beforeEach(() => {
    isV1.mockReset()
    isHybrid.mockReset()
  })

  it('renders V2 when not in v1 mode', () => {
    isV1.mockReturnValue(false)
    isHybrid.mockReturnValue(false)
    const wrapper = mount(ChannelStatusView)
    expect(wrapper.find('[data-testid="v2"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="v1"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="hybrid"]').exists()).toBe(false)
  })

  it('renders V1 when in v1 mode', () => {
    isV1.mockReturnValue(true)
    isHybrid.mockReturnValue(false)
    const wrapper = mount(ChannelStatusView)
    expect(wrapper.find('[data-testid="v1"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="v2"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="hybrid"]').exists()).toBe(false)
  })

  // hybrid 下 isChannelMonitorV1Mode() 同样返回 true，所以模板里 hybrid 必须先判断。
  // 这条盯的就是分支顺序：写反了会静默退回 V1 界面。
  it('renders the hybrid view when in hybrid mode', () => {
    isV1.mockReturnValue(true)
    isHybrid.mockReturnValue(true)
    const wrapper = mount(ChannelStatusView)
    expect(wrapper.find('[data-testid="hybrid"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="v1"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="v2"]').exists()).toBe(false)
  })
})
