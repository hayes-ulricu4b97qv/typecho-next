<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { ApiError } from '../api'
import { listOutboundMail } from '../api/auth'
import type { OutboundMail } from '../types'

const pageSize = 20
const props = defineProps<{ sessionGeneration: number }>()
const emit = defineEmits<{
  sessionExpired: [requestSession: number]
  accessRevoked: [requestSession: number]
}>()
const page = ref(1)
const total = ref(0)
const items = ref<OutboundMail[]>([])
const error = ref('')
const loading = ref(true)
const retryPage = ref(1)
const pageCount = computed(() => Math.max(1, Math.ceil(total.value / pageSize)))
let loadGeneration = 0

async function load(requestedPage = page.value) {
  const requestSession = props.sessionGeneration
  const generation = ++loadGeneration
  retryPage.value = requestedPage
  loading.value = true
  error.value = ''
  try {
    let result = await listOutboundMail(requestedPage, pageSize)
    if (generation !== loadGeneration) return
    const lastPage = Math.max(1, Math.ceil(result.total / pageSize))
    if (requestedPage > lastPage) {
      result = await listOutboundMail(lastPage, pageSize)
      if (generation !== loadGeneration) return
    }
    items.value = result.items
    total.value = result.total
    page.value = result.page
  } catch (reason) {
    if (generation === loadGeneration) {
      if (reason instanceof ApiError && reason.status === 401) {
        emit('sessionExpired', requestSession)
      } else if (reason instanceof ApiError && reason.status === 403 && reason.code === 'forbidden') {
        items.value = []
        total.value = 0
        page.value = 1
        emit('accessRevoked', requestSession)
      } else {
        error.value = reason instanceof ApiError ? reason.message : '站内通知加载失败'
      }
    }
  } finally {
    if (generation === loadGeneration) loading.value = false
  }
}

async function changePage(next: number) {
  if (loading.value || next < 1 || next > pageCount.value || next === page.value) return
  await load(next)
}

onMounted(() => void load())
</script>

<template>
  <section class="admin-section" id="mail">
    <div class="section-heading">
      <div><p class="eyebrow">通知</p><h2>站内通知</h2></div>
      <span class="section-count">{{ total }} 条</span>
    </div>
    <p class="admin-lead">待审核评论的通知会保存在这里；配置 SMTP 后也会发送到站点邮箱。</p>
    <p v-if="error" class="form-error" role="alert">{{ error }}</p>
    <button v-if="error" class="text-button" type="button" :disabled="loading" @click="load(retryPage)">重试加载</button>
    <div v-if="loading" class="user-state" role="status">正在加载站内通知…</div>
    <div v-else-if="!error && items.length === 0" class="empty-state">还没有站内通知。</div>
    <div v-else-if="!error && items.length > 0" class="admin-comments">
      <article v-for="message in items" :key="message.id" class="admin-mail-row">
        <div class="comment-row-content">
          <div class="comment-row-meta">
            <strong>{{ message.subject }}</strong>
            <span>{{ message.recipient }}</span>
            <time>{{ new Date(message.createdAt).toLocaleString('zh-CN') }}</time>
          </div>
          <p class="mail-body">{{ message.body }}</p>
        </div>
      </article>
    </div>
    <div v-if="total > pageSize" class="pagination-controls">
      <button class="button button-small" type="button" :disabled="loading || page <= 1" @click="changePage(page - 1)">上一页</button>
      <span>第 {{ page }} / {{ pageCount }} 页</span>
      <button class="button button-small" type="button" :disabled="loading || page >= pageCount" @click="changePage(page + 1)">下一页</button>
    </div>
  </section>
</template>
