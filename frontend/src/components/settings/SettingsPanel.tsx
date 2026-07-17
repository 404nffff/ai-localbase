import { useEffect, useMemo, useRef, useState } from 'react'
import type { AppConfig, ChatConfig, EmbeddingConfig } from '../../App'
import AppIcon from '../common/AppIcon'
import type { AppIconName } from '../common/AppIcon'

interface SettingsPanelProps {
  config: AppConfig
  onClose: () => void
  onSaveConfig: (value: AppConfig) => Promise<AppConfig>
}

type SettingsTab = 'overview' | 'models' | 'mcp'
type ModelSection = 'chat' | 'embedding'

const settingsNavItems: Array<{
  id: SettingsTab
  label: string
  description: string
  icon: AppIconName
}> = [
  { id: 'overview', label: '概览', description: '运行状态与当前配置', icon: 'info' },
  { id: 'models', label: '模型', description: '聊天与 Embedding', icon: 'brain' },
  { id: 'mcp', label: '接入', description: 'MCP HTTP 配置', icon: 'key' },
]

// 使用稳定序列化值判断是否存在未保存更改，供组件与单元测试共同复用。
export const getSettingsConfigFingerprint = (config: AppConfig) => JSON.stringify(config)

export const hasSettingsConfigChanges = (baseline: AppConfig, draft: AppConfig) => (
  getSettingsConfigFingerprint(baseline) !== getSettingsConfigFingerprint(draft)
)

const validateSettingsConfig = (config: AppConfig) => {
  if (!config.chat.baseUrl.trim()) return '请填写聊天模型 Base URL'
  if (!config.chat.model.trim()) return '请填写聊天模型名称'
  if (!config.embedding.baseUrl.trim()) return '请填写 Embedding 模型 Base URL'
  if (!config.embedding.model.trim()) return '请填写 Embedding 模型名称'
  if (config.chat.temperature < 0 || config.chat.temperature > 1) {
    return 'Temperature 需要在 0 到 1 之间'
  }
  if (config.chat.contextMessageLimit < 1 || config.chat.contextMessageLimit > 100) {
    return '上下文消息数量需要在 1 到 100 之间'
  }
  return null
}

const SettingsPanel = ({ config, onClose, onSaveConfig }: SettingsPanelProps) => {
  const mcpClientConfigExample = `[mcp_servers.ai_localbase]
url = "http://127.0.0.1:8080/mcp"
startup_timeout_sec = 120.0
http_headers = { "Authorization" = "Bearer your-app-access-token" }`
  const [activeTab, setActiveTab] = useState<SettingsTab>('overview')
  const [activeModelSection, setActiveModelSection] = useState<ModelSection>('chat')
  const [isMobileDetailOpen, setIsMobileDetailOpen] = useState(false)
  const [baselineConfig, setBaselineConfig] = useState(config)
  const [draftConfig, setDraftConfig] = useState(config)
  const [isSaving, setIsSaving] = useState(false)
  const [saveNotice, setSaveNotice] = useState<{ type: 'success' | 'error'; text: string } | null>(null)
  const [showCopySuccessHint, setShowCopySuccessHint] = useState(false)
  const saveNoticeTimerRef = useRef<number | null>(null)
  const copySuccessTimerRef = useRef<number | null>(null)
  const isDirty = useMemo(
    () => hasSettingsConfigChanges(baselineConfig, draftConfig),
    [baselineConfig, draftConfig],
  )
  const activeNavItem = settingsNavItems.find((item) => item.id === activeTab) ?? settingsNavItems[0]
  const draftChatConfig = draftConfig.chat
  const draftEmbeddingConfig = draftConfig.embedding

  useEffect(() => {
    if (isDirty || isSaving) return
    setBaselineConfig(config)
    setDraftConfig(config)
  }, [config, isDirty, isSaving])

  useEffect(() => () => {
    if (saveNoticeTimerRef.current) window.clearTimeout(saveNoticeTimerRef.current)
    if (copySuccessTimerRef.current) window.clearTimeout(copySuccessTimerRef.current)
  }, [])

  const showSaveNotice = (type: 'success' | 'error', text: string) => {
    setSaveNotice({ type, text })
    if (saveNoticeTimerRef.current) window.clearTimeout(saveNoticeTimerRef.current)
    saveNoticeTimerRef.current = window.setTimeout(() => {
      setSaveNotice(null)
      saveNoticeTimerRef.current = null
    }, 2200)
  }

  const updateChatConfig = <K extends keyof ChatConfig>(key: K, value: ChatConfig[K]) => {
    setSaveNotice(null)
    setDraftConfig((current) => ({
      ...current,
      chat: { ...current.chat, [key]: value },
    }))
  }

  const updateEmbeddingConfig = <K extends keyof EmbeddingConfig>(
    key: K,
    value: EmbeddingConfig[K],
  ) => {
    setSaveNotice(null)
    setDraftConfig((current) => ({
      ...current,
      embedding: { ...current.embedding, [key]: value },
    }))
  }

  const handleSave = async () => {
    if (!isDirty || isSaving) return
    const validationError = validateSettingsConfig(draftConfig)
    if (validationError) {
      showSaveNotice('error', validationError)
      return
    }

    setIsSaving(true)
    try {
      const savedConfig = await onSaveConfig(draftConfig)
      setBaselineConfig(savedConfig)
      setDraftConfig(savedConfig)
      showSaveNotice('success', 'AI 设置已保存')
    } catch (error) {
      const message = error instanceof Error ? error.message : 'AI 设置保存失败'
      showSaveNotice('error', `保存失败：${message}`)
    } finally {
      setIsSaving(false)
    }
  }

  const handleDiscard = () => {
    setDraftConfig(baselineConfig)
    setSaveNotice(null)
  }

  // 移动端先展示分类列表，选择分类后再进入对应详情页。
  const handleTabChange = (tab: SettingsTab) => {
    setActiveTab(tab)
    setIsMobileDetailOpen(true)
  }

  const handleClose = () => {
    if (isDirty && !window.confirm('当前设置尚未保存，确认放弃更改并关闭吗？')) return
    onClose()
  }

  const handleCopyMCPExample = async () => {
    try {
      await navigator.clipboard.writeText(mcpClientConfigExample)
      setShowCopySuccessHint(true)
      if (copySuccessTimerRef.current) window.clearTimeout(copySuccessTimerRef.current)
      copySuccessTimerRef.current = window.setTimeout(() => {
        setShowCopySuccessHint(false)
        copySuccessTimerRef.current = null
      }, 1800)
      showSaveNotice('success', 'MCP 客户端配置已复制')
    } catch (error) {
      const message = error instanceof Error ? error.message : '复制 MCP 客户端配置失败'
      showSaveNotice('error', `复制失败：${message}`)
    }
  }

  const renderOverview = () => {
    const chatProvider = draftChatConfig.provider === 'ollama' ? 'Ollama' : 'OpenAI Compatible'
    const embeddingProvider = draftEmbeddingConfig.provider === 'ollama' ? 'Ollama' : 'OpenAI Compatible'

    return (
      <div className="settings-tab-content settings-overview-page">
        <section className="settings-overview-summary" aria-label="当前运行状态">
          <div className="settings-overview-metric">
            <span className="settings-overview-metric-icon"><AppIcon name="brain" size={17} /></span>
            <div><span>聊天模型</span><strong>{draftChatConfig.model}</strong><small>{chatProvider}</small></div>
            <span className="settings-overview-dot enabled" />
          </div>
          <div className="settings-overview-metric">
            <span className="settings-overview-metric-icon"><AppIcon name="database" size={17} /></span>
            <div><span>Embedding</span><strong>{draftEmbeddingConfig.model}</strong><small>{embeddingProvider}</small></div>
            <span className="settings-overview-dot enabled" />
          </div>
          <div className="settings-overview-metric">
            <span className="settings-overview-metric-icon"><AppIcon name="key" size={17} /></span>
            <div><span>MCP 服务</span><strong>已启用</strong><small>/mcp</small></div>
            <span className="settings-overview-dot enabled" />
          </div>
        </section>

        <section className="settings-overview-section">
          <header><h3>当前配置</h3><p>快速确认模型和接入状态。</p></header>
          <div className="settings-overview-config-list">
            <div className="settings-overview-config-row">
              <div><strong>聊天模型</strong><p>{chatProvider} · {draftChatConfig.baseUrl}</p></div>
              <div className="settings-overview-config-value"><span>{draftChatConfig.model}</span><small>上下文 {draftChatConfig.contextMessageLimit} 条</small></div>
            </div>
            <div className="settings-overview-config-row">
              <div><strong>Embedding 模型</strong><p>{embeddingProvider} · {draftEmbeddingConfig.baseUrl}</p></div>
              <div className="settings-overview-config-value"><span>{draftEmbeddingConfig.model}</span></div>
            </div>
            <div className="settings-overview-config-row">
              <div><strong>MCP 服务</strong><p>JSON-RPC over HTTP</p></div>
              <div className="settings-overview-config-value"><span className="settings-status-like enabled">已启用</span><small>/mcp</small></div>
            </div>
          </div>
        </section>
      </div>
    )
  }

  const renderModels = () => (
    <div className="settings-tab-content settings-models-page">
      <div className="settings-subnav" aria-label="模型类型" role="tablist">
        <button type="button" className={activeModelSection === 'chat' ? 'active' : ''} onClick={() => setActiveModelSection('chat')}>
          <AppIcon name="message" size={16} /><span>聊天模型</span>
        </button>
        <button type="button" className={activeModelSection === 'embedding' ? 'active' : ''} onClick={() => setActiveModelSection('embedding')}>
          <AppIcon name="database" size={16} /><span>Embedding</span>
        </button>
      </div>

      {activeModelSection === 'chat' ? (
        <section className="settings-config-panel" aria-label="聊天模型配置">
          <header className="settings-config-panel-header">
            <div><span className="settings-config-panel-icon"><AppIcon name="brain" size={18} /></span><div><h3>聊天模型</h3><p>配置对话生成和上下文窗口。</p></div></div>
            <span className="settings-compact-status enabled">已配置</span>
          </header>
          <section className="settings-form-section">
            <header><h4>连接</h4><p>模型服务地址和身份凭据。</p></header>
            <div className="settings-form-grid settings-form-grid-dense">
              <div className="settings-form-group"><label className="settings-form-label" htmlFor="chat-provider">Provider</label><select id="chat-provider" value={draftChatConfig.provider} onChange={(event) => updateChatConfig('provider', event.target.value as ChatConfig['provider'])}><option value="ollama">Ollama</option><option value="openai-compatible">OpenAI Compatible</option></select></div>
              <div className="settings-form-group"><label className="settings-form-label" htmlFor="chat-base-url">Base URL</label><input id="chat-base-url" value={draftChatConfig.baseUrl} onChange={(event) => updateChatConfig('baseUrl', event.target.value)} /></div>
              <div className="settings-form-group"><label className="settings-form-label" htmlFor="chat-model">Model</label><input id="chat-model" value={draftChatConfig.model} onChange={(event) => updateChatConfig('model', event.target.value)} /></div>
              <div className="settings-form-group"><label className="settings-form-label" htmlFor="chat-api-key">API Key</label><input id="chat-api-key" type="password" value={draftChatConfig.apiKey} onChange={(event) => updateChatConfig('apiKey', event.target.value)} placeholder="选填" /></div>
            </div>
          </section>
          <section className="settings-form-section">
            <header><h4>生成</h4><p>控制回答随机性和上下文规模。</p></header>
            <div className="settings-form-grid settings-form-grid-dense">
              <div className="settings-form-group settings-form-group-full"><label className="settings-form-label settings-form-label-inline" htmlFor="chat-temperature"><span>Temperature</span><strong>{draftChatConfig.temperature.toFixed(1)}</strong></label><input id="chat-temperature" type="range" min="0" max="1" step="0.1" value={draftChatConfig.temperature} onChange={(event) => updateChatConfig('temperature', Number(event.target.value))} /></div>
              <div className="settings-form-group"><label className="settings-form-label" htmlFor="chat-context-limit">上下文消息数量</label><input id="chat-context-limit" type="number" min="1" max="100" value={draftChatConfig.contextMessageLimit} onChange={(event) => updateChatConfig('contextMessageLimit', Number(event.target.value))} /><small>范围 1-100。</small></div>
            </div>
          </section>
        </section>
      ) : (
        <section className="settings-config-panel" aria-label="Embedding 模型配置">
          <header className="settings-config-panel-header">
            <div><span className="settings-config-panel-icon"><AppIcon name="database" size={18} /></span><div><h3>Embedding</h3><p>配置文档索引和语义召回模型。</p></div></div>
            <span className="settings-compact-status enabled">已配置</span>
          </header>
          <section className="settings-form-section">
            <header><h4>连接</h4><p>Embedding 服务地址、模型和凭据。</p></header>
            <div className="settings-form-grid settings-form-grid-dense">
              <div className="settings-form-group"><label className="settings-form-label" htmlFor="embedding-provider">Provider</label><select id="embedding-provider" value={draftEmbeddingConfig.provider} onChange={(event) => updateEmbeddingConfig('provider', event.target.value as EmbeddingConfig['provider'])}><option value="ollama">Ollama</option><option value="openai-compatible">OpenAI Compatible</option></select></div>
              <div className="settings-form-group"><label className="settings-form-label" htmlFor="embedding-base-url">Base URL</label><input id="embedding-base-url" value={draftEmbeddingConfig.baseUrl} onChange={(event) => updateEmbeddingConfig('baseUrl', event.target.value)} /></div>
              <div className="settings-form-group"><label className="settings-form-label" htmlFor="embedding-model">Model</label><input id="embedding-model" value={draftEmbeddingConfig.model} onChange={(event) => updateEmbeddingConfig('model', event.target.value)} /></div>
              <div className="settings-form-group"><label className="settings-form-label" htmlFor="embedding-api-key">API Key</label><input id="embedding-api-key" type="password" value={draftEmbeddingConfig.apiKey} onChange={(event) => updateEmbeddingConfig('apiKey', event.target.value)} placeholder="选填" /></div>
            </div>
          </section>
        </section>
      )}
    </div>
  )

  const renderMcp = () => (
    <div className="settings-tab-content">
      <section className="settings-config-panel">
        <header className="settings-config-panel-header">
          <div><span className="settings-config-panel-icon"><AppIcon name="key" size={18} /></span><div><h3>MCP HTTP 接入</h3><p>当前后端真实支持的单端点连接方式。</p></div></div>
          <span className="settings-compact-status enabled">已启用</span>
        </header>
        <section className="settings-form-section">
          <div className="mcp-config-chip-list"><span className="mcp-config-chip">POST /mcp</span><span className="mcp-config-chip">Bearer Token</span><span className="mcp-config-chip">JSON-RPC over HTTP</span></div>
          <p className="mcp-config-description">应用访问令牌通过 <code>Authorization: Bearer &lt;token&gt;</code> 请求头传递，与模型 API Key 无关。</p>
        </section>
        <div className="mcp-config-example">
          <div className="mcp-config-example-header"><div><span className="mcp-config-label">客户端配置</span><h4>复制 MCP 连接配置</h4></div><button type="button" className="mcp-copy-btn" onClick={handleCopyMCPExample}>{showCopySuccessHint ? '已复制' : '复制配置'}</button></div>
          <pre className="mcp-config-code"><code>{mcpClientConfigExample}</code></pre>
        </div>
      </section>
    </div>
  )

  return (
    <section className="settings-workspace app-workspace" aria-labelledby="settings-workspace-title">
      <header className="workspace-page-header settings-workspace-header">
        <div><span className="workspace-page-kicker">AI LocalBase</span><h2 id="settings-workspace-title">设置</h2><p>管理本地模型与 MCP 接入。</p></div>
        <button className="workspace-page-back" onClick={handleClose} aria-label="返回聊天" title="返回聊天" type="button"><AppIcon name="chevronLeft" size={20} /></button>
      </header>

      <div className={`settings-layout ${isMobileDetailOpen ? 'mobile-detail-open' : ''}`}>
        <aside className="settings-sidebar" aria-label="设置分类">
          <nav className="settings-nav" aria-label="设置分类">
            {settingsNavItems.map((item) => (
              <button key={item.id} type="button" className={`settings-nav-item ${activeTab === item.id ? 'active' : ''}`} aria-current={activeTab === item.id ? 'page' : undefined} onClick={() => handleTabChange(item.id)}>
                <span className="settings-nav-icon"><AppIcon name={item.icon} size={18} /></span>
                <span className="settings-nav-text"><span className="settings-nav-label">{item.label}</span><span className="settings-nav-description">{item.description}</span></span>
                <span className="settings-nav-trailing"><AppIcon name="chevronRight" size={16} /></span>
              </button>
            ))}
          </nav>
        </aside>

        <main className="settings-main">
          <header className="settings-main-header">
            <button className="settings-mobile-back" onClick={() => setIsMobileDetailOpen(false)} aria-label="返回设置分类" title="返回设置分类" type="button"><AppIcon name="chevronLeft" size={18} /></button>
            <div><h3>{activeNavItem.label}</h3><p className="settings-main-visible-description">{activeNavItem.description}</p></div>
          </header>
          <section className="settings-content-scroll" key={activeTab} role="region" tabIndex={0}>
            {saveNotice && <div className={`settings-save-notice ${saveNotice.type}`}>{saveNotice.text}</div>}
            {activeTab === 'overview' ? renderOverview() : activeTab === 'models' ? renderModels() : renderMcp()}
          </section>

          <footer className={`settings-save-bar ${saveNotice?.type === 'error' ? 'has-error' : ''}`}>
            <div className="settings-save-state" role="status" aria-live="polite">
              <span className="settings-save-state-icon"><AppIcon className={isSaving ? 'settings-save-spinner' : undefined} name={saveNotice?.type === 'error' ? 'alert' : isSaving ? 'loader' : 'check'} size={16} /></span>
              <span><strong>{saveNotice?.type === 'error' ? '保存失败' : isSaving ? '正在保存' : isDirty ? '有未保存的更改' : '所有更改已保存'}</strong><small>修改只在点击保存后写入后端。</small></span>
            </div>
            <div className="settings-save-actions"><button className="settings-action-btn" disabled={!isDirty || isSaving} onClick={handleDiscard} type="button">放弃更改</button><button className="settings-action-btn settings-action-btn-primary" disabled={!isDirty || isSaving} onClick={() => void handleSave()} type="button">{isSaving ? '保存中…' : '保存'}</button></div>
          </footer>
        </main>
      </div>
    </section>
  )
}

export default SettingsPanel
