<template>
  <BaseDialog
    :show="show"
    :title="created ? t('providerHall.createKey.doneTitle') : t('providerHall.createKey.title')"
    width="normal"
    @close="handleClose"
  >
    <!-- 创建成功：把完整 Key 亮出来。这是唯一一次能完整复制的机会。 -->
    <div v-if="created" class="space-y-4">
      <p class="text-sm text-gray-600 dark:text-dark-300">
        {{ t('providerHall.createKey.doneHint') }}
      </p>
      <div class="flex items-center gap-2 rounded-lg border border-gray-200 bg-gray-50 p-3 dark:border-dark-600 dark:bg-dark-900/40">
        <code class="min-w-0 flex-1 break-all font-mono text-xs text-gray-900 dark:text-gray-100">
          {{ created.key }}
        </code>
        <button type="button" class="btn btn-secondary btn-sm shrink-0" @click="copyKey">
          {{ t('common.copy') }}
        </button>
      </div>
    </div>

    <!-- 创建表单 -->
    <form v-else id="hall-create-key-form" class="space-y-5" @submit.prevent="handleSubmit">
      <div>
        <label class="input-label">{{ t('providerHall.createKey.groupLabel') }}</label>
        <div class="mt-1">
          <GroupBadge
            v-if="group"
            :name="group.name"
            :platform="group.platform"
            :subscription-type="group.subscription_type"
            :rate-multiplier="group.rate_multiplier"
          />
          <span v-else class="text-sm text-gray-500 dark:text-dark-400">{{ groupName }}</span>
        </div>
        <p v-if="!group" class="mt-1 text-xs text-amber-600 dark:text-amber-400">
          {{ t('providerHall.createKey.groupUnavailable') }}
        </p>
      </div>

      <div>
        <label class="input-label">
          {{ t('providerHall.createKey.nameLabel') }}
          <span class="text-red-500">*</span>
        </label>
        <input
          v-model="name"
          type="text"
          required
          maxlength="100"
          class="input"
          :placeholder="t('providerHall.createKey.namePlaceholder')"
        />
      </div>

      <div class="flex items-start justify-between gap-4">
        <div class="min-w-0">
          <p class="text-sm font-medium text-gray-900 dark:text-white">
            {{ t('providerHall.createKey.expiration') }}
          </p>
          <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">
            {{ t('providerHall.createKey.expirationHint') }}
          </p>
        </div>
        <Toggle v-model="enableExpiration" />
      </div>
      <div v-if="enableExpiration">
        <input
          v-model.number="expiresInDays"
          type="number"
          min="1"
          max="3650"
          class="input"
          :placeholder="t('providerHall.createKey.expiresInDaysPlaceholder')"
        />
      </div>
    </form>

    <template #footer>
      <div class="flex justify-end gap-3">
        <button type="button" class="btn btn-secondary" @click="handleClose">
          {{ created ? t('common.close') : t('common.cancel') }}
        </button>
        <button
          v-if="!created"
          type="submit"
          form="hall-create-key-form"
          class="btn btn-primary"
          :disabled="submitting || !group"
        >
          {{ submitting ? t('common.saving') : t('providerHall.createKey.submit') }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
/**
 * 大厅里就地创建 API Key。
 *
 * 刻意不复用 KeysView 的创建弹窗：那个表单同时承担创建与编辑，
 * 带 IP 限制、配额、多档限速等十几个字段，抽出来要动上游文件的大半，
 * rebase 冲突面太大。这里只做「为选定分组开一把 Key」这一件事，
 * 高级配置去 API Key 页调。
 */
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { useClipboard } from '@/composables/useClipboard'
import { extractApiErrorMessage } from '@/utils/apiError'
import { keysAPI, userGroupsAPI } from '@/api'
import type { ApiKey, Group } from '@/types'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Toggle from '@/components/common/Toggle.vue'
import GroupBadge from '@/components/common/GroupBadge.vue'

const props = defineProps<{
  show: boolean
  groupId: number | null
  /** 分组名兜底：用户无权访问该分组时，至少让他知道自己点的是哪个。 */
  groupName: string
}>()

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'created'): void
}>()

const { t } = useI18n()
const appStore = useAppStore()
const { copyToClipboard } = useClipboard()

const name = ref('')
const enableExpiration = ref(false)
const expiresInDays = ref<number | null>(null)
const submitting = ref(false)
const created = ref<ApiKey | null>(null)

const groups = ref<Group[]>([])
let groupsLoaded = false

/**
 * 只在用户「可用分组」列表里查。查不到说明他无权用这个分组
 * （专属分组、订阅未生效），此时禁用提交，而不是让后端报错。
 */
const group = computed(() => groups.value.find((g) => g.id === props.groupId) ?? null)

async function ensureGroups() {
  if (groupsLoaded) return
  try {
    groups.value = await userGroupsAPI.getAvailable()
    groupsLoaded = true
  } catch {
    groups.value = []
  }
}

watch(
  () => props.show,
  (show) => {
    if (!show) return
    void ensureGroups()
    // 每次打开都重置，避免上一次的成功态或输入残留。
    created.value = null
    name.value = defaultKeyName()
    enableExpiration.value = false
    expiresInDays.value = null
  },
)

function defaultKeyName(): string {
  return props.groupName ? `${props.groupName}` : ''
}

async function handleSubmit() {
  if (submitting.value || !group.value) return
  const trimmed = name.value.trim()
  if (!trimmed) {
    appStore.showError(t('providerHall.createKey.nameRequired'))
    return
  }

  const days = enableExpiration.value && expiresInDays.value && expiresInDays.value > 0
    ? expiresInDays.value
    : undefined

  submitting.value = true
  try {
    created.value = await keysAPI.create(
      trimmed,
      props.groupId,
      undefined,
      undefined,
      undefined,
      undefined,
      days,
    )
    appStore.showSuccess(t('providerHall.createKey.success'))
    emit('created')
  } catch (err: unknown) {
    appStore.showError(extractApiErrorMessage(err, t('providerHall.createKey.failed')))
  } finally {
    submitting.value = false
  }
}

async function copyKey() {
  if (!created.value) return
  await copyToClipboard(created.value.key, t('providerHall.createKey.copied'))
}

function handleClose() {
  emit('close')
}
</script>
