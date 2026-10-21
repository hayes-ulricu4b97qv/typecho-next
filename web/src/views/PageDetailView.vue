<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { ApiError } from '../api'
import { getPage } from '../api/content'
import { formatSiteDate } from '../date'
import { renderMarkdown } from '../markdown'
import { setDocumentMeta } from '../meta'
import { decodePathSegment } from '../path'
import type { Page, Status } from '../types'

const props = defineProps<{ status: Status }>()

const rawSlug = window.location.pathname.match(/^\/page\/(.+)$/)?.[1] ?? ''
const pageSlug = rawSlug ? decodePathSegment(rawSlug) : ''
const page = ref<Page | null>(null)
const loading = ref(true)
const error = ref('')
const progress = ref(0)
let pageLoadGeneration = 0
const renderedContent = computed(() => renderMarkdown(page.value?.content ?? ''))
const readingLabel = computed(() => {
  const text = page.value?.content.replace(/\s+/g, '') ?? ''
  if (!text) return ''
  return `约 ${Math.max(1, Math.ceil(text.length / 450))} 分钟`
})

function updateProgress() {
  const height = document.documentElement.scrollHeight - window.innerHeight
  progress.value = height <= 0 ? 0 : Math.min(100, Math.max(0, (window.scrollY / height) * 100))
}

async function loadPage() {
  const generation = ++pageLoadGeneration
  loading.value = true
  error.value = ''
  if (!pageSlug) {
    error.value = '页面地址无效'
    loading.value = false
    return
  }
  try {
    const loadedPage = await getPage(pageSlug)
    if (generation !== pageLoadGeneration) return
    page.value = loadedPage
    const site = props.status.site
    const title = site?.name ? `${loadedPage.title} · ${site.name}` : loadedPage.title
    setDocumentMeta(title, loadedPage.excerpt || site?.description || '', site?.keywords ?? '')
  } catch (reason) {
    if (generation === pageLoadGeneration) {
      error.value = reason instanceof ApiError ? reason.message : '页面加载失败'
    }
  } finally {
    if (generation === pageLoadGeneration) {
      loading.value = false
      updateProgress()
    }
  }
}

onMounted(() => {
  updateProgress()
  window.addEventListener('scroll', updateProgress, { passive: true })
  void loadPage()
})

onUnmounted(() => {
  pageLoadGeneration++
  window.removeEventListener('scroll', updateProgress)
})
</script>

<template>
  <div v-if="loading" class="empty-state" role="status">正在加载页面…</div>
  <div v-else-if="error" class="empty-state empty-error"><strong role="alert">{{ error }}</strong><button v-if="pageSlug" class="button button-dark" type="button" :disabled="loading" @click="loadPage">重试加载</button><a href="/user" class="button button-dark">返回文章列表</a></div>
  <article v-else-if="page" class="article-detail page-detail">
    <div class="read-progress" aria-hidden="true"><span :style="{ width: progress + '%' }"></span></div>
    <header class="article-head">
      <a class="back-link" href="/user">全部文章</a>
      <p class="eyebrow">页面 · <time :datetime="page.updatedAt">{{ formatSiteDate(page.updatedAt, props.status.site?.timezone || 'UTC') }}</time><template v-if="readingLabel"> · {{ readingLabel }}</template></p>
      <h1>{{ page.title }}</h1>
    </header>
    <p v-if="page.excerpt" class="article-excerpt">{{ page.excerpt }}</p>
    <div class="article-body article-rich" v-html="renderedContent"></div>
  </article>
</template>
