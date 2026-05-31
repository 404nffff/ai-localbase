import React from 'react'
import type { DocumentItem } from '../../App'
import { documentStatusLabel } from './knowledgeLabels'

interface DocumentListProps {
  documents: DocumentItem[]
  selectedDocumentId: string | null
  documentDetailLoadingId: string | null
  reindexingDocumentId: string | null
  onSelectDocument: (documentId: string | null) => void
  onOpenDocumentDetail: (documentId: string) => void
  onReindexDocument: (documentId: string) => void
  onRemoveDocument: (documentId: string) => void
}

const DocumentList: React.FC<DocumentListProps> = ({
  documents,
  selectedDocumentId,
  documentDetailLoadingId,
  reindexingDocumentId,
  onSelectDocument,
  onOpenDocumentDetail,
  onReindexDocument,
  onRemoveDocument,
}) => (
  <section className="kb-docs-panel">
    <div className="kb-panel-section-head">
      <div>
        <h3>文档</h3>
        <p>{documents.length} 份文档 · 可切换查询范围</p>
      </div>
    </div>

    <div className="kb-scope-bar">
      <button
        className={`kb-scope-btn${selectedDocumentId === null ? ' kb-scope-btn--active' : ''}`}
        onClick={() => onSelectDocument(null)}
      >
        全部文档
      </button>
      {documents.map((document) => (
        <button
          key={document.id}
          className={`kb-scope-btn${selectedDocumentId === document.id ? ' kb-scope-btn--active' : ''}`}
          onClick={() => onSelectDocument(document.id)}
        >
          {document.name}
        </button>
      ))}
    </div>

    <div className="kb-docs">
      {documents.length === 0 ? (
        <div className="kb-docs-empty">
          <span>暂无文档，点击上传添加文件</span>
        </div>
      ) : (
        documents.map((document) => {
          const badge = documentStatusLabel(document.status)
          return (
            <div
              key={document.id}
              className={`kb-doc-item${selectedDocumentId === document.id ? ' kb-doc-item--active' : ''}`}
            >
              <button className="kb-doc-main" onClick={() => onSelectDocument(document.id)}>
                <div className="kb-doc-top">
                  <span className="kb-doc-name">{document.name}</span>
                  <span className="kb-doc-badge" style={{ color: badge.color, background: badge.bg }}>
                    {badge.text}
                  </span>
                </div>
                {document.contentPreview && <p className="kb-doc-preview">{document.contentPreview}</p>}
                <div className="kb-doc-meta">
                  <span>{document.sizeLabel}</span>
                  {typeof document.chunkCount === 'number' && (
                    <>
                      <span>·</span>
                      <span>{document.chunkCount} chunks</span>
                    </>
                  )}
                  <span>·</span>
                  <span>{new Date(document.uploadedAt).toLocaleDateString('zh-CN')}</span>
                </div>
              </button>
              <div className="kb-doc-actions">
                <button
                  className="kb-doc-action"
                  onClick={() => onOpenDocumentDetail(document.id)}
                  disabled={documentDetailLoadingId === document.id}
                  title="查看索引详情"
                >
                  {documentDetailLoadingId === document.id ? '加载' : '详情'}
                </button>
                <button
                  className="kb-doc-action"
                  onClick={() => onReindexDocument(document.id)}
                  disabled={reindexingDocumentId === document.id}
                  title="重新解析并重建索引"
                >
                  {reindexingDocumentId === document.id ? '重建中' : '重建'}
                </button>
                <button className="kb-doc-remove" onClick={() => onRemoveDocument(document.id)} title="删除文档">
                  x
                </button>
              </div>
            </div>
          )
        })
      )}
    </div>
  </section>
)

export default DocumentList
