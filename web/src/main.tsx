import React from 'react'
import { createRoot } from 'react-dom/client'
import { Activity, LogOut, Plus, RefreshCw, Save, Settings, Trash2 } from 'lucide-react'
import {
  clearToken,
  createChannel,
  deleteChannel,
  getToken,
  listChannels,
  login,
  me,
  setToken,
  status,
  testChannel,
  updateChannel,
} from './api'
import type { Channel } from './types'
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
        <h1>Hermes</h1>
        <label>
          Username
          <input value={username} onChange={(e) => setUsername(e.target.value)} />
        </label>
        <label>
          Password
          <input type="password" value={password} onChange={(e) => setPassword(e.target.value)} />
        </label>
        {error ? <p className="error">{error}</p> : null}
        <button disabled={loading}>{loading ? 'Signing in' : 'Sign in'}</button>
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
          <a href="#channels">Channels</a>
          <a href="#settings">Settings</a>
        </nav>
      </aside>
      <main>
        <header>
          <div>
            <h1>Channels</h1>
            <p>Configure OpenAI-compatible upstreams for the gateway.</p>
          </div>
          <div className="user">
            <span>{username || 'admin'}</span>
            <button className="icon-button" onClick={onLogout} title="Sign out">
              <LogOut size={18} />
            </button>
          </div>
        </header>
        <Channels />
        <SettingsPanel />
      </main>
    </div>
  )
}

function Channels() {
  const [channels, setChannels] = React.useState<Channel[]>([])
  const [editing, setEditing] = React.useState<Channel>(emptyChannel())
  const [message, setMessage] = React.useState('')

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
    try {
      await testChannel(channel.id)
      setMessage(`${channel.name} is healthy`)
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
          <h2>Channel Pool</h2>
          <button className="icon-button" onClick={() => setEditing(emptyChannel())} title="New channel">
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
              <div>{channel.models.length} models</div>
              <div className={channel.enabled ? 'ok' : 'muted'}>{channel.enabled ? 'Enabled' : 'Disabled'}</div>
              <div className="actions">
                <button className="icon-button" onClick={() => setEditing(channel)} title="Edit">
                  <Settings size={17} />
                </button>
                <button className="icon-button" onClick={() => runTest(channel)} title="Test">
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
                  title="Delete"
                >
                  <Trash2 size={17} />
                </button>
              </div>
              {channel.last_error ? <p className="row-error">{channel.last_error}</p> : null}
            </div>
          ))}
        </div>
      </div>
      <Editor channel={editing} setChannel={setEditing} onSave={save} message={message} />
    </section>
  )
}

function Editor({
  channel,
  setChannel,
  onSave,
  message,
}: {
  channel: Channel
  setChannel: (channel: Channel) => void
  onSave: () => void
  message: string
}) {
  function patch(update: Partial<Channel>) {
    setChannel({ ...channel, ...update })
  }

  function applyProvider(provider: string) {
    const preset = presets[provider as keyof typeof presets] || presets.custom
    patch({ provider, base_url: preset.base_url, extra_headers: preset.extra_headers })
  }

  return (
    <div className="panel editor">
      <h2>{channel.id ? 'Edit Channel' : 'New Channel'}</h2>
      <label>
        Name
        <input value={channel.name} onChange={(e) => patch({ name: e.target.value })} />
      </label>
      <label>
        Provider
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
      <label>
        Models
        <textarea
          value={channel.models.join('\n')}
          onChange={(e) => patch({ models: e.target.value.split('\n').map((item) => item.trim()).filter(Boolean) })}
        />
      </label>
      <label>
        Model Mapping JSON
        <textarea
          value={JSON.stringify(channel.model_mapping, null, 2)}
          onChange={(e) => patch({ model_mapping: parseObject(e.target.value) })}
        />
      </label>
      <label>
        Extra Headers JSON
        <textarea
          value={JSON.stringify(channel.extra_headers, null, 2)}
          onChange={(e) => patch({ extra_headers: parseObject(e.target.value) as Record<string, string> })}
        />
      </label>
      <div className="inline">
        <label>
          Priority
          <input type="number" value={channel.priority} onChange={(e) => patch({ priority: Number(e.target.value) })} />
        </label>
        <label>
          Weight
          <input type="number" value={channel.weight} onChange={(e) => patch({ weight: Number(e.target.value) })} />
        </label>
        <label className="check">
          <input type="checkbox" checked={channel.enabled} onChange={(e) => patch({ enabled: e.target.checked })} />
          Enabled
        </label>
      </div>
      <button className="primary" onClick={onSave}>
        <Save size={17} />
        Save
      </button>
      {message ? <p className="message">{message}</p> : null}
    </div>
  )
}

function SettingsPanel() {
  const [data, setData] = React.useState('')
  return (
    <section id="settings" className="panel settings-panel">
      <h2>Settings</h2>
      <button
        onClick={async () => {
          const result = await status()
          setData(JSON.stringify(result, null, 2))
        }}
      >
        <RefreshCw size={17} />
        Refresh status
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

