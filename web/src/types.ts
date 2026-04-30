export type Channel = {
  id?: string
  name: string
  provider: 'openrouter' | 'doubao_coding' | 'custom' | string
  base_url: string
  api_key?: string
  models: string[]
  model_mapping: Record<string, string>
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

