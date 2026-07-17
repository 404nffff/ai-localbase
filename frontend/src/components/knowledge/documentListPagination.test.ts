import { describe, expect, it } from 'vitest'
import { DOCUMENTS_PER_PAGE, getDocumentPage } from './documentListPagination'

describe('getDocumentPage', () => {
  it('limits a very large document collection to one rendered page', () => {
    const documents = Array.from({ length: 10_000 }, (_, index) => `doc-${index}`)
    const result = getDocumentPage(documents, 1)

    expect(result.items).toHaveLength(DOCUMENTS_PER_PAGE)
    expect(result.pageCount).toBe(200)
    expect(result.items[0]).toBe('doc-0')
    expect(result.items[result.items.length - 1]).toBe('doc-49')
  })

  it('clamps an invalid page to the final available page', () => {
    const documents = Array.from({ length: 121 }, (_, index) => `doc-${index}`)
    const result = getDocumentPage(documents, 99)

    expect(result.page).toBe(3)
    expect(result.items).toEqual(documents.slice(100))
  })

  it('keeps empty results on a stable first page', () => {
    expect(getDocumentPage([], -3)).toEqual({ items: [], page: 1, pageCount: 1 })
  })

  it('normalizes invalid page sizes', () => {
    const documents = ['a', 'b', 'c']
    expect(getDocumentPage(documents, 2, 0)).toEqual({
      items: documents,
      page: 1,
      pageCount: 1,
    })
  })
})
