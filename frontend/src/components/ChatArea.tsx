import { useEffect, useRef, useState } from 'react'
import type { ChangeEvent, KeyboardEvent } from 'react'
import type { AppConfig, Conversation, DocumentItem, KnowledgeBase } from '../App'
import MessageCard from './chat/MessageCard'
import MessageSkeleton from './chat/MessageSkeleton'
import AppIcon from './common/AppIcon'
import DocumentScopePicker from './knowledge/DocumentScopePicker'

interface ChatAreaProps {
  sidebarOpen: boolean
  activeConversation: Conversation
  knowledgeBases: KnowledgeBase[]
  selectedKnowledgeBase: KnowledgeBase | null
  selectedDocument: DocumentItem | null
  config: AppConfig
  isLoading: boolean
  isGlobalGenerating: boolean
  generatingConversationTitle: string
  enforceSingleFlight: boolean
  onSelectKnowledgeBase: (knowledgeBaseId: string | null) => void
  onSelectDocument: (documentId: string | null) => void
  onSendMessage: (content: string) => Promise<void>
  onClearConversation: () => void
}

const ALL_KNOWLEDGE_BASES_VALUE = '__all_knowledge_bases__'

const suggestedPrompts = [
  '请总结当前知识库的核心观点',
  '请列出这个知识库中最关键的结论',
  '如果基于当前资料开始实现，下一步建议是什么？',
]

const ChatArea = ({
  sidebarOpen,
  activeConversation,
  knowledgeBases,
  selectedKnowledgeBase,
  selectedDocument,
  config,
  isLoading,
  isGlobalGenerating,
  generatingConversationTitle,
  enforceSingleFlight,
  onSelectKnowledgeBase,
  onSelectDocument,
  onSendMessage,
  onClearConversation,
}: ChatAreaProps) => {
  const [inputValue, setInputValue] = useState('')
  const [copiedMessageId, setCopiedMessageId] = useState<string | null>(null)
  const messagesEndRef = useRef<HTMLDivElement | null>(null)
  const textareaRef = useRef<HTMLTextAreaElement | null>(null)

  const canSend = inputValue.trim().length > 0 && !(enforceSingleFlight && isGlobalGenerating)
  const hasMessages = activeConversation.messages.length > 0
  const scopeText = selectedDocument
    ? `文档问答：${selectedDocument.name}`
    : selectedKnowledgeBase
      ? `知识库问答：${selectedKnowledgeBase.name}`
      : knowledgeBases.length > 0
        ? '全部知识库'
        : '未选择知识库'

  // 输入框根据内容增长到六行，避免固定大输入区占用主要阅读空间。
  useEffect(() => {
    const textarea = textareaRef.current
    if (!textarea) return
    textarea.style.height = 'auto'
    textarea.style.height = `${Math.min(textarea.scrollHeight, 132)}px`
  }, [inputValue])

  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [activeConversation.messages, isLoading])

  const handleSubmit = async () => {
    const content = inputValue.trim()
    if (!content || isLoading) return
    setInputValue('')
    await onSendMessage(content)
  }

  const handleKeyDown = async (event: KeyboardEvent<HTMLTextAreaElement>) => {
    if (event.key === 'Enter' && !event.shiftKey) {
      event.preventDefault()
      await handleSubmit()
    }
  }

  const handleCopyMessage = async (messageId: string, content: string) => {
    try {
      await navigator.clipboard.writeText(content)
      setCopiedMessageId(messageId)
      window.setTimeout(() => {
        setCopiedMessageId((current) => (current === messageId ? null : current))
      }, 1500)
    } catch {
      // 复制失败不影响聊天主链路，浏览器权限错误由用户下一次操作重试。
    }
  }

  const handleKnowledgeBaseChange = (event: ChangeEvent<HTMLSelectElement>) => {
    const value = event.target.value
    onSelectKnowledgeBase(value === ALL_KNOWLEDGE_BASES_VALUE ? null : value)
  }

  return (
    <main className={`chat-area ${sidebarOpen ? 'sidebar-open' : 'sidebar-closed'}`}>
      <header className="chat-topbar">
        <div className="chat-topbar-main">
          <div className="chat-topbar-left">
            <span className="chat-topbar-title">{activeConversation.title}</span>
          </div>

          <div className="chat-context-summary" aria-label="当前问答范围">
            <AppIcon name="database" size={16} />
            <label className="chat-context-select-label">
              <span className="sr-only">选择知识库</span>
              <select
                className="chat-context-select"
                value={selectedKnowledgeBase?.id ?? ALL_KNOWLEDGE_BASES_VALUE}
                onChange={handleKnowledgeBaseChange}
                disabled={knowledgeBases.length === 0}
              >
                <option value={ALL_KNOWLEDGE_BASES_VALUE}>
                  {knowledgeBases.length > 0 ? '全部知识库' : '暂无知识库'}
                </option>
                {knowledgeBases.map((knowledgeBase) => (
                  <option key={knowledgeBase.id} value={knowledgeBase.id}>
                    {knowledgeBase.name}
                  </option>
                ))}
              </select>
            </label>
            <span className="chat-context-separator">/</span>
            <DocumentScopePicker
              documents={selectedKnowledgeBase?.documents ?? []}
              selectedDocumentId={selectedDocument?.id ?? null}
              onSelectDocument={onSelectDocument}
              disabled={!selectedKnowledgeBase}
            />
          </div>

          <div className="chat-topbar-right">
            {enforceSingleFlight && isGlobalGenerating && (
              <span className="chat-topbar-hint" aria-live="polite">
                生成中：{generatingConversationTitle}
              </span>
            )}
            <span className="chat-model-status" title={`当前模型：${config.chat.model}`}>
              <AppIcon name="zap" size={16} />
              <span>{config.chat.model}</span>
            </span>
            <button
              type="button"
              className="chat-topbar-action-btn chat-topbar-clear-btn"
              onClick={onClearConversation}
              disabled={isLoading}
              title="清空对话"
              aria-label="清空对话"
            >
              <AppIcon name="trash" size={17} />
            </button>
          </div>
        </div>
      </header>

      <div className="messages-container">
        {!hasMessages ? (
          <section className="welcome-message">
            <span className="welcome-mark">
              <AppIcon name="sparkles" size={22} />
            </span>
            <h2>开始本地对话</h2>
            <p>选择知识范围后直接提问，回答会保留在当前会话中。</p>
          </section>
        ) : (
          activeConversation.messages.map((message) => {
            const isStreamingPlaceholder =
              isLoading &&
              message.role === 'assistant' &&
              message.id === activeConversation.messages.at(-1)?.id &&
              !message.content.trim()

            return (
              <MessageCard
                key={message.id}
                message={message}
                isStreamingPlaceholder={isStreamingPlaceholder}
                copiedMessageId={copiedMessageId}
                onCopyMessage={handleCopyMessage}
              />
            )
          })
        )}

        {isLoading && activeConversation.messages.at(-1)?.role !== 'assistant' && (
          <MessageSkeleton />
        )}
        <div ref={messagesEndRef} />
      </div>

      {!hasMessages && (
        <div className="prompt-list">
          {suggestedPrompts.map((prompt) => (
            <button
              key={prompt}
              type="button"
              className="prompt-chip"
              disabled={enforceSingleFlight && isGlobalGenerating}
              onClick={() => void onSendMessage(prompt)}
            >
              {prompt}
            </button>
          ))}
        </div>
      )}

      <div className="input-area">
        <div className="input-context-row">
          <span className="input-context-icon">
            <AppIcon name={selectedDocument ? 'file' : 'database'} size={16} />
          </span>
          <span className="input-context-text">{scopeText}</span>
        </div>
        <div className="input-container">
          <textarea
            ref={textareaRef}
            value={inputValue}
            onChange={(event) => setInputValue(event.target.value)}
            onKeyDown={handleKeyDown}
            placeholder={
              enforceSingleFlight && isGlobalGenerating
                ? `当前正在后台生成「${generatingConversationTitle}」，请等待完成后再发送`
                : '输入您的问题，Enter 发送，Shift + Enter 换行'
            }
            rows={1}
          />
          <button
            type="button"
            onClick={() => void handleSubmit()}
            disabled={!canSend}
            className={`send-btn ${canSend ? 'send-btn-active' : ''}`}
            aria-label="发送消息"
          >
            {isLoading ? <span className="send-loading-dot" /> : <AppIcon name="send" size={18} />}
          </button>
        </div>
      </div>
    </main>
  )
}

export default ChatArea
