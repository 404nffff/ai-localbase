import React from 'react'
import type { AppConfig } from '../../../App'
import AppIcon, { type AppIconName } from '../../common/AppIcon'
import AboutSettings from './AboutSettings'

interface GeneralSettingsProps {
  config: AppConfig
  thinkModel: string
}

interface PreferenceRow {
  title: string
  description: string
  value: string
  icon: AppIconName
  meta?: string
  status?: 'enabled' | 'disabled' | 'neutral'
}

interface OverviewMetric {
  label: string
  value: string
  detail: string
  icon: AppIconName
  status?: 'enabled' | 'disabled'
}

const GeneralSettings: React.FC<GeneralSettingsProps> = ({ config, thinkModel }) => {
  const chatProviderLabel = config.chat.provider === 'ollama' ? 'Ollama' : 'OpenAI Compatible'
  const embeddingProviderLabel = config.embedding.provider === 'ollama' ? 'Ollama' : 'OpenAI Compatible'
  const searchModeLabel = config.retrieval.defaultSearchMode === 'hybrid' ? '混合检索' : '向量检索'
  const rerankLabel = config.retrieval.rerankStrategy === 'semantic' ? '语义重排' : '关键词融合'
  const mcpWarnings = config.mcp.deploymentWarnings ?? []

  const overviewMetrics: OverviewMetric[] = [
    {
      label: '聊天模型',
      value: config.chat.model || '未配置',
      detail: chatProviderLabel,
      icon: 'brain',
      status: config.chat.model ? 'enabled' : 'disabled',
    },
    {
      label: '默认检索',
      value: searchModeLabel,
      detail: config.retrieval.hybridSearchEnabled ? '混合召回可用' : '向量召回优先',
      icon: 'database',
      status: 'enabled',
    },
    {
      label: 'MCP 服务',
      value: config.mcp.enabled ? '已启用' : '未启用',
      detail: config.mcp.basePath || '未配置路径',
      icon: 'key',
      status: config.mcp.enabled ? 'enabled' : 'disabled',
    },
  ]

  const preferenceRows: PreferenceRow[] = [
    {
      title: '聊天模型',
      description: `${chatProviderLabel} · ${config.chat.baseUrl || '未配置 Base URL'}`,
      value: config.chat.model || '未配置',
      icon: 'message',
      status: config.chat.model ? 'neutral' : 'disabled',
    },
    {
      title: '生成与推理',
      description: `普通聊天温度 ${config.chat.temperature.toFixed(1)} · 知识库温度 ${config.chat.knowledgeTemperature.toFixed(1)}`,
      value: thinkModel || config.chat.model || '跟随聊天模型',
      icon: 'brain',
      meta: `思考模型 · 上下文 ${config.chat.contextMessageLimit} 条`,
    },
    {
      title: 'Embedding 模型',
      description: `${embeddingProviderLabel} · ${config.embedding.baseUrl || '未配置 Base URL'}`,
      value: config.embedding.model || '未配置',
      icon: 'database',
      status: config.embedding.model ? 'neutral' : 'disabled',
    },
    {
      title: '检索策略',
      description: `${rerankLabel} · ${config.retrieval.enableQueryRewrite ? `${config.retrieval.queryRewriteMaxVariants} 路问题改写` : '不改写问题'}`,
      value: searchModeLabel,
      icon: 'sliders',
      meta: config.retrieval.enableLowConfidenceBoost ? '低置信补强已启用' : '低置信补强未启用',
      status: config.retrieval.hybridSearchEnabled ? 'enabled' : 'neutral',
    },
    {
      title: '召回规模',
      description: `文档 ${config.retrieval.topKDocument}/${config.retrieval.candidateTopKDocument} · 知识库 ${config.retrieval.topKKnowledgeBase}/${config.retrieval.candidateTopKAllDocs}`,
      value: config.retrieval.maxContextChars.toLocaleString(),
      icon: 'search',
      meta: `上下文字符 · 每文档 ${config.retrieval.maxChunksPerDocument} 个片段`,
    },
    {
      title: 'MCP 接入',
      description: `${config.mcp.basePath || '未配置路径'} · ${config.mcp.recommendedAuthMode || 'API Key Scope'}`,
      value: config.mcp.enabled ? '已启用' : '未启用',
      icon: 'key',
      meta: `危险操作：${config.mcp.dangerConfirmationMode || 'confirmNonce'}`,
      status: config.mcp.enabled ? 'enabled' : 'disabled',
    },
  ]

  return (
    <div className="settings-tab-content settings-overview-page">
      <section className="settings-overview-summary" aria-label="当前运行状态">
        {overviewMetrics.map((metric) => (
          <div className="settings-overview-metric" key={metric.label}>
            <span className="settings-overview-metric-icon" aria-hidden="true">
              <AppIcon name={metric.icon} size={17} />
            </span>
            <div>
              <span>{metric.label}</span>
              <strong>{metric.value}</strong>
              <small>{metric.detail}</small>
            </div>
            <span className={`settings-overview-dot ${metric.status ?? ''}`} aria-hidden="true" />
          </div>
        ))}
      </section>

      {mcpWarnings.length > 0 && (
        <section className="settings-overview-warning" aria-label="部署提醒">
          <span aria-hidden="true"><AppIcon name="alert" size={17} /></span>
          <div>
            <strong>部署配置需要检查</strong>
            <p>{mcpWarnings.join('；')}</p>
          </div>
        </section>
      )}

      <section className="settings-overview-section">
        <header>
          <h3>当前配置</h3>
          <p>模型、生成、检索和接入参数一览。</p>
        </header>
        <div className="settings-overview-config-list">
          {preferenceRows.map((row) => (
            <div className="settings-overview-config-row" key={row.title}>
              <span className="settings-overview-config-icon" aria-hidden="true">
                <AppIcon name={row.icon} size={16} />
              </span>
              <div className="settings-overview-config-copy">
                <strong>{row.title}</strong>
                <p>{row.description}</p>
              </div>
              <div className="settings-overview-config-value">
                <span className={row.status ? `settings-status-like ${row.status}` : undefined}>
                  {row.value}
                </span>
                {row.meta ? <small>{row.meta}</small> : null}
              </div>
            </div>
          ))}
        </div>
      </section>

      <AboutSettings embedded />
    </div>
  )
}

export default GeneralSettings
