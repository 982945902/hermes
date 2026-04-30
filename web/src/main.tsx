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
import type { BarometerChannel, BarometerModel, Channel } from './types'
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
  const bestTier = channel.models.reduce<BarometerModel['tier'] | undefined>((tier, item) => {
    if (!tier) return item.tier
    return tierRank(item.tier) < tierRank(tier) ? item.tier : tier
  }, undefined)
  return (
    <div className={`barometer-card ${bestTier || 'excellent'}`}>
      <div className="barometer-head">
        <div>
          <strong>{channel.name}</strong>
          <span>{channel.provider}</span>
        </div>
        <b>{channel.models.length} 个模型</b>
      </div>
      <div className="barometer-models">
        {channel.models.map((item) => (
          <BarometerModelRow key={`${item.external_model}-${item.upstream_model}`} item={item} />
        ))}
      </div>
    </div>
  )
}

function BarometerModelRow({ item }: { item: BarometerModel }) {
  const scorePct = Math.round(item.score * 100)
  const successPct = Math.round(item.success_rate * 100)
  const qualityPct = Math.round(item.quality * 100)
  return (
    <div className={`barometer-model ${item.tier}`}>
      <div className="barometer-model-title">
        <div>
          <strong>{item.external_model}</strong>
          <span>{item.upstream_model === item.external_model ? '直连上游模型' : `映射到 ${item.upstream_model}`}</span>
        </div>
        <b>{tierLabel(item.tier)}</b>
      </div>
      <div className="gauge">
        <div style={{ width: `${scorePct}%` }} />
      </div>
      <div className="metrics">
        <span>评分 {scorePct}</span>
        <span>成功率 {successPct}%</span>
        <span>耗时 {Math.round(item.latency_ms)}ms</span>
        <span>质量 {qualityPct}</span>
        <span>样本 {item.requests}</span>
        <span>状态 {item.last_status_code || '-'}</span>
      </div>
      {item.last_error ? <p className="row-error">{item.last_error}</p> : null}
    </div>
  )
}

function tierLabel(tier: BarometerModel['tier']) {
  if (tier === 'excellent') return '优质'
  if (tier === 'unstable') return '不稳定'
  return '不可用'
}

function tierRank(tier: BarometerModel['tier']) {
  if (tier === 'excellent') return 0
  if (tier === 'unstable') return 1
  return 2
}

function Channels() {
  const [channels, setChannels] = React.useState<Channel[]>([])
  const [editing, setEditing] = React.useState<Channel>(emptyChannel())
  const [message, setMessage] = React.useState('')
  const [testPrompt, setTestPrompt] = React.useState('请用一句话回复：Hermes channel test ok')
  const [testModel, setTestModel] = React.useState('')
  const [testMessages, setTestMessages] = React.useState<Array<{ role: 'user' | 'assistant'; content: string }>>([])

  async function load(selectChannelId?: string, selectFirst = false) {
    const data = await listChannels()
    setChannels(data.data)
    const selected = selectChannelId ? data.data.find((item) => item.id === selectChannelId) : undefined
    if (selected) {
      setEditing(selected)
      return
    }
    if (selectFirst && data.data.length > 0) {
      setEditing(data.data[0])
    }
  }

  React.useEffect(() => {
    load(undefined, true).catch((err) => setMessage(err.message))
  }, [])

  async function save() {
    setMessage('')
    try {
      const payload = normalizeChannel(editing)
      const saved = payload.id ? await updateChannel(payload) : await createChannel(payload)
      setEditing(saved)
      await load(saved.id, false)
      setMessage('已保存')
    } catch (err) {
      setMessage(err instanceof Error ? err.message : '保存失败')
    }
  }

  async function runTest(channel: Channel) {
    if (!channel.id) return
    const modelName = testModel || channel.models[0] || ''
    if (!modelName) {
      setMessage('请先为渠道选择至少一个模型')
      return
    }
    setMessage(`正在测试 ${channel.name}`)
    setTestMessages((items) => [...items, { role: 'user', content: testPrompt }])
    try {
      const result = await testChannel(channel.id, modelName, testPrompt)
      setMessage(`${channel.name} 测试成功`)
      setTestMessages((items) => [...items, { role: 'assistant', content: result.response || '(empty response)' }])
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
          <button
            className="icon-button"
            onClick={() => {
              setEditing(emptyChannel())
              setTestMessages([])
              setMessage('正在新建渠道')
            }}
            title="新建渠道"
          >
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
                <button
                  className="icon-button"
                  onClick={() => {
                    setEditing(channel)
                    setTestMessages([])
                    setMessage(`正在编辑 ${channel.name}`)
                  }}
                  title="编辑"
                >
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
                      await load(undefined, true)
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
        testModel={testModel}
        setTestModel={setTestModel}
        testMessages={testMessages}
        onRunTest={() => runTest(editing)}
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
  testModel,
  setTestModel,
  testMessages,
  onRunTest,
}: {
  channel: Channel
  setChannel: React.Dispatch<React.SetStateAction<Channel>>
  onSave: () => void
  message: string
  testPrompt: string
  setTestPrompt: (value: string) => void
  testModel: string
  setTestModel: (value: string) => void
  testMessages: Array<{ role: 'user' | 'assistant'; content: string }>
  onRunTest: () => void
}) {
  const [modelSearch, setModelSearch] = React.useState('')
  const [modelError, setModelError] = React.useState('')
  const [fetching, setFetching] = React.useState(false)
  const [upstreamModels, setUpstreamModels] = React.useState<string[]>([])
  const [externalDrafts, setExternalDrafts] = React.useState<Record<string, string>>({})
  const availableModels = React.useMemo(() => {
    const set = new Set<string>([
      ...upstreamModels,
      ...Object.values(channel.model_mapping || {}),
      ...channel.models.map((modelName) => channel.model_mapping[modelName] || modelName),
    ])
    const query = modelSearch.toLowerCase()
    return Array.from(set).filter((upstreamModel) => {
      const externalModel =
        channel.models.find((modelName) => (channel.model_mapping[modelName] || modelName) === upstreamModel) ||
        externalDrafts[upstreamModel] ||
        upstreamModel
      return upstreamModel.toLowerCase().includes(query) || externalModel.toLowerCase().includes(query)
    })
  }, [channel.models, channel.model_mapping, externalDrafts, modelSearch, upstreamModels])

  function patch(update: Partial<Channel>) {
    setChannel((current) => ({ ...current, ...update }))
  }

  React.useEffect(() => {
    if (!channel.models.length) return
    if (!testModel || !channel.models.includes(testModel)) {
      setTestModel(channel.models[0])
    }
  }, [channel.models, testModel, setTestModel])

  function applyProvider(provider: string) {
    const preset = presets[provider as keyof typeof presets] || presets.custom
    patch({ provider, base_url: preset.base_url, extra_headers: preset.extra_headers })
  }

  async function loadModels() {
    setModelError('')
    setFetching(true)
    try {
      const result = await fetchUpstreamModels({
        id: channel.id,
        provider: channel.provider,
        base_url: channel.base_url,
        api_key: channel.api_key,
        extra_headers: channel.extra_headers || {},
      })
      setUpstreamModels(result.data)
      setExternalDrafts((drafts) => {
        const nextDrafts = { ...drafts }
        for (const upstreamModel of result.data) {
          if (!nextDrafts[upstreamModel]) nextDrafts[upstreamModel] = findExternalModel(upstreamModel) || upstreamModel
        }
        return nextDrafts
      })
    } catch (err) {
      setModelError(err instanceof Error ? err.message : '拉取模型失败')
    } finally {
      setFetching(false)
    }
  }

  function findExternalIn(source: Channel, upstreamModel: string) {
    return source.models.find((modelName) => (source.model_mapping[modelName] || modelName) === upstreamModel) || ''
  }

  function findExternalModel(upstreamModel: string) {
    return findExternalIn(channel, upstreamModel)
  }

  function externalValue(upstreamModel: string) {
    const selectedExternal = findExternalModel(upstreamModel)
    return (externalDrafts[upstreamModel] ?? selectedExternal) || upstreamModel
  }

  function setExternalDraft(upstreamModel: string, externalModel: string) {
    setExternalDrafts((drafts) => ({ ...drafts, [upstreamModel]: externalModel }))
  }

  function commitExternalName(upstreamModel: string) {
    setChannel((current) => {
      const existingExternal = findExternalIn(current, upstreamModel)
      if (!existingExternal) return current
      const nextExternal = (externalDrafts[upstreamModel] ?? existingExternal).trim()
      if (!nextExternal) {
        setModelError('对外模型名称不能为空')
        return current
      }
      if (current.models.some((item) => item !== existingExternal && item === nextExternal)) {
        setModelError(`对外模型 ${nextExternal} 已存在`)
        return current
      }
      const nextModels = current.models.filter((item) => item !== existingExternal)
      const nextMapping = { ...current.model_mapping }
      delete nextMapping[existingExternal]
      nextModels.push(nextExternal)
      nextMapping[nextExternal] = upstreamModel
      setModelError('')
      return { ...current, models: Array.from(new Set(nextModels)), model_mapping: nextMapping }
    })
  }

  function toggleModel(upstreamModel: string, checked: boolean) {
    setModelError('')
    setChannel((current) => {
      const existingExternal = findExternalIn(current, upstreamModel)
      const nextModels = current.models.filter((item) => item !== existingExternal)
      const nextMapping = { ...current.model_mapping }
      if (existingExternal) delete nextMapping[existingExternal]
      if (checked) {
        const rawExternal = externalDrafts[upstreamModel] ?? existingExternal
        const externalModel = (rawExternal || upstreamModel).trim()
        if (!externalModel) {
          setModelError('请填写对外模型名称')
          return current
        }
        if (current.models.some((item) => item !== existingExternal && item === externalModel)) {
          setModelError(`对外模型 ${externalModel} 已存在`)
          return current
        }
        nextModels.push(externalModel)
        nextMapping[externalModel] = upstreamModel
      }
      return { ...current, models: Array.from(new Set(nextModels)), model_mapping: nextMapping }
    })
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
            <p>左侧是上游真实模型；勾选后在右侧填写对外模型名称，保存后按“对外模型 → 上游模型”转发。</p>
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
            <p className="muted">填写 URL 和 Key 后点击拉取模型。默认不启用，需要手动勾选。</p>
          ) : (
            availableModels.map((upstreamModel) => {
              const checked = Boolean(findExternalModel(upstreamModel))
              return (
              <div className="model-item" key={upstreamModel}>
                <label className="model-check">
                  <input
                    type="checkbox"
                    checked={checked}
                    onChange={(e) => toggleModel(upstreamModel, e.target.checked)}
                  />
                  <span>{upstreamModel}</span>
                </label>
                <input
                  value={externalValue(upstreamModel)}
                  onChange={(e) => setExternalDraft(upstreamModel, e.target.value)}
                  onBlur={() => commitExternalName(upstreamModel)}
                  placeholder="对外模型名称"
                />
              </div>
              )
            })
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
        <label>
          测试模型
          <select value={testModel} onChange={(e) => setTestModel(e.target.value)}>
            {channel.models.length === 0 ? <option value="">请先勾选模型</option> : null}
            {channel.models.map((modelName) => (
              <option key={modelName} value={modelName}>
                {modelName} → {channel.model_mapping[modelName] || modelName}
              </option>
            ))}
          </select>
        </label>
        <label>
          用户消息
          <textarea value={testPrompt} onChange={(e) => setTestPrompt(e.target.value)} />
        </label>
        <button className="primary" onClick={onRunTest} disabled={!channel.id || channel.models.length === 0}>
          <Activity size={17} />
          发送测试
        </button>
        <div className="chat-preview">
          {testMessages.length === 0 ? (
            <p className="muted">点击渠道行里的测试按钮发送当前消息。</p>
          ) : (
            testMessages.map((item, index) => (
              <div className={`chat-bubble ${item.role}`} key={`${item.role}-${index}`}>
                <b>{item.role === 'user' ? '你' : '模型'}</b>
                <p>{item.content}</p>
              </div>
            ))
          )}
        </div>
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
  const models = channel.models.map((modelName) => modelName.trim()).filter(Boolean)
  const model_mapping: Record<string, string> = {}
  for (const modelName of models) {
    model_mapping[modelName] = channel.model_mapping?.[modelName] || modelName
  }
  return {
    ...channel,
    models,
    model_mapping,
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
