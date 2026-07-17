import React, { ChangeEvent, useDeferredValue, useEffect, useMemo, useRef, useState } from 'react'
import {
  DirectoryUploadTask,
  DocumentDetailResponse,
  DocumentItem,
  KnowledgeBase,
  KnowledgeBaseFileUploadState,
  KnowledgeBaseHealthResponse,
  OperationLogEntry,
  OperationLogFilters,
  OperationLogListResponse,
} from '../../App'
import DocumentDetailDialog from './DocumentDetailDialog'
import { getDocumentPage } from './documentListPagination'
import { formatDocumentPreviewText } from './documentPreviewText'

interface KnowledgePanelProps {
  open: boolean
  knowledgeBases: KnowledgeBase[]
  collapsedKnowledgeBases: Record<string, boolean>
  onToggleCollapse: (knowledgeBaseId: string) => void
  selectedKnowledgeBaseId: string | null
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
  operationLogs: OperationLogListResponse
  operationLogFilters: OperationLogFilters
  isOperationLogLoading: boolean
  operationLogError: string | null
  onOperationLogFiltersChange: (filters: OperationLogFilters) => void
  onRefreshOperationLogs: () => void
  onClose: () => void
}

type KnowledgePanelView = 'knowledge' | 'logs'
type DocumentSort = 'uploadedAt:desc' | 'uploadedAt:asc' | 'name:asc' | 'name:desc' | 'size:desc' | 'size:asc'

const KnowledgePanel: React.FC<KnowledgePanelProps> = ({
  open,
  knowledgeBases,
  collapsedKnowledgeBases,
  onToggleCollapse,
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
  operationLogs,
  operationLogFilters,
  isOperationLogLoading,
  operationLogError,
  onOperationLogFiltersChange,
  onRefreshOperationLogs,
  onClose,
}) => {
  const [activeView, setActiveView] = useState<KnowledgePanelView>('knowledge')
  const [showCreateModal, setShowCreateModal] = useState(false)
  const [newName, setNewName] = useState('')
  const [newDescription, setNewDescription] = useState('')
  const [knowledgeBaseQuery, setKnowledgeBaseQuery] = useState('')
  const [documentQuery, setDocumentQuery] = useState('')
  const deferredDocumentQuery = useDeferredValue(documentQuery)
  const [documentSort, setDocumentSort] = useState<DocumentSort>('uploadedAt:desc')
  const [documentPage, setDocumentPage] = useState(1)
  const [knowledgeHealth, setKnowledgeHealth] = useState<KnowledgeBaseHealthResponse | null>(null)
  const [knowledgeHealthError, setKnowledgeHealthError] = useState<string | null>(null)
  const [isKnowledgeHealthLoading, setIsKnowledgeHealthLoading] = useState(false)
  const [documentDetail, setDocumentDetail] = useState<DocumentDetailResponse | null>(null)
  const [documentDetailError, setDocumentDetailError] = useState<string | null>(null)
  const [documentDetailLoadingId, setDocumentDetailLoadingId] = useState<string | null>(null)
  const [reindexingDocumentId, setReindexingDocumentId] = useState<string | null>(null)
  const [deleteConfirmId, setDeleteConfirmId] = useState<string | null>(null)
  const [showUploadTaskDetails, setShowUploadTaskDetails] = useState(false)
  const [showFailedItems, setShowFailedItems] = useState(false)
  const [showSkippedItems, setShowSkippedItems] = useState(false)
  const directoryInputRefs = useRef<Record<string, HTMLInputElement | null>>({})
  const fetchKnowledgeHealthRef = useRef(onFetchKnowledgeBaseHealth)

  const handleFileChange = (knowledgeBaseId: string, event: ChangeEvent<HTMLInputElement>) => {
    onUploadFiles(knowledgeBaseId, event.target.files)
    event.target.value = ''
  }

  const handleDirectoryChange = (knowledgeBaseId: string, event: ChangeEvent<HTMLInputElement>) => {
    onUploadDirectory(knowledgeBaseId, event.target.files)
    event.target.value = ''
  }

  const registerDirectoryInput = (knowledgeBaseId: string, element: HTMLInputElement | null) => {
    directoryInputRefs.current[knowledgeBaseId] = element
    if (element) {
      element.setAttribute('webkitdirectory', '')
      element.setAttribute('directory', '')
    }
  }

  const handleOpenCreate = () => {
    setNewName('')
    setNewDescription('')
    setShowCreateModal(true)
  }

  const handleConfirmCreate = () => {
    const trimmedName = newName.trim()
    if (!trimmedName) return
    onCreateKnowledgeBase(trimmedName, newDescription.trim())
    setShowCreateModal(false)
    setNewName('')
    setNewDescription('')
  }

  const handleCancelCreate = () => {
    setShowCreateModal(false)
    setNewName('')
    setNewDescription('')
  }

  const statusLabel = (status: string) => {
    if (status === 'indexed') return { text: '已索引', color: '#16a34a', bg: '#dcfce7' }
    if (status === 'processing') return { text: '处理中', color: '#d97706', bg: '#fef3c7' }
    if (status === 'failed') return { text: '失败', color: '#b91c1c', bg: '#fee2e2' }
    return { text: '就绪', color: '#2563eb', bg: '#dbeafe' }
  }

  const operationLabel = (operation: string) => {
    if (operation === 'upload_file') return '上传文件'
    if (operation === 'index_document') return '建立索引'
    if (operation === 'rebuild_index') return '重建索引'
    return operation || '-'
  }

  const operationStatusLabel = (status: string) => {
    if (status === 'success') return '成功'
    if (status === 'failed') return '失败'
    if (status === 'partial_success') return '部分成功'
    return status || '-'
  }

  const sourceLabel = (source: string) => {
    if (source === 'web') return '页面'
    if (source === 'mcp') return 'MCP'
    if (source === 'admin_rebuild') return '重建'
    return source || '-'
  }

  const formatLogTime = (value: string) => {
    if (!value) return '-'
    return new Date(value).toLocaleString('zh-CN', {
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
    })
  }

  const formatLogMeta = (log: OperationLogEntry) => {
    const parts: string[] = []
    const indexedDocuments = log.metadata?.indexedDocuments
    const skippedFileCount = log.metadata?.skippedFileCount
    const chunkCount = log.metadata?.chunkCount
    if (typeof indexedDocuments === 'number') parts.push(`索引 ${indexedDocuments} 份`)
    if (typeof skippedFileCount === 'number' && skippedFileCount > 0) {
      parts.push(`跳过 ${skippedFileCount} 份`)
    }
    if (typeof chunkCount === 'number') parts.push(`分块 ${chunkCount}`)
    if (log.durationMs > 0) parts.push(`${log.durationMs}ms`)
    return parts.join(' · ')
  }

  const handleLogFilterChange = (field: keyof OperationLogFilters, value: string) => {
    onOperationLogFiltersChange({
      ...operationLogFilters,
      [field]: value,
    })
  }

  const handleShowLogs = () => {
    setActiveView('logs')
    onRefreshOperationLogs()
  }

  const uploadProgressPercent =
    directoryUploadTask.eligibleFiles > 0
      ? Math.round((directoryUploadTask.processedFiles / directoryUploadTask.eligibleFiles) * 100)
      : 0

  const visibleFailedItems = useMemo(() => directoryUploadTask.failedItems, [directoryUploadTask.failedItems])

  const visibleSkippedItems = useMemo(
    () => directoryUploadTask.skippedItems,
    [directoryUploadTask.skippedItems],
  )

  const selectedKnowledgeBase = useMemo(
    () =>
      knowledgeBases.find((knowledgeBase) => knowledgeBase.id === selectedKnowledgeBaseId) ?? null,
    [knowledgeBases, selectedKnowledgeBaseId],
  )

  const activeKnowledgeBase = useMemo(
    () => knowledgeBases.find((knowledgeBase) => knowledgeBase.id === activeKnowledgeBaseId) ?? null,
    [activeKnowledgeBaseId, knowledgeBases],
  )

  const activeDocument = useMemo(() => {
    if (!activeKnowledgeBase || !activeDocumentId) {
      return null
    }

    return (
      activeKnowledgeBase.documents.find((document) => document.id === activeDocumentId) ?? null
    )
  }, [activeDocumentId, activeKnowledgeBase])

  const normalizedKnowledgeBaseQuery = knowledgeBaseQuery.trim().toLowerCase()
  const filteredKnowledgeBases = useMemo(() => {
    if (!normalizedKnowledgeBaseQuery) {
      return knowledgeBases
    }

    return knowledgeBases.filter((knowledgeBase) => {
      const baseText = `${knowledgeBase.name} ${knowledgeBase.description}`.toLowerCase()
      if (baseText.includes(normalizedKnowledgeBaseQuery)) {
        return true
      }

      return knowledgeBase.documents.some((document) =>
        `${document.name} ${document.contentPreview ?? ''}`
          .toLowerCase()
          .includes(normalizedKnowledgeBaseQuery),
      )
    })
  }, [knowledgeBases, normalizedKnowledgeBaseQuery])

  const normalizedDocumentQuery = deferredDocumentQuery.trim().toLowerCase()
  const filteredAndSortedDocuments = useMemo(() => {
    if (!selectedKnowledgeBase) {
      return []
    }

    const filtered = normalizedDocumentQuery
      ? selectedKnowledgeBase.documents.filter((document) =>
          `${document.name} ${document.contentPreview ?? ''}`
            .toLowerCase()
            .includes(normalizedDocumentQuery),
        )
      : selectedKnowledgeBase.documents

    const [field, order] = documentSort.split(':') as ['uploadedAt' | 'name' | 'size', 'asc' | 'desc']
    return [...filtered].sort((left, right) => {
      let comparison = 0
      if (field === 'name') comparison = left.name.localeCompare(right.name, 'zh-CN')
      if (field === 'size') comparison = left.size - right.size
      if (field === 'uploadedAt') {
        comparison = new Date(left.uploadedAt).getTime() - new Date(right.uploadedAt).getTime()
      }
      return order === 'asc' ? comparison : -comparison
    })
  }, [documentSort, normalizedDocumentQuery, selectedKnowledgeBase])
  const visibleDocumentPage = useMemo(
    () => getDocumentPage(filteredAndSortedDocuments, documentPage),
    [documentPage, filteredAndSortedDocuments],
  )
  const healthByDocumentId = useMemo(
    () => new Map(knowledgeHealth?.documents.map((document) => [document.documentId, document]) ?? []),
    [knowledgeHealth],
  )
  const isBrowsingActiveKnowledgeBase =
    selectedKnowledgeBase?.id !== null && selectedKnowledgeBase?.id === activeKnowledgeBaseId
  const activeScopeText = activeDocument
    ? `文档问答：${activeDocument.name}`
    : activeKnowledgeBase
      ? `知识库问答：${activeKnowledgeBase.name}`
      : knowledgeBases.length > 0
        ? '全部知识库问答'
        : '未设置聊天范围'

  const isTaskVisible = directoryUploadTask.status !== 'idle'
  const canCancelUpload =
    directoryUploadTask.status === 'uploading' || directoryUploadTask.status === 'canceling'
  const canContinueUpload =
    (directoryUploadTask.status === 'canceled' || directoryUploadTask.status === 'partial-failed') &&
    directoryUploadTask.pendingFiles > 0

  const isTaskActive =
    directoryUploadTask.status === 'scanning' ||
    directoryUploadTask.status === 'uploading' ||
    directoryUploadTask.status === 'canceling'

  const refreshKnowledgeHealth = async (knowledgeBaseId: string) => {
    setIsKnowledgeHealthLoading(true)
    setKnowledgeHealthError(null)
    try {
      const health = await onFetchKnowledgeBaseHealth(knowledgeBaseId)
      setKnowledgeHealth(health)
    } catch (error) {
      setKnowledgeHealth(null)
      setKnowledgeHealthError(error instanceof Error ? error.message : '知识库健康状态加载失败')
    } finally {
      setIsKnowledgeHealthLoading(false)
    }
  }

  const handleOpenDocumentDetail = async (knowledgeBaseId: string, documentId: string) => {
    setDocumentDetail(null)
    setDocumentDetailError(null)
    setDocumentDetailLoadingId(documentId)
    try {
      setDocumentDetail(await onFetchDocumentDetail(knowledgeBaseId, documentId))
    } catch (error) {
      setDocumentDetailError(error instanceof Error ? error.message : '文档详情加载失败')
    } finally {
      setDocumentDetailLoadingId(null)
    }
  }

  const handleReindexDocument = async (knowledgeBaseId: string, documentId: string) => {
    setReindexingDocumentId(documentId)
    try {
      await onReindexDocument(knowledgeBaseId, documentId)
      await refreshKnowledgeHealth(knowledgeBaseId)
      if (documentDetail?.document.id === documentId) {
        setDocumentDetail(await onFetchDocumentDetail(knowledgeBaseId, documentId))
      }
    } catch (error) {
      window.alert(`重建索引失败：${error instanceof Error ? error.message : '未知错误'}`)
    } finally {
      setReindexingDocumentId(null)
    }
  }

  useEffect(() => {
    fetchKnowledgeHealthRef.current = onFetchKnowledgeBaseHealth
  }, [onFetchKnowledgeBaseHealth])

  useEffect(() => {
    if (!open || !selectedKnowledgeBaseId) {
      setKnowledgeHealth(null)
      setKnowledgeHealthError(null)
      return
    }

    let canceled = false
    setIsKnowledgeHealthLoading(true)
    setKnowledgeHealthError(null)
    void fetchKnowledgeHealthRef.current(selectedKnowledgeBaseId)
      .then((health) => {
        if (!canceled) setKnowledgeHealth(health)
      })
      .catch((error: unknown) => {
        if (canceled) return
        setKnowledgeHealth(null)
        setKnowledgeHealthError(error instanceof Error ? error.message : '知识库健康状态加载失败')
      })
      .finally(() => {
        if (!canceled) setIsKnowledgeHealthLoading(false)
      })

    return () => {
      canceled = true
    }
  }, [open, selectedKnowledgeBaseId])

  useEffect(() => {
    if (isTaskActive) {
      setShowUploadTaskDetails(true)
    }
  }, [isTaskActive])

  useEffect(() => {
    setShowFailedItems(false)
    setShowSkippedItems(false)
  }, [directoryUploadTask.knowledgeBaseId, directoryUploadTask.status])

  useEffect(() => {
    setDocumentQuery('')
    setDocumentPage(1)
    setDocumentDetail(null)
    setDocumentDetailError(null)
  }, [selectedKnowledgeBaseId])

  useEffect(() => {
    setDocumentPage(1)
  }, [deferredDocumentQuery, documentSort])

  useEffect(() => {
    if (documentPage !== visibleDocumentPage.page) {
      setDocumentPage(visibleDocumentPage.page)
    }
  }, [documentPage, visibleDocumentPage.page])

  if (!open) return null

  return (
    <>
      {/* 主弹窗 */}
      <div className="kb-backdrop" onClick={onClose}>
        <div className="kb-modal" onClick={(e) => e.stopPropagation()}>
          {/* 头部 */}
          <div className="kb-header">
            <div className="kb-header-left">
              <div className="kb-header-icon">🗂️</div>
              <div>
                <h2 className="kb-header-title">知识库管理</h2>
                <p className="kb-header-sub">
                  共 {knowledgeBases.length} 个知识库 ·{' '}
                  {knowledgeBases.reduce((s, kb) => s + kb.documents.length, 0)} 份文档
                </p>
              </div>
            </div>
            <div className="kb-header-actions">
              <div className="kb-view-switch" role="tablist" aria-label="知识库面板视图">
                <button
                  type="button"
                  className={`kb-view-switch-btn${activeView === 'knowledge' ? ' kb-view-switch-btn--active' : ''}`}
                  onClick={() => setActiveView('knowledge')}
                >
                  知识库
                </button>
                <button
                  type="button"
                  className={`kb-view-switch-btn${activeView === 'logs' ? ' kb-view-switch-btn--active' : ''}`}
                  onClick={handleShowLogs}
                >
                  操作日志
                </button>
              </div>
              <button className="kb-create-btn" onClick={handleOpenCreate}>
                <span>＋</span> 新建知识库
              </button>
              <button className="kb-close-btn" onClick={onClose} title="关闭">✕</button>
            </div>
          </div>

          {/* 内容区 */}
          <div className="kb-body">
            {activeView === 'logs' ? (
              <section className="kb-log-panel">
                <div className="kb-log-toolbar">
                  <label className="kb-search-field kb-log-filter">
                    <span>知识库</span>
                    <select
                      value={operationLogFilters.knowledgeBaseId}
                      onChange={(event) => handleLogFilterChange('knowledgeBaseId', event.target.value)}
                    >
                      <option value="">全部知识库</option>
                      {knowledgeBases.map((knowledgeBase) => (
                        <option key={knowledgeBase.id} value={knowledgeBase.id}>
                          {knowledgeBase.name}
                        </option>
                      ))}
                    </select>
                  </label>
                  <label className="kb-search-field kb-log-filter">
                    <span>操作</span>
                    <select
                      value={operationLogFilters.operation}
                      onChange={(event) => handleLogFilterChange('operation', event.target.value)}
                    >
                      <option value="">全部操作</option>
                      <option value="upload_file">上传文件</option>
                      <option value="index_document">建立索引</option>
                      <option value="rebuild_index">重建索引</option>
                    </select>
                  </label>
                  <label className="kb-search-field kb-log-filter">
                    <span>结果</span>
                    <select
                      value={operationLogFilters.status}
                      onChange={(event) => handleLogFilterChange('status', event.target.value)}
                    >
                      <option value="">全部结果</option>
                      <option value="success">成功</option>
                      <option value="failed">失败</option>
                      <option value="partial_success">部分成功</option>
                    </select>
                  </label>
                  <label className="kb-search-field kb-log-filter">
                    <span>来源</span>
                    <select
                      value={operationLogFilters.source}
                      onChange={(event) => handleLogFilterChange('source', event.target.value)}
                    >
                      <option value="">全部来源</option>
                      <option value="web">页面</option>
                      <option value="mcp">MCP</option>
                      <option value="admin_rebuild">重建</option>
                    </select>
                  </label>
                  <button
                    type="button"
                    className="kb-export-btn"
                    onClick={onRefreshOperationLogs}
                    disabled={isOperationLogLoading}
                  >
                    {isOperationLogLoading ? '刷新中...' : '刷新'}
                  </button>
                </div>

                <div className="kb-log-summary">
                  最近 {operationLogs.items.length} 条 / 共 {operationLogs.total} 条，最多保留 1000 条。
                </div>

                {operationLogError && (
                  <div className="kb-filter-notice kb-filter-notice--danger">
                    {operationLogError}
                  </div>
                )}

                <div className="kb-log-list">
                  {isOperationLogLoading && operationLogs.items.length === 0 ? (
                    <div className="kb-docs-empty">
                      <span>⏳</span>
                      <span>正在加载操作日志</span>
                    </div>
                  ) : operationLogs.items.length === 0 ? (
                    <div className="kb-docs-empty">
                      <span>🧾</span>
                      <span>暂无匹配的操作日志</span>
                    </div>
                  ) : (
                    operationLogs.items.map((log) => (
                      <article
                        key={log.id}
                        className={`kb-log-item kb-log-item--${log.status}`}
                      >
                        <div className="kb-log-main">
                          <div className="kb-log-title-row">
                            <span className="kb-log-operation">{operationLabel(log.operation)}</span>
                            <span className={`kb-log-status kb-log-status--${log.status}`}>
                              {operationStatusLabel(log.status)}
                            </span>
                            <span className="kb-log-source">{sourceLabel(log.source)}</span>
                          </div>
                          <div className="kb-log-message">
                            {log.message || operationLabel(log.operation)}
                            {log.error && <span className="kb-log-error"> · {log.error}</span>}
                          </div>
                          <div className="kb-log-meta">
                            <span>{formatLogTime(log.createdAt)}</span>
                            {log.knowledgeBaseName || log.knowledgeBaseId ? (
                              <span>{log.knowledgeBaseName || log.knowledgeBaseId}</span>
                            ) : null}
                            {log.documentName && <span>{log.documentName}</span>}
                            {log.stage && <span>阶段：{log.stage}</span>}
                            {formatLogMeta(log) && <span>{formatLogMeta(log)}</span>}
                          </div>
                        </div>
                        <div className="kb-log-correlation" title={log.correlationId}>
                          {log.correlationId}
                        </div>
                      </article>
                    ))
                  )}
                </div>
              </section>
            ) : knowledgeBases.length === 0 ? (
              <div className="kb-empty">
                <div className="kb-empty-icon">📚</div>
                <p className="kb-empty-title">暂无知识库</p>
                <p className="kb-empty-sub">创建第一个知识库，开始管理您的文档</p>
                <button className="kb-create-btn" onClick={handleOpenCreate}>
                  <span>＋</span> 新建知识库
                </button>
              </div>
            ) : (
              <div className="kb-workspace">
                <aside className="kb-sidebar-panel">
                  <div className="kb-panel-heading">
                    <div>
                      <h3>知识库</h3>
                      <p>先定位知识库，再在右侧查看和筛选文件。</p>
                    </div>
                    <span className="kb-panel-count">
                      {filteredKnowledgeBases.length}/{knowledgeBases.length}
                    </span>
                  </div>

                  <label className="kb-search-field">
                    <span>搜索知识库</span>
                    <input
                      type="text"
                      value={knowledgeBaseQuery}
                      onChange={(event) => setKnowledgeBaseQuery(event.target.value)}
                      placeholder="按名称、描述或文件名筛选"
                    />
                  </label>

                  <div className="kb-side-list">
                    {filteredKnowledgeBases.length === 0 ? (
                      <div className="kb-side-empty">未找到匹配的知识库</div>
                    ) : (
                      filteredKnowledgeBases.map((kb) => {
                        const isSelected = selectedKnowledgeBaseId === kb.id
                        const fileUploadState = knowledgeBaseFileUploadStates[kb.id]
                        return (
                          <button
                            key={kb.id}
                            type="button"
                            className={`kb-side-item${isSelected ? ' kb-side-item--active' : ''}`}
                            onClick={() => onSelectKnowledgeBase(kb.id)}
                          >
                            <div className="kb-side-item-top">
                              <span className="kb-side-item-name">{kb.name}</span>
                              <span className="kb-side-item-count">{kb.documents.length}</span>
                            </div>
                            {kb.description && (
                              <p className="kb-side-item-desc">{kb.description}</p>
                            )}
                            <div className="kb-side-item-meta">
                              <span>创建于 {new Date(kb.createdAt).toLocaleDateString('zh-CN')}</span>
                              {fileUploadState && (
                                <span className="kb-side-item-uploading">
                                  上传中 {fileUploadState.completedFiles}/{fileUploadState.totalFiles}
                                </span>
                              )}
                            </div>
                          </button>
                        )
                      })
                    )}
                  </div>
                </aside>

                <section className="kb-detail-panel">
                  {selectedKnowledgeBase ? (
                    <>
                      <div className="kb-detail-header">
                        <div className="kb-detail-summary">
                          <div className="kb-card-icon">📁</div>
                          <div className="kb-card-info">
                            <span className="kb-card-name">{selectedKnowledgeBase.name}</span>
                            {selectedKnowledgeBase.description && (
                              <span className="kb-card-desc">{selectedKnowledgeBase.description}</span>
                            )}
                            <span className="kb-card-meta">
                              {selectedKnowledgeBase.documents.length} 份文档 · 创建于{' '}
                              {new Date(selectedKnowledgeBase.createdAt).toLocaleDateString('zh-CN')}
                            </span>
                          </div>
                        </div>

                        <div className="kb-card-actions kb-card-actions--detail">
                          {/* 单文件上传直接在按钮上反馈进度，避免与目录上传任务面板混淆。 */}
                          <label
                            className={`kb-upload-btn${knowledgeBaseFileUploadStates[selectedKnowledgeBase.id] ? ' kb-upload-btn--loading' : ''}`}
                            title={knowledgeBaseFileUploadStates[selectedKnowledgeBase.id] ? '文件上传中' : '上传文档'}
                            aria-disabled={Boolean(knowledgeBaseFileUploadStates[selectedKnowledgeBase.id])}
                          >
                            {knowledgeBaseFileUploadStates[selectedKnowledgeBase.id] ? (
                              <span className="kb-inline-spinner" aria-hidden="true" />
                            ) : (
                              <span>📤</span>
                            )}
                            <span className="kb-upload-btn-label">
                              {knowledgeBaseFileUploadStates[selectedKnowledgeBase.id]
                                ? `上传中 ${knowledgeBaseFileUploadStates[selectedKnowledgeBase.id]?.completedFiles ?? 0}/${knowledgeBaseFileUploadStates[selectedKnowledgeBase.id]?.totalFiles ?? 0}`
                                : '上传文件'}
                            </span>
                            <input
                              type="file"
                              multiple
                              accept=".txt,.md,.pdf,.csv,.xlsx"
                              className="hidden-input"
                              disabled={Boolean(knowledgeBaseFileUploadStates[selectedKnowledgeBase.id])}
                              onChange={(event) => handleFileChange(selectedKnowledgeBase.id, event)}
                            />
                          </label>
                          <label className="kb-upload-btn kb-upload-btn--secondary" title="上传目录">
                            <span>🗂️</span> 上传目录
                            <input
                              ref={(element) => registerDirectoryInput(selectedKnowledgeBase.id, element)}
                              type="file"
                              multiple
                              className="hidden-input"
                              onChange={(event) => handleDirectoryChange(selectedKnowledgeBase.id, event)}
                            />
                          </label>
                          <button
                            type="button"
                            className="kb-export-btn"
                            onClick={() => onExportKnowledgeBase(selectedKnowledgeBase.id)}
                            disabled={exportingKnowledgeBaseId === selectedKnowledgeBase.id}
                          >
                            {exportingKnowledgeBaseId === selectedKnowledgeBase.id ? '导出中...' : '导出知识库'}
                          </button>
                          <button
                            className="kb-collapse-btn"
                            onClick={() => onToggleCollapse(selectedKnowledgeBase.id)}
                            title={collapsedKnowledgeBases[selectedKnowledgeBase.id] ? '展开文件列表' : '折叠文件列表'}
                          >
                            {collapsedKnowledgeBases[selectedKnowledgeBase.id] ? '▸' : '▾'}
                          </button>
                          {deleteConfirmId === selectedKnowledgeBase.id ? (
                            <div className="kb-delete-confirm">
                              <span>确认删除？</span>
                              <button
                                className="kb-delete-yes"
                                onClick={() => {
                                  onDeleteKnowledgeBase(selectedKnowledgeBase.id)
                                  setDeleteConfirmId(null)
                                }}
                              >
                                删除
                              </button>
                              <button
                                className="kb-delete-no"
                                onClick={() => setDeleteConfirmId(null)}
                              >
                                取消
                              </button>
                            </div>
                          ) : (
                            <button
                              className="kb-delete-btn"
                              onClick={() => setDeleteConfirmId(selectedKnowledgeBase.id)}
                              title="删除知识库"
                            >
                              🗑️
                            </button>
                          )}
                        </div>
                      </div>

                      {normalizedKnowledgeBaseQuery &&
                        !filteredKnowledgeBases.some(
                          (knowledgeBase) => knowledgeBase.id === selectedKnowledgeBase.id,
                        ) && (
                          <div className="kb-filter-notice">
                            当前右侧仍展示已选知识库，左侧筛选结果中未包含它。
                          </div>
                        )}

                      {!isBrowsingActiveKnowledgeBase && activeKnowledgeBase && (
                        <div className="kb-filter-notice">
                          当前正在浏览“{selectedKnowledgeBase.name}”，聊天仍使用“{activeScopeText}”。
                          需要点击“全部文档”或某个文件后，才会切换聊天范围。
                        </div>
                      )}

                      {isTaskVisible &&
                        directoryUploadTask.knowledgeBaseId === selectedKnowledgeBase.id && (
                          <div className="kb-upload-task-shell">
                            <div className="kb-upload-task-compact">
                              <div className="kb-upload-task-compact-main">
                                <span className={`kb-upload-task-pill kb-upload-task-pill--${directoryUploadTask.status}`}>
                                  {directoryUploadTask.status === 'scanning' && '扫描中'}
                                  {directoryUploadTask.status === 'uploading' && '上传中'}
                                  {directoryUploadTask.status === 'canceling' && '取消中'}
                                  {directoryUploadTask.status === 'canceled' && '已取消'}
                                  {directoryUploadTask.status === 'done' && '已完成'}
                                  {directoryUploadTask.status === 'partial-failed' && '部分完成'}
                                  {directoryUploadTask.status === 'failed' && '失败'}
                                </span>
                                <div className="kb-upload-task-compact-text">
                                  <div className="kb-upload-task-compact-title">目录上传任务</div>
                                  <div className="kb-upload-task-compact-summary">
                                    {directoryUploadTask.processedFiles}/{directoryUploadTask.eligibleFiles} · 成功 {directoryUploadTask.successFiles} · 失败 {directoryUploadTask.failedFiles} · 跳过 {directoryUploadTask.skippedFiles}
                                  </div>
                                </div>
                              </div>
                              <div className="kb-upload-task-actions">
                                <button
                                  className="kb-upload-task-btn kb-upload-task-btn--ghost"
                                  onClick={() => setShowUploadTaskDetails((prev) => !prev)}
                                >
                                  {showUploadTaskDetails ? '收起' : '详情'}
                                </button>
                                {canContinueUpload && (
                                  <button className="kb-upload-task-btn" onClick={onContinueDirectoryUpload}>
                                    继续上传
                                  </button>
                                )}
                                {canCancelUpload && (
                                  <button
                                    className="kb-upload-task-btn kb-upload-task-btn--danger"
                                    onClick={onCancelDirectoryUpload}
                                    disabled={directoryUploadTask.status === 'canceling'}
                                  >
                                    {directoryUploadTask.status === 'canceling' ? '取消中…' : '取消上传'}
                                  </button>
                                )}
                              </div>
                            </div>

                            {showUploadTaskDetails && (
                              <div className="kb-upload-task">
                                <div className="kb-upload-progress-meta">
                                  <span>
                                    已处理 {directoryUploadTask.processedFiles} / {directoryUploadTask.eligibleFiles}
                                  </span>
                                  <span>{uploadProgressPercent}%</span>
                                </div>
                                <div className="kb-upload-progress-track">
                                  <div
                                    className="kb-upload-progress-fill"
                                    style={{ width: `${uploadProgressPercent}%` }}
                                  />
                                </div>

                                <div className="kb-upload-stats-grid">
                                  <div className="kb-upload-stat-card">
                                    <span className="kb-upload-stat-label">总文件</span>
                                    <strong>{directoryUploadTask.totalFiles}</strong>
                                  </div>
                                  <div className="kb-upload-stat-card">
                                    <span className="kb-upload-stat-label">可上传</span>
                                    <strong>{directoryUploadTask.eligibleFiles}</strong>
                                  </div>
                                  <div className="kb-upload-stat-card">
                                    <span className="kb-upload-stat-label">成功</span>
                                    <strong>{directoryUploadTask.successFiles}</strong>
                                  </div>
                                  <div className="kb-upload-stat-card">
                                    <span className="kb-upload-stat-label">失败</span>
                                    <strong>{directoryUploadTask.failedFiles}</strong>
                                  </div>
                                  <div className="kb-upload-stat-card">
                                    <span className="kb-upload-stat-label">跳过</span>
                                    <strong>{directoryUploadTask.skippedFiles}</strong>
                                  </div>
                                  <div className="kb-upload-stat-card">
                                    <span className="kb-upload-stat-label">未执行</span>
                                    <strong>{directoryUploadTask.pendingFiles}</strong>
                                  </div>
                                </div>

                                {directoryUploadTask.currentFilePath && (
                                  <div className="kb-upload-current-file">
                                    当前处理：{directoryUploadTask.currentFilePath}
                                  </div>
                                )}

                                {directoryUploadTask.summaryMessage && (
                                  <div className="kb-upload-summary">{directoryUploadTask.summaryMessage}</div>
                                )}

                                {directoryUploadTask.failedItems.length > 0 && (
                                  <div className="kb-upload-issues-toggle-row">
                                    <button
                                      className="kb-upload-task-btn kb-upload-task-btn--ghost"
                                      onClick={() => setShowFailedItems((prev) => !prev)}
                                    >
                                      {showFailedItems
                                        ? '隐藏失败文件'
                                        : `查看失败文件（${directoryUploadTask.failedItems.length}）`}
                                    </button>
                                  </div>
                                )}

                                {showFailedItems && visibleFailedItems.length > 0 && (
                                  <div className="kb-upload-issues">
                                    <div className="kb-upload-issues-title">失败文件</div>
                                    {visibleFailedItems.map((item) => (
                                      <div key={`${item.path}-${item.reason}`} className="kb-upload-issue-item">
                                        <span className="kb-upload-issue-path">{item.path}</span>
                                        <span className="kb-upload-issue-reason">{item.reason}</span>
                                      </div>
                                    ))}
                                  </div>
                                )}

                                {directoryUploadTask.skippedItems.length > 0 && (
                                  <div className="kb-upload-issues-toggle-row">
                                    <button
                                      className="kb-upload-task-btn kb-upload-task-btn--ghost"
                                      onClick={() => setShowSkippedItems((prev) => !prev)}
                                    >
                                      {showSkippedItems
                                        ? '隐藏已跳过文件'
                                        : `查看已跳过文件（${directoryUploadTask.skippedItems.length}）`}
                                    </button>
                                  </div>
                                )}

                                {showSkippedItems && visibleSkippedItems.length > 0 && (
                                  <div className="kb-upload-issues kb-upload-issues--muted">
                                    <div className="kb-upload-issues-title">已跳过文件</div>
                                    {visibleSkippedItems.map((item) => (
                                      <div key={`${item.path}-${item.reason}`} className="kb-upload-issue-item">
                                        <span className="kb-upload-issue-path">{item.path}</span>
                                        <span className="kb-upload-issue-reason">{item.reason}</span>
                                      </div>
                                    ))}
                                  </div>
                                )}
                              </div>
                            )}
                          </div>
                        )}

                      <div className="kb-health-summary">
                        <div>
                          <span>索引健康</span>
                          <strong>
                            {isKnowledgeHealthLoading
                              ? '检查中…'
                              : knowledgeHealth
                                ? `${knowledgeHealth.score} 分 · ${knowledgeHealth.status}`
                                : '暂无结果'}
                          </strong>
                          {knowledgeHealth?.recommendations[0] && (
                            <small>{knowledgeHealth.recommendations[0]}</small>
                          )}
                          {knowledgeHealthError && <small className="kb-health-error">{knowledgeHealthError}</small>}
                        </div>
                        <button
                          type="button"
                          className="kb-export-btn"
                          disabled={isKnowledgeHealthLoading}
                          onClick={() => void refreshKnowledgeHealth(selectedKnowledgeBase.id)}
                        >
                          刷新健康状态
                        </button>
                      </div>

                      <div className="kb-detail-toolbar">
                        <div className="kb-current-scope">
                          <button
                            className={`kb-scope-btn${isBrowsingActiveKnowledgeBase && activeDocumentId === null ? ' kb-scope-btn--active' : ''}`}
                            onClick={() => onSelectDocument(selectedKnowledgeBase.id, null)}
                          >
                            全部文档
                          </button>
                          <span className="kb-current-scope-chip">当前聊天范围：{activeScopeText}</span>
                        </div>
                        <label className="kb-search-field kb-search-field--compact">
                          <span>搜索文件</span>
                          <input
                            type="text"
                            value={documentQuery}
                            onChange={(event) => setDocumentQuery(event.target.value)}
                            placeholder="按文件名或预览内容筛选"
                          />
                        </label>
                        <label className="kb-search-field kb-search-field--compact kb-document-sort">
                          <span>排序</span>
                          <select
                            value={documentSort}
                            onChange={(event) => setDocumentSort(event.target.value as DocumentSort)}
                          >
                            <option value="uploadedAt:desc">最近上传</option>
                            <option value="uploadedAt:asc">最早上传</option>
                            <option value="name:asc">名称升序</option>
                            <option value="name:desc">名称降序</option>
                            <option value="size:desc">文件较大</option>
                            <option value="size:asc">文件较小</option>
                          </select>
                        </label>
                      </div>

                      {!collapsedKnowledgeBases[selectedKnowledgeBase.id] ? (
                        <div className="kb-docs">
                          {selectedKnowledgeBase.documents.length === 0 ? (
                            <div className="kb-docs-empty">
                              <span>📄</span>
                              <span>暂无文档，点击「上传」添加文件</span>
                            </div>
                          ) : filteredAndSortedDocuments.length === 0 ? (
                            <div className="kb-docs-empty">
                              <span>🔎</span>
                              <span>没有匹配当前筛选条件的文件</span>
                            </div>
                          ) : (
                            visibleDocumentPage.items.map((doc) => {
                              const badge = statusLabel(doc.status)
                              const health = healthByDocumentId.get(doc.id)
                              const needsReindex = Boolean(health?.needsReindex || doc.indexError)
                              return (
                                <div
                                  key={doc.id}
                                  className={`kb-doc-item${isBrowsingActiveKnowledgeBase && activeDocumentId === doc.id ? ' kb-doc-item--active' : ''}${needsReindex ? ' kb-doc-item--attention' : ''}`}
                                >
                                  <button
                                    className="kb-doc-main"
                                    onClick={() => onSelectDocument(selectedKnowledgeBase.id, doc.id)}
                                  >
                                    <div className="kb-doc-top">
                                      <span className="kb-doc-icon">📄</span>
                                      <span className="kb-doc-name">{doc.name}</span>
                                      <span
                                        className="kb-doc-badge"
                                        style={{ color: badge.color, background: badge.bg }}
                                      >
                                        {badge.text}
                                      </span>
                                    </div>
                                    {doc.contentPreview && (
                                      <p className="kb-doc-preview">{formatDocumentPreviewText(doc.contentPreview)}</p>
                                    )}
                                    <div className="kb-doc-meta">
                                      <span>{doc.sizeLabel}</span>
                                      <span>·</span>
                                      <span>{health?.chunkCount ?? doc.chunkCount ?? 0} chunks</span>
                                      <span>·</span>
                                      <span>{new Date(doc.uploadedAt).toLocaleDateString('zh-CN')}</span>
                                    </div>
                                    {(doc.indexError || health?.recommendation) && (
                                      <p className="kb-doc-issue">{doc.indexError || health?.recommendation}</p>
                                    )}
                                  </button>
                                  <div className="kb-doc-actions">
                                    <button
                                      className="kb-doc-detail"
                                      disabled={documentDetailLoadingId === doc.id}
                                      onClick={() => void handleOpenDocumentDetail(selectedKnowledgeBase.id, doc.id)}
                                      title="查看文档详情"
                                    >
                                      {documentDetailLoadingId === doc.id ? '…' : '详情'}
                                    </button>
                                    <button
                                      className="kb-doc-reindex"
                                      disabled={reindexingDocumentId === doc.id}
                                      onClick={() => void handleReindexDocument(selectedKnowledgeBase.id, doc.id)}
                                      title="重新建立索引"
                                    >
                                      {reindexingDocumentId === doc.id ? '重建中' : '重建'}
                                    </button>
                                    <button
                                      className="kb-doc-remove"
                                      onClick={() => onRemoveDocument(selectedKnowledgeBase.id, doc.id)}
                                      title="删除文档"
                                    >
                                      ✕
                                    </button>
                                  </div>
                                </div>
                              )
                            })
                          )}

                          {filteredAndSortedDocuments.length > 50 && (
                            <div className="kb-document-pagination" aria-label="文档分页">
                              <span>共 {filteredAndSortedDocuments.length} 份文档</span>
                              <div>
                                <button
                                  type="button"
                                  disabled={visibleDocumentPage.page === 1}
                                  onClick={() => setDocumentPage((page) => Math.max(1, page - 1))}
                                >
                                  上一页
                                </button>
                                <strong>第 {visibleDocumentPage.page} / {visibleDocumentPage.pageCount} 页</strong>
                                <button
                                  type="button"
                                  disabled={visibleDocumentPage.page === visibleDocumentPage.pageCount}
                                  onClick={() => setDocumentPage((page) => Math.min(visibleDocumentPage.pageCount, page + 1))}
                                >
                                  下一页
                                </button>
                              </div>
                            </div>
                          )}
                        </div>
                      ) : (
                        <div className="kb-docs-empty kb-docs-empty--collapsed">
                          <span>🗂️</span>
                          <span>文件列表已折叠，点击右上角展开。</span>
                        </div>
                      )}
                    </>
                  ) : (
                    <div className="kb-empty kb-empty--inner">
                      <div className="kb-empty-icon">📁</div>
                      <p className="kb-empty-title">请选择知识库</p>
                      <p className="kb-empty-sub">先在左侧选择一个知识库，再查看和筛选文件。</p>
                    </div>
                  )}
                </section>
              </div>
            )}
          </div>
        </div>
      </div>

      {/* 新建知识库弹窗 */}
      {showCreateModal && (
        <div className="kb-create-backdrop" onClick={handleCancelCreate}>
          <div className="kb-create-dialog" onClick={(e) => e.stopPropagation()}>
            <div className="kb-create-dialog-header">
              <h3>新建知识库</h3>
              <button className="kb-close-btn" onClick={handleCancelCreate}>✕</button>
            </div>
            <div className="kb-create-dialog-body">
              <div className="kb-form-field">
                <label className="kb-form-label">知识库名称 <span className="kb-required">*</span></label>
                <input
                  className="kb-form-input"
                  type="text"
                  placeholder="例如：产品文档、技术手册…"
                  value={newName}
                  onChange={(e) => setNewName(e.target.value)}
                  onKeyDown={(e) => e.key === 'Enter' && handleConfirmCreate()}
                  autoFocus
                  maxLength={50}
                />
              </div>
              <div className="kb-form-field">
                <label className="kb-form-label">描述（可选）</label>
                <textarea
                  className="kb-form-textarea"
                  placeholder="简要描述该知识库的用途…"
                  value={newDescription}
                  onChange={(e) => setNewDescription(e.target.value)}
                  rows={3}
                  maxLength={200}
                />
              </div>
            </div>
            <div className="kb-create-dialog-footer">
              <button className="kb-cancel-btn" onClick={handleCancelCreate}>取消</button>
              <button
                className="kb-confirm-btn"
                onClick={handleConfirmCreate}
                disabled={!newName.trim()}
              >
                创建知识库
              </button>
            </div>
          </div>
        </div>
      )}

      <DocumentDetailDialog
        detail={documentDetail}
        error={documentDetailError}
        loading={documentDetailLoadingId !== null}
        onClose={() => {
          setDocumentDetail(null)
          setDocumentDetailError(null)
          setDocumentDetailLoadingId(null)
        }}
      />
    </>
  )
}

export default KnowledgePanel
