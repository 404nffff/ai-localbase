import React from 'react'
import type { DocumentDetailResponse } from '../../App'
import MarkdownRenderer from '../chat/MarkdownRenderer'
import KnowledgeIcon from './KnowledgeIcon'
import { formatDocumentPreviewText, shouldUseRawDocumentPreview } from './documentPreviewText'

interface DocumentDetailDialogProps {
  detail: DocumentDetailResponse | null
  error: string | null
  loading: boolean
  onClose: () => void
}

// 文档详情只展示后端返回的截断内容，避免在浏览器加载完整超大文件。
const DocumentDetailDialog: React.FC<DocumentDetailDialogProps> = ({
  detail,
  error,
  loading,
  onClose,
}) => {
  if (!loading && !error && !detail) return null

  const rawMode = detail ? shouldUseRawDocumentPreview(detail.document.name) : false
  const formattedContent = detail ? formatDocumentPreviewText(detail.rawContent) : ''

  return (
    <div className="document-detail-backdrop" onClick={onClose}>
      <section
        className="document-detail-dialog"
        aria-label="文档详情"
        onClick={(event) => event.stopPropagation()}
      >
        <header className="document-detail-header">
          <div>
            <span>文档详情</span>
            <h3>{detail?.document.name ?? '正在加载'}</h3>
          </div>
          <button type="button" onClick={onClose} aria-label="关闭文档详情">
            <KnowledgeIcon name="x" size={18} />
          </button>
        </header>

        <div className="document-detail-body">
          {loading && <div className="document-detail-state">正在读取文档详情…</div>}
          {error && <div className="document-detail-state document-detail-state--error">{error}</div>}

          {detail && !loading && (
            <>
              <div className="document-detail-metrics">
                <div><span>原文字符</span><strong>{detail.diagnostics.rawContentChars}</strong></div>
                <div><span>Chunks</span><strong>{detail.diagnostics.chunkCount}</strong></div>
                <div><span>向量</span><strong>{detail.diagnostics.vectorCount}</strong></div>
                <div><span>结构化行</span><strong>{detail.diagnostics.structuredRowCount}</strong></div>
              </div>

              {detail.summary && (
                <section className="document-detail-section">
                  <h4>索引摘要</h4>
                  <MarkdownRenderer content={detail.summary} />
                </section>
              )}

              <section className="document-detail-section">
                <h4>原文预览</h4>
                {rawMode ? (
                  <pre className="document-detail-raw">{detail.rawContent}</pre>
                ) : (
                  <div className="document-detail-markdown">
                    <MarkdownRenderer content={formattedContent} />
                  </div>
                )}
                {detail.diagnostics.rawContentTruncated && <small>原文过长，当前仅展示前 20000 个字符。</small>}
              </section>

              <section className="document-detail-section">
                <h4>Chunk 预览</h4>
                <div className="document-chunk-list">
                  {detail.chunks.map((chunk) => (
                    <article key={chunk.id}>
                      <div><strong>#{chunk.index + 1}</strong><span>{chunk.kind || 'text'}</span></div>
                      <p>{chunk.text}</p>
                    </article>
                  ))}
                  {detail.chunks.length === 0 && <div className="document-detail-state">暂无 chunk。</div>}
                </div>
                {detail.diagnostics.chunkPreviewTruncated && <small>仅展示前 30 个 chunk。</small>}
              </section>
            </>
          )}
        </div>
      </section>
    </div>
  )
}

export default DocumentDetailDialog
