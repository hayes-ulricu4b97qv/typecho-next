export class ApiError extends Error {
  status: number
  code: string
  requestId: string

  constructor(status: number, code: string, message: string, requestId: string) {
    super(status >= 500 && requestId ? `${message}（请求编号：${requestId}）` : message)
    this.status = status
    this.code = code
    this.requestId = requestId
  }
}

async function apiErrorFromResponse(response: Response) {
	const payload = await response.json().catch(() => ({}))
	const error = payload?.error
	return new ApiError(
		response.status,
		error?.code ?? 'request_failed',
		error?.message ?? '请求失败',
		response.headers.get('X-Request-ID') ?? '',
	)
}

function cookieValue(name: string) {
	if (typeof document === 'undefined') return ''
	const prefix = `${encodeURIComponent(name)}=`
	const cookie = document.cookie.split(';').map((entry) => entry.trimStart()).find((entry) => entry.startsWith(prefix))
	return cookie ? cookie.slice(prefix.length) : ''
}

export async function api<T>(path: string, init: RequestInit = {}): Promise<T> {
	const headers = new Headers(init.headers)
	const isFormData = typeof FormData !== 'undefined' && init.body instanceof FormData
	const method = (init.method ?? 'GET').toUpperCase()
	if (init.body != null && !isFormData && !headers.has('Content-Type')) {
		headers.set('Content-Type', 'application/json')
	}
	if (!['GET', 'HEAD', 'OPTIONS'].includes(method) && !headers.has('X-CSRF-Token')) {
		const csrfToken = cookieValue('app_csrf')
		if (csrfToken) headers.set('X-CSRF-Token', csrfToken)
	}
  const response = await fetch(path, {
		...init,
    credentials: 'include',
		headers,
  })
  if (response.status === 204) return undefined as T
  if (!response.ok) throw await apiErrorFromResponse(response)
  const payload = await response.json().catch(() => ({}))
  return payload as T
}

export async function requestDownload(path: string, signal?: AbortSignal) {
	const response = await fetch(path, { credentials: 'include', signal })
	if (!response.ok) throw await apiErrorFromResponse(response)
	return response
}

export function postJSON<T>(path: string, body: unknown) {
  return api<T>(path, { method: 'POST', body: JSON.stringify(body) })
}

export function putJSON<T>(path: string, body: unknown) {
  return api<T>(path, { method: 'PUT', body: JSON.stringify(body) })
}

export function postForm<T>(path: string, body: FormData) {
	return api<T>(path, { method: 'POST', body })
}
