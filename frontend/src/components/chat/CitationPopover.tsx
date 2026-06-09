import React, { useState } from 'react'
import type { ChatSourceMetadata } from '../../App'

interface CitationPopoverProps {
  source: ChatSourceMetadata
  isOpen: boolean
  onClose: () => void
  onNavigateToDocument?: (knowledgeBaseId: string, documentId: string, chunkId?: string) => void
}

const CitationPopover: React.FC<CitationPopoverProps> = ({
  source,
  isOpen,
  onClose,
  onNavigateToDocument,
}) => {
  const [copied, setCopied] = useState(false)

  if (!isOpen) return null

  const handleCopySnippet = async () => {
    if (!source.snippet) return
    try {
      await navigator.clipboard.writeText(source.snippet)
      setCopied(true)
      setTimeout(() => setCopied(false), 1500)
    } catch {
      // 忽略复制失败
    }
  }

  const handleNavigate = () => {
    if (source.knowledgeBaseId && source.documentId && onNavigateToDocument) {
      onNavigateToDocument(source.knowledgeBaseId, source.documentId, source.chunkId)
      onClose()
    }
  }

  return (
    <div className="citation-popover-overlay" onClick={onClose}>
      <div className="citation-popover" onClick={(e) => e.stopPropagation()}>
        <div className="citation-popover-header">
          <h3>引用来源详情</h3>
          <button
            type="button"
            className="citation-popover-close"
            onClick={onClose}
            aria-label="关闭"
          >
            ✕
          </button>
        </div>

        <div className="citation-popover-body">
          <div className="citation-field">
            <label>文档名称</label>
            <div className="citation-value">
              {source.documentName || source.toolName || '未知来源'}
            </div>
          </div>

          {source.chunkKind && (
            <div className="citation-field">
              <label>块类型</label>
              <div className="citation-value">{source.chunkKind}</div>
            </div>
          )}

          {source.chunkIndex && (
            <div className="citation-field">
              <label>块索引</label>
              <div className="citation-value">#{source.chunkIndex}</div>
            </div>
          )}

          {source.score && (
            <div className="citation-field">
              <label>相关度分数</label>
              <div className="citation-value">{Number(source.score).toFixed(4)}</div>
            </div>
          )}

          {source.citationConfidence && (
            <div className="citation-field">
              <label>证据强度</label>
              <div className="citation-value">
                {source.citationConfidence === 'high' && '强证据'}
                {source.citationConfidence === 'medium' && '中证据'}
                {source.citationConfidence === 'low' && '弱证据'}
              </div>
            </div>
          )}

          {source.snippet && (
            <div className="citation-field">
              <label>内容片段</label>
              <div className="citation-snippet">
                {source.snippet}
                <button
                  type="button"
                  className="citation-copy-btn"
                  onClick={() => {
                    void handleCopySnippet()
                  }}
                  title={copied ? '已复制' : '复制内容'}
                >
                  {copied ? '✓' : '⧉'}
                </button>
              </div>
            </div>
          )}
        </div>

        <div className="citation-popover-footer">
          {source.documentId && onNavigateToDocument && (
            <button
              type="button"
              className="citation-navigate-btn"
              onClick={handleNavigate}
            >
              跳转到文档详情
            </button>
          )}
          <button
            type="button"
            className="citation-close-btn"
            onClick={onClose}
          >
            关闭
          </button>
        </div>
      </div>
    </div>
  )
}

export default CitationPopover
