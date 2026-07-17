import { useMemo, useState } from 'react'
import type {
  AppConfig,
  Conversation,
  DirectoryUploadTask,
  DocumentDetailResponse,
  DocumentItem,
  KnowledgeBase,
  KnowledgeBaseFileUploadState,
  KnowledgeBaseHealthResponse,
  OperationLogFilters,
  OperationLogListResponse,
} from '../App'
import AppIcon from './common/AppIcon'
import ThemeToggle from './common/ThemeToggle'
import KnowledgePanel from './knowledge/KnowledgePanel'
import SettingsPanel from './settings/SettingsPanel'

interface SidebarProps {
  isOpen: boolean
  onToggle: () => void
  knowledgeBases: KnowledgeBase[]
  selectedKnowledgeBaseId: string | null
  selectedDocumentId: string | null
  activeKnowledgeBaseId: string | null
  activeDocumentId: string | null
  onSelectKnowledgeBase: (knowledgeBaseId: string) => void
  onSelectDocument: (knowledgeBaseId: string, documentId: string | null) => void
  onCreateKnowledgeBase: (name: string, description: string) => void
  onDeleteKnowledgeBase: (knowledgeBaseId: string) => void
  onExportKnowledgeBase: (knowledgeBaseId: string) => void
  onUploadFiles: (knowledgeBaseId: string, files: FileList | null) => void
  onUploadDirectory: (knowledgeBaseId: string, files: FileList | null) => void
  directoryUploadTask: DirectoryUploadTask
  knowledgeBaseFileUploadStates: Record<string, KnowledgeBaseFileUploadState>
  exportingKnowledgeBaseId: string | null
  onCancelDirectoryUpload: () => void
  onContinueDirectoryUpload: () => void
  onRemoveDocument: (knowledgeBaseId: string, documentId: string) => void
  onFetchKnowledgeBaseHealth: (knowledgeBaseId: string) => Promise<KnowledgeBaseHealthResponse>
  onFetchDocumentDetail: (
    knowledgeBaseId: string,
    documentId: string,
  ) => Promise<DocumentDetailResponse>
  onReindexDocument: (knowledgeBaseId: string, documentId: string) => Promise<DocumentItem>
  conversations: Conversation[]
  activeConversationId: string | null
  onSelectConversation: (conversationId: string) => void
  onCreateConversation: () => void
  onRenameConversation: (conversationId: string, title: string) => void
  onDeleteConversation: (conversationId: string) => void
  config: AppConfig
  isSettingsOpen: boolean
  isKnowledgePanelOpen: boolean
  onToggleSettings: () => void
  onToggleKnowledgePanel: () => void
  onSaveConfig: (value: AppConfig) => Promise<AppConfig>
  operationLogs: OperationLogListResponse
  operationLogFilters: OperationLogFilters
  isOperationLogLoading: boolean
  operationLogError: string | null
  onOperationLogFiltersChange: (filters: OperationLogFilters) => void
  onRefreshOperationLogs: () => void
}

const formatDateTime = (value: string) =>
  new Date(value).toLocaleString('zh-CN', {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  })

type WorkspaceId = 'chat' | 'knowledge' | 'settings'

const workspaceItems = [
  { id: 'chat', label: '会话', icon: 'message' },
  { id: 'knowledge', label: '知识库', icon: 'book' },
  { id: 'settings', label: '设置', icon: 'settings' },
] as const

const Sidebar = ({
  isOpen,
  onToggle,
  knowledgeBases,
  selectedKnowledgeBaseId,
  activeKnowledgeBaseId,
  activeDocumentId,
  onSelectKnowledgeBase,
  onSelectDocument,
  onCreateKnowledgeBase,
  onDeleteKnowledgeBase,
  onExportKnowledgeBase,
  onUploadFiles,
  onUploadDirectory,
  directoryUploadTask,
  knowledgeBaseFileUploadStates,
  exportingKnowledgeBaseId,
  onCancelDirectoryUpload,
  onContinueDirectoryUpload,
  onRemoveDocument,
  onFetchKnowledgeBaseHealth,
  onFetchDocumentDetail,
  onReindexDocument,
  conversations,
  activeConversationId,
  onSelectConversation,
  onCreateConversation,
  onRenameConversation,
  onDeleteConversation,
  config,
  isSettingsOpen,
  isKnowledgePanelOpen,
  onToggleSettings,
  onToggleKnowledgePanel,
  onSaveConfig,
  operationLogs,
  operationLogFilters,
  isOperationLogLoading,
  operationLogError,
  onOperationLogFiltersChange,
  onRefreshOperationLogs,
}: SidebarProps) => {
  const [collapsedKnowledgeBases, setCollapsedKnowledgeBases] = useState<Record<string, boolean>>({})
  const [menuConversationId, setMenuConversationId] = useState<string | null>(null)
  const [editingConversationId, setEditingConversationId] = useState<string | null>(null)
  const [editingTitle, setEditingTitle] = useState('')
  const [isComposingTitle, setIsComposingTitle] = useState(false)
  const [conversationFilter, setConversationFilter] = useState('')

  const activeWorkspace: WorkspaceId = isSettingsOpen
    ? 'settings'
    : isKnowledgePanelOpen
      ? 'knowledge'
      : 'chat'
  const sortedKnowledgeBases = useMemo(() => knowledgeBases, [knowledgeBases])
  const filteredConversations = useMemo(() => {
    const query = conversationFilter.trim().toLowerCase()
    if (!query) return conversations
    return conversations.filter((conversation) => conversation.title.toLowerCase().includes(query))
  }, [conversationFilter, conversations])

  const toggleKnowledgeBaseCollapse = (knowledgeBaseId: string) => {
    setCollapsedKnowledgeBases((current) => ({
      ...current,
      [knowledgeBaseId]: !current[knowledgeBaseId],
    }))
  }

  // 当前 App 仍分别维护两个弹窗开关，这里统一成工作区切换，避免知识库和设置同时打开。
  const handleWorkspaceChange = (workspace: WorkspaceId) => {
    if (workspace === 'chat') {
      if (isSettingsOpen) onToggleSettings()
      if (isKnowledgePanelOpen) onToggleKnowledgePanel()
      return
    }

    if (workspace === 'knowledge') {
      if (isSettingsOpen) onToggleSettings()
      if (!isKnowledgePanelOpen) onToggleKnowledgePanel()
      return
    }

    if (isKnowledgePanelOpen) onToggleKnowledgePanel()
    if (!isSettingsOpen) onToggleSettings()
  }

  const finishRename = (conversation: Conversation) => {
    if (isComposingTitle) return
    const nextTitle = editingTitle.trim()
    setEditingConversationId(null)
    if (nextTitle && nextTitle !== conversation.title.trim()) {
      onRenameConversation(conversation.id, nextTitle)
    }
  }

  return (
    <>
      <aside
        className={`app-navigation ${isOpen ? 'context-open' : 'context-closed'} workspace-${activeWorkspace}`}
        aria-label="应用导航"
      >
        <div className="app-rail">
          <div className="app-brand-mark" title="AI LocalBase">
            <AppIcon name="database" size={20} />
            <span className="sr-only">AI LocalBase</span>
          </div>

          <nav className="app-rail-nav" aria-label="工作区">
            {workspaceItems.map((item) => (
              <button
                key={item.id}
                type="button"
                className={`app-rail-button ${activeWorkspace === item.id ? 'active' : ''}`}
                aria-current={activeWorkspace === item.id ? 'page' : undefined}
                aria-label={item.label}
                title={item.label}
                onClick={() => handleWorkspaceChange(item.id)}
              >
                <AppIcon name={item.icon} size={20} />
                <span>{item.label}</span>
              </button>
            ))}
          </nav>

          <div className="app-rail-footer">
            <ThemeToggle />
            <button
              type="button"
              className="app-rail-button app-rail-toggle"
              onClick={onToggle}
              aria-label={isOpen ? '收起会话栏' : '展开会话栏'}
              title={isOpen ? '收起会话栏' : '展开会话栏'}
            >
              <AppIcon name={isOpen ? 'panelClose' : 'panelOpen'} size={20} />
              <span>{isOpen ? '收起' : '展开'}</span>
            </button>
          </div>
        </div>

        <section className="conversation-sidebar" aria-hidden={!isOpen}>
          <header className="conversation-sidebar-header">
            <div>
              <span className="conversation-sidebar-kicker">AI LocalBase</span>
              <h1>会话</h1>
            </div>
            <button
              type="button"
              className="conversation-create-button"
              onClick={onCreateConversation}
              aria-label="新建会话"
              title="新建会话"
            >
              <AppIcon name="plus" size={18} />
            </button>
          </header>

          <label className="conversation-search">
            <AppIcon name="search" size={16} />
            <span className="sr-only">搜索会话</span>
            <input
              type="search"
              placeholder="搜索会话"
              value={conversationFilter}
              onChange={(event) => setConversationFilter(event.target.value)}
            />
          </label>

          <div className="conversation-list" aria-label="会话列表">
            {filteredConversations.length === 0 && (
              <p className="conversation-empty">没有匹配的会话</p>
            )}

            {filteredConversations.map((conversation) => {
              const isMenuOpen = menuConversationId === conversation.id
              const isEditing = editingConversationId === conversation.id
              const isActive = activeConversationId === conversation.id

              return (
                <div
                  key={conversation.id}
                  className={`conversation-item-row ${isMenuOpen ? 'menu-open' : ''}`}
                >
                  {isEditing ? (
                    <div className={`conversation-item conversation-item-editing ${isActive ? 'active' : ''}`}>
                      <input
                        className="conversation-title-input"
                        type="text"
                        value={editingTitle}
                        autoFocus
                        onFocus={(event) => event.currentTarget.select()}
                        onClick={(event) => event.stopPropagation()}
                        onChange={(event) => setEditingTitle(event.currentTarget.value)}
                        onCompositionStart={() => setIsComposingTitle(true)}
                        onCompositionEnd={(event) => {
                          setIsComposingTitle(false)
                          setEditingTitle(event.currentTarget.value)
                        }}
                        onKeyDown={(event) => {
                          event.stopPropagation()
                          if (isComposingTitle || event.nativeEvent.isComposing) return
                          if (event.key === 'Enter') finishRename(conversation)
                          if (event.key === 'Escape') {
                            setEditingConversationId(null)
                            setEditingTitle(conversation.title)
                          }
                        }}
                        onBlur={() => finishRename(conversation)}
                      />
                      <span className="conversation-meta">
                        {conversation.messages.length} 条消息 · {formatDateTime(conversation.updatedAt)}
                      </span>
                    </div>
                  ) : (
                    <button
                      type="button"
                      className={`conversation-item ${isActive ? 'active' : ''}`}
                      onClick={() => {
                        setMenuConversationId(null)
                        setEditingConversationId(null)
                        onSelectConversation(conversation.id)
                        if (window.innerWidth <= 768 && isOpen) onToggle()
                      }}
                    >
                      <span className="conversation-title">{conversation.title}</span>
                      <span className="conversation-meta">
                        {conversation.messages.length} 条消息 · {formatDateTime(conversation.updatedAt)}
                      </span>
                    </button>
                  )}

                  <div className="conversation-item-actions">
                    <button
                      type="button"
                      className="conversation-menu-trigger"
                      aria-label="打开会话菜单"
                      title="会话操作"
                      onClick={(event) => {
                        event.stopPropagation()
                        setEditingConversationId(null)
                        setMenuConversationId((current) =>
                          current === conversation.id ? null : conversation.id,
                        )
                      }}
                    >
                      <AppIcon name="more" size={17} />
                    </button>

                    {isMenuOpen && (
                      <div className="conversation-menu" onClick={(event) => event.stopPropagation()}>
                        <button
                          type="button"
                          className="conversation-menu-item"
                          onMouseDown={(event) => event.preventDefault()}
                          onClick={() => {
                            setMenuConversationId(null)
                            setEditingConversationId(conversation.id)
                            setEditingTitle(conversation.title)
                            setIsComposingTitle(false)
                          }}
                        >
                          <AppIcon name="pencil" size={15} />
                          重命名
                        </button>
                        <button
                          type="button"
                          className="conversation-menu-item danger"
                          onMouseDown={(event) => event.preventDefault()}
                          onClick={() => {
                            const confirmed = window.confirm(`确定删除会话“${conversation.title}”吗？`)
                            setMenuConversationId(null)
                            setEditingConversationId(null)
                            if (confirmed) onDeleteConversation(conversation.id)
                          }}
                        >
                          <AppIcon name="trash" size={15} />
                          删除
                        </button>
                      </div>
                    )}
                  </div>
                </div>
              )
            })}
          </div>
        </section>
      </aside>

      {isSettingsOpen && (
        <SettingsPanel config={config} onClose={onToggleSettings} onSaveConfig={onSaveConfig} />
      )}

      <KnowledgePanel
        open={isKnowledgePanelOpen}
        knowledgeBases={sortedKnowledgeBases}
        collapsedKnowledgeBases={collapsedKnowledgeBases}
        onToggleCollapse={toggleKnowledgeBaseCollapse}
        selectedKnowledgeBaseId={selectedKnowledgeBaseId}
        activeKnowledgeBaseId={activeKnowledgeBaseId}
        activeDocumentId={activeDocumentId}
        onSelectKnowledgeBase={onSelectKnowledgeBase}
        onSelectDocument={onSelectDocument}
        onCreateKnowledgeBase={onCreateKnowledgeBase}
        onDeleteKnowledgeBase={onDeleteKnowledgeBase}
        onExportKnowledgeBase={onExportKnowledgeBase}
        onUploadFiles={onUploadFiles}
        onUploadDirectory={onUploadDirectory}
        directoryUploadTask={directoryUploadTask}
        knowledgeBaseFileUploadStates={knowledgeBaseFileUploadStates}
        exportingKnowledgeBaseId={exportingKnowledgeBaseId}
        onCancelDirectoryUpload={onCancelDirectoryUpload}
        onContinueDirectoryUpload={onContinueDirectoryUpload}
        onRemoveDocument={onRemoveDocument}
        onFetchKnowledgeBaseHealth={onFetchKnowledgeBaseHealth}
        onFetchDocumentDetail={onFetchDocumentDetail}
        onReindexDocument={onReindexDocument}
        operationLogs={operationLogs}
        operationLogFilters={operationLogFilters}
        isOperationLogLoading={isOperationLogLoading}
        operationLogError={operationLogError}
        onOperationLogFiltersChange={onOperationLogFiltersChange}
        onRefreshOperationLogs={onRefreshOperationLogs}
        onClose={onToggleKnowledgePanel}
      />
    </>
  )
}

export default Sidebar
