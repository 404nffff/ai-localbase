// 文档列表固定分页，避免大知识库一次渲染全部节点。
export const DOCUMENTS_PER_PAGE = 50

interface DocumentPage<T> {
  items: T[]
  page: number
  pageCount: number
}

// 自动收敛非法页码，保证删除或筛选后仍能显示有效页面。
export const getDocumentPage = <T>(
  documents: T[],
  requestedPage: number,
  pageSize = DOCUMENTS_PER_PAGE,
): DocumentPage<T> => {
  const normalizedPageSize = Math.max(1, Math.floor(pageSize) || DOCUMENTS_PER_PAGE)
  const pageCount = Math.max(1, Math.ceil(documents.length / normalizedPageSize))
  const page = Math.min(Math.max(1, Math.floor(requestedPage) || 1), pageCount)
  const start = (page - 1) * normalizedPageSize

  return {
    items: documents.slice(start, start + normalizedPageSize),
    page,
    pageCount,
  }
}
