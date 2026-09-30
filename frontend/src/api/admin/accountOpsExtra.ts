import { apiClient } from '../client'

/**
 * 智能运维扩展接口：首字监控（按分组自动开关账号调度）与账号账单对账。
 */

export interface FirstTokenMonitorConfig {
  enabled: boolean
  group_ids: number[]
  window_minutes: number
  min_samples: number
  open_threshold_ms: number
  close_threshold_ms: number
  interval_seconds: number
}

export interface FirstTokenMonitorSample {
  account_id: number
  account_name: string
  status: string
  schedulable: boolean
  average_ms: number
  samples: number
  group_ids: number[]
}

export interface FirstTokenMonitorEvent {
  at: string
  account_id: number
  account_name: string
  action: 'scheduling_enabled' | 'scheduling_disabled' | string
  average_ms: number
  samples: number
}

export interface FirstTokenMonitorStatus {
  config: FirstTokenMonitorConfig
  running: boolean
  last_run_at?: string
  last_error?: string
  events: FirstTokenMonitorEvent[]
  samples: FirstTokenMonitorSample[]
}

export async function getFirstTokenMonitor(): Promise<FirstTokenMonitorStatus> {
  return (await apiClient.get('/admin/account-ops/first-token')).data
}

export async function saveFirstTokenMonitorConfig(
  config: FirstTokenMonitorConfig
): Promise<FirstTokenMonitorConfig> {
  return (await apiClient.put('/admin/account-ops/first-token/config', config)).data
}

export async function runFirstTokenMonitor(): Promise<{
  events: FirstTokenMonitorEvent[]
  samples: FirstTokenMonitorSample[]
}> {
  return (await apiClient.post('/admin/account-ops/first-token/run')).data
}

export interface BillingReconcileRow {
  account_id: number
  account_name: string
  status: string
  requests: number
  total_tokens: number
  platform_cost: number
  platform_charged: number
  upstream_balance?: number
  upstream_currency?: string
  upstream_probed_at?: string
  upstream_spent?: number
  difference?: number
}

export interface BillingReconcileStatus {
  day: string
  rows: BillingReconcileRow[]
  total_platform_cost: number
  total_platform_charged: number
  total_upstream_spent: number
  upstream_spent_accounts: number
  last_run_at?: string
  last_error?: string
}

export async function getBillingReconcile(day?: string): Promise<BillingReconcileStatus> {
  return (
    await apiClient.get('/admin/account-ops/billing-reconcile', {
      params: day ? { day } : undefined
    })
  ).data
}

export async function runBillingReconcile(day?: string): Promise<{
  day: string
  rows: BillingReconcileRow[]
}> {
  return (
    await apiClient.post('/admin/account-ops/billing-reconcile/run', undefined, {
      params: day ? { day } : undefined
    })
  ).data
}