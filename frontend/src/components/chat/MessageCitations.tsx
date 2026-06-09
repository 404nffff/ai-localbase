import React from 'react'
import type { ChatSourceMetadata } from '../../App'
import { chunkKindLabel } from '../knowledge/knowledgeLabels'

interface MessageCitationsProps {
  sources: ChatSourceMetadata[]
  onOpenCitationSource?: (source: ChatSourceMetadata) => void
}

const sourceIdentity = (source: ChatSourceMetadata, index: number) =>
  [
    source.toolName,
    source.knowledgeBaseId,
    source.documentId,
    source.chunkId,
    source.chunkIndex,
  ].filter(Boolean).join(':') || `source-${index}`

const normalizeSources = (sources?: ChatSourceMetadata[]) => {
  if (!sources || sources.length === 0) return []
  const seen = new Set<string>()
  return sources.filter((source, index) => {
    const key = sourceIdentity(source, index)
    if (seen.has(key)) return false
    seen.add(key)
    return Boolean(source.documentName || source.toolName || source.snippet)
  })
}

const sourceTypeLabel = (source: ChatSourceMetadata) => {
  if (source.toolName) return `工具：${source.toolName}`
  if (source.sourceType === 'structured-data') return '结构化数据'
  if (source.chunkKind) return chunkKindLabel(source.chunkKind)
  return '来源'
}

const sourceRankLabel = (source: ChatSourceMetadata, index: number) => {
  if (source.chunkIndex) return `#${source.chunkIndex}`
  return `#${index + 1}`
}

const scoreLabel = (score?: string) => {
  if (!score) return ''
  const value = Number(score)
  if (!Number.isFinite(value)) return ''
  return `分数 ${value.toFixed(4)}`
}

const citationConfidenceLabel = (value?: string) => {
  switch (value) {
    case 'high':
      return '强证据'
    case 'medium':
      return '中证据'
    case 'low':
      return '弱证据'
    default:
      return ''
  }
}

const MessageCitations: React.FC<MessageCitationsProps> = ({
  sources,
  onOpenCitationSource,
}) => {
  const visibleSources = normalizeSources(sources).slice(0, 6)
  if (visibleSources.length === 0) return null

  return (
    <details className="message-citations">
      <summary>
        <span>引用来源</span>
        <strong>{visibleSources.length}</strong>
      </summary>
      <div className="message-citation-list">
        {visibleSources.map((source, index) => (
          <article className="message-citation" key={sourceIdentity(source, index)}>
            <div className="message-citation-head">
              <strong>{source.documentName || source.toolName || '未知来源'}</strong>
              <span>{sourceTypeLabel(source)}</span>
              <span>{sourceRankLabel(source, index)}</span>
              {citationConfidenceLabel(source.citationConfidence) && (
                <span>{citationConfidenceLabel(source.citationConfidence)}</span>
              )}
              {scoreLabel(source.score) && <span>{scoreLabel(source.score)}</span>}
              {source.documentId && (
                <button
                  type="button"
                  onClick={() => onOpenCitationSource?.(source)}
                  disabled={!onOpenCitationSource}
                >
                  定位
                </button>
              )}
            </div>
            {source.snippet && <p>{source.snippet}</p>}
          </article>
        ))}
      </div>
    </details>
  )
}

export default MessageCitations
