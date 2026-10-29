import { useRef, useState } from 'react'
import type { ChatMessageMetadata, DocumentDetailResponse } from '../../App'
import { fetchDocumentDetail } from '../../services/api'
import DocumentDetailDialog from '../knowledge/DocumentDetailDialog'

/** 普通文档引用展示原始片段并打开对应分块；Wiki 引用仍由消息卡片维护。 */
export default function MessageCitations({ metadata }: { metadata?: ChatMessageMetadata }) {
  const requestID = useRef(0)
  const [detail, setDetail] = useState<DocumentDetailResponse | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const sources = (metadata?.sources ?? []).filter((source) => !source.pageId && source.documentId && source.knowledgeBaseId)

  /** 按返回的真实库、文档和分块标识定位；失败明确展示，不用预览冒充详情。 */
  async function openSource(source: NonNullable<ChatMessageMetadata['sources']>[number]) {
    if (!source.knowledgeBaseId || !source.documentId) return
    const currentRequest = ++requestID.current
    setLoading(true)
    setError(null)
    setDetail(null)
    try {
      const result = await fetchDocumentDetail(source.knowledgeBaseId, source.documentId, source.chunkId)
      if (currentRequest !== requestID.current) return
      // 引用分块放在预览首位，即使它位于默认截断范围之外也能直接定位。
      setDetail({ ...result, chunks: [...result.chunks].sort((a, b) => Number(b.id === source.chunkId) - Number(a.id === source.chunkId)) })
    } catch (reason) {
      if (currentRequest !== requestID.current) return
      setError(reason instanceof Error ? reason.message : '读取引用失败')
    } finally {
      if (currentRequest === requestID.current) setLoading(false)
    }
  }

  if (sources.length === 0 && !metadata?.citationSupport) return null
  return <section className="message-citations" aria-label="引用证据">
    {metadata?.citationSupport && <p role="status">词法辅助检查：{metadata.citationSupport.summary}</p>}
    {sources.map((source, index) => <details key={`${source.documentId}-${source.chunkId ?? index}`}>
      <summary>{source.documentName || source.documentId}{source.chunkIndex ? ` · 第 ${source.chunkIndex} 块` : ''}</summary>
      <blockquote>{source.citationSnippet || source.snippet || source.quote || '该来源未返回片段，请打开文档核对。'}</blockquote>
      <button type="button" disabled={loading} onClick={() => void openSource(source)}>查看原文与分块</button>
    </details>)}
    <DocumentDetailDialog detail={detail} loading={loading} error={error} reindexing={false} onClose={() => { requestID.current++; setLoading(false); setDetail(null); setError(null) }} />
  </section>
}
