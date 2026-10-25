import type {
  AuditEntry, CreateUserResult, Dashboard, Health, Me, ModelView, Page, PresenceResult,
  Price, PublicConfig, ResetResult, RevealResult, Run, RunDetail, Setting, UsageAgg, UsageOverview,
  UsageRow, User, UserDetail, Event, Message,
} from './types'

const BASE = '/api/ops/v1'
let csrfToken = ''
let unauthorizedHandler: () => void = () => undefined

export function setCSRF(token: string): void { csrfToken = token }
export function onUnauthorized(fn: () => void): void { unauthorizedHandler = fn }

export class ApiError extends Error {
  status: number
  code: string
  retryAfter?: number
  constructor(status: number, code: string, message: string, retryAfter?: number) {
    super(message)
    this.status = status
    this.code = code
    this.retryAfter = retryAfter
  }
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const headers: Record<string, string> = { Accept: 'application/json' }
  if (body !== undefined) headers['Content-Type'] = 'application/json'
  if (method !== 'GET' && csrfToken) headers['X-CSRF-Token'] = csrfToken
  let response: Response
  try {
    response = await fetch(BASE + path, {
      method,
      credentials: 'include',
      headers,
      body: body === undefined ? undefined : JSON.stringify(body),
    })
  } catch {
    throw new ApiError(0, 'NETWORK', '网络连接失败，请检查服务是否在线')
  }
  if (response.status === 204) return undefined as T
  const text = await response.text()
  let payload: unknown = undefined
  if (text) {
    try { payload = JSON.parse(text) } catch { payload = text }
  }
  if (!response.ok) {
    const err = (payload as { error?: { code?: string; message?: string } } | undefined)?.error
    const retry = Number(response.headers.get('Retry-After') || '')
    if (response.status === 401) unauthorizedHandler()
    throw new ApiError(response.status, err?.code || `HTTP_${response.status}`, err?.message || '请求失败', Number.isFinite(retry) ? retry : undefined)
  }
  return payload as T
}

function query(params: Record<string, string | number | undefined | null>): string {
  const q = new URLSearchParams()
  for (const [k, v] of Object.entries(params)) {
    if (v !== undefined && v !== null && v !== '') q.set(k, String(v))
  }
  const s = q.toString()
  return s ? `?${s}` : ''
}

async function download(path: string, fallbackName: string): Promise<void> {
  const response = await fetch(BASE + path, { credentials: 'include', headers: { Accept: 'text/csv' } })
  if (!response.ok) {
    let message = '导出失败'
    try {
      const payload = await response.json() as { error?: { message?: string } }
      message = payload.error?.message || message
    } catch { /* ignore */ }
    throw new ApiError(response.status, 'EXPORT_FAILED', message)
  }
  const blob = await response.blob()
  const disposition = response.headers.get('Content-Disposition') || ''
  const match = /filename="?([^";]+)"?/.exec(disposition)
  const name = match?.[1] || fallbackName
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = name
  document.body.appendChild(a)
  a.click()
  a.remove()
  setTimeout(() => URL.revokeObjectURL(url), 1000)
}

export const api = {
  publicConfig: () => request<PublicConfig>('GET', '/public/config'),
  auth: {
    login: (admin_id: string, password: string) => request<void>('POST', '/auth/login', { admin_id, password }),
    logout: () => request<void>('POST', '/auth/logout'),
    me: () => request<Me>('GET', '/auth/me'),
    stepUp: (password: string) => request<void>('POST', '/auth/step-up', { password }),
    changePassword: (old_password: string, new_password: string) =>
      request<{ ok: boolean; message: string }>('POST', '/auth/password', { old_password, new_password }),
  },
  dashboard: () => request<Dashboard>('GET', '/dashboard/summary'),
  users: {
    list: (p: { q?: string; status?: number; pwd_state?: string; cursor?: string; limit?: number }) =>
      request<Page<User>>('GET', `/users${query(p)}`),
    create: (body: { user_id: string; name: string; password?: string; max_conv: number; tags: string[]; note: string; client_request_id?: string }) =>
      request<CreateUserResult>('POST', '/users', body),
    get: (id: string) => request<UserDetail>('GET', `/users/${encodeURIComponent(id)}`),
    patch: (id: string, body: { name?: string; max_conv?: number; note?: string; tags?: string[] }) =>
      request<void>('PATCH', `/users/${encodeURIComponent(id)}`, body),
    reset: (id: string, body: { password?: string; reason: string; client_request_id?: string }) =>
      request<ResetResult>('POST', `/users/${encodeURIComponent(id)}/reset-password`, body),
    disable: (id: string, reason: string) =>
      request<void>('POST', `/users/${encodeURIComponent(id)}/disable`, { reason }),
    enable: (id: string) => request<void>('POST', `/users/${encodeURIComponent(id)}/enable`),
    reveal: (id: string, reason: string) =>
      request<RevealResult>('POST', `/users/${encodeURIComponent(id)}/reveal-password`, { reason }),
    revokeSessions: (id: string) => request<void>('POST', `/users/${encodeURIComponent(id)}/sessions/revoke`),
    conversations: (id: string, limit = 20) =>
      request<{ items: import('./types').ConversationBrief[] }>('GET', `/users/${encodeURIComponent(id)}/conversations${query({ limit })}`),
    runs: (id: string, limit = 20, cursor?: string) =>
      request<Page<Run>>('GET', `/users/${encodeURIComponent(id)}/runs${query({ limit, cursor })}`),
    usage: (id: string) =>
      request<{ from: string; to: string; items: UsageAgg[]; total: UsageAgg }>('GET', `/users/${encodeURIComponent(id)}/usage`),
  },
  models: {
    list: () => request<{ items: ModelView[] }>('GET', '/models'),
    get: (id: string) => request<{ model: ModelView; prices: Price[] }>('GET', `/models/${encodeURIComponent(id)}`),
    import: (yaml: string) => request<{ imported: number; models: string[] }>('POST', '/models/import', { yaml }),
    patch: (id: string, body: { display_name?: string; provider?: string; notes?: string; enabled?: boolean }) =>
      request<void>('PATCH', `/models/${encodeURIComponent(id)}`, body),
    prices: (id: string) => request<{ items: Price[] }>('GET', `/models/${encodeURIComponent(id)}/prices`),
    addPrice: (id: string, body: { effective_from?: string; input_per_mtok: number; output_per_mtok: number; currency: string }) =>
      request<Price>('POST', `/models/${encodeURIComponent(id)}/prices`, body),
    usage: (id: string, from?: string, to?: string) =>
      request<{ from: string; to: string; items: UsageAgg[]; total: UsageAgg }>('GET', `/models/${encodeURIComponent(id)}/usage${query({ from, to })}`),
  },
  usage: {
    overview: (p: { from?: string; to?: string; group_by?: string; user_id?: string; model?: string; conv_id?: string }) =>
      request<UsageOverview>('GET', `/usage/overview${query(p)}`),
    export: (p: { from?: string; to?: string; user_id?: string; model?: string }) =>
      download(`/usage/export${query(p)}`, 'tenon-ops-usage.csv'),
  },
  presence: (window = '15m') => request<PresenceResult>('GET', `/presence${query({ window })}`),
  runs: {
    list: (p: { status?: string; user_id?: string; model?: string; from?: string; to?: string; cursor?: string; limit?: number }) =>
      request<Page<Run>>('GET', `/runs${query(p)}`),
    get: (id: string) => request<RunDetail>('GET', `/runs/${encodeURIComponent(id)}`),
    events: (id: string, after_seq = 0, limit = 100) =>
      request<{ items: Event[]; next_cursor: string }>('GET', `/runs/${encodeURIComponent(id)}/events${query({ after_seq, limit })}`),
    messages: (id: string) => request<{ items: Message[] }>('GET', `/runs/${encodeURIComponent(id)}/messages`),
    usage: (id: string) => request<{ items: UsageRow[] }>('GET', `/runs/${encodeURIComponent(id)}/usage`),
  },
  audit: {
    list: (p: { admin_id?: string; action?: string; target?: string; result?: number; from?: string; to?: string; cursor?: string; limit?: number }) =>
      request<Page<AuditEntry>>('GET', `/audit${query(p)}`),
    actions: () => request<{ items: string[] }>('GET', '/audit/actions'),
    export: (p: { admin_id?: string; action?: string; target?: string }) =>
      download(`/audit/export${query(p)}`, 'tenon-ops-audit.csv'),
  },
  system: {
    health: () => request<Health>('GET', '/system/health'),
    settings: () => request<{ items: Setting[] }>('GET', '/system/settings'),
    updateSettings: (body: Record<string, string>) => request<void>('PATCH', '/system/settings', body),
  },
}
