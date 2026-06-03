import React from 'react'
import type { KnowledgeBaseHealthResponse } from '../../services/api'
import { healthStatusLabel } from './knowledgeLabels'

interface KnowledgeHealthPanelProps {
  health?: KnowledgeBaseHealthResponse
  loading: boolean
  error: string
  onReindexDocument: (documentId: string) => void
  reindexingDocumentId: string | null
}

const KnowledgeHealthPanel: React.FC<KnowledgeHealthPanelProps> = ({
  health,
  loading,
  error,
  onReindexDocument,
  reindexingDocumentId,
}) => {
  const badge = health ? healthStatusLabel(health.status) : null
  const needsReindexDocuments = health?.documents.filter((item) => item.needsReindex) ?? []

  return (
    <section className="kb-health-panel">
      <div className="kb-panel-section-head">
        <div>
          <h3>索引健康度</h3>
          <p>
            {health?.metrics.lastIndexedAt
              ? `最近索引：${new Date(health.metrics.lastIndexedAt).toLocaleString('zh-CN')}`
              : '暂无索引时间'}
          </p>
        </div>
        {badge ? (
          <span className="kb-health-badge" style={{ color: badge.color, background: badge.bg }}>
            {badge.text} · {health?.score}
          </span>
        ) : (
          <span className="kb-health-badge kb-health-badge--loading">
            {loading ? '检查中' : '待检查'}
          </span>
        )}
      </div>

      {error && !loading && <div className="kb-health-error">{error}</div>}

      {health && (
        <>
          <div className="kb-health-stats">
            <div><span>文档</span><strong>{health.metrics.documentCount}</strong></div>
            <div><span>已索引</span><strong>{health.metrics.indexedCount}</strong></div>
            <div><span>失败</span><strong>{health.metrics.failedCount}</strong></div>
            <div><span>Chunks</span><strong>{health.metrics.chunkCount}</strong></div>
            <div><span>向量</span><strong>{health.metrics.vectorCount}</strong></div>
            <div><span>结构化</span><strong>{health.metrics.structuredRowCount}</strong></div>
          </div>

          <div className="kb-health-recommendations">
            {health.recommendations.map((item) => (
              <div key={item} className="kb-health-recommendation">{item}</div>
            ))}
          </div>

          {needsReindexDocuments.length > 0 && (
            <details className="kb-health-docs">
              <summary>查看需处理文档</summary>
              <div className="kb-health-doc-list">
                {needsReindexDocuments.map((item) => (
                  <div key={item.documentId} className="kb-health-doc-item">
                    <div>
                      <strong>{item.documentName}</strong>
                      <span>{item.recommendation || '建议检查索引状态'}</span>
                    </div>
                    <button
                      onClick={() => onReindexDocument(item.documentId)}
                      disabled={reindexingDocumentId === item.documentId}
                    >
                      {reindexingDocumentId === item.documentId ? '重建中' : '重建'}
                    </button>
                  </div>
                ))}
              </div>
            </details>
          )}
        </>
      )}
    </section>
  )
}

export default KnowledgeHealthPanel
