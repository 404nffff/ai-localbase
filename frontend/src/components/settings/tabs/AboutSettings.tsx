import React from 'react'
import { APP_VERSION_LABEL, IS_RELEASE_BUILD } from '../../../utils/appInfo'

const repositoryUrl = 'https://github.com/veyliss/ai-localbase'
const releaseUrl = `${repositoryUrl}/releases`
const deploymentUrl = `${repositoryUrl}/blob/main/DOCKER_DEPLOY.md`
const backupUrl = `${repositoryUrl}/blob/main/docs/backup-restore.md`

const AboutSettings: React.FC = () => {
  const buildStatus = IS_RELEASE_BUILD ? 'Release 构建' : '本地开发'

  return (
    <div className="settings-tab-content settings-about-page">
      <section className="settings-about-hero" aria-label="关于 AI LocalBase">
        <div className="settings-about-mark" aria-hidden="true">AI</div>
        <div className="settings-about-copy">
          <span>AI LocalBase</span>
          <h3>本地优先的 AI 知识库</h3>
          <p>面向自托管部署的 RAG 工作台，包含聊天、知识库、检索评估、MCP 和 OpenAI-compatible API。</p>
        </div>
        <div className="settings-about-version">
          <span>当前版本</span>
          <strong>{APP_VERSION_LABEL}</strong>
          <small>{buildStatus}</small>
        </div>
      </section>

      <section className="settings-setting-section">
        <div className="settings-setting-section-header">
          <div>
            <h3>版本信息</h3>
            <p>发布版由 Git tag 构建自动注入版本号，本地开发环境会显示为开发构建。</p>
          </div>
        </div>
        <div className="settings-about-facts">
          <div>
            <span>版本号</span>
            <strong>{APP_VERSION_LABEL}</strong>
          </div>
          <div>
            <span>构建状态</span>
            <strong>{buildStatus}</strong>
          </div>
          <div>
            <span>推荐部署</span>
            <strong>Docker</strong>
          </div>
        </div>
      </section>

      <section className="settings-setting-section">
        <div className="settings-setting-section-header">
          <div>
            <h3>项目资源</h3>
            <p>常用文档和发布入口。</p>
          </div>
        </div>
        <div className="settings-setting-list">
          <a className="settings-about-link-row" href={repositoryUrl} target="_blank" rel="noreferrer">
            <span>
              <strong>GitHub 仓库</strong>
              <small>源码、问题反馈与贡献入口</small>
            </span>
            <em>打开</em>
          </a>
          <a className="settings-about-link-row" href={releaseUrl} target="_blank" rel="noreferrer">
            <span>
              <strong>Release</strong>
              <small>查看版本记录和发布说明</small>
            </span>
            <em>打开</em>
          </a>
          <a className="settings-about-link-row" href={deploymentUrl} target="_blank" rel="noreferrer">
            <span>
              <strong>Docker 部署</strong>
              <small>镜像、端口、数据目录和部署建议</small>
            </span>
            <em>打开</em>
          </a>
          <a className="settings-about-link-row" href={backupUrl} target="_blank" rel="noreferrer">
            <span>
              <strong>备份与恢复</strong>
              <small>升级、迁移服务器前的运维清单</small>
            </span>
            <em>打开</em>
          </a>
        </div>
      </section>
    </div>
  )
}

export default AboutSettings
