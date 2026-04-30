export type Channel = {
  id?: string
  name: string
  provider: 'openrouter' | 'doubao_coding' | 'custom' | string
  base_url: string
  api_key?: string
  models: string[]
  model_mapping: Record<string, string>
  model_mappings?: Record<string, string[]>
  extra_headers: Record<string, string>
  strategy: Record<string, unknown>
  enabled: boolean
  priority: number
  weight: number
  last_error?: string
  last_test_at?: string
  created_at?: string
  updated_at?: string
}

export type BarometerModel = {
  channel_id: string
  name: string
  provider: string
  external_model: string
  upstream_model: string
  requests: number
  success_rate: number
  latency_ms: number
  quality: number
  score: number
  tier: 'excellent' | 'unstable' | 'unavailable'
  consecutive_failures: number
  last_status_code: number
  last_error?: string
  last_updated_at?: string
  last_probe_at?: string
}

export type BarometerChannel = {
  channel_id: string
  name: string
  provider: string
  models: BarometerModel[]
}

export type BarometerSnapshot = {
  generated_at: string
  channels: BarometerChannel[]
}
