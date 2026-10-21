import { api, postForm, postJSON, putJSON } from '../api'
import type { Comment, CommentPage, Media, MediaPage, MonthCount, Page, PagePage, Post, PostPage, Term, TermPage } from '../types'

export interface PostInput {
  title: string
  slug: string
  excerpt: string
  content: string
  customFields?: Record<string, string>
  status: 'draft' | 'published'
  allowComment?: boolean
  categories: string[]
  tags: string[]
  publishAt?: string
  passphrase?: string
}

export interface PostUpdateInput extends PostInput {
  expectedUpdatedAt: string
}

export interface PageInput {
  title: string
  slug: string
  excerpt: string
  content: string
  status: 'draft' | 'published'
}

export interface PageUpdateInput extends PageInput {
  expectedUpdatedAt: string
}

export interface CommentInput {
  authorName: string
  authorEmail: string
  authorUrl?: string
  content: string
  parentId?: string
}

export interface TermInput {
  name: string
  kind: 'category' | 'tag'
}

function segment(value: string) {
  return encodeURIComponent(value)
}

function pageQuery(page: number, pageSize: number) {
  return new URLSearchParams({ page: String(page), pageSize: String(pageSize) })
}

export interface PostListQuery {
  search?: string
  category?: string
  tag?: string
  month?: string
  order?: 'updated' | 'published'
  includeDraft?: boolean
  status?: 'draft' | 'published'
  page?: number
  pageSize?: number
}

export function listPosts(query: PostListQuery = {}, signal?: AbortSignal) {
  const params = pageQuery(query.page ?? 1, query.pageSize ?? 20)
  if (query.search?.trim()) params.set('q', query.search.trim())
  if (query.category?.trim()) params.set('category', query.category.trim())
  if (query.tag?.trim()) params.set('tag', query.tag.trim())
  if (query.month?.trim()) params.set('month', query.month.trim())
  if (query.order) params.set('order', query.order)
  if (query.includeDraft) params.set('includeDraft', 'true')
  if (query.status) params.set('status', query.status)
	return api<PostPage>(`/api/v1/content/posts?${params.toString()}`, { signal })
}

export function getPost(slug: string, passphrase?: string) {
  const headers = new Headers()
  if (passphrase) headers.set('X-Post-Passphrase', passphrase)
  return api<Post>(`/api/v1/content/posts/${segment(slug)}`, { headers })
}

export function savePost(id: null, input: PostInput): Promise<Post>
export function savePost(id: string, input: PostUpdateInput): Promise<Post>
export function savePost(id: string | null, input: PostInput | PostUpdateInput): Promise<Post> {
  return id
    ? putJSON<Post>(`/api/v1/content/posts/${segment(id)}`, input)
    : postJSON<Post>('/api/v1/content/posts', input)
}

export function deletePost(id: string) {
  return api<void>(`/api/v1/content/posts/${segment(id)}`, { method: 'DELETE' })
}

export function listPages(includeDraft = false, page = 1, pageSize = 20) {
  const params = pageQuery(page, pageSize)
  if (includeDraft) params.set('includeDraft', 'true')
  return api<PagePage>(`/api/v1/content/pages?${params.toString()}`)
}

export function getPage(slug: string) {
  return api<Page>(`/api/v1/content/pages/${segment(slug)}`)
}

export function savePage(id: null, input: PageInput): Promise<Page>
export function savePage(id: string, input: PageUpdateInput): Promise<Page>
export function savePage(id: string | null, input: PageInput | PageUpdateInput): Promise<Page> {
  return id
    ? putJSON<Page>(`/api/v1/content/pages/${segment(id)}`, input)
    : postJSON<Page>('/api/v1/content/pages', input)
}

export function deletePage(id: string) {
  return api<void>(`/api/v1/content/pages/${segment(id)}`, { method: 'DELETE' })
}

export function listTerms(includeDraft = false, page = 1, pageSize = 100) {
  const params = pageQuery(page, pageSize)
  if (includeDraft) params.set('includeDraft', 'true')
  return api<TermPage>(`/api/v1/content/terms?${params.toString()}`)
}

export function createTerm(input: TermInput) {
  return postJSON<Term>('/api/v1/content/terms', input)
}

export function updateTerm(id: string, input: TermInput) {
  return putJSON<Term>(`/api/v1/content/terms/${segment(id)}`, input)
}

export function deleteTerm(id: string) {
  return api<void>(`/api/v1/content/terms/${segment(id)}`, { method: 'DELETE' })
}

export function listArchive() {
  return api<{ items: MonthCount[] }>('/api/v1/content/archive')
}

export function listRecentComments(page = 1, pageSize = 8) {
  const params = pageQuery(page, pageSize)
  return api<CommentPage>(`/api/v1/content/comments/recent?${params.toString()}`)
}

export function listPostComments(slug: string, page = 1, pageSize = 100, cursor?: string) {
  const params = pageQuery(page, pageSize)
  if (cursor) params.set('cursor', cursor)
  return api<CommentPage>(`/api/v1/content/posts/${segment(slug)}/comments?${params.toString()}`)
}

export function createComment(slug: string, input: CommentInput) {
  return postJSON<Comment>(`/api/v1/content/posts/${segment(slug)}/comments`, input)
}

export function listPendingComments(page = 1, pageSize = 100) {
  return listModerationComments('pending', page, pageSize)
}

export function listModerationComments(status: 'pending' | 'approved' | 'spam', page = 1, pageSize = 100) {
  const params = pageQuery(page, pageSize)
  params.set('status', status)
  return api<CommentPage>(`/api/v1/content/comments?${params.toString()}`)
}

export interface PostRevisionSummary {
  id: string
  postId: string
  title: string
  createdAt: string
}

export interface PostRevisionPage {
  items: PostRevisionSummary[]
  total: number
  page: number
  pageSize: number
}

export function listRevisions(id: string, page = 1, pageSize = 20) {
  const params = pageQuery(page, pageSize)
  return api<PostRevisionPage>(`/api/v1/content/posts/${segment(id)}/revisions?${params.toString()}`)
}

export function restoreRevision(postId: string, revisionId: string) {
  return postJSON<Post>(`/api/v1/content/posts/${segment(postId)}/revisions/${segment(revisionId)}/restore`, {})
}

export function moderateComment(id: string, status: 'approved' | 'spam' | 'pending') {
  return putJSON<Comment>(`/api/v1/content/comments/${segment(id)}`, { status })
}

export function deleteComment(id: string) {
  return api<void>(`/api/v1/content/comments/${segment(id)}`, { method: 'DELETE' })
}

export function listMedia(page = 1, pageSize = 100) {
  const params = pageQuery(page, pageSize)
  return api<MediaPage>(`/api/v1/media?${params.toString()}`)
}

export function uploadMedia(file: File) {
  const form = new FormData()
  form.append('file', file)
  return postForm<Media>('/api/v1/media', form)
}

export function deleteMedia(id: string) {
  return api<void>(`/api/v1/media/${segment(id)}`, { method: 'DELETE' })
}
