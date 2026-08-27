<template>
  <AppLayout>
    <div class="mx-auto w-full max-w-5xl space-y-6 pb-8">
      <header class="page-header rounded-3xl bg-white p-5 shadow-sm ring-1 ring-gray-900/5 dark:bg-dark-800 dark:ring-dark-700 sm:p-6">
        <div class="flex items-start gap-3">
          <span class="inline-flex h-10 w-10 flex-shrink-0 items-center justify-center rounded-2xl bg-primary-50 text-primary-600 dark:bg-primary-900/30 dark:text-primary-300">
            <Icon name="bolt" size="md" />
          </span>
          <div>
            <h1 class="page-title text-xl font-black text-gray-900 dark:text-white">
              {{ t('admin.channelOnboarding.title') }}
            </h1>
            <p class="page-description mt-1.5 text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.channelOnboarding.description') }}
            </p>
          </div>
        </div>
      </header>

      <div class="grid gap-6 lg:grid-cols-[minmax(0,1fr)_280px]">
        <form class="card space-y-5 p-5 sm:p-6" @submit.prevent="submit">
          <div class="grid gap-4 sm:grid-cols-2">
            <div class="sm:col-span-2">
              <label class="input-label" for="channel-onboarding-name">
                {{ t('admin.channelOnboarding.form.name') }} <span class="text-red-500">*</span>
              </label>
              <input
                id="channel-onboarding-name"
                v-model.trim="form.name"
                class="input w-full"
                maxlength="100"
                required
                :placeholder="t('admin.channelOnboarding.form.namePlaceholder')"
              />
            </div>

            <div>
              <label class="input-label" for="channel-onboarding-platform">
                {{ t('admin.channelOnboarding.form.platform') }} <span class="text-red-500">*</span>
              </label>
              <select id="channel-onboarding-platform" v-model="form.platform" class="input w-full" required>
                <option v-for="option in platformOptions" :key="option.value" :value="option.value">
                  {{ option.label }}
                </option>
              </select>
            </div>

            <div>
              <label class="input-label" for="channel-onboarding-rate">
                {{ t('admin.channelOnboarding.form.rateMultiplier') }} <span class="text-red-500">*</span>
              </label>
              <input
                id="channel-onboarding-rate"
                v-model.number="form.rate_multiplier"
                class="input w-full"
                type="number"
                min="0"
                step="any"
                required
              />
              <p class="input-hint">{{ t('admin.channelOnboarding.form.rateMultiplierHint') }}</p>
            </div>

            <div class="sm:col-span-2">
              <label class="input-label" for="channel-onboarding-base-url">
                {{ t('admin.channelOnboarding.form.baseUrl') }} <span class="text-red-500">*</span>
              </label>
              <input
                id="channel-onboarding-base-url"
                v-model.trim="form.upstream_base_url"
                class="input w-full"
                type="url"
                required
                :placeholder="t('admin.channelOnboarding.form.baseUrlPlaceholder')"
              />
            </div>

            <div class="sm:col-span-2">
              <label class="input-label" for="channel-onboarding-api-key">
                {{ t('admin.channelOnboarding.form.apiKey') }} <span class="text-red-500">*</span>
              </label>
              <input
                id="channel-onboarding-api-key"
                v-model="form.upstream_api_key"
                class="input w-full font-mono"
                type="password"
                autocomplete="new-password"
                required
                :placeholder="t('admin.channelOnboarding.form.apiKeyPlaceholder')"
              />
              <p class="input-hint">{{ t('admin.channelOnboarding.form.apiKeyHint') }}</p>
            </div>

            <div>
              <label class="input-label" for="channel-onboarding-model">
                {{ t('admin.channelOnboarding.form.primaryModel') }} <span class="text-red-500">*</span>
              </label>
              <input
                id="channel-onboarding-model"
                v-model.trim="form.primary_model"
                class="input w-full"
                maxlength="200"
                required
                :placeholder="t('admin.channelOnboarding.form.primaryModelPlaceholder')"
              />
            </div>

            <div>
              <label class="input-label" for="channel-onboarding-interval">
                {{ t('admin.channelOnboarding.form.intervalSeconds') }} <span class="text-red-500">*</span>
              </label>
              <input
                id="channel-onboarding-interval"
                v-model.number="form.interval_seconds"
                class="input w-full"
                type="number"
                min="15"
                max="3600"
                step="1"
                required
              />
              <p class="input-hint">{{ t('admin.channelOnboarding.form.intervalSecondsHint') }}</p>
            </div>

            <div class="sm:col-span-2">
              <label class="input-label" for="channel-onboarding-tokens">
                {{ t('admin.channelOnboarding.form.expectedTokens') }}
              </label>
              <input
                id="channel-onboarding-tokens"
                v-model.number="form.expected_input_tokens"
                class="input w-full"
                type="number"
                min="1"
                step="1"
                :placeholder="t('admin.channelOnboarding.form.expectedTokensPlaceholder')"
              />
              <p class="input-hint">{{ t('admin.channelOnboarding.form.expectedTokensHint') }}</p>
            </div>
          </div>

          <div class="flex flex-wrap items-center justify-end gap-3 border-t border-gray-100 pt-5 dark:border-dark-700">
            <button type="button" class="btn btn-secondary" :disabled="submitting" @click="resetForm">
              {{ t('common.reset') }}
            </button>
            <button type="submit" class="btn btn-primary" :disabled="submitting">
              <Icon v-if="submitting" name="refresh" size="sm" class="mr-2 animate-spin" />
              {{ submitting ? t('admin.channelOnboarding.submitting') : t('admin.channelOnboarding.submit') }}
            </button>
          </div>
        </form>

        <aside class="card h-fit p-5 sm:p-6">
          <h2 class="text-sm font-semibold text-gray-900 dark:text-white">
            {{ t('admin.channelOnboarding.autoConfig.title') }}
          </h2>
          <ul class="mt-4 space-y-3 text-sm text-gray-600 dark:text-gray-300">
            <li v-for="item in autoConfigItems" :key="item" class="flex gap-2">
              <Icon name="checkCircle" size="sm" class="mt-0.5 flex-shrink-0 text-emerald-500" />
              <span>{{ item }}</span>
            </li>
          </ul>
        </aside>
      </div>

      <section v-if="result" class="card border border-emerald-200 bg-emerald-50/70 p-5 dark:border-emerald-900/60 dark:bg-emerald-950/20 sm:p-6">
        <div class="flex items-start gap-3">
          <Icon name="checkCircle" size="md" class="mt-0.5 flex-shrink-0 text-emerald-600 dark:text-emerald-400" />
          <div class="min-w-0 flex-1">
            <h2 class="text-base font-semibold text-emerald-900 dark:text-emerald-200">
              {{ t('admin.channelOnboarding.success.title') }}
            </h2>
            <p class="mt-1 text-sm text-emerald-800 dark:text-emerald-300">
              {{ t('admin.channelOnboarding.success.description') }}
            </p>
            <dl class="mt-4 grid gap-3 text-sm sm:grid-cols-2">
              <div v-for="item in resultItems" :key="item.label" class="rounded-xl bg-white/70 p-3 dark:bg-dark-900/40">
                <dt class="text-xs text-emerald-700 dark:text-emerald-400">{{ item.label }}</dt>
                <dd class="mt-1 break-all font-mono text-emerald-950 dark:text-emerald-100">{{ item.value }}</dd>
              </div>
            </dl>
            <p class="mt-4 text-xs text-emerald-800 dark:text-emerald-300">
              {{ t('admin.channelOnboarding.success.keyHint') }}
            </p>
          </div>
        </div>
      </section>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type {
  ChannelOnboardingPlatform,
  ChannelOnboardingResult,
} from '@/api/admin/channelOnboarding'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'

const { t } = useI18n()
const appStore = useAppStore()

const form = reactive<{
  name: string
  platform: ChannelOnboardingPlatform
  rate_multiplier: number
  upstream_base_url: string
  upstream_api_key: string
  primary_model: string
  interval_seconds: number
  expected_input_tokens: number | undefined
}>({
  name: '',
  platform: 'openai',
  rate_multiplier: 1,
  upstream_base_url: '',
  upstream_api_key: '',
  primary_model: '',
  interval_seconds: 900,
  expected_input_tokens: undefined,
})

const submitting = ref(false)
const result = ref<ChannelOnboardingResult | null>(null)

const platformOptions = computed(() => [
  { value: 'openai' as const, label: t('admin.groups.platforms.openai') },
  { value: 'anthropic' as const, label: t('admin.groups.platforms.anthropic') },
  { value: 'gemini' as const, label: t('admin.groups.platforms.gemini') },
  { value: 'grok' as const, label: t('admin.groups.platforms.grok') },
  { value: 'kimi' as const, label: t('admin.groups.platforms.kimi') },
  { value: 'zhipu' as const, label: t('admin.groups.platforms.zhipu') },
  { value: 'deepseek' as const, label: t('admin.groups.platforms.deepseek') },
])

const autoConfigItems = computed(() => [
  t('admin.channelOnboarding.autoConfig.standardGroup'),
  t('admin.channelOnboarding.autoConfig.apiKeyAccount'),
  t('admin.channelOnboarding.autoConfig.adminKey'),
  t('admin.channelOnboarding.autoConfig.monitor'),
])

const resultItems = computed(() => {
  if (!result.value) return []
  return [
    { label: t('admin.channelOnboarding.success.groupId'), value: result.value.group_id },
    { label: t('admin.channelOnboarding.success.accountId'), value: result.value.account_id },
    { label: t('admin.channelOnboarding.success.apiKeyId'), value: result.value.api_key_id },
    { label: t('admin.channelOnboarding.success.monitorId'), value: result.value.monitor_id },
    { label: t('admin.channelOnboarding.success.keyMasked'), value: result.value.api_key_masked },
  ]
})

function resetForm(): void {
  form.name = ''
  form.platform = 'openai'
  form.rate_multiplier = 1
  form.upstream_base_url = ''
  form.upstream_api_key = ''
  form.primary_model = ''
  form.interval_seconds = 900
  form.expected_input_tokens = undefined
  result.value = null
}

async function submit(): Promise<void> {
  submitting.value = true
  try {
    const params = {
      name: form.name,
      platform: form.platform,
      rate_multiplier: form.rate_multiplier,
      upstream_base_url: form.upstream_base_url,
      upstream_api_key: form.upstream_api_key,
      primary_model: form.primary_model,
      interval_seconds: form.interval_seconds,
      ...(form.expected_input_tokens ? { expected_input_tokens: form.expected_input_tokens } : {}),
    }
    result.value = await adminAPI.channelOnboarding.create(params)
    appStore.showSuccess(t('admin.channelOnboarding.success.toast'))
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('admin.channelOnboarding.error')))
  } finally {
    submitting.value = false
  }
}
</script>
