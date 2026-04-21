import React, { ChangeEvent, useEffect, useMemo, useRef, useState } from 'react'
import { DirectoryUploadTask, KnowledgeBase, KnowledgeBaseFileUploadState } from '../../App'

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
  onUploadFiles: (knowledgeBaseId: string, files: FileList | null) => void
  onUploadDirectory: (knowledgeBaseId: string, files: FileList | null) => void
  directoryUploadTask: DirectoryUploadTask
  knowledgeBaseFileUploadStates: Record<string, KnowledgeBaseFileUploadState>
  onCancelDirectoryUpload: () => void
  onContinueDirectoryUpload: () => void
  onRemoveDocument: (knowledgeBaseId: string, documentId: string) => void
  onClose: () => void
}

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
  onUploadFiles,
  onUploadDirectory,
  directoryUploadTask,
  knowledgeBaseFileUploadStates,
  onCancelDirectoryUpload,
  onContinueDirectoryUpload,
  onRemoveDocument,
  onClose,
}) => {
  const [showCreateModal, setShowCreateModal] = useState(false)
  const [newName, setNewName] = useState('')
  const [newDescription, setNewDescription] = useState('')
  const [knowledgeBaseQuery, setKnowledgeBaseQuery] = useState('')
  const [documentQuery, setDocumentQuery] = useState('')
  const [deleteConfirmId, setDeleteConfirmId] = useState<string | null>(null)
  const [showUploadTaskDetails, setShowUploadTaskDetails] = useState(false)
  const [showFailedItems, setShowFailedItems] = useState(false)
  const [showSkippedItems, setShowSkippedItems] = useState(false)
  const directoryInputRefs = useRef<Record<string, HTMLInputElement | null>>({})

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
    return { text: '就绪', color: '#2563eb', bg: '#dbeafe' }
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

  const normalizedDocumentQuery = documentQuery.trim().toLowerCase()
  const filteredDocuments = useMemo(() => {
    if (!selectedKnowledgeBase) {
      return []
    }

    if (!normalizedDocumentQuery) {
      return selectedKnowledgeBase.documents
    }

    return selectedKnowledgeBase.documents.filter((document) =>
      `${document.name} ${document.contentPreview ?? ''}`
        .toLowerCase()
        .includes(normalizedDocumentQuery),
    )
  }, [normalizedDocumentQuery, selectedKnowledgeBase])
  const isBrowsingActiveKnowledgeBase =
    selectedKnowledgeBase?.id !== null && selectedKnowledgeBase?.id === activeKnowledgeBaseId
  const activeScopeText = activeDocument
    ? `文档问答：${activeDocument.name}`
    : activeKnowledgeBase
      ? `知识库问答：${activeKnowledgeBase.name}`
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
  }, [selectedKnowledgeBaseId])

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
              <button className="kb-create-btn" onClick={handleOpenCreate}>
                <span>＋</span> 新建知识库
              </button>
              <button className="kb-close-btn" onClick={onClose} title="关闭">✕</button>
            </div>
          </div>

          {/* 内容区 */}
          <div className="kb-body">
            {knowledgeBases.length === 0 ? (
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
                      </div>

                      {!collapsedKnowledgeBases[selectedKnowledgeBase.id] ? (
                        <div className="kb-docs">
                          {selectedKnowledgeBase.documents.length === 0 ? (
                            <div className="kb-docs-empty">
                              <span>📄</span>
                              <span>暂无文档，点击「上传」添加文件</span>
                            </div>
                          ) : filteredDocuments.length === 0 ? (
                            <div className="kb-docs-empty">
                              <span>🔎</span>
                              <span>没有匹配当前筛选条件的文件</span>
                            </div>
                          ) : (
                            filteredDocuments.map((doc) => {
                              const badge = statusLabel(doc.status)
                              return (
                                <div
                                  key={doc.id}
                                  className={`kb-doc-item${isBrowsingActiveKnowledgeBase && activeDocumentId === doc.id ? ' kb-doc-item--active' : ''}`}
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
                                      <p className="kb-doc-preview">{doc.contentPreview}</p>
                                    )}
                                    <div className="kb-doc-meta">
                                      <span>{doc.sizeLabel}</span>
                                      <span>·</span>
                                      <span>{new Date(doc.uploadedAt).toLocaleDateString('zh-CN')}</span>
                                    </div>
                                  </button>
                                  <button
                                    className="kb-doc-remove"
                                    onClick={() => onRemoveDocument(selectedKnowledgeBase.id, doc.id)}
                                    title="删除文档"
                                  >
                                    ✕
                                  </button>
                                </div>
                              )
                            })
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
    </>
  )
}

export default KnowledgePanel
