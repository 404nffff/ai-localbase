import React, { createContext, useContext, useState, useCallback, useMemo, useEffect, type ReactNode } from 'react'
import type { KnowledgeBase } from '../../../App'

interface KnowledgeBaseContextValue {
  // State
  knowledgeBases: KnowledgeBase[]
  selectedKnowledgeBaseId: string | null
  selectedKnowledgeBase: KnowledgeBase | null
  collapsedKnowledgeBases: Record<string, boolean>

  // Actions
  setKnowledgeBases: (bases: KnowledgeBase[]) => void
  selectKnowledgeBase: (id: string) => void
  toggleCollapse: (id: string) => void
  updateKnowledgeBase: (id: string, updates: Partial<KnowledgeBase>) => void
}

const KnowledgeBaseContext = createContext<KnowledgeBaseContextValue | null>(null)

export const useKnowledgeBase = () => {
  const context = useContext(KnowledgeBaseContext)
  if (!context) {
    throw new Error('useKnowledgeBase must be used within KnowledgeBaseProvider')
  }
  return context
}

interface KnowledgeBaseProviderProps {
  children: ReactNode
  initialKnowledgeBases?: KnowledgeBase[]
  initialSelectedId?: string | null
  initialCollapsed?: Record<string, boolean>
}

export const KnowledgeBaseProvider: React.FC<KnowledgeBaseProviderProps> = ({
  children,
  initialKnowledgeBases = [],
  initialSelectedId = null,
  initialCollapsed = {},
}) => {
  const [knowledgeBases, setKnowledgeBases] = useState<KnowledgeBase[]>(initialKnowledgeBases)
  const [selectedKnowledgeBaseId, setSelectedKnowledgeBaseId] = useState<string | null>(initialSelectedId)
  const [collapsedKnowledgeBases, setCollapsedKnowledgeBases] = useState<Record<string, boolean>>(initialCollapsed)

  // Sync with external changes
  useEffect(() => {
    setKnowledgeBases(initialKnowledgeBases)
  }, [initialKnowledgeBases])

  useEffect(() => {
    setSelectedKnowledgeBaseId(initialSelectedId)
  }, [initialSelectedId])

  useEffect(() => {
    setCollapsedKnowledgeBases(initialCollapsed)
  }, [initialCollapsed])

  // Derived state
  const selectedKnowledgeBase = useMemo(() => {
    return knowledgeBases.find(kb => kb.id === selectedKnowledgeBaseId) || null
  }, [knowledgeBases, selectedKnowledgeBaseId])

  // Actions
  const selectKnowledgeBase = useCallback((id: string) => {
    setSelectedKnowledgeBaseId(id)
  }, [])

  const toggleCollapse = useCallback((id: string) => {
    setCollapsedKnowledgeBases(prev => ({
      ...prev,
      [id]: !prev[id],
    }))
  }, [])

  const updateKnowledgeBase = useCallback((id: string, updates: Partial<KnowledgeBase>) => {
    setKnowledgeBases(prev =>
      prev.map(kb => (kb.id === id ? { ...kb, ...updates } : kb))
    )
  }, [])

  // Memoize context value to prevent unnecessary re-renders
  const value = useMemo<KnowledgeBaseContextValue>(
    () => ({
      knowledgeBases,
      selectedKnowledgeBaseId,
      selectedKnowledgeBase,
      collapsedKnowledgeBases,
      setKnowledgeBases,
      selectKnowledgeBase,
      toggleCollapse,
      updateKnowledgeBase,
    }),
    [
      knowledgeBases,
      selectedKnowledgeBaseId,
      selectedKnowledgeBase,
      collapsedKnowledgeBases,
      selectKnowledgeBase,
      toggleCollapse,
      updateKnowledgeBase,
    ]
  )

  return <KnowledgeBaseContext.Provider value={value}>{children}</KnowledgeBaseContext.Provider>
}
