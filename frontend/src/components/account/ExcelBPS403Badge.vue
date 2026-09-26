<template>
  <span
    v-if="disabledAt"
    data-test="excel-bps-403-badge"
    class="mt-1 inline-flex items-center self-start rounded bg-amber-400 px-1.5 py-0.5 text-[11px] font-semibold leading-4 text-amber-950 ring-1 ring-amber-500"
    :title="t('admin.accounts.openai.excelBPS403BadgeTooltip', { time: formatDateTime(disabledAt) })"
  >
    {{ t('admin.accounts.openai.excelBPS403Badge') }}
  </span>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Account } from '@/types'
import { formatDateTime } from '@/utils/format'

const props = defineProps<{
  account: Account
}>()

const { t } = useI18n()

// The backend records this time when a BPS 403 automatically turns the protocol
// off, and clears it when an admin turns the protocol back on.
const disabledAt = computed(() => {
  const { platform, type, extra } = props.account
  if (platform !== 'openai' || type !== 'oauth' || !extra || extra.openai_excel_bps === true) return null
  const value = extra.openai_excel_bps_403_disabled_at
  if (typeof value !== 'string') return null
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? null : date
})
</script>
