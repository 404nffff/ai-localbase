import type { KnowledgeBaseHealthResponse } from '../../services/api'

export interface LabelTone {
  text: string
  color: string
  bg: string
}

export const documentStatusLabel = (status: string): LabelTone => {
  if (status === 'indexed') return { text: '已索引', color: '#047857', bg: '#d1fae5' }
  if (status === 'processing') return { text: '处理中', color: '#b45309', bg: '#fef3c7' }
  if (status === 'failed') return { text: '失败', color: '#b91c1c', bg: '#fee2e2' }
  return { text: '就绪', color: '#2563eb', bg: '#dbeafe' }
}

export const healthStatusLabel = (status: KnowledgeBaseHealthResponse['status']): LabelTone => {
  if (status === 'healthy') return { text: '健康', color: '#047857', bg: '#d1fae5' }
  if (status === 'warning') return { text: '需关注', color: '#b45309', bg: '#fef3c7' }
  if (status === 'attention') return { text: '需处理', color: '#b91c1c', bg: '#fee2e2' }
  return { text: '空库', color: '#475569', bg: '#e2e8f0' }
}

export const chunkKindLabel = (kind: string): string => {
  if (kind === 'structured_deterministic') return '确定性'
  if (kind === 'structured_summary') return '摘要'
  if (kind === 'structured_row') return '数据行'
  return '正文'
}

export const structuredIntentLabel = (intent?: string): string => {
  switch (intent) {
    case 'max':
      return '最大值'
    case 'min':
      return '最小值'
    case 'average':
      return '平均值'
    case 'filter':
      return '筛选'
    case 'group':
      return '分布'
    case 'count':
      return '计数'
    case 'preview':
      return '预览'
    default:
      return ''
  }
}
