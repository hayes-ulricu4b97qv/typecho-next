<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, reactive, ref } from 'vue'
import { api, ApiError, postJSON, putJSON } from '../api'
import { setDocumentMeta } from '../meta'
import { getReading, getSite, updateReading } from '../api/settings'
import type { AuditEvent, AuditPage, ReadingSettings, SiteSettings, Status, UserRecord } from '../types'

const props = defineProps<{ status: Status }>()
const initialized = props.status.initialized
const submitting = ref(false)
const message = ref('')
const error = ref('')
const accountSubmitting = ref(false)
const accountMessage = ref('')
const accountError = ref('')
const accountLoading = ref(false)
const canManageUsers = ref(false)
const accountChecked = ref(false)
const signedIn = ref(false)
const settingsAccessNotice = ref('')
const settingsSessionGeneration = ref(0)
const settingsRevalidating = ref(false)
const userActionId = ref('')
const userSaved = ref<Record<string, string>>({})
const settingsSection = ref('site')
let settingsObserver: IntersectionObserver | undefined
const users = ref<UserRecord[]>([])
const usersLoading = ref(false)
const userSubmitting = ref(false)
const usersMessage = ref('')
const usersError = ref('')
let usersLoadGeneration = 0
const siteLoading = ref(false)
const siteLoaded = ref(false)
const siteLoadError = ref('')
const auditEvents = ref<AuditEvent[]>([])
const auditLoading = ref(false)
const auditError = ref('')
const auditPage = ref(1)
const auditRetryPage = ref(1)
const auditTotal = ref(0)
const auditPageSize = 20
let auditLoadGeneration = 0
const form = reactive({
  siteName: props.status.site?.name ?? '',
  siteUrl: props.status.site?.url ?? window.location.origin,
  adminEmail: '',
  timezone: props.status.site?.timezone ?? 'Asia/Shanghai',
  description: props.status.site?.description ?? '',
  keywords: props.status.site?.keywords ?? '',
  adminUsername: '',
  adminPassword: '',
})
const accountForm = reactive({ email: '', currentPassword: '', newPassword: '' })
const userForm = reactive({ username: '', email: '', password: '', role: 'editor' as 'editor' | 'moderator' })
const readingForm = reactive<ReadingSettings>({
  postsPerPage: props.status.reading?.postsPerPage ?? 10,
  commentsEnabled: props.status.reading?.commentsEnabled ?? true,
  commentsRequireModeration: props.status.reading?.commentsRequireModeration ?? true,
  commentsThreaded: props.status.reading?.commentsThreaded ?? true,
})
const readingSubmitting = ref(false)
const readingMessage = ref('')
const readingError = ref('')
const readingLoadError = ref('')
const readingLoading = ref(false)
const readingLoaded = ref(false)
const siteSnapshot = () => JSON.stringify([form.siteName, form.siteUrl, form.adminEmail, form.timezone, form.description, form.keywords])
const savedSite = ref(siteSnapshot())
const savedReading = ref(JSON.stringify(readingForm))
const savedEmail = ref(accountForm.email)
const siteDirty = computed(() => siteSnapshot() !== savedSite.value)
const readingDirty = computed(() => JSON.stringify(readingForm) !== savedReading.value)
const accountDirty = computed(() => accountForm.email !== savedEmail.value || !!accountForm.currentPassword || !!accountForm.newPassword)
const userSnapshot = (user: UserRecord) => JSON.stringify([user.email, user.role, user.active])
const userDirty = (user: UserRecord) => userSnapshot(user) !== userSaved.value[user.id] || !!user.password
const unsaved = computed(() => siteDirty.value || readingDirty.value || accountDirty.value || users.value.some(userDirty) || !!userForm.username || !!userForm.email || !!userForm.password)

function clearOwnerSettings(notice: string, retainSignedIn = false) {
  settingsSessionGeneration.value++
  usersLoadGeneration++
  auditLoadGeneration++
  settingsObserver?.disconnect()
  settingsObserver = undefined
  canManageUsers.value = false
  signedIn.value = retainSignedIn
  settingsRevalidating.value = false
  accountChecked.value = true
  accountLoading.value = false
  accountError.value = ''
  settingsAccessNotice.value = notice
  Object.assign(form, { siteName: '', siteUrl: '', adminEmail: '', timezone: 'Asia/Shanghai', description: '', keywords: '', adminUsername: '', adminPassword: '' })
  savedSite.value = siteSnapshot()
  submitting.value = false
  message.value = ''
  error.value = ''
  siteLoading.value = false
  siteLoaded.value = false
  siteLoadError.value = ''
  Object.assign(accountForm, { email: '', currentPassword: '', newPassword: '' })
  savedEmail.value = ''
  accountSubmitting.value = false
  accountMessage.value = ''
  users.value = []
  userSaved.value = {}
  usersLoading.value = false
  userSubmitting.value = false
  usersMessage.value = ''
  usersError.value = ''
  userActionId.value = ''
  Object.assign(userForm, { username: '', email: '', password: '', role: 'editor' })
  Object.assign(readingForm, { postsPerPage: 10, commentsEnabled: true, commentsRequireModeration: true, commentsThreaded: true })
  savedReading.value = JSON.stringify(readingForm)
  readingSubmitting.value = false
  readingMessage.value = ''
  readingError.value = ''
  readingLoadError.value = ''
  readingLoading.value = false
  readingLoaded.value = false
  auditEvents.value = []
  auditLoading.value = false
  auditError.value = ''
  auditPage.value = 1
  auditRetryPage.value = 1
  auditTotal.value = 0
  settingsSection.value = 'site'
}

function handleSettingsFailure(reason: unknown, requestSession: number) {
  if (requestSession !== settingsSessionGeneration.value) return true
  if (signedIn.value && reason instanceof ApiError && reason.status === 401) {
    clearOwnerSettings('登录状态已过期，请重新登录。')
    return true
  }
  if (signedIn.value && reason instanceof ApiError && reason.status === 403 && reason.code === 'forbidden') {
    clearOwnerSettings('当前账户已没有所有者权限。', true)
    return true
  }
  return false
}

function confirmLeave(event: BeforeUnloadEvent) {
  if (initialized && canManageUsers.value && unsaved.value) {
    event.preventDefault()
    event.returnValue = ''
  }
}

function bindSettingsSections() {
  settingsObserver?.disconnect()
  const nodes = ['site', 'account', 'reading', 'users', 'audit']
    .map((id) => document.getElementById(id))
    .filter((node): node is HTMLElement => node instanceof HTMLElement)
  if (nodes.length === 0) return
  settingsObserver = new IntersectionObserver((entries) => {
    const visible = entries.filter((entry) => entry.isIntersecting).sort((left, right) => right.intersectionRatio - left.intersectionRatio)[0]
    if (visible?.target instanceof HTMLElement && visible.target.id) settingsSection.value = visible.target.id
  }, { rootMargin: '-150px 0px -55% 0px', threshold: 0 })
  for (const node of nodes) settingsObserver.observe(node)
}

async function loadAccount(knownSignedIn = false) {
  if (!initialized || accountLoading.value) return
  const requestSession = settingsSessionGeneration.value
  accountLoading.value = true
  accountChecked.value = false
  accountError.value = ''
  settingsAccessNotice.value = ''
  signedIn.value = false
  canManageUsers.value = false
  try {
    const response = await api<{ user: { email: string; role?: string } }>('/api/v1/auth/me')
    if (requestSession !== settingsSessionGeneration.value) return
    signedIn.value = true
    accountForm.email = response.user.email
    savedEmail.value = response.user.email
    canManageUsers.value = response.user.role === 'owner'
    if (canManageUsers.value) {
      await Promise.all([loadSite(), loadUsers(), loadAudit(), loadReading()])
      if (requestSession !== settingsSessionGeneration.value) return
      await nextTick()
      if (requestSession === settingsSessionGeneration.value && canManageUsers.value) bindSettingsSections()
    }
  } catch (reason) {
    if (handleSettingsFailure(reason, requestSession)) return
    if (reason instanceof ApiError && reason.status === 401) {
      if (knownSignedIn) settingsAccessNotice.value = '登录状态已过期，请重新登录。'
    } else {
      accountError.value = reason instanceof ApiError ? reason.message : '无法读取账户信息'
    }
  } finally {
    if (requestSession === settingsSessionGeneration.value) {
      accountChecked.value = true
      accountLoading.value = false
    }
  }
}

async function loadSite() {
  if (siteLoading.value) return
  const requestSession = settingsSessionGeneration.value
  siteLoading.value = true
  siteLoaded.value = false
  siteLoadError.value = ''
  try {
    const site = await getSite()
    if (requestSession !== settingsSessionGeneration.value) return
    form.siteName = site.name
    form.siteUrl = site.url
    form.adminEmail = site.adminMail
    form.timezone = site.timezone
    form.description = site.description
    form.keywords = site.keywords
    savedSite.value = siteSnapshot()
    siteLoaded.value = true
  } catch (reason) {
    if (handleSettingsFailure(reason, requestSession)) return
    siteLoadError.value = reason instanceof ApiError ? reason.message : '无法读取站点信息'
  } finally {
    if (requestSession === settingsSessionGeneration.value) siteLoading.value = false
  }
}

async function loadAudit(requestedPage = auditPage.value) {
  const requestSession = settingsSessionGeneration.value
  const generation = ++auditLoadGeneration
  auditRetryPage.value = requestedPage
  auditLoading.value = true
  auditError.value = ''
  try {
    let response = await api<AuditPage>(`/api/v1/audit?page=${requestedPage}&pageSize=${auditPageSize}`)
    if (generation !== auditLoadGeneration || requestSession !== settingsSessionGeneration.value) return
    const lastPage = Math.max(1, Math.ceil(response.total / auditPageSize))
    if (requestedPage > lastPage) {
      response = await api<AuditPage>(`/api/v1/audit?page=${lastPage}&pageSize=${auditPageSize}`)
      if (generation !== auditLoadGeneration || requestSession !== settingsSessionGeneration.value) return
    }
    auditEvents.value = response.items
    auditTotal.value = response.total
    auditPage.value = response.page
  } catch (reason) {
    if (generation === auditLoadGeneration && !handleSettingsFailure(reason, requestSession)) {
      auditError.value = reason instanceof ApiError ? reason.message : '无法读取审计记录'
    }
  } finally {
    if (generation === auditLoadGeneration && requestSession === settingsSessionGeneration.value) auditLoading.value = false
  }
}

async function loadReading() {
  if (readingLoading.value) return
  const requestSession = settingsSessionGeneration.value
  readingLoading.value = true
  readingLoadError.value = ''
  try {
    const reading = await getReading()
    if (requestSession !== settingsSessionGeneration.value) return
    Object.assign(readingForm, reading)
    savedReading.value = JSON.stringify(readingForm)
    readingLoaded.value = true
  } catch (reason) {
    if (handleSettingsFailure(reason, requestSession)) return
    readingLoadError.value = reason instanceof ApiError ? reason.message : '无法读取阅读设置'
  } finally {
    if (requestSession === settingsSessionGeneration.value) readingLoading.value = false
  }
}

async function saveReading() {
  if (!readingLoaded.value || readingLoading.value || readingSubmitting.value) return
  const requestSession = settingsSessionGeneration.value
  readingSubmitting.value = true
  readingMessage.value = ''
  readingError.value = ''
  try {
    const reading = await updateReading({ ...readingForm })
    if (requestSession !== settingsSessionGeneration.value) return
    Object.assign(readingForm, reading)
    savedReading.value = JSON.stringify(readingForm)
    readingMessage.value = '阅读与评论设置已保存'
  } catch (reason) {
    if (handleSettingsFailure(reason, requestSession)) return
    readingError.value = reason instanceof ApiError ? reason.message : '保存阅读设置失败'
  } finally {
    if (requestSession === settingsSessionGeneration.value) readingSubmitting.value = false
  }
}

async function loadUsers() {
  const requestSession = settingsSessionGeneration.value
  const generation = ++usersLoadGeneration
  usersLoading.value = true
  usersError.value = ''
  try {
    const response = await api<{ items: UserRecord[] }>('/api/v1/settings/users')
    if (generation !== usersLoadGeneration || requestSession !== settingsSessionGeneration.value) return
    const pending = new Map(users.value.filter(userDirty).map(user => [user.id, user]))
    users.value = response.items.map(user => pending.has(user.id) ? { ...user, ...pending.get(user.id) } : user)
    userSaved.value = Object.fromEntries(response.items.map(user => [user.id, userSnapshot(user)]))
  } catch (reason) {
    if (generation === usersLoadGeneration && !handleSettingsFailure(reason, requestSession)) {
      usersError.value = reason instanceof ApiError ? reason.message : '无法读取用户列表'
    }
  } finally {
    if (generation === usersLoadGeneration && requestSession === settingsSessionGeneration.value) usersLoading.value = false
  }
}

async function submit() {
  if (submitting.value) return
  if (initialized && (!canManageUsers.value || !accountChecked.value || !siteLoaded.value || siteLoading.value)) return
  const requestSession = settingsSessionGeneration.value
  submitting.value = true
  message.value = ''
  error.value = ''
  try {
    if (!initialized) {
      await postJSON('/api/v1/setup', {
        siteName: form.siteName,
        siteUrl: form.siteUrl,
        adminUsername: form.adminUsername,
        adminPassword: form.adminPassword,
        adminEmail: form.adminEmail,
        timezone: form.timezone,
      })
      if (requestSession !== settingsSessionGeneration.value) return
      window.location.replace('/admin')
      return
    }
    await putJSON<SiteSettings>('/api/v1/settings/site', {
      name: form.siteName,
      url: form.siteUrl,
      adminMail: form.adminEmail,
      timezone: form.timezone,
      description: form.description,
      keywords: form.keywords,
    })
    if (requestSession !== settingsSessionGeneration.value) return
    message.value = '设置已保存'
    savedSite.value = siteSnapshot()
    setDocumentMeta(`${form.siteName} · 设置`, form.description, form.keywords)
  } catch (reason) {
    if (handleSettingsFailure(reason, requestSession)) return
    error.value = reason instanceof ApiError ? reason.message : '保存失败'
  } finally {
    if (requestSession === settingsSessionGeneration.value) submitting.value = false
  }
}

async function submitAccount() {
  if (!canManageUsers.value || accountSubmitting.value) return
  const requestSession = settingsSessionGeneration.value
  accountSubmitting.value = true
  accountMessage.value = ''
  accountError.value = ''
  try {
    await putJSON('/api/v1/settings/account', accountForm)
    if (requestSession !== settingsSessionGeneration.value) return
    accountForm.currentPassword = ''
    accountForm.newPassword = ''
    savedEmail.value = accountForm.email
    accountMessage.value = '账户信息已保存'
  } catch (reason) {
    if (handleSettingsFailure(reason, requestSession)) return
    accountError.value = reason instanceof ApiError ? reason.message : '保存账户信息失败'
  } finally {
    if (requestSession === settingsSessionGeneration.value) accountSubmitting.value = false
  }
}

async function createUser() {
  if (!canManageUsers.value || userSubmitting.value) return
  const requestSession = settingsSessionGeneration.value
  userSubmitting.value = true
  usersMessage.value = ''
  usersError.value = ''
  try {
    await postJSON('/api/v1/settings/users', userForm)
    if (requestSession !== settingsSessionGeneration.value) return
    Object.assign(userForm, { username: '', email: '', password: '', role: 'editor' })
    usersMessage.value = '用户已创建'
    await loadUsers()
  } catch (reason) {
    if (handleSettingsFailure(reason, requestSession)) return
    usersError.value = reason instanceof ApiError ? reason.message : '创建用户失败'
  } finally {
    if (requestSession === settingsSessionGeneration.value) userSubmitting.value = false
  }
}

async function updateUser(user: UserRecord) {
  if (!canManageUsers.value || userActionId.value) return
  const requestSession = settingsSessionGeneration.value
  userActionId.value = user.id
  usersError.value = ''
  usersMessage.value = ''
  try {
    await putJSON(`/api/v1/settings/users/${encodeURIComponent(user.username)}`, { email: user.email, role: user.role, active: user.active, password: user.password?.trim() || undefined })
    if (requestSession !== settingsSessionGeneration.value) return
    user.password = ''
    userSaved.value[user.id] = userSnapshot(user)
    usersMessage.value = '用户信息已保存'
  } catch (reason) {
    if (handleSettingsFailure(reason, requestSession)) return
    usersError.value = reason instanceof ApiError ? reason.message : '保存用户失败'
  } finally {
    if (requestSession === settingsSessionGeneration.value) userActionId.value = ''
  }
}

async function removeUser(user: UserRecord) {
  if (!canManageUsers.value || userActionId.value) return
  if (!window.confirm(`确定删除用户“${user.username}”吗？`)) return
  const requestSession = settingsSessionGeneration.value
  userActionId.value = user.id
  usersError.value = ''
  usersMessage.value = ''
  try {
    await api(`/api/v1/settings/users/${encodeURIComponent(user.username)}`, { method: 'DELETE' })
    if (requestSession !== settingsSessionGeneration.value) return
    usersMessage.value = '用户已删除'
    await loadUsers()
  } catch (reason) {
    if (handleSettingsFailure(reason, requestSession)) return
    usersError.value = reason instanceof ApiError ? reason.message : '删除用户失败'
  } finally {
    if (requestSession === settingsSessionGeneration.value) userActionId.value = ''
  }
}

function changeAuditPage(next: number) {
  const last = Math.max(1, Math.ceil(auditTotal.value / auditPageSize))
  if (auditLoading.value || next < 1 || next > last || next === auditPage.value) return
  void loadAudit(next)
}

function onPageShow(event: PageTransitionEvent) {
  if (!event.persisted || !canManageUsers.value) return
  clearOwnerSettings('')
  settingsRevalidating.value = true
  const generation = settingsSessionGeneration.value
  void loadAccount(true).finally(() => {
    if (generation === settingsSessionGeneration.value) settingsRevalidating.value = false
  })
}

onMounted(() => {
  window.addEventListener('beforeunload', confirmLeave)
  window.addEventListener('pageshow', onPageShow)
  const site = props.status.site
  setDocumentMeta(site?.name ? `${site.name} · 设置` : '设置', site?.description ?? '', site?.keywords ?? '')
  void loadAccount()
})

onUnmounted(() => {
  settingsSessionGeneration.value++
  usersLoadGeneration++
  auditLoadGeneration++
  settingsObserver?.disconnect()
  window.removeEventListener('beforeunload', confirmLeave)
  window.removeEventListener('pageshow', onPageShow)
})
</script>

<template>
  <main class="settings-shell" :class="{ 'is-setup': !initialized }">
    <header class="settings-bar">
      <a class="brand" href="/user"><span class="brand-mark">{{ status.site?.name?.trim().slice(0, 1) || '博' }}</span><span class="brand-name">{{ status.site?.name ?? '博客' }}</span></a>
      <nav class="settings-links"><a href="/admin">管理</a><a href="/user">阅读</a></nav>
    </header>
    <p v-if="settingsRevalidating" class="settings-access" role="status">正在重新确认账户权限…</p>
    <div v-else class="settings-frame">
      <nav v-if="initialized && canManageUsers" class="settings-index" aria-label="设置分区">
        <a href="#site" :aria-current="settingsSection === 'site' ? 'location' : undefined" :class="{ active: settingsSection === 'site' }">站点信息</a>
        <a href="#account" :aria-current="settingsSection === 'account' ? 'location' : undefined" :class="{ active: settingsSection === 'account' }">账户安全</a>
        <a href="#reading" :aria-current="settingsSection === 'reading' ? 'location' : undefined" :class="{ active: settingsSection === 'reading' }">阅读与评论</a>
        <a href="#users" :aria-current="settingsSection === 'users' ? 'location' : undefined" :class="{ active: settingsSection === 'users' }">用户与权限</a>
        <a href="#audit" :aria-current="settingsSection === 'audit' ? 'location' : undefined" :class="{ active: settingsSection === 'audit' }">操作记录</a>
      </nav>
      <div class="settings-main">
        <p v-if="initialized && !accountChecked" class="settings-access" role="status">正在读取设置…</p>
        <section v-if="!initialized || canManageUsers" id="site" class="settings-block">
          <p class="eyebrow">{{ initialized ? '站点' : '第一次设置' }}</p>
          <h2>{{ initialized ? '站点信息' : '创建站点和管理员' }}</h2>
          <p class="settings-lead">{{ initialized ? '名称、根地址和描述会用于阅读页、订阅源与搜索引擎信息。站点地址只填协议和域名，例如 https://example.com。' : '填完这一张表，管理端和阅读页就会打开。站点地址只填协议和域名，例如 https://example.com。' }}</p>
          <form @submit.prevent="submit">
            <fieldset class="settings-fieldset" :disabled="submitting || (initialized && (!accountChecked || !siteLoaded || siteLoading))">
              <legend>站点</legend>
              <label>站点名称<input v-model="form.siteName" required maxlength="80" placeholder="例如：我的新博客" /></label>
              <label>站点地址<input v-model="form.siteUrl" required type="url" placeholder="https://example.com" /></label>
              <template v-if="initialized">
                <label>站点描述<textarea v-model="form.description" maxlength="2000" rows="3" placeholder="用一两句话介绍这个站点"></textarea></label>
                <label>关键词<input v-model="form.keywords" maxlength="500" placeholder="用逗号分隔，例如：笔记, Go" /></label>
              </template>
              <div class="form-grid">
                <label>管理员邮箱<input v-model="form.adminEmail" required type="email" autocomplete="email" placeholder="you@example.com" /></label>
                <label>时区
                  <input v-model.trim="form.timezone" list="site-timezone-options" required maxlength="128" autocomplete="off" placeholder="例如 Europe/Berlin" aria-describedby="site-timezone-hint" />
                  <small id="site-timezone-hint" class="field-hint">输入有效的 IANA 时区名称；列表中的常用时区仅作建议。</small>
                </label>
              </div>
              <datalist id="site-timezone-options">
                <option value="Asia/Shanghai" />
                <option value="Asia/Tokyo" />
                <option value="Asia/Singapore" />
                <option value="Asia/Kolkata" />
                <option value="UTC" />
                <option value="Europe/London" />
                <option value="Europe/Berlin" />
                <option value="America/Los_Angeles" />
                <option value="America/New_York" />
                <option value="Australia/Sydney" />
              </datalist>
            </fieldset>
            <div v-if="initialized && siteLoading" class="user-state" role="status">正在读取站点信息…</div>
            <p v-if="siteLoadError" class="form-error" role="alert">{{ siteLoadError }}</p>
            <button v-if="siteLoadError" class="text-button" type="button" :disabled="siteLoading" @click="loadSite">重试读取站点信息</button>
            <fieldset v-if="!initialized" class="settings-fieldset">
              <legend>管理员登录</legend>
              <div class="form-grid">
                <label>用户名<input v-model="form.adminUsername" required minlength="3" maxlength="32" pattern="[A-Za-z0-9_-]+" autocomplete="username" autocapitalize="none" spellcheck="false" placeholder="admin" /></label>
                <label>密码<input v-model="form.adminPassword" required minlength="8" type="password" autocomplete="new-password" placeholder="至少 8 位" /></label>
              </div>
            </fieldset>
            <p v-if="error" class="form-error" role="alert">{{ error }}</p>
            <div class="settings-save-row">
              <span v-if="initialized" role="status" :class="{ 'settings-unsaved': siteDirty }">{{ siteDirty ? '有未保存的修改' : message || '当前设置已保存' }}</span>
              <button class="button button-primary" :disabled="submitting || (initialized && (!siteLoaded || !siteDirty))">{{ submitting ? '保存中…' : initialized ? '保存设置' : '完成设置并进入管理端' }}</button>
            </div>
          </form>
        </section>
        <section v-if="initialized && canManageUsers" id="account" class="settings-block settings-account">
          <p class="eyebrow">账户</p>
          <h2>管理员账户</h2>
          <p class="settings-lead">修改邮箱，或同时填写当前密码和新密码来更换登录密码。</p>
          <form @submit.prevent="submitAccount">
            <label>账户邮箱<input v-model="accountForm.email" :disabled="accountSubmitting" required type="email" autocomplete="email" placeholder="you@example.com" /></label>
            <label>当前密码<input v-model="accountForm.currentPassword" :disabled="accountSubmitting" :required="!!accountForm.newPassword" type="password" autocomplete="current-password" placeholder="修改密码时填写" /></label>
            <label>新密码<input v-model="accountForm.newPassword" :disabled="accountSubmitting" :required="!!accountForm.currentPassword" minlength="8" type="password" autocomplete="new-password" placeholder="留空则不修改密码" /></label>
            <p v-if="accountError" class="form-error" role="alert">{{ accountError }}</p>
            <div class="settings-save-row">
              <span role="status" :class="{ 'settings-unsaved': accountDirty }">{{ accountDirty ? '有未保存的修改' : accountMessage || '当前账户信息已保存' }}</span>
              <button class="button button-dark" :disabled="accountSubmitting || !accountDirty">{{ accountSubmitting ? '保存中…' : '保存账户信息' }}</button>
            </div>
          </form>
        </section>
        <section v-if="initialized && canManageUsers" id="reading" class="settings-block settings-reading">
          <p class="eyebrow">阅读</p>
          <h2>阅读与评论</h2>
          <p class="settings-lead">保存后应用到文章列表和全站评论。单篇文章也可以单独关闭评论。</p>
          <form @submit.prevent="saveReading">
            <label>每页文章数<input v-model.number="readingForm.postsPerPage" :disabled="readingSubmitting || readingLoading || !readingLoaded" required type="number" min="1" max="50" /></label>
            <label class="check-row"><input v-model="readingForm.commentsEnabled" :disabled="readingSubmitting || readingLoading || !readingLoaded" type="checkbox" />开放评论</label>
            <label class="check-row"><input v-model="readingForm.commentsRequireModeration" :disabled="readingSubmitting || readingLoading || !readingLoaded || !readingForm.commentsEnabled" type="checkbox" />新评论需要审核</label>
            <label class="check-row"><input v-model="readingForm.commentsThreaded" :disabled="readingSubmitting || readingLoading || !readingLoaded || !readingForm.commentsEnabled" type="checkbox" />允许回复评论，最多三层</label>
            <div v-if="readingLoading" class="user-state" role="status">正在读取阅读设置…</div>
            <p v-if="readingLoadError" class="form-error" role="alert">{{ readingLoadError }}</p>
            <button v-if="readingLoadError" class="text-button" type="button" :disabled="readingLoading" @click="loadReading">重试读取设置</button>
            <p v-if="readingError" class="form-error" role="alert">{{ readingError }}</p>
            <div class="settings-save-row">
              <span role="status" :class="{ 'settings-unsaved': readingDirty }">{{ readingDirty ? '有未保存的修改' : readingMessage || '当前阅读设置已保存' }}</span>
              <button class="button button-dark" :disabled="readingSubmitting || readingLoading || !readingLoaded || !readingDirty">{{ readingSubmitting ? '保存中…' : '保存阅读与评论设置' }}</button>
            </div>
          </form>
        </section>
        <section v-if="initialized && canManageUsers" id="users" class="settings-block settings-users">
          <p class="eyebrow">用户</p>
          <h2>用户与权限</h2>
          <p class="settings-lead">编辑负责文章和媒体，审核员负责评论。取消启用会立即停用该账户。修改角色或密码后，点该用户的保存。</p>
          <form class="user-create-form" @submit.prevent="createUser">
            <div class="form-grid"><label>用户名<input v-model="userForm.username" required minlength="3" maxlength="32" pattern="[A-Za-z0-9_-]+" autocomplete="username" autocapitalize="none" spellcheck="false" placeholder="editor" /></label><label>邮箱<input v-model="userForm.email" required type="email" autocomplete="email" placeholder="editor@example.com" /></label></div>
            <div class="form-grid"><label>初始密码<input v-model="userForm.password" required minlength="8" type="password" autocomplete="new-password" placeholder="至少 8 位" /></label><label>角色<select v-model="userForm.role"><option value="editor">编辑</option><option value="moderator">审核员</option></select></label></div>
            <button class="button button-dark" :disabled="userSubmitting">{{ userSubmitting ? '创建中…' : '创建用户' }}</button>
          </form>
          <p v-if="usersError" class="form-error" role="alert">{{ usersError }}</p>
          <button v-if="usersError" class="text-button" type="button" :disabled="usersLoading" @click="loadUsers">重试加载用户</button>
          <p v-if="usersMessage" class="form-success" role="status">{{ usersMessage }}</p>
          <div v-if="usersLoading" class="user-state" role="status">正在加载用户…</div>
          <div v-else class="user-list">
            <div v-for="managedUser in users" :key="managedUser.id" class="user-row">
              <div class="user-row-main"><strong>{{ managedUser.username }}</strong><span>{{ managedUser.email }}</span></div>
              <select v-model="managedUser.role" :aria-label="`${managedUser.username} 的角色`" :disabled="managedUser.role === 'owner' || !!userActionId"><option v-if="managedUser.role === 'owner'" value="owner">所有者</option><option value="editor">编辑</option><option value="moderator">审核员</option></select>
              <input v-if="managedUser.role !== 'owner'" v-model="managedUser.password" :disabled="!!userActionId" :aria-label="`${managedUser.username} 的新密码`" class="user-password" type="password" minlength="8" autocomplete="new-password" placeholder="重置密码" @keyup.enter="updateUser(managedUser)" />
              <label class="user-active"><input v-model="managedUser.active" type="checkbox" :disabled="managedUser.role === 'owner' || !!userActionId" @change="updateUser(managedUser)" />启用</label>
              <span v-if="managedUser.role !== 'owner' && userDirty(managedUser)" class="settings-unsaved">未保存</span>
              <button v-if="managedUser.role !== 'owner'" class="text-button" type="button" :disabled="!!userActionId || !userDirty(managedUser)" @click="updateUser(managedUser)">{{ userActionId === managedUser.id ? '处理中…' : '保存' }}</button>
              <button v-if="managedUser.role !== 'owner'" class="text-button danger" type="button" :disabled="!!userActionId" @click="removeUser(managedUser)">删除</button>
            </div>
          </div>
        </section>
        <section v-if="initialized && canManageUsers" id="audit" class="settings-block settings-audit">
          <p class="eyebrow">记录</p>
          <h2>操作记录</h2>
          <p class="settings-lead">成功的内容、媒体、评论、用户和站点变更会记在这里。</p>
          <p v-if="auditError" class="form-error" role="alert">{{ auditError }}</p>
          <button v-if="auditError" class="text-button" type="button" :disabled="auditLoading" @click="loadAudit(auditRetryPage)">重试加载记录</button>
          <div v-if="auditLoading" class="user-state" role="status">正在加载操作记录…</div>
          <div v-else-if="!auditError && auditEvents.length === 0" class="user-state">还没有操作记录</div>
          <div v-else-if="auditEvents.length > 0" class="audit-list">
            <article v-for="event in auditEvents" :key="event.id" class="audit-row">
              <div><strong>{{ event.action }}</strong><span>{{ event.resourceType }}{{ event.resourceId ? ` · ${event.resourceId}` : '' }}</span></div>
              <div><span>{{ event.actorUsername }}</span><time>{{ new Date(event.createdAt).toLocaleString('zh-CN') }}</time></div>
            </article>
          </div>
          <div v-if="auditTotal > auditPageSize" class="pagination"><button class="text-button" type="button" :disabled="auditLoading || auditPage === 1" @click="changeAuditPage(auditPage - 1)">上一页</button><span>{{ auditPage }} / {{ Math.ceil(auditTotal / auditPageSize) }} · 共 {{ auditTotal }} 条</span><button class="text-button" type="button" :disabled="auditLoading || auditPage >= Math.ceil(auditTotal / auditPageSize)" @click="changeAuditPage(auditPage + 1)">下一页</button></div>
        </section>
        <div v-if="initialized && accountChecked && !canManageUsers" class="settings-access">
          <h2>{{ accountError ? '无法读取账户权限' : signedIn ? '需要所有者权限' : '请先登录' }}</h2>
          <p>{{ accountError ? '暂时无法确认当前账户权限，请检查连接后重试。' : settingsAccessNotice || (signedIn ? '当前账户可以在管理端处理权限范围内的内容。站点和账户设置由所有者管理。' : '登录所有者账户后，即可修改站点设置。') }}</p>
          <button v-if="accountError" class="button button-primary" type="button" :disabled="accountLoading" @click="loadAccount()">重试读取账户</button>
          <a v-else class="button button-primary" href="/admin">{{ signedIn ? '返回管理端' : settingsAccessNotice ? '重新登录' : '前往登录' }}</a>
          <p v-if="accountError" class="form-error" role="alert">{{ accountError }}</p>
        </div>
        <p class="card-note">数据保存在服务端数据目录中，密码经过单向哈希处理。</p>
      </div>
    </div>
  </main>
</template>
