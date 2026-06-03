import React from 'react'
import type { RetrievalDebugResponse, RetrievalSearchMode } from '../../services/api'
import { chunkKindLabel, structuredIntentLabel } from './knowledgeLabels'

interface RetrievalDebugPanelProps {
  scopeLabel: string
  query: string
  searchMode: RetrievalSearchMode
  result: RetrievalDebugResponse | null
  error: string
  loading: boolean
  savingEvalCandidate: boolean
  evalCandidateSaveMessage: string
  onQueryChange: (value: string) => void
  onSearchModeChange: (value: RetrievalSearchMode) => void
  onRun: () => void
  onDownloadEvalCandidate: () => void
  onAddEvalCandidate: () => void
}

const searchModeOptions: Array<{ value: RetrievalSearchMode; label: string }> = [
  { value: 'auto', label: '自动' },
  { value: 'dense', label: '向量' },
  { value: 'hybrid', label: '混合' },
]

const resolvedSearchModeLabel = (mode?: string) => {
  if (mode === 'hybrid') return '混合检索'
  if (mode === 'dense') return '向量检索'
  return '等待检索'
}

const RetrievalDebugPanel: React.FC<RetrievalDebugPanelProps> = ({
  scopeLabel,
  query,
  searchMode,
  result,
  error,
  loading,
  savingEvalCandidate,
  evalCandidateSaveMessage,
  onQueryChange,
  onSearchModeChange,
  onRun,
  onDownloadEvalCandidate,
  onAddEvalCandidate,
}) => (
  <section className="kb-retrieval-debug">
    <div className="kb-panel-section-head">
      <div>
        <h3>检索调试台</h3>
        <p>当前范围：{scopeLabel}</p>
      </div>
      <div className="kb-retrieval-mode-block">
        <span className="kb-retrieval-mode">
          {resolvedSearchModeLabel(result?.searchMode)}
        </span>
        <div className="kb-retrieval-mode-tabs" role="tablist" aria-label="检索模式">
          {searchModeOptions.map((option) => (
            <button
              key={option.value}
              type="button"
              className={searchMode === option.value ? 'active' : ''}
              onClick={() => onSearchModeChange(option.value)}
              disabled={loading}
            >
              {option.label}
            </button>
          ))}
        </div>
      </div>
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
              {evalCandidateSaveMessage && <span>{evalCandidateSaveMessage}</span>}
            </div>
            <div className="kb-retrieval-eval-actions">
              <button onClick={onAddEvalCandidate} disabled={savingEvalCandidate}>
                {savingEvalCandidate ? '加入中' : '加入待审核'}
              </button>
              <button onClick={onDownloadEvalCandidate}>下载样本</button>
            </div>
          </div>
        )}

        {result.contextPreview && (
          <details className="kb-retrieval-context">
            <summary>上下文预览</summary>
            <pre>{result.contextPreview}</pre>
          </details>
        )}

        {result.trace && result.trace.length > 0 && (
          <details className="kb-retrieval-trace">
            <summary>检索处理说明</summary>
            <div>
              {result.trace.map((step, index) => (
                <span key={`${step.stage}-${index}`}>
                  {step.stage}：{step.reason || step.status}
                  {(step.inputCount || step.outputCount) ? `（${step.inputCount ?? '-'} -> ${step.outputCount ?? '-'}）` : ''}
                </span>
              ))}
            </div>
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
                {item.matchReasons && item.matchReasons.length > 0 && (
                  <div className="kb-retrieval-reasons">
                    {item.matchReasons.map((reason) => (
                      <span key={reason}>{reason}</span>
                    ))}
                  </div>
                )}
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
