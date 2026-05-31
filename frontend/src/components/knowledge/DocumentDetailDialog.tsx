import React from 'react'
import type { DocumentDetailResponse } from '../../services/api'
import { chunkKindLabel } from './knowledgeLabels'

interface DocumentDetailDialogProps {
  detail: DocumentDetailResponse | null
  error: string
  onClose: () => void
}

const DocumentDetailDialog: React.FC<DocumentDetailDialogProps> = ({
  detail,
  error,
  onClose,
}) => (
  <div className="kb-detail-backdrop" onClick={onClose}>
    <div className="kb-detail-dialog" onClick={(event) => event.stopPropagation()}>
      <div className="kb-detail-header">
        <div>
          <h3>{detail?.document.name ?? '文档详情'}</h3>
          {detail?.document.indexedAt && (
            <p>最近索引：{new Date(detail.document.indexedAt).toLocaleString('zh-CN')}</p>
          )}
        </div>
        <button className="kb-close-btn" onClick={onClose}>x</button>
      </div>

      {error ? (
        <div className="kb-detail-error">{error}</div>
      ) : detail && (
        <div className="kb-detail-body">
          <section className="kb-detail-section">
            <div className="kb-detail-stats">
              <div><span>原文字符</span><strong>{detail.diagnostics.rawContentChars}</strong></div>
              <div><span>分块数量</span><strong>{detail.diagnostics.chunkCount}</strong></div>
              <div><span>向量数量</span><strong>{detail.diagnostics.vectorCount}</strong></div>
              <div><span>摘要块</span><strong>{detail.diagnostics.summaryChunkCount}</strong></div>
              <div><span>数据行块</span><strong>{detail.diagnostics.structuredRowCount}</strong></div>
              <div><span>Qdrant</span><strong>{detail.diagnostics.qdrantEnabled ? '启用' : '未启用'}</strong></div>
            </div>
            {detail.document.indexError && (
              <div className="kb-detail-error">索引错误：{detail.document.indexError}</div>
            )}
          </section>

          <section className="kb-detail-section">
            <h4>摘要预览</h4>
            <pre className="kb-detail-pre">{detail.summary || '暂无摘要'}</pre>
          </section>

          <section className="kb-detail-section">
            <h4>
              原文预览
              {detail.diagnostics.rawContentTruncated && <span className="kb-detail-muted">已截断</span>}
            </h4>
            <pre className="kb-detail-pre">{detail.rawContent || '暂无可读取原文'}</pre>
          </section>

          <section className="kb-detail-section">
            <h4>
              Chunk 预览
              {detail.diagnostics.chunkPreviewTruncated && (
                <span className="kb-detail-muted">仅显示前 {detail.chunks.length} 个</span>
              )}
            </h4>
            <div className="kb-detail-chunks">
              {detail.chunks.length === 0 ? (
                <div className="kb-docs-empty">暂无 chunk</div>
              ) : (
                detail.chunks.map((chunk) => (
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
)

export default DocumentDetailDialog
