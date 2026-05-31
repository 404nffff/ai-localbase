import React, { useMemo } from 'react'
import type { EvalGroundTruthCase, GenerateEvalDatasetResponse } from '../../services/api'

interface EvalDatasetDialogProps {
  dataset: GenerateEvalDatasetResponse
  scopeName: string
  onClose: () => void
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

const downloadEvalDataset = (dataset: GenerateEvalDatasetResponse) => {
  const blob = new Blob([JSON.stringify(dataset.items, null, 2)], {
    type: 'application/json;charset=utf-8',
  })
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  const timestamp = new Date().toISOString().slice(0, 19).replace(/[-:T]/g, '')
  const scope = dataset.documentId || dataset.knowledgeBaseId || 'all'
  link.href = url
  link.download = `ground_truth_${scope}_${timestamp}.json`
  document.body.appendChild(link)
  link.click()
  link.remove()
  URL.revokeObjectURL(url)
}

const EvalDatasetDialog: React.FC<EvalDatasetDialogProps> = ({
  dataset,
  scopeName,
  onClose,
}) => {
  const previewItems = dataset.items.slice(0, 8)
  const answerTypeStats = useMemo(
    () => formatStats(countBy(dataset.items, 'answer_type'), answerTypeLabel),
    [dataset.items],
  )
  const difficultyStats = useMemo(
    () => formatStats(countBy(dataset.items, 'difficulty'), difficultyLabel),
    [dataset.items],
  )

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
            <strong>{dataset.documentCount}</strong>
            <span>覆盖文档</span>
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

        <div className="kb-eval-preview-list">
          {previewItems.map((item, index) => (
            <article className="kb-eval-preview-item" key={item.id || index}>
              <div className="kb-eval-preview-head">
                <span>#{index + 1}</span>
                <span>{answerTypeLabel[item.answer_type] ?? item.answer_type}</span>
                <span>{difficultyLabel[item.difficulty] ?? item.difficulty}</span>
              </div>
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
            </article>
          ))}
        </div>

        <footer className="kb-eval-dialog-actions">
          <span>预览前 {previewItems.length} 条，下载文件包含全部用例。</span>
          <button onClick={() => downloadEvalDataset(dataset)}>下载 JSON</button>
        </footer>
      </div>
    </div>
  )
}

export default EvalDatasetDialog
