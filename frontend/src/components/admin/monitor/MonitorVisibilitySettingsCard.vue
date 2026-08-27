<template>
  <section
    class="card overflow-hidden !rounded-3xl !border-0 shadow-sm ring-1 ring-gray-900/5 dark:!bg-dark-800 dark:ring-dark-700"
  >
    <div class="card-header flex flex-wrap items-center justify-between gap-3 !py-3">
      <div>
        <h2 class="text-sm font-semibold text-gray-900 dark:text-white">
          {{ t('channelMonitorV2.settings.displayTitle') }}
        </h2>
        <p class="mt-0.5 text-xs text-gray-500 dark:text-dark-400">
          {{ t('channelMonitorV2.settings.failureThresholdHint') }}
        </p>
      </div>
      <button
        type="button"
        class="btn btn-primary btn-sm"
        :disabled="loading || saving || !dirty"
        @click="save"
      >
        <Icon name="check" size="sm" />
        {{ t('channelMonitorV2.settings.save') }}
      </button>
    </div>

    <div class="flex flex-wrap items-center justify-between gap-3 px-5 py-4">
      <label class="input-label" for="channel-monitor-legacy-failure-threshold">
        {{ t('channelMonitorV2.settings.failureThreshold') }}
      </label>
      <input
        id="channel-monitor-legacy-failure-threshold"
        v-model.number="failureThreshold"
        class="input w-28"
        type="number"
        min="1"
        max="100"
        step="1"
        :disabled="loading || saving"
      />
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import { adminAPI } from '@/api/admin'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'

const { t } = useI18n()
const appStore = useAppStore()
const loading = ref(true)
const saving = ref(false)
const failureThreshold = ref(3)
const originalFailureThreshold = ref(3)

const dirty = computed(() => normalizeFailureThreshold(failureThreshold.value) !== originalFailureThreshold.value)

function normalizeFailureThreshold(value: unknown): number {
  const parsed = Number(value)
  if (!Number.isFinite(parsed)) return 3
  return Math.max(1, Math.min(100, Math.trunc(parsed)))
}

async function load() {
  loading.value = true
  try {
    const settings = await adminAPI.settings.getSettings()
    const normalized = normalizeFailureThreshold(settings.channel_monitor_failure_threshold)
    failureThreshold.value = normalized
    originalFailureThreshold.value = normalized
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('channelMonitorV2.settings.displayLoadFailed')))
  } finally {
    loading.value = false
  }
}

async function save() {
  const normalized = normalizeFailureThreshold(failureThreshold.value)
  saving.value = true
  try {
    await adminAPI.settings.updateSettings({ channel_monitor_failure_threshold: normalized })
    failureThreshold.value = normalized
    originalFailureThreshold.value = normalized
    appStore.showSuccess(t('channelMonitorV2.settings.displaySaveSuccess'))
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('channelMonitorV2.settings.displaySaveFailed')))
  } finally {
    saving.value = false
  }
}

onMounted(load)
</script>
