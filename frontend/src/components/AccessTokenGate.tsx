import React, { useEffect, useState } from 'react'

export interface AccessTokenGateFeedback {
  kind: 'token' | 'network' | 'warming' | 'general'
  title: string
  message: string
}

interface AccessTokenGateProps {
  initialValue: string
  isSubmitting: boolean
  feedback: AccessTokenGateFeedback | null
  onSubmit: (value: string) => Promise<void>
}

const AccessTokenGate: React.FC<AccessTokenGateProps> = ({
  initialValue,
  isSubmitting,
  feedback,
  onSubmit,
}) => {
  const [draftToken, setDraftToken] = useState(initialValue)
  const [localFeedback, setLocalFeedback] = useState<AccessTokenGateFeedback | null>(null)

  useEffect(() => {
    setDraftToken(initialValue)
  }, [initialValue])

  const handleSubmit = async () => {
    const normalizedToken = draftToken.trim()
    if (!normalizedToken) {
      setLocalFeedback({
        kind: 'general',
        title: '需要访问令牌',
        message: '请输入访问令牌后再验证。',
      })
      return
    }

    setLocalFeedback(null)
    await onSubmit(normalizedToken)
  }

  const activeFeedback = localFeedback ?? feedback
  const feedbackBadgeMap: Record<AccessTokenGateFeedback['kind'], string> = {
    token: '令牌错误',
    network: '连接异常',
    warming: '服务启动中',
    general: '验证失败',
  }

  return (
    <div className="access-gate-backdrop">
      <div className="access-gate-card">
        <div className="access-gate-badge">访问验证</div>
        <h1>输入访问令牌后继续</h1>
        <p>
          当前后端已开启应用访问鉴权。请先填写有效的访问令牌并完成验证，验证通过后才会进入聊天界面。
        </p>

        <label className="access-gate-field">
          <span>访问令牌</span>
          <input
            type="password"
            value={draftToken}
            autoFocus
            placeholder="请输入访问令牌"
            onChange={(event) => {
              setDraftToken(event.target.value)
              if (localFeedback) {
                setLocalFeedback(null)
              }
            }}
            onKeyDown={(event) => {
              if (event.key === 'Enter' && !isSubmitting) {
                void handleSubmit()
              }
            }}
          />
        </label>

        <div className="access-gate-hint">
          令牌仅保存在当前浏览器本地，并会自动附带到后续 `/api` 与 `/v1` 请求中。
        </div>

        {activeFeedback && (
          <div className={`access-gate-error access-gate-error-${activeFeedback.kind}`}>
            <div className="access-gate-error-head">
              <span className="access-gate-error-icon" aria-hidden="true">
                {activeFeedback.kind === 'token'
                  ? '!'
                  : activeFeedback.kind === 'network'
                    ? '~'
                    : activeFeedback.kind === 'warming'
                      ? '...'
                      : 'x'}
              </span>
              <div className="access-gate-error-copy">
                <strong>{activeFeedback.title}</strong>
                <span>{feedbackBadgeMap[activeFeedback.kind]}</span>
              </div>
            </div>
            <p>{activeFeedback.message}</p>
          </div>
        )}

        <div className="access-gate-actions">
          <button
            type="button"
            className="access-gate-submit"
            onClick={() => {
              void handleSubmit()
            }}
            disabled={isSubmitting}
          >
            {isSubmitting ? '验证中...' : '验证并进入'}
          </button>
        </div>
      </div>
    </div>
  )
}

export default AccessTokenGate
