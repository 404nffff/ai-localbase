import React, { useState } from 'react'
import type { AppConfig, ChatConfig, ChatModeSettings, EmbeddingConfig, RetrievalConfig } from '../../App'
import { ModelConfigTest } from './ModelConfigTest'

interface SettingsPanelProps {
  config: AppConfig
  onClose: () => void
  onChatConfigChange: <K extends keyof ChatConfig>(key: K, value: ChatConfig[K]) => void
  onEmbeddingConfigChange: <K extends keyof EmbeddingConfig>(
    key: K,
    value: EmbeddingConfig[K],
  ) => void
  onRetrievalConfigChange: <K extends keyof RetrievalConfig>(
    key: K,
    value: RetrievalConfig[K],
  ) => void
  chatModeSettings: ChatModeSettings
  onThinkModelChange: (value: string) => void
  onCopyMcpToken: () => Promise<void>
  onResetMcpToken: () => Promise<void>
}

const SettingsPanel: React.FC<SettingsPanelProps> = ({
  config,
  onClose,
  onChatConfigChange,
  onEmbeddingConfigChange,
  onRetrievalConfigChange,
  chatModeSettings,
  onThinkModelChange,
  onCopyMcpToken,
  onResetMcpToken,
}) => {
  const [mcpFeedback, setMcpFeedback] = useState('')
  const [isMcpTokenVisible, setIsMcpTokenVisible] = useState(false)
  const chatProviderLabel = config.chat.provider === 'ollama' ? 'Ollama' : 'OpenAI Compatible'
  const embeddingProviderLabel = config.embedding.provider === 'ollama' ? 'Ollama' : 'OpenAI Compatible'
  const retrievalModeLabel = config.retrieval.defaultSearchMode === 'hybrid' ? '混合检索' : '向量检索'
  const mcpStatusLabel = config.mcp.enabled ? '已启用' : '未启用'

  const handleCopyToken = async () => {
    try {
      await onCopyMcpToken()
      setMcpFeedback('Token 已复制')
    } catch {
      setMcpFeedback('复制失败')
    }
  }

  const handleResetToken = async () => {
    try {
      await onResetMcpToken()
      setMcpFeedback('Token 已重置')
    } catch {
      setMcpFeedback('重置失败')
    }
  }
  return (
    <div className="settings-modal-backdrop" onClick={onClose}>
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
          <div className="settings-summary-grid">
            <div className="settings-summary-item">
              <span>聊天模型</span>
              <strong>{config.chat.model || '未配置'}</strong>
              <small>{chatProviderLabel}</small>
            </div>
            <div className="settings-summary-item">
              <span>Embedding</span>
              <strong>{config.embedding.model || '未配置'}</strong>
              <small>{embeddingProviderLabel}</small>
            </div>
            <div className="settings-summary-item">
              <span>检索策略</span>
              <strong>{retrievalModeLabel}</strong>
              <small>{config.retrieval.rerankStrategy === 'semantic' ? '语义重排' : '关键词融合'}</small>
            </div>
            <div className="settings-summary-item">
              <span>MCP</span>
              <strong>{mcpStatusLabel}</strong>
              <small>{config.mcp.basePath || '未配置路径'}</small>
            </div>
          </div>

          <section className="settings-panel-block ai-config-panel single-column">
            <div className="settings-section-head">
              <div>
                <h3>聊天模型</h3>
                <p>控制普通对话、上下文窗口和思考模式使用的模型。</p>
              </div>
            </div>

            <div className="ai-config-fields">
              <label className="settings-field">
                <span>Provider</span>
                <select
                  value={config.chat.provider}
                  onChange={(event) =>
                    onChatConfigChange('provider', event.target.value as ChatConfig['provider'])
                  }
                >
                  <option value="ollama">Ollama</option>
                  <option value="openai-compatible">OpenAI Compatible</option>
                </select>
              </label>

              <label className="settings-field">
                <span>Base URL</span>
                <input
                  value={config.chat.baseUrl}
                  onChange={(event) => onChatConfigChange('baseUrl', event.target.value)}
                  placeholder={
                    config.chat.provider === 'ollama'
                      ? 'http://localhost:11434'
                      : 'http://localhost:11434/v1'
                  }
                />
              </label>

              <label className="settings-field">
                <span>Model</span>
                <input
                  value={config.chat.model}
                  onChange={(event) => onChatConfigChange('model', event.target.value)}
                  placeholder="llama3.2"
                />
              </label>

              <label className="settings-field">
                <span>API Key</span>
                <input
                  type="password"
                  value={config.chat.apiKey}
                  onChange={(event) => onChatConfigChange('apiKey', event.target.value)}
                  placeholder="选填"
                />
              </label>

              <label className="settings-field settings-field-full">
                <span>Temperature: {config.chat.temperature.toFixed(1)}</span>
                <input
                  type="range"
                  min="0"
                  max="1"
                  step="0.1"
                  value={config.chat.temperature}
                  onChange={(event) =>
                    onChatConfigChange('temperature', Number(event.target.value))
                  }
                />
              </label>

              <label className="settings-field settings-field-full">
                <span>上下文消息数量</span>
                <input
                  type="number"
                  min="1"
                  max="100"
                  value={config.chat.contextMessageLimit}
                  onChange={(event) =>
                    onChatConfigChange('contextMessageLimit', Number(event.target.value))
                  }
                  placeholder="12"
                />
                <small>限制每次发送给模型的最近消息条数，范围 1-100。</small>
              </label>

              <div className="settings-field settings-field-full">
                <ModelConfigTest
                  type="chat"
                  provider={config.chat.provider}
                  baseUrl={config.chat.baseUrl}
                  modelName={config.chat.model}
                  apiKey={config.chat.apiKey}
                  temperature={config.chat.temperature}
                />
              </div>

              <label className="settings-field settings-field-full">
                <span>思考模式模型</span>
                <input
                  value={chatModeSettings.thinkModel}
                  onChange={(event) => onThinkModelChange(event.target.value)}
                  placeholder="deepseek-r1:8b"
                />
                <small>用于“思考模式”的专用模型，建议填写推理更强但更慢的模型。</small>
              </label>
            </div>
          </section>

          <section className="settings-panel-block ai-config-panel single-column">
            <div className="settings-section-head">
              <div>
                <h3>Embedding 模型</h3>
                <p>控制文档索引和语义召回使用的向量模型。</p>
              </div>
            </div>

            <div className="ai-config-fields">
              <label className="settings-field">
                <span>Provider</span>
                <select
                  value={config.embedding.provider}
                  onChange={(event) =>
                    onEmbeddingConfigChange(
                      'provider',
                      event.target.value as EmbeddingConfig['provider'],
                    )
                  }
                >
                  <option value="ollama">Ollama</option>
                  <option value="openai-compatible">OpenAI Compatible</option>
                </select>
              </label>

              <label className="settings-field">
                <span>Base URL</span>
                <input
                  value={config.embedding.baseUrl}
                  onChange={(event) => onEmbeddingConfigChange('baseUrl', event.target.value)}
                  placeholder={
                    config.embedding.provider === 'ollama'
                      ? 'http://localhost:11434'
                      : 'http://localhost:11434/v1'
                  }
                />
              </label>

              <label className="settings-field">
                <span>Model</span>
                <input
                  value={config.embedding.model}
                  onChange={(event) => onEmbeddingConfigChange('model', event.target.value)}
                  placeholder="nomic-embed-text"
                />
              </label>

              <label className="settings-field">
                <span>API Key</span>
                <input
                  type="password"
                  value={config.embedding.apiKey}
                  onChange={(event) => onEmbeddingConfigChange('apiKey', event.target.value)}
                  placeholder="选填"
                />
              </label>

              <div className="settings-field settings-field-full">
                <ModelConfigTest
                  type="embedding"
                  provider={config.embedding.provider}
                  baseUrl={config.embedding.baseUrl}
                  modelName={config.embedding.model}
                  apiKey={config.embedding.apiKey}
                />
              </div>
            </div>
          </section>

          <section className="settings-panel-block ai-config-panel single-column">
            <div className="settings-section-head">
              <div>
                <h3>高级检索</h3>
                <p>调整召回、重排、改写和低置信补强策略。</p>
              </div>
            </div>

            <div className="ai-config-fields">
              <div className="settings-field-full settings-form-group">
                <div className="settings-form-group-head">
                  <strong>召回策略</strong>
                  <span>决定先用什么方式召回，再如何排序。</span>
                </div>
                <div className="settings-group-grid">
                  <label className="settings-field">
                    <span>默认模式</span>
                    <select
                      value={config.retrieval.defaultSearchMode}
                      onChange={(event) =>
                        onRetrievalConfigChange(
                          'defaultSearchMode',
                          event.target.value as RetrievalConfig['defaultSearchMode'],
                        )
                      }
                    >
                      <option value="dense">向量检索</option>
                      <option value="hybrid">混合检索</option>
                    </select>
                  </label>

                  <label className="settings-field settings-field-toggle settings-field-toggle--inline">
                    <span>启用混合检索</span>
                    <input
                      type="checkbox"
                      checked={config.retrieval.hybridSearchEnabled}
                      onChange={(event) =>
                        onRetrievalConfigChange('hybridSearchEnabled', event.target.checked)
                      }
                    />
                  </label>

                  <label className="settings-field">
                    <span>重排策略</span>
                    <select
                      value={config.retrieval.rerankStrategy}
                      onChange={(event) =>
                        onRetrievalConfigChange(
                          'rerankStrategy',
                          event.target.value as RetrievalConfig['rerankStrategy'],
                        )
                      }
                    >
                      <option value="keyword">关键词融合</option>
                      <option value="semantic">语义重排</option>
                    </select>
                  </label>

                  <label className="settings-field settings-field-toggle settings-field-toggle--inline">
                    <span>启用问题改写</span>
                    <input
                      type="checkbox"
                      checked={config.retrieval.enableQueryRewrite}
                      onChange={(event) =>
                        onRetrievalConfigChange('enableQueryRewrite', event.target.checked)
                      }
                    />
                  </label>

                  <label className="settings-field">
                    <span>改写数量</span>
                    <input
                      type="number"
                      min="1"
                      max="5"
                      value={config.retrieval.queryRewriteMaxVariants}
                      onChange={(event) =>
                        onRetrievalConfigChange('queryRewriteMaxVariants', Number(event.target.value))
                      }
                    />
                  </label>
                </div>
              </div>

              <div className="settings-field-full settings-form-group">
                <div className="settings-form-group-head">
                  <strong>召回规模</strong>
                  <span>控制候选集大小和最终进入上下文的片段数量。</span>
                </div>
                <div className="settings-group-grid">
                  <label className="settings-field">
                    <span>文档 TopK</span>
                    <input
                      type="number"
                      min="1"
                      max="30"
                      value={config.retrieval.topKDocument}
                      onChange={(event) =>
                        onRetrievalConfigChange('topKDocument', Number(event.target.value))
                      }
                    />
                  </label>

                  <label className="settings-field">
                    <span>文档候选 TopK</span>
                    <input
                      type="number"
                      min={config.retrieval.topKDocument}
                      max="80"
                      value={config.retrieval.candidateTopKDocument}
                      onChange={(event) =>
                        onRetrievalConfigChange('candidateTopKDocument', Number(event.target.value))
                      }
                    />
                  </label>

                  <label className="settings-field">
                    <span>知识库 TopK</span>
                    <input
                      type="number"
                      min="1"
                      max="40"
                      value={config.retrieval.topKKnowledgeBase}
                      onChange={(event) =>
                        onRetrievalConfigChange('topKKnowledgeBase', Number(event.target.value))
                      }
                    />
                  </label>

                  <label className="settings-field">
                    <span>知识库候选 TopK</span>
                    <input
                      type="number"
                      min={config.retrieval.topKKnowledgeBase}
                      max="120"
                      value={config.retrieval.candidateTopKAllDocs}
                      onChange={(event) =>
                        onRetrievalConfigChange('candidateTopKAllDocs', Number(event.target.value))
                      }
                    />
                  </label>

                  <label className="settings-field">
                    <span>每文档片段数</span>
                    <input
                      type="number"
                      min="1"
                      max="10"
                      value={config.retrieval.maxChunksPerDocument}
                      onChange={(event) =>
                        onRetrievalConfigChange('maxChunksPerDocument', Number(event.target.value))
                      }
                    />
                  </label>
                </div>
              </div>

              <div className="settings-field-full settings-form-group">
                <div className="settings-form-group-head">
                  <strong>上下文与补强</strong>
                  <span>控制进入回答前的证据长度和低置信兜底。</span>
                </div>
                <div className="settings-group-grid settings-group-grid--context">
                  <label className="settings-field">
                    <span>上下文字符</span>
                    <input
                      type="number"
                      min="800"
                      max="20000"
                      step="100"
                      value={config.retrieval.maxContextChars}
                      onChange={(event) =>
                        onRetrievalConfigChange('maxContextChars', Number(event.target.value))
                      }
                    />
                  </label>

                  <label className="settings-field settings-field-toggle settings-field-toggle--described">
                    <span>低置信自动扩展</span>
                    <input
                      type="checkbox"
                      checked={config.retrieval.enableLowConfidenceBoost}
                      onChange={(event) =>
                        onRetrievalConfigChange('enableLowConfidenceBoost', event.target.checked)
                      }
                    />
                    <small>当知识库范围召回置信偏低时，扩大候选并尝试补充更多片段。</small>
                  </label>
                </div>
              </div>
            </div>
          </section>

          <section className="settings-panel-block ai-config-panel single-column">
            <div className="settings-section-head">
              <div>
                <h3>MCP 设置</h3>
                <p>管理外部工具调用入口和访问 Token。</p>
              </div>
            </div>

            <div className="ai-config-fields">
              <label className="settings-field">
                <span>状态</span>
                <input value={config.mcp.enabled ? '已启用' : '未启用'} readOnly />
              </label>

              <label className="settings-field">
                <span>Base Path</span>
                <input value={config.mcp.basePath} readOnly />
              </label>

              <label className="settings-field settings-field-full">
                <span>Token</span>
                <div className="settings-inline-actions">
                  <input
                    type={isMcpTokenVisible ? 'text' : 'password'}
                    value={config.mcp.token}
                    readOnly
                    className="settings-token-input"
                  />
                  <button
                    type="button"
                    className="ghost-btn settings-visibility-btn"
                    onClick={() => setIsMcpTokenVisible((visible) => !visible)}
                    aria-label={isMcpTokenVisible ? '隐藏 Token' : '显示 Token'}
                    title={isMcpTokenVisible ? '隐藏 Token' : '显示 Token'}
                  >
                    {isMcpTokenVisible ? '隐藏' : '显示'}
                  </button>
                  <button type="button" className="ghost-btn" onClick={() => void handleCopyToken()}>
                    复制
                  </button>
                  <button type="button" className="ghost-btn" onClick={() => void handleResetToken()}>
                    重置
                  </button>
                </div>
                <small>
                  用于访问 MCP 接口的 Bearer Token。重置后旧 Token 会立刻失效。
                </small>
                {mcpFeedback ? <small className="settings-feedback">{mcpFeedback}</small> : null}
              </label>
            </div>
          </section>
        </div>
      </div>
    </div>
  )
}

export default SettingsPanel
