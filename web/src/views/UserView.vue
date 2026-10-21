<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref } from 'vue'
import { listArchive, listPages, listPosts, listRecentComments } from '../api/content'
import { setDocumentMeta } from '../meta'
import { decodePathSegment } from '../path'
import PostDetailView from './PostDetailView.vue'
import PostListView from './PostListView.vue'
import PageDetailView from './PageDetailView.vue'
import type { Comment, MonthCount, Page, Post, Status } from '../types'

const props = defineProps<{ status: Status }>()

const rawPost = window.location.pathname.match(/^\/user\/(.+)$/)?.[1] ?? ''
const postSlug = rawPost ? decodePathSegment(rawPost) : ''
const rawPage = window.location.pathname.match(/^\/page\/(.+)$/)?.[1] ?? ''
const pageSlug = rawPage ? decodePathSegment(rawPage) : ''
const pages = ref<Page[]>([])
const pagesPage = ref(0)
const pagesTotal = ref(0)
const pagesLoading = ref(false)
const pagesError = ref(false)
const pagesRetryPage = ref(1)
const pagesDisclosure = ref<HTMLDetailsElement | null>(null)
const pagesPerBatch = 20
const archive = ref<MonthCount[]>([])
const archiveLoading = ref(true)
const archiveError = ref(false)
const recentPosts = ref<Post[]>([])
const recentPostsLoading = ref(true)
const recentPostsError = ref(false)
const recentComments = ref<Comment[]>([])
const recentCommentsLoading = ref(true)
const recentCommentsError = ref(false)
const menuOpen = ref(false)
const menuToggle = ref<HTMLButtonElement | null>(null)
const menuRail = ref<HTMLElement | null>(null)
const showTop = ref(false)
const brandMark = computed(() => props.status.site?.name?.trim().slice(0, 1) || '博')

function monthLabel(month: string) {
  const [year, value] = month.split('-')
  return `${year}年${Number(value)}月`
}

function closeMenu() {
  menuOpen.value = false
}

function menuFocusableElements() {
  return Array.from(menuRail.value?.querySelectorAll<HTMLElement>(
    'a[href], button:not(:disabled), input:not([type="hidden"]):not(:disabled), select:not(:disabled), textarea:not(:disabled), [tabindex]:not([tabindex="-1"])',
  ) ?? []).filter((element) => element.tabIndex >= 0 && element.getClientRects().length > 0)
}

async function openMenu() {
  menuOpen.value = true
  await nextTick()
  ;(menuFocusableElements()[0] ?? menuRail.value)?.focus({ preventScroll: true })
}

async function closeMenuAndRestoreFocus() {
  closeMenu()
  await nextTick()
  menuToggle.value?.focus({ preventScroll: true })
}

function toggleMenu() {
  if (menuOpen.value) {
    void closeMenuAndRestoreFocus()
  } else {
    void openMenu()
  }
}

function onResize() {
  if (menuOpen.value && window.innerWidth > 860) closeMenu()
}

function onScroll() {
  showTop.value = window.scrollY > 520
}

function scrollTop() {
  const behavior = window.matchMedia('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth'
  window.scrollTo({ top: 0, behavior })
}

function onKeydown(event: KeyboardEvent) {
  if (event.key === 'Tab' && menuOpen.value && window.innerWidth <= 860) {
    const rail = menuRail.value
    const focusable = menuFocusableElements()
    if (!rail || focusable.length === 0) {
      event.preventDefault()
      rail?.focus({ preventScroll: true })
      return
    }

    const first = focusable[0]
    const last = focusable[focusable.length - 1]
    const active = document.activeElement
    if (!rail.contains(active) || active === rail) {
      event.preventDefault()
      ;(event.shiftKey ? last : first).focus({ preventScroll: true })
    } else if (event.shiftKey && active === first) {
      event.preventDefault()
      last.focus({ preventScroll: true })
    } else if (!event.shiftKey && active === last) {
      event.preventDefault()
      first.focus({ preventScroll: true })
    }
  }

  if (event.key === 'Escape') {
    if (menuOpen.value) {
      void closeMenuAndRestoreFocus()
      event.preventDefault()
    }
    if (pagesDisclosure.value) pagesDisclosure.value.open = false
  }
}

async function loadPages(pageNo: number) {
  if (pagesLoading.value) return
  pagesRetryPage.value = pageNo
  pagesLoading.value = true
  pagesError.value = false
  try {
    const response = await listPages(false, pageNo, pagesPerBatch)
    pages.value = pageNo === 1 ? response.items : [...pages.value, ...response.items]
    pagesPage.value = response.page
    pagesTotal.value = response.total
  } catch {
    pagesError.value = true
  } finally {
    pagesLoading.value = false
  }
}

function loadMorePages() {
  if (!pagesLoading.value && pages.value.length < pagesTotal.value) {
    void loadPages(pagesPage.value + 1)
  }
}

async function loadArchive() {
  archiveLoading.value = true
  archiveError.value = false
  try {
    archive.value = (await listArchive()).items
  } catch {
    archiveError.value = true
  } finally {
    archiveLoading.value = false
  }
}

async function loadRecentPosts() {
  recentPostsLoading.value = true
  recentPostsError.value = false
  try {
    recentPosts.value = (await listPosts({ order: 'published', page: 1, pageSize: 6 })).items
  } catch {
    recentPostsError.value = true
  } finally {
    recentPostsLoading.value = false
  }
}

async function loadRecentComments() {
  recentCommentsLoading.value = true
  recentCommentsError.value = false
  try {
    recentComments.value = (await listRecentComments(1, 6)).items
  } catch {
    recentCommentsError.value = true
  } finally {
    recentCommentsLoading.value = false
  }
}

onMounted(() => {
  window.addEventListener('scroll', onScroll, { passive: true })
  window.addEventListener('keydown', onKeydown)
  window.addEventListener('resize', onResize)
  if (!postSlug && !pageSlug) {
    const site = props.status.site
    setDocumentMeta(site?.name || '博客', site?.description ?? '', site?.keywords ?? '')
  }
  void loadPages(1)
  void loadArchive()
  void loadRecentPosts()
  void loadRecentComments()
})

onUnmounted(() => {
  window.removeEventListener('scroll', onScroll)
  window.removeEventListener('keydown', onKeydown)
  window.removeEventListener('resize', onResize)
})
</script>

<template>
  <div class="public-shell" :class="{ 'menu-open': menuOpen }">
    <a class="skip-link" href="#reader-main" :inert="menuOpen">跳到正文</a>
    <header class="public-bar" :inert="menuOpen">
      <button ref="menuToggle" class="nav-toggle" type="button" :aria-expanded="menuOpen" aria-controls="reader-rail" @click="toggleMenu">{{ menuOpen ? '关闭目录' : '目录' }}</button>
      <a class="brand" href="/user">
        <span class="brand-mark">{{ brandMark }}</span>
        <span class="brand-copy">
          <span class="brand-name">{{ status.site?.name ?? '博客' }}</span>
          <span v-if="status.site?.description && (postSlug || pageSlug)" class="brand-tagline">{{ status.site.description }}</span>
        </span>
      </a>
      <nav class="site-nav" aria-label="站点" @click="closeMenu">
        <a href="/user" :class="{ active: !postSlug && !pageSlug }">文章</a>
        <details v-if="pages.length || pagesLoading || pagesError" ref="pagesDisclosure" class="site-pages">
          <summary>页面 <small>{{ pages.length }} / {{ pagesTotal }}</small></summary>
          <div class="site-pages-panel">
            <a v-for="page in pages" :key="page.id" :href="`/page/${page.slug}`" :class="{ active: pageSlug === page.slug }">{{ page.title }}</a>
            <p v-if="pagesLoading" class="site-pages-status" role="status">正在加载页面…</p>
            <p v-else-if="pagesError" class="site-pages-status" role="alert">页面加载失败，请重试。</p>
            <button v-if="pagesError || pages.length < pagesTotal" type="button" :disabled="pagesLoading" @click="pagesError ? loadPages(pagesRetryPage) : loadMorePages()">{{ pagesLoading ? '加载中…' : pagesError ? '重试' : `加载更多页面 · ${pages.length} / ${pagesTotal}` }}</button>
          </div>
        </details>
        <a href="/admin">管理</a>
      </nav>
    </header>
    <div v-if="menuOpen" class="rail-backdrop" aria-hidden="true" @click="closeMenuAndRestoreFocus"></div>
    <div class="public-frame">
      <aside ref="menuRail" id="reader-rail" class="reader-rail" :class="{ 'is-open': menuOpen }" :role="menuOpen ? 'dialog' : undefined" :aria-modal="menuOpen ? 'true' : undefined" aria-label="归档与最新动态" tabindex="-1">
        <button class="reader-menu-close" type="button" @click="closeMenuAndRestoreFocus">关闭目录</button>
        <section aria-label="月份归档">
          <h2>归档 <span>{{ archiveLoading ? '…' : archiveError ? '—' : archive.length }}</span></h2>
          <a v-for="item in archive" :key="item.month" :href="`/user?month=${item.month}`" @click="closeMenu"><span>{{ monthLabel(item.month) }}</span><small>{{ item.count }}</small></a>
          <p v-if="archiveLoading" class="rail-empty" role="status">正在加载归档…</p>
          <template v-else-if="archiveError">
            <p class="rail-empty" role="alert">归档加载失败</p>
            <button class="text-button" type="button" :disabled="archiveLoading" @click="loadArchive">重试</button>
          </template>
          <p v-else-if="archive.length === 0" class="rail-empty">还没有已发布文章</p>
        </section>
        <section aria-label="最新文章">
          <h2>最新文章 <span>{{ recentPostsLoading ? '…' : recentPostsError ? '—' : recentPosts.length }}</span></h2>
          <a v-for="post in recentPosts" :key="post.id" :href="`/user/${post.slug}`" @click="closeMenu"><span>{{ post.title }}</span></a>
          <p v-if="recentPostsLoading" class="rail-empty" role="status">正在加载文章…</p>
          <template v-else-if="recentPostsError">
            <p class="rail-empty" role="alert">最新文章加载失败</p>
            <button class="text-button" type="button" :disabled="recentPostsLoading" @click="loadRecentPosts">重试</button>
          </template>
          <p v-else-if="recentPosts.length === 0" class="rail-empty">还没有已发布文章</p>
        </section>
        <section aria-label="最近回复">
          <h2>最近回复 <span>{{ recentCommentsLoading ? '…' : recentCommentsError ? '—' : recentComments.length }}</span></h2>
          <a v-for="comment in recentComments" :key="comment.id" :href="`/user/${comment.postSlug}`" @click="closeMenu"><span>{{ comment.authorName }}：{{ comment.content }}</span></a>
          <p v-if="recentCommentsLoading" class="rail-empty" role="status">正在加载回复…</p>
          <template v-else-if="recentCommentsError">
            <p class="rail-empty" role="alert">最近回复加载失败</p>
            <button class="text-button" type="button" :disabled="recentCommentsLoading" @click="loadRecentComments">重试</button>
          </template>
          <p v-else-if="recentComments.length === 0" class="rail-empty">还没有已通过的评论</p>
        </section>
      </aside>
      <main class="public-content" :inert="menuOpen">
        <div id="reader-main" class="reader-main" tabindex="-1">
          <PageDetailView v-if="pageSlug" :status="status" />
          <PostDetailView v-else-if="postSlug" :status="status" />
          <PostListView v-else :page-size="status.reading?.postsPerPage || 10" :site-name="status.site?.name || ''" :site-description="status.site?.description || ''" :site-timezone="status.site?.timezone || 'UTC'" />
        </div>
        <footer class="public-footer">
          <span>{{ status.site?.name ?? '博客' }}</span>
          <nav aria-label="订阅与地图">
            <a href="/feed.xml">订阅</a>
            <a href="/sitemap.xml">站点地图</a>
          </nav>
          <span>v{{ status.version }}</span>
        </footer>
      </main>
    </div>
    <button v-show="showTop && !menuOpen" class="to-top" type="button" @click="scrollTop">回到顶部</button>
  </div>
</template>
