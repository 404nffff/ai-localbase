import React from 'react'
import { APP_VERSION_LABEL, IS_RELEASE_BUILD } from '../../../utils/appInfo'
import AppIcon from '../../common/AppIcon'

const repositoryUrl = 'https://github.com/veyliss/ai-localbase'
const releaseUrl = `${repositoryUrl}/releases`
const licenseUrl = `${repositoryUrl}/blob/main/LICENSE`

interface AboutSettingsProps {
  embedded?: boolean
}

const AboutSettings: React.FC<AboutSettingsProps> = ({ embedded = false }) => {
  const buildStatus = IS_RELEASE_BUILD ? 'Release 构建' : '本地开发'
  const projectName = 'AI LocalBase'

  return (
    <div className={embedded ? 'settings-about-page settings-about-embedded' : 'settings-tab-content settings-about-page'}>
      <details className="settings-about-disclosure">
        <summary>
          <span>
            <strong>{projectName}</strong>
            <small>{APP_VERSION_LABEL} · {buildStatus}</small>
          </span>
          <AppIcon name="chevronDown" size={16} />
        </summary>
        <div className="settings-about-links">
          <a href={repositoryUrl} target="_blank" rel="noreferrer">GitHub</a>
          <a href={releaseUrl} target="_blank" rel="noreferrer">发布记录</a>
          <a href={licenseUrl} target="_blank" rel="noreferrer">许可证</a>
        </div>
      </details>
    </div>
  )
}

export default AboutSettings
