import React from 'react'
import type { RetrievalDebugResponse } from '../../services/api'
import { chunkKindLabel, structuredIntentLabel } from './knowledgeLabels'

interface RetrievalDebugPanelProps {
  scopeLabel: string
  query: string
  result: RetrievalDebugResponse | null
  error: string
  loading: boolean
  onQueryChange: (value: string) => void
  onRun: () => void
  onDownloadEvalCandidate: () => void
}

const RetrievalDebugPanel: React.FC<RetrievalDebugPanelProps> = ({
  scopeLabel,
  query,
  result,
  error,
  loading,
  onQueryChange,
  onRun,
  onDownloadEvalCandidate,
}) => (
  <section className="kb-retrieval-debug">
    <div className="kb-panel-section-head">
      <div>
        <h3>检索调试台</h3>
        <p>当前范围：{scopeLabel}</p>
      </div>
      <span className="kb-retrieval-mode">
        {result?.searchMode === 'hybrid' ? '混合检索' : '向量检索'}
      </span>
    </div>
    <div className="kb-retrieval-input-row">
      <input
        className="kb-retrieval-input"
        value={query}
        onChange={(event) => onQueryChange(event.target.value)}
        onKeyDown={(event) => {
          if (event.key === 'Enter') onRun()
        }}
        placeholder="输入一个问题，查看实际命中的 chunk"
      />
      <button className="kb-retrieval-run" onClick={onRun} disabled={loading}>
        {loading ? '检索中' : '运行'}
      </button>
    </div>

    {error && <div className="kb-retrieval-error">{error}</div>}

    {result && (
      <div className="kb-retrieval-result">
        <div className="kb-retrieval-summary">
          <span>{result.count} 个命中</span>
          <span>{result.elapsedMs} ms</span>
          <span>{result.lowConfidence ? '低置信' : '置信正常'}</span>
          <span>{result.deterministicUsed ? '确定性补全' : '向量优先'}</span>
          {structuredIntentLabel(result.structuredIntent) && (
            <span>
              {structuredIntentLabel(result.structuredIntent)}
              {result.targetField ? `：${result.targetField}` : ''}
            </span>
          )}
        </div>

        {result.evalCandidate && (
          <div className="kb-retrieval-eval">
            <div>
              <strong>低置信评测候选</strong>
              <p>当前问题可沉淀为后续检索评测样本，下载后建议人工复核答案片段。</p>
            </div>
            <button onClick={onDownloadEvalCandidate}>下载样本</button>
          </div>
        )}

        {result.contextPreview && (
          <details className="kb-retrieval-context">
            <summary>上下文预览</summary>
            <pre>{result.contextPreview}</pre>
          </details>
        )}

        <div className="kb-retrieval-hits">
          {result.items.length === 0 ? (
            <div className="kb-docs-empty">没有命中 chunk</div>
          ) : (
            result.items.map((item) => (
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
  </section>
)

export default RetrievalDebugPanel
