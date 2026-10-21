export interface Site {
  name: string
  url: string
  timezone: string
  description: string
  keywords: string
}

export interface SiteSettings extends Site {
  adminMail: string
}

export interface ReadingSettings {
  postsPerPage: number
  commentsEnabled: boolean
  commentsRequireModeration: boolean
  commentsThreaded: boolean
}

export interface Status {
  initialized: boolean
  version: string
  site?: Site
  reading?: ReadingSettings
}

export interface User {
  username: string
  email: string
  role?: 'owner' | 'editor' | 'moderator'
  active?: boolean
}

export interface UserRecord {
  id: string
  username: string
  email: string
  role: 'owner' | 'editor' | 'moderator'
  active: boolean
  createdAt: string
  password?: string
}

export interface PostNeighbor {
  id: string
  title: string
  slug: string
  publishedAt?: string
}

export interface Post {
  id: string
  title: string
  slug: string
  excerpt: string
  content?: string
  customFields?: Record<string, string>
  locked?: boolean
  status: 'draft' | 'published'
  allowComment?: boolean
  categories?: Term[]
  tags?: Term[]
  publishedAt?: string
  updatedAt: string
  previous?: PostNeighbor
  next?: PostNeighbor
}

export interface MonthCount {
  month: string
  count: number
}

export interface Term {
  id: string
  name: string
  slug: string
  kind: 'category' | 'tag'
  postCount: number
}

export interface TermPage {
  items: Term[]
  total: number
  page: number
  pageSize: number
}

export interface PostPage {
  items: Post[]
  total: number
  page: number
  pageSize: number
}

export interface Page {
  id: string
  title: string
  slug: string
  excerpt: string
  content: string
  status: 'draft' | 'published'
  publishedAt?: string
  updatedAt: string
}

export interface PagePage {
  items: Page[]
  total: number
  page: number
  pageSize: number
}

export interface Comment {
  id: string
  postId?: string
  parentId?: string
  postTitle?: string
  postSlug?: string
  authorName: string
  authorEmail?: string
  authorUrl?: string
  content: string
  status: 'pending' | 'approved' | 'spam'
  createdAt: string
  updatedAt: string
}

export interface CommentPage {
  items: Comment[]
  total: number
  page: number
  pageSize: number
  nextCursor?: string
}

export interface Media {
  id: string
  filename: string
  contentType: string
  size: number
  url: string
  createdAt: string
}

export interface MediaPage {
  items: Media[]
  total: number
  page: number
  pageSize: number
}

export interface AuditEvent {
  id: string
  actorUserId?: string
  actorUsername: string
  action: string
  resourceType: string
  resourceId?: string
  details?: Record<string, string>
  createdAt: string
}

export interface AuditPage {
  items: AuditEvent[]
  total: number
  page: number
  pageSize: number
}

export interface OutboundMail {
  id: string
  recipient: string
  subject: string
  body: string
  createdAt: string
}

export interface OutboundMailPage {
	items: OutboundMail[]
	total: number
	page: number
	pageSize: number
}

export interface Stats {
  posts: number
  publishedPosts: number
  drafts: number
  comments: number
  pendingComments: number
}
