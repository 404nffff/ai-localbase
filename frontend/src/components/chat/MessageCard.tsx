import type { ChatMessage } from '../../App'
import AppIcon from '../common/AppIcon'
import MarkdownRenderer from './MarkdownRenderer'

interface MessageCardProps {
  message: ChatMessage
  isStreamingPlaceholder: boolean
  copiedMessageId: string | null
  onCopyMessage: (messageId: string, content: string) => Promise<void>
}

const formatTime = (value: string) =>
  new Date(value).toLocaleTimeString('zh-CN', {
    hour: '2-digit',
    minute: '2-digit',
  })

// 消息卡片只负责展示当前 ChatMessage，不引入 origin 额外的编辑、引用或导出契约。
const MessageCard = ({
  message,
  isStreamingPlaceholder,
  copiedMessageId,
  onCopyMessage,
}: MessageCardProps) => {
  const degradedMetadata =
    message.role === 'assistant' && message.metadata?.degraded
      ? message.metadata
      : null
  const hasContent = message.content.trim().length > 0

  if (!hasContent && !degradedMetadata && !isStreamingPlaceholder) {
    return null
  }

  return (
    <article className={`message ${message.role}`}>
      {!isStreamingPlaceholder && hasContent && (
        <div className="message-actions">
          <button
            type="button"
            className="message-action-btn"
            onClick={() => void onCopyMessage(message.id, message.content)}
            aria-label="复制消息"
            title={copiedMessageId === message.id ? '已复制' : '复制消息'}
          >
            <AppIcon name={copiedMessageId === message.id ? 'check' : 'copy'} />
          </button>
        </div>
      )}

      <div
        className={`message-content ${
          isStreamingPlaceholder ? 'message-content-thinking' : ''
        } ${message.role === 'assistant' ? 'message-content-markdown' : ''}`}
      >
        {degradedMetadata && (
          <div className="message-degraded-banner" role="status" aria-live="polite">
            <div className="message-degraded-title">
              <span className="message-degraded-title-icon">
                <AppIcon name="alert" />
              </span>
              <span>当前回答为降级回复，模型或检索链路出现异常</span>
            </div>
            {degradedMetadata.fallbackStrategy && (
              <div className="message-degraded-detail">
                策略：{degradedMetadata.fallbackStrategy}
              </div>
            )}
            {degradedMetadata.upstreamError && (
              <div className="message-degraded-subtle">
                上游错误：{degradedMetadata.upstreamError}
              </div>
            )}
          </div>
        )}

        {isStreamingPlaceholder ? (
          <div className="thinking-indicator" aria-label="AI 正在思考">
            <span className="thinking-dot" />
            <span className="thinking-dot" />
            <span className="thinking-dot" />
          </div>
        ) : message.role === 'assistant' ? (
          <MarkdownRenderer content={message.content} />
        ) : (
          message.content
        )}
      </div>

      <div className="message-time">{formatTime(message.timestamp)}</div>
    </article>
  )
}

export default MessageCard
