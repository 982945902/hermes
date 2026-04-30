import type { BarometerSnapshot, Channel } from './types'

const TOKEN_KEY = 'hermes_admin_token'

export function getToken() {
  return localStorage.getItem(TOKEN_KEY) || ''
}

export function setToken(token: string) {
  localStorage.setItem(TOKEN_KEY, token)
}

export function clearToken() {
  localStorage.removeItem(TOKEN_KEY)
}

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers)
  headers.set('Content-Type', 'application/json')
  const token = getToken()
  if (token) headers.set('Authorization', `Bearer ${token}`)
  const res = await fetch(path, { ...init, headers })
  if (!res.ok) {
    const data = await res.json().catch(() => null)
    throw new Error(data?.error || `HTTP ${res.status}`)
  }
  if (res.status === 204) return undefined as T
  return res.json() as Promise<T>
}

export async function login(username: string, password: string) {
  return request<{ token: string }>('/api/admin/login', {
    method: 'POST',
    body: JSON.stringify({ username, password }),
  })
}

export async function me() {
  return request<{ username: string }>('/api/admin/me')
}

export async function status() {
  return request<{ status: string; time: string }>('/api/status')
}

export async function barometer() {
  return request<BarometerSnapshot>('/api/barometer')
}

export async function listChannels() {
  return request<{ data: Channel[] }>('/api/channels')
}

export async function createChannel(channel: Channel) {
  return request<Channel>('/api/channels', {
    method: 'POST',
    body: JSON.stringify(channel),
  })
}

export async function updateChannel(channel: Channel) {
  return request<Channel>(`/api/channels/${channel.id}`, {
    method: 'PUT',
    body: JSON.stringify(channel),
  })
}

export async function deleteChannel(id: string) {
  return request<void>(`/api/channels/${id}`, { method: 'DELETE' })
}

export async function testChannel(id: string, model: string, prompt: string) {
  return request<{ success: boolean; response?: string; error?: string }>(`/api/channels/${id}/test`, {
    method: 'POST',
    body: JSON.stringify({ model, prompt }),
  })
}

export async function fetchUpstreamModels(input: {
  id?: string
  provider: string
  base_url: string
  api_key?: string
  extra_headers: Record<string, string>
}) {
  return request<{ data: string[] }>('/api/channels/fetch-models', {
    method: 'POST',
    body: JSON.stringify(input),
  })
}
