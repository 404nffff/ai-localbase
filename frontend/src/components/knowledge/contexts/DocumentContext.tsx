import React, { createContext, useContext, useState, useCallback, useMemo, useRef, type ReactNode } from 'react'
import type { DocumentDetailResponse, DirectoryUploadTask, DirectoryUploadIssueItem, DocumentItem } from '../../../App'
import {
  deleteKnowledgeBaseDocument,
  fetchKnowledgeBaseDocumentDetail,
  reindexKnowledgeBaseDocument,
  uploadKnowledgeBaseFile,
  stageUpload,
  batchIndexDocuments,
  getDocumentIndexStatus,
  extractErrorMessage,
} from '../../../services/api'

interface DocumentContextValue {
  // Selection
  selectedDocumentId: string | null
  selectDocument: (documentId: string | null) => void

  // Document Detail
  documentDetail: DocumentDetailResponse | null
  documentDetailLoading: boolean
  documentDetailError: string
  documentDetailFocusChunkId: string
  openDocumentDetail: (knowledgeBaseId: string, documentId: string, focusChunkId?: string) => Promise<void>
  closeDocumentDetail: () => void

  // Document Actions
  reindexDocument: (knowledgeBaseId: string, documentId: string) => Promise<void>
  reindexingDocumentId: string | null
  reindexError: string
  removeDocument: (knowledgeBaseId: string, documentId: string, onSuccess?: () => void) => Promise<void>

  // Directory Upload
  directoryUploadTask: DirectoryUploadTask
  uploadDirectory: (knowledgeBaseId: string, files: FileList) => Promise<void>
  cancelDirectoryUpload: () => void
  retryFailedUploads: () => Promise<void>
}

const DocumentContext = createContext<DocumentContextValue | null>(null)

export const useDocument = () => {
  const context = useContext(DocumentContext)
  if (!context) {
    throw new Error('useDocument must be used within DocumentProvider')
  }
  return context
}

interface DocumentProviderProps {
  children: ReactNode
  onDocumentsChange?: (knowledgeBaseId: string) => void
}

const createEmptyDirectoryUploadTask = (): DirectoryUploadTask => ({
  knowledgeBaseId: null,
  status: 'idle',
  totalFiles: 0,
  eligibleFiles: 0,
  skippedFiles: 0,
  successFiles: 0,
  failedFiles: 0,
  pendingFiles: 0,
  processedFiles: 0,
  currentFileName: '',
  currentFilePath: '',
  failedItems: [],
  skippedItems: [],
  summaryMessage: '',
  indexingFiles: 0,
  indexedFiles: 0,
  indexFailedFiles: 0,
})

export const DocumentProvider: React.FC<DocumentProviderProps> = ({
  children,
  onDocumentsChange,
}) => {
  // Selection
  const [selectedDocumentId, setSelectedDocumentId] = useState<string | null>(null)

  // Document Detail
  const [documentDetail, setDocumentDetail] = useState<DocumentDetailResponse | null>(null)
  const [documentDetailLoading, setDocumentDetailLoading] = useState(false)
  const [documentDetailLoadingId, setDocumentDetailLoadingId] = useState<string | null>(null)
  const [documentDetailError, setDocumentDetailError] = useState('')
  const [documentDetailFocusChunkId, setDocumentDetailFocusChunkId] = useState('')

  // Document Actions
  const [reindexingDocumentId, setReindexingDocumentId] = useState<string | null>(null)
  const [reindexError, setReindexError] = useState('')

  // Directory Upload
  const [directoryUploadTask, setDirectoryUploadTask] = useState<DirectoryUploadTask>(
    createEmptyDirectoryUploadTask()
  )
  const directoryUploadCancelRef = useRef(false)
  const directoryUploadPendingFilesRef = useRef<Array<{ file: File; path: string }>>([])

  // Actions
  const selectDocument = useCallback((documentId: string | null) => {
    setSelectedDocumentId(documentId)
  }, [])

  const openDocumentDetail = useCallback(async (
    knowledgeBaseId: string,
    documentId: string,
    focusChunkId = ''
  ) => {
    if (documentDetailLoadingId === documentId) {
      return // Already loading this document
    }

    setDocumentDetailLoadingId(documentId)
    setDocumentDetailLoading(true)
    setDocumentDetailError('')
    setDocumentDetailFocusChunkId(focusChunkId)

    try {
      const detail = await fetchKnowledgeBaseDocumentDetail(knowledgeBaseId, documentId, focusChunkId)
      setDocumentDetail(detail)
      setSelectedDocumentId(documentId)
    } catch (err) {
      setDocumentDetailError(await extractErrorMessage(err))
    } finally {
      setDocumentDetailLoading(false)
      setDocumentDetailLoadingId(null)
    }
  }, [documentDetailLoadingId])

  const closeDocumentDetail = useCallback(() => {
    setDocumentDetail(null)
    setDocumentDetailError('')
    setDocumentDetailFocusChunkId('')
  }, [])

  const reindexDocument = useCallback(async (knowledgeBaseId: string, documentId: string) => {
    setReindexingDocumentId(documentId)
    setReindexError('')

    try {
      await reindexKnowledgeBaseDocument(knowledgeBaseId, documentId)

      // Refresh document detail if it's currently open
      if (documentDetail?.document.id === documentId) {
        await openDocumentDetail(knowledgeBaseId, documentId, documentDetailFocusChunkId)
      }

      // Notify parent about document change
      onDocumentsChange?.(knowledgeBaseId)
    } catch (err) {
      setReindexError(await extractErrorMessage(err))
    } finally {
      setReindexingDocumentId(null)
    }
  }, [documentDetail, documentDetailFocusChunkId, openDocumentDetail, onDocumentsChange])

  const removeDocument = useCallback(async (
    knowledgeBaseId: string,
    documentId: string,
    onSuccess?: () => void
  ) => {
    try {
      await deleteKnowledgeBaseDocument(knowledgeBaseId, documentId)

      // Close detail if this document was open
      if (documentDetail?.document.id === documentId) {
        closeDocumentDetail()
      }

      // Clear selection if this document was selected
      if (selectedDocumentId === documentId) {
        setSelectedDocumentId(null)
      }

      // Notify parent about document change
      onDocumentsChange?.(knowledgeBaseId)
      onSuccess?.()
    } catch (err) {
      throw err
    }
  }, [documentDetail, selectedDocumentId, closeDocumentDetail, onDocumentsChange])

  // Directory Upload Implementation
  const uploadDirectory = useCallback(async (knowledgeBaseId: string, files: FileList) => {
    directoryUploadCancelRef.current = false
    const allowedExtensions = ['.txt', '.md', '.pdf', '.csv', '.xlsx']

    // Phase 1: Scanning
    setDirectoryUploadTask({
      ...createEmptyDirectoryUploadTask(),
      knowledgeBaseId,
      status: 'scanning',
      totalFiles: files.length,
    })

    const eligibleItems: Array<{ file: File; path: string }> = []
    const skippedItems: DirectoryUploadIssueItem[] = []

    for (let i = 0; i < files.length; i++) {
      const file = files[i]
      const ext = '.' + file.name.split('.').pop()?.toLowerCase()

      if (allowedExtensions.includes(ext)) {
        eligibleItems.push({ file, path: (file as any).webkitRelativePath || file.name })
      } else {
        skippedItems.push({
          fileName: file.name,
          filePath: (file as any).webkitRelativePath || file.name,
          reason: `不支持的文件类型: ${ext}`,
        })
      }
    }

    directoryUploadPendingFilesRef.current = eligibleItems

    setDirectoryUploadTask(prev => ({
      ...prev,
      status: 'uploading',
      eligibleFiles: eligibleItems.length,
      skippedFiles: skippedItems.length,
      pendingFiles: eligibleItems.length,
      skippedItems,
    }))

    // Phase 2: Stage all files
    const uploadIds: string[] = []
    const failedItems: DirectoryUploadIssueItem[] = []

    for (let i = 0; i < eligibleItems.length; i++) {
      if (directoryUploadCancelRef.current) {
        setDirectoryUploadTask(prev => ({ ...prev, status: 'canceled' }))
        return
      }

      const { file, path } = eligibleItems[i]

      setDirectoryUploadTask(prev => ({
        ...prev,
        currentFileName: file.name,
        currentFilePath: path,
        processedFiles: i,
        pendingFiles: eligibleItems.length - i,
      }))

      try {
        const result = await stageUpload(file)
        uploadIds.push(result.id)

        setDirectoryUploadTask(prev => ({
          ...prev,
          successFiles: prev.successFiles + 1,
        }))
      } catch (err) {
        failedItems.push({
          fileName: file.name,
          filePath: path,
          reason: await extractErrorMessage(err),
        })

        setDirectoryUploadTask(prev => ({
          ...prev,
          failedFiles: prev.failedFiles + 1,
        }))
      }
    }

    if (uploadIds.length === 0) {
      setDirectoryUploadTask(prev => ({
        ...prev,
        status: 'failed',
        failedItems,
        summaryMessage: '所有文件上传失败',
      }))
      return
    }

    // Phase 3: Batch index
    setDirectoryUploadTask(prev => ({
      ...prev,
      status: 'indexing',
      indexingFiles: uploadIds.length,
      currentFileName: '',
      currentFilePath: '',
    }))

    try {
      const indexResult = await batchIndexDocuments(knowledgeBaseId, uploadIds, 3)

      const indexedCount = indexResult.results.filter(r => r.success).length
      const indexFailedCount = indexResult.results.filter(r => !r.success).length

      setDirectoryUploadTask(prev => ({
        ...prev,
        indexedFiles: indexedCount,
        indexFailedFiles: indexFailedCount,
      }))

      // Phase 4: Polling (simplified - just mark as done)
      setDirectoryUploadTask(prev => ({
        ...prev,
        status: indexFailedCount > 0 ? 'partial-failed' : 'done',
        summaryMessage: `成功上传并索引 ${indexedCount} 个文件${indexFailedCount > 0 ? `，${indexFailedCount} 个失败` : ''}`,
      }))

      // Notify parent about document change
      onDocumentsChange?.(knowledgeBaseId)
    } catch (err) {
      const errorMsg = await extractErrorMessage(err)
      setDirectoryUploadTask(prev => ({
        ...prev,
        status: 'failed',
        summaryMessage: errorMsg,
      }))
    }
  }, [onDocumentsChange])

  const cancelDirectoryUpload = useCallback(() => {
    directoryUploadCancelRef.current = true
  }, [])

  const retryFailedUploads = useCallback(async () => {
    const failedFiles = directoryUploadPendingFilesRef.current
    if (failedFiles.length === 0 || !directoryUploadTask.knowledgeBaseId) {
      return
    }

    // Retry with failed files
    const fileList = new DataTransfer()
    failedFiles.forEach(({ file }) => fileList.items.add(file))

    await uploadDirectory(directoryUploadTask.knowledgeBaseId, fileList.files)
  }, [directoryUploadTask.knowledgeBaseId, uploadDirectory])

  // Memoize context value
  const value = useMemo<DocumentContextValue>(
    () => ({
      // Selection
      selectedDocumentId,
      selectDocument,

      // Document Detail
      documentDetail,
      documentDetailLoading,
      documentDetailError,
      documentDetailFocusChunkId,
      openDocumentDetail,
      closeDocumentDetail,

      // Document Actions
      reindexDocument,
      reindexingDocumentId,
      reindexError,
      removeDocument,

      // Directory Upload
      directoryUploadTask,
      uploadDirectory,
      cancelDirectoryUpload,
      retryFailedUploads,
    }),
    [
      selectedDocumentId,
      selectDocument,
      documentDetail,
      documentDetailLoading,
      documentDetailError,
      documentDetailFocusChunkId,
      openDocumentDetail,
      closeDocumentDetail,
      reindexDocument,
      reindexingDocumentId,
      reindexError,
      removeDocument,
      directoryUploadTask,
      uploadDirectory,
      cancelDirectoryUpload,
      retryFailedUploads,
    ]
  )

  return <DocumentContext.Provider value={value}>{children}</DocumentContext.Provider>
}
