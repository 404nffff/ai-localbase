import type { DocumentItem, KnowledgeBaseDocumentHealth } from '../../App'
import KnowledgeIcon from './KnowledgeIcon'
import { getDocumentPage } from './documentListPagination'
import { formatDocumentPreviewText } from './documentPreviewText'

interface DocumentListProps {
  documents: DocumentItem[]
  filteredDocuments: DocumentItem[]
  page: number
  knowledgeBaseId: string
  activeDocumentId: string | null
  isBrowsingActiveKnowledgeBase: boolean
  healthByDocumentId: Map<string, KnowledgeBaseDocumentHealth>
  documentDetailLoadingId: string | null
  reindexingDocumentId: string | null
  /** 跨当前筛选结果保留的待重建资料 ID。 */
  selectedIds: string[]
  /** 批量重建期间禁止选择、单项重建和删除。 */
  batchBusy: boolean
  /** 切换单份资料的批量选择状态。 */
  onToggleSelection: (id: string) => void
  onSelectDocument: (knowledgeBaseId: string, documentId: string | null) => void
  onOpenDocumentDetail: (knowledgeBaseId: string, documentId: string) => void
  onReindexDocument: (knowledgeBaseId: string, documentId: string) => void
  onRemoveDocument: (knowledgeBaseId: string, documentId: string) => void
  onPageChange: (page: number) => void
}

const statusLabel = (status: string) => {
  if (status === 'indexed') return { text: '已索引', color: '#166534', bg: '#dcfce7' }
  if (status === 'processing') return { text: '处理中', color: '#92400e', bg: '#fef3c7' }
  if (status === 'failed') return { text: '失败', color: '#b91c1c', bg: '#fee2e2' }
  return { text: '就绪', color: '#2563eb', bg: '#dbeafe' }
}

/** 展示当前资料分页和索引状态；选择独立于当前页，批量处理期间禁用冲突写操作。 */
const DocumentList = ({
  documents,
  filteredDocuments,
  page,
  knowledgeBaseId,
  activeDocumentId,
  isBrowsingActiveKnowledgeBase,
  healthByDocumentId,
  documentDetailLoadingId,
  reindexingDocumentId,
  selectedIds,
  batchBusy,
  onToggleSelection,
  onSelectDocument,
  onOpenDocumentDetail,
  onReindexDocument,
  onRemoveDocument,
  onPageChange,
}: DocumentListProps) => {
  const visibleDocumentPage = getDocumentPage(filteredDocuments, page, 50)

  if (documents.length === 0) {
    return (
      <div className="kb-docs-empty">
        <KnowledgeIcon name="file" size={22} />
        <span>暂无文档，点击「上传」添加文件</span>
      </div>
    )
  }

  if (filteredDocuments.length === 0) {
    return (
      <div className="kb-docs-empty">
        <KnowledgeIcon name="search" size={22} />
        <span>没有匹配当前筛选条件的文件</span>
      </div>
    )
  }

  return (
    <>
      {visibleDocumentPage.items.map((doc) => {
        const badge = statusLabel(doc.status)
        const health = healthByDocumentId.get(doc.id)
        const needsReindex = Boolean(health?.needsReindex || doc.indexError)
        return (
          <div
            key={doc.id}
            className={`kb-doc-item${isBrowsingActiveKnowledgeBase && activeDocumentId === doc.id ? ' kb-doc-item--active' : ''}${needsReindex ? ' kb-doc-item--attention' : ''}`}
          >
            <input type="checkbox" aria-label={`选择 ${doc.name}`} checked={selectedIds.includes(doc.id)} disabled={batchBusy} onChange={() => onToggleSelection(doc.id)} />
            <button
              className="kb-doc-main"
              onClick={() => onSelectDocument(knowledgeBaseId, doc.id)}
            >
              <div className="kb-doc-top">
                <span className="kb-doc-icon"><KnowledgeIcon name="file" size={17} /></span>
                <span className="kb-doc-name">{doc.name}</span>
                <span className="kb-doc-badge" style={{ color: badge.color, background: badge.bg }}>
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
                type="button"
                className="kb-doc-detail"
                disabled={documentDetailLoadingId === doc.id}
                onClick={() => onOpenDocumentDetail(knowledgeBaseId, doc.id)}
                title="查看文档详情"
              >
                {documentDetailLoadingId === doc.id ? '…' : '详情'}
              </button>
              <button
                type="button"
                className="kb-doc-reindex"
                disabled={batchBusy || reindexingDocumentId !== null}
                onClick={() => onReindexDocument(knowledgeBaseId, doc.id)}
                title="重新建立索引"
              >
                {reindexingDocumentId === doc.id ? '重建中' : '重建'}
              </button>
              <button
                type="button"
                className="kb-doc-remove"
                aria-label={`删除 ${doc.name}`}
                disabled={batchBusy}
                onClick={() => onRemoveDocument(knowledgeBaseId, doc.id)}
                title="删除文档"
              >
                <KnowledgeIcon name="x" size={16} />
              </button>
            </div>
          </div>
        )
      })}

      {filteredDocuments.length > 50 && (
        <div className="kb-document-pagination" aria-label="文档分页">
          <span>共 {filteredDocuments.length} 份文档</span>
          <div>
            <button
              type="button"
              disabled={visibleDocumentPage.page === 1}
              onClick={() => onPageChange(Math.max(1, visibleDocumentPage.page - 1))}
            >
              上一页
            </button>
            <strong>第 {visibleDocumentPage.page} / {visibleDocumentPage.pageCount} 页</strong>
            <button
              type="button"
              disabled={visibleDocumentPage.page === visibleDocumentPage.pageCount}
              onClick={() => onPageChange(Math.min(visibleDocumentPage.pageCount, visibleDocumentPage.page + 1))}
            >
              下一页
            </button>
          </div>
        </div>
      )}
    </>
  )
}

export default DocumentList
