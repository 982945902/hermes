import React from 'react'
import { createRoot } from 'react-dom/client'
import { Activity, LogOut, Plus, RefreshCw, Save, Settings, Trash2 } from 'lucide-react'
import {
  barometer,
  clearToken,
  createChannel,
  deleteChannel,
  fetchUpstreamModels,
  getToken,
  listChannels,
  login,
  me,
  setToken,
  status,
  testChannel,
  updateChannel,
} from './api'
import type { BarometerChannel, Channel } from './types'
import './styles.css'

const presets = {
  openrouter: {
    base_url: 'https://openrouter.ai/api/v1',
    extra_headers: {
      'HTTP-Referer': 'https://github.com/982945902/hermes',
      'X-Title': 'Hermes',
    },
  },
  doubao_coding: {
    base_url: 'https://ark.cn-beijing.volces.com/api/coding/v3',
    extra_headers: {},
  },
  custom: {
    base_url: '',
    extra_headers: {},
  },
}

function emptyChannel(): Channel {
  return {
    name: '',
    provider: 'openrouter',
    base_url: presets.openrouter.base_url,
    api_key: '',
    models: [],
    model_mapping: {},
    extra_headers: presets.openrouter.extra_headers,
    strategy: {},
    enabled: true,
    priority: 0,
    weight: 1,
  }
}

function App() {
  const [token, setTokenState] = React.useState(getToken())
  const [username, setUsername] = React.useState('')
  const [ready, setReady] = React.useState(false)

  React.useEffect(() => {
    if (!token) {
      setReady(true)
      return
    }
    me()
      .then((data) => setUsername(data.username))
      .catch(() => {
        clearToken()
        setTokenState('')
      })
      .finally(() => setReady(true))
  }, [token])

  if (!ready) return <div className="center">Loading</div>
  if (!token) {
    return (
      <Login
        onLogin={(newToken) => {
          setToken(newToken)
          setTokenState(newToken)
        }}
      />
    )
  }
  return (
    <Shell
      username={username}
      onLogout={() => {
        clearToken()
        setTokenState('')
      }}
    />
  )
}

function Login({ onLogin }: { onLogin: (token: string) => void }) {
  const [username, setUsername] = React.useState('admin')
  const [password, setPassword] = React.useState('')
  const [error, setError] = React.useState('')
  const [loading, setLoading] = React.useState(false)

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setError('')
    setLoading(true)
    try {
      const data = await login(username, password)
      onLogin(data.token)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Login failed')
    } finally {
      setLoading(false)
    }
  }

  return (
    <main className="login-page">
      <form className="login-panel" onSubmit={submit}>
        <h1>Hermes 管理台</h1>
        <label>
          用户名
          <input value={username} onChange={(e) => setUsername(e.target.value)} />
        </label>
        <label>
          密码
          <input type="password" value={password} onChange={(e) => setPassword(e.target.value)} />
        </label>
        {error ? <p className="error">{error}</p> : null}
        <button disabled={loading}>{loading ? '登录中' : '登录'}</button>
      </form>
    </main>
  )
}

function Shell({ username, onLogout }: { username: string; onLogout: () => void }) {
  return (
    <div className="app">
      <aside>
        <div className="brand">Hermes</div>
        <nav>
          <a href="#channels">渠道</a>
          <a href="#settings">设置</a>
        </nav>
      </aside>
      <main>
        <header>
          <div>
            <h1>渠道管理</h1>
            <p>配置上游渠道、模型映射，并让晴雨表动态选择最合适的线路。</p>
          </div>
          <div className="user">
            <span>{username || 'admin'}</span>
            <button className="icon-button" onClick={onLogout} title="Sign out">
              <LogOut size={18} />
            </button>
          </div>
        </header>
        <Barometer />
        <Channels />
        <SettingsPanel />
      </main>
    </div>
  )
}

function Barometer() {
  const [channels, setChannels] = React.useState<BarometerChannel[]>([])
  const [updatedAt, setUpdatedAt] = React.useState('')

  React.useEffect(() => {
    let mounted = true
    async function load() {
      try {
        const data = await barometer()
        if (!mounted) return
        setChannels(data.channels)
        setUpdatedAt(data.generated_at)
      } catch {
        if (mounted) setChannels([])
      }
    }
    load()
    const timer = window.setInterval(load, 3000)
    return () => {
      mounted = false
      window.clearInterval(timer)
    }
  }, [])

  return (
    <section className="panel barometer">
      <div className="panel-title">
        <h2>动态晴雨表</h2>
        <span>{updatedAt ? new Date(updatedAt).toLocaleTimeString() : '暂无样本'}</span>
      </div>
      <div className="barometer-grid">
        {channels.length === 0 ? (
          <p className="muted">请求或渠道测试后会显示实时指标。</p>
        ) : (
          channels.map((channel) => <BarometerCard key={channel.channel_id} channel={channel} />)
        )}
      </div>
    </section>
  )
}

function BarometerCard({ channel }: { channel: BarometerChannel }) {
  const scorePct = Math.round(channel.score * 100)
  const successPct = Math.round(channel.success_rate * 100)
  const qualityPct = Math.round(channel.quality * 100)
  return (
    <div className={`barometer-card ${channel.tier}`}>
      <div className="barometer-head">
        <div>
          <strong>{channel.name}</strong>
          <span>{channel.provider}</span>
        </div>
        <b>{tierLabel(channel.tier)}</b>
      </div>
      <div className="gauge">
        <div style={{ width: `${scorePct}%` }} />
      </div>
      <div className="metrics">
        <span>评分 {scorePct}</span>
        <span>成功率 {successPct}%</span>
        <span>耗时 {Math.round(channel.latency_ms)}ms</span>
        <span>质量 {qualityPct}</span>
      </div>
      {channel.last_error ? <p className="row-error">{channel.last_error}</p> : null}
    </div>
  )
}

function tierLabel(tier: BarometerChannel['tier']) {
  if (tier === 'excellent') return '优质'
  if (tier === 'unstable') return '不稳定'
  return '不可用'
}

function Channels() {
  const [channels, setChannels] = React.useState<Channel[]>([])
  const [editing, setEditing] = React.useState<Channel>(emptyChannel())
  const [message, setMessage] = React.useState('')
  const [testPrompt, setTestPrompt] = React.useState('请用一句话回复：Hermes channel test ok')
  const [testResponse, setTestResponse] = React.useState('')

  async function load() {
    const data = await listChannels()
    setChannels(data.data)
  }

  React.useEffect(() => {
    load().catch((err) => setMessage(err.message))
  }, [])

  async function save() {
    setMessage('')
    try {
      const payload = normalizeChannel(editing)
      if (payload.id) await updateChannel(payload)
      else await createChannel(payload)
      setEditing(emptyChannel())
      await load()
      setMessage('Saved')
    } catch (err) {
      setMessage(err instanceof Error ? err.message : 'Save failed')
    }
  }

  async function runTest(channel: Channel) {
    if (!channel.id) return
    setMessage(`Testing ${channel.name}`)
    setTestResponse('')
    try {
      const result = await testChannel(channel.id, testPrompt)
      setMessage(`${channel.name} 测试成功`)
      setTestResponse(result.response || '(empty response)')
      await load()
    } catch (err) {
      setMessage(err instanceof Error ? err.message : 'Test failed')
      await load()
    }
  }

  return (
    <section id="channels" className="section-grid">
      <div className="panel">
        <div className="panel-title">
          <h2>渠道池</h2>
          <button className="icon-button" onClick={() => setEditing(emptyChannel())} title="新建渠道">
            <Plus size={18} />
          </button>
        </div>
        <div className="table">
          {channels.map((channel) => (
            <div className="row" key={channel.id}>
              <div>
                <strong>{channel.name}</strong>
                <span>{channel.provider}</span>
              </div>
              <div>{channel.models.length} 个模型</div>
              <div className={channel.enabled ? 'ok' : 'muted'}>{channel.enabled ? '启用' : '停用'}</div>
              <div className="actions">
                <button className="icon-button" onClick={() => setEditing(channel)} title="编辑">
                  <Settings size={17} />
                </button>
                <button className="icon-button" onClick={() => runTest(channel)} title="测试">
                  <Activity size={17} />
                </button>
                <button
                  className="icon-button danger"
                  onClick={async () => {
                    if (channel.id) {
                      await deleteChannel(channel.id)
                      await load()
                    }
                  }}
                  title="删除"
                >
                  <Trash2 size={17} />
                </button>
              </div>
              {channel.last_error ? <p className="row-error">{channel.last_error}</p> : null}
            </div>
          ))}
        </div>
      </div>
      <Editor
        channel={editing}
        setChannel={setEditing}
        onSave={save}
        message={message}
        testPrompt={testPrompt}
        setTestPrompt={setTestPrompt}
        testResponse={testResponse}
      />
    </section>
  )
}

function Editor({
  channel,
  setChannel,
  onSave,
  message,
  testPrompt,
  setTestPrompt,
  testResponse,
}: {
  channel: Channel
  setChannel: (channel: Channel) => void
  onSave: () => void
  message: string
  testPrompt: string
  setTestPrompt: (value: string) => void
  testResponse: string
}) {
  const [modelSearch, setModelSearch] = React.useState('')
  const [modelError, setModelError] = React.useState('')
  const [fetching, setFetching] = React.useState(false)
  const availableModels = React.useMemo(() => {
    const set = new Set<string>([...channel.models, ...Object.values(channel.model_mapping || {})])
    return Array.from(set).filter((item) => item.toLowerCase().includes(modelSearch.toLowerCase()))
  }, [channel.models, channel.model_mapping, modelSearch])

  function patch(update: Partial<Channel>) {
    setChannel({ ...channel, ...update })
  }

  function applyProvider(provider: string) {
    const preset = presets[provider as keyof typeof presets] || presets.custom
    patch({ provider, base_url: preset.base_url, extra_headers: preset.extra_headers })
  }

  async function loadModels() {
    setModelError('')
    setFetching(true)
    try {
      const result = await fetchUpstreamModels({
        provider: channel.provider,
        base_url: channel.base_url,
        api_key: channel.api_key,
        extra_headers: channel.extra_headers || {},
      })
      const nextMapping = { ...channel.model_mapping }
      for (const modelName of result.data) {
        if (!nextMapping[modelName]) nextMapping[modelName] = modelName
      }
      patch({ models: result.data, model_mapping: nextMapping })
    } catch (err) {
      setModelError(err instanceof Error ? err.message : '拉取模型失败')
    } finally {
      setFetching(false)
    }
  }

  function toggleModel(modelName: string, checked: boolean) {
    const models = checked ? Array.from(new Set([...channel.models, modelName])) : channel.models.filter((item) => item !== modelName)
    patch({ models })
  }

  function setMapping(modelName: string, upstream: string) {
    patch({ model_mapping: { ...channel.model_mapping, [modelName]: upstream } })
  }

  return (
    <div className="panel editor">
      <h2>{channel.id ? '编辑渠道' : '新建渠道'}</h2>
      <label>
        渠道名称
        <input value={channel.name} onChange={(e) => patch({ name: e.target.value })} />
      </label>
      <label>
        渠道类型
        <select value={channel.provider} onChange={(e) => applyProvider(e.target.value)}>
          <option value="openrouter">OpenRouter</option>
          <option value="doubao_coding">Doubao Coding Plan</option>
          <option value="custom">Custom</option>
        </select>
      </label>
      <label>
        Base URL
        <input value={channel.base_url} onChange={(e) => patch({ base_url: e.target.value })} />
      </label>
      <label>
        API Key
        <input value={channel.api_key || ''} onChange={(e) => patch({ api_key: e.target.value })} />
      </label>
      <div className="model-picker">
        <div className="model-toolbar">
          <div>
            <h3>模型映射</h3>
            <p>先拉取上游模型，勾选要对外暴露的模型；右侧可填写对外模型对应的真实上游模型。</p>
          </div>
          <button onClick={loadModels} disabled={fetching || !channel.base_url || !channel.api_key}>
            <RefreshCw size={17} />
            {fetching ? '拉取中' : '拉取模型'}
          </button>
        </div>
        <input placeholder="搜索模型..." value={modelSearch} onChange={(e) => setModelSearch(e.target.value)} />
        {modelError ? <p className="error">{modelError}</p> : null}
        <div className="model-list">
          {availableModels.length === 0 ? (
            <p className="muted">填写 URL 和 Key 后点击拉取模型。</p>
          ) : (
            availableModels.map((modelName) => (
              <div className="model-item" key={modelName}>
                <label className="model-check">
                  <input
                    type="checkbox"
                    checked={channel.models.includes(modelName)}
                    onChange={(e) => toggleModel(modelName, e.target.checked)}
                  />
                  <span>{modelName}</span>
                </label>
                <input
                  value={channel.model_mapping[modelName] || modelName}
                  onChange={(e) => setMapping(modelName, e.target.value)}
                  placeholder="上游真实模型"
                />
              </div>
            ))
          )}
        </div>
      </div>
      <label>
        Extra Headers JSON
        <textarea
          value={JSON.stringify(channel.extra_headers, null, 2)}
          onChange={(e) => patch({ extra_headers: parseObject(e.target.value) as Record<string, string> })}
        />
      </label>
      <div className="inline single">
        <label className="check">
          <input type="checkbox" checked={channel.enabled} onChange={(e) => patch({ enabled: e.target.checked })} />
          启用渠道
        </label>
      </div>
      <button className="primary" onClick={onSave}>
        <Save size={17} />
        保存渠道
      </button>
      {message ? <p className="message">{message}</p> : null}
      <div className="test-box">
        <h3>渠道对话测试</h3>
        <textarea value={testPrompt} onChange={(e) => setTestPrompt(e.target.value)} />
        {testResponse ? <pre>{testResponse}</pre> : null}
      </div>
    </div>
  )
}

function SettingsPanel() {
  const [data, setData] = React.useState('')
  return (
    <section id="settings" className="panel settings-panel">
      <h2>设置</h2>
      <button
        onClick={async () => {
          const result = await status()
          setData(JSON.stringify(result, null, 2))
        }}
      >
        <RefreshCw size={17} />
        刷新状态
      </button>
      {data ? <pre>{data}</pre> : null}
    </section>
  )
}

function parseObject(value: string): Record<string, string> {
  try {
    const parsed = JSON.parse(value)
    if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) return parsed
  } catch {
    return {}
  }
  return {}
}

function normalizeChannel(channel: Channel): Channel {
  return {
    ...channel,
    models: channel.models.filter(Boolean),
    model_mapping: channel.model_mapping || {},
    extra_headers: channel.extra_headers || {},
    strategy: channel.strategy || {},
    weight: channel.weight > 0 ? channel.weight : 1,
  }
}

createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
)
