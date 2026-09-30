import { apiClient } from '../client'

/**
 * 智能运维 · 上游余额：为每个账号保存上游面板登录凭据（NewAPI / Sub2API）并读取余额。
 */

export type UpstreamPanelSiteType = 'newapi' | 'sub2api'
export type UpstreamPanelStatus = 'ok' | 'failed' | 'unsupported' | ''

export interface UpstreamPanelCredential {
  account_id: number
  account_name: string
  account_status: string
  site_type: UpstreamPanelSiteType | string
  base_url: string
  username: string
  password_configured: boolean
  enabled: boolean
  last_probe_at?: string
  last_status?: UpstreamPanelStatus | string
  last_error?: string
  last_http_status?: number
  balance?: number
  balance_unit?: string
  balance_detail?: Record<string, unknown>
  updated_at?: string
}

export interface SaveUpstreamPanelCredentialInput {
  site_type: UpstreamPanelSiteType
  base_url: string
  username: string
  password?: string
  enabled: boolean
}

export interface UpstreamPanelProbeSummary {
  total: number
  succeeded: number
  failed: number
  results: Array<{
    account_id: number
    status: string
    error?: string
    balance?: number
    unit?: string
  }>
}

export async function listUpstreamPanelCredentials(): Promise<{ rows: UpstreamPanelCredential[] }> {
  return (await apiClient.get('/admin/account-ops/upstream-balance')).data
}

export async function saveUpstreamPanelCredential(
  accountId: number,
  input: SaveUpstreamPanelCredentialInput
): Promise<UpstreamPanelCredential> {
  return (await apiClient.put(`/admin/account-ops/upstream-balance/accounts/${accountId}`, input)).data
}

export async function deleteUpstreamPanelCredential(accountId: number): Promise<void> {
  await apiClient.delete(`/admin/account-ops/upstream-balance/accounts/${accountId}`)
}

export async function probeUpstreamPanelCredential(accountId: number): Promise<UpstreamPanelCredential> {
  return (await apiClient.post(`/admin/account-ops/upstream-balance/accounts/${accountId}/probe`)).data
}

export async function probeAllUpstreamPanelCredentials(): Promise<UpstreamPanelProbeSummary> {
  return (await apiClient.post('/admin/account-ops/upstream-balance/probe')).data
}