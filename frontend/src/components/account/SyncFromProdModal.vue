<template>
  <BaseDialog
    :show="show"
    :title="t('admin.accounts.syncFromProdTitle')"
    width="normal"
    close-on-click-outside
    @close="handleClose"
  >
    <!-- Step 1: Preview & select -->
    <div v-if="currentStep === 'preview'" class="space-y-4">
      <div class="text-sm text-gray-600 dark:text-dark-300">
        {{ t('admin.accounts.syncFromProdDesc') }}
      </div>
      <div
        class="rounded-lg bg-gray-50 p-3 text-xs text-gray-500 dark:bg-dark-700/60 dark:text-dark-400"
      >
        {{ t('admin.accounts.prodUpdateBehaviorNote') }}
      </div>

      <!-- Loading -->
      <div
        v-if="previewing"
        class="rounded-lg bg-gray-50 p-4 text-center text-sm text-gray-500 dark:bg-dark-700/60 dark:text-dark-400"
      >
        {{ t('admin.accounts.prodPreviewing') }}
      </div>

      <template v-else-if="previewResult">
        <!-- Candidate accounts (selectable) -->
        <div v-if="previewResult.candidates.length">
          <div class="mb-2 flex items-center justify-between">
            <div class="text-sm font-medium text-gray-900 dark:text-white">
              {{ t('admin.accounts.prodCandidates') }}
              <span class="ml-1 text-xs text-gray-400">({{ previewResult.candidates.length }})</span>
            </div>
            <div class="flex gap-2">
              <button
                type="button"
                class="text-xs text-blue-600 hover:text-blue-700 dark:text-blue-400"
                @click="selectAll"
              >{{ t('admin.accounts.crsSelectAll') }}</button>
              <button
                type="button"
                class="text-xs text-gray-500 hover:text-gray-600 dark:text-gray-400"
                @click="selectNone"
              >{{ t('admin.accounts.crsSelectNone') }}</button>
            </div>
          </div>
          <div
            class="max-h-64 overflow-auto rounded-lg border border-gray-200 p-2 dark:border-dark-600"
          >
            <label
              v-for="acc in previewResult.candidates"
              :key="acc.source_id"
              class="flex cursor-pointer items-center gap-2 rounded px-2 py-1.5 hover:bg-gray-50 dark:hover:bg-dark-700/40"
            >
              <input
                type="checkbox"
                :checked="selectedIds.has(acc.source_id)"
                class="rounded border-gray-300 dark:border-dark-600"
                @change="toggleSelect(acc.source_id)"
              />
              <span
                class="inline-block rounded bg-green-100 px-1.5 py-0.5 text-[10px] font-medium text-green-700 dark:bg-green-900/30 dark:text-green-400"
              >{{ acc.platform }} / {{ acc.type }}</span>
              <span class="truncate text-sm text-gray-700 dark:text-dark-300">{{ acc.name }}</span>
              <span
                v-if="acc.groups.length"
                class="truncate text-xs text-gray-400 dark:text-dark-500"
              >{{ acc.groups.join(', ') }}</span>
              <span
                v-if="acc.has_proxy"
                class="ml-auto inline-block rounded bg-blue-100 px-1.5 py-0.5 text-[10px] font-medium text-blue-700 dark:bg-blue-900/30 dark:text-blue-400"
              >{{ t('admin.accounts.syncProxies') }}</span>
            </label>
          </div>
          <div class="mt-1 text-xs text-gray-400">
            {{ t('admin.accounts.crsSelectedCount', { count: selectedIds.size }) }}
          </div>
        </div>

        <!-- No candidates -->
        <div
          v-else
          class="rounded-lg bg-gray-50 p-4 text-center text-sm text-gray-500 dark:bg-dark-700/60 dark:text-dark-400"
        >
          {{ t('admin.accounts.prodNoCandidates') }}
        </div>
      </template>
    </div>

    <!-- Step 2: Result -->
    <div v-else-if="currentStep === 'result' && result" class="space-y-4">
      <div class="space-y-2 rounded-xl border border-gray-200 p-4 dark:border-dark-700">
        <div class="text-sm font-medium text-gray-900 dark:text-white">
          {{ t('admin.accounts.syncResult') }}
        </div>
        <div class="text-sm text-gray-700 dark:text-dark-300">
          {{ t('admin.accounts.prodSyncResultSummary', summaryParams) }}
        </div>

        <div v-if="result.failed.length" class="mt-2">
          <div class="text-sm font-medium text-red-600 dark:text-red-400">
            {{ t('admin.accounts.syncErrors') }}
          </div>
          <div
            class="mt-2 max-h-48 overflow-auto rounded-lg bg-gray-50 p-3 font-mono text-xs dark:bg-dark-800"
          >
            <div v-for="(item, idx) in result.failed" :key="idx" class="whitespace-pre-wrap">
              #{{ item.source_id }} — {{ item.reason }}
            </div>
          </div>
        </div>
      </div>
    </div>

    <template #footer>
      <div class="flex justify-end gap-3">
        <!-- Step 1: Preview -->
        <template v-if="currentStep === 'preview'">
          <button
            class="btn btn-secondary"
            type="button"
            :disabled="syncing"
            @click="handleClose"
          >
            {{ t('common.cancel') }}
          </button>
          <button
            class="btn btn-primary"
            type="button"
            :disabled="previewing || syncing || noneSelected"
            @click="handleSync"
          >
            {{ syncing ? t('admin.accounts.syncing') : t('admin.accounts.syncNow') }}
          </button>
        </template>

        <!-- Step 2: Result -->
        <template v-else-if="currentStep === 'result'">
          <button class="btn btn-secondary" type="button" @click="handleClose">
            {{ t('common.close') }}
          </button>
        </template>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import { useAppStore } from '@/stores/app'
import { adminAPI } from '@/api/admin'
import type { PreviewFromProdResult, ImportFromProdResult } from '@/api/admin/accounts'

interface Props {
  show: boolean
}

interface Emits {
  (e: 'close'): void
  (e: 'synced'): void
}

const props = defineProps<Props>()
const emit = defineEmits<Emits>()

const { t } = useI18n()
const appStore = useAppStore()

type Step = 'preview' | 'result'
const currentStep = ref<Step>('preview')
const previewing = ref(false)
const syncing = ref(false)
const previewResult = ref<PreviewFromProdResult | null>(null)
const selectedIds = ref(new Set<number>())
const result = ref<ImportFromProdResult | null>(null)

const noneSelected = computed(() => selectedIds.value.size === 0)

const summaryParams = computed(() => ({
  created: result.value?.created_accounts ?? 0,
  groups: result.value?.created_groups ?? 0,
  proxies: result.value?.created_proxies ?? 0,
  skipped: result.value?.skipped ?? 0,
  failed: result.value?.failed.length ?? 0
}))

watch(
  () => props.show,
  (open) => {
    if (open) {
      currentStep.value = 'preview'
      previewResult.value = null
      selectedIds.value = new Set()
      result.value = null
      void handlePreview()
    }
  }
)

const handleClose = () => {
  if (syncing.value || previewing.value) {
    return
  }
  emit('close')
}

const selectAll = () => {
  if (!previewResult.value) return
  selectedIds.value = new Set(previewResult.value.candidates.map((a) => a.source_id))
}

const selectNone = () => {
  selectedIds.value = new Set()
}

const toggleSelect = (id: number) => {
  const s = new Set(selectedIds.value)
  if (s.has(id)) {
    s.delete(id)
  } else {
    s.add(id)
  }
  selectedIds.value = s
}

const handlePreview = async () => {
  previewing.value = true
  try {
    const res = await adminAPI.accounts.previewFromProd()
    previewResult.value = res
    // 默认全选所有候选账号
    selectedIds.value = new Set(res.candidates.map((a) => a.source_id))
  } catch (error: any) {
    appStore.showError(error?.message || t('admin.accounts.prodPreviewFailed'))
    emit('close')
  } finally {
    previewing.value = false
  }
}

const handleSync = async () => {
  if (selectedIds.value.size === 0) return

  syncing.value = true
  try {
    const res = await adminAPI.accounts.syncFromProd([...selectedIds.value])
    result.value = res
    currentStep.value = 'result'

    if (res.failed.length > 0) {
      appStore.showError(t('admin.accounts.prodSyncCompletedWithErrors', summaryParams.value))
    } else {
      appStore.showSuccess(t('admin.accounts.prodSyncCompleted', summaryParams.value))
    }
    emit('synced')
  } catch (error: any) {
    appStore.showError(error?.message || t('admin.accounts.syncFailed'))
  } finally {
    syncing.value = false
  }
}
</script>
