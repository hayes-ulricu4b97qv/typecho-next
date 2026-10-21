<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { ApiError } from '../api'
import { listPosts, listTerms } from '../api/content'
import { siteDateParts } from '../date'
import type { Post, Term } from '../types'

const props = withDefaults(defineProps<{ pageSize?: number; siteName?: string; siteDescription?: string; siteTimezone?: string }>(), { pageSize: 10, siteName: '', siteDescription: '', siteTimezone: 'UTC' })
const pageSize = computed(() => (props.pageSize >= 1 && props.pageSize <= 50 ? props.pageSize : 10))

const posts = ref<Post[]>([])
const terms = ref<Term[]>([])
const termsPage = ref(0)
const termsTotal = ref(0)
const termsLoading = ref(false)
const termsError = ref(false)
const termsRetryPage = ref(1)
const termsPerBatch = 24
const loading = ref(true)
const error = ref('')
const search = ref('')
const submittedSearch = ref('')
const categoryFilter = ref('')
const tagFilter = ref('')
const monthFilter = ref('')
const page = ref(1)
const total = ref(0)
let postLoadGeneration = 0
let postLoadController: AbortController | null = null

function syncFiltersFromURL() {
  const filters = new URLSearchParams(window.location.search)
  categoryFilter.value = filters.get('category') ?? ''
  tagFilter.value = filters.get('tag') ?? ''
  monthFilter.value = filters.get('month') ?? ''
  const requestedSearch = filters.get('q') ?? ''
  submittedSearch.value = requestedSearch.trim()
  search.value = submittedSearch.value
  if (submittedSearch.value !== requestedSearch) {
    const normalizedURL = new URL(window.location.href)
    if (submittedSearch.value) normalizedURL.searchParams.set('q', submittedSearch.value)
    else normalizedURL.searchParams.delete('q')
    window.history.replaceState(window.history.state, '', `${normalizedURL.pathname}${normalizedURL.search}${normalizedURL.hash}`)
  }
  const requestedPage = filters.get('page') ?? ''
  page.value = /^[1-9]\d*$/.test(requestedPage) && Number.isSafeInteger(Number(requestedPage)) ? Number(requestedPage) : 1
}

function updateFilterURL(replace = false) {
  const query = new URLSearchParams()
  if (categoryFilter.value) query.set('category', categoryFilter.value)
  if (tagFilter.value) query.set('tag', tagFilter.value)
  if (monthFilter.value) query.set('month', monthFilter.value)
  if (submittedSearch.value.trim()) query.set('q', submittedSearch.value.trim())
  if (page.value > 1) query.set('page', String(page.value))
  const suffix = query.toString() ? `?${query.toString()}` : ''
  const method = replace ? 'replaceState' : 'pushState'
  window.history[method]({}, '', `${window.location.pathname}${suffix}`)
}

async function loadPosts() {
  const generation = ++postLoadGeneration
  postLoadController?.abort()
  const controller = new AbortController()
  postLoadController = controller
  const requested = {
    search: submittedSearch.value,
    category: categoryFilter.value,
    tag: tagFilter.value,
    month: monthFilter.value,
    page: page.value,
    pageSize: pageSize.value,
  }
  loading.value = true
  error.value = ''
  try {
    let response = await listPosts(requested, controller.signal)
    if (generation !== postLoadGeneration) return
    let correctedPage: number | undefined
    const lastPage = Math.max(1, Math.ceil(response.total / requested.pageSize))
    if (requested.page > lastPage) {
      correctedPage = lastPage
      response = await listPosts({ ...requested, page: lastPage }, controller.signal)
      if (generation !== postLoadGeneration) return
    }
    posts.value = response.items
    total.value = response.total
    if (correctedPage !== undefined) {
      page.value = correctedPage
      updateFilterURL(true)
    }
  } catch (reason) {
    if (generation === postLoadGeneration) {
      error.value = reason instanceof ApiError ? reason.message : '文章加载失败'
    }
  } finally {
    if (postLoadController === controller) postLoadController = null
    if (generation === postLoadGeneration) loading.value = false
  }
}

async function loadTerms(pageNo = 1) {
  if (termsLoading.value) return
  termsRetryPage.value = pageNo
  termsLoading.value = true
  termsError.value = false
  try {
    const response = await listTerms(false, pageNo, termsPerBatch)
    terms.value = pageNo === 1 ? response.items : [...terms.value, ...response.items]
    termsPage.value = response.page
    termsTotal.value = response.total
  } catch {
    if (pageNo === 1) terms.value = []
    termsError.value = true
  } finally {
    termsLoading.value = false
  }
}

function loadMoreTerms() {
  if (!termsLoading.value && terms.value.length < termsTotal.value) {
    void loadTerms(termsPage.value + 1)
  }
}

function publishedParts(post: Post) {
  const raw = post.publishedAt || post.updatedAt
  const parts = siteDateParts(raw, props.siteTimezone)
  if (!parts) return { iso: raw, day: '--', rest: '' }
  return {
    iso: raw,
    day: parts.day,
    rest: `${parts.year}.${parts.month}`,
  }
}

function submitSearch() {
  submittedSearch.value = search.value.trim()
  search.value = submittedSearch.value
  page.value = 1
  updateFilterURL()
  void loadPosts()
}

function selectTerm(term: Term) {
  if (term.kind === 'category') {
    categoryFilter.value = categoryFilter.value === term.slug ? '' : term.slug
  } else {
    tagFilter.value = tagFilter.value === term.slug ? '' : term.slug
  }
  page.value = 1
  updateFilterURL()
  void loadPosts()
}

function clearTermFilter(kind: 'category' | 'tag') {
  if (kind === 'category') categoryFilter.value = ''
  else tagFilter.value = ''
  page.value = 1
  updateFilterURL()
  void loadPosts()
}

function clearTerms() {
  categoryFilter.value = ''
  tagFilter.value = ''
  page.value = 1
  updateFilterURL()
  void loadPosts()
}

function changePage(nextPage: number) {
  if (nextPage < 1 || nextPage > totalPages.value || nextPage === page.value) return
  page.value = nextPage
  updateFilterURL()
  void loadPosts()
}

function termURL(term: Term) {
  return `/user?${term.kind}=${encodeURIComponent(term.slug)}`
}

function handlePopState() {
  syncFiltersFromURL()
  void loadPosts()
}

const totalPages = computed(() => Math.max(1, Math.ceil(total.value / pageSize.value)))
const categories = computed(() => terms.value.filter((term) => term.kind === 'category'))
const tags = computed(() => terms.value.filter((term) => term.kind === 'tag'))
const heading = computed(() => {
  if (monthFilter.value) return `${monthFilter.value} 的文章`
  if (submittedSearch.value) return `搜索“${submittedSearch.value}”`
  if (categoryFilter.value || tagFilter.value) return '筛选结果'
  return '最新文章'
})
const listEmpty = computed(() => Boolean(submittedSearch.value || categoryFilter.value || tagFilter.value || monthFilter.value))
const activeFilterLabel = computed(() => {
  const parts: string[] = []
  if (submittedSearch.value) parts.push(`“${submittedSearch.value}”`)
  for (const [kind, slug] of [['category', categoryFilter.value], ['tag', tagFilter.value]] as const) {
    if (!slug) continue
    const term = terms.value.find((item) => item.kind === kind && item.slug === slug)
    parts.push(term?.name || slug)
  }
  if (monthFilter.value) parts.push(monthFilter.value)
  return parts.join(' · ')
})

function clearSearch() {
  search.value = ''
  submittedSearch.value = ''
  page.value = 1
  updateFilterURL()
  void loadPosts()
}

function clearAllFilters() {
  search.value = ''
  submittedSearch.value = ''
  categoryFilter.value = ''
  tagFilter.value = ''
  monthFilter.value = ''
  page.value = 1
  updateFilterURL()
  void loadPosts()
}

function blurSearch(event: KeyboardEvent) {
  if (event.key !== 'Escape') return
  const target = event.target
  if (target instanceof HTMLInputElement) target.blur()
}

function onKeydown(event: KeyboardEvent) {
  const target = event.target
  const typing = target instanceof HTMLElement && (target.tagName === 'INPUT' || target.tagName === 'TEXTAREA' || target.isContentEditable)
  if (event.key === '/' && !typing && !event.metaKey && !event.ctrlKey && !event.altKey) {
    event.preventDefault()
    document.getElementById('reader-search')?.focus()
  }
}

onMounted(async () => {
  syncFiltersFromURL()
  window.addEventListener('popstate', handlePopState)
  window.addEventListener('keydown', onKeydown)
  await Promise.all([loadPosts(), loadTerms()])
})

onUnmounted(() => {
  postLoadGeneration++
  postLoadController?.abort()
  postLoadController = null
  window.removeEventListener('popstate', handlePopState)
  window.removeEventListener('keydown', onKeydown)
})
</script>

<template>
  <header class="list-mast">
    <p class="eyebrow">文章</p>
    <h1>{{ siteName || '博客' }}</h1>
    <p v-if="siteDescription">{{ siteDescription }}</p>
    <form class="search-bar" @submit.prevent="submitSearch">
      <input id="reader-search" v-model="search" type="search" maxlength="200" placeholder="搜索标题或正文" aria-label="搜索文章" @keydown="blurSearch" />
      <button class="button button-primary" type="submit">搜索</button>
      <button v-if="submittedSearch" class="button" type="button" @click="clearSearch">清除</button>
    </form>
  </header>
  <div v-if="terms.length || termsError" class="term-filter-bar" role="group" aria-label="按分类或标签筛选">
    <button class="term-filter" type="button" :class="{ active: !categoryFilter && !tagFilter }" @click="clearTerms">全部</button>
    <details v-if="categories.length" class="term-group">
      <summary>分类 <small>{{ categories.length }}</small></summary>
      <div class="term-group-options">
        <a v-for="term in categories" :key="term.id" class="term-filter" :class="{ active: categoryFilter === term.slug }" :aria-current="categoryFilter === term.slug ? 'true' : undefined" :href="termURL(term)" @click.prevent="selectTerm(term)">{{ term.name }} <small>{{ term.postCount }}</small></a>
      </div>
    </details>
    <details v-if="tags.length" class="term-group">
      <summary>标签 <small>{{ tags.length }}</small></summary>
      <div class="term-group-options">
        <a v-for="term in tags" :key="term.id" class="term-filter" :class="{ active: tagFilter === term.slug }" :aria-current="tagFilter === term.slug ? 'true' : undefined" :href="termURL(term)" @click.prevent="selectTerm(term)">{{ term.name }} <small>{{ term.postCount }}</small></a>
      </div>
    </details>
    <div v-if="termsError || terms.length < termsTotal" class="term-load-control">
      <span v-if="termsError" role="alert">分类和标签加载失败</span>
      <span v-else-if="termsLoading" role="status">正在加载分类和标签…</span>
      <span v-else>已载入 {{ terms.length }} / {{ termsTotal }} 个筛选</span>
      <button class="text-button" type="button" :disabled="termsLoading" @click="termsError ? loadTerms(termsRetryPage) : loadMoreTerms()">{{ termsLoading ? '加载中…' : termsError ? '重试' : '加载更多' }}</button>
    </div>
  </div>
  <div v-if="activeFilterLabel" class="month-filter-bar">
    <span>当前筛选 {{ activeFilterLabel }}</span>
    <div class="month-filter-actions">
      <button v-if="categoryFilter" class="text-button" type="button" @click="clearTermFilter('category')">取消分类</button>
      <button v-if="tagFilter" class="text-button" type="button" @click="clearTermFilter('tag')">取消标签</button>
      <button class="text-button" type="button" @click="clearAllFilters">查看全部</button>
    </div>
  </div>
  <div class="content-heading">
    <div><h2>{{ heading }}</h2><span v-if="!loading && !error">共 {{ total }} 篇</span></div>
  </div>

  <div v-if="loading" class="empty-state" role="status">正在加载文章…</div>
  <div v-else-if="error" class="empty-state empty-error">
    <strong role="alert">{{ error }}</strong>
    <button class="button button-dark" type="button" @click="loadPosts">重试加载</button>
  </div>
  <div v-else-if="posts.length === 0" class="empty-state">
    <strong>{{ listEmpty ? '没有匹配的文章' : '还没有公开文章' }}</strong>
    <span>{{ listEmpty ? '换一个词，或清掉当前筛选。' : '登录管理端，写下第一篇吧。' }}</span>
    <button v-if="listEmpty" class="button button-dark" type="button" @click="clearAllFilters">清除筛选</button>
    <a v-else href="/admin" class="button button-dark">进入管理端</a>
  </div>
  <template v-else>
    <div class="post-list">
      <article v-for="post in posts" :key="post.id" class="post-card">
        <time :datetime="publishedParts(post).iso"><span class="post-day">{{ publishedParts(post).day }}</span><span class="post-month">{{ publishedParts(post).rest }}</span></time>
        <div class="post-card-main">
          <h3><a :href="`/user/${post.slug}`">{{ post.title }}</a></h3>
          <div v-if="post.categories?.length || post.tags?.length" class="post-terms"><a v-for="term in [...(post.categories ?? []), ...(post.tags ?? [])]" :key="term.id" :href="termURL(term)">{{ term.name }}</a></div>
          <p v-if="post.excerpt">{{ post.excerpt }}</p>
        </div>
        <a class="post-open" :href="`/user/${post.slug}`">阅读</a>
      </article>
    </div>
    <div v-if="totalPages > 1" class="pagination"><button class="text-button" type="button" :disabled="page === 1" @click="changePage(page - 1)">上一页</button><span>{{ page }} / {{ totalPages }}</span><button class="text-button" type="button" :disabled="page === totalPages" @click="changePage(page + 1)">下一页</button></div>
  </template>
</template>
