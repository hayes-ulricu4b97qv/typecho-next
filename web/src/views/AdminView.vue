<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, reactive, ref } from 'vue'
import { ApiError, requestDownload } from '../api'
import { forgotPassword, getCurrentUser, login as loginRequest, logout as logoutRequest, resetPassword } from '../api/auth'
import {
  createTerm as createTermRequest,
  deleteComment,
  deleteMedia,
  deletePage,
  deletePost,
  deleteTerm,
  getPost,
  listMedia,
  listModerationComments,
  listPages,
  listPosts,
  listRevisions,
  listTerms,
  moderateComment as moderateCommentRequest,
  restoreRevision,
  savePage as savePageRequest,
  savePost,
  updateTerm,
  uploadMedia as uploadMediaFile,
  type PostRevisionSummary,
} from '../api/content'
import { setDocumentMeta } from '../meta'
import { renderMarkdown } from '../markdown'
import { getDashboardStats } from '../api/settings'
import AdminNotifications from './AdminNotifications.vue'
import type { Comment, Media, Page, Post, Stats, Status, Term, User } from '../types'

type DashboardPageKey = 'posts' | 'pages' | 'comments' | 'media' | 'terms'
type DashboardPageOverrides = Partial<Record<DashboardPageKey, number>>
type DashboardLoadOverrides = DashboardPageOverrides & {
  postFilter?: 'all' | 'published' | 'draft'
  commentStatus?: 'pending' | 'approved' | 'spam'
}
type PageResult<T> = { items: T[]; total: number; page: number; pageSize: number }
type MarkdownAction = 'heading' | 'bold' | 'italic' | 'link' | 'image' | 'quote' | 'list' | 'code' | 'rule'
type BackupFileHandle = { createWritable(): Promise<WritableStream<Uint8Array>> }
type BackupFilePickerWindow = Window & {
  showSaveFilePicker?: (options: {
    suggestedName: string
    types: Array<{ description: string; accept: Record<string, string[]> }>
  }) => Promise<BackupFileHandle>
}
const canStreamBackup = typeof window !== 'undefined' && typeof (window as BackupFilePickerWindow).showSaveFilePicker === 'function'

async function reloadLastPageIfNeeded<T>(response: PageResult<T>, loadPage: (page: number) => Promise<PageResult<T>>) {
  const lastPage = Math.max(1, Math.ceil(response.total / response.pageSize))
  return response.page > lastPage ? loadPage(lastPage) : response
}

const props = defineProps<{ status: Status }>()
const adminPageSize = 100

const user = ref<User | null>(null)
const sessionGeneration = ref(0)
const sessionChecking = ref(false)
const stats = ref<Stats>({ posts: 0, publishedPosts: 0, drafts: 0, comments: 0, pendingComments: 0 })
const posts = ref<Post[]>([])
const postPage = ref(1)
const postTotal = ref(0)
const pages = ref<Page[]>([])
const pagePage = ref(1)
const pageTotal = ref(0)
const moderationComments = ref<Comment[]>([])
const commentPage = ref(1)
const commentTotal = ref(0)
const commentStatus = ref<'pending' | 'approved' | 'spam'>('pending')
const media = ref<Media[]>([])
const mediaPage = ref(1)
const mediaTotal = ref(0)
const terms = ref<Term[]>([])
const termPage = ref(1)
const termTotal = ref(0)
const loading = ref(true)
const error = ref('')
const dashboardLoadError = ref('')
const backupLoading = ref(false)
const backupError = ref('')
const backupMessage = ref('')
const loginForm = reactive({ username: '', password: '' })
const loginError = ref('')
const loginLoading = ref(false)
const showEditor = ref(false)
const editor = reactive({ title: '', slug: '', excerpt: '', content: '', categories: '', tags: '', allowComment: true, status: 'draft' as 'draft' | 'published', publishAt: '', passphrase: '' })
const customFieldRows = ref<Array<{ key: string; value: string }>>([])
const revisions = ref<PostRevisionSummary[]>([])
const revisionsPage = ref(0)
const revisionsTotal = ref(0)
const revisionsLoading = ref(false)
const postContentInput = ref<HTMLTextAreaElement | null>(null)
const previewHTML = computed(() => renderMarkdown(editor.content || ''))
const editorError = ref('')
const editingId = ref<string | null>(null)
const editingPostUpdatedAt = ref<string | null>(null)
const showPageEditor = ref(false)
const pageEditor = reactive({ title: '', slug: '', excerpt: '', content: '', status: 'draft' as 'draft' | 'published' })
const pageContentInput = ref<HTMLTextAreaElement | null>(null)
const pagePreviewHTML = computed(() => renderMarkdown(pageEditor.content || ''))
const pageEditorError = ref('')
const editingPageId = ref<string | null>(null)
const editingPageUpdatedAt = ref<string | null>(null)
const commentError = ref('')
const commentActionId = ref<string | null>(null)
const mediaError = ref('')
const mediaUploading = ref(false)
const mediaActionId = ref<string | null>(null)
const termForm = reactive({ name: '', kind: 'category' as 'category' | 'tag' })
const termError = ref('')
const termSubmitting = ref(false)
const termActionId = ref<string | null>(null)
const renamingId = ref<string | null>(null)
const renameValue = ref('')
const showReset = ref(false)
const showPreview = ref(false)
const showPagePreview = ref(false)
const postListFilter = ref<'all' | 'published' | 'draft'>('all')
const activeAnchor = ref('overview')
let sectionObserver: IntersectionObserver | undefined
let revisionRequestGeneration = 0
let dashboardLoadGeneration = 0
let lastDashboardLoadOverrides: DashboardLoadOverrides = {}
let backupController: AbortController | null = null
const resetToken = ref('')
const resetPasswordValue = ref('')
const forgotMessage = ref('')
const canEditContent = computed(() => user.value?.role === 'owner' || user.value?.role === 'editor')
const canModerateComments = computed(() => user.value?.role === 'owner' || user.value?.role === 'moderator')
const visiblePosts = computed(() => posts.value)
const markdownActions: Array<{ command: MarkdownAction; label: string; title: string }> = [
  { command: 'heading', label: 'H2', title: '插入二级标题' },
  { command: 'bold', label: '粗体', title: '插入粗体文字' },
  { command: 'italic', label: '斜体', title: '插入斜体文字' },
  { command: 'link', label: '链接', title: '插入链接' },
  { command: 'image', label: '图片', title: '插入图片' },
  { command: 'quote', label: '引用', title: '引用所选段落' },
  { command: 'list', label: '列表', title: '插入无序列表' },
  { command: 'code', label: '代码', title: '插入代码块' },
  { command: 'rule', label: '分隔线', title: '插入分隔线' },
]

function insertMarkdown(
  value: string,
  target: HTMLTextAreaElement | null,
  update: (next: string) => void,
  action: MarkdownAction,
) {
  const start = target?.selectionStart ?? value.length
  const end = target?.selectionEnd ?? start
  const selected = value.slice(start, end)
  let replacement = ''
  let selectionStart = start
  let selectionEnd = start

  if (action === 'rule') {
    const before = start > 0 && value[start - 1] !== '\n' ? '\n\n' : ''
    const after = end < value.length && value[end] !== '\n' ? '\n\n' : ''
    replacement = `${before}---${after}`
    selectionStart += before.length + 3
    selectionEnd = selectionStart
  } else if (action === 'heading' || action === 'quote' || action === 'list') {
    const prefix = action === 'heading' ? '## ' : action === 'quote' ? '> ' : '- '
    const placeholder = action === 'heading' ? '二级标题' : action === 'quote' ? '引用文字' : '列表项'
    const lines = (selected || placeholder).split('\n')
    replacement = lines.map((line) => `${prefix}${line}`).join('\n')
    if (selected) {
      selectionEnd = start + replacement.length
    } else {
      selectionStart = start + prefix.length
      selectionEnd = selectionStart + placeholder.length
    }
  } else {
    const formats: Record<Exclude<MarkdownAction, 'heading' | 'quote' | 'list' | 'rule'>, { before: string; after: string; placeholder: string }> = {
      bold: { before: '**', after: '**', placeholder: '粗体文字' },
      italic: { before: '*', after: '*', placeholder: '斜体文字' },
      link: { before: '[', after: '](https://example.com)', placeholder: '链接文字' },
      image: { before: '![', after: '](图片地址)', placeholder: '替代文字' },
      code: { before: '```\n', after: '\n```', placeholder: '代码' },
    }
    const format = formats[action]
    const content = selected || format.placeholder
    replacement = `${format.before}${content}${format.after}`
    selectionStart = start + format.before.length
    selectionEnd = selectionStart + content.length
    if (action === 'link') {
      selectionStart = start + format.before.length + content.length + 2
      selectionEnd = selectionStart + 'https://example.com'.length
    } else if (action === 'image') {
      selectionStart = start + format.before.length + content.length + 2
      selectionEnd = selectionStart + '图片地址'.length
    }
  }

  update(`${value.slice(0, start)}${replacement}${value.slice(end)}`)
  void nextTick(() => {
    if (!target) return
    target.focus()
    target.setSelectionRange(selectionStart, selectionEnd)
  })
}

function formatPost(action: MarkdownAction) {
  insertMarkdown(editor.content, postContentInput.value, (content) => { editor.content = content }, action)
}

function formatPage(action: MarkdownAction) {
  insertMarkdown(pageEditor.content, pageContentInput.value, (content) => { pageEditor.content = content }, action)
}

function clearAdminSessionState(loginMessage = '') {
  backupController?.abort()
  backupController = null
  backupLoading.value = false
  backupError.value = ''
  backupMessage.value = ''
  sessionGeneration.value++
  dashboardLoadGeneration++
  revisionRequestGeneration++
  sectionObserver?.disconnect()
  sectionObserver = undefined
  user.value = null
  sessionChecking.value = false
  stats.value = { posts: 0, publishedPosts: 0, drafts: 0, comments: 0, pendingComments: 0 }
  posts.value = []
  postPage.value = 1
  postTotal.value = 0
  postListFilter.value = 'all'
  pages.value = []
  pagePage.value = 1
  pageTotal.value = 0
  moderationComments.value = []
  commentPage.value = 1
  commentTotal.value = 0
  commentStatus.value = 'pending'
  media.value = []
  mediaPage.value = 1
  mediaTotal.value = 0
  terms.value = []
  termPage.value = 1
  termTotal.value = 0
  loading.value = false
  error.value = ''
  dashboardLoadError.value = ''
  cancelEditor()
  cancelPageEditor()
  commentError.value = ''
  commentActionId.value = null
  mediaError.value = ''
  mediaUploading.value = false
  mediaActionId.value = null
  Object.assign(termForm, { name: '', kind: 'category' })
  termError.value = ''
  termSubmitting.value = false
  termActionId.value = null
  renamingId.value = null
  renameValue.value = ''
  showReset.value = false
  activeAnchor.value = 'overview'
  lastDashboardLoadOverrides = {}
  loginForm.password = ''
  loginError.value = loginMessage
  loginLoading.value = false
  resetToken.value = ''
  resetPasswordValue.value = ''
  forgotMessage.value = ''
}

function handleAdminRequestFailure(reason: unknown, requestSession: number) {
  if (requestSession !== sessionGeneration.value) return true
  if ((user.value || sessionChecking.value) && reason instanceof ApiError && reason.status === 401) {
    clearAdminSessionState('登录状态已过期，请重新登录。')
    return true
  }
  if (user.value && reason instanceof ApiError && reason.status === 403 && reason.code === 'forbidden') {
    revalidateAdminSession(requestSession)
    return true
  }
  return false
}

function onAdminSessionExpired(requestSession: number) {
  if (requestSession !== sessionGeneration.value || !user.value) return
  clearAdminSessionState('登录状态已过期，请重新登录。')
}

function revalidateAdminSession(requestSession: number) {
  if (requestSession !== sessionGeneration.value || !user.value) return
  clearAdminSessionState()
  sessionChecking.value = true
  const generation = sessionGeneration.value
  void loadDashboard().finally(() => {
    if (generation === sessionGeneration.value) sessionChecking.value = false
  })
}

async function loadDashboard(overrides: DashboardLoadOverrides = {}) {
  const requestSession = sessionGeneration.value
  const generation = ++dashboardLoadGeneration
  lastDashboardLoadOverrides = { ...overrides }
  const requestedPages: Record<DashboardPageKey, number> = {
    posts: overrides.posts ?? postPage.value,
    pages: overrides.pages ?? pagePage.value,
    comments: overrides.comments ?? commentPage.value,
    media: overrides.media ?? mediaPage.value,
    terms: overrides.terms ?? termPage.value,
  }
  const requestedPostFilter = overrides.postFilter ?? postListFilter.value
  const requestedCommentStatus = overrides.commentStatus ?? commentStatus.value
  loading.value = true
  error.value = ''
  dashboardLoadError.value = ''
  try {
    const me = await getCurrentUser()
    if (generation !== dashboardLoadGeneration) return
    user.value = me.user
    const canEdit = me.user.role === 'owner' || me.user.role === 'editor'
    const canModerate = me.user.role === 'owner' || me.user.role === 'moderator'
    const effectivePostFilter = canEdit ? requestedPostFilter : 'all'
    const blank = <T,>() => ({ items: [] as T[], total: 0, page: 1, pageSize: adminPageSize })
    const [summary, initialPostResponse, initialPageResponse, initialCommentResponse, initialMediaResponse, initialTermResponse] = await Promise.all([
      getDashboardStats(),
      canEdit ? listPosts({ includeDraft: true, status: effectivePostFilter === 'all' ? undefined : effectivePostFilter, page: requestedPages.posts, pageSize: adminPageSize }) : Promise.resolve(blank<Post>()),
      canEdit ? listPages(true, requestedPages.pages, adminPageSize) : Promise.resolve(blank<Page>()),
      canModerate ? listModerationComments(requestedCommentStatus, requestedPages.comments, adminPageSize) : Promise.resolve(blank<Comment>()),
      canEdit ? listMedia(requestedPages.media, adminPageSize) : Promise.resolve(blank<Media>()),
      canEdit ? listTerms(true, requestedPages.terms, adminPageSize) : Promise.resolve(blank<Term>()),
    ])
    const [postResponse, pageResponse, commentResponse, mediaResponse, termResponse] = await Promise.all([
      canEdit ? reloadLastPageIfNeeded(initialPostResponse, (page) => listPosts({ includeDraft: true, status: effectivePostFilter === 'all' ? undefined : effectivePostFilter, page, pageSize: adminPageSize })) : Promise.resolve(initialPostResponse),
      canEdit ? reloadLastPageIfNeeded(initialPageResponse, (page) => listPages(true, page, adminPageSize)) : Promise.resolve(initialPageResponse),
      canModerate ? reloadLastPageIfNeeded(initialCommentResponse, (page) => listModerationComments(requestedCommentStatus, page, adminPageSize)) : Promise.resolve(initialCommentResponse),
      canEdit ? reloadLastPageIfNeeded(initialMediaResponse, (page) => listMedia(page, adminPageSize)) : Promise.resolve(initialMediaResponse),
      canEdit ? reloadLastPageIfNeeded(initialTermResponse, (page) => listTerms(true, page, adminPageSize)) : Promise.resolve(initialTermResponse),
    ])
    if (generation !== dashboardLoadGeneration) return
    stats.value = summary
    posts.value = postResponse.items
    postListFilter.value = effectivePostFilter
    postPage.value = postResponse.page
    postTotal.value = postResponse.total
    pages.value = pageResponse.items
    pagePage.value = pageResponse.page
    pageTotal.value = pageResponse.total
    moderationComments.value = commentResponse.items
    commentStatus.value = requestedCommentStatus
    commentPage.value = commentResponse.page
    commentTotal.value = commentResponse.total
    media.value = mediaResponse.items
    mediaPage.value = mediaResponse.page
    mediaTotal.value = mediaResponse.total
    terms.value = termResponse.items
    termPage.value = termResponse.page
    termTotal.value = termResponse.total
  } catch (reason) {
    if (generation !== dashboardLoadGeneration) return
    if (handleAdminRequestFailure(reason, requestSession)) return
    if (!(reason instanceof ApiError && reason.status === 401)) {
      const message = reason instanceof ApiError ? reason.message : '管理数据加载失败'
      if (user.value) dashboardLoadError.value = message
      else loginError.value = message
    }
  } finally {
    if (generation === dashboardLoadGeneration) loading.value = false
  }
  if (generation === dashboardLoadGeneration && user.value) await bindSections(generation)
}

function retryDashboard() {
  void loadDashboard(lastDashboardLoadOverrides)
}

function bindSections(generation: number) {
  sectionObserver?.disconnect()
  return nextTick(() => {
    if (generation !== dashboardLoadGeneration || !user.value) return
    const nodes = ['overview', 'posts', 'pages', 'comments', 'media', 'taxonomy', 'mail']
      .map((id) => document.getElementById(id))
      .filter((node): node is HTMLElement => node instanceof HTMLElement)
    if (nodes.length === 0) return
    sectionObserver = new IntersectionObserver((entries) => {
      const visible = entries.filter((entry) => entry.isIntersecting).sort((left, right) => right.intersectionRatio - left.intersectionRatio)[0]
      if (visible?.target instanceof HTMLElement && visible.target.id) activeAnchor.value = visible.target.id
    }, { rootMargin: '-30% 0px -55% 0px', threshold: [0.15, 0.4] })
    for (const node of nodes) sectionObserver.observe(node)
  })
}

async function login() {
  if (loginLoading.value) return
  const requestSession = sessionGeneration.value
  loginLoading.value = true
  loginError.value = ''
  try {
    const response = await loginRequest(loginForm)
    if (requestSession !== sessionGeneration.value) return
    clearAdminSessionState()
    user.value = response.user
    await loadDashboard()
  } catch (reason) {
    if (requestSession === sessionGeneration.value) {
      loginError.value = reason instanceof ApiError ? reason.message : '登录失败'
    }
  } finally {
    if (requestSession === sessionGeneration.value) loginLoading.value = false
  }
}

async function logout() {
  clearAdminSessionState()
  const logoutSession = sessionGeneration.value
  try {
    await logoutRequest()
  } catch (reason) {
    if (logoutSession !== sessionGeneration.value) return
    if (reason instanceof ApiError && reason.status === 401) return
    const message = reason instanceof ApiError ? reason.message : '退出登录失败'
    loginError.value = `${message}；本地管理数据已清除，但服务器会话状态未确认。`
  }
}

function onPageShow(event: PageTransitionEvent) {
  if (!event.persisted || !user.value) return
  revalidateAdminSession(sessionGeneration.value)
}

async function downloadBackup() {
  if (backupLoading.value) return
  const requestSession = sessionGeneration.value
  const pickerWindow = window as BackupFilePickerWindow
  const showSaveFilePicker = pickerWindow.showSaveFilePicker
  if (!showSaveFilePicker) {
    window.open('/api/v1/backup', '_blank', 'noopener,noreferrer')
    return
  }

  backupLoading.value = true
  backupError.value = ''
  backupMessage.value = ''
  let controller: AbortController | null = null
  try {
    const fileHandle = await showSaveFilePicker.call(pickerWindow, {
      suggestedName: 'typecho-backup.zip',
      types: [{ description: 'ZIP archive', accept: { 'application/zip': ['.zip'] } }],
    })
    if (requestSession !== sessionGeneration.value) return
    controller = new AbortController()
    backupController = controller
    backupMessage.value = '正在生成并保存备份…'
    const response = await requestDownload('/api/v1/backup', controller.signal)
    if (requestSession !== sessionGeneration.value) return
    const contentType = response.headers.get('Content-Type')?.split(';', 1)[0].trim().toLowerCase()
    if (contentType !== 'application/zip' || !response.body) {
      throw new Error('服务器没有返回有效的备份文件')
    }
    await response.body.pipeTo(await fileHandle.createWritable())
    if (requestSession === sessionGeneration.value) backupMessage.value = '备份已保存'
  } catch (reason) {
    if (reason instanceof DOMException && reason.name === 'AbortError') return
    if (handleAdminRequestFailure(reason, requestSession)) return
    backupMessage.value = ''
    backupError.value = reason instanceof ApiError ? reason.message : reason instanceof Error ? reason.message : '备份下载失败'
  } finally {
    if (backupController === controller) backupController = null
    if (requestSession === sessionGeneration.value) backupLoading.value = false
  }
}

function pageCount(total: number) {
  return Math.max(1, Math.ceil(total / adminPageSize))
}

async function changePostPage(next: number) {
  if (next < 1 || next > pageCount(postTotal.value)) return
  await loadDashboard({ posts: next })
}

async function switchPostFilter(filter: 'all' | 'published' | 'draft') {
  if (postListFilter.value === filter) return
  await loadDashboard({ posts: 1, postFilter: filter })
}

async function changePagePage(next: number) {
  if (next < 1 || next > pageCount(pageTotal.value)) return
  await loadDashboard({ pages: next })
}

async function changeMediaPage(next: number) {
  if (next < 1 || next > pageCount(mediaTotal.value)) return
  await loadDashboard({ media: next })
}

async function changeTermPage(next: number) {
  if (next < 1 || next > pageCount(termTotal.value)) return
  await loadDashboard({ terms: next })
}

async function changeCommentPage(next: number) {
  if (next < 1 || next > pageCount(commentTotal.value)) return
  await loadDashboard({ comments: next })
}

async function switchCommentStatus(status: 'pending' | 'approved' | 'spam') {
  if (commentStatus.value === status) return
  await loadDashboard({ comments: 1, commentStatus: status })
}

async function startEdit(post: Post) {
  const requestSession = sessionGeneration.value
  const generation = ++revisionRequestGeneration
  editingPostUpdatedAt.value = null
  editorError.value = ''
  revisions.value = []
  revisionsPage.value = 0
  revisionsTotal.value = 0
  revisionsLoading.value = false
  try {
    const full = await getPost(post.slug)
    if (generation !== revisionRequestGeneration) return
    editingId.value = full.id
    editingPostUpdatedAt.value = full.updatedAt
    Object.assign(editor, {
      title: full.title,
      slug: full.slug,
      excerpt: full.excerpt,
      content: full.content ?? '',
      categories: full.categories?.map((term) => term.name).join(', ') ?? '',
      tags: full.tags?.map((term) => term.name).join(', ') ?? '',
      allowComment: full.allowComment !== false,
      status: full.status,
      publishAt: '',
      passphrase: '',
    })
    customFieldRows.value = Object.entries(full.customFields ?? {}).map(([key, value]) => ({ key, value }))
    const history = await listRevisions(full.id)
    if (generation !== revisionRequestGeneration) return
    revisions.value = history.items
    revisionsPage.value = history.page
    revisionsTotal.value = history.total
    showEditor.value = true
    showPreview.value = false
    await nextTick()
    document.getElementById('post-editor')?.scrollIntoView({ block: 'start', behavior: 'auto' })
    document.querySelector<HTMLInputElement>('#post-editor .editor-title')?.focus({ preventScroll: true })
  } catch (reason) {
    if (generation !== revisionRequestGeneration) return
    if (handleAdminRequestFailure(reason, requestSession)) return
    editorError.value = reason instanceof ApiError ? reason.message : '无法打开文章'
    showEditor.value = true
  }
}

async function toggleEditor() {
  if (showEditor.value) {
    cancelEditor()
    return
  }
  revisionRequestGeneration++
  editingId.value = null
  editingPostUpdatedAt.value = null
  customFieldRows.value = []
  revisions.value = []
  revisionsPage.value = 0
  revisionsTotal.value = 0
  revisionsLoading.value = false
  showEditor.value = true
  showPreview.value = false
  await nextTick()
  document.getElementById('post-editor')?.scrollIntoView({ block: 'start', behavior: 'auto' })
  document.querySelector<HTMLInputElement>('#post-editor .editor-title')?.focus({ preventScroll: true })
}

function cancelEditor() {
  revisionRequestGeneration++
  editingId.value = null
  editingPostUpdatedAt.value = null
  showEditor.value = false
  showPreview.value = false
  revisions.value = []
  revisionsPage.value = 0
  revisionsTotal.value = 0
  revisionsLoading.value = false
  customFieldRows.value = []
  Object.assign(editor, { title: '', slug: '', excerpt: '', content: '', categories: '', tags: '', allowComment: true, status: 'draft', publishAt: '', passphrase: '' })
  editorError.value = ''
}

function addCustomField() {
  if (customFieldRows.value.length < 50) customFieldRows.value.push({ key: '', value: '' })
}

function removeCustomField(index: number) {
  customFieldRows.value.splice(index, 1)
}

function collectCustomFields() {
  const fields: Record<string, string> = {}
  for (const row of customFieldRows.value) {
    const key = row.key.trim()
    if (!key && row.value === '') continue
    if (!key) {
      editorError.value = '请填写自定义字段名称'
      return null
    }
    if (Object.prototype.hasOwnProperty.call(fields, key)) {
      editorError.value = '自定义字段名称不能重复'
      return null
    }
    fields[key] = row.value
  }
  return fields
}

async function publish() {
  const requestSession = sessionGeneration.value
  editorError.value = ''
  const customFields = collectCustomFields()
  if (!customFields) return
  const postId = editingId.value
  try {
    const payload = {
      title: editor.title,
      slug: editor.slug,
      excerpt: editor.excerpt,
      content: editor.content,
      customFields,
      status: editor.status,
      categories: splitTerms(editor.categories),
      tags: splitTerms(editor.tags),
      allowComment: editor.allowComment,
      publishAt: editor.publishAt ? new Date(editor.publishAt).toISOString() : undefined,
      passphrase: editor.passphrase || undefined,
    }
    if (postId) {
      const expectedUpdatedAt = editingPostUpdatedAt.value
      if (!expectedUpdatedAt) {
        editorError.value = '文章版本信息已失效，请重新打开后再保存。'
        return
      }
      await savePost(postId, { ...payload, expectedUpdatedAt })
    } else {
      await savePost(null, payload)
    }
    if (requestSession !== sessionGeneration.value) return
    cancelEditor()
    await loadDashboard()
  } catch (reason) {
    if (handleAdminRequestFailure(reason, requestSession)) return
    editorError.value = reason instanceof ApiError ? reason.message : '发布失败'
  }
}

function startPageEdit(page: Page) {
  editingPageId.value = page.id
  editingPageUpdatedAt.value = page.updatedAt
  Object.assign(pageEditor, { title: page.title, slug: page.slug, excerpt: page.excerpt, content: page.content, status: page.status })
  pageEditorError.value = ''
  showPageEditor.value = true
  showPagePreview.value = false
  void nextTick(() => {
    document.getElementById('page-editor')?.scrollIntoView({ block: 'start', behavior: 'auto' })
    document.querySelector<HTMLInputElement>('#page-editor .editor-title')?.focus({ preventScroll: true })
  })
}

async function togglePageEditor() {
  if (showPageEditor.value) {
    cancelPageEditor()
    return
  }
  editingPageUpdatedAt.value = null
  showPageEditor.value = true
  showPagePreview.value = false
  await nextTick()
  document.getElementById('page-editor')?.scrollIntoView({ block: 'start', behavior: 'auto' })
  document.querySelector<HTMLInputElement>('#page-editor .editor-title')?.focus({ preventScroll: true })
}

function cancelPageEditor() {
  editingPageId.value = null
  editingPageUpdatedAt.value = null
  showPageEditor.value = false
  showPagePreview.value = false
  Object.assign(pageEditor, { title: '', slug: '', excerpt: '', content: '', status: 'draft' })
  pageEditorError.value = ''
}

async function savePage() {
  const requestSession = sessionGeneration.value
  pageEditorError.value = ''
  const pageId = editingPageId.value
  try {
    const payload = {
      title: pageEditor.title,
      slug: pageEditor.slug,
      excerpt: pageEditor.excerpt,
      content: pageEditor.content,
      status: pageEditor.status,
    }
    if (pageId) {
      const expectedUpdatedAt = editingPageUpdatedAt.value
      if (!expectedUpdatedAt) {
        pageEditorError.value = '页面版本信息已失效，请重新打开后再保存。'
        return
      }
      await savePageRequest(pageId, { ...payload, expectedUpdatedAt })
    } else {
      await savePageRequest(null, payload)
    }
    if (requestSession !== sessionGeneration.value) return
    cancelPageEditor()
    await loadDashboard()
  } catch (reason) {
    if (handleAdminRequestFailure(reason, requestSession)) return
    pageEditorError.value = reason instanceof ApiError ? reason.message : '保存页面失败'
  }
}

async function removePage(page: Page) {
  if (!window.confirm(`确定删除页面“${page.title}”吗？`)) return
  const requestSession = sessionGeneration.value
  try {
    await deletePage(page.id)
    if (requestSession !== sessionGeneration.value) return
    await loadDashboard()
  } catch (reason) {
    if (handleAdminRequestFailure(reason, requestSession)) return
    error.value = reason instanceof ApiError ? reason.message : '删除页面失败'
  }
}

function splitTerms(value: string) {
  return [...new Set(value.split(',').map((term) => term.trim()).filter(Boolean))]
}

function postPreviewURL(post: Post) {
  return `/user/${encodeURIComponent(post.slug)}`
}

function pagePreviewURL(page: Page) {
  return `/page/${encodeURIComponent(page.slug)}`
}

async function removePost(post: Post) {
  if (!window.confirm(`确定删除“${post.title}”吗？`)) return
  const requestSession = sessionGeneration.value
  try {
    await deletePost(post.id)
    if (requestSession !== sessionGeneration.value) return
    await loadDashboard()
  } catch (reason) {
    if (handleAdminRequestFailure(reason, requestSession)) return
    error.value = reason instanceof ApiError ? reason.message : '删除失败'
  }
}

async function moderateComment(comment: Comment, status: 'approved' | 'spam') {
  const requestSession = sessionGeneration.value
  commentActionId.value = comment.id
  commentError.value = ''
  try {
    await moderateCommentRequest(comment.id, status)
    if (requestSession !== sessionGeneration.value) return
    await loadDashboard()
  } catch (reason) {
    if (handleAdminRequestFailure(reason, requestSession)) return
    commentError.value = reason instanceof ApiError ? reason.message : '更新评论失败'
  } finally {
    if (requestSession === sessionGeneration.value) commentActionId.value = null
  }
}

async function removeComment(comment: Comment) {
  if (!window.confirm(`确定删除来自“${comment.authorName}”的评论吗？`)) return
  const requestSession = sessionGeneration.value
  commentActionId.value = comment.id
  commentError.value = ''
  try {
    await deleteComment(comment.id)
    if (requestSession !== sessionGeneration.value) return
    await loadDashboard()
  } catch (reason) {
    if (handleAdminRequestFailure(reason, requestSession)) return
    commentError.value = reason instanceof ApiError ? reason.message : '删除评论失败'
  } finally {
    if (requestSession === sessionGeneration.value) commentActionId.value = null
  }
}

async function uploadMedia(event: Event) {
  const requestSession = sessionGeneration.value
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  mediaUploading.value = true
  mediaError.value = ''
  try {
    await uploadMediaFile(file)
    if (requestSession !== sessionGeneration.value) return
    input.value = ''
    await loadDashboard()
  } catch (reason) {
    if (handleAdminRequestFailure(reason, requestSession)) return
    mediaError.value = reason instanceof ApiError ? reason.message : '上传媒体失败'
  } finally {
    if (requestSession === sessionGeneration.value) mediaUploading.value = false
  }
}

async function removeMedia(asset: Media) {
  if (!window.confirm(`确定删除“${asset.filename}”吗？`)) return
  const requestSession = sessionGeneration.value
  mediaActionId.value = asset.id
  mediaError.value = ''
  try {
    await deleteMedia(asset.id)
    if (requestSession !== sessionGeneration.value) return
    await loadDashboard()
  } catch (reason) {
    if (handleAdminRequestFailure(reason, requestSession)) return
    mediaError.value = reason instanceof ApiError ? reason.message : '删除媒体失败'
  } finally {
    if (requestSession === sessionGeneration.value) mediaActionId.value = null
  }
}

function insertBlockAtSelection(content: string, start: number, end: number, block: string) {
  const before = content.slice(0, start)
  const after = content.slice(end)
  const prefix = before.length === 0 ? '' : before.endsWith('\n\n') ? '' : before.endsWith('\n') ? '\n' : '\n\n'
  const suffix = after.length === 0 ? '' : after.startsWith('\n\n') ? '' : after.startsWith('\n') ? '\n' : '\n\n'
  return {
    content: before + prefix + block + suffix + after,
    selectionStart: before.length + prefix.length + 2,
    selectionEnd: before.length + prefix.length + 6,
  }
}

function insertMedia(asset: Media, target: 'post' | 'page' = 'post') {
  const input = target === 'post' ? postContentInput.value : pageContentInput.value
  const current = target === 'post' ? editor.content : pageEditor.content
  const start = input?.selectionStart ?? current.length
  const end = input?.selectionEnd ?? start
  const insertion = insertBlockAtSelection(current, start, end, '![图片说明](' + asset.url + ')')

  if (target === 'post') {
    editor.content = insertion.content
    showEditor.value = true
    showPreview.value = false
  } else {
    pageEditor.content = insertion.content
    showPageEditor.value = true
    showPagePreview.value = false
  }
  void nextTick(() => {
    const form = document.getElementById(target === 'post' ? 'post-editor' : 'page-editor')
    const textarea = target === 'post' ? postContentInput.value : pageContentInput.value
    form?.scrollIntoView({ block: 'start', behavior: 'auto' })
    textarea?.focus({ preventScroll: true })
    textarea?.setSelectionRange(insertion.selectionStart, insertion.selectionEnd)
  })
}

async function restore(revision: PostRevisionSummary) {
  const requestSession = sessionGeneration.value
  const postId = editingId.value
  if (!postId) return
  const generation = ++revisionRequestGeneration
  revisionsLoading.value = false
  editorError.value = ''
  try {
    const restored = await restoreRevision(postId, revision.id)
    if (generation !== revisionRequestGeneration || editingId.value !== postId) return
    editingPostUpdatedAt.value = restored.updatedAt
    editor.title = restored.title
    editor.excerpt = restored.excerpt
    editor.content = restored.content ?? ''
    customFieldRows.value = Object.entries(restored.customFields ?? {}).map(([key, value]) => ({ key, value }))
    const history = await listRevisions(postId)
    if (generation !== revisionRequestGeneration || editingId.value !== postId) return
    revisions.value = history.items
    revisionsPage.value = history.page
    revisionsTotal.value = history.total
  } catch (reason) {
    if (generation !== revisionRequestGeneration) return
    if (handleAdminRequestFailure(reason, requestSession)) return
    editorError.value = reason instanceof ApiError ? reason.message : '恢复版本失败'
  }
}

async function loadMoreRevisions() {
  const requestSession = sessionGeneration.value
  const postId = editingId.value
  if (!postId || revisionsLoading.value || revisions.value.length >= revisionsTotal.value) return
  const generation = revisionRequestGeneration
  editorError.value = ''
  revisionsLoading.value = true
  try {
    const history = await listRevisions(postId, revisionsPage.value + 1)
    if (generation !== revisionRequestGeneration || editingId.value !== postId) return
    if (history.items.length === 0) {
      revisionsTotal.value = revisions.value.length
      return
    }
    revisions.value.push(...history.items)
    revisionsPage.value = history.page
    revisionsTotal.value = history.total
  } catch (reason) {
    if (generation !== revisionRequestGeneration) return
    if (handleAdminRequestFailure(reason, requestSession)) return
    editorError.value = reason instanceof ApiError ? reason.message : '无法加载更早的修订版本'
  } finally {
    if (generation === revisionRequestGeneration) revisionsLoading.value = false
  }
}

async function sendReset() {
  const requestSession = sessionGeneration.value
  forgotMessage.value = ''
  loginError.value = ''
  try {
    const response = await forgotPassword(loginForm.username)
    if (requestSession !== sessionGeneration.value) return
    forgotMessage.value = response.message
  } catch (reason) {
    if (requestSession === sessionGeneration.value) {
      loginError.value = reason instanceof ApiError ? reason.message : '无法发送重置邮件'
    }
  }
}

async function applyReset() {
  const requestSession = sessionGeneration.value
  forgotMessage.value = ''
  loginError.value = ''
  try {
    await resetPassword(resetToken.value.trim(), resetPasswordValue.value)
    if (requestSession !== sessionGeneration.value) return
    forgotMessage.value = '密码已重置，请使用新密码登录。'
    loginForm.password = ''
    resetPasswordValue.value = ''
    showReset.value = false
  } catch (reason) {
    if (requestSession === sessionGeneration.value) {
      loginError.value = reason instanceof ApiError ? reason.message : '重置密码失败'
    }
  }
}

function beginRename(term: Term) {
  renamingId.value = term.id
  renameValue.value = term.name
}

async function renameTerm(term: Term) {
  const requestSession = sessionGeneration.value
  termActionId.value = term.id
  termError.value = ''
  try {
    await updateTerm(term.id, { name: renameValue.value.trim(), kind: term.kind })
    if (requestSession !== sessionGeneration.value) return
    renamingId.value = null
    await loadDashboard()
  } catch (reason) {
    if (handleAdminRequestFailure(reason, requestSession)) return
    termError.value = reason instanceof ApiError ? reason.message : '重命名失败'
  } finally {
    if (requestSession === sessionGeneration.value) termActionId.value = null
  }
}

async function createTerm() {
  const requestSession = sessionGeneration.value
  termSubmitting.value = true
  termError.value = ''
  try {
    await createTermRequest(termForm)
    if (requestSession !== sessionGeneration.value) return
    termForm.name = ''
    await loadDashboard()
  } catch (reason) {
    if (handleAdminRequestFailure(reason, requestSession)) return
    termError.value = reason instanceof ApiError ? reason.message : '保存分类/标签失败'
  } finally {
    if (requestSession === sessionGeneration.value) termSubmitting.value = false
  }
}

async function removeTerm(term: Term) {
  if (!window.confirm(`确定删除“${term.name}”吗？`)) return
  const requestSession = sessionGeneration.value
  termActionId.value = term.id
  termError.value = ''
  try {
    await deleteTerm(term.id)
    if (requestSession !== sessionGeneration.value) return
    await loadDashboard()
  } catch (reason) {
    if (handleAdminRequestFailure(reason, requestSession)) return
    termError.value = reason instanceof ApiError ? reason.message : '删除分类/标签失败'
  } finally {
    if (requestSession === sessionGeneration.value) termActionId.value = null
  }
}

onMounted(async () => {
  window.addEventListener('pageshow', onPageShow)
  const site = props.status.site
  setDocumentMeta(site?.name ? `${site.name} · 管理` : '管理', site?.description ?? '', site?.keywords ?? '')
  await loadDashboard()
})

onUnmounted(() => {
  backupController?.abort()
  backupController = null
  sessionGeneration.value++
  dashboardLoadGeneration++
  revisionRequestGeneration++
  sectionObserver?.disconnect()
  window.removeEventListener('pageshow', onPageShow)
})
</script>

<template>
  <div class="admin-shell" :class="{ 'is-guest': !user }">
    <header class="admin-command">
      <a class="brand" href="/user"><span class="brand-mark">{{ status.site?.name?.trim().slice(0, 1) || '博' }}</span><span class="brand-name">{{ status.site?.name ?? '博客' }}</span></a>
      <nav v-if="user" class="command-nav" aria-label="管理分区">
        <a href="#overview" :class="{ active: activeAnchor === 'overview' }">概览</a>
        <a href="#posts" :class="{ active: activeAnchor === 'posts' }">文章</a>
        <a v-if="canEditContent" href="#pages" :class="{ active: activeAnchor === 'pages' }">页面</a>
        <a v-if="canModerateComments" href="#comments" :class="{ active: activeAnchor === 'comments' }">评论</a>
        <a v-if="canEditContent" href="#media" :class="{ active: activeAnchor === 'media' }">媒体</a>
        <a v-if="canEditContent" href="#taxonomy" :class="{ active: activeAnchor === 'taxonomy' }">分类</a>
        <a v-if="user.role === 'owner'" href="#mail" :class="{ active: activeAnchor === 'mail' }">通知</a>
        <a v-if="user.role === 'owner'" href="/setting">设置</a>
      </nav>
      <div class="top-actions">
        <button v-if="user?.role === 'owner' && canStreamBackup" class="command-link" type="button" :disabled="backupLoading" @click="downloadBackup">{{ backupLoading ? '备份中…' : '备份' }}</button>
        <a v-else-if="user?.role === 'owner'" class="command-link" href="/api/v1/backup" download="typecho-backup.zip">备份</a>
        <a v-if="user" class="command-link" href="/user">阅读</a>
        <button v-if="user" class="avatar" type="button" :aria-label="'退出 ' + user.username" :title="'退出 ' + user.username" @click="logout">{{ user.username.slice(0, 1).toUpperCase() }}</button>
      </div>
    </header>
    <main class="admin-main">
      <div v-if="user && (backupError || backupMessage)" class="backup-feedback">
        <p v-if="backupError" class="form-error" role="alert">{{ backupError }}</p>
        <p v-else class="form-success" role="status">{{ backupMessage }}</p>
        <button v-if="backupError && canStreamBackup" class="text-button" type="button" :disabled="backupLoading" @click="downloadBackup">重试备份</button>
      </div>
      <div v-if="sessionChecking" class="empty-state" role="status">正在重新验证管理会话…</div>
      <div v-else-if="!user" class="login-stage">
      <div class="login-panel">
        <div class="login-copy"><span class="login-mark">笔</span><h2>进入管理端</h2><p>使用首次设置时创建的管理员账户登录。写完文章，读者就能在前台看到。</p></div>
        <form @submit.prevent="login">
          <label>用户名<input v-model="loginForm.username" required autocomplete="username" placeholder="admin" /></label>
          <label>密码<input v-model="loginForm.password" required type="password" autocomplete="current-password" placeholder="••••••••" /></label>
          <p v-if="loginError" class="form-error" role="alert">{{ loginError }}</p>
          <p v-if="forgotMessage" class="form-success" role="status">{{ forgotMessage }}</p>
          <button class="button button-primary button-wide" :disabled="loginLoading">{{ loginLoading ? '登录中…' : '登录管理端' }}</button>
        </form>
        <button class="text-button" type="button" @click="showReset = !showReset">忘记密码</button>
        <form v-if="showReset" class="login-reset" @submit.prevent="applyReset">
          <p>重置口令会发送到该账户登记的邮箱，并在 30 分钟后失效。站点需要配置 SMTP 邮件投递。</p>
          <button class="text-button" type="button" :disabled="!loginForm.username" @click="sendReset">发送重置邮件</button>
          <label>重置口令<input v-model="resetToken" required autocomplete="off" /></label>
          <label>新密码<input v-model="resetPasswordValue" required type="password" minlength="8" autocomplete="new-password" /></label>
          <button class="button button-dark button-wide">重置密码</button>
        </form>
      </div>
      </div>

      <template v-else>
        <header id="overview" class="admin-topbar">
          <p class="eyebrow">工作台</p>
          <h1>欢迎回来<span v-if="user">，{{ user.username }}</span></h1>
          <p class="admin-lead">写文章、审评论、整理分类和图片，都从这一页开始。</p>
        </header>
        <p v-if="error" class="form-error" role="alert">{{ error }}</p>
        <p v-if="dashboardLoadError" class="form-error" role="alert">{{ dashboardLoadError }}</p>
        <button v-if="dashboardLoadError" class="text-button" type="button" :disabled="loading" @click="retryDashboard">重试加载管理数据</button>
        <div class="stats-grid">
          <div class="stat-card"><span>全部文章</span><strong>{{ stats.posts }}</strong><small>累计内容</small></div>
          <div class="stat-card stat-green"><span>已发布</span><strong>{{ stats.publishedPosts }}</strong><small>对外可见</small></div>
          <div class="stat-card stat-yellow"><span>草稿</span><strong>{{ stats.drafts }}</strong><small>等待完善</small></div>
          <div class="stat-card stat-blue"><span>待审核评论</span><strong>{{ stats.pendingComments }}</strong><small>需要处理</small></div>
        </div>

        <AdminNotifications v-if="user?.role === 'owner'" :session-generation="sessionGeneration" @session-expired="onAdminSessionExpired" @access-revoked="revalidateAdminSession" />

        <section v-if="canEditContent" class="admin-section" id="posts">
          <div class="section-heading"><div><p class="eyebrow">文章</p><h2>写作与稿件</h2></div><span class="section-count">{{ postTotal }} 篇</span><button v-if="canEditContent" class="button button-primary" type="button" @click="toggleEditor">{{ showEditor ? '关闭编辑器' : '+ 写文章' }}</button></div>
          <form v-if="showEditor && canEditContent" id="post-editor" class="editor-card editor-split" @submit.prevent="publish">
            <div class="editor-write">
              <input v-model="editor.title" class="editor-title" required placeholder="文章标题" />
              <label class="editor-slug">固定链接<input v-model="editor.slug" placeholder="留空则保留现有地址或按标题生成" /></label>
              <textarea ref="postContentInput" v-model="editor.content" rows="12" required placeholder="开始写作，支持 Markdown…" aria-label="文章 Markdown 正文"></textarea>
              <div class="editor-preview-bar">
                <div class="editor-markdown-tools" role="group" aria-label="文章 Markdown 格式">
                  <button v-for="action in markdownActions" :key="action.command" class="text-button markdown-action" type="button" :title="action.title" :aria-label="action.title" @mousedown.prevent @click="formatPost(action.command)">{{ action.label }}</button>
                </div>
                <button class="text-button" type="button" :aria-expanded="showPreview" @click="showPreview = !showPreview">{{ showPreview ? '收起预览' : '看预览' }}</button>
              </div>
              <div v-if="showPreview" class="article-body article-rich editor-preview" v-html="previewHTML"></div>
            </div>
            <div class="editor-meta">
              <label>摘要<textarea v-model="editor.excerpt" rows="3" placeholder="一句话摘要（可选）"></textarea></label>
              <label>分类<input v-model="editor.categories" placeholder="产品, 随笔" /></label>
              <label>标签<input v-model="editor.tags" placeholder="Go, Vue, 重构" /></label>
              <label>定时发布<input v-model="editor.publishAt" type="datetime-local" /></label>
              <label>阅读口令<input v-model="editor.passphrase" type="password" autocomplete="new-password" placeholder="留空则不修改；填写后读者需口令才能看正文" /></label>
              <section class="custom-field-editor" aria-label="自定义字段">
                <div class="custom-field-heading"><strong>自定义字段</strong><button class="text-button" type="button" :disabled="customFieldRows.length >= 50" @click="addCustomField">添加字段</button></div>
                <div v-for="(field, index) in customFieldRows" :key="index" class="custom-field-row">
                  <label>名称<input v-model="field.key" maxlength="80" placeholder="例如 subtitle" /></label>
                  <label>值<textarea v-model="field.value" maxlength="2000" rows="2" placeholder="字段内容"></textarea></label>
                  <button class="text-button danger" type="button" :aria-label="`删除自定义字段 ${index + 1}`" @click="removeCustomField(index)">删除</button>
                </div>
                <small>字段名称最多 80 个字符，字段值最多 2,000 个字符；每篇文章最多 50 项。</small>
              </section>
              <div v-if="revisionsTotal" class="taxonomy-list">
                <div v-for="revision in revisions" :key="revision.id" class="taxonomy-row">
                  <div><strong>{{ revision.title }}</strong><small>{{ new Date(revision.createdAt).toLocaleString('zh-CN') }}</small></div>
                  <button class="text-button" type="button" @click="restore(revision)">恢复此版本</button>
                </div>
                <div v-if="revisions.length < revisionsTotal" class="taxonomy-row">
                  <small>已加载 {{ revisions.length }} / {{ revisionsTotal }} 个版本</small>
                  <button class="text-button" type="button" :disabled="revisionsLoading" @click="loadMoreRevisions">{{ revisionsLoading ? '正在加载…' : '加载更早版本' }}</button>
                </div>
              </div>
              <div class="editor-actions"><label class="editor-check"><input v-model="editor.allowComment" type="checkbox" />允许评论</label><select v-model="editor.status"><option value="draft">保存为草稿</option><option value="published">立即发布</option></select><p v-if="editorError" class="form-error" role="alert">{{ editorError }}</p><button class="button button-dark">{{ editingId ? '保存修改' : '保存文章' }}</button></div>
            </div>
          </form>
          <div v-if="loading" class="empty-state" role="status">正在加载…</div>
          <div v-else-if="posts.length === 0" class="empty-state"><strong>开始你的第一篇文章</strong><span>文章会保存在新的内容服务中。</span></div>
          <template v-else>
            <div class="term-filter-bar" aria-label="按状态筛选文章">
              <button class="term-filter" type="button" :class="{ active: postListFilter === 'all' }" @click="switchPostFilter('all')">全部</button>
              <button class="term-filter" type="button" :class="{ active: postListFilter === 'published' }" @click="switchPostFilter('published')">已发布</button>
              <button class="term-filter" type="button" :class="{ active: postListFilter === 'draft' }" @click="switchPostFilter('draft')">草稿</button>
            </div>
            <div v-if="visiblePosts.length === 0" class="empty-state">这一页没有符合的文章</div>
            <div v-else class="admin-posts">
              <div v-for="post in visiblePosts" :key="post.id" class="admin-post-row"><div><span class="post-status" :class="post.status">{{ post.status === 'published' ? '已发布' : '草稿' }}</span><div><strong>{{ post.title }}</strong><small v-if="post.categories?.length || post.tags?.length">{{ [...(post.categories ?? []), ...(post.tags ?? [])].map((term) => term.name).join(' · ') }}</small></div></div><time>{{ new Date(post.publishedAt || post.updatedAt).toLocaleDateString('zh-CN') }}</time><div v-if="canEditContent" class="row-actions"><button class="text-button" type="button" @click="startEdit(post)">编辑</button><a class="text-button" :href="postPreviewURL(post)" target="_blank" rel="noreferrer">{{ post.status === 'published' ? '查看' : '预览' }}</a><button class="text-button danger" type="button" @click="removePost(post)">删除</button></div></div>
            </div>
          </template>
          <div v-if="postTotal > adminPageSize" class="pagination"><button class="text-button" type="button" :disabled="postPage === 1" @click="changePostPage(postPage - 1)">上一页</button><span>{{ postPage }} / {{ pageCount(postTotal) }} · 共 {{ postTotal }} 篇</span><button class="text-button" type="button" :disabled="postPage >= pageCount(postTotal)" @click="changePostPage(postPage + 1)">下一页</button></div>
        </section>

        <section v-if="canEditContent" class="admin-section page-section" id="pages">
          <div class="section-heading"><div><p class="eyebrow">页面</p><h2>独立页面</h2></div><span class="section-count">{{ pageTotal }} 个</span><button class="button button-primary" type="button" @click="togglePageEditor">{{ showPageEditor ? '关闭编辑器' : '+ 新建页面' }}</button></div>
          <form v-if="showPageEditor" id="page-editor" class="editor-card editor-split" @submit.prevent="savePage">
            <div class="editor-write">
              <input v-model="pageEditor.title" class="editor-title" required placeholder="页面标题" />
              <label class="editor-slug">固定链接<input v-model="pageEditor.slug" placeholder="留空则按标题生成" /></label>
              <textarea ref="pageContentInput" v-model="pageEditor.content" rows="12" required placeholder="页面内容，支持 Markdown…" aria-label="页面 Markdown 正文"></textarea>
              <div class="editor-preview-bar">
                <div class="editor-markdown-tools" role="group" aria-label="页面 Markdown 格式">
                  <button v-for="action in markdownActions" :key="action.command" class="text-button markdown-action" type="button" :title="action.title" :aria-label="action.title" @mousedown.prevent @click="formatPage(action.command)">{{ action.label }}</button>
                </div>
                <button class="text-button" type="button" :aria-expanded="showPagePreview" @click="showPagePreview = !showPagePreview">{{ showPagePreview ? '收起预览' : '看预览' }}</button>
              </div>
              <div v-if="showPagePreview" class="article-body article-rich editor-preview" v-html="pagePreviewHTML"></div>
            </div>
            <div class="editor-meta">
              <label>摘要<textarea v-model="pageEditor.excerpt" rows="3" placeholder="页面摘要（可选）"></textarea></label>
              <div class="editor-actions"><select v-model="pageEditor.status"><option value="draft">保存为草稿</option><option value="published">立即发布</option></select><p v-if="pageEditorError" class="form-error" role="alert">{{ pageEditorError }}</p><button class="button button-dark">{{ editingPageId ? '保存修改' : '保存页面' }}</button></div>
            </div>
          </form>
          <div v-if="pages.length === 0" class="empty-state page-empty">还没有独立页面</div>
          <div v-else class="admin-posts">
            <div v-for="page in pages" :key="page.id" class="admin-post-row"><div><span class="post-status" :class="page.status">{{ page.status === 'published' ? '已发布' : '草稿' }}</span><div><strong>{{ page.title }}</strong><small>/page/{{ page.slug }}</small></div></div><time>{{ new Date(page.updatedAt).toLocaleDateString('zh-CN') }}</time><div class="row-actions"><button class="text-button" type="button" @click="startPageEdit(page)">编辑</button><a class="text-button" :href="pagePreviewURL(page)" target="_blank" rel="noreferrer">{{ page.status === 'published' ? '查看' : '预览' }}</a><button class="text-button danger" type="button" @click="removePage(page)">删除</button></div></div>
          </div>
          <div v-if="pageTotal > adminPageSize" class="pagination"><button class="text-button" type="button" :disabled="pagePage === 1" @click="changePagePage(pagePage - 1)">上一页</button><span>{{ pagePage }} / {{ pageCount(pageTotal) }} · 共 {{ pageTotal }} 个</span><button class="text-button" type="button" :disabled="pagePage >= pageCount(pageTotal)" @click="changePagePage(pagePage + 1)">下一页</button></div>
        </section>

        <section v-if="canModerateComments" id="comments" class="admin-section comments-section">
          <div class="section-heading"><div><p class="eyebrow">评论</p><h2>审核</h2></div><span class="section-count">{{ commentTotal }} 条</span></div>
          <div class="term-filter-bar">
            <button class="term-filter" type="button" :class="{ active: commentStatus === 'pending' }" @click="switchCommentStatus('pending')">待审核</button>
            <button class="term-filter" type="button" :class="{ active: commentStatus === 'approved' }" @click="switchCommentStatus('approved')">已通过</button>
            <button class="term-filter" type="button" :class="{ active: commentStatus === 'spam' }" @click="switchCommentStatus('spam')">垃圾</button>
          </div>
          <p v-if="commentError" class="form-error" role="alert">{{ commentError }}</p>
          <div v-if="moderationComments.length === 0" class="empty-state">这个状态下还没有评论</div>
          <div v-else class="admin-comments">
            <article v-for="comment in moderationComments" :key="comment.id" class="admin-comment-row">
              <div class="comment-row-content">
                <div class="comment-row-meta"><strong>{{ comment.authorName }}</strong><span v-if="comment.authorEmail">{{ comment.authorEmail }}</span><time>{{ new Date(comment.createdAt).toLocaleDateString('zh-CN') }}</time></div>
                <p>{{ comment.content }}</p>
                <small v-if="comment.postTitle">文章：{{ comment.postTitle }}</small>
                <small v-if="comment.parentId">这是一条回复</small>
              </div>
              <div class="row-actions"><button v-if="comment.status !== 'approved'" class="text-button" :disabled="commentActionId === comment.id" @click="moderateComment(comment, 'approved')">通过</button><button v-if="comment.status !== 'spam'" class="text-button danger" :disabled="commentActionId === comment.id" @click="moderateComment(comment, 'spam')">垃圾</button><button class="text-button danger" :disabled="commentActionId === comment.id" @click="removeComment(comment)">删除</button></div>
            </article>
          </div>
          <div v-if="commentTotal > adminPageSize" class="pagination"><button class="text-button" type="button" :disabled="commentPage === 1" @click="changeCommentPage(commentPage - 1)">上一页</button><span>{{ commentPage }} / {{ pageCount(commentTotal) }} · 共 {{ commentTotal }} 条</span><button class="text-button" type="button" :disabled="commentPage >= pageCount(commentTotal)" @click="changeCommentPage(commentPage + 1)">下一页</button></div>
        </section>

        <section v-if="canEditContent" id="media" class="admin-section media-section">
          <div class="section-heading"><div><p class="eyebrow">媒体</p><h2>图片</h2><p class="media-upload-hint">单文件不超过 10 MiB；最长边 16,384 px，最多 4,000 万像素</p></div><span class="section-count">{{ mediaTotal }} 个</span><label class="button button-primary media-upload"><input type="file" accept="image/jpeg,image/png,image/gif,image/webp" :disabled="mediaUploading" @change="uploadMedia" />{{ mediaUploading ? '上传中…' : '+ 上传图片' }}</label></div>
          <p v-if="mediaError" class="form-error" role="alert">{{ mediaError }}</p>
          <div v-if="media.length === 0" class="empty-state media-empty">还没有上传媒体</div>
          <div v-else class="media-grid">
            <article v-for="asset in media" :key="asset.id" class="media-card">
              <a :href="asset.url" target="_blank" rel="noreferrer"><img :src="asset.url" :alt="asset.filename" loading="lazy" /></a>
              <div class="media-card-info"><strong :title="asset.filename">{{ asset.filename }}</strong><span>{{ Math.ceil(asset.size / 1024) }} KB</span></div>
              <div class="row-actions"><button class="text-button" type="button" @click="insertMedia(asset, 'post')">插入文章</button><button v-if="showPageEditor" class="text-button" type="button" @click="insertMedia(asset, 'page')">插入页面</button><button class="text-button danger" :disabled="mediaActionId === asset.id" @click="removeMedia(asset)">删除</button></div>
            </article>
          </div>
          <div v-if="mediaTotal > adminPageSize" class="pagination"><button class="text-button" type="button" :disabled="mediaPage === 1" @click="changeMediaPage(mediaPage - 1)">上一页</button><span>{{ mediaPage }} / {{ pageCount(mediaTotal) }} · 共 {{ mediaTotal }} 个</span><button class="text-button" type="button" :disabled="mediaPage >= pageCount(mediaTotal)" @click="changeMediaPage(mediaPage + 1)">下一页</button></div>
        </section>

        <section v-if="canEditContent" id="taxonomy" class="admin-section taxonomy-section">
          <div class="section-heading"><div><p class="eyebrow">分类</p><h2>分类与标签</h2></div><span class="section-count">{{ termTotal }} 个</span></div>
          <form class="taxonomy-form" @submit.prevent="createTerm">
            <input v-model="termForm.name" required maxlength="60" placeholder="名称" />
            <select v-model="termForm.kind"><option value="category">分类</option><option value="tag">标签</option></select>
            <button class="button button-primary" :disabled="termSubmitting">{{ termSubmitting ? '保存中…' : '新增' }}</button>
          </form>
          <p v-if="termError" class="form-error" role="alert">{{ termError }}</p>
          <div v-if="terms.length === 0" class="empty-state taxonomy-empty">还没有分类或标签</div>
          <div v-else class="taxonomy-list">
            <div v-for="term in terms" :key="term.id" class="taxonomy-row">
              <div><strong v-if="renamingId !== term.id">{{ term.name }}</strong><input v-else v-model="renameValue" maxlength="60" /><small>{{ term.kind === 'category' ? '分类' : '标签' }} · {{ term.postCount }} 篇已发布文章</small></div>
              <div class="row-actions"><button v-if="renamingId !== term.id" class="text-button" type="button" @click="beginRename(term)">重命名</button><button v-else class="text-button" type="button" :disabled="termActionId === term.id" @click="renameTerm(term)">保存</button><button class="text-button danger" :disabled="termActionId === term.id" @click="removeTerm(term)">删除</button></div>
            </div>
          </div>
          <div v-if="termTotal > adminPageSize" class="pagination"><button class="text-button" type="button" :disabled="termPage === 1" @click="changeTermPage(termPage - 1)">上一页</button><span>{{ termPage }} / {{ pageCount(termTotal) }} · 共 {{ termTotal }} 个</span><button class="text-button" type="button" :disabled="termPage >= pageCount(termTotal)" @click="changeTermPage(termPage + 1)">下一页</button></div>
        </section>

      </template>
    </main>
  </div>
</template>
