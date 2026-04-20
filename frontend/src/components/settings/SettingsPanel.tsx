import React, { useEffect, useRef, useState } from 'react'
import { AppConfig, ChatConfig, EmbeddingConfig } from '../../App'

interface SettingsPanelProps {
  config: AppConfig
  onClose: () => void
  onSaveChatConfig: (value: ChatConfig) => Promise<void>
  onSaveEmbeddingConfig: (value: EmbeddingConfig) => Promise<void>
}

const SettingsPanel: React.FC<SettingsPanelProps> = ({
  config,
  onClose,
  onSaveChatConfig,
  onSaveEmbeddingConfig,
}) => {
  const [draftChatConfig, setDraftChatConfig] = useState(config.chat)
  const [draftEmbeddingConfig, setDraftEmbeddingConfig] = useState(config.embedding)
  const [saveNotice, setSaveNotice] = useState<{
    type: 'success' | 'error'
    text: string
  } | null>(null)
  const saveNoticeTimerRef = useRef<number | null>(null)

  useEffect(() => {
    setDraftChatConfig(config.chat)
    setDraftEmbeddingConfig(config.embedding)
  }, [config])

  useEffect(() => {
    return () => {
      if (saveNoticeTimerRef.current) {
        window.clearTimeout(saveNoticeTimerRef.current)
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

  const persistChatConfig = async () => {
    if (JSON.stringify(draftChatConfig) === JSON.stringify(config.chat)) {
      return
    }
    try {
      await onSaveChatConfig(draftChatConfig)
      showSaveNotice('success', '聊天模型设置已保存')
    } catch (error) {
      const message = error instanceof Error ? error.message : '聊天模型设置保存失败'
      showSaveNotice('error', `保存失败：${message}`)
    }
  }

  const persistEmbeddingConfig = async () => {
    if (JSON.stringify(draftEmbeddingConfig) === JSON.stringify(config.embedding)) {
      return
    }
    try {
      await onSaveEmbeddingConfig(draftEmbeddingConfig)
      showSaveNotice('success', 'Embedding 模型设置已保存')
    } catch (error) {
      const message = error instanceof Error ? error.message : 'Embedding 模型设置保存失败'
      showSaveNotice('error', `保存失败：${message}`)
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
          <button type="button" className="ghost-btn settings-close-btn" onClick={onClose}>
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
                  onChange={(event) =>
                    setDraftChatConfig((prev) => ({
                      ...prev,
                      provider: event.target.value as ChatConfig['provider'],
                    }))
                  }
                  onBlur={() => {
                    void persistChatConfig()
                  }}
                >
                  <option value="ollama">Ollama</option>
                  <option value="openai-compatible">OpenAI Compatible</option>
                </select>
              </label>

              <label className="settings-field">
                <span>Base URL</span>
                <input
                  value={draftChatConfig.baseUrl}
                  onChange={(event) =>
                    setDraftChatConfig((prev) => ({
                      ...prev,
                      baseUrl: event.target.value,
                    }))
                  }
                  onBlur={() => {
                    void persistChatConfig()
                  }}
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
                  onChange={(event) =>
                    setDraftChatConfig((prev) => ({
                      ...prev,
                      model: event.target.value,
                    }))
                  }
                  onBlur={() => {
                    void persistChatConfig()
                  }}
                  placeholder="llama3.2"
                />
              </label>

              <label className="settings-field">
                <span>API Key</span>
                <input
                  type="password"
                  value={draftChatConfig.apiKey}
                  onChange={(event) =>
                    setDraftChatConfig((prev) => ({
                      ...prev,
                      apiKey: event.target.value,
                    }))
                  }
                  onBlur={() => {
                    void persistChatConfig()
                  }}
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
                  onChange={(event) =>
                    setDraftChatConfig((prev) => ({
                      ...prev,
                      temperature: Number(event.target.value),
                    }))
                  }
                  onBlur={() => {
                    void persistChatConfig()
                  }}
                />
              </label>

              <label className="settings-field settings-field-full">
                <span>上下文消息数量</span>
                <input
                  type="number"
                  min="1"
                  max="100"
                  value={draftChatConfig.contextMessageLimit}
                  onChange={(event) =>
                    setDraftChatConfig((prev) => ({
                      ...prev,
                      contextMessageLimit: Number(event.target.value),
                    }))
                  }
                  onBlur={() => {
                    void persistChatConfig()
                  }}
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
                  onChange={(event) =>
                    setDraftEmbeddingConfig((prev) => ({
                      ...prev,
                      provider: event.target.value as EmbeddingConfig['provider'],
                    }))
                  }
                  onBlur={() => {
                    void persistEmbeddingConfig()
                  }}
                >
                  <option value="ollama">Ollama</option>
                  <option value="openai-compatible">OpenAI Compatible</option>
                </select>
              </label>

              <label className="settings-field">
                <span>Base URL</span>
                <input
                  value={draftEmbeddingConfig.baseUrl}
                  onChange={(event) =>
                    setDraftEmbeddingConfig((prev) => ({
                      ...prev,
                      baseUrl: event.target.value,
                    }))
                  }
                  onBlur={() => {
                    void persistEmbeddingConfig()
                  }}
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
                  onChange={(event) =>
                    setDraftEmbeddingConfig((prev) => ({
                      ...prev,
                      model: event.target.value,
                    }))
                  }
                  onBlur={() => {
                    void persistEmbeddingConfig()
                  }}
                  placeholder="nomic-embed-text"
                />
              </label>

              <label className="settings-field">
                <span>API Key</span>
                <input
                  type="password"
                  value={draftEmbeddingConfig.apiKey}
                  onChange={(event) =>
                    setDraftEmbeddingConfig((prev) => ({
                      ...prev,
                      apiKey: event.target.value,
                    }))
                  }
                  onBlur={() => {
                    void persistEmbeddingConfig()
                  }}
                  placeholder="选填"
                />
              </label>
            </div>
          </section>
        </div>
      </div>
    </div>
  )
}

export default SettingsPanel
