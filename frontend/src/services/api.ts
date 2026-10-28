import type {
  AppConfig,
  ChatConfig,
  Conversation,
  DocumentDetailResponse,
  DocumentItem,
  EmbeddingConfig,
  KnowledgeBase,
  KnowledgeBaseHealthResponse,
  OperationLogFilters,
  OperationLogListResponse,
} from '../App'

export const API_BASE_PATH = ''
export const UNAUTHORIZED_ERROR_MESSAGE = 'unauthorized'

interface ApiErrorResponse {
  error?: string
}

let accessToken = ''

export const setApiAccessToken = (token: string) => {
  accessToken = token.trim()
}

export const getApiAccessToken = () => accessToken

const buildRequestHeaders = (headers?: HeadersInit, accessTokenOverride?: string) => {
  const nextHeaders = new Headers(headers)
  const normalizedAccessToken = (accessTokenOverride ?? accessToken).trim()
  if (normalizedAccessToken) {
    nextHeaders.set('Authorization', `Bearer ${normalizedAccessToken}`)
  }
  return nextHeaders
}

export const apiFetch = (path: string, init: RequestInit = {}, accessTokenOverride?: string) => {
  const nextHeaders = buildRequestHeaders(init.headers, accessTokenOverride)
  return fetch(`${API_BASE_PATH}${path}`, {
    ...init,
    headers: nextHeaders,
  })
}

export const extractErrorMessage = async (response: Response) => {
  try {
    const errorBody = (await response.json()) as ApiErrorResponse
    return errorBody.error || '请求失败'
  } catch {
    return '请求失败'
  }
}

const ensureOk = async (response: Response) => {
  if (response.ok) {
    return response
  }
  if (response.status === 401) {
    throw new Error(UNAUTHORIZED_ERROR_MESSAGE)
  }
  throw new Error(await extractErrorMessage(response))
}

export interface TestModelResponse {
  success: boolean
  latencyMs?: number
  errorMessage?: string
  vectorSize?: number
  modelInfo?: string
}

export interface StagedUpload {
  id: string
  fileName: string
  size: number
  sizeLabel: string
  createdAt: string
}

export interface IndexJobItem {
  uploadId: string
  fileName: string
  documentId?: string
  success: boolean
  errorCode?: string
  error?: string
}

export interface IndexJob {
  id: string
  knowledgeBaseId: string
  status: string
  total: number
  completed: number
  failed: number
  currentFileName?: string
  error?: string
  items?: IndexJobItem[]
  createdAt: string
  updatedAt: string
}

export interface RetrievalDebugChunk {
  documentId: string
  documentName: string
  chunkId: string
  text: string
  score: number
  index: number
}

export interface RetrievalDebugResponse {
  query: string
  count: number
  chunks: RetrievalDebugChunk[]
}

export const testChatModel = async (config: ChatConfig) => {
  const response = await ensureOk(await apiFetch('/api/config/test-chat-model', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      provider: config.provider,
      baseUrl: config.baseUrl,
      model: config.model,
      apiKey: config.apiKey,
      temperature: config.temperature,
      useDefaultTemperature: config.useDefaultTemperature === true,
    }),
  }))
  return (await response.json()) as TestModelResponse
}

export const testEmbeddingModel = async (config: EmbeddingConfig) => {
  const response = await ensureOk(await apiFetch('/api/config/test-embedding-model', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      provider: config.provider,
      baseUrl: config.baseUrl,
      model: config.model,
      apiKey: config.apiKey,
    }),
  }))
  return (await response.json()) as TestModelResponse
}

export const fetchKnowledgeBaseHealth = async (knowledgeBaseId: string) => {
  const response = await ensureOk(await apiFetch(
    `/api/knowledge-bases/${encodeURIComponent(knowledgeBaseId)}/health`,
  ))
  return (await response.json()) as KnowledgeBaseHealthResponse
}

export const fetchDocumentDetail = async (knowledgeBaseId: string, documentId: string) => {
  const response = await ensureOk(await apiFetch(
    `/api/knowledge-bases/${encodeURIComponent(knowledgeBaseId)}/documents/${encodeURIComponent(documentId)}`,
  ))
  return (await response.json()) as DocumentDetailResponse
}

export const reindexKnowledgeBaseDocument = async (knowledgeBaseId: string, documentId: string) => {
  const response = await ensureOk(await apiFetch(
    `/api/knowledge-bases/${encodeURIComponent(knowledgeBaseId)}/documents/${encodeURIComponent(documentId)}/reindex`,
    { method: 'POST' },
  ))
  const result = (await response.json()) as { document: DocumentItem }
  return result.document
}

export const stageUpload = async (file: File) => {
  const formData = new FormData()
  formData.append('file', file)
  const response = await ensureOk(await apiFetch('/api/uploads', {
    method: 'POST',
    body: formData,
  }))
  const result = (await response.json()) as { uploadId: string; staged: StagedUpload }
  return result
}

export const startBatchIndex = async (knowledgeBaseId: string, uploadIds: string[]) => {
  const response = await ensureOk(await apiFetch(
    `/api/knowledge-bases/${encodeURIComponent(knowledgeBaseId)}/documents/batch-index`,
    {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ uploadIds, async: true }),
    },
  ))
  const result = (await response.json()) as { job: IndexJob }
  return result.job
}

export const getIndexJob = async (jobId: string) => {
  const response = await ensureOk(await apiFetch(`/api/jobs/${encodeURIComponent(jobId)}`))
  const result = (await response.json()) as { job: IndexJob }
  return result.job
}

export const cancelIndexJob = async (jobId: string) => {
  const response = await ensureOk(await apiFetch(`/api/jobs/${encodeURIComponent(jobId)}/cancel`, {
    method: 'POST',
  }))
  const result = (await response.json()) as { job: IndexJob }
  return result.job
}

export const retryIndexJob = async (jobId: string) => {
  const response = await ensureOk(await apiFetch(`/api/jobs/${encodeURIComponent(jobId)}/retry`, {
    method: 'POST',
  }))
  const result = (await response.json()) as { job: IndexJob }
  return result.job
}

export const debugKnowledgeBaseRetrieval = async (
  knowledgeBaseId: string,
  query: string,
  documentId?: string,
) => {
  const response = await ensureOk(await apiFetch(
    `/api/knowledge-bases/${encodeURIComponent(knowledgeBaseId)}/retrieval/debug`,
    {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ query, documentId: documentId ?? '' }),
    },
  ))
  return (await response.json()) as RetrievalDebugResponse
}

export const editConversationMessage = async (
  conversationId: string,
  messageId: string,
  content: string,
) => {
  const response = await ensureOk(await apiFetch(
    `/api/conversations/${encodeURIComponent(conversationId)}/messages/${encodeURIComponent(messageId)}`,
    {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ content }),
    },
  ))
  const result = (await response.json()) as { conversation: Conversation }
  return result.conversation
}

export const deleteConversationMessage = async (conversationId: string, messageId: string) => {
  const response = await ensureOk(await apiFetch(
    `/api/conversations/${encodeURIComponent(conversationId)}/messages/${encodeURIComponent(messageId)}`,
    { method: 'DELETE' },
  ))
  const result = (await response.json()) as { conversation: Conversation }
  return result.conversation
}

export const regenerateConversationMessage = async (conversationId: string, messageId: string) => {
  const response = await ensureOk(await apiFetch(
    `/api/conversations/${encodeURIComponent(conversationId)}/messages/${encodeURIComponent(messageId)}/regenerate`,
    { method: 'POST' },
  ))
  const result = (await response.json()) as { conversation: Conversation }
  return result.conversation
}

export const exportConversationMarkdown = async (conversationId: string) => {
  const response = await ensureOk(await apiFetch(
    `/api/conversations/${encodeURIComponent(conversationId)}/export`,
  ))
  const blob = await response.blob()
  return {
    blob,
    fileName: response.headers.get('Content-Disposition'),
  }
}

export const fetchOperationLogs = async (filters: OperationLogFilters) => {
  const params = new URLSearchParams()
  params.set('limit', '50')
  params.set('offset', '0')
  if (filters.knowledgeBaseId) params.set('knowledgeBaseId', filters.knowledgeBaseId)
  if (filters.operation) params.set('operation', filters.operation)
  if (filters.status) params.set('status', filters.status)
  if (filters.source) params.set('source', filters.source)
  const response = await ensureOk(await apiFetch(`/api/operation-logs?${params.toString()}`))
  return (await response.json()) as OperationLogListResponse
}

export const fetchAppConfig = async (accessTokenOverride?: string) => {
  const response = await ensureOk(await apiFetch('/api/config', {}, accessTokenOverride))
  return (await response.json()) as AppConfig
}

export const fetchKnowledgeBases = async (accessTokenOverride?: string) => {
  const response = await ensureOk(await apiFetch('/api/knowledge-bases', {}, accessTokenOverride))
  return (await response.json()) as { items: KnowledgeBase[] }
}

/** 原始资料的固定版本引用，用于查看和重新核对回答证据。 */
export interface WikiSourceRef { documentId: string; checksum: string; quote: string; anchor?: string }
/** 知识页摘要；正文通过分页接口按需读取。 */
export interface WikiPage {
  id: string
  name: string
  wiki: { pageType: string; freshness: string; revision: number; indexedRevision: number; updatedAt: string; sourceRefs: WikiSourceRef[]; history?: WikiRevision[] }
}
/** 页面已发布的历史修订；正文按所选版本单独读取。 */
export interface WikiRevision { revision: number; title: string; createdAt: string }
/** 全库实际发布记录，点击后读取记录对应的版本，来源数按文档去重。 */
export interface WikiChange {
  knowledgeBaseId: string
  knowledgeBaseName: string
  pageId: string
  title: string
  revision: number
  createdAt: string
  sourceCount: number
}

/** 全库历史通过服务端统一排序分页，避免只拼接各库首页造成遗漏。 */
export async function fetchWikiChanges(knowledgeBaseId: string, offset: number, signal?: AbortSignal): Promise<WikiList<WikiChange>> {
  const params = new URLSearchParams({ knowledgeBaseId, offset: String(offset), limit: '20' })
  const response = await ensureOk(await apiFetch(`/api/wiki/changes?${params}`, { signal }))
  return response.json() as Promise<WikiList<WikiChange>>
}
/** 持久整理任务及实际覆盖范围，不包含模型连接信息。 */
export interface WikiJob {
  id: string; kind: string; status: string; stage: string; attempt: number
  error?: string; totalSegments: number; processedSegments: number
  totalPages: number; checkedPages: number; semanticStatus?: string; updatedAt: string
}
/** 内容疑点及处理理由，疑点不代表事实已经判定。 */
export interface WikiIssue { id: string; type: string; pageIds: string[]; status: string; reason: string }
/** 当前知识库的知识维护概况。 */
export interface WikiOverview {
  /** 本库保存的自动整理开关，显式关闭优先于默认值。 */
  enabled: boolean
  /** 第三方聊天配置是否完整；为 true 不代表已探测连接成功。 */
  autoCompileReady: boolean
  /** 知识页、失效页与未处理问题的数量。 */
  counts: Record<string, number>
  /** 尚未完成整理的来源版本数量。 */
  pendingCount: number
  /** 最近任务及其实际执行状态。 */
  jobs: WikiJob[]
}
/** 统一分页列表，空游标表示末页。 */
export interface WikiList<T> { items: T[]; total: number; nextCursor: string }
/** 正文按 Unicode 码点分页，nextOffset 为零表示已完整读取。 */
export interface WikiPageContent {
  pageId: string; title: string; revision: number; markdown: string
  metadata: WikiPage['wiki']; sourceRefs: WikiSourceRef[]; links: string[]
  missingSources: string[]; totalChars: number; nextOffset: number
}
/** 原始资料历史版本的正文片段。 */
export interface WikiSourceContent { name: string; content: string; nextOffset: number; totalChars: number }

/** 调用单库 Wiki 接口，复用既有鉴权和错误语义；请求失败直接抛出。 */
export async function wikiRequest<T>(knowledgeBaseId: string, path = '', method = 'GET', body?: unknown, signal?: AbortSignal): Promise<T> {
  const response = await ensureOk(await apiFetch(`/api/knowledge-bases/${encodeURIComponent(knowledgeBaseId)}/wiki${path}`, {
    method, signal,
    headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  }))
  return response.json() as Promise<T>
}

/** 导出 Markdown ZIP，通过带鉴权的请求获取文件。 */
export async function downloadWiki(knowledgeBaseId: string): Promise<void> {
  const response = await ensureOk(await apiFetch(`/api/knowledge-bases/${encodeURIComponent(knowledgeBaseId)}/wiki/export`))
  const url = URL.createObjectURL(await response.blob())
  const anchor = document.createElement('a')
  anchor.href = url
  anchor.download = 'wiki.zip'
  anchor.click()
  URL.revokeObjectURL(url)
}

/** 单库全部任务统计；待整理资料数量与排队任务数量分别展示。 */
export interface WikiLibraryProgress {
  knowledgeBaseId: string
  name: string
  enabled: boolean
  pendingCount: number
  pageCount: number
  total: number
  counts: Record<string, number>
  updatedAt: string
}

/** 全库汇总与指定库的任务分页，统计不受筛选和分页限制。 */
export interface WikiProgressResponse {
  items: WikiLibraryProgress[]
  autoCompileReady: boolean
  jobs: WikiList<WikiJob>
}

/** 读取整理总进度；可通过查询参数指定库、状态和偏移，保留服务端失败信息。 */
export async function fetchWikiProgress(params: URLSearchParams, signal?: AbortSignal): Promise<WikiProgressResponse> {
  const response = await ensureOk(await apiFetch(`/api/wiki/progress?${params}`, { signal }))
  return response.json() as Promise<WikiProgressResponse>
}

/** 本库失败任务批量提交结果；submitted 只表示已排队，remaining 为中断后未提交项。 */
export interface WikiRetryBatchResult {
  total: number
  submitted: number
  skipped: number
  remaining: number
  failures: Array<{ jobId: string; error: string }>
}
