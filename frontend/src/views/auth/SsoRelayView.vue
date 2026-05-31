<script setup lang="ts">
/**
 * SSO 中继页（带生图分组选择）
 *
 * 子产品（画境工坊生图工作台）把浏览器导向 `/sso?redirect_uri=...&state=...`。
 * 本页在已登录状态下（未登录会被路由守卫先导向登录、登录后回到这里）：
 *   1. 拉取当前用户可绑定、且允许生图的分组（/groups/available 中 allow_image_generation=true）
 *   2. 让用户选定一个生图分组
 *   3. 带 group_id 调 /auth/sso/authorize，拿到一次性 code 拼好的回跳地址
 *   4. window.location 跳回子产品；子产品后端用该 code 换“绑定此分组的临时 key”
 *
 * 注意：sub2api 为 header-only JWT 认证，authorize 只能由持 token 的 SPA 调用，
 * 故 SSO 入口必须经过本中继页，不能由子产品直接超链接跳 authorize。
 */
import { onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { apiClient } from '@/api/client'
import { userGroupsAPI } from '@/api/groups'
import type { Group } from '@/types'

const route = useRoute()

const loading = ref(true)
const redirecting = ref(false)
const errorMessage = ref('')
const groups = ref<Group[]>([])
const selectedGroupId = ref<number | null>(null)

const redirectUri = typeof route.query.redirect_uri === 'string' ? route.query.redirect_uri : ''
const state = typeof route.query.state === 'string' ? route.query.state : ''

onMounted(async () => {
  if (!redirectUri) {
    errorMessage.value = '缺少 redirect_uri 参数，无法完成登录跳转。'
    loading.value = false
    return
  }
  try {
    const all = await userGroupsAPI.getAvailable()
    groups.value = all.filter((g) => g.allow_image_generation && g.status === 'active')
    if (groups.value.length === 1) {
      selectedGroupId.value = groups.value[0].id
    }
  } catch (err: unknown) {
    const e = err as { message?: string }
    errorMessage.value = e?.message || '获取分组失败，请稍后重试。'
  } finally {
    loading.value = false
  }
})

async function enter(): Promise<void> {
  if (selectedGroupId.value == null) {
    return
  }
  redirecting.value = true
  errorMessage.value = ''
  try {
    const { data } = await apiClient.get<{ redirect: string }>('/auth/sso/authorize', {
      params: { redirect_uri: redirectUri, state, group_id: selectedGroupId.value },
    })
    if (data?.redirect) {
      window.location.href = data.redirect
    } else {
      errorMessage.value = '授权失败：服务端未返回跳转地址。'
      redirecting.value = false
    }
  } catch (err: unknown) {
    const e = err as { message?: string }
    errorMessage.value = e?.message || '授权失败，请稍后重试。'
    redirecting.value = false
  }
}
</script>

<template>
  <div class="sso-relay">
    <div class="sso-relay__panel">
      <h2 class="sso-relay__title">选择生图分组</h2>
      <p class="sso-relay__subtitle">进入生图工作台后，将使用所选分组的模型与计费，消耗你的账户余额。</p>

      <div v-if="loading" class="sso-relay__hint">
        <span class="sso-relay__spinner" aria-hidden="true"></span>
        正在加载可用分组…
      </div>

      <p v-else-if="errorMessage" class="sso-relay__error">{{ errorMessage }}</p>

      <p v-else-if="groups.length === 0" class="sso-relay__error">
        你的账号下没有可生图的分组，请联系管理员开通。
      </p>

      <ul v-else class="sso-relay__list">
        <li
          v-for="g in groups"
          :key="g.id"
          class="sso-relay__item"
          :class="{ 'is-selected': selectedGroupId === g.id }"
          @click="selectedGroupId = g.id"
        >
          <input
            type="radio"
            name="sso-group"
            :value="g.id"
            :checked="selectedGroupId === g.id"
            @change="selectedGroupId = g.id"
          />
          <div class="sso-relay__item-main">
            <span class="sso-relay__item-name">{{ g.name }}</span>
            <span v-if="g.description" class="sso-relay__item-desc">{{ g.description }}</span>
            <span class="sso-relay__item-price">
              <template v-if="g.image_price_1k != null">普通 {{ g.image_price_1k }} · </template>
              <template v-if="g.image_price_2k != null">2K {{ g.image_price_2k }} · </template>
              <template v-if="g.image_price_4k != null">4K {{ g.image_price_4k }}</template>
            </span>
          </div>
        </li>
      </ul>

      <button
        v-if="!loading && !errorMessage && groups.length > 0"
        class="sso-relay__enter"
        type="button"
        :disabled="selectedGroupId == null || redirecting"
        @click="enter"
      >
        {{ redirecting ? '正在跳转…' : '进入生图工作台' }}
      </button>
    </div>
  </div>
</template>

<style scoped>
.sso-relay {
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 70vh;
  padding: 24px;
}

.sso-relay__panel {
  width: 100%;
  max-width: 440px;
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.sso-relay__title {
  font-size: 20px;
  font-weight: 600;
}

.sso-relay__subtitle {
  font-size: 13px;
  opacity: 0.7;
}

.sso-relay__hint {
  display: flex;
  align-items: center;
  gap: 10px;
  font-size: 14px;
  opacity: 0.85;
  padding: 16px 0;
}

.sso-relay__spinner {
  width: 18px;
  height: 18px;
  border: 2px solid rgba(127, 127, 127, 0.25);
  border-top-color: currentColor;
  border-radius: 50%;
  animation: sso-relay-spin 0.8s linear infinite;
}

@keyframes sso-relay-spin {
  to {
    transform: rotate(360deg);
  }
}

.sso-relay__error {
  font-size: 14px;
  color: #d33;
  padding: 12px 0;
}

.sso-relay__list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.sso-relay__item {
  display: flex;
  align-items: flex-start;
  gap: 10px;
  padding: 12px 14px;
  border: 1px solid rgba(127, 127, 127, 0.25);
  border-radius: 10px;
  cursor: pointer;
  transition: border-color 0.15s, background 0.15s;
}

.sso-relay__item.is-selected {
  border-color: #4f7cff;
  background: rgba(79, 124, 255, 0.06);
}

.sso-relay__item-main {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.sso-relay__item-name {
  font-size: 15px;
  font-weight: 500;
}

.sso-relay__item-desc {
  font-size: 12px;
  opacity: 0.7;
}

.sso-relay__item-price {
  font-size: 12px;
  opacity: 0.6;
}

.sso-relay__enter {
  margin-top: 8px;
  padding: 11px 16px;
  font-size: 15px;
  font-weight: 500;
  color: #fff;
  background: #4f7cff;
  border: none;
  border-radius: 10px;
  cursor: pointer;
}

.sso-relay__enter:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
</style>
