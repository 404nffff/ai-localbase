import type { KnowledgeBaseHealthResponse } from '../../services/api'

export interface LabelTone {
  text: string
  color: string
  bg: string
}

export const documentStatusLabel = (status: string): LabelTone => {
  if (status === 'indexed') return { text: '已索引', color: 'var(--color-success)', bg: 'var(--color-success-light)' }
  if (status === 'processing') return { text: '处理中', color: 'var(--color-warning)', bg: 'var(--color-warning-light)' }
  if (status === 'failed') return { text: '失败', color: 'var(--color-error)', bg: 'var(--color-error-light)' }
  return { text: '就绪', color: 'var(--color-primary)', bg: 'var(--color-primary-light)' }
}

export const healthStatusLabel = (status: KnowledgeBaseHealthResponse['status']): LabelTone => {
  if (status === 'healthy') return { text: '健康', color: 'var(--color-success)', bg: 'var(--color-success-light)' }
  if (status === 'warning') return { text: '需关注', color: 'var(--color-warning)', bg: 'var(--color-warning-light)' }
  if (status === 'attention') return { text: '需处理', color: 'var(--color-error)', bg: 'var(--color-error-light)' }
  return { text: '空库', color: 'var(--text-secondary)', bg: 'var(--surface-muted)' }
}

export const chunkKindLabel = (kind: string): string => {
  if (kind === 'structured_summary') return '摘要'
  if (kind === 'structured_row') return '数据行'
  return '正文'
}
