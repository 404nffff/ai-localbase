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
