export const DOCUMENT_SCOPE_RESULT_LIMIT = 100

export interface DocumentScopeOption {
  id: string
  name: string
}

interface DocumentScopeMatches<T> {
  total: number
  visible: T[]
}

const matchRank = (name: string, query: string) => {
  const normalizedName = name.toLocaleLowerCase('zh-CN')
  if (normalizedName === query) return 0
  if (normalizedName.startsWith(query)) return 1
  return 2
}

// 范围选择优先精确与前缀匹配，并限制超大知识库的渲染数量。
export const getDocumentScopeMatches = <T extends DocumentScopeOption>(
  documents: T[],
  query: string,
  selectedDocumentId: string | null,
  limit = DOCUMENT_SCOPE_RESULT_LIMIT,
): DocumentScopeMatches<T> => {
  const normalizedLimit = Math.max(0, Math.floor(limit) || 0)
  const normalizedQuery = query.trim().toLocaleLowerCase('zh-CN')
  const matches = normalizedQuery
    ? documents
      .filter((document) => document.name.toLocaleLowerCase('zh-CN').includes(normalizedQuery))
      .sort((left, right) => {
        const rankDifference = matchRank(left.name, normalizedQuery) - matchRank(right.name, normalizedQuery)
        return rankDifference || left.name.localeCompare(right.name, 'zh-CN')
      })
    : documents

  const visible = matches.slice(0, normalizedLimit)
  if (!normalizedQuery && selectedDocumentId && !visible.some((document) => document.id === selectedDocumentId)) {
    const selectedDocument = matches.find((document) => document.id === selectedDocumentId)
    if (selectedDocument && normalizedLimit > 0) {
      visible.splice(Math.max(normalizedLimit - 1, 0), 1)
      visible.unshift(selectedDocument)
    }
  }

  return {
    total: matches.length,
    visible,
  }
}
