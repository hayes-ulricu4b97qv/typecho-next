<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, reactive, ref } from 'vue'
import { ApiError } from '../api'
import { createComment, getPost, listPostComments } from '../api/content'
import { formatSiteDate } from '../date'
import { renderMarkdown } from '../markdown'
import { setDocumentMeta } from '../meta'
import { decodePathSegment } from '../path'
import type { Comment, Post, Status, Term } from '../types'

const props = defineProps<{ status: Status }>()
const rawSlug = window.location.pathname.match(/^\/user\/(.+)$/)?.[1] ?? ''
const postSlug = rawSlug ? decodePathSegment(rawSlug) : ''
const selectedPost = ref<Post | null>(null)
const loading = ref(true)
const error = ref('')
const comments = ref<Comment[]>([])
const commentsLoading = ref(false)
const commentLoadError = ref('')
const commentSubmitting = ref(false)
const commentError = ref('')
const commentMessage = ref('')
const replyTo = ref<Comment | null>(null)
const commentForm = reactive({ authorName: '', authorEmail: '', authorUrl: '', content: '' })
const commentPage = ref(1)
const commentTotal = ref(0)
const commentCursor = ref('')
const failedCommentPage = ref<number | null>(null)
let commentLoadGeneration = 0
let commentSubmissionGeneration = 0
let postLoadGeneration = 0
let unlockGeneration = 0
const passphrase = ref('')
const unlocking = ref(false)
const unlockError = ref('')
const progress = ref(0)
const commentsOpen = computed(() => selectedPost.value?.allowComment !== false && props.status.reading?.commentsEnabled !== false)
const repliesOpen = computed(() => commentsOpen.value && props.status.reading?.commentsThreaded !== false)
const threadedComments = computed(() => threadComments(comments.value))
const renderedContent = computed(() => renderMarkdown(selectedPost.value?.content ?? ''))
const readingLabel = computed(() => {
  const text = selectedPost.value?.content?.replace(/\s+/g, '') ?? ''
  if (!text) return ''
  return `约 ${Math.max(1, Math.ceil(text.length / 450))} 分钟`
})

function applyMeta(post: Post) {
  const site = props.status.site
  const title = site?.name ? `${post.title} · ${site.name}` : post.title
  setDocumentMeta(title, post.excerpt || site?.description || '', site?.keywords ?? '')
}

function termURL(term: Term) {
  return `/user?${term.kind}=${encodeURIComponent(term.slug)}`
}

function safeURL(value?: string) {
  if (!value) return false
  try {
    const url = new URL(value)
    return (url.protocol === 'http:' || url.protocol === 'https:') && !url.username && !url.password
  } catch {
    return false
  }
}

function threadComments(items: Comment[]) {
  const children = new Map<string, Comment[]>()
  const roots: Comment[] = []
  const known = new Set(items.map((comment) => comment.id))
  for (const comment of items) {
    if (comment.parentId && known.has(comment.parentId)) {
      const list = children.get(comment.parentId) ?? []
      list.push(comment)
      children.set(comment.parentId, list)
    } else {
      roots.push(comment)
    }
  }
  const byTime = (left: Comment, right: Comment) => left.createdAt.localeCompare(right.createdAt)
  roots.sort(byTime)
  const flat: Array<Comment & { depth: number }> = []
  const walk = (comment: Comment, depth: number) => {
    flat.push({ ...comment, depth })
    for (const child of (children.get(comment.id) ?? []).sort(byTime)) walk(child, Math.min(depth + 1, 2))
  }
  for (const root of roots) walk(root, 0)
  return flat
}

function startReply(comment: Comment) {
  replyTo.value = comment
  commentMessage.value = ''
  commentError.value = ''
  void nextTick(() => {
    const behavior = window.matchMedia('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth'
    document.getElementById('comment-form')?.scrollIntoView({ block: 'center', behavior })
    document.getElementById('comment-body')?.focus({ preventScroll: true })
  })
}

function updateProgress() {
  const height = document.documentElement.scrollHeight - window.innerHeight
  progress.value = height <= 0 ? 0 : Math.min(100, Math.max(0, (window.scrollY / height) * 100))
}

async function loadComments(reset = true, requestedPage = reset ? 1 : commentPage.value) {
  if (!postSlug) return
  const generation = ++commentLoadGeneration
  commentsLoading.value = true
  commentLoadError.value = ''
  try {
    const response = await listPostComments(postSlug, requestedPage, 100, reset ? undefined : commentCursor.value)
    if (generation !== commentLoadGeneration) return
    comments.value = reset ? response.items : comments.value.concat(response.items)
    commentPage.value = response.page
    commentTotal.value = response.total
    commentCursor.value = response.nextCursor ?? ''
    failedCommentPage.value = null
  } catch (reason) {
    if (generation === commentLoadGeneration) {
      failedCommentPage.value = requestedPage
      commentLoadError.value = reason instanceof ApiError ? reason.message : '评论加载失败'
    }
  } finally {
    if (generation === commentLoadGeneration) commentsLoading.value = false
  }
}

async function loadMoreComments() {
  if (commentsLoading.value || !commentCursor.value) return
  await loadComments(false, commentPage.value + 1)
}

async function retryComments() {
  if (commentsLoading.value || failedCommentPage.value === null) return
  const page = failedCommentPage.value
  await loadComments(page === 1, page)
}

async function unlockPost() {
  if (!postSlug || !passphrase.value || unlocking.value) return
  const generation = ++unlockGeneration
  unlocking.value = true
  unlockError.value = ''
  try {
    const post = await getPost(postSlug, passphrase.value)
    if (generation !== unlockGeneration) return
    if (post.locked && !post.content) {
      unlockError.value = '阅读口令不正确，请重试'
      return
    }
    selectedPost.value = post
    applyMeta(post)
  } catch (reason) {
    if (generation === unlockGeneration) {
      unlockError.value = reason instanceof ApiError ? reason.message : '验证口令失败，请重试'
    }
  } finally {
    if (generation === unlockGeneration) unlocking.value = false
  }
}

async function submitComment() {
  if (!postSlug || commentSubmitting.value) return
  const generation = ++commentSubmissionGeneration
  commentSubmitting.value = true
  commentError.value = ''
  commentMessage.value = ''
  try {
    const created = await createComment(postSlug, { ...commentForm, parentId: replyTo.value?.id ?? '' })
    if (generation !== commentSubmissionGeneration) return
    commentForm.content = ''
    replyTo.value = null
    commentMessage.value = created.status === 'approved' ? '评论已发布。' : '评论已提交，审核通过后会显示。'
    if (created.status === 'approved') await loadComments()
  } catch (reason) {
    if (generation === commentSubmissionGeneration) {
      commentError.value = reason instanceof ApiError ? reason.message : '评论提交失败'
    }
  } finally {
    if (generation === commentSubmissionGeneration) commentSubmitting.value = false
  }
}

async function loadPost() {
  const generation = ++postLoadGeneration
  loading.value = true
  error.value = ''
  if (!postSlug) {
    loading.value = false
    error.value = '文章地址无效'
    return
  }
  try {
    const post = await getPost(postSlug)
    if (generation !== postLoadGeneration) return
    selectedPost.value = post
    applyMeta(post)
    await loadComments()
  } catch (reason) {
    if (generation === postLoadGeneration) {
      error.value = reason instanceof ApiError ? reason.message : '文章加载失败'
    }
  } finally {
    if (generation === postLoadGeneration) {
      loading.value = false
      updateProgress()
    }
  }
}

onMounted(() => {
  updateProgress()
  window.addEventListener('scroll', updateProgress, { passive: true })
  void loadPost()
})

onUnmounted(() => {
  commentLoadGeneration++
  commentSubmissionGeneration++
  postLoadGeneration++
  unlockGeneration++
  window.removeEventListener('scroll', updateProgress)
})
</script>

<template>
  <div v-if="loading" class="empty-state" role="status">正在加载文章…</div>
  <div v-else-if="error" class="empty-state empty-error"><strong role="alert">{{ error }}</strong><button v-if="postSlug" class="button button-dark" type="button" :disabled="loading" @click="loadPost">重试加载</button><a href="/user" class="button button-dark">返回文章列表</a></div>
  <article v-else-if="selectedPost" class="article-detail">
    <div class="read-progress" aria-hidden="true"><span :style="{ width: progress + '%' }"></span></div>
    <header class="article-head">
      <a class="back-link" href="/user">全部文章</a>
      <p class="eyebrow"><time :datetime="selectedPost.publishedAt || selectedPost.updatedAt">{{ formatSiteDate(selectedPost.publishedAt || selectedPost.updatedAt, props.status.site?.timezone || 'UTC') }}</time><template v-if="readingLabel"> · {{ readingLabel }}</template></p>
      <h1>{{ selectedPost.title }}</h1>
      <div v-if="selectedPost.categories?.length || selectedPost.tags?.length" class="post-terms"><a v-for="term in [...(selectedPost.categories ?? []), ...(selectedPost.tags ?? [])]" :key="term.id" :href="termURL(term)">{{ term.name }}</a></div>
    </header>
    <p v-if="selectedPost.excerpt" class="article-excerpt">{{ selectedPost.excerpt }}</p>
    <form v-if="selectedPost.locked && !selectedPost.content" class="comment-form" @submit.prevent="unlockPost">
      <p class="comment-form-title">这篇文章上了锁</p>
      <p class="settings-lead">输入阅读口令后，才能看到正文。</p>
      <label>阅读口令<input v-model="passphrase" type="password" required autocomplete="off" placeholder="输入口令后查看正文" /></label>
      <p v-if="unlockError" class="form-error" role="alert">{{ unlockError }}</p>
      <button class="button button-dark" :disabled="unlocking">{{ unlocking ? '验证中…' : '查看正文' }}</button>
    </form>
    <div v-else class="article-body article-rich" v-html="renderedContent"></div>
    <section v-if="selectedPost.content !== undefined && Object.keys(selectedPost.customFields ?? {}).length" class="post-custom-fields" aria-label="文章自定义字段">
      <dl>
        <div v-for="field in Object.entries(selectedPost.customFields ?? {})" :key="field[0]">
          <dt>{{ field[0] }}</dt><dd>{{ field[1] }}</dd>
        </div>
      </dl>
    </section>
    <nav v-if="selectedPost.previous || selectedPost.next" class="post-neighbors" aria-label="相邻文章">
      <a v-if="selectedPost.previous" :href="`/user/${selectedPost.previous.slug}`"><small>上一篇</small><strong>{{ selectedPost.previous.title }}</strong></a>
      <span v-else></span>
      <a v-if="selectedPost.next" class="neighbor-next" :href="`/user/${selectedPost.next.slug}`"><small>下一篇</small><strong>{{ selectedPost.next.title }}</strong></a>
    </nav>

    <section class="comment-section">
      <div class="comment-heading"><div><p class="eyebrow">留言</p><h2>评论</h2></div><span>{{ commentTotal }} 条</span></div>
      <div v-if="commentsLoading && comments.length === 0" class="comment-state" role="status">正在加载评论…</div>
      <div v-else-if="threadedComments.length === 0 && !commentLoadError" class="comment-state">{{ commentsOpen ? '还没有评论。想说的话，可以从这里开始。' : '评论已关闭。' }}</div>
      <div v-if="threadedComments.length > 0" class="comment-list">
        <article v-for="comment in threadedComments" :key="comment.id" class="comment-card" :data-depth="comment.depth">
          <div class="comment-meta">
            <span class="comment-person">
              <span class="comment-avatar" aria-hidden="true">{{ comment.authorName.slice(0, 1) }}</span>
              <a v-if="safeURL(comment.authorUrl)" :href="comment.authorUrl" rel="nofollow noreferrer" target="_blank">{{ comment.authorName }}</a>
              <strong v-else>{{ comment.authorName }}</strong>
            </span>
            <time :datetime="comment.createdAt">{{ formatSiteDate(comment.createdAt, props.status.site?.timezone || 'UTC') }}</time>
          </div>
          <p>{{ comment.content }}</p>
          <button v-if="repliesOpen && comment.depth < 2" class="text-button" type="button" @click="startReply(comment)">回复</button>
        </article>
      </div>
      <p v-if="commentLoadError" class="form-error" role="alert">{{ commentLoadError }}</p>
      <div v-if="commentCursor || commentLoadError" class="pagination"><button class="text-button" type="button" :disabled="commentsLoading" @click="commentLoadError ? retryComments() : loadMoreComments()">{{ commentsLoading ? '正在加载…' : commentLoadError ? '重试加载评论' : '加载更多评论' }}</button><span v-if="comments.length > 0">已显示 {{ comments.length }} / {{ commentTotal }}</span></div>
      <p v-if="!commentsOpen && threadedComments.length > 0" class="comment-state">评论已关闭。</p>
      <form v-if="commentsOpen" id="comment-form" class="comment-form" @submit.prevent="submitComment">
        <p class="comment-form-title">{{ replyTo ? '写下回复' : '参与讨论' }}</p>
        <div v-if="replyTo" class="comment-reply-target"><span>回复 {{ replyTo.authorName }}</span><button class="text-button" type="button" @click="replyTo = null">取消</button></div>
        <div class="form-grid"><label>称呼<input v-model="commentForm.authorName" required maxlength="80" placeholder="你的名字" /></label><label>邮箱（可选）<input v-model="commentForm.authorEmail" type="email" placeholder="you@example.com" /></label></div>
        <label>网站（可选）<input v-model="commentForm.authorUrl" type="url" maxlength="200" placeholder="https://example.com" /></label>
        <label>评论<textarea id="comment-body" v-model="commentForm.content" required maxlength="4000" rows="4" placeholder="写下你的想法…"></textarea></label>
        <p v-if="commentError" class="form-error" role="alert">{{ commentError }}</p>
        <p v-if="commentMessage" class="form-success" role="status">{{ commentMessage }}</p>
        <button class="button button-dark" :disabled="commentSubmitting">{{ commentSubmitting ? '提交中…' : replyTo ? '发表回复' : '发表评论' }}</button>
      </form>
    </section>
  </article>
</template>
