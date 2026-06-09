import React, { createContext, useContext, useState, useCallback, useMemo, type ReactNode } from 'react'
import type {
  KnowledgeBaseHealthResponse,
  RetrievalDebugResponse,
  RetrievalSearchMode,
} from '../../../services/api'
import {
  getKnowledgeBaseHealth,
  debugRetrieve,
  extractErrorMessage,
} from '../../../services/api'

interface HealthContextValue {
  // Health Monitoring
  healthByKnowledgeBase: Record<string, KnowledgeBaseHealthResponse>
  healthLoadingId: string | null
  healthError: string
  fetchHealth: (knowledgeBaseId: string) => Promise<void>
  clearHealthError: () => void

  // Retrieval Debug
  retrievalQuery: string
  retrievalSearchMode: RetrievalSearchMode
  retrievalDebugKnowledgeBaseId: string | null
  retrievalDebugResult: RetrievalDebugResponse | null
  retrievalDebugError: string
  retrievalDebugLoading: boolean

  setRetrievalQuery: (query: string) => void
  setRetrievalSearchMode: (mode: RetrievalSearchMode) => void
  runRetrievalDebug: (
    knowledgeBaseId: string,
    documentId?: string | null,
    verbose?: boolean
  ) => Promise<void>
  clearRetrievalDebug: () => void
}

const HealthContext = createContext<HealthContextValue | null>(null)

export const useHealth = () => {
  const context = useContext(HealthContext)
  if (!context) {
    throw new Error('useHealth must be used within HealthProvider')
  }
  return context
}

interface HealthProviderProps {
  children: ReactNode
}

export const HealthProvider: React.FC<HealthProviderProps> = ({ children }) => {
  // Health Monitoring
  const [healthByKnowledgeBase, setHealthByKnowledgeBase] = useState<
    Record<string, KnowledgeBaseHealthResponse>
  >({})
  const [healthLoadingId, setHealthLoadingId] = useState<string | null>(null)
  const [healthError, setHealthError] = useState('')

  // Retrieval Debug
  const [retrievalQuery, setRetrievalQuery] = useState('')
  const [retrievalSearchMode, setRetrievalSearchMode] = useState<RetrievalSearchMode>('auto')
  const [retrievalDebugKnowledgeBaseId, setRetrievalDebugKnowledgeBaseId] = useState<string | null>(null)
  const [retrievalDebugResult, setRetrievalDebugResult] = useState<RetrievalDebugResponse | null>(null)
  const [retrievalDebugError, setRetrievalDebugError] = useState('')
  const [retrievalDebugLoading, setRetrievalDebugLoading] = useState(false)

  // Fetch Health
  const fetchHealth = useCallback(async (knowledgeBaseId: string) => {
    setHealthLoadingId(knowledgeBaseId)
    setHealthError('')

    try {
      const health = await getKnowledgeBaseHealth(knowledgeBaseId)

      setHealthByKnowledgeBase(prev => ({
        ...prev,
        [knowledgeBaseId]: health,
      }))
    } catch (err) {
      setHealthError(await extractErrorMessage(err))
    } finally {
      setHealthLoadingId(null)
    }
  }, [])

  const clearHealthError = useCallback(() => {
    setHealthError('')
  }, [])

  // Run Retrieval Debug
  const runRetrievalDebug = useCallback(async (
    knowledgeBaseId: string,
    documentId: string | null = null,
    verbose = false
  ) => {
    if (!retrievalQuery.trim()) {
      setRetrievalDebugError('请输入查询内容')
      return
    }

    setRetrievalDebugLoading(true)
    setRetrievalDebugError('')
    setRetrievalDebugKnowledgeBaseId(knowledgeBaseId)

    try {
      const result = await debugRetrieve(
        knowledgeBaseId,
        retrievalQuery,
        documentId,
        retrievalSearchMode,
        verbose
      )

      setRetrievalDebugResult(result)
    } catch (err) {
      setRetrievalDebugError(await extractErrorMessage(err))
      setRetrievalDebugResult(null)
    } finally {
      setRetrievalDebugLoading(false)
    }
  }, [retrievalQuery, retrievalSearchMode])

  const clearRetrievalDebug = useCallback(() => {
    setRetrievalDebugResult(null)
    setRetrievalDebugError('')
    setRetrievalDebugKnowledgeBaseId(null)
  }, [])

  // Memoize context value
  const value = useMemo<HealthContextValue>(
    () => ({
      healthByKnowledgeBase,
      healthLoadingId,
      healthError,
      fetchHealth,
      clearHealthError,

      retrievalQuery,
      retrievalSearchMode,
      retrievalDebugKnowledgeBaseId,
      retrievalDebugResult,
      retrievalDebugError,
      retrievalDebugLoading,

      setRetrievalQuery,
      setRetrievalSearchMode,
      runRetrievalDebug,
      clearRetrievalDebug,
    }),
    [
      healthByKnowledgeBase,
      healthLoadingId,
      healthError,
      fetchHealth,
      clearHealthError,
      retrievalQuery,
      retrievalSearchMode,
      retrievalDebugKnowledgeBaseId,
      retrievalDebugResult,
      retrievalDebugError,
      retrievalDebugLoading,
      runRetrievalDebug,
      clearRetrievalDebug,
    ]
  )

  return <HealthContext.Provider value={value}>{children}</HealthContext.Provider>
}
