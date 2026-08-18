import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import MonitorFormDialog from '@/components/admin/monitor/MonitorFormDialog.vue'
import type { ChannelMonitor } from '@/api/admin/channelMonitor'
import type { ApiKey } from '@/types'

const { listTemplates, listKeys, getUserGroupRates } = vi.hoisted(() => ({
  listTemplates: vi.fn(),
  listKeys: vi.fn(),
  getUserGroupRates: vi.fn(),
}))

vi.mock('@/utils/featureFlags', () => ({
  isChannelMonitorV1Mode: () => true,
  isChannelMonitorV2Mode: () => false,
  getChannelMonitorMode: () => 'v1' as const,
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    channelMonitor: { create: vi.fn(), update: vi.fn() },
    channelMonitorTemplate: { list: listTemplates },
  },
}))

vi.mock('@/api/keys', () => ({
  keysAPI: { list: listKeys },
}))

vi.mock('@/api/groups', () => ({
  userGroupsAPI: { getUserGroupRates },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    cachedPublicSettings: null,
    showError: vi.fn(),
    showSuccess: vi.fn(),
  }),
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  }
})

const BaseDialogStub = defineComponent({
  props: { show: { type: Boolean, default: false } },
  template: '<div v-if="show"><slot /><slot name="footer" /></div>',
})

// 只关心「显示的是哪个分组」，不关心 GroupBadge 自身的配色逻辑。
const GroupBadgeStub = defineComponent({
  props: { name: { type: String, default: '' } },
  template: '<span class="group-badge">{{ name }}</span>',
})

function makeKey(id: number, groupId: number, groupName: string): ApiKey {
  return {
    id,
    key: `sk-monitor-key-${id}`,
    name: `key-${id}`,
    status: 'active',
    group_id: groupId,
    expires_at: null,
    group: {
      id: groupId,
      name: groupName,
      platform: 'anthropic',
      subscription_type: 'standard',
      rate_multiplier: 1,
    },
  } as unknown as ApiKey
}

const KEY_BASIC = makeKey(1, 10, 'Claude 基础组')
const KEY_PRO = makeKey(2, 20, 'Claude 高级组')

function mountDialog() {
  return mount(MonitorFormDialog, {
    props: { show: true, monitor: null },
    global: {
      stubs: {
        BaseDialog: BaseDialogStub,
        GroupBadge: GroupBadgeStub,
        Toggle: true,
        Select: true,
        ModelTagInput: true,
        MonitorAdvancedRequestConfig: true,
        MonitorProviderHallConfig: true,
      },
    },
  })
}

type Wrapper = ReturnType<typeof mountDialog>

async function openPicker(wrapper: Wrapper) {
  const button = wrapper
    .findAll('button')
    .find((b) => b.text() === 'admin.channelMonitor.form.useMyKey')
  expect(button).toBeDefined()
  await button!.trigger('click')
  await flushPromises()
}

function pickerRows(wrapper: Wrapper) {
  return wrapper.findAll('tbody tr')
}

function selectedBadge(wrapper: Wrapper) {
  return wrapper.find('[data-testid="monitor-selected-key"]')
}

describe('monitor form: selected API key marker', () => {
  beforeEach(() => {
    listTemplates.mockReset().mockResolvedValue({ items: [] })
    listKeys.mockReset().mockResolvedValue({ items: [KEY_BASIC, KEY_PRO] })
    getUserGroupRates.mockReset().mockResolvedValue({ 10: 1, 20: 1.5 })
  })

  it('shows the picked key group below the input and marks its row on reopen', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    // 没选过之前不显示任何标记
    expect(selectedBadge(wrapper).exists()).toBe(false)

    await openPicker(wrapper)
    expect(pickerRows(wrapper)).toHaveLength(2)
    // 首次打开时两行都没有勾
    expect(wrapper.findAll('[data-testid="monitor-key-picked"]')).toHaveLength(0)

    await pickerRows(wrapper)[1].trigger('click')
    await flushPromises()

    // 1. 选中后在下方标出当前选中的 Key（只显示分组）
    const badge = selectedBadge(wrapper)
    expect(badge.exists()).toBe(true)
    expect(badge.text()).toContain('admin.channelMonitor.form.selectedKey')
    expect(badge.text()).toContain('Claude 高级组')
    // 不泄露 Key 名称与 Key 值
    expect(badge.text()).not.toContain('key-2')
    expect(badge.text()).not.toContain('sk-monitor')

    // 2. 再次打开 picker 时该行高亮 + 行末打勾
    await openPicker(wrapper)
    const rows = pickerRows(wrapper)
    expect(rows[0].find('[data-testid="monitor-key-picked"]').exists()).toBe(false)
    expect(rows[1].find('[data-testid="monitor-key-picked"]').exists()).toBe(true)
    expect(rows[1].classes().join(' ')).toContain('bg-primary-50/60')
    expect(rows[0].classes().join(' ')).not.toContain('bg-primary-50/60')
  })

  it('drops the marker whenever the underlying api_key stops matching', async () => {
    const wrapper = mountDialog()
    await flushPromises()
    await openPicker(wrapper)
    await pickerRows(wrapper)[0].trigger('click')
    await flushPromises()
    expect(selectedBadge(wrapper).text()).toContain('Claude 基础组')

    // 手动改输入框 -> 标记作废，不能继续声称选的是那把 Key
    const apiKeyInput = wrapper.find('input[type="password"]')
    await apiKeyInput.setValue('sk-typed-by-hand')
    expect(selectedBadge(wrapper).exists()).toBe(false)

    // 重新选回来 -> 标记恢复
    await openPicker(wrapper)
    await pickerRows(wrapper)[0].trigger('click')
    await flushPromises()
    expect(selectedBadge(wrapper).exists()).toBe(true)

    // 切换 provider 会清空 api_key -> 标记跟着消失
    await wrapper.get('[data-testid="monitor-provider-openai"]').trigger('click')
    expect(selectedBadge(wrapper).exists()).toBe(false)
  })

  it('does not carry the marker over to another monitor', async () => {
    const wrapper = mountDialog()
    await flushPromises()
    await openPicker(wrapper)
    await pickerRows(wrapper)[1].trigger('click')
    await flushPromises()
    expect(selectedBadge(wrapper).exists()).toBe(true)

    // 本组件常驻挂载，切到另一个监控项编辑时标记必须清掉，
    // 否则会出现「编辑 A 时选的 Key 在编辑 B 上还挂着」的错标记。
    await wrapper.setProps({
      monitor: {
        id: 7,
        name: 'other',
        provider: 'anthropic',
        endpoint: 'https://api.example.com',
        primary_model: 'claude-3',
        api_key_masked: 'sk-b***',
        enabled: true,
      } as unknown as ChannelMonitor,
    })
    await flushPromises()

    expect(selectedBadge(wrapper).exists()).toBe(false)
    await openPicker(wrapper)
    expect(wrapper.findAll('[data-testid="monitor-key-picked"]')).toHaveLength(0)
  })
})
