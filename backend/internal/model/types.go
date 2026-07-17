package model

import "sync"

type ServerConfig struct {
	Port                     string
	UploadDir                string
	StateFile                string
	ChatHistoryFile          string
	OperationLogFile         string
	AccessToken              string
	QdrantURL                string
	QdrantAPIKey             string
	QdrantCollectionPrefix   string
	QdrantVectorSize         int
	QdrantDistance           string
	QdrantTimeoutSeconds     int
	EnableHybridSearch       bool
	EnableSemanticReranker   bool
	EnableQueryRewrite       bool
	EnableSemanticCache      bool
	EnableContextCompression bool
	OllamaBaseURL            string
}

type AppState struct {
	Mu             sync.RWMutex
	Config         AppConfig
	KnowledgeBases map[string]KnowledgeBase
}

type HealthResponse struct {
	Status       string            `json:"status"`
	Name         string            `json:"name"`
	AuthRequired bool              `json:"auth_required"`
	Config       map[string]string `json:"config"`
}

type ChatConfig struct {
	Provider            string  `json:"provider"`
	BaseURL             string  `json:"baseUrl"`
	Model               string  `json:"model"`
	APIKey              string  `json:"apiKey"`
	Temperature         float64 `json:"temperature"`
	ContextMessageLimit int     `json:"contextMessageLimit"`
}

type EmbeddingConfig struct {
	Provider string `json:"provider"`
	BaseURL  string `json:"baseUrl"`
	Model    string `json:"model"`
	APIKey   string `json:"apiKey"`
}

type AppConfig struct {
	Chat      ChatConfig      `json:"chat"`
	Embedding EmbeddingConfig `json:"embedding"`
}

type KnowledgeBase struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Documents   []Document `json:"documents"`
	CreatedAt   string     `json:"createdAt"`
}

type KnowledgeBaseInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type Document struct {
	ID              string `json:"id"`
	KnowledgeBaseID string `json:"knowledgeBaseId"`
	Name            string `json:"name"`
	Size            int64  `json:"size"`
	SizeLabel       string `json:"sizeLabel"`
	UploadedAt      string `json:"uploadedAt"`
	Status          string `json:"status"`
	Path            string `json:"path"`
	MarkdownPath    string `json:"markdownPath"`
	ContentPreview  string `json:"contentPreview"`
	ChunkCount      int    `json:"chunkCount,omitempty"`
	IndexedAt       string `json:"indexedAt,omitempty"`
	IndexError      string `json:"indexError,omitempty"`
}

// DocumentChunkPreview 描述文档详情页可展示的单个分块，正文已由服务层限长。
type DocumentChunkPreview struct {
	ID    string `json:"id"`
	Index int    `json:"index"`
	Kind  string `json:"kind"`
	Text  string `json:"text"`
}

// DocumentIndexDiagnostics 汇总文档原文、分块和向量索引的诊断指标。
type DocumentIndexDiagnostics struct {
	RawContentChars       int  `json:"rawContentChars"`
	ChunkCount            int  `json:"chunkCount"`
	VectorCount           int  `json:"vectorCount"`
	SummaryChunkCount     int  `json:"summaryChunkCount"`
	StructuredRowCount    int  `json:"structuredRowCount"`
	RawContentAvailable   bool `json:"rawContentAvailable"`
	QdrantEnabled         bool `json:"qdrantEnabled"`
	RawContentTruncated   bool `json:"rawContentTruncated"`
	ChunkPreviewTruncated bool `json:"chunkPreviewTruncated"`
}

// DocumentDetailResponse 返回文档原文预览、摘要、分块和索引诊断信息。
type DocumentDetailResponse struct {
	KnowledgeBaseID string                   `json:"knowledgeBaseId"`
	Document        Document                 `json:"document"`
	Diagnostics     DocumentIndexDiagnostics `json:"diagnostics"`
	RawContent      string                   `json:"rawContent"`
	Summary         string                   `json:"summary"`
	Chunks          []DocumentChunkPreview   `json:"chunks"`
}

// KnowledgeBaseHealthMetrics 汇总知识库级索引健康指标。
type KnowledgeBaseHealthMetrics struct {
	DocumentCount      int    `json:"documentCount"`
	IndexedCount       int    `json:"indexedCount"`
	ProcessingCount    int    `json:"processingCount"`
	FailedCount        int    `json:"failedCount"`
	EmptyContentCount  int    `json:"emptyContentCount"`
	ChunkCount         int    `json:"chunkCount"`
	VectorCount        int    `json:"vectorCount"`
	SummaryChunkCount  int    `json:"summaryChunkCount"`
	StructuredRowCount int    `json:"structuredRowCount"`
	RawContentChars    int    `json:"rawContentChars"`
	QdrantEnabled      bool   `json:"qdrantEnabled"`
	LastIndexedAt      string `json:"lastIndexedAt,omitempty"`
}

// KnowledgeBaseDocumentHealth 描述单文档健康状态和可执行建议。
type KnowledgeBaseDocumentHealth struct {
	DocumentID          string `json:"documentId"`
	DocumentName        string `json:"documentName"`
	Status              string `json:"status"`
	IndexedAt           string `json:"indexedAt,omitempty"`
	IndexError          string `json:"indexError,omitempty"`
	ChunkCount          int    `json:"chunkCount"`
	VectorCount         int    `json:"vectorCount"`
	SummaryChunkCount   int    `json:"summaryChunkCount"`
	StructuredRowCount  int    `json:"structuredRowCount"`
	RawContentChars     int    `json:"rawContentChars"`
	RawContentAvailable bool   `json:"rawContentAvailable"`
	NeedsReindex        bool   `json:"needsReindex"`
	Recommendation      string `json:"recommendation,omitempty"`
}

// KnowledgeBaseHealthResponse 返回知识库健康评分、指标和逐文档诊断。
type KnowledgeBaseHealthResponse struct {
	KnowledgeBaseID string                        `json:"knowledgeBaseId"`
	Name            string                        `json:"name"`
	Status          string                        `json:"status"`
	Score           int                           `json:"score"`
	Metrics         KnowledgeBaseHealthMetrics    `json:"metrics"`
	Recommendations []string                      `json:"recommendations"`
	Documents       []KnowledgeBaseDocumentHealth `json:"documents"`
}

type UploadResponse struct {
	Message       string   `json:"message"`
	KnowledgeBase string   `json:"knowledgeBaseId"`
	Uploaded      Document `json:"uploaded"`
}

const (
	OperationUploadFile    = "upload_file"
	OperationIndexDocument = "index_document"
	OperationRebuildIndex  = "rebuild_index"

	OperationSourceWeb          = "web"
	OperationSourceMCP          = "mcp"
	OperationSourceAdminRebuild = "admin_rebuild"

	OperationStatusSuccess        = "success"
	OperationStatusFailed         = "failed"
	OperationStatusPartialSuccess = "partial_success"
)

type OperationLogEntry struct {
	ID                string         `json:"id"`
	CorrelationID     string         `json:"correlationId"`
	Operation         string         `json:"operation"`
	Source            string         `json:"source"`
	Status            string         `json:"status"`
	KnowledgeBaseID   string         `json:"knowledgeBaseId"`
	KnowledgeBaseName string         `json:"knowledgeBaseName"`
	DocumentID        string         `json:"documentId"`
	DocumentName      string         `json:"documentName"`
	FileSize          int64          `json:"fileSize"`
	SizeLabel         string         `json:"sizeLabel"`
	Stage             string         `json:"stage"`
	IndexStatus       string         `json:"indexStatus"`
	Message           string         `json:"message"`
	Error             string         `json:"error"`
	Metadata          map[string]any `json:"metadata"`
	StartedAt         string         `json:"startedAt"`
	FinishedAt        string         `json:"finishedAt"`
	DurationMs        int64          `json:"durationMs"`
	CreatedAt         string         `json:"createdAt"`
}

type OperationLogListQuery struct {
	KnowledgeBaseID string
	Operation       string
	Status          string
	Source          string
	Limit           int
	Offset          int
}

type OperationLogListResponse struct {
	Items  []OperationLogEntry `json:"items"`
	Total  int                 `json:"total"`
	Limit  int                 `json:"limit"`
	Offset int                 `json:"offset"`
}

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatModelConfig struct {
	Provider            string  `json:"provider"`
	BaseURL             string  `json:"baseUrl"`
	Model               string  `json:"model"`
	APIKey              string  `json:"apiKey"`
	Temperature         float64 `json:"temperature"`
	ContextMessageLimit int     `json:"contextMessageLimit"`
}

type EmbeddingModelConfig struct {
	Provider string `json:"provider"`
	BaseURL  string `json:"baseUrl"`
	Model    string `json:"model"`
	APIKey   string `json:"apiKey"`
}

type ChatCompletionRequest struct {
	ConversationID  string               `json:"conversationId"`
	Model           string               `json:"model"`
	Messages        []ChatMessage        `json:"messages"`
	KnowledgeBaseID string               `json:"knowledgeBaseId"`
	DocumentID      string               `json:"documentId"`
	Config          ChatModelConfig      `json:"config"`
	Embedding       EmbeddingModelConfig `json:"embedding"`
}

type ChatCompletionChoice struct {
	Index   int         `json:"index"`
	Message ChatMessage `json:"message"`
}

type ChatCompletionResponse struct {
	ID       string                 `json:"id"`
	Object   string                 `json:"object"`
	Created  int64                  `json:"created"`
	Model    string                 `json:"model"`
	Choices  []ChatCompletionChoice `json:"choices"`
	Metadata map[string]any         `json:"metadata"`
}

type ConfigUpdateRequest struct {
	Chat      ChatConfig      `json:"chat"`
	Embedding EmbeddingConfig `json:"embedding"`
}

type Conversation struct {
	ID              string              `json:"id"`
	Title           string              `json:"title"`
	KnowledgeBaseID string              `json:"knowledgeBaseId"`
	DocumentID      string              `json:"documentId"`
	CreatedAt       string              `json:"createdAt"`
	UpdatedAt       string              `json:"updatedAt"`
	Messages        []StoredChatMessage `json:"messages"`
}

type StoredChatMessage struct {
	ID        string         `json:"id"`
	Role      string         `json:"role"`
	Content   string         `json:"content"`
	CreatedAt string         `json:"createdAt"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

type ConversationListItem struct {
	ID              string `json:"id"`
	Title           string `json:"title"`
	KnowledgeBaseID string `json:"knowledgeBaseId"`
	DocumentID      string `json:"documentId"`
	CreatedAt       string `json:"createdAt"`
	UpdatedAt       string `json:"updatedAt"`
	MessageCount    int    `json:"messageCount"`
}

type SaveConversationRequest struct {
	ID              string              `json:"id"`
	Title           string              `json:"title"`
	KnowledgeBaseID string              `json:"knowledgeBaseId"`
	DocumentID      string              `json:"documentId"`
	Messages        []StoredChatMessage `json:"messages"`
}

type APIError struct {
	Error string `json:"error"`
}
