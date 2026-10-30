import type { ChatMessage } from '../../App'
import { useState } from 'react'
import { WikiPageViewer } from '../knowledge/WikiPanel'
import AppIcon from '../common/AppIcon'
import MarkdownRenderer from './LazyMarkdownRenderer'
import MessageCitations from './MessageCitations'

interface MessageCardProps {
  message: ChatMessage
  isStreamingPlaceholder: boolean
  copiedMessageId: string | null
  onCopyMessage: (messageId: string, content: string) => Promise<void>
  onEditMessage?: (messageId: string, content: string) => Promise<void>
  onDeleteMessage?: (messageId: string) => Promise<void>
  onRegenerateMessage?: (messageId: string) => Promise<void>
  /** 提交保存回答的沉淀任务，返回可显示反馈；未提供时隐藏动作。 */
  onCaptureMessage?: (messageId: string) => Promise<string>
}

const formatTime = (value: string) =>
  new Date(value).toLocaleTimeString('zh-CN', {
    hour: '2-digit',
    minute: '2-digit',
  })

/** 消息卡片按库、知识页和修订合并跳转入口，完整引文仍由知识页阅读器展示。 */
const MessageCard = ({
  message,
  isStreamingPlaceholder,
  copiedMessageId,
  onCopyMessage,
  onEditMessage,
  onDeleteMessage,
  onRegenerateMessage,
  onCaptureMessage,
}: MessageCardProps) => {
  const [capturing, setCapturing] = useState(false)
  const [captureFeedback, setCaptureFeedback] = useState('')
  const [captureError, setCaptureError] = useState(false)
  const [wikiSource, setWikiSource] = useState<{ knowledgeBaseId: string; pageId: string; revision: number } | null>(null)
  const [showAllSources, setShowAllSources] = useState(false)
  /** 沉淀动作等待服务端接受任务，失败不显示成功反馈。 */
  async function capture() {
    if (!onCaptureMessage) return
    setCapturing(true)
    setCaptureFeedback('')
    setCaptureError(false)
    try {
      setCaptureFeedback(await onCaptureMessage(message.id))
    } catch (reason) {
      setCaptureError(true)
      setCaptureFeedback(reason instanceof Error ? reason.message : '沉淀失败')
    } finally {
      setCapturing(false)
    }
  }
  const degradedMetadata =
    message.role === 'assistant' && message.metadata?.degraded
      ? message.metadata
      : null
  const sources = message.role === 'assistant' ? message.metadata?.sources ?? [] : []
  // 原始引文也带 pageId；不能将每条引文渲染成同一个知识页按钮。
  const wikiSources = new Map<string, { knowledgeBaseId: string; pageId: string; revision: number; title: string }>()
  for (const source of sources) {
    if (!source.pageId || !source.knowledgeBaseId) continue
    const revision = Number(source.wikiRevision) || 0
    const key = JSON.stringify([source.knowledgeBaseId, source.pageId, revision])
    const existing = wikiSources.get(key)
    const pageTitle = source.documentId === source.pageId ? source.documentName : ''
    wikiSources.set(key, {
      knowledgeBaseId: source.knowledgeBaseId,
      pageId: source.pageId,
      revision,
      title: pageTitle || existing?.title || `知识页 ${source.pageId}`,
    })
  }
  const visibleWikiSources = [...wikiSources.entries()].slice(0, showAllSources ? undefined : 3)
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
          {message.role === 'user' && onEditMessage && (
            <button
              type="button"
              className="message-action-btn"
              onClick={() => {
                const nextContent = window.prompt('编辑消息', message.content)
                if (nextContent && nextContent.trim() && nextContent !== message.content) {
                  void onEditMessage(message.id, nextContent.trim())
                }
              }}
              aria-label="编辑消息"
              title="编辑消息"
            >
              <AppIcon name="pencil" />
            </button>
          )}
          {message.role === 'assistant' && onRegenerateMessage && (
            <button
              type="button"
              className="message-action-btn"
              onClick={() => void onRegenerateMessage(message.id)}
              aria-label="重新生成"
              title="重新生成"
            >
              <AppIcon name="refresh" />
            </button>
          )}
          {message.role === 'assistant' && !degradedMetadata && onCaptureMessage && <button type="button" className="message-action-btn" disabled={capturing} onClick={() => void capture()} aria-label="沉淀为知识页" title="沉淀为知识页">{capturing ? '提交中…' : '沉淀'}</button>}
          {onDeleteMessage && (
            <button
              type="button"
              className="message-action-btn"
              onClick={() => void onDeleteMessage(message.id)}
              aria-label="删除消息"
              title="删除此条及之后的消息"
            >
              <AppIcon name="trash" />
            </button>
          )}
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
          <>
            <MarkdownRenderer content={message.content} />
            <MessageCitations metadata={message.metadata} />
            {wikiSources.size > 0 && (
              <section className="message-wiki-sources" aria-label="知识页来源">
                <div className="message-wiki-sources-heading"><AppIcon name="book" size={14} /><span>知识页来源 · {wikiSources.size}</span></div>
                <div className="message-wiki-source-list">
                  {visibleWikiSources.map(([key, source]) => (
                    <button type="button" key={key} onClick={() => setWikiSource(source)}>
                      <span>{source.title}</span><small>修订 {source.revision || '当前'} · 查看原文依据</small><AppIcon name="chevronRight" size={14} />
                    </button>
                  ))}
                </div>
                {wikiSources.size > 3 && <button type="button" className="message-wiki-sources-toggle" aria-expanded={showAllSources} onClick={() => setShowAllSources(!showAllSources)}>{showAllSources ? '收起来源' : `展开其余 ${wikiSources.size - 3} 个来源`}</button>}
              </section>
            )}
          </>
        ) : (
          message.content
        )}
      </div>

      {captureFeedback && <p className="wiki-capture-feedback" role={captureError ? 'alert' : 'status'}>{captureFeedback}</p>}
      {wikiSource && <WikiPageViewer key={`${wikiSource.pageId}-${wikiSource.revision}`} knowledgeBaseId={wikiSource.knowledgeBaseId} pageId={wikiSource.pageId} initialRevision={wikiSource.revision} onClose={() => setWikiSource(null)} />}

      <div className="message-time">{formatTime(message.timestamp)}</div>
    </article>
  )
}

export default MessageCard
