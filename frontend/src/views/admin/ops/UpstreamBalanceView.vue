<template>
  <AppLayout>
    <div class="upb">
      <SmartOpsNav />
      <header class="page-heading">
        <div>
          <p class="eyebrow">{{ t('accountOps.smartTitle') }}</p>
          <h2>{{ t('upstreamBalance.title') }}</h2>
          <p class="subtitle">{{ t('upstreamBalance.description') }}</p>
        </div>
        <div class="flex flex-wrap gap-2">
          <button class="btn btn-secondary" :disabled="loading" @click="load()">
            {{ loading ? t('upstreamBalance.loading') : t('upstreamBalance.refresh') }}
          </button>
          <button class="btn btn-secondary" :disabled="probingAll || loading" @click="probeAll">
            {{ probingAll ? t('upstreamBalance.probingAll') : t('upstreamBalance.probeAll') }}
          </button>
          <button class="btn btn-primary" @click="openCreate">{{ t('upstreamBalance.addCredential') }}</button>
        </div>
      </header>

      <p v-if="error" role="alert" class="error-banner">{{ error }}</p>
      <p v-if="notice" role="status" class="success-banner">{{ notice }}</p>

      <CredentialEncryptionSetup class="mb-5" @ready="encryptionReady = $event" />
      <p v-if="!encryptionReady" class="hint-banner">{{ t('upstreamBalance.encryptionRequired') }}</p>

      <section class="summary-grid">
        <article class="summary-card">
          <span>{{ t('upstreamBalance.summaryConfigured') }}</span>
          <strong>{{ rows.length }}</strong>
          <small>{{ t('upstreamBalance.summaryConfiguredHint') }}</small>
        </article>
        <article class="summary-card">
          <span>{{ t('upstreamBalance.summaryOK') }}</span>
          <strong>{{ okCount }}</strong>
          <small>{{ t('upstreamBalance.summaryOKHint') }}</small>
        </article>
        <article class="summary-card">
          <span>{{ t('upstreamBalance.summaryFailed') }}</span>
          <strong>{{ failedCount }}</strong>
          <small>{{ t('upstreamBalance.summaryFailedHint') }}</small>
        </article>
        <article class="summary-card">
          <span>{{ t('upstreamBalance.summaryTotal') }}</span>
          <strong>{{ formatMoney(totalBalance) }}</strong>
          <small>{{ t('upstreamBalance.summaryTotalHint', { count: balanceCount }) }}</small>
        </article>
      </section>

      <section class="card">
        <div class="card-heading">
          <h3>{{ t('upstreamBalance.tableTitle') }}</h3>
          <small>{{ t('upstreamBalance.tableHint') }}</small>
        </div>
        <div class="table-wrap">
          <table>
            <thead>
              <tr>
                <th>{{ t('upstreamBalance.colAccount') }}</th>
                <th>{{ t('upstreamBalance.colSiteType') }}</th>
                <th>{{ t('upstreamBalance.colBaseURL') }}</th>
                <th>{{ t('upstreamBalance.colUsername') }}</th>
                <th>{{ t('upstreamBalance.colPassword') }}</th>
                <th>{{ t('upstreamBalance.colEnabled') }}</th>
                <th>{{ t('upstreamBalance.colBalance') }}</th>
                <th>{{ t('upstreamBalance.colStatus') }}</th>
                <th>{{ t('upstreamBalance.colActions') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="row in rows" :key="row.account_id">
                <td>
                  <strong>{{ row.account_name || ('#' + row.account_id) }}</strong>
                  <small>#{{ row.account_id }} · {{ row.account_status || '-' }}</small>
                </td>
                <td><span class="badge">{{ siteTypeLabel(row.site_type) }}</span></td>
                <td><small class="break-all">{{ row.base_url }}</small></td>
                <td><small class="break-all">{{ row.username }}</small></td>
                <td>
                  <span class="badge" :class="row.password_configured ? 'ok' : 'danger'">
                    {{ row.password_configured ? t('upstreamBalance.passwordSaved') : t('upstreamBalance.passwordMissing') }}
                  </span>
                </td>
                <td>
                  <span class="badge" :class="row.enabled ? 'ok' : 'muted'">
                    {{ row.enabled ? t('upstreamBalance.enabled') : t('upstreamBalance.disabled') }}
                  </span>
                </td>
                <td class="tabular-nums">
                  <strong>{{ formatBalance(row) }}</strong>
                  <small v-if="detailHint(row)">{{ detailHint(row) }}</small>
                </td>
                <td>
                  <span class="badge" :class="statusClass(row.last_status)">{{ statusLabel(row.last_status) }}</span>
                  <small v-if="row.last_probe_at">{{ formatTime(row.last_probe_at) }}</small>
                  <small v-if="row.last_error" class="text-red-500">{{ row.last_error }}</small>
                </td>
                <td>
                  <div class="actions">
                    <button class="link-btn" :disabled="probingId === row.account_id || !row.password_configured" @click="probe(row)">
                      {{ probingId === row.account_id ? t('upstreamBalance.probing') : t('upstreamBalance.probe') }}
                    </button>
                    <button class="link-btn" @click="openEdit(row)">{{ t('upstreamBalance.edit') }}</button>
                    <button class="link-btn danger-text" @click="remove(row)">{{ t('upstreamBalance.delete') }}</button>
                  </div>
                </td>
              </tr>
              <tr v-if="!rows.length">
                <td colspan="9" class="empty-cell">{{ t('upstreamBalance.empty') }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>

      <BaseDialog :show="editorOpen" :title="editorTitle" width="normal" @close="closeEditor">
        <form class="editor-form" @submit.prevent="save">
          <label class="field-label">
            {{ t('upstreamBalance.fieldAccount') }}
            <select v-model.number="form.account_id" class="input" :disabled="editingExisting">
              <option :value="0" disabled>{{ t('upstreamBalance.fieldAccountPlaceholder') }}</option>
              <option v-for="account in accounts" :key="account.id" :value="account.id">
                {{ account.name }} (#{{ account.id }})
              </option>
            </select>
            <small class="field-hint">{{ t('upstreamBalance.fieldAccountHint') }}</small>
          </label>

          <label class="field-label">
            {{ t('upstreamBalance.fieldSiteType') }}
            <select v-model="form.site_type" class="input">
              <option value="newapi">NewAPI</option>
              <option value="sub2api">Sub2API</option>
            </select>
            <small class="field-hint">{{ t('upstreamBalance.fieldSiteTypeHint') }}</small>
          </label>

          <label class="field-label">
            {{ t('upstreamBalance.fieldBaseURL') }}
            <input v-model.trim="form.base_url" class="input" type="url" placeholder="https://relay.example.com" required />
            <small class="field-hint">{{ siteTypeHint }}</small>
          </label>

          <label class="field-label">
            {{ t('upstreamBalance.fieldUsername') }}
            <input v-model.trim="form.username" class="input" type="text" autocomplete="off" required />
            <small class="field-hint">{{ form.site_type === 'sub2api' ? t('upstreamBalance.fieldUsernameSub2API') : t('upstreamBalance.fieldUsernameNewAPI') }}</small>
          </label>

          <label class="field-label">
            {{ t('upstreamBalance.fieldPassword') }}
            <input v-model="form.password" class="input" type="password" autocomplete="new-password" :required="!editingExisting" />
            <small class="field-hint">{{ editingExisting ? t('upstreamBalance.fieldPasswordKeep') : t('upstreamBalance.fieldPasswordHint') }}</small>
          </label>

          <label class="switch-label">
            <input v-model="form.enabled" type="checkbox" />
            <span>{{ t('upstreamBalance.fieldEnabled') }}</span>
          </label>

          <p v-if="editorError" class="error-inline">{{ editorError }}</p>

          <footer class="dialog-actions">
            <button type="button" class="btn btn-secondary" @click="closeEditor">{{ t('upstreamBalance.cancel') }}</button>
            <button type="submit" class="btn btn-primary" :disabled="saving || !encryptionReady">
              {{ saving ? t('upstreamBalance.saving') : t('upstreamBalance.save') }}
            </button>
          </footer>
        </form>
      </BaseDialog>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import SmartOpsNav from '@/components/admin/operations/SmartOpsNav.vue'
import CredentialEncryptionSetup from '@/components/account/CredentialEncryptionSetup.vue'
import { BaseDialog } from '@/components/common'
import { list as listAccounts } from '@/api/admin/accounts'
import type { AccountListItem } from '@/types'
import {
  deleteUpstreamPanelCredential,
  listUpstreamPanelCredentials,
  probeAllUpstreamPanelCredentials,
  probeUpstreamPanelCredential,
  saveUpstreamPanelCredential,
  type UpstreamPanelCredential,
  type UpstreamPanelSiteType
} from '@/api/admin/accountOpsUpstreamBalance'

const { t } = useI18n()
const loading = ref(false)
const saving = ref(false)
const probingAll = ref(false)
const probingId = ref<number | null>(null)
const error = ref('')
const notice = ref('')
const editorError = ref('')
const editorOpen = ref(false)
const editingExisting = ref(false)
const encryptionReady = ref(true)
const rows = ref<UpstreamPanelCredential[]>([])
const accounts = ref<AccountListItem[]>([])

const form = reactive({
  account_id: 0,
  site_type: 'newapi' as UpstreamPanelSiteType,
  base_url: '',
  username: '',
  password: '',
  enabled: true
})

const okCount = computed(() => rows.value.filter((row) => row.last_status === 'ok').length)
const failedCount = computed(() => rows.value.filter((row) => row.last_status && row.last_status !== 'ok').length)
const balanceRows = computed(() => rows.value.filter((row) => typeof row.balance === 'number'))
const balanceCount = computed(() => balanceRows.value.length)
const totalBalance = computed(() => balanceRows.value.reduce((sum, row) => sum + (row.balance ?? 0), 0))
const editorTitle = computed(() => (editingExisting.value ? t('upstreamBalance.editTitle') : t('upstreamBalance.createTitle')))
const siteTypeHint = computed(() =>
  form.site_type === 'sub2api' ? t('upstreamBalance.fieldBaseURLSub2API') : t('upstreamBalance.fieldBaseURLNewAPI')
)

function siteTypeLabel(siteType: string): string {
  return siteType === 'sub2api' ? 'Sub2API' : siteType === 'newapi' ? 'NewAPI' : siteType
}

function statusLabel(status?: string): string {
  switch (status) {
    case 'ok':
      return t('upstreamBalance.statusOK')
    case 'failed':
      return t('upstreamBalance.statusFailed')
    case 'unsupported':
      return t('upstreamBalance.statusUnsupported')
    default:
      return t('upstreamBalance.statusNever')
  }
}

function statusClass(status?: string): string {
  switch (status) {
    case 'ok':
      return 'ok'
    case 'failed':
      return 'danger'
    case 'unsupported':
      return 'warn'
    default:
      return 'muted'
  }
}

function formatMoney(value: number): string {
  if (!Number.isFinite(value)) return '-'
  return `$${value.toFixed(2)}`
}

function formatBalance(row: UpstreamPanelCredential): string {
  if (typeof row.balance !== 'number') return '-'
  const unit = row.balance_unit || 'USD'
  if (unit === 'quota') return `${row.balance.toLocaleString()} quota`
  return `${unit === 'USD' ? '$' : ''}${row.balance.toFixed(4)}${unit === 'USD' ? '' : ` ${unit}`}`
}

function detailHint(row: UpstreamPanelCredential): string {
  const detail = row.balance_detail
  if (!detail) return ''
  const quota = detail.quota
  const used = detail.used_quota
  const frozen = detail.frozen_balance
  const parts: string[] = []
  if (typeof quota === 'number') parts.push(`${t('upstreamBalance.detailQuota')}: ${quota.toLocaleString()}`)
  if (typeof used === 'number') parts.push(`${t('upstreamBalance.detailUsed')}: ${used.toLocaleString()}`)
  if (typeof frozen === 'number') parts.push(`${t('upstreamBalance.detailFrozen')}: ${frozen}`)
  return parts.join(' · ')
}

function formatTime(value: string): string {
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString()
}

async function load(): Promise<void> {
  loading.value = true
  error.value = ''
  try {
    const response = await listUpstreamPanelCredentials()
    rows.value = response.rows ?? []
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err)
  } finally {
    loading.value = false
  }
}

async function loadAccounts(): Promise<void> {
  try {
    const response = await listAccounts(1, 200)
    accounts.value = response.items
  } catch {
    accounts.value = []
  }
}

function openCreate(): void {
  editingExisting.value = false
  editorError.value = ''
  form.account_id = 0
  form.site_type = 'newapi'
  form.base_url = ''
  form.username = ''
  form.password = ''
  form.enabled = true
  editorOpen.value = true
  void loadAccounts()
}

function openEdit(row: UpstreamPanelCredential): void {
  editingExisting.value = true
  editorError.value = ''
  form.account_id = row.account_id
  form.site_type = (row.site_type === 'sub2api' ? 'sub2api' : 'newapi') as UpstreamPanelSiteType
  form.base_url = row.base_url
  form.username = row.username
  form.password = ''
  form.enabled = row.enabled
  editorOpen.value = true
}

function closeEditor(): void {
  editorOpen.value = false
}

async function save(): Promise<void> {
  saving.value = true
  editorError.value = ''
  try {
    await saveUpstreamPanelCredential(form.account_id, {
      site_type: form.site_type,
      base_url: form.base_url,
      username: form.username,
      password: form.password || undefined,
      enabled: form.enabled
    })
    notice.value = t('upstreamBalance.saved')
    editorOpen.value = false
    await load()
  } catch (err) {
    editorError.value = err instanceof Error ? err.message : String(err)
  } finally {
    saving.value = false
  }
}

async function probe(row: UpstreamPanelCredential): Promise<void> {
  probingId.value = row.account_id
  error.value = ''
  notice.value = ''
  try {
    await probeUpstreamPanelCredential(row.account_id)
    await load()
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err)
  } finally {
    probingId.value = null
  }
}

async function probeAll(): Promise<void> {
  probingAll.value = true
  error.value = ''
  notice.value = ''
  try {
    const summary = await probeAllUpstreamPanelCredentials()
    notice.value = t('upstreamBalance.probeAllDone', { total: summary.total, succeeded: summary.succeeded, failed: summary.failed })
    await load()
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err)
  } finally {
    probingAll.value = false
  }
}

async function remove(row: UpstreamPanelCredential): Promise<void> {
  if (!window.confirm(t('upstreamBalance.confirmDelete', { name: row.account_name || `#${row.account_id}` }))) return
  error.value = ''
  try {
    await deleteUpstreamPanelCredential(row.account_id)
    await load()
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err)
  }
}

onMounted(() => {
  void load()
})
</script>

<style scoped>
.upb { @apply w-full min-w-0 text-gray-900 dark:text-gray-100; }
.page-heading { @apply mb-6 flex flex-wrap items-center justify-between gap-4; }
.eyebrow { @apply mb-1 text-[11px] font-semibold tracking-widest text-primary-600; }
.page-heading h2 { @apply text-2xl font-semibold tracking-tight; }
.subtitle { @apply mt-2 max-w-3xl text-sm leading-6 text-gray-500 dark:text-gray-400; }
.hint-banner { @apply mb-5 rounded-xl border border-amber-200 bg-amber-50 px-4 py-3 text-xs text-amber-700 dark:border-amber-900 dark:bg-amber-950/30 dark:text-amber-300; }
.summary-grid { display:grid; grid-template-columns:repeat(auto-fit,minmax(190px,1fr)); gap:20px; @apply mb-5; }
.summary-card,.card { @apply overflow-hidden rounded-2xl border border-gray-200/80 bg-white p-5 shadow-sm dark:border-dark-700 dark:bg-dark-900; }
.summary-card span { @apply block text-xs font-medium text-gray-400; }
.summary-card strong { @apply mt-2 block text-2xl font-semibold tabular-nums; }
.summary-card small { @apply mt-2 block truncate text-xs text-gray-400; }
.card-heading { @apply flex flex-wrap items-center justify-between gap-2 border-b border-gray-100 pb-3 dark:border-dark-800; }
.card-heading h3 { @apply text-base font-semibold; }
.card-heading small { @apply text-xs text-gray-400; }
.table-wrap { max-height:65vh; @apply mt-3 overflow-auto overscroll-contain; }
table { @apply w-full min-w-[1180px] text-left text-xs; }
thead { @apply sticky top-0 z-10 bg-white text-gray-400 dark:bg-dark-900; }
th { @apply whitespace-nowrap px-4 py-3 font-medium; }
td { @apply border-b border-gray-100 px-4 py-4 align-top dark:border-dark-800; }
td strong { @apply block max-w-56 truncate text-[13px] font-medium; }
td small { @apply mt-1.5 block max-w-64 text-[10px] leading-4 text-gray-400; }
.empty-cell { @apply py-10 text-center text-xs text-gray-400; }
.badge { @apply inline-flex rounded-md bg-gray-100 px-2 py-1 text-[11px] font-medium text-gray-600 dark:bg-dark-700 dark:text-gray-300; }
.badge.ok { @apply bg-emerald-50 text-emerald-700 dark:bg-emerald-950/30 dark:text-emerald-300; }
.badge.danger { @apply bg-red-50 text-red-600 dark:bg-red-950/30 dark:text-red-300; }
.badge.warn { @apply bg-amber-50 text-amber-700 dark:bg-amber-950/30 dark:text-amber-300; }
.actions { @apply flex max-w-64 flex-wrap gap-1.5; }
.link-btn { @apply rounded-lg border border-gray-200 px-2.5 py-1 text-[11px] font-medium text-primary-600 transition-colors hover:bg-primary-50 disabled:cursor-not-allowed disabled:opacity-40 dark:border-dark-600 dark:hover:bg-dark-800; }
.danger-text { @apply text-red-600 hover:bg-red-50 dark:hover:bg-red-950/30; }
.editor-form { @apply space-y-4; }
.field-label { @apply block text-xs font-medium text-gray-600 dark:text-gray-300; }
.field-label .input { @apply mt-2 block w-full; }
.field-hint { @apply mt-1.5 block text-[11px] font-normal leading-4 text-gray-400; }
.switch-label { @apply flex items-center gap-2 text-sm font-medium; }
.error-inline { @apply text-xs text-red-500; }
.dialog-actions { @apply flex justify-end gap-2 pt-2; }
</style>