import React, { useEffect, useRef } from 'react'
import type { DocumentDetailResponse } from '../../App'
import MarkdownRenderer from '../chat/LazyMarkdownRenderer'
import KnowledgeIcon from './KnowledgeIcon'
import { shouldUseRawDocumentPreview } from './documentPreviewText'

interface DocumentDetailDialogProps {
  detail: DocumentDetailResponse | null
  error: string | null
  loading: boolean
  reindexing: boolean
  reindexMessage?: string | null
  onReindex?: () => void
  onClose: () => void
}

/** 以原生模态展示后端截断的详情与原文，保留 Markdown，并通过父组件执行重建和错误反馈。 */
const DocumentDetailDialog: React.FC<DocumentDetailDialogProps> = ({
  detail,
  error,
  loading,
  reindexing,
  reindexMessage,
  onReindex,
  onClose,
}) => {
  const dialog = useRef<HTMLDialogElement>(null)
  const open = Boolean(loading || error || detail)
  useEffect(() => {
    // 原生模态约束键盘焦点，关闭时由父组件清除详情状态并卸载窗口。
    if (open && !dialog.current?.open) {
      dialog.current?.showModal()
    }
  }, [open])
  if (!loading && !error && !detail) return null

  const rawMode = detail ? shouldUseRawDocumentPreview(detail.document.name) : false

  return (
      <dialog
        ref={dialog}
        className="document-detail-dialog"
        aria-label="文档详情"
        onCancel={onClose}
        onClick={(event) => event.stopPropagation()}
      >
        <header className="document-detail-header">
          <div>
            <span>文档详情</span>
            <h1>{detail?.document.name ?? '正在加载'}</h1>
          </div>
          <div className="document-detail-header-actions">
            {detail && onReindex && (
              <button
                type="button"
                className="kb-doc-reindex"
                disabled={reindexing || loading}
                onClick={() => onReindex()}
                title="使用现有源文件重建索引"
              >
                {reindexing ? '重建中' : '重建索引'}
              </button>
            )}
            <button type="button" onClick={onClose} aria-label="关闭文档详情">
              <KnowledgeIcon name="x" size={18} />
            </button>
          </div>
        </header>

        {/* 模态内正文可独立键盘滚动；产品标题从一级、二级组织，原文标题保持原样。 */}
        <div className="document-detail-body" tabIndex={0} role="region" aria-label="文档详情内容">
          {loading && <div className="document-detail-state">正在读取文档详情…</div>}
          {error && <div className="document-detail-state document-detail-state--error">{error}</div>}

          {reindexMessage && (
            <div className={`document-detail-state${reindexMessage.includes('失败') ? ' document-detail-state--error' : ''}`}>
              {reindexMessage}
            </div>
          )}

          {detail && !loading && (
            <>
              <div className="document-detail-metrics">
                <div><span>原文字符</span><strong>{detail.diagnostics.rawContentChars}</strong></div>
                <div><span>Chunks</span><strong>{detail.diagnostics.chunkCount}</strong></div>
                <div><span>实际向量</span><strong>{detail.diagnostics.vectorCountSource === 'actual' ? detail.diagnostics.vectorCount : detail.diagnostics.vectorCountSource === 'not_applicable' ? '不适用' : '未知'}</strong><small>{detail.diagnostics.vectorCountStatus && detail.diagnostics.vectorCountStatus !== 'ok' && detail.diagnostics.vectorCountStatus !== 'not_applicable' ? (detail.diagnostics.vectorCountStatus === 'collection_missing' ? '集合不存在' : detail.diagnostics.vectorCountStatus === 'dimension_mismatch' ? '维度不一致' : '查询失败') : ''}</small></div>
                <div><span>结构化行</span><strong>{detail.diagnostics.structuredRowCount}</strong></div>
              </div>

              {detail.summary && (
                <section className="document-detail-section">
                  <h2>索引摘要</h2>
                  <MarkdownRenderer content={detail.summary} />
                </section>
              )}

              <section className="document-detail-section">
                <h2>原文预览</h2>
                {rawMode ? (
                  <pre className="document-detail-raw">{detail.rawContent}</pre>
                ) : (
                  <div className="document-detail-markdown">
                    <MarkdownRenderer content={detail.rawContent} preserveSource />
                  </div>
                )}
                {detail.diagnostics.rawContentTruncated && <small>原文过长，当前仅展示前 20000 个字符。</small>}
              </section>

              <section className="document-detail-section">
                <h2>Chunk 预览</h2>
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
      </dialog>
  )
}

export default DocumentDetailDialog
