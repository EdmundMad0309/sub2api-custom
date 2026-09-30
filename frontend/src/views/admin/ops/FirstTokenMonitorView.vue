<template>
  <AppLayout>
    <div class="ftm">
      <SmartOpsNav />
      <header class="page-heading">
        <div>
          <p class="eyebrow">{{ t('accountOps.smartTitle') }}</p>
          <h2>{{ t('firstTokenMonitor.title') }}</h2>
          <p class="subtitle">{{ t('firstTokenMonitor.description') }}</p>
        </div>
        <div class="flex flex-wrap gap-2">
          <button class="btn btn-secondary" :disabled="loading" @click="load()">
            {{ loading ? t('firstTokenMonitor.loading') : t('firstTokenMonitor.refresh') }}
          </button>
          <button class="btn btn-secondary" :disabled="running || loading" @click="runNow">
            {{ running ? t('firstTokenMonitor.running') : t('firstTokenMonitor.runOnce') }}
          </button>
          <button class="btn btn-primary" :disabled="saving" @click="save">
            {{ saving ? t('firstTokenMonitor.saving') : t('firstTokenMonitor.save') }}
          </button>
        </div>
      </header>

      <p v-if="error" role="alert" class="error-banner">{{ error }}</p>
      <p v-if="notice" role="status" class="success-banner">{{ notice }}</p>

      <section class="summary-grid">
        <article class="summary-card">
          <span>{{ t('firstTokenMonitor.status') }}</span>
          <strong>{{ form.enabled ? t('firstTokenMonitor.enabled') : t('firstTokenMonitor.disabled') }}</strong>
          <small>{{ t('firstTokenMonitor.intervalHint', { seconds: form.interval_seconds }) }}</small>
        </article>
        <article class="summary-card">
          <span>{{ t('firstTokenMonitor.groups') }}</span>
          <strong>{{ form.group_ids.length }}</strong>
          <small>{{ t('firstTokenMonitor.groupsHint') }}</small>
        </article>
        <article class="summary-card">
          <span>{{ t('firstTokenMonitor.thresholds') }}</span>
          <strong>{{ formatSeconds(form.open_threshold_ms) }} / {{ formatSeconds(form.close_threshold_ms) }}</strong>
          <small>{{ t('firstTokenMonitor.thresholdsHint') }}</small>
        </article>
        <article class="summary-card">
          <span>{{ t('firstTokenMonitor.lastRun') }}</span>
          <strong>{{ lastRunText }}</strong>
          <small v-if="status.last_error" class="text-red-500">{{ status.last_error }}</small>
          <small v-else>{{ t('firstTokenMonitor.lastRunHint', { count: status.events.length }) }}</small>
        </article>
      </section>

      <nav class="section-tabs" :aria-label="t('firstTokenMonitor.tabs')">
        <button type="button" :class="{ active: section === 'config' }" @click="section = 'config'">{{ t('firstTokenMonitor.tabConfig') }}</button>
        <button type="button" :class="{ active: section === 'accounts' }" @click="section = 'accounts'">{{ t('firstTokenMonitor.tabAccounts') }}</button>
        <button type="button" :class="{ active: section === 'events' }" @click="section = 'events'">{{ t('firstTokenMonitor.tabEvents') }}</button>
      </nav>

      <section v-if="section === 'config'" class="panel-grid">
        <article class="card">
          <h3>{{ t('firstTokenMonitor.ruleTitle') }}</h3>
          <p class="card-hint">{{ t('firstTokenMonitor.ruleHint') }}</p>
          <div class="field-grid">
            <label class="field-label">{{ t('firstTokenMonitor.windowMinutes') }}
              <input v-model.number="form.window_minutes" class="input" type="number" min="1" max="1440" />
              <small class="field-hint">{{ t('firstTokenMonitor.windowHint') }}</small>
            </label>
            <label class="field-label">{{ t('firstTokenMonitor.minSamples') }}
              <input v-model.number="form.min_samples" class="input" type="number" min="1" max="200" />
              <small class="field-hint">{{ t('firstTokenMonitor.minSamplesHint') }}</small>
            </label>
            <label class="field-label">{{ t('firstTokenMonitor.openThreshold') }}
              <input v-model.number="form.open_threshold_ms" class="input" type="number" min="100" max="600000" step="100" />
              <small class="field-hint">{{ t('firstTokenMonitor.openThresholdHint') }}</small>
            </label>
            <label class="field-label">{{ t('firstTokenMonitor.closeThreshold') }}
              <input v-model.number="form.close_threshold_ms" class="input" type="number" min="100" max="600000" step="100" />
              <small class="field-hint">{{ t('firstTokenMonitor.closeThresholdHint') }}</small>
            </label>
            <label class="field-label">{{ t('firstTokenMonitor.intervalSeconds') }}
              <input v-model.number="form.interval_seconds" class="input" type="number" min="30" max="3600" />
              <small class="field-hint">{{ t('firstTokenMonitor.intervalHint', { seconds: form.interval_seconds }) }}</small>
            </label>
            <label class="switch-label">
              <input v-model="form.enabled" type="checkbox" />
              <span>{{ t('firstTokenMonitor.enableMonitor') }}</span>
            </label>
          </div>
        </article>

        <article class="card">
          <h3>{{ t('firstTokenMonitor.groupTitle') }}</h3>
          <p class="card-hint">{{ t('firstTokenMonitor.groupHint') }}</p>
          <input v-model="groupFilter" class="input group-filter" type="search" :placeholder="t('firstTokenMonitor.searchGroups')" />
          <div class="group-list">
            <label v-for="group in filteredGroups" :key="group.id" class="group-item">
              <input type="checkbox" :checked="form.group_ids.includes(group.id)" @change="toggleGroup(group.id, ($event.target as HTMLInputElement).checked)" />
              <span>{{ group.name }}</span>
              <small>#{{ group.id }}</small>
            </label>
            <p v-if="!filteredGroups.length" class="empty-hint">{{ t('firstTokenMonitor.noGroups') }}</p>
          </div>
          <p class="selected-hint">{{ t('firstTokenMonitor.selected', { count: form.group_ids.length }) }}</p>
        </article>
      </section>

      <section v-else-if="section === 'accounts'" class="card">
        <div class="card-heading">
          <h3>{{ t('firstTokenMonitor.accountsTitle') }}</h3>
          <small>{{ t('firstTokenMonitor.accountsHint', { minutes: form.window_minutes }) }}</small>
        </div>
        <div class="table-wrap">
          <table>
            <thead>
              <tr>
                <th>{{ t('firstTokenMonitor.colAccount') }}</th>
                <th>{{ t('firstTokenMonitor.colStatus') }}</th>
                <th>{{ t('firstTokenMonitor.colSchedulable') }}</th>
                <th>{{ t('firstTokenMonitor.colAverage') }}</th>
                <th>{{ t('firstTokenMonitor.colSamples') }}</th>
                <th>{{ t('firstTokenMonitor.colGroups') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="sample in samples" :key="sample.account_id">
                <td>
                  <strong>{{ sample.account_name || ('#' + sample.account_id) }}</strong>
                  <small>#{{ sample.account_id }}</small>
                </td>
                <td>{{ sample.status || '-' }}</td>
                <td>
                  <span class="badge" :class="sample.schedulable ? 'ok' : 'muted'">{{ sample.schedulable ? t('firstTokenMonitor.on') : t('firstTokenMonitor.off') }}</span>
                </td>
                <td :class="averageClass(sample)">
                  <strong class="tabular-nums">{{ sample.samples ? formatSeconds(sample.average_ms) : '-' }}</strong>
                  <small v-if="sample.samples">{{ sample.average_ms.toFixed(0) }} ms</small>
                </td>
                <td class="tabular-nums">{{ sample.samples }}</td>
                <td>
                  <small>{{ sample.group_ids.map((id) => groupName(id)).join(' / ') || '-' }}</small>
                </td>
              </tr>
              <tr v-if="!samples.length">
                <td colspan="6" class="empty-cell">{{ t('firstTokenMonitor.noSamples') }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>

      <section v-else class="card">
        <div class="card-heading">
          <h3>{{ t('firstTokenMonitor.eventsTitle') }}</h3>
          <small>{{ t('firstTokenMonitor.eventsHint') }}</small>
        </div>
        <ul class="event-list">
          <li v-for="(event, index) in status.events" :key="index">
            <span class="badge" :class="event.action === 'scheduling_disabled' ? 'danger' : 'ok'">{{ actionLabel(event.action) }}</span>
            <strong>{{ event.account_name || ('#' + event.account_id) }}</strong>
            <small>{{ formatSeconds(event.average_ms) }} · {{ t('firstTokenMonitor.sampleCount', { count: event.samples }) }} · {{ formatTime(event.at) }}</small>
          </li>
          <li v-if="!status.events.length" class="empty-cell">{{ t('firstTokenMonitor.noEvents') }}</li>
        </ul>
      </section>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import SmartOpsNav from '@/components/admin/operations/SmartOpsNav.vue'
import { getAll as listGroups } from '@/api/admin/groups'
import type { AdminGroup } from '@/types'
import {
  getFirstTokenMonitor,
  runFirstTokenMonitor,
  saveFirstTokenMonitorConfig,
  type FirstTokenMonitorConfig,
  type FirstTokenMonitorSample,
  type FirstTokenMonitorStatus
} from '@/api/admin/accountOpsExtra'

const { t } = useI18n()
const loading = ref(false)
const saving = ref(false)
const running = ref(false)
const error = ref('')
const notice = ref('')
const section = ref<'config' | 'accounts' | 'events'>('config')
const groupFilter = ref('')
const groups = ref<AdminGroup[]>([])
const status = ref<FirstTokenMonitorStatus>({
  config: {
    enabled: false,
    group_ids: [],
    window_minutes: 30,
    min_samples: 3,
    open_threshold_ms: 7000,
    close_threshold_ms: 10000,
    interval_seconds: 120
  },
  running: false,
  events: [],
  samples: []
})
const form = reactive<FirstTokenMonitorConfig>({ ...status.value.config, group_ids: [] })

let timer: ReturnType<typeof setInterval> | undefined

const samples = computed(() => [...status.value.samples].sort((a, b) => a.account_id - b.account_id))
const filteredGroups = computed(() => {
  const keyword = groupFilter.value.trim().toLowerCase()
  if (!keyword) return groups.value
  return groups.value.filter((group) => group.name.toLowerCase().includes(keyword) || String(group.id).includes(keyword))
})
const lastRunText = computed(() => (status.value.last_run_at ? formatTime(status.value.last_run_at) : t('firstTokenMonitor.never')))

function formatSeconds(ms: number): string {
  if (!Number.isFinite(ms) || ms <= 0) return '-'
  return `${(ms / 1000).toFixed(1)}s`
}

function formatTime(value: string): string {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString()
}

function groupName(id: number): string {
  const found = groups.value.find((group) => group.id === id)
  return found ? found.name : `#${id}`
}

function actionLabel(action: string): string {
  return action === 'scheduling_disabled' ? t('firstTokenMonitor.actionDisable') : t('firstTokenMonitor.actionEnable')
}

function averageClass(sample: FirstTokenMonitorSample): string {
  if (!sample.samples) return 'text-gray-400'
  if (sample.average_ms <= form.open_threshold_ms) return 'text-emerald-600'
  if (sample.average_ms > form.close_threshold_ms) return 'text-red-600'
  return 'text-amber-600'
}

function toggleGroup(id: number, checked: boolean): void {
  const next = new Set(form.group_ids)
  if (checked) next.add(id)
  else next.delete(id)
  form.group_ids = [...next].sort((a, b) => a - b)
}

async function load(): Promise<void> {
  loading.value = true
  error.value = ''
  try {
    const [snapshot, groupList] = await Promise.all([getFirstTokenMonitor(), listGroups()])
    status.value = snapshot
    groups.value = groupList
    Object.assign(form, snapshot.config, { group_ids: [...snapshot.config.group_ids] })
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err)
  } finally {
    loading.value = false
  }
}

async function save(): Promise<void> {
  saving.value = true
  error.value = ''
  notice.value = ''
  try {
    await saveFirstTokenMonitorConfig({ ...form, group_ids: [...form.group_ids] })
    notice.value = t('firstTokenMonitor.saved')
    await load()
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err)
  } finally {
    saving.value = false
  }
}

async function runNow(): Promise<void> {
  running.value = true
  error.value = ''
  notice.value = ''
  try {
    const result = await runFirstTokenMonitor()
    notice.value = t('firstTokenMonitor.runDone', { events: result.events.length, samples: result.samples.length })
    await load()
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err)
  } finally {
    running.value = false
  }
}

onMounted(async () => {
  await load()
  timer = setInterval(() => {
    if (form.enabled) void load()
  }, 60000)
})

onBeforeUnmount(() => {
  if (timer) clearInterval(timer)
})
</script>

<style scoped>
.ftm { @apply w-full min-w-0 text-gray-900 dark:text-gray-100; }
.page-heading { @apply mb-6 flex flex-wrap items-center justify-between gap-4; }
.eyebrow { @apply mb-1 text-[11px] font-semibold tracking-widest text-primary-600; }
.page-heading h2 { @apply text-2xl font-semibold tracking-tight; }
.subtitle { @apply mt-2 max-w-3xl text-sm leading-6 text-gray-500 dark:text-gray-400; }
.summary-grid { display:grid; grid-template-columns:repeat(auto-fit,minmax(190px,1fr)); gap:20px; @apply mb-5; }
.summary-card,.card { @apply overflow-hidden rounded-2xl border border-gray-200/80 bg-white p-5 shadow-sm dark:border-dark-700 dark:bg-dark-900; }
.summary-card span { @apply block text-xs font-medium text-gray-400; }
.summary-card strong { @apply mt-2 block text-2xl font-semibold tabular-nums; }
.summary-card small { @apply mt-2 block truncate text-xs text-gray-400; }
.section-tabs { @apply mb-4 flex w-fit gap-1 rounded-xl bg-gray-100 p-1 dark:bg-dark-800; }
.section-tabs button { @apply rounded-lg px-4 py-2 text-xs font-medium text-gray-500 transition-colors dark:text-gray-400; }
.section-tabs button.active { @apply bg-white text-gray-900 shadow-sm dark:bg-dark-700 dark:text-white; }
.panel-grid { display:grid; grid-template-columns:repeat(auto-fit,minmax(340px,1fr)); gap:20px; }
.card h3 { @apply text-base font-semibold; }
.card-hint { @apply mt-1 text-xs leading-5 text-gray-500 dark:text-gray-400; }
.card-heading { @apply flex flex-wrap items-center justify-between gap-2 border-b border-gray-100 pb-3 dark:border-dark-800; }
.card-heading small { @apply text-xs text-gray-400; }
.field-grid { @apply mt-4 grid gap-4; }
.field-label { @apply block text-xs font-medium text-gray-600 dark:text-gray-300; }
.field-label .input { @apply mt-2 block w-full; }
.field-hint { @apply mt-1.5 block text-[11px] font-normal leading-4 text-gray-400; }
.switch-label { @apply flex items-center gap-2 text-sm font-medium; }
.group-filter { @apply mt-3 w-full; }
.group-list { @apply mt-3 max-h-72 space-y-1 overflow-auto overscroll-contain pr-1; }
.group-item { @apply flex items-center gap-2 rounded-lg px-2 py-1.5 text-sm hover:bg-gray-50 dark:hover:bg-dark-800; }
.group-item small { @apply ml-auto text-[11px] text-gray-400; }
.selected-hint,.empty-hint { @apply mt-3 text-xs text-gray-400; }
.table-wrap { max-height:60vh; @apply mt-3 overflow-auto overscroll-contain; }
table { @apply w-full min-w-[760px] text-left text-xs; }
thead { @apply sticky top-0 z-10 bg-white text-gray-400 dark:bg-dark-900; }
th { @apply whitespace-nowrap px-4 py-3 font-medium; }
td { @apply border-b border-gray-100 px-4 py-3 align-top dark:border-dark-800; }
td strong { @apply block max-w-64 truncate text-[13px] font-medium; }
td small { @apply mt-1 block text-[10px] leading-4 text-gray-400; }
.empty-cell { @apply py-10 text-center text-xs text-gray-400; }
.badge { @apply inline-flex rounded-md bg-gray-100 px-2 py-1 text-[11px] font-medium text-gray-600 dark:bg-dark-700 dark:text-gray-300; }
.badge.ok { @apply bg-emerald-50 text-emerald-700 dark:bg-emerald-950/30 dark:text-emerald-300; }
.badge.danger { @apply bg-red-50 text-red-600 dark:bg-red-950/30 dark:text-red-300; }
.event-list { @apply mt-3 divide-y divide-gray-100 dark:divide-dark-800; }
.event-list li { @apply flex flex-wrap items-center gap-3 py-2.5 text-sm; }
.event-list small { @apply text-xs text-gray-400; }
</style>