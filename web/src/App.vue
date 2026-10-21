<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { ApiError } from './api'
import { getStatus } from './api/auth'
import type { Status } from './types'
import AdminView from './views/AdminView.vue'
import SettingView from './views/SettingView.vue'
import UserView from './views/UserView.vue'

const status = ref<Status | null>(null)
const loading = ref(true)
const error = ref('')
const currentPath = ref(window.location.pathname)

const page = computed(() => {
  if (currentPath.value === '/admin' || currentPath.value.startsWith('/admin/')) return 'admin'
  if (currentPath.value === '/setting' || currentPath.value.startsWith('/setting/')) return 'setting'
  return 'user'
})

function refreshPath() {
  currentPath.value = window.location.pathname
}

function reload() {
  window.location.reload()
}

onMounted(async () => {
  window.addEventListener('popstate', refreshPath)
  try {
    status.value = await getStatus()
    if (!status.value.initialized && page.value !== 'setting') {
      window.location.replace('/setting')
    }
  } catch (reason) {
    error.value = reason instanceof ApiError ? reason.message : '无法连接到后端服务'
  } finally {
    loading.value = false
  }
})
</script>

<template>
  <div v-if="loading" class="app-loading">
    <div class="mark">笔</div>
    <span role="status">正在铺开这一页…</span>
  </div>
  <div v-else-if="error" class="app-loading">
    <div class="mark mark-error">!</div>
    <h1>服务暂时不可用</h1>
    <p role="alert">{{ error }}</p>
    <button class="button" @click="reload">重新连接</button>
  </div>
  <template v-else>
    <SettingView v-if="page === 'setting'" :status="status!" />
    <AdminView v-else-if="page === 'admin'" :status="status!" />
    <UserView v-else :status="status!" />
  </template>
</template>
