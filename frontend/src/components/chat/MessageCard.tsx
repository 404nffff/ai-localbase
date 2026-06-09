import React, { useState } from 'react'
import type { ChatMessage, ChatSourceMetadata } from '../../App'
import MarkdownRenderer from './MarkdownRenderer'
import MessageCitations from './MessageCitations'

interface MessageCardProps {
  message: ChatMessage
  isLoading: boolean
  isStreamingPlaceholder: boolean
  onCopyMessage: (messageId: string, content: string) => Promise<void>
  onEditMessage?: (messageId: string, newContent: string) => Promise<void>
  onDeleteMessage?: (messageId: string) => Promise<void>
  onRegenerateMessage?: (messageId: string) => Promise<void>
  onOpenCitationSource?: (source: ChatSourceMetadata) => void
  copiedMessageId: string | null
}

const formatTime = (value: string) =>
  new Date(value).toLocaleTimeString('zh-CN', {
    hour: '2-digit',
    minute: '2-digit',
  })

const MessageCard: React.FC<MessageCardProps> = ({
  message,
  isLoading,
  isStreamingPlaceholder,
  onCopyMessage,
  onEditMessage,
  onDeleteMessage,
  onRegenerateMessage,
  onOpenCitationSource,
  copiedMessageId,
}) => {
  const [isEditing, setIsEditing] = useState(false)
  const [editedContent, setEditedContent] = useState(message.content)
  const [isRegenerating, setIsRegenerating] = useState(false)

  const degradedMetadata =
    message.role === 'assistant' && message.metadata?.degraded
      ? message.metadata
      : null

  const handleSaveEdit = async () => {
    const trimmedContent = editedContent.trim()
    if (!trimmedContent || trimmedContent === message.content) {
      setIsEditing(false)
      setEditedContent(message.content)
      return
    }

    if (onEditMessage) {
      await onEditMessage(message.id, trimmedContent)
    }
    setIsEditing(false)
  }

  const handleCancelEdit = () => {
    setIsEditing(false)
    setEditedContent(message.content)
  }

  const handleRegenerateClick = async () => {
    if (!onRegenerateMessage || isRegenerating) return
    setIsRegenerating(true)
    try {
      await onRegenerateMessage(message.id)
    } finally {
      setIsRegenerating(false)
    }
  }

  const handleDeleteClick = async () => {
    if (!onDeleteMessage) return
    const confirmed = window.confirm('确定要删除这条消息吗？')
    if (confirmed) {
      await onDeleteMessage(message.id)
    }
  }

  return (
    <div className={`message ${message.role}`}>
      {!isStreamingPlaceholder && message.content.trim() && !isEditing && (
        <div className="message-actions">
          <button
            type="button"
            className="message-action-btn"
            onClick={() => {
              void onCopyMessage(message.id, message.content)
            }}
            aria-label="复制消息"
            title={copiedMessageId === message.id ? '已复制' : '复制消息'}
          >
            {copiedMessageId === message.id ? '✓' : '⧉'}
          </button>
          {message.role === 'user' && onEditMessage && (
            <button
              type="button"
              className="message-action-btn"
              onClick={() => {
                setIsEditing(true)
                setEditedContent(message.content)
              }}
              aria-label="编辑消息"
              title="编辑消息"
            >
              ✎
            </button>
          )}
          {message.role === 'assistant' && onRegenerateMessage && !isLoading && (
            <button
              type="button"
              className="message-action-btn"
              onClick={() => {
                void handleRegenerateClick()
              }}
              aria-label="重新生成"
              title="重新生成"
              disabled={isRegenerating}
            >
              {isRegenerating ? '⟳' : '↻'}
            </button>
          )}
          {onDeleteMessage && (
            <button
              type="button"
              className="message-action-btn message-action-delete"
              onClick={() => {
                void handleDeleteClick()
              }}
              aria-label="删除消息"
              title="删除消息"
            >
              ✕
            </button>
          )}
        </div>
      )}

      {isEditing ? (
        <div className="message-edit-container">
          <textarea
            className="message-edit-textarea"
            value={editedContent}
            onChange={(e) => setEditedContent(e.target.value)}
            rows={5}
            autoFocus
          />
          <div className="message-edit-actions">
            <button
              type="button"
              className="message-edit-btn message-edit-save"
              onClick={() => {
                void handleSaveEdit()
              }}
            >
              保存
            </button>
            <button
              type="button"
              className="message-edit-btn message-edit-cancel"
              onClick={handleCancelEdit}
            >
              取消
            </button>
          </div>
        </div>
      ) : (
        <div
          className={`message-content ${
            isStreamingPlaceholder ? 'message-content-thinking' : ''
          } ${message.role === 'assistant' ? 'message-content-markdown' : ''}`}
        >
          {degradedMetadata && (
            <div className="message-degraded-banner" role="status" aria-live="polite">
              <div className="message-degraded-title">
                ⚠ 当前回答为降级回复，模型或检索链路出现异常
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
      )}

      {message.role === 'assistant' && message.metadata?.sources && !isEditing && (
        <MessageCitations
          sources={message.metadata.sources}
          onOpenCitationSource={onOpenCitationSource}
        />
      )}

      {!isEditing && <div className="message-time">{formatTime(message.timestamp)}</div>}
    </div>
  )
}

export default MessageCard
