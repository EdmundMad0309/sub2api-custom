<template>
  <AppLayout>
    <div class="brc">
      <SmartOpsNav />
      <header class="page-heading">
        <div>
          <p class="eyebrow">{{ t('accountOps.smartTitle') }}</p>
          <h2>{{ t('billingReconcile.title') }}</h2>
          <p class="subtitle">{{ t('billingReconcile.description') }}</p>
        </div>
        <div class="flex flex-wrap items-center gap-2">
          <input v-model="day" class="input day-input" type="date" :max="today" />
          <button class="btn btn-secondary" :disabled="loading" @click="load()">
            {{ loading ? t('billingReconcile.loading') : t('billingReconcile.refresh') }}
          </button>
          <button class="btn btn-primary" :disabled="running || loading" @click="collectNow">
            {{ running ? t('billingReconcile.collecting') : t('billingReconcile.collect') }}
          </button>
        </div>
      </header>

      <p v-if="error" role="alert" class="error-banner">{{ error }}</p>
      <p v-if="notice" role="status" class="success-banner">{{ notice }}</p>

      <section class="summary-grid">
        <article class="summary-card">
          <span>{{ t('billingReconcile.day') }}</span>
          <strong>{{ status.day || '-' }}</strong>
          <small>{{ t('billingReconcile.dayHint') }}</small>
        </article>
        <article class="summary-card">
          <span>{{ t('billingReconcile.platformCost') }}</span>
          <strong>${{ formatMoney(status.total_platform_cost) }}</strong>
          <small>{{ t('billingReconcile.platformCostHint') }}</small>
        </article>
        <article class="summary-card">
          <span>{{ t('billingReconcile.platformCharged') }}</span>
          <strong>${{ formatMoney(status.total_platform_charged) }}</strong>
          <small>{{ t('billingReconcile.platformChargedHint') }}</small>
        </article>
        <article class="summary-card">
          <span>{{ t('billingReconcile.upstreamSpent') }}</span>
          <strong>${{ formatMoney(status.total_upstream_spent) }}</strong>
          <small>{{ t('billingReconcile.upstreamSpentHint', { count: status.upstream_spent_accounts }) }}</small>
        </article>
      </section>

      <section class="card">
        <div class="card-heading">
          <h3>{{ t('billingReconcile.tableTitle') }}</h3>
          <small v-if="status.last_run_at">{{ t('billingReconcile.lastRun', { time: formatTime(status.last_run_at) }) }}</small>
        </div>
        <p v-if="status.last_error" class="error-inline">{{ status.last_error }}</p>
        <div class="table-wrap">
          <table>
            <thead>
              <tr>
                <th>{{ t('billingReconcile.colAccount') }}</th>
                <th>{{ t('billingReconcile.colStatus') }}</th>
                <th>{{ t('billingReconcile.colRequests') }}</th>
                <th>{{ t('billingReconcile.colTokens') }}</th>
                <th>{{ t('billingReconcile.colPlatformCost') }}</th>
                <th>{{ t('billingReconcile.colPlatformCharged') }}</th>
                <th>{{ t('billingReconcile.colUpstreamBalance') }}</th>
                <th>{{ t('billingReconcile.colUpstreamSpent') }}</th>
                <th>{{ t('billingReconcile.colDifference') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="row in status.rows" :key="row.account_id">
                <td>
                  <strong>{{ row.account_name || ('#' + row.account_id) }}</strong>
                  <small>#{{ row.account_id }}</small>
                </td>
                <td>{{ row.status || '-' }}</td>
                <td class="tabular-nums">{{ row.requests }}</td>
                <td class="tabular-nums">{{ formatTokens(row.total_tokens) }}</td>
                <td class="tabular-nums">${{ formatMoney(row.platform_cost) }}</td>
                <td class="tabular-nums">${{ formatMoney(row.platform_charged) }}</td>
                <td class="tabular-nums">
                  <template v-if="row.upstream_balance !== undefined && row.upstream_balance !== null">
                    {{ row.upstream_currency || '' }}{{ formatMoney(row.upstream_balance) }}
                  </template>
                  <template v-else>-</template>
                  <small v-if="row.upstream_probed_at">{{ formatTime(row.upstream_probed_at) }}</small>
                </td>
                <td class="tabular-nums">
                  <template v-if="row.upstream_spent !== undefined && row.upstream_spent !== null">
                    {{ row.upstream_currency || '' }}{{ formatMoney(row.upstream_spent) }}
                  </template>
                  <template v-else>-</template>
                </td>
                <td class="tabular-nums" :class="differenceClass(row.difference)">
                  <strong>{{ formatDifference(row.difference) }}</strong>
                  <small>{{ t('billingReconcile.differenceHint') }}</small>
                </td>
              </tr>
              <tr v-if="!status.rows.length">
                <td colspan="9" class="empty-cell">{{ t('billingReconcile.empty') }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import SmartOpsNav from '@/components/admin/operations/SmartOpsNav.vue'
import {
  getBillingReconcile,
  runBillingReconcile,
  type BillingReconcileStatus
} from '@/api/admin/accountOpsExtra'

const { t } = useI18n()
const loading = ref(false)
const running = ref(false)
const error = ref('')
const notice = ref('')
const day = ref('')
const today = new Date().toISOString().slice(0, 10)
const status = ref<BillingReconcileStatus>({
  day: '',
  rows: [],
  total_platform_cost: 0,
  total_platform_charged: 0,
  total_upstream_spent: 0,
  upstream_spent_accounts: 0
})

function formatMoney(value: number): string {
  if (!Number.isFinite(value)) return '-'
  return value.toFixed(4)
}

function formatTokens(value: number): string {
  if (!Number.isFinite(value)) return '-'
  if (Math.abs(value) >= 1_000_000) return `${(value / 1_000_000).toFixed(2)}M`
  if (Math.abs(value) >= 1_000) return `${(value / 1_000).toFixed(1)}K`
  return String(value)
}

function formatTime(value: string): string {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString()
}

function formatDifference(value?: number | null): string {
  if (value === undefined || value === null || !Number.isFinite(value)) return '-'
  const sign = value > 0 ? '+' : ''
  return `${sign}${value.toFixed(4)}`
}

function differenceClass(value?: number | null): string {
  if (value === undefined || value === null || !Number.isFinite(value)) return 'text-gray-400'
  if (Math.abs(value) < 0.01) return 'text-gray-500'
  return value > 0 ? 'text-red-600' : 'text-emerald-600'
}

async function load(): Promise<void> {
  loading.value = true
  error.value = ''
  try {
    status.value = await getBillingReconcile(day.value || undefined)
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err)
  } finally {
    loading.value = false
  }
}

async function collectNow(): Promise<void> {
  running.value = true
  error.value = ''
  notice.value = ''
  try {
    const result = await runBillingReconcile(day.value || undefined)
    notice.value = t('billingReconcile.collected', { day: result.day, count: result.rows.length })
    await load()
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err)
  } finally {
    running.value = false
  }
}

onMounted(() => {
  void load()
})
</script>

<style scoped>
.brc { @apply w-full min-w-0 text-gray-900 dark:text-gray-100; }
.page-heading { @apply mb-6 flex flex-wrap items-center justify-between gap-4; }
.eyebrow { @apply mb-1 text-[11px] font-semibold tracking-widest text-primary-600; }
.page-heading h2 { @apply text-2xl font-semibold tracking-tight; }
.subtitle { @apply mt-2 max-w-3xl text-sm leading-6 text-gray-500 dark:text-gray-400; }
.day-input { @apply w-40; }
.summary-grid { display:grid; grid-template-columns:repeat(auto-fit,minmax(190px,1fr)); gap:20px; @apply mb-5; }
.summary-card,.card { @apply overflow-hidden rounded-2xl border border-gray-200/80 bg-white p-5 shadow-sm dark:border-dark-700 dark:bg-dark-900; }
.summary-card span { @apply block text-xs font-medium text-gray-400; }
.summary-card strong { @apply mt-2 block text-2xl font-semibold tabular-nums; }
.summary-card small { @apply mt-2 block truncate text-xs text-gray-400; }
.card-heading { @apply flex flex-wrap items-center justify-between gap-2 border-b border-gray-100 pb-3 dark:border-dark-800; }
.card-heading h3 { @apply text-base font-semibold; }
.card-heading small { @apply text-xs text-gray-400; }
.error-inline { @apply mt-3 text-xs text-red-500; }
.table-wrap { max-height:65vh; @apply mt-3 overflow-auto overscroll-contain; }
table { @apply w-full min-w-[1080px] text-left text-xs; }
thead { @apply sticky top-0 z-10 bg-white text-gray-400 dark:bg-dark-900; }
th { @apply whitespace-nowrap px-4 py-3 font-medium; }
td { @apply border-b border-gray-100 px-4 py-4 align-top dark:border-dark-800; }
td strong { @apply block max-w-56 truncate text-[13px] font-medium; }
td small { @apply mt-1.5 block text-[10px] leading-4 text-gray-400; }
.empty-cell { @apply py-10 text-center text-xs text-gray-400; }
</style>