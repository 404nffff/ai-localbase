import { useCallback, useMemo, useState } from 'react'
import type { DocumentItem, KnowledgeBase } from '../App'

export const useKnowledgeWorkspaceState = () => {
  const [knowledgeBases, setKnowledgeBases] = useState<KnowledgeBase[]>([])
  const [selectedKnowledgeBaseId, setSelectedKnowledgeBaseId] = useState<string | null>(null)
  const [selectedDocumentId, setSelectedDocumentId] = useState<string | null>(null)
  const [collapsedKnowledgeBases, setCollapsedKnowledgeBases] = useState<Record<string, boolean>>({})

  const selectedKnowledgeBase = useMemo(
    () => knowledgeBases.find((knowledgeBase) => knowledgeBase.id === selectedKnowledgeBaseId) ?? null,
    [knowledgeBases, selectedKnowledgeBaseId],
  )

  const selectedDocument = useMemo<DocumentItem | null>(() => {
    if (!selectedKnowledgeBase || !selectedDocumentId) {
      return null
    }
    return selectedKnowledgeBase.documents.find((document) => document.id === selectedDocumentId) ?? null
  }, [selectedDocumentId, selectedKnowledgeBase])

  const toggleKnowledgeBaseCollapse = useCallback((knowledgeBaseId: string) => {
    setCollapsedKnowledgeBases((current) => ({
      ...current,
      [knowledgeBaseId]: !current[knowledgeBaseId],
    }))
  }, [])

  return {
    knowledgeBases,
    setKnowledgeBases,
    selectedKnowledgeBaseId,
    setSelectedKnowledgeBaseId,
    selectedDocumentId,
    setSelectedDocumentId,
    collapsedKnowledgeBases,
    setCollapsedKnowledgeBases,
    selectedKnowledgeBase,
    selectedDocument,
    toggleKnowledgeBaseCollapse,
  }
}
