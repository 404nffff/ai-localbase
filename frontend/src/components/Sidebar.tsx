import { useEffect, useMemo, useState } from 'react'
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
import WikiProgressPage from '../pages/WikiProgressPage'
import WikiPanel from './knowledge/WikiPanel'
import WikiHistoryPage from '../pages/WikiHistoryPage'
import type { WikiChange } from '../services/api'

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

type WorkspaceId = 'chat' | 'knowledge' | 'settings' | 'progress' | 'wiki'

const workspaceItems = [
  { id: 'chat', label: '会话', description: '基于资料提问与对话', icon: 'message' },
  { id: 'knowledge', label: '知识库', description: '上传资料、检查与重建索引', icon: 'book' },
  { id: 'settings', label: '设置', description: '配置模型和检索参数', icon: 'settings' },
  { id: 'progress', label: '整理总进度', description: '查看待处理资料和失败任务', icon: 'clock' },
  { id: 'wiki', label: '知识文库', description: '批量整理、阅读与追溯来源', icon: 'file' },
] as const

/** 管理工作区入口与会话侧栏；展开时说明用途，Wiki 与历史入口可通过 URL 恢复。 */
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
  // URL 只记录工作区与文档身份，刷新可恢复历史入口，不保存资料正文或凭据。
  const [initialLocation] = useState(() => new URLSearchParams(window.location.hash.slice(1)))
  const [collapsedKnowledgeBases, setCollapsedKnowledgeBases] = useState<Record<string, boolean>>({})
  const [menuConversationId, setMenuConversationId] = useState<string | null>(null)
  const [editingConversationId, setEditingConversationId] = useState<string | null>(null)
  const [editingTitle, setEditingTitle] = useState('')
  const [isComposingTitle, setIsComposingTitle] = useState(false)
  const [conversationFilter, setConversationFilter] = useState('')
  const [progressLibraryId, setProgressLibraryId] = useState<string | null>(() => initialLocation.get('workspace') === 'progress' ? initialLocation.get('library') ?? '' : null)
  const [progressVisit, setProgressVisit] = useState(0)
  const [wikiOpen, setWikiOpen] = useState(() => initialLocation.get('workspace') === 'wiki')
  const [wikiHistoryOpen, setWikiHistoryOpen] = useState(() => initialLocation.get('view') === 'history')
  const [wikiTarget, setWikiTarget] = useState<Pick<WikiChange, 'knowledgeBaseId' | 'pageId' | 'revision'> | null>(() => {
    const knowledgeBaseId = initialLocation.get('library')
    const pageId = initialLocation.get('page')
    const revision = Number(initialLocation.get('revision'))
    return knowledgeBaseId && pageId && Number.isInteger(revision) && revision >= 0 ? { knowledgeBaseId, pageId, revision } : null
  })
  const wikiLibrary = knowledgeBases.find((kb) => kb.id === (wikiTarget?.knowledgeBaseId ?? selectedKnowledgeBaseId)) ?? knowledgeBases[0]

  useEffect(() => {
    const params = new URLSearchParams(window.location.hash.slice(1))
    for (const key of ['workspace', 'view', 'library', 'page', 'revision']) {
      params.delete(key)
    }
    if (wikiOpen) {
      params.set('workspace', 'wiki')
      if (wikiHistoryOpen) {
        params.set('view', 'history')
      }
      if (wikiTarget) {
        params.set('library', wikiTarget.knowledgeBaseId)
        params.set('page', wikiTarget.pageId)
        params.set('revision', String(wikiTarget.revision))
      }
    } else if (progressLibraryId !== null) {
      params.set('workspace', 'progress')
      params.set('library', progressLibraryId)
    }
    if (!wikiHistoryOpen) {
      params.delete('historyLibrary')
      params.delete('historyOffset')
    }
    const hash = params.toString()
    window.history.replaceState(null, '', `${window.location.pathname}${window.location.search}${hash ? '#' + hash : ''}`)
  }, [wikiOpen, wikiHistoryOpen, wikiTarget, progressLibraryId])

  let activeWorkspace: WorkspaceId = 'chat'
  if (wikiOpen) {
    activeWorkspace = 'wiki'
  } else if (progressLibraryId !== null) {
    activeWorkspace = 'progress'
  } else if (isSettingsOpen) {
    activeWorkspace = 'settings'
  } else if (isKnowledgePanelOpen) {
    activeWorkspace = 'knowledge'
  }
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

  /** 工作区互斥显示；整理总进度使用独立入口，保留知识库原有视图选择。 */
  const handleWorkspaceChange = (workspace: WorkspaceId) => {
    setWikiHistoryOpen(false)
    setWikiTarget(null)
    setWikiOpen(workspace === 'wiki')
    setProgressLibraryId(workspace === 'progress' ? '' : null)
    if (workspace === 'wiki') {
      if (isSettingsOpen) onToggleSettings()
      if (isKnowledgePanelOpen) onToggleKnowledgePanel()
      return
    }
    if (workspace === 'progress') {
      setProgressVisit((visit) => visit + 1)
      if (isSettingsOpen) onToggleSettings()
      if (isKnowledgePanelOpen) onToggleKnowledgePanel()
      return
    }
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
                <span>{item.label}<small>{item.description}</small></span>
              </button>
            ))}
          </nav>

          <div className="app-rail-footer">
            <ThemeToggle />
            <button
              type="button"
              className="app-rail-button app-rail-toggle"
              onClick={onToggle}
              aria-label={isOpen ? '收起侧边栏' : '展开侧边栏'}
              aria-expanded={isOpen}
              title={isOpen ? '收起侧边栏' : '展开侧边栏'}
            >
              <AppIcon name={isOpen ? 'panelClose' : 'panelOpen'} size={20} />
              <span>{isOpen ? '收起' : '展开'}</span>
            </button>
          </div>
        </div>

        <section className="conversation-sidebar" aria-hidden={!isOpen || activeWorkspace !== 'chat'}>
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

      {progressLibraryId !== null && <WikiProgressPage key={progressVisit} initialKnowledgeBaseId={progressLibraryId} onClose={() => handleWorkspaceChange('chat')} />}

      {/* Wiki 独占主工作区，切库重建阅读状态，上传仍复用知识库入口。 */}
      {wikiOpen && wikiHistoryOpen && <WikiHistoryPage knowledgeBases={knowledgeBases}
        onClose={() => setWikiHistoryOpen(false)}
        onOpenProgress={() => handleWorkspaceChange('progress')}
        onOpenPage={(change) => {
          onSelectKnowledgeBase(change.knowledgeBaseId)
          setWikiTarget(change)
          setWikiHistoryOpen(false)
        }}
      />}
      {wikiOpen && !wikiHistoryOpen && <section className="wiki-workspace-page app-workspace" aria-label="Wiki 工作区">
        {wikiLibrary ? <WikiPanel
          key={wikiLibrary.id}
          initialPageId={wikiTarget?.pageId}
          initialRevision={wikiTarget?.revision}
          onOpenHistory={() => setWikiHistoryOpen(true)}
          knowledgeBaseId={wikiLibrary.id}
          knowledgeBaseName={wikiLibrary.name}
          documents={wikiLibrary.documents}
          libraryPicker={<label>当前知识库<select aria-label="切换 Wiki 知识库" value={wikiLibrary.id} onChange={(event) => { setWikiTarget(null); onSelectKnowledgeBase(event.target.value) }}>{knowledgeBases.map((kb) => <option key={kb.id} value={kb.id}>{kb.name}</option>)}</select></label>}
          onUploadFiles={(files) => onUploadFiles(wikiLibrary.id, files)}
          uploadState={knowledgeBaseFileUploadStates[wikiLibrary.id]}
          onOpenProgress={() => { handleWorkspaceChange('progress'); setProgressLibraryId(wikiLibrary.id) }}
        /> : <div className="wiki-no-library"><AppIcon name="book" size={32} /><h2>还没有知识库</h2><p>先创建知识库并上传资料，再查看整理后的知识页。</p><button onClick={() => handleWorkspaceChange('knowledge')}>打开知识库</button></div>}
      </section>}

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
        onOpenWiki={() => handleWorkspaceChange('wiki')}
      />
    </>
  )
}

export default Sidebar
