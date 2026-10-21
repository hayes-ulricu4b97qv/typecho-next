<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { ApiError } from '../api'
import { listMedia, uploadMedia } from '../api/content'
import type { Media } from '../types'

const props = defineProps<{ sessionGeneration: number }>()
const emit = defineEmits<{
  close: []
  select: [selection: { asset: Media; altText: string }]
  sessionExpired: [sessionGeneration: number]
  accessRevoked: [sessionGeneration: number]
}>()

const pageSize = 24
const dialog = ref<HTMLDialogElement | null>(null)
const uploadInput = ref<HTMLInputElement | null>(null)
const items = ref<Media[]>([])
const page = ref(1)
const total = ref(0)
const loading = ref(false)
const uploading = ref(false)
const error = ref('')
const altText = ref('图片说明')
let requestGeneration = 0

function close() {
  if (uploading.value) return
  if (dialog.value?.open) dialog.value.close()
}

function closeFromBackdrop(event: MouseEvent) {
  const bounds = dialog.value?.getBoundingClientRect()
  if (!bounds) return
  const outside = event.clientX < bounds.left || event.clientX > bounds.right || event.clientY < bounds.top || event.clientY > bounds.bottom
  if (outside) close()
}

function handleRequestFailure(reason: unknown) {
  if (!(reason instanceof ApiError)) return false
  if (reason.status === 401) {
    emit('sessionExpired', props.sessionGeneration)
    close()
    return true
  }
  if (reason.status === 403 && reason.code === 'forbidden') {
    emit('accessRevoked', props.sessionGeneration)
    close()
    return true
  }
  error.value = reason.message
  return true
}

async function loadMedia(nextPage = 1) {
  const generation = ++requestGeneration
  loading.value = true
  error.value = ''
  try {
    const response = await listMedia(nextPage, pageSize)
    if (generation !== requestGeneration) return
    items.value = response.items
    page.value = response.page
    total.value = response.total
  } catch (reason) {
    if (generation !== requestGeneration) return
    if (!handleRequestFailure(reason)) error.value = '无法读取媒体库'
  } finally {
    if (generation === requestGeneration) loading.value = false
  }
}

function choose(asset: Media) {
  emit('select', { asset, altText: altText.value.trim() || '图片说明' })
  close()
}

async function uploadAndInsert(event: Event) {
  if (uploading.value) return
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  uploading.value = true
  error.value = ''
  try {
    const asset = await uploadMedia(file)
    choose(asset)
  } catch (reason) {
    if (!handleRequestFailure(reason)) error.value = reason instanceof Error ? reason.message : '上传图片失败'
  } finally {
    input.value = ''
    uploading.value = false
  }
}

onMounted(() => {
  dialog.value?.showModal()
  void loadMedia()
})

onBeforeUnmount(() => {
  requestGeneration++
})
</script>

<template>
  <dialog ref="dialog" class="media-picker-dialog" aria-labelledby="media-picker-title" :aria-busy="loading || uploading" @cancel.prevent="close" @close="emit('close')" @click="closeFromBackdrop">
    <header class="media-picker-header">
      <div><p class="eyebrow">媒体库</p><h2 id="media-picker-title">插入图片</h2></div>
      <button class="text-button" type="button" :disabled="uploading" @click="close">关闭</button>
    </header>
    <div class="media-picker-toolbar">
      <label>替代文字<input v-model="altText" maxlength="200" placeholder="图片说明" /></label>
      <label class="button button-primary media-picker-upload"><input ref="uploadInput" type="file" accept="image/jpeg,image/png,image/gif,image/webp" :disabled="uploading" @change="uploadAndInsert" /><span>{{ uploading ? '上传中…' : '上传并插入' }}</span></label>
    </div>
    <p v-if="error" class="form-error" role="alert">{{ error }}</p>
    <div v-if="loading" class="empty-state media-picker-state" role="status">正在读取图片…</div>
    <div v-else-if="error && items.length === 0" class="empty-state media-picker-state"><button class="button" type="button" @click="loadMedia(page)">重试</button></div>
    <div v-else-if="items.length === 0" class="empty-state media-picker-state">媒体库还是空的，上传一张图片即可插入正文。</div>
    <div v-else class="media-picker-grid">
      <button v-for="asset in items" :key="asset.id" class="media-picker-item" type="button" :disabled="uploading" @click="choose(asset)">
        <img :src="asset.url" :alt="asset.filename" loading="lazy" />
        <span :title="asset.filename">{{ asset.filename }}</span>
        <small>{{ Math.ceil(asset.size / 1024) }} KB</small>
      </button>
    </div>
    <nav v-if="total > pageSize" class="pagination media-picker-pagination" aria-label="媒体库分页">
      <button class="text-button" type="button" :disabled="loading || page === 1" @click="loadMedia(page - 1)">上一页</button>
      <span>{{ page }} / {{ Math.ceil(total / pageSize) }} · 共 {{ total }} 张</span>
      <button class="text-button" type="button" :disabled="loading || page >= Math.ceil(total / pageSize)" @click="loadMedia(page + 1)">下一页</button>
    </nav>
  </dialog>
</template>
