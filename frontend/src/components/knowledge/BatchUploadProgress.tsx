import React from 'react'

export type BatchIndexStatus =
  | 'idle'
  | 'indexing'
  | 'canceling'
  | 'canceled'
  | 'done'
  | 'partial-failed'
  | 'failed'

interface BatchIndexIssueItem {
  name: string
  path: string
  reason: string
}

export interface BatchIndexTask {
  knowledgeBaseId: string | null
  status: BatchIndexStatus
  totalFiles: number
  successFiles: number
  failedFiles: number
  pendingFiles: number
  processedFiles: number
  currentFileName: string
  currentFilePath: string
  failedItems: BatchIndexIssueItem[]
  summaryMessage: string
}

interface BatchUploadProgressProps {
  task: BatchIndexTask
  progressPercent: number
  showDetails: boolean
  showFailedItems: boolean
  canCancelIndex: boolean
  onToggleDetails: () => void
  onToggleFailedItems: () => void
  onCancel: () => void
}

const taskStatusText = (status: BatchIndexStatus) => {
  if (status === 'indexing') return '索引中'
  if (status === 'canceling') return '取消中'
  if (status === 'canceled') return '已取消'
  if (status === 'done') return '已完成'
  if (status === 'partial-failed') return '部分完成'
  if (status === 'failed') return '失败'
  return '待开始'
}

const BatchUploadProgress: React.FC<BatchUploadProgressProps> = ({
  task,
  progressPercent,
  showDetails,
  showFailedItems,
  canCancelIndex,
  onToggleDetails,
  onToggleFailedItems,
  onCancel,
}) => (
  <div className="kb-upload-task-shell">
    <div className="kb-upload-task-compact">
      <div className="kb-upload-task-compact-main">
        <span className={`kb-upload-task-pill kb-upload-task-pill--${task.status}`}>
          {taskStatusText(task.status)}
        </span>
        <div className="kb-upload-task-compact-text">
          <div className="kb-upload-task-compact-title">批量索引任务</div>
          <div className="kb-upload-task-compact-summary">
            {task.processedFiles}/{task.totalFiles} · 成功 {task.successFiles} · 失败 {task.failedFiles}
          </div>
        </div>
      </div>
      <div className="kb-upload-task-actions">
        <button className="kb-upload-task-btn kb-upload-task-btn--ghost" onClick={onToggleDetails}>
          {showDetails ? '收起' : '详情'}
        </button>
        {canCancelIndex && (
          <button
            className="kb-upload-task-btn kb-upload-task-btn--danger"
            onClick={onCancel}
            disabled={task.status === 'canceling'}
          >
            {task.status === 'canceling' ? '取消中' : '取消索引'}
          </button>
        )}
      </div>
    </div>

    {showDetails && (
      <div className="kb-upload-task">
        <div className="kb-upload-progress-meta">
          <span>已处理 {task.processedFiles} / {task.totalFiles}</span>
          <span>{progressPercent}%</span>
        </div>
        <div className="kb-upload-progress-track">
          <div className="kb-upload-progress-fill" style={{ width: `${progressPercent}%` }} />
        </div>

        <div className="kb-upload-stats-grid">
          <div className="kb-upload-stat-card"><span className="kb-upload-stat-label">总文件</span><strong>{task.totalFiles}</strong></div>
          <div className="kb-upload-stat-card"><span className="kb-upload-stat-label">成功</span><strong>{task.successFiles}</strong></div>
          <div className="kb-upload-stat-card"><span className="kb-upload-stat-label">失败</span><strong>{task.failedFiles}</strong></div>
          <div className="kb-upload-stat-card"><span className="kb-upload-stat-label">待处理</span><strong>{task.pendingFiles}</strong></div>
        </div>

        {task.currentFilePath && (
          <div className="kb-upload-current-file">当前处理：{task.currentFilePath}</div>
        )}
        {task.summaryMessage && (
          <div className="kb-upload-summary">{task.summaryMessage}</div>
        )}

        {task.failedItems.length > 0 && (
          <div className="kb-upload-issues-toggle-row">
            <button className="kb-upload-task-btn kb-upload-task-btn--ghost" onClick={onToggleFailedItems}>
              {showFailedItems ? '隐藏失败文件' : `查看失败文件（${task.failedItems.length}）`}
            </button>
          </div>
        )}
        {showFailedItems && task.failedItems.length > 0 && (
          <div className="kb-upload-issues">
            <div className="kb-upload-issues-title">失败文件</div>
            {task.failedItems.map((item) => (
              <div key={`${item.path}-${item.reason}`} className="kb-upload-issue-item">
                <span className="kb-upload-issue-path">{item.path}</span>
                <span className="kb-upload-issue-reason">{item.reason}</span>
              </div>
            ))}
          </div>
        )}
      </div>
    )}
  </div>
)

export default BatchUploadProgress
