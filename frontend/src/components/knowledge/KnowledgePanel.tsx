import React, { ChangeEvent, useEffect, useMemo, useRef, useState } from 'react'
import { DirectoryUploadTask, DocumentItem, KnowledgeBase } from '../../App'
import type { DocumentDetailResponse, RetrievalDebugResponse } from '../../services/api'

interface KnowledgePanelProps {
  open: boolean
  knowledgeBases: KnowledgeBase[]
  collapsedKnowledgeBases: Record<string, boolean>
  onToggleCollapse: (knowledgeBaseId: string) => void
  selectedKnowledgeBaseId: string | null
  selectedDocumentId: string | null
  onSelectKnowledgeBase: (knowledgeBaseId: string) => void
  onSelectDocument: (knowledgeBaseId: string, documentId: string | null) => void
  onCreateKnowledgeBase: (name: string, description: string) => void
  onDeleteKnowledgeBase: (knowledgeBaseId: string) => void
  onUploadFiles: (knowledgeBaseId: string, files: FileList | null) => void
  onUploadDirectory: (knowledgeBaseId: string, files: FileList | null) => void
  onGenerateEvalDataset: (knowledgeBaseId: string) => Promise<void>
  directoryUploadTask: DirectoryUploadTask
  onCancelDirectoryUpload: () => void
  onContinueDirectoryUpload: () => void
  onRemoveDocument: (knowledgeBaseId: string, documentId: string) => void
  onFetchDocumentDetail: (
    knowledgeBaseId: string,
    documentId: string,
  ) => Promise<DocumentDetailResponse>
  onReindexDocument: (knowledgeBaseId: string, documentId: string) => Promise<DocumentItem>
  onDebugRetrieval: (
    knowledgeBaseId: string,
    query: string,
    documentId: string | null,
  ) => Promise<RetrievalDebugResponse>
  onClose: () => void
}

const KnowledgePanel: React.FC<KnowledgePanelProps> = ({
  open,
  knowledgeBases,
  collapsedKnowledgeBases,
  onToggleCollapse,
  selectedKnowledgeBaseId,
  selectedDocumentId,
  onSelectKnowledgeBase,
  onSelectDocument,
  onCreateKnowledgeBase,
  onDeleteKnowledgeBase,
  onUploadFiles,
  onUploadDirectory,
  onGenerateEvalDataset,
  directoryUploadTask,
  onCancelDirectoryUpload,
  onContinueDirectoryUpload,
  onRemoveDocument,
  onFetchDocumentDetail,
  onReindexDocument,
  onDebugRetrieval,
  onClose,
}) => {
  const [showCreateModal, setShowCreateModal] = useState(false)
  const [newName, setNewName] = useState('')
  const [newDescription, setNewDescription] = useState('')
  const [deleteConfirmId, setDeleteConfirmId] = useState<string | null>(null)
  const [showUploadTaskDetails, setShowUploadTaskDetails] = useState(false)
  const [showFailedItems, setShowFailedItems] = useState(false)
  const [showSkippedItems, setShowSkippedItems] = useState(false)
  const [generatingEvalKnowledgeBaseId, setGeneratingEvalKnowledgeBaseId] = useState<string | null>(null)
  const [documentDetail, setDocumentDetail] = useState<DocumentDetailResponse | null>(null)
  const [documentDetailLoadingId, setDocumentDetailLoadingId] = useState<string | null>(null)
  const [documentDetailError, setDocumentDetailError] = useState('')
  const [reindexingDocumentId, setReindexingDocumentId] = useState<string | null>(null)
  const [retrievalQuery, setRetrievalQuery] = useState('')
  const [retrievalDebugKnowledgeBaseId, setRetrievalDebugKnowledgeBaseId] = useState<string | null>(null)
  const [retrievalDebugResult, setRetrievalDebugResult] = useState<RetrievalDebugResponse | null>(null)
  const [retrievalDebugError, setRetrievalDebugError] = useState('')
  const directoryInputRefs = useRef<Record<string, HTMLInputElement | null>>({})

  useEffect(() => {
    setRetrievalDebugResult(null)
    setRetrievalDebugError('')
  }, [selectedKnowledgeBaseId, selectedDocumentId])

  const handleFileChange = (knowledgeBaseId: string, event: ChangeEvent<HTMLInputElement>) => {
    onUploadFiles(knowledgeBaseId, event.target.files)
    event.target.value = ''
  }

  const handleDirectoryChange = (knowledgeBaseId: string, event: ChangeEvent<HTMLInputElement>) => {
    onUploadDirectory(knowledgeBaseId, event.target.files)
    event.target.value = ''
  }

  const handleGenerateEvalDataset = async (knowledgeBaseId: string) => {
    setGeneratingEvalKnowledgeBaseId(knowledgeBaseId)
    try {
      await onGenerateEvalDataset(knowledgeBaseId)
    } finally {
      setGeneratingEvalKnowledgeBaseId(null)
    }
  }

  const handleOpenDocumentDetail = async (knowledgeBaseId: string, documentId: string) => {
    setDocumentDetail(null)
    setDocumentDetailError('')
    setDocumentDetailLoadingId(documentId)
    try {
      const detail = await onFetchDocumentDetail(knowledgeBaseId, documentId)
      setDocumentDetail(detail)
    } catch (error) {
      setDocumentDetailError(error instanceof Error ? error.message : '加载文档详情失败')
    } finally {
      setDocumentDetailLoadingId(null)
    }
  }

  const handleReindexDocument = async (knowledgeBaseId: string, documentId: string) => {
    setReindexingDocumentId(documentId)
    try {
      const updatedDocument = await onReindexDocument(knowledgeBaseId, documentId)
      if (documentDetail?.document.id === documentId) {
        const detail = await onFetchDocumentDetail(knowledgeBaseId, documentId)
        setDocumentDetail({
          ...detail,
          document: {
            ...detail.document,
            ...updatedDocument,
          },
        })
      }
    } finally {
      setReindexingDocumentId(null)
    }
  }

  const handleRunRetrievalDebug = async (knowledgeBaseId: string) => {
    const query = retrievalQuery.trim()
    if (!query) {
      setRetrievalDebugError('请输入要调试的问题')
      return
    }

    setRetrievalDebugKnowledgeBaseId(knowledgeBaseId)
    setRetrievalDebugError('')
    try {
      const result = await onDebugRetrieval(knowledgeBaseId, query, selectedDocumentId)
      setRetrievalDebugResult(result)
    } catch (error) {
      setRetrievalDebugResult(null)
      setRetrievalDebugError(error instanceof Error ? error.message : '检索调试失败')
    } finally {
      setRetrievalDebugKnowledgeBaseId(null)
    }
  }

  const handleDownloadRetrievalEvalCandidate = () => {
    if (!retrievalDebugResult?.evalCandidate) {
      return
    }

    const blob = new Blob([JSON.stringify([retrievalDebugResult.evalCandidate], null, 2)], {
      type: 'application/json;charset=utf-8',
    })
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    const timestamp = new Date().toISOString().slice(0, 19).replace(/[-:T]/g, '')
    const scope = retrievalDebugResult.documentId || retrievalDebugResult.knowledgeBaseId || 'all'
    link.href = url
    link.download = `retrieval_debug_eval_${scope}_${timestamp}.json`
    document.body.appendChild(link)
    link.click()
    link.remove()
    URL.revokeObjectURL(url)
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

  const chunkKindLabel = (kind: string) => {
    if (kind === 'structured_deterministic') return '确定性'
    if (kind === 'structured_summary') return '摘要'
    if (kind === 'structured_row') return '数据行'
    return '正文'
  }

  const structuredIntentLabel = (intent?: string) => {
    switch (intent) {
      case 'max':
        return '最大值'
      case 'min':
        return '最小值'
      case 'average':
        return '平均值'
      case 'filter':
        return '筛选'
      case 'group':
        return '分布'
      case 'count':
        return '计数'
      case 'preview':
        return '预览'
      default:
        return ''
    }
  }

  const selectedScopeLabel =
    selectedDocumentId
      ? knowledgeBases
          .flatMap((knowledgeBase) => knowledgeBase.documents)
          .find((document) => document.id === selectedDocumentId)?.name ?? '当前文档'
      : '全部文档'

  const uploadProgressPercent =
    directoryUploadTask.eligibleFiles > 0
      ? Math.round((directoryUploadTask.processedFiles / directoryUploadTask.eligibleFiles) * 100)
      : 0

  const visibleFailedItems = useMemo(() => directoryUploadTask.failedItems, [directoryUploadTask.failedItems])

  const visibleSkippedItems = useMemo(
    () => directoryUploadTask.skippedItems,
    [directoryUploadTask.skippedItems],
  )

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
              <div className="kb-list">
                {knowledgeBases.map((kb) => {
                  const isSelected = selectedKnowledgeBaseId === kb.id
                  const isCollapsed = collapsedKnowledgeBases[kb.id]
                  const isGeneratingEval = generatingEvalKnowledgeBaseId === kb.id
                  return (
                    <div key={kb.id} className={`kb-card${isSelected ? ' kb-card--active' : ''}`}>
                      {/* 知识库卡片头部 */}
                      <div className="kb-card-header">
                        <button
                          className="kb-card-main"
                          onClick={() => onSelectKnowledgeBase(kb.id)}
                        >
                          <div className="kb-card-icon">📁</div>
                          <div className="kb-card-info">
                            <span className="kb-card-name">{kb.name}</span>
                            {kb.description && (
                              <span className="kb-card-desc">{kb.description}</span>
                            )}
                            <span className="kb-card-meta">
                              {kb.documents.length} 份文档 · 创建于 {new Date(kb.createdAt).toLocaleDateString('zh-CN')}
                            </span>
                          </div>
                        </button>
                        <div className="kb-card-actions">
                          <label className="kb-upload-btn" title="上传文档">
                            <span>📤</span> 上传文件
                            <input
                              type="file"
                              multiple
                              accept=".txt,.md,.pdf,.csv,.xlsx"
                              className="hidden-input"
                              onChange={(e) => handleFileChange(kb.id, e)}
                            />
                          </label>
                          <label className="kb-upload-btn kb-upload-btn--secondary" title="上传目录">
                            <span>🗂️</span> 上传目录
                            <input
                              ref={(element) => registerDirectoryInput(kb.id, element)}
                              type="file"
                              multiple
                              className="hidden-input"
                              onChange={(e) => handleDirectoryChange(kb.id, e)}
                            />
                          </label>
                          <button
                            className="kb-eval-btn"
                            onClick={() => handleGenerateEvalDataset(kb.id)}
                            disabled={kb.documents.length === 0 || isGeneratingEval}
                            title="生成评估集"
                          >
                            <span>⤓</span> {isGeneratingEval ? '生成中' : '评估集'}
                          </button>
                          <button
                            className="kb-collapse-btn"
                            onClick={() => onToggleCollapse(kb.id)}
                            title={isCollapsed ? '展开' : '折叠'}
                          >
                            {isCollapsed ? '▸' : '▾'}
                          </button>
                          {deleteConfirmId === kb.id ? (
                            <div className="kb-delete-confirm">
                              <span>确认删除？</span>
                              <button
                                className="kb-delete-yes"
                                onClick={() => {
                                  onDeleteKnowledgeBase(kb.id)
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
                              onClick={() => setDeleteConfirmId(kb.id)}
                              title="删除知识库"
                            >
                              🗑️
                            </button>
                          )}
                        </div>
                      </div>

                      {isSelected && isTaskVisible && directoryUploadTask.knowledgeBaseId === kb.id && (
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

                      {/* 查询范围选择 */}
                      {isSelected && (
                        <div className="kb-scope-bar">
                          <button
                            className={`kb-scope-btn${selectedDocumentId === null ? ' kb-scope-btn--active' : ''}`}
                            onClick={() => onSelectDocument(kb.id, null)}
                          >
                            全部文档
                          </button>
                          {kb.documents.map((doc) => (
                            <button
                              key={doc.id}
                              className={`kb-scope-btn${selectedDocumentId === doc.id ? ' kb-scope-btn--active' : ''}`}
                              onClick={() => onSelectDocument(kb.id, doc.id)}
                            >
                              {doc.name}
                            </button>
                          ))}
                        </div>
                      )}

                      {isSelected && (
                        <div className="kb-retrieval-debug">
                          <div className="kb-retrieval-debug-head">
                            <div>
                              <h3>检索调试台</h3>
                              <p>当前范围：{selectedScopeLabel}</p>
                            </div>
                            <span className="kb-retrieval-mode">
                              {retrievalDebugResult?.searchMode === 'hybrid' ? '混合检索' : '向量检索'}
                            </span>
                          </div>
                          <div className="kb-retrieval-input-row">
                            <input
                              className="kb-retrieval-input"
                              value={retrievalQuery}
                              onChange={(event) => setRetrievalQuery(event.target.value)}
                              onKeyDown={(event) => {
                                if (event.key === 'Enter') {
                                  void handleRunRetrievalDebug(kb.id)
                                }
                              }}
                              placeholder="输入一个问题，查看实际命中的 chunk"
                            />
                            <button
                              className="kb-retrieval-run"
                              onClick={() => void handleRunRetrievalDebug(kb.id)}
                              disabled={retrievalDebugKnowledgeBaseId === kb.id}
                            >
                              {retrievalDebugKnowledgeBaseId === kb.id ? '检索中' : '运行'}
                            </button>
                          </div>

                          {retrievalDebugError && (
                            <div className="kb-retrieval-error">{retrievalDebugError}</div>
                          )}

                          {retrievalDebugResult && (
                            <div className="kb-retrieval-result">
                              <div className="kb-retrieval-summary">
                                <span>{retrievalDebugResult.count} 个命中</span>
                                <span>{retrievalDebugResult.elapsedMs} ms</span>
                                <span>{retrievalDebugResult.lowConfidence ? '低置信' : '置信正常'}</span>
                                <span>{retrievalDebugResult.deterministicUsed ? '确定性补全' : '向量优先'}</span>
                                {structuredIntentLabel(retrievalDebugResult.structuredIntent) && (
                                  <span>
                                    {structuredIntentLabel(retrievalDebugResult.structuredIntent)}
                                    {retrievalDebugResult.targetField ? `：${retrievalDebugResult.targetField}` : ''}
                                  </span>
                                )}
                              </div>
                              {retrievalDebugResult.evalCandidate && (
                                <div className="kb-retrieval-eval">
                                  <div>
                                    <strong>低置信评测候选</strong>
                                    <p>当前问题可沉淀为后续检索评测样本，下载后建议人工复核答案片段。</p>
                                  </div>
                                  <button onClick={handleDownloadRetrievalEvalCandidate}>
                                    下载样本
                                  </button>
                                </div>
                              )}
                              {retrievalDebugResult.contextPreview && (
                                <details className="kb-retrieval-context">
                                  <summary>上下文预览</summary>
                                  <pre>{retrievalDebugResult.contextPreview}</pre>
                                </details>
                              )}
                              <div className="kb-retrieval-hits">
                                {retrievalDebugResult.items.length === 0 ? (
                                  <div className="kb-docs-empty">没有命中 chunk</div>
                                ) : (
                                  retrievalDebugResult.items.map((item) => (
                                    <div key={item.id} className="kb-retrieval-hit">
                                      <div className="kb-retrieval-hit-head">
                                        <strong>{item.documentName}</strong>
                                        <span>{chunkKindLabel(item.kind)}</span>
                                        <span>#{item.index + 1}</span>
                                        <span>{item.score.toFixed(4)}</span>
                                      </div>
                                      <pre>{item.text}</pre>
                                    </div>
                                  ))
                                )}
                              </div>
                            </div>
                          )}
                        </div>
                      )}

                      {/* 文档列表 */}
                      {!isCollapsed && (
                        <div className="kb-docs">
                          {kb.documents.length === 0 ? (
                            <div className="kb-docs-empty">
                              <span>📄</span>
                              <span>暂无文档，点击「上传」添加文件</span>
                            </div>
                          ) : (
                            kb.documents.map((doc) => {
                              const badge = statusLabel(doc.status)
                              return (
                                <div
                                  key={doc.id}
                                  className={`kb-doc-item${selectedDocumentId === doc.id ? ' kb-doc-item--active' : ''}`}
                                >
                                  <button
                                    className="kb-doc-main"
                                    onClick={() => onSelectDocument(kb.id, doc.id)}
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
                                      {typeof doc.chunkCount === 'number' && (
                                        <>
                                          <span>·</span>
                                          <span>{doc.chunkCount} chunks</span>
                                        </>
                                      )}
                                      <span>·</span>
                                      <span>{new Date(doc.uploadedAt).toLocaleDateString('zh-CN')}</span>
                                    </div>
                                  </button>
                                  <div className="kb-doc-actions">
                                    <button
                                      className="kb-doc-action"
                                      onClick={() => handleOpenDocumentDetail(kb.id, doc.id)}
                                      disabled={documentDetailLoadingId === doc.id}
                                      title="查看索引详情"
                                    >
                                      {documentDetailLoadingId === doc.id ? '加载' : '详情'}
                                    </button>
                                    <button
                                      className="kb-doc-action"
                                      onClick={() => handleReindexDocument(kb.id, doc.id)}
                                      disabled={reindexingDocumentId === doc.id}
                                      title="重新解析并重建索引"
                                    >
                                      {reindexingDocumentId === doc.id ? '重建中' : '重建'}
                                    </button>
                                    <button
                                      className="kb-doc-remove"
                                      onClick={() => onRemoveDocument(kb.id, doc.id)}
                                      title="删除文档"
                                    >
                                      ✕
                                    </button>
                                  </div>
                                </div>
                              )
                            })
                          )}
                        </div>
                      )}
                    </div>
                  )
                })}
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

      {(documentDetail || documentDetailError) && (
        <div className="kb-detail-backdrop" onClick={() => {
          setDocumentDetail(null)
          setDocumentDetailError('')
        }}>
          <div className="kb-detail-dialog" onClick={(e) => e.stopPropagation()}>
            <div className="kb-detail-header">
              <div>
                <h3>{documentDetail?.document.name ?? '文档详情'}</h3>
                {documentDetail?.document.indexedAt && (
                  <p>最近索引：{new Date(documentDetail.document.indexedAt).toLocaleString('zh-CN')}</p>
                )}
              </div>
              <button
                className="kb-close-btn"
                onClick={() => {
                  setDocumentDetail(null)
                  setDocumentDetailError('')
                }}
              >
                ✕
              </button>
            </div>

            {documentDetailError ? (
              <div className="kb-detail-error">{documentDetailError}</div>
            ) : documentDetail && (
              <div className="kb-detail-body">
                <section className="kb-detail-section">
                  <div className="kb-detail-stats">
                    <div>
                      <span>原文字符</span>
                      <strong>{documentDetail.diagnostics.rawContentChars}</strong>
                    </div>
                    <div>
                      <span>分块数量</span>
                      <strong>{documentDetail.diagnostics.chunkCount}</strong>
                    </div>
                    <div>
                      <span>向量数量</span>
                      <strong>{documentDetail.diagnostics.vectorCount}</strong>
                    </div>
                    <div>
                      <span>摘要块</span>
                      <strong>{documentDetail.diagnostics.summaryChunkCount}</strong>
                    </div>
                    <div>
                      <span>数据行块</span>
                      <strong>{documentDetail.diagnostics.structuredRowCount}</strong>
                    </div>
                    <div>
                      <span>Qdrant</span>
                      <strong>{documentDetail.diagnostics.qdrantEnabled ? '启用' : '未启用'}</strong>
                    </div>
                  </div>
                  {documentDetail.document.indexError && (
                    <div className="kb-detail-error">索引错误：{documentDetail.document.indexError}</div>
                  )}
                </section>

                <section className="kb-detail-section">
                  <h4>摘要预览</h4>
                  <pre className="kb-detail-pre">{documentDetail.summary || '暂无摘要'}</pre>
                </section>

                <section className="kb-detail-section">
                  <h4>
                    原文预览
                    {documentDetail.diagnostics.rawContentTruncated && (
                      <span className="kb-detail-muted">已截断</span>
                    )}
                  </h4>
                  <pre className="kb-detail-pre">{documentDetail.rawContent || '暂无可读取原文'}</pre>
                </section>

                <section className="kb-detail-section">
                  <h4>
                    Chunk 预览
                    {documentDetail.diagnostics.chunkPreviewTruncated && (
                      <span className="kb-detail-muted">仅显示前 {documentDetail.chunks.length} 个</span>
                    )}
                  </h4>
                  <div className="kb-detail-chunks">
                    {documentDetail.chunks.length === 0 ? (
                      <div className="kb-docs-empty">暂无 chunk</div>
                    ) : (
                      documentDetail.chunks.map((chunk) => (
                        <div key={chunk.id} className="kb-detail-chunk">
                          <div className="kb-detail-chunk-head">
                            <span>#{chunk.index + 1}</span>
                            <span>{chunkKindLabel(chunk.kind)}</span>
                            <code>{chunk.id}</code>
                          </div>
                          <pre>{chunk.text}</pre>
                        </div>
                      ))
                    )}
                  </div>
                </section>
              </div>
            )}
          </div>
        </div>
      )}
    </>
  )
}

export default KnowledgePanel
