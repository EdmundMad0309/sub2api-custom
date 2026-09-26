import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import ExcelBPS403Badge from '../ExcelBPS403Badge.vue'
import type { Account } from '@/types'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: { time?: string }) => (params?.time ? `${key}@${params.time}` : key)
    })
  }
})

vi.mock('@/utils/format', async () => {
  const actual = await vi.importActual<typeof import('@/utils/format')>('@/utils/format')
  return {
    ...actual,
    formatDateTime: (date: Date) => date.toISOString()
  }
})

const disabledAt = '2026-09-26T15:04:05Z'

function makeAccount(overrides: Partial<Account>): Account {
  return {
    id: 1,
    name: 'account',
    platform: 'openai',
    type: 'oauth',
    proxy_id: null,
    concurrency: 1,
    priority: 1,
    status: 'active',
    error_message: null,
    last_used_at: null,
    expires_at: null,
    auto_pause_on_expired: true,
    created_at: '2026-09-01T00:00:00Z',
    updated_at: '2026-09-01T00:00:00Z',
    schedulable: true,
    rate_limited_at: null,
    rate_limit_reset_at: null,
    overload_until: null,
    temp_unschedulable_until: null,
    temp_unschedulable_reason: null,
    session_window_start: null,
    session_window_end: null,
    session_window_status: null,
    ...overrides
  }
}

describe('ExcelBPS403Badge', () => {
  it('BPS 403 自动关闭协议后显示标签和触发时间', () => {
    const wrapper = mount(ExcelBPS403Badge, {
      props: {
        account: makeAccount({
          extra: { openai_excel_bps: false, openai_excel_bps_403_disabled_at: disabledAt }
        })
      }
    })

    const badge = wrapper.get('[data-test="excel-bps-403-badge"]')
    expect(badge.text()).toBe('admin.accounts.openai.excelBPS403Badge')
    expect(badge.attributes('title')).toBe('admin.accounts.openai.excelBPS403BadgeTooltip@2026-09-26T15:04:05.000Z')
    expect(badge.classes()).toContain('bg-amber-400')
  })

  it('协议开关键被删除后仍显示标签', () => {
    const wrapper = mount(ExcelBPS403Badge, {
      props: { account: makeAccount({ extra: { openai_excel_bps_403_disabled_at: disabledAt } }) }
    })

    expect(wrapper.find('[data-test="excel-bps-403-badge"]').exists()).toBe(true)
  })

  it.each([
    ['重新开启协议', makeAccount({ extra: { openai_excel_bps: true, openai_excel_bps_403_disabled_at: disabledAt } })],
    ['没有自动关闭记录', makeAccount({ extra: { openai_excel_bps: false } })],
    ['记录时间无效', makeAccount({ extra: { openai_excel_bps_403_disabled_at: 'not-a-time' } })],
    ['记录不是字符串', makeAccount({ extra: { openai_excel_bps_403_disabled_at: 1 } })],
    ['没有 extra', makeAccount({ extra: undefined })],
    ['非 OAuth 账号', makeAccount({ type: 'apikey', extra: { openai_excel_bps_403_disabled_at: disabledAt } })],
    ['非 OpenAI 账号', makeAccount({ platform: 'anthropic', extra: { openai_excel_bps_403_disabled_at: disabledAt } })]
  ])('%s时不显示标签', (_, account) => {
    const wrapper = mount(ExcelBPS403Badge, { props: { account } })

    expect(wrapper.find('[data-test="excel-bps-403-badge"]').exists()).toBe(false)
  })
})
