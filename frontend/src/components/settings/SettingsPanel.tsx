import React, { useEffect, useMemo, useRef, useState } from 'react'
import type { AppConfig, ChatConfig, EmbeddingConfig } from '../../App'

interface SettingsPanelProps {
  config: AppConfig
  onClose: () => void
  onSaveConfig: (value: AppConfig) => Promise<AppConfig>
}

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

const SettingsPanel: React.FC<SettingsPanelProps> = ({
  config,
  onClose,
  onSaveConfig,
}) => {
  // 复制 MCP 客户端配置，直接贴到支持 TOML 的客户端配置文件里即可使用。
  const mcpClientConfigExample = `[mcp_servers.ai_localbase]
url = "http://127.0.0.1:8080/mcp"
startup_timeout_sec = 120.0
http_headers = { "Authorization" = "Bearer your-app-access-token" }`
  const [baselineConfig, setBaselineConfig] = useState(config)
  const [draftConfig, setDraftConfig] = useState(config)
  const [isSaving, setIsSaving] = useState(false)
  const [saveNotice, setSaveNotice] = useState<{
    type: 'success' | 'error'
    text: string
  } | null>(null)
  const saveNoticeTimerRef = useRef<number | null>(null)
  const [showCopySuccessHint, setShowCopySuccessHint] = useState(false)
  const copySuccessTimerRef = useRef<number | null>(null)
  const isDirty = useMemo(
    () => hasSettingsConfigChanges(baselineConfig, draftConfig),
    [baselineConfig, draftConfig],
  )
  const draftChatConfig = draftConfig.chat
  const draftEmbeddingConfig = draftConfig.embedding

  useEffect(() => {
    if (isDirty || isSaving) return
    setBaselineConfig(config)
    setDraftConfig(config)
  }, [config, isDirty, isSaving])

  useEffect(() => {
    return () => {
      if (saveNoticeTimerRef.current) {
        window.clearTimeout(saveNoticeTimerRef.current)
      }
      if (copySuccessTimerRef.current) {
        window.clearTimeout(copySuccessTimerRef.current)
      }
    }
  }, [])

  const showSaveNotice = (type: 'success' | 'error', text: string) => {
    setSaveNotice({ type, text })
    if (saveNoticeTimerRef.current) {
      window.clearTimeout(saveNoticeTimerRef.current)
    }
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

  const handleClose = () => {
    if (isDirty && !window.confirm('当前设置尚未保存，确认放弃更改并关闭吗？')) {
      return
    }
    onClose()
  }

  const handleCopyMCPExample = async () => {
    try {
      await navigator.clipboard.writeText(mcpClientConfigExample)
      setShowCopySuccessHint(true)
      if (copySuccessTimerRef.current) {
        window.clearTimeout(copySuccessTimerRef.current)
      }
      copySuccessTimerRef.current = window.setTimeout(() => {
        setShowCopySuccessHint(false)
        copySuccessTimerRef.current = null
      }, 1800)
      showSaveNotice('success', 'MCP 客户端配置已复制')
    } catch (error) {
      setShowCopySuccessHint(false)
      const message = error instanceof Error ? error.message : '复制 MCP 客户端配置失败'
      showSaveNotice('error', `复制失败：${message}`)
    }
  }

  return (
    <div className="settings-modal-backdrop">
      <div className="settings-modal settings-modal-single" onClick={(event) => event.stopPropagation()}>
        <div className="settings-modal-header">
          <div>
            <h3>AI 设置</h3>
            <p>分别管理聊天模型与 Embedding 模型配置</p>
          </div>
          <button type="button" className="ghost-btn settings-close-btn" onClick={handleClose}>
            关闭
          </button>
        </div>

        <div className="settings-modal-scroll">
          {saveNotice && (
            <div className={`settings-save-notice ${saveNotice.type}`}>
              {saveNotice.text}
            </div>
          )}

          <section className="settings-panel-block ai-config-panel single-column">
            <div className="section-title-row knowledge-panel-header">
              <h3>聊天模型</h3>
            </div>

            <div className="ai-config-fields">
              <label className="settings-field">
                <span>Provider</span>
                <select
                  value={draftChatConfig.provider}
                  onChange={(event) => updateChatConfig(
                    'provider',
                    event.target.value as ChatConfig['provider'],
                  )}
                >
                  <option value="ollama">Ollama</option>
                  <option value="openai-compatible">OpenAI Compatible</option>
                </select>
              </label>

              <label className="settings-field">
                <span>Base URL</span>
                <input
                  value={draftChatConfig.baseUrl}
                  onChange={(event) => updateChatConfig('baseUrl', event.target.value)}
                  placeholder={
                    draftChatConfig.provider === 'ollama'
                      ? 'http://localhost:11434'
                      : 'http://localhost:11434/v1'
                  }
                />
              </label>

              <label className="settings-field">
                <span>Model</span>
                <input
                  value={draftChatConfig.model}
                  onChange={(event) => updateChatConfig('model', event.target.value)}
                  placeholder="llama3.2"
                />
              </label>

              <label className="settings-field">
                <span>API Key</span>
                <input
                  type="password"
                  value={draftChatConfig.apiKey}
                  onChange={(event) => updateChatConfig('apiKey', event.target.value)}
                  placeholder="选填"
                />
              </label>

              <label className="settings-field settings-field-full">
                <span>Temperature: {draftChatConfig.temperature.toFixed(1)}</span>
                <input
                  type="range"
                  min="0"
                  max="1"
                  step="0.1"
                  value={draftChatConfig.temperature}
                  onChange={(event) => updateChatConfig('temperature', Number(event.target.value))}
                />
              </label>

              <label className="settings-field settings-field-full">
                <span>上下文消息数量</span>
                <input
                  type="number"
                  min="1"
                  max="100"
                  value={draftChatConfig.contextMessageLimit}
                  onChange={(event) => updateChatConfig(
                    'contextMessageLimit',
                    Number(event.target.value),
                  )}
                  placeholder="12"
                />
                <small>限制每次发送给模型的最近消息条数，范围 1-100。</small>
              </label>
            </div>
          </section>

          <section className="settings-panel-block ai-config-panel single-column">
            <div className="section-title-row knowledge-panel-header">
              <h3>Embedding 模型</h3>
            </div>

            <div className="ai-config-fields">
              <label className="settings-field">
                <span>Provider</span>
                <select
                  value={draftEmbeddingConfig.provider}
                  onChange={(event) => updateEmbeddingConfig(
                    'provider',
                    event.target.value as EmbeddingConfig['provider'],
                  )}
                >
                  <option value="ollama">Ollama</option>
                  <option value="openai-compatible">OpenAI Compatible</option>
                </select>
              </label>

              <label className="settings-field">
                <span>Base URL</span>
                <input
                  value={draftEmbeddingConfig.baseUrl}
                  onChange={(event) => updateEmbeddingConfig('baseUrl', event.target.value)}
                  placeholder={
                    draftEmbeddingConfig.provider === 'ollama'
                      ? 'http://localhost:11434'
                      : 'http://localhost:11434/v1'
                  }
                />
              </label>

              <label className="settings-field">
                <span>Model</span>
                <input
                  value={draftEmbeddingConfig.model}
                  onChange={(event) => updateEmbeddingConfig('model', event.target.value)}
                  placeholder="nomic-embed-text"
                />
              </label>

              <label className="settings-field">
                <span>API Key</span>
                <input
                  type="password"
                  value={draftEmbeddingConfig.apiKey}
                  onChange={(event) => updateEmbeddingConfig('apiKey', event.target.value)}
                  placeholder="选填"
                />
              </label>
            </div>
          </section>

          <section className="settings-panel-block ai-config-panel single-column">
            {/* 只展示当前后端真实支持的 MCP HTTP 接入要点，避免误导成通用协议文档。 */}
            <div className="section-title-row knowledge-panel-header">
              <h3>MCP HTTP 接入说明</h3>
            </div>

            <div className="mcp-config-summary">
              <div className="mcp-config-chip-list">
                <span className="mcp-config-chip">POST /mcp</span>
                <span className="mcp-config-chip">Bearer Token</span>
                <span className="mcp-config-chip">JSON-RPC over HTTP</span>
              </div>
              <p className="mcp-config-description">
                当前后端已提供 MCP 单端点入口，接入时请把应用访问令牌放进
                <code>Authorization: Bearer &lt;token&gt;</code> 请求头。这里的 Bearer Token
                是应用访问令牌，不是模型的 API Key。
              </p>
            </div>

            <div className="mcp-config-grid">
              <div className="mcp-config-card">
                <span className="mcp-config-label">Endpoint</span>
                <strong>/mcp</strong>
                <p>使用当前站点同域地址，按 JSON-RPC 请求体发起 POST 请求。</p>
              </div>
              <div className="mcp-config-card">
                <span className="mcp-config-label">调用顺序</span>
                <strong>initialize → tools/list → tools/call</strong>
                <p>先完成初始化，再读取工具清单，最后按工具名发起调用。</p>
              </div>
            </div>

            <div className="mcp-config-example">
              <div className="mcp-config-example-header">
                <div>
                  <span className="mcp-config-label">客户端配置</span>
                  <h4>一键复制 MCP 连接配置</h4>
                </div>
                <div className="mcp-copy-actions">
                  <button type="button" className="mcp-copy-btn" onClick={handleCopyMCPExample}>
                    {showCopySuccessHint ? '已复制' : '复制配置'}
                  </button>
                  {showCopySuccessHint && (
                    <span className="mcp-copy-feedback" role="status" aria-live="polite">
                      复制成功
                    </span>
                  )}
                </div>
              </div>
              <pre className="mcp-config-code">
                <code>{mcpClientConfigExample}</code>
              </pre>
            </div>
          </section>
        </div>

        <div className="settings-save-bar">
          <div className="settings-save-state" role="status" aria-live="polite">
            <strong>
              {isSaving ? '正在保存' : isDirty ? '有未保存的更改' : '所有更改已保存'}
            </strong>
            <span>修改只在点击保存后写入后端。</span>
          </div>
          <div className="settings-save-actions">
            <button
              type="button"
              className="settings-discard-btn"
              disabled={!isDirty || isSaving}
              onClick={handleDiscard}
            >
              放弃更改
            </button>
            <button
              type="button"
              className="settings-confirm-save-btn"
              disabled={!isDirty || isSaving}
              onClick={() => void handleSave()}
            >
              {isSaving ? '保存中…' : '保存'}
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}

export default SettingsPanel
