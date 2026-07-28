import React from 'react'
import type { AppConfig } from '../../../App'
import AppIcon, { type AppIconName } from '../../common/AppIcon'
import AboutSettings from './AboutSettings'

interface GeneralSettingsProps {
  config: AppConfig
}

interface OverviewMetric {
  label: string
  value: string
  detail: string
  icon: AppIconName
  status?: 'enabled' | 'disabled'
}

const GeneralSettings: React.FC<GeneralSettingsProps> = ({ config }) => {
  const chatProviderLabel = config.chat.provider === 'ollama' ? 'Ollama' : 'OpenAI Compatible'
  const searchModeLabel = config.retrieval.defaultSearchMode === 'hybrid' ? '混合检索' : '向量检索'
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

      <AboutSettings embedded />
    </div>
  )
}

export default GeneralSettings
