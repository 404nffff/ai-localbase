import React, { useDeferredValue, useEffect, useMemo, useRef, useState } from 'react'
import type { DocumentItem } from '../../App'
import { DOCUMENT_SCOPE_RESULT_LIMIT, getDocumentScopeMatches } from './documentScopeOptions'

interface DocumentScopePickerProps {
  documents: DocumentItem[]
  selectedDocumentId: string | null
  onSelectDocument: (documentId: string | null) => void
  disabled?: boolean
}

// 聊天区范围选择器只改变当前会话范围，不修改知识库或文档数据。
const DocumentScopePicker: React.FC<DocumentScopePickerProps> = ({
  documents,
  selectedDocumentId,
  onSelectDocument,
  disabled = false,
}) => {
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState('')
  const deferredQuery = useDeferredValue(query)
  const pickerRef = useRef<HTMLDivElement>(null)
  const searchRef = useRef<HTMLInputElement>(null)
  const selectedDocument = useMemo(
    () => documents.find((document) => document.id === selectedDocumentId) ?? null,
    [documents, selectedDocumentId],
  )
  const matches = useMemo(
    () => getDocumentScopeMatches(documents, deferredQuery, selectedDocumentId),
    [deferredQuery, documents, selectedDocumentId],
  )

  useEffect(() => {
    if (!open) return

    const handlePointerDown = (event: PointerEvent) => {
      const target = event.target
      if (target instanceof Node && pickerRef.current?.contains(target)) return
      setOpen(false)
      setQuery('')
    }
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key !== 'Escape') return
      setOpen(false)
      setQuery('')
    }

    document.addEventListener('pointerdown', handlePointerDown)
    document.addEventListener('keydown', handleKeyDown)
    return () => {
      document.removeEventListener('pointerdown', handlePointerDown)
      document.removeEventListener('keydown', handleKeyDown)
    }
  }, [open])

  useEffect(() => {
    if (open) searchRef.current?.focus()
  }, [open])

  const selectDocument = (documentId: string | null) => {
    onSelectDocument(documentId)
    setOpen(false)
    setQuery('')
  }

  const isLimited = matches.total > matches.visible.length

  return (
    <div className={`document-scope-picker${open ? ' document-scope-picker--open' : ''}`} ref={pickerRef}>
      <button
        type="button"
        className="document-scope-trigger"
        disabled={disabled}
        aria-expanded={open}
        aria-haspopup="dialog"
        onClick={() => setOpen((current) => !current)}
        title={selectedDocument?.name ?? (disabled ? '请先选择知识库' : '全部文档')}
      >
        <span aria-hidden="true">🧭</span>
        <span className="document-scope-trigger-label">
          {selectedDocument?.name ?? (disabled ? '全部知识库' : '全部文档')}
        </span>
        <span aria-hidden="true">▾</span>
      </button>

      {open && !disabled && (
        <div className="document-scope-popover" role="dialog" aria-label="选择文档检索范围">
          <label className="document-scope-search">
            <span aria-hidden="true">🔎</span>
            <input
              ref={searchRef}
              type="search"
              value={query}
              onChange={(event) => setQuery(event.target.value)}
              placeholder="输入文件名搜索"
              aria-label="搜索文档范围"
            />
          </label>

          <div className="document-scope-summary">
            <span>{deferredQuery ? `${matches.total} 个匹配` : `${documents.length} 份文档`}</span>
            {isLimited && <span>仅显示前 {DOCUMENT_SCOPE_RESULT_LIMIT} 项</span>}
          </div>

          <div className="document-scope-options" role="listbox">
            <button
              type="button"
              role="option"
              aria-selected={!selectedDocumentId}
              className={!selectedDocumentId ? 'is-selected' : ''}
              onClick={() => selectDocument(null)}
            >
              <span aria-hidden="true">📚</span>
              <span>
                <strong>全部文档</strong>
                <small>检索当前知识库的全部资料</small>
              </span>
              {!selectedDocumentId && <span aria-hidden="true">✓</span>}
            </button>

            {matches.visible.map((document) => {
              const selected = document.id === selectedDocumentId
              return (
                <button
                  type="button"
                  role="option"
                  aria-selected={selected}
                  className={selected ? 'is-selected' : ''}
                  key={document.id}
                  onClick={() => selectDocument(document.id)}
                >
                  <span aria-hidden="true">📄</span>
                  <span>
                    <strong title={document.name}>{document.name}</strong>
                    <small>{document.sizeLabel} · {document.chunkCount ?? 0} chunks</small>
                  </span>
                  {selected && <span aria-hidden="true">✓</span>}
                </button>
              )
            })}

            {matches.total === 0 && <div className="document-scope-empty">没有匹配的文件</div>}
          </div>
        </div>
      )}
    </div>
  )
}

export default DocumentScopePicker
