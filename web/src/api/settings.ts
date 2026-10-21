import { api, postJSON, putJSON } from '../api'
import type { AuditPage, ReadingSettings, SiteSettings, Stats, UserRecord } from '../types'

export interface SiteInput {
  name: string
  url: string
  adminMail: string
  timezone: string
  description: string
  keywords: string
}

export interface AccountInput {
  email: string
  currentPassword: string
  newPassword: string
}

export interface UserInput {
  username?: string
  email?: string
  password?: string
  role?: 'owner' | 'editor' | 'moderator'
  active?: boolean
}

export function getDashboardStats() {
  return api<Stats>('/api/v1/dashboard/stats')
}

export function updateSite(input: SiteInput) {
  return putJSON<SiteSettings>('/api/v1/settings/site', input)
}

export function getSite() {
  return api<SiteSettings>('/api/v1/settings/site')
}

export function getReading() {
  return api<ReadingSettings>('/api/v1/settings/reading')
}

export function updateReading(input: ReadingSettings) {
  return putJSON<ReadingSettings>('/api/v1/settings/reading', input)
}

export function updateAccount(input: AccountInput) {
  return putJSON('/api/v1/settings/account', input)
}

export function listUsers() {
  return api<{ items: UserRecord[] }>('/api/v1/settings/users')
}

export function createUser(input: Required<Pick<UserInput, 'username' | 'email' | 'password'>> & Pick<UserInput, 'role'>) {
  return postJSON<UserRecord>('/api/v1/settings/users', input)
}

export function updateUser(username: string, input: UserInput) {
  return putJSON<UserRecord>(`/api/v1/settings/users/${encodeURIComponent(username)}`, input)
}

export function deleteUser(username: string) {
  return api<void>(`/api/v1/settings/users/${encodeURIComponent(username)}`, { method: 'DELETE' })
}

export function listAudit(page = 1, pageSize = 20) {
  const params = new URLSearchParams({ page: String(page), pageSize: String(pageSize) })
  return api<AuditPage>(`/api/v1/audit?${params.toString()}`)
}
