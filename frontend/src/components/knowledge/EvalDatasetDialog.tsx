import React, { useMemo, useState } from 'react'
import type {
  EvalDatasetDetail,
  EvalGroundTruthCase,
  GenerateEvalDatasetResponse,
  RetrievalSearchMode,
  RunEvalDatasetResponse,
} from '../../services/api'

type EvalDatasetDialogDataset = GenerateEvalDatasetResponse | EvalDatasetDetail

interface EvalDatasetDialogProps {
  dataset: EvalDatasetDialogDataset
  scopeName: string
  onClose: () => void
  onUpdateItem?: (
    datasetId: string,
    itemId: string,
    item: EvalGroundTruthCase,
  ) => Promise<EvalGroundTruthCase>
  onDeleteItem?: (datasetId: string, itemId: string) => Promise<void>
  onRun?: (datasetId: string, searchMode?: RetrievalSearchMode) => Promise<RunEvalDatasetResponse>
}

interface EvalItemDraft {
  question: string
  answer: string
  snippets: string
  answerType: string
  difficulty: string
  reviewStatus: string
  disabled: boolean
  notes: string
}

interface EvalComparisonReport {
  dense: RunEvalDatasetResponse
  hybrid: RunEvalDatasetResponse
}

type EvalComparisonCaseLabel = '混合修复' | '混合退步' | '排名提升' | '排名下降' | '均未命中' | '保持稳定'

interface EvalComparisonCase {
  caseId: string
  question: string
  label: EvalComparisonCaseLabel
  tone: 'good' | 'bad' | 'neutral'
  denseLabel: string
  hybridLabel: string
  expectedAnswer: string
}

type HybridRecommendationStatus = 'enable_hybrid' | 'keep_dense' | 'need_more_samples'

interface HybridRecommendation {
  status: HybridRecommendationStatus
  title: string
  tone: 'good' | 'bad' | 'neutral'
  reasons: string[]
}

const difficultyLabel: Record<string, string> = {
  easy: '简单',
  medium: '中等',
  hard: '困难',
}

const answerTypeLabel: Record<string, string> = {
  numeric: '数值',
  listing: '列表',
  process: '流程',
  extractive: '摘录',
  'retrieval-debug-candidate': '调试候选',
}

const reviewStatusLabel: Record<string, string> = {
  pending: '待审核',
  approved: '已审核',
}

const countBy = (items: EvalGroundTruthCase[], key: keyof EvalGroundTruthCase) =>
  items.reduce<Record<string, number>>((acc, item) => {
    const value = String(item[key] || 'unknown')
    acc[value] = (acc[value] ?? 0) + 1
    return acc
  }, {})

const formatStats = (stats: Record<string, number>, labels: Record<string, string>) =>
  Object.entries(stats)
    .sort((left, right) => right[1] - left[1])
    .map(([key, count]) => `${labels[key] ?? key} ${count}`)
    .join(' · ')

type LooseEvalGroundTruthCase = EvalGroundTruthCase & {
  Question?: string
  Answer?: string
  answerSnippets?: string[]
  AnswerSnippets?: string[]
}

const getEvalQuestion = (item: EvalGroundTruthCase) => {
  const looseItem = item as LooseEvalGroundTruthCase
  return looseItem.question || looseItem.Question || '（问题为空）'
}

const getEvalAnswer = (item: EvalGroundTruthCase) => {
  const looseItem = item as LooseEvalGroundTruthCase
  return looseItem.answer || looseItem.Answer || '（答案为空）'
}

const getEvalSnippets = (item: EvalGroundTruthCase) => {
  const looseItem = item as LooseEvalGroundTruthCase
  return looseItem.answer_snippets || looseItem.answerSnippets || looseItem.AnswerSnippets || []
}

const getSavedDatasetId = (dataset: EvalDatasetDialogDataset) => (
  ('datasetId' in dataset && dataset.datasetId) || ('id' in dataset ? dataset.id : '')
)

const lineList = (value: string) => (
  value
    .split('\n')
    .map((item) => item.trim())
    .filter(Boolean)
)

const draftFromItem = (item: EvalGroundTruthCase): EvalItemDraft => ({
  question: getEvalQuestion(item),
  answer: getEvalAnswer(item),
  snippets: getEvalSnippets(item).join('\n'),
  answerType: item.answer_type || 'extractive',
  difficulty: item.difficulty || 'medium',
  reviewStatus: item.review_status || 'approved',
  disabled: Boolean(item.disabled),
  notes: item.notes || '',
})

const itemFromDraft = (item: EvalGroundTruthCase, draft: EvalItemDraft): EvalGroundTruthCase => ({
  ...item,
  question: draft.question.trim(),
  answer: draft.answer.trim(),
  answer_snippets: lineList(draft.snippets),
  answer_type: draft.answerType,
  difficulty: draft.difficulty,
  review_status: draft.reviewStatus,
  disabled: draft.disabled,
  notes: draft.notes.trim() || undefined,
})

const formatPercent = (value: number) => `${(value * 100).toFixed(1)}%`

const formatSignedPercent = (value: number) => `${value >= 0 ? '+' : ''}${formatPercent(value)}`

const formatSignedDecimal = (value: number) => `${value >= 0 ? '+' : ''}${value.toFixed(3)}`

const formatSignedInteger = (value: number) => `${value >= 0 ? '+' : ''}${value}`

const searchModeLabel = (mode?: string) => {
  if (mode === 'hybrid') return '混合检索'
  if (mode === 'dense') return '向量检索'
  return '自动'
}

const compareTone = (delta: number, higherIsBetter = true) => {
  if (delta === 0) return 'neutral'
  const improved = higherIsBetter ? delta > 0 : delta < 0
  return improved ? 'good' : 'bad'
}

const formatLatencyGrowth = (value: number) => {
  if (!Number.isFinite(value)) return '无法计算'
  return formatSignedPercent(value)
}

const evalCaseRankLabel = (hit: boolean, rank: number) => (hit && rank > 0 ? `命中 #${rank}` : '未命中')

const evalComparisonCaseLabel = (
  denseCase: RunEvalDatasetResponse['cases'][number],
  hybridCase: RunEvalDatasetResponse['cases'][number],
): { label: EvalComparisonCaseLabel; tone: EvalComparisonCase['tone'] } => {
  if (!denseCase.hit && hybridCase.hit) {
    return { label: '混合修复', tone: 'good' as const }
  }
  if (denseCase.hit && !hybridCase.hit) {
    return { label: '混合退步', tone: 'bad' as const }
  }
  if (denseCase.hit && hybridCase.hit && hybridCase.hitRank < denseCase.hitRank) {
    return { label: '排名提升', tone: 'good' as const }
  }
  if (denseCase.hit && hybridCase.hit && hybridCase.hitRank > denseCase.hitRank) {
    return { label: '排名下降', tone: 'bad' as const }
  }
  if (!denseCase.hit && !hybridCase.hit) {
    return { label: '均未命中', tone: 'neutral' as const }
  }
  return { label: '保持稳定', tone: 'neutral' as const }
}

const buildHybridRecommendation = (
  comparison: EvalComparisonReport,
  caseStats: {
    fixed: number
    regressed: number
    rankImproved: number
    rankWorse: number
  },
): HybridRecommendation => {
  const denseMetrics = comparison.dense.metrics
  const hybridMetrics = comparison.hybrid.metrics
  const totalCases = Math.min(denseMetrics.totalCases, hybridMetrics.totalCases)
  const hitRateDelta = hybridMetrics.hitRate - denseMetrics.hitRate
  const mrrDelta = hybridMetrics.mrr - denseMetrics.mrr
  const lowConfidenceDelta = hybridMetrics.lowConfidence - denseMetrics.lowConfidence
  const latencyDelta = hybridMetrics.latencyP95Ms - denseMetrics.latencyP95Ms
  const latencyGrowth = denseMetrics.latencyP95Ms > 0
    ? latencyDelta / denseMetrics.latencyP95Ms
    : (latencyDelta > 0 ? Number.POSITIVE_INFINITY : 0)
  const blockerReasons: string[] = []
  const positiveReasons: string[] = []

  if (totalCases < 5) {
    return {
      status: 'need_more_samples',
      title: '样本不足，先不要调整默认策略',
      tone: 'neutral',
      reasons: [
        `当前只覆盖 ${totalCases} 条启用用例，建议至少 5 条后再判断默认模式。`,
        '可以继续从低置信调试结果、关键词样本和跨文档样本中补充评估集。',
      ],
    }
  }

  if (hitRateDelta < 0) {
    blockerReasons.push(`Hit Rate 下降 ${formatPercent(Math.abs(hitRateDelta))}`)
  } else if (hitRateDelta > 0) {
    positiveReasons.push(`Hit Rate 提升 ${formatPercent(hitRateDelta)}`)
  } else {
    positiveReasons.push('Hit Rate 未低于向量检索')
  }

  if (mrrDelta < 0) {
    blockerReasons.push(`MRR 下降 ${Math.abs(mrrDelta).toFixed(3)}`)
  } else if (mrrDelta > 0) {
    positiveReasons.push(`MRR 提升 ${mrrDelta.toFixed(3)}`)
  } else {
    positiveReasons.push('MRR 保持稳定')
  }

  if (lowConfidenceDelta > 0) {
    blockerReasons.push(`低置信用例增加 ${lowConfidenceDelta}`)
  } else if (lowConfidenceDelta < 0) {
    positiveReasons.push(`低置信用例减少 ${Math.abs(lowConfidenceDelta)}`)
  } else {
    positiveReasons.push('低置信数量未增加')
  }

  if (latencyGrowth > 0.3) {
    blockerReasons.push(`P95 延迟增长 ${formatLatencyGrowth(latencyGrowth)}，超过 30% 阈值`)
  } else {
    positiveReasons.push(`P95 延迟增长 ${formatLatencyGrowth(latencyGrowth)}，处于 30% 阈值内`)
  }

  if (caseStats.regressed >= caseStats.fixed) {
    blockerReasons.push(`混合修复 ${caseStats.fixed} 条，退步 ${caseStats.regressed} 条，修复未超过退步`)
  } else {
    positiveReasons.push(`混合修复 ${caseStats.fixed} 条，退步 ${caseStats.regressed} 条`)
  }

  if (blockerReasons.length > 0) {
    return {
      status: 'keep_dense',
      title: '建议暂时保持向量检索默认策略',
      tone: 'bad',
      reasons: blockerReasons,
    }
  }

  if (caseStats.fixed > caseStats.regressed) {
    return {
      status: 'enable_hybrid',
      title: '建议将混合检索作为默认候选策略',
      tone: 'good',
      reasons: positiveReasons,
    }
  }

  return {
    status: 'need_more_samples',
    title: '结果稳定但收益不明显，继续观察',
    tone: 'neutral',
    reasons: [
      ...positiveReasons.slice(0, 3),
      '当前没有明确的修复收益，建议补充更多真实问题后再开启默认策略。',
    ],
  }
}

const downloadEvalDataset = (dataset: EvalDatasetDialogDataset, enabledOnly: boolean) => {
  const items = enabledOnly ? dataset.items.filter((item) => !item.disabled) : dataset.items
  const blob = new Blob([JSON.stringify(items, null, 2)], {
    type: 'application/json;charset=utf-8',
  })
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  const timestamp = new Date().toISOString().slice(0, 19).replace(/[-:T]/g, '')
  const scope = dataset.documentId || dataset.knowledgeBaseId || 'all'
  const suffix = enabledOnly ? 'enabled' : 'all'
  link.href = url
  link.download = `ground_truth_${scope}_${suffix}_${timestamp}.json`
  document.body.appendChild(link)
  link.click()
  link.remove()
  URL.revokeObjectURL(url)
}

const EvalDatasetDialog: React.FC<EvalDatasetDialogProps> = ({
  dataset,
  scopeName,
  onClose,
  onUpdateItem,
  onDeleteItem,
  onRun,
}) => {
  const [editingItemId, setEditingItemId] = useState<string | null>(null)
  const [draft, setDraft] = useState<EvalItemDraft | null>(null)
  const [savingItemId, setSavingItemId] = useState<string | null>(null)
  const [deletingItemId, setDeletingItemId] = useState<string | null>(null)
  const [running, setRunning] = useState(false)
  const [comparing, setComparing] = useState(false)
  const [evalSearchMode, setEvalSearchMode] = useState<RetrievalSearchMode>('auto')
  const [evalRun, setEvalRun] = useState<RunEvalDatasetResponse | null>(null)
  const [evalComparison, setEvalComparison] = useState<EvalComparisonReport | null>(null)
  const [actionError, setActionError] = useState('')

  const datasetId = getSavedDatasetId(dataset)
  const editable = Boolean(datasetId && onUpdateItem && onDeleteItem)
  const previewItems = dataset.items
  const enabledCount = dataset.items.filter((item) => !item.disabled).length
  const answerTypeStats = useMemo(
    () => formatStats(countBy(dataset.items, 'answer_type'), answerTypeLabel),
    [dataset.items],
  )
  const difficultyStats = useMemo(
    () => formatStats(countBy(dataset.items, 'difficulty'), difficultyLabel),
    [dataset.items],
  )
  const comparisonRows = useMemo(() => {
    if (!evalComparison) return []
    const { dense, hybrid } = evalComparison
    return [
      {
        label: 'Hit Rate',
        dense: formatPercent(dense.metrics.hitRate),
        hybrid: formatPercent(hybrid.metrics.hitRate),
        delta: formatSignedPercent(hybrid.metrics.hitRate - dense.metrics.hitRate),
        tone: compareTone(hybrid.metrics.hitRate - dense.metrics.hitRate),
      },
      {
        label: 'MRR',
        dense: dense.metrics.mrr.toFixed(3),
        hybrid: hybrid.metrics.mrr.toFixed(3),
        delta: formatSignedDecimal(hybrid.metrics.mrr - dense.metrics.mrr),
        tone: compareTone(hybrid.metrics.mrr - dense.metrics.mrr),
      },
      {
        label: '低置信',
        dense: String(dense.metrics.lowConfidence),
        hybrid: String(hybrid.metrics.lowConfidence),
        delta: formatSignedInteger(hybrid.metrics.lowConfidence - dense.metrics.lowConfidence),
        tone: compareTone(hybrid.metrics.lowConfidence - dense.metrics.lowConfidence, false),
      },
      {
        label: '检索 P95',
        dense: `${dense.metrics.latencyP95Ms}ms`,
        hybrid: `${hybrid.metrics.latencyP95Ms}ms`,
        delta: `${formatSignedInteger(hybrid.metrics.latencyP95Ms - dense.metrics.latencyP95Ms)}ms`,
        tone: compareTone(hybrid.metrics.latencyP95Ms - dense.metrics.latencyP95Ms, false),
      },
    ]
  }, [evalComparison])
  const comparisonCases = useMemo(() => {
    if (!evalComparison) return []
    const denseByCaseId = new Map(evalComparison.dense.cases.map((item) => [item.caseId, item]))
    return evalComparison.hybrid.cases
      .map<EvalComparisonCase | null>((hybridCase) => {
        const denseCase = denseByCaseId.get(hybridCase.caseId)
        if (!denseCase) return null
        const result = evalComparisonCaseLabel(denseCase, hybridCase)
        return {
          caseId: hybridCase.caseId,
          question: hybridCase.question,
          label: result.label,
          tone: result.tone,
          denseLabel: evalCaseRankLabel(denseCase.hit, denseCase.hitRank),
          hybridLabel: evalCaseRankLabel(hybridCase.hit, hybridCase.hitRank),
          expectedAnswer: hybridCase.expectedAnswer || denseCase.expectedAnswer,
        }
      })
      .filter((item): item is EvalComparisonCase => Boolean(item))
  }, [evalComparison])
  const comparisonCaseStats = useMemo(() => ({
    fixed: comparisonCases.filter((item) => item.label === '混合修复').length,
    regressed: comparisonCases.filter((item) => item.label === '混合退步').length,
    rankImproved: comparisonCases.filter((item) => item.label === '排名提升').length,
    rankWorse: comparisonCases.filter((item) => item.label === '排名下降').length,
  }), [comparisonCases])
  const hybridRecommendation = useMemo(() => (
    evalComparison ? buildHybridRecommendation(evalComparison, comparisonCaseStats) : null
  ), [evalComparison, comparisonCaseStats])
  const visibleComparisonCases = useMemo(() => (
    comparisonCases
      .filter((item) => item.label !== '保持稳定')
      .sort((left, right) => {
        const priority: Record<EvalComparisonCase['label'], number> = {
          混合修复: 0,
          混合退步: 1,
          排名提升: 2,
          排名下降: 3,
          均未命中: 4,
          保持稳定: 5,
        }
        return priority[left.label] - priority[right.label]
      })
      .slice(0, 8)
  ), [comparisonCases])

  const startEditing = (item: EvalGroundTruthCase) => {
    setActionError('')
    setEditingItemId(item.id)
    setDraft(draftFromItem(item))
  }

  const updateDraft = <K extends keyof EvalItemDraft>(key: K, value: EvalItemDraft[K]) => {
    setDraft((prev) => prev ? { ...prev, [key]: value } : prev)
  }

  const saveItem = async (item: EvalGroundTruthCase, nextDraft: EvalItemDraft) => {
    if (!datasetId || !onUpdateItem) return
    const nextItem = itemFromDraft(item, nextDraft)
    setSavingItemId(item.id)
    setActionError('')
    try {
      await onUpdateItem(datasetId, item.id, nextItem)
      setEditingItemId(null)
      setDraft(null)
    } catch (error) {
      setActionError(error instanceof Error ? error.message : '更新评估样本失败')
    } finally {
      setSavingItemId(null)
    }
  }

  const quickUpdateItem = async (item: EvalGroundTruthCase, patch: Partial<EvalGroundTruthCase>) => {
    await saveItem(item, draftFromItem({ ...item, ...patch }))
  }

  const deleteItem = async (item: EvalGroundTruthCase) => {
    if (!datasetId || !onDeleteItem) return
    if (!window.confirm(`确认删除样本「${getEvalQuestion(item)}」？`)) return
    setDeletingItemId(item.id)
    setActionError('')
    try {
      await onDeleteItem(datasetId, item.id)
      if (editingItemId === item.id) {
        setEditingItemId(null)
        setDraft(null)
      }
    } catch (error) {
      setActionError(error instanceof Error ? error.message : '删除评估样本失败')
    } finally {
      setDeletingItemId(null)
    }
  }

  const runDataset = async () => {
    if (!datasetId || !onRun) return
    setRunning(true)
    setActionError('')
    try {
      const report = await onRun(datasetId, evalSearchMode)
      setEvalRun(report)
    } catch (error) {
      setActionError(error instanceof Error ? error.message : '运行评估失败')
    } finally {
      setRunning(false)
    }
  }

  const runComparison = async () => {
    if (!datasetId || !onRun) return
    setComparing(true)
    setActionError('')
    setEvalComparison(null)
    try {
      const denseReport = await onRun(datasetId, 'dense')
      const hybridReport = await onRun(datasetId, 'hybrid')
      setEvalComparison({
        dense: denseReport,
        hybrid: hybridReport,
      })
      setEvalRun(hybridReport)
    } catch (error) {
      setActionError(error instanceof Error ? error.message : '运行检索对比失败')
    } finally {
      setComparing(false)
    }
  }

  const reportCases = evalRun?.cases ?? []
  const issueCases = reportCases.filter((item) => !item.hit || item.lowConfidence || item.error)
  const actionBusy = running || comparing

  return (
    <div className="kb-dialog-backdrop" onClick={onClose}>
      <div className="kb-eval-dialog" onClick={(event) => event.stopPropagation()}>
        <header className="kb-eval-dialog-head">
          <div>
            <span>评估集预览</span>
            <h3>{scopeName || dataset.knowledgeBaseId || '当前知识库'}</h3>
          </div>
          <button className="kb-close-btn" onClick={onClose} title="关闭">x</button>
        </header>

        <section className="kb-eval-summary-grid">
          <div>
            <strong>{dataset.count}</strong>
            <span>评估用例</span>
          </div>
          <div>
            <strong>{enabledCount}</strong>
            <span>已启用</span>
          </div>
          <div>
            <strong>{answerTypeStats || '-'}</strong>
            <span>题型分布</span>
          </div>
          <div>
            <strong>{difficultyStats || '-'}</strong>
            <span>难度分布</span>
          </div>
        </section>

        {actionError && <div className="kb-eval-dialog-error">{actionError}</div>}

        {evalRun && (
          <section className="kb-eval-run-report">
            <div className="kb-eval-run-head">
              <div>
                <span>最近一次评估</span>
                <strong>{evalRun.runId}</strong>
              </div>
              <span>
                {searchModeLabel(evalRun.searchMode)} · {new Date(evalRun.startedAt).toLocaleString()} · {evalRun.elapsedMs}ms
              </span>
            </div>
            <div className="kb-eval-run-metrics">
              <div>
                <strong>{formatPercent(evalRun.metrics.hitRate)}</strong>
                <span>Hit Rate</span>
              </div>
              <div>
                <strong>{evalRun.metrics.mrr.toFixed(3)}</strong>
                <span>MRR</span>
              </div>
              <div>
                <strong>{evalRun.metrics.hitCount}/{evalRun.metrics.totalCases}</strong>
                <span>命中用例</span>
              </div>
              <div>
                <strong>{evalRun.metrics.latencyP95Ms}ms</strong>
                <span>检索 P95</span>
              </div>
              <div>
                <strong>{evalRun.metrics.lowConfidence}</strong>
                <span>低置信</span>
              </div>
              <div>
                <strong>{evalRun.metrics.skippedDisabled}</strong>
                <span>跳过禁用</span>
              </div>
            </div>
            <div className="kb-eval-run-cases">
              {(issueCases.length > 0 ? issueCases : reportCases).slice(0, 8).map((item) => (
                <div className="kb-eval-run-case" key={item.caseId}>
                  <div className="kb-eval-run-case-main">
                    <span className={item.hit ? 'kb-eval-run-pass' : 'kb-eval-run-fail'}>
                      {item.hit ? `命中 #${item.hitRank}` : '未命中'}
                    </span>
                    {item.lowConfidence && <span>低置信</span>}
                    {item.matchedBy && <span>{item.matchedBy}</span>}
                    <strong>{item.question}</strong>
                    <p>{item.error || item.expectedAnswer}</p>
                  </div>
                  {item.retrieved[0] && (
                    <div className="kb-eval-run-evidence">
                      <span>{item.retrieved[0].documentName || item.retrieved[0].documentId}</span>
                      <p>{item.retrieved[0].text}</p>
                    </div>
                  )}
                </div>
              ))}
            </div>
          </section>
        )}

        {evalComparison && (
          <section className="kb-eval-compare-report">
            <div className="kb-eval-compare-head">
              <div>
                <span>检索模式对比</span>
                <strong>混合检索 - 向量检索</strong>
              </div>
              <span>
                {evalComparison.hybrid.metrics.totalCases} 条用例 · {evalComparison.dense.elapsedMs + evalComparison.hybrid.elapsedMs}ms
              </span>
            </div>
            <div className="kb-eval-compare-grid">
              <div className="kb-eval-compare-row kb-eval-compare-row--head">
                <span>指标</span>
                <span>向量</span>
                <span>混合</span>
                <span>变化</span>
              </div>
              {comparisonRows.map((row) => (
                <div className="kb-eval-compare-row" key={row.label}>
                  <span>{row.label}</span>
                  <span>{row.dense}</span>
                  <span>{row.hybrid}</span>
                  <span className={`kb-eval-compare-delta kb-eval-compare-delta--${row.tone}`}>
                    {row.delta}
                  </span>
                </div>
              ))}
            </div>
            {hybridRecommendation && (
              <div className={`kb-eval-hybrid-recommendation kb-eval-hybrid-recommendation--${hybridRecommendation.tone}`}>
                <div>
                  <span>默认策略建议</span>
                  <strong>{hybridRecommendation.title}</strong>
                </div>
                <ul>
                  {hybridRecommendation.reasons.map((reason) => (
                    <li key={reason}>{reason}</li>
                  ))}
                </ul>
              </div>
            )}
            <div className="kb-eval-compare-case-summary">
              <span>修复 {comparisonCaseStats.fixed}</span>
              <span>退步 {comparisonCaseStats.regressed}</span>
              <span>排名提升 {comparisonCaseStats.rankImproved}</span>
              <span>排名下降 {comparisonCaseStats.rankWorse}</span>
            </div>
            <div className="kb-eval-compare-case-list">
              {visibleComparisonCases.length === 0 ? (
                <div className="kb-eval-compare-case-empty">没有明显用例级变化。</div>
              ) : (
                visibleComparisonCases.map((item) => (
                  <article className="kb-eval-compare-case" key={item.caseId}>
                    <div>
                      <span className={`kb-eval-compare-delta--${item.tone}`}>{item.label}</span>
                      <strong>{item.question}</strong>
                      <p>{item.expectedAnswer}</p>
                    </div>
                    <div className="kb-eval-compare-case-ranks">
                      <span>向量：{item.denseLabel}</span>
                      <span>混合：{item.hybridLabel}</span>
                    </div>
                  </article>
                ))
              )}
            </div>
          </section>
        )}

        <div className="kb-eval-preview-list">
          {previewItems.map((item, index) => {
            const isEditing = editingItemId === item.id
            const itemDraft = isEditing ? draft : null
            return (
              <article className="kb-eval-preview-item" key={item.id || index}>
                <div className="kb-eval-preview-head">
                  <div className="kb-eval-preview-tags">
                    <span>#{index + 1}</span>
                    <span>{answerTypeLabel[item.answer_type] ?? item.answer_type}</span>
                    <span>{difficultyLabel[item.difficulty] ?? item.difficulty}</span>
                    {item.review_status && <span>{reviewStatusLabel[item.review_status] ?? item.review_status}</span>}
                    {item.disabled && <span>未启用</span>}
                  </div>
                  {editable && !isEditing && (
                    <div className="kb-eval-item-actions kb-eval-item-actions--head">
                      {item.review_status !== 'approved' && (
                        <button
                          onClick={() => void quickUpdateItem(item, { review_status: 'approved', disabled: false })}
                          disabled={savingItemId === item.id}
                        >
                          审核通过
                        </button>
                      )}
                      <button
                        onClick={() => void quickUpdateItem(item, { disabled: !item.disabled })}
                        disabled={savingItemId === item.id}
                      >
                        {item.disabled ? '启用' : '禁用'}
                      </button>
                      <button onClick={() => startEditing(item)}>编辑</button>
                      <button
                        className="kb-eval-danger-btn"
                        onClick={() => void deleteItem(item)}
                        disabled={deletingItemId === item.id}
                      >
                        {deletingItemId === item.id ? '删除中' : '删除'}
                      </button>
                    </div>
                  )}
                </div>

                {isEditing && itemDraft ? (
                  <form
                    className="kb-eval-edit-form"
                    onSubmit={(event) => {
                      event.preventDefault()
                      void saveItem(item, itemDraft)
                    }}
                  >
                    <label>
                      <span>问题</span>
                      <input
                        value={itemDraft.question}
                        onChange={(event) => updateDraft('question', event.currentTarget.value)}
                      />
                    </label>
                    <label>
                      <span>答案</span>
                      <textarea
                        value={itemDraft.answer}
                        onChange={(event) => updateDraft('answer', event.currentTarget.value)}
                      />
                    </label>
                    <label>
                      <span>证据片段</span>
                      <textarea
                        value={itemDraft.snippets}
                        onChange={(event) => updateDraft('snippets', event.currentTarget.value)}
                      />
                    </label>
                    <div className="kb-eval-edit-row">
                      <label>
                        <span>题型</span>
                        <select
                          value={itemDraft.answerType}
                          onChange={(event) => updateDraft('answerType', event.currentTarget.value)}
                        >
                          <option value="extractive">摘录</option>
                          <option value="numeric">数值</option>
                          <option value="listing">列表</option>
                          <option value="process">流程</option>
                          <option value="retrieval-debug-candidate">调试候选</option>
                        </select>
                      </label>
                      <label>
                        <span>难度</span>
                        <select
                          value={itemDraft.difficulty}
                          onChange={(event) => updateDraft('difficulty', event.currentTarget.value)}
                        >
                          <option value="easy">简单</option>
                          <option value="medium">中等</option>
                          <option value="hard">困难</option>
                        </select>
                      </label>
                      <label>
                        <span>审核</span>
                        <select
                          value={itemDraft.reviewStatus}
                          onChange={(event) => updateDraft('reviewStatus', event.currentTarget.value)}
                        >
                          <option value="pending">待审核</option>
                          <option value="approved">已审核</option>
                        </select>
                      </label>
                    </div>
                    <label className="kb-eval-edit-check">
                      <input
                        type="checkbox"
                        checked={!itemDraft.disabled}
                        onChange={(event) => updateDraft('disabled', !event.currentTarget.checked)}
                      />
                      <span>参与评估</span>
                    </label>
                    <label>
                      <span>备注</span>
                      <textarea
                        value={itemDraft.notes}
                        onChange={(event) => updateDraft('notes', event.currentTarget.value)}
                      />
                    </label>
                    <div className="kb-eval-item-actions">
                      <button type="submit" disabled={savingItemId === item.id}>
                        {savingItemId === item.id ? '保存中' : '保存'}
                      </button>
                      <button
                        type="button"
                        onClick={() => {
                          setEditingItemId(null)
                          setDraft(null)
                        }}
                      >
                        取消
                      </button>
                    </div>
                  </form>
                ) : (
                  <>
                    <div className="kb-eval-preview-body">
                      <div className="kb-eval-preview-question">{getEvalQuestion(item)}</div>
                      <div className="kb-eval-preview-answer">{getEvalAnswer(item)}</div>
                    </div>
                    {getEvalSnippets(item).length > 0 && (
                      <div className="kb-eval-preview-evidence">
                        <span>证据片段</span>
                        <pre>{getEvalSnippets(item).join('\n\n')}</pre>
                      </div>
                    )}
                  </>
                )}
              </article>
            )
          })}
        </div>

        <footer className="kb-eval-dialog-actions">
          <span>
            共 {dataset.items.length} 条，已启用 {enabledCount} 条。
            {datasetId ? ` 已保存为 ${datasetId}。` : ''}
          </span>
          <div>
            {onRun && datasetId && (
              <label className="kb-eval-run-mode">
                <span>检索模式</span>
                <select
                  value={evalSearchMode}
                  onChange={(event) => setEvalSearchMode(event.currentTarget.value as RetrievalSearchMode)}
                  disabled={actionBusy}
                >
                  <option value="auto">自动</option>
                  <option value="dense">向量</option>
                  <option value="hybrid">混合</option>
                </select>
              </label>
            )}
            {onRun && datasetId && (
              <button onClick={() => void runDataset()} disabled={actionBusy}>
                {running ? '评估中' : '运行评估'}
              </button>
            )}
            {onRun && datasetId && (
              <button onClick={() => void runComparison()} disabled={actionBusy}>
                {comparing ? '对比中' : '运行对比'}
              </button>
            )}
            <button onClick={() => downloadEvalDataset(dataset, true)}>下载启用 JSON</button>
            <button onClick={() => downloadEvalDataset(dataset, false)}>下载全部 JSON</button>
          </div>
        </footer>
      </div>
    </div>
  )
}

export default EvalDatasetDialog
