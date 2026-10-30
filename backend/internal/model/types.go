package model

import "sync"

type ServerConfig struct {
	Port                     string
	UploadDir                string
	StagingDir               string
	StateFile                string
	ChatHistoryFile          string
	OperationLogFile         string
	AccessToken              string
	CORSAllowedOrigins       string
	MaxUploadBytes           int64
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
	UseDefaultTemperature bool    `json:"useDefaultTemperature"` // 为 true 时不传温度，由模型自行选择默认值。
	Provider              string  `json:"provider"`
	BaseURL               string  `json:"baseUrl"`
	Model                 string  `json:"model"`
	APIKey                string  `json:"apiKey"`
	Temperature           float64 `json:"temperature"`
	ContextMessageLimit   int     `json:"contextMessageLimit"`
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
	Wiki      WikiConfig      `json:"wiki"`
}

// WikiConfig 控制知识整理，模型连接继续复用 Chat 配置。
type WikiConfig struct {
	// ModelTimeoutSeconds 是每次模型调用的上限，不含任务排队时间，范围 30–1800 秒。
	ModelTimeoutSeconds int `json:"modelTimeoutSeconds"`
	// Queue 为全站整理队列控制，仅由专用队列接口修改。
	Queue WikiQueueSettings `json:"queue"`
}

// DefaultWikiModelTimeoutSeconds 用于新安装及缺少 Wiki 配置的旧状态。
const DefaultWikiModelTimeoutSeconds = 180

type KnowledgeBase struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Documents   []Document `json:"documents"`
	CreatedAt   string     `json:"createdAt"`
	// Wiki 为可选知识维护状态，历史库缺省关闭。
	Wiki *WikiState `json:"wiki,omitempty"`
}

type KnowledgeBaseInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type Document struct {
	// IndexGeneration 仅发布成功的向量代际可参与检索。
	IndexGeneration string `json:"indexGeneration,omitempty"`
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
	IndexErrorCode  string `json:"indexErrorCode,omitempty"`
	ContentChecksum string `json:"contentChecksum,omitempty"`
	// Kind 缺省为原始资料；wiki 文档通过版本发布维护。
	Kind            string            `json:"kind,omitempty"`
	SourceRevisions []SourceRevision  `json:"sourceRevisions,omitempty"`
	Wiki            *WikiPageMetadata `json:"wiki,omitempty"`
}

// SourceRevision 保存原始资料的只读正文版本，不包含连接配置。
type SourceRevision struct {
	Checksum     string `json:"checksum"`
	SnapshotPath string `json:"snapshotPath,omitempty"`
	CreatedAt    string `json:"createdAt"`
}

// WikiSourceRef 将页面结论定位到原始资料版本和可核实引文。
type WikiSourceRef struct {
	DocumentID string `json:"documentId"`
	// Checksum 基于提取后的完整正文；Quote 必须是该版本中的连续原文。
	Checksum string `json:"checksum"`
	// Anchor 可选，仅用于定位提示，不代替引文校验。
	Anchor string `json:"anchor,omitempty"`
	Quote  string `json:"quote"`
}

// WikiRevision 记录已发布页面历史；正文保存在独立 Markdown 文件。
type WikiRevision struct {
	Revision   int             `json:"revision"`
	Path       string          `json:"path"`
	Title      string          `json:"title"`
	SourceRefs []WikiSourceRef `json:"sourceRefs"`
	Links      []string        `json:"links"`
	CreatedAt  string          `json:"createdAt"`
}

// WikiPageMetadata 保存知识页身份、证据、修订和索引可用性。
type WikiPageMetadata struct {
	PageID string `json:"pageId"`
	Slug   string `json:"slug"`
	// PageType 为 topic、decision、source-summary 或 question；无来源只允许 question。
	PageType string   `json:"pageType"`
	Aliases  []string `json:"aliases"`
	Revision int      `json:"revision"`
	// IndexedRevision 等于 Revision 才表示当前正文完成索引，零表示尚未索引。
	IndexedRevision int             `json:"indexedRevision"`
	SourceRefs      []WikiSourceRef `json:"sourceRefs"`
	Links           []string        `json:"links"`
	// Freshness 为 fresh、stale 或 missing-source；最后两者不能作为默认问答证据。
	Freshness string         `json:"freshness"`
	UpdatedAt string         `json:"updatedAt"`
	History   []WikiRevision `json:"history"`
}

// WikiState 是库内可恢复维护状态，凭证只从运行配置读取。
type WikiState struct {
	Enabled      bool        `json:"enabled"`
	RulesVersion string      `json:"rulesVersion"`
	Jobs         []WikiJob   `json:"jobs"`
	Issues       []WikiIssue `json:"issues"`
	// PendingSources 将原始资料 ID 映射到待整理正文校验和，用于恢复入库后的排队缺口。
	PendingSources map[string]string `json:"pendingSources"`
}

// WikiJob 记录持久任务输入和阶段；发布后的重试只补索引。
type WikiJob struct {
	// ModelTimeoutSeconds 记录本次执行的超时快照；显式重试时读取最新配置。
	ModelTimeoutSeconds int    `json:"modelTimeoutSeconds,omitempty"`
	ID                  string `json:"id"`
	KnowledgeBaseID     string `json:"knowledgeBaseId"`
	Kind                string `json:"kind"`
	// RequestKey 标识客户端动作；InputFingerprint 标识同库、同版本及规则下的整理输入。
	RequestKey       string   `json:"requestKey"`
	InputFingerprint string   `json:"inputFingerprint"`
	DocumentIDs      []string `json:"documentIds"`
	// SourceVersions 固定来源校验和；BaseRevisions 在实际读取页面时刷新，发布时复核。
	SourceVersions map[string]string `json:"sourceVersions"`
	BaseRevisions  map[string]int    `json:"baseRevisions"`
	// Status 表示 queued/running/succeeded/failed/needs_split/canceled/partial。
	Status string `json:"status"`
	Stage  string `json:"stage"`
	// Attempt 和 ModelCalls 累计记录执行及模型调用次数，不因手动重试清零或限制重试。
	Attempt    int `json:"attempt"`
	ModelCalls int `json:"modelCalls"`
	// ModelCallLimit 为本轮允许达到的累计调用数；在历史调用数上追加本轮来源规模预算。
	ModelCallLimit    int `json:"modelCallLimit,omitempty"`
	TotalSegments     int `json:"totalSegments"`
	ProcessedSegments int `json:"processedSegments"`
	// Extracts 保留已成功提取的分段事实，失败重试不重复处理已完成分段。
	Extracts       map[string]string `json:"extracts,omitempty"`
	ChangedPageIDs []string          `json:"changedPageIds"`
	Error          string            `json:"error,omitempty"`
	Semantic       bool              `json:"semantic"`
	// CheckedPages/TotalPages 描述本次读取覆盖；SemanticStatus 不得把部分结果表示为全库完成。
	CheckedPages   int    `json:"checkedPages"`
	TotalPages     int    `json:"totalPages"`
	SemanticStatus string `json:"semanticStatus,omitempty"`
	CaptureText    string `json:"captureText,omitempty"`
	// TargetPageID 非空表示将保存回答沉淀到指定知识页；空值由整理计划决定页面。
	TargetPageID string `json:"targetPageId,omitempty"`
	CreatedAt    string `json:"createdAt"`
	UpdatedAt    string `json:"updatedAt"`
}

// WikiIssue 保存内容问题与处理理由；疑似矛盾不是事实裁决。
type WikiIssue struct {
	// EvidenceFingerprint 包含相关页面和库内来源版本，同证据保留人工处理状态。
	EvidenceFingerprint string          `json:"evidenceFingerprint"`
	ID                  string          `json:"id"`
	Type                string          `json:"type"`
	PageIDs             []string        `json:"pageIds"`
	SourceRefs          []WikiSourceRef `json:"sourceRefs"`
	Severity            string          `json:"severity"`
	Status              string          `json:"status"`
	Reason              string          `json:"reason"`
}

// WikiPageChange 是模型提出的页面更新，发布前核对来源和版本。
type WikiPageChange struct {
	PageID string `json:"pageId"`
	// Key 是同批新页面互链的临时标识，发布时统一解析为服务端分配的 PageID。
	Key          string          `json:"key,omitempty"`
	PageType     string          `json:"pageType"`
	Title        string          `json:"title"`
	Aliases      []string        `json:"aliases"`
	BaseRevision int             `json:"baseRevision"`
	Markdown     string          `json:"markdown"`
	SourceRefs   []WikiSourceRef `json:"sourceRefs"`
	Links        []string        `json:"links"`
}

// WikiPlan 是一次完整发布的页面与问题集合。
type WikiPlan struct {
	Pages  []WikiPageChange `json:"pages"`
	Issues []WikiIssue      `json:"issues"`
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
	// VectorCountSource 区分 actual、unknown 与 not_applicable；仅 actual 的数量可展示为精确值。
	VectorCountSource     string `json:"vectorCountSource"`
	VectorCountStatus     string `json:"vectorCountStatus"`
	RawContentChars       int    `json:"rawContentChars"`
	ChunkCount            int    `json:"chunkCount"`
	VectorCount           int    `json:"vectorCount"`
	SummaryChunkCount     int    `json:"summaryChunkCount"`
	StructuredRowCount    int    `json:"structuredRowCount"`
	RawContentAvailable   bool   `json:"rawContentAvailable"`
	QdrantEnabled         bool   `json:"qdrantEnabled"`
	RawContentTruncated   bool   `json:"rawContentTruncated"`
	ChunkPreviewTruncated bool   `json:"chunkPreviewTruncated"`
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
	// 任一文档计数未知时，汇总同样标记 unknown，不能将部分数量当作总数。
	VectorCountSource  string `json:"vectorCountSource"`
	VectorCountStatus  string `json:"vectorCountStatus"`
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
	VectorCountSource   string `json:"vectorCountSource"` // actual 为远端实测，unknown 为查询未完成。
	VectorCountStatus   string `json:"vectorCountStatus"` // 集合、维度或连接异常的稳定状态。
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
	UseDefaultTemperature bool    `json:"useDefaultTemperature"` // 普通聊天、Wiki和模型测试共用的温度传参模式。
	Provider              string  `json:"provider"`
	BaseURL               string  `json:"baseUrl"`
	Model                 string  `json:"model"`
	APIKey                string  `json:"apiKey"`
	Temperature           float64 `json:"temperature"`
	ContextMessageLimit   int     `json:"contextMessageLimit"`
}

type EmbeddingModelConfig struct {
	Provider string `json:"provider"`
	BaseURL  string `json:"baseUrl"`
	Model    string `json:"model"`
	APIKey   string `json:"apiKey"`
}

type ChatCompletionRequest struct {
	KnowledgeScope  string               `json:"knowledgeScope"` // none 不检索、selected 指定范围、all 显式全库。
	ConversationID  string               `json:"conversationId"`
	Model           string               `json:"model"`
	Messages        []ChatMessage        `json:"messages"`
	KnowledgeBaseID string               `json:"knowledgeBaseId"`
	DocumentID      string               `json:"documentId"`
	Config          ChatModelConfig      `json:"config"`
	Embedding       EmbeddingModelConfig `json:"embedding"`
	// SingleAttempt 仅供内部 Wiki 预算控制，外部请求不能覆盖模型重试策略。
	SingleAttempt bool `json:"-"`
}

// WikiJobRequest 定义整理、体检和沉淀的业务输入，不允许传入模型连接。
type WikiJobRequest struct {
	// ExpectedVersions 仅供保存回答的证据校验使用，不接受客户端覆盖。
	ExpectedVersions map[string]string `json:"-"`
	Kind             string            `json:"kind"`
	DocumentIDs      []string          `json:"documentIds"`
	RequestKey       string            `json:"requestKey"`
	Semantic         bool              `json:"semantic"`
	CaptureText      string            `json:"-"`
	TargetPageID     string            `json:"targetPageId,omitempty"`
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
	// Wiki 缺省时保留当前设置，兼容仅保存模型连接的客户端。
	Wiki *WikiConfig `json:"wiki,omitempty"`
}

type Conversation struct {
	KnowledgeScope  string              `json:"knowledgeScope"` // 保存显式全库选择，避免重启后与普通聊天混淆。
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
	KnowledgeScope  string `json:"knowledgeScope"`
	ID              string `json:"id"`
	Title           string `json:"title"`
	KnowledgeBaseID string `json:"knowledgeBaseId"`
	DocumentID      string `json:"documentId"`
	CreatedAt       string `json:"createdAt"`
	UpdatedAt       string `json:"updatedAt"`
	MessageCount    int    `json:"messageCount"`
}

type SaveConversationRequest struct {
	KnowledgeScope  string              `json:"knowledgeScope"`
	ID              string              `json:"id"`
	Title           string              `json:"title"`
	KnowledgeBaseID string              `json:"knowledgeBaseId"`
	DocumentID      string              `json:"documentId"`
	Messages        []StoredChatMessage `json:"messages"`
}

type APIError struct {
	Error string `json:"error"`
}

type TestChatModelRequest struct {
	UseDefaultTemperature bool    `json:"useDefaultTemperature"` // 探测使用当前草稿的默认温度选项。
	Provider              string  `json:"provider"`
	BaseURL               string  `json:"baseUrl"`
	Model                 string  `json:"model"`
	APIKey                string  `json:"apiKey"`
	Temperature           float64 `json:"temperature"`
}

type TestEmbeddingModelRequest struct {
	Provider string `json:"provider"`
	BaseURL  string `json:"baseUrl"`
	Model    string `json:"model"`
	APIKey   string `json:"apiKey"`
}

type TestModelResponse struct {
	Success      bool   `json:"success"`
	LatencyMs    int64  `json:"latencyMs,omitempty"`
	ErrorMessage string `json:"errorMessage,omitempty"`
	VectorSize   int    `json:"vectorSize,omitempty"`
	ModelInfo    string `json:"modelInfo,omitempty"`
}

type EditMessageRequest struct {
	Content string `json:"content"`
}

type RetrievalDebugRequest struct {
	Query           string `json:"query"`
	KnowledgeBaseID string `json:"knowledgeBaseId,omitempty"`
	DocumentID      string `json:"documentId,omitempty"`
}

type RetrievalDebugChunk struct {
	DocumentID   string  `json:"documentId"`
	DocumentName string  `json:"documentName"`
	ChunkID      string  `json:"chunkId"`
	Text         string  `json:"text"`
	Score        float64 `json:"score"`
	Index        int     `json:"index"`
}

type RetrievalDebugResponse struct {
	Query  string                `json:"query"`
	Count  int                   `json:"count"`
	Chunks []RetrievalDebugChunk `json:"chunks"`
}

type StagedUpload struct {
	ID        string `json:"id"`
	FileName  string `json:"fileName"`
	Size      int64  `json:"size"`
	SizeLabel string `json:"sizeLabel"`
	CreatedAt string `json:"createdAt"`
}

type StageUploadResponse struct {
	Message  string       `json:"message"`
	UploadID string       `json:"uploadId"`
	Staged   StagedUpload `json:"staged"`
}

type BatchIndexRequest struct {
	UploadIDs []string `json:"uploadIds"`
	Async     bool     `json:"async"`
}

type IndexJobItem struct {
	UploadID   string `json:"uploadId"`
	FileName   string `json:"fileName"`
	DocumentID string `json:"documentId,omitempty"`
	Success    bool   `json:"success"`
	ErrorCode  string `json:"errorCode,omitempty"`
	Error      string `json:"error,omitempty"`
}

type IndexJob struct {
	ID              string         `json:"id"`
	KnowledgeBaseID string         `json:"knowledgeBaseId"`
	Status          string         `json:"status"`
	Total           int            `json:"total"`
	Completed       int            `json:"completed"`
	Failed          int            `json:"failed"`
	CurrentFileName string         `json:"currentFileName,omitempty"`
	Error           string         `json:"error,omitempty"`
	Items           []IndexJobItem `json:"items,omitempty"`
	CreatedAt       string         `json:"createdAt"`
	UpdatedAt       string         `json:"updatedAt"`
}

// WikiQueueSettings 是当前进程的全站任务限额；暂停保留队列并允许已领取任务结束。
type WikiQueueSettings struct {
	Paused      bool `json:"paused"`      // 是否停止领取新任务，不取消运行中任务。
	Concurrency int  `json:"concurrency"` // 同时执行任务数，范围1–20；同库仍串行。
}

// WikiQueueUpdate 仅修改明确提交的字段，避免暂停按钮覆盖另一用户保存的并发数。
type WikiQueueUpdate struct {
	Paused      *bool `json:"paused"`      // 省略时保留当前暂停状态。
	Concurrency *int  `json:"concurrency"` // 省略时保留当前并发上限。
}
