package router

import (
	"net/http"
	"strings"

	"ai-localbase/internal/handler"
	"ai-localbase/internal/model"
	"ai-localbase/internal/util"

	"github.com/gin-gonic/gin"
)

const defaultJSONBodyLimit int64 = 4 * 1024 * 1024

func NewRouter(appHandler *handler.AppHandler, mcpHandler *handler.MCPHandler, serverConfig model.ServerConfig) *gin.Engine {
	r := gin.New()
	// 本地开发场景仅信任回环地址代理，避免 Gin 输出“trust all proxies”警告。
	_ = r.SetTrustedProxies([]string{"127.0.0.1", "::1"})
	maxUploadBytes := serverConfig.MaxUploadBytes
	if maxUploadBytes <= 0 {
		maxUploadBytes = 25 * 1024 * 1024
	}
	r.MaxMultipartMemory = maxUploadBytes
	r.Use(gin.Logger(), gin.Recovery(), requestIDMiddleware(), corsMiddleware(serverConfig.CORSAllowedOrigins), jsonBodyLimitMiddleware(defaultJSONBodyLimit))

	r.GET("/", appHandler.Root)
	r.GET("/health", appHandler.Health)
	r.GET("/livez", appHandler.Liveness)
	r.GET("/readyz", appHandler.Readiness)
	// 兼容上传入口也会写入文件和索引，沿用 API 的令牌校验规则。
	r.POST("/upload", authMiddleware(serverConfig.AccessToken), appHandler.Upload)

	api := r.Group("/api")
	api.Use(authMiddleware(serverConfig.AccessToken))
	{
		api.GET("/auth/verify", appHandler.VerifyAccessToken)
		api.GET("/config", appHandler.GetConfig)
		api.PUT("/config", appHandler.UpdateConfig)
		api.POST("/config/test-chat-model", appHandler.TestChatModel)
		api.POST("/config/test-embedding-model", appHandler.TestEmbeddingModel)
		api.GET("/conversations", appHandler.ListConversations)
		api.GET("/conversations/:id", appHandler.GetConversation)
		api.PUT("/conversations/:id", appHandler.SaveConversation)
		api.DELETE("/conversations/:id", appHandler.DeleteConversation)
		api.PUT("/conversations/:id/messages/:msgId", appHandler.EditMessage)
		api.DELETE("/conversations/:id/messages/:msgId", appHandler.DeleteMessage)
		api.POST("/conversations/:id/messages/:msgId/regenerate", appHandler.RegenerateMessage)
		api.GET("/conversations/:id/export", appHandler.ExportConversation)
		api.GET("/knowledge-bases", appHandler.ListKnowledgeBases)
		api.POST("/knowledge-bases", appHandler.CreateKnowledgeBase)
		api.DELETE("/knowledge-bases/:id", appHandler.DeleteKnowledgeBase)
		api.GET("/knowledge-bases/:id/export", appHandler.ExportKnowledgeBase)
		api.GET("/knowledge-bases/:id/health", appHandler.GetKnowledgeBaseHealth)
		wiki := api.Group("/knowledge-bases/:id/wiki")
		wiki.GET("", appHandler.WikiGet)
		wiki.PUT("", appHandler.WikiConfigure)
		wiki.POST("/jobs", appHandler.WikiCreateJob)
		wiki.GET("/jobs/:jobId", appHandler.WikiGetJob)
		wiki.POST("/jobs/:jobId/retry", appHandler.WikiRetryJob)
		wiki.GET("/pages", appHandler.WikiListPages)
		wiki.GET("/pages/:pageId", appHandler.WikiGetPage)
		wiki.GET("/sources/:documentId/revisions/:checksum", appHandler.WikiGetSource)
		wiki.POST("/capture", appHandler.WikiCapture)
		wiki.GET("/issues", appHandler.WikiListIssues)
		wiki.PATCH("/issues/:issueId", appHandler.WikiUpdateIssue)
		wiki.GET("/export", appHandler.WikiExport)
		api.POST("/knowledge-bases/:id/retrieval/debug", appHandler.DebugRetrieve)
		api.GET("/knowledge-bases/:id/documents", appHandler.ListDocuments)
		api.POST("/knowledge-bases/:id/documents", appHandler.UploadToKnowledgeBase)
		api.POST("/knowledge-bases/:id/documents/batch-index", appHandler.BatchIndexDocuments)
		api.GET("/knowledge-bases/:id/documents/:documentId", appHandler.GetDocumentDetail)
		api.POST("/knowledge-bases/:id/documents/:documentId/reindex", appHandler.ReindexDocument)
		api.DELETE("/knowledge-bases/:id/documents/:documentId", appHandler.DeleteDocument)
		api.POST("/uploads", appHandler.StageUpload)
		api.GET("/jobs/:jobId", appHandler.GetJobStatus)
		api.POST("/jobs/:jobId/cancel", appHandler.CancelJob)
		api.POST("/jobs/:jobId/retry", appHandler.RetryJob)
		api.GET("/operation-logs", appHandler.ListOperationLogs)
		api.POST("/admin/rebuild-qdrant-index", appHandler.RebuildQdrantIndex)
		api.POST("/mcp", mcpHandler.Handle)
		api.POST("/mcp/tools/:name/call", mcpHandler.CallTool)
	}

	v1 := r.Group("/v1")
	v1.Use(authMiddleware(serverConfig.AccessToken))
	{
		v1.POST("/chat/completions", appHandler.ChatCompletions)
		v1.POST("/chat/completions/stream", appHandler.ChatCompletionsStream)
	}

	mcp := r.Group("/mcp")
	mcp.Use(authMiddleware(serverConfig.AccessToken))
	{
		mcp.POST("", mcpHandler.Handle)
	}

	return r
}

func requestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := strings.TrimSpace(c.GetHeader("X-Request-Id"))
		if requestID == "" {
			requestID = util.NextID("req")
		}
		c.Header("X-Request-Id", requestID)
		c.Next()
	}
}

func corsMiddleware(allowedOrigins string) gin.HandlerFunc {
	allowed := map[string]struct{}{}
	for _, origin := range strings.Split(allowedOrigins, ",") {
		origin = strings.TrimSpace(origin)
		if origin != "" {
			allowed[origin] = struct{}{}
		}
	}
	return func(c *gin.Context) {
		origin := strings.TrimSpace(c.GetHeader("Origin"))
		if origin != "" {
			if _, ok := allowed[origin]; ok {
				c.Header("Access-Control-Allow-Origin", origin)
				c.Header("Vary", "Origin")
				c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Request-Id")
				c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			}
		}

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}

func jsonBodyLimitMiddleware(limit int64) gin.HandlerFunc {
	if limit <= 0 {
		limit = defaultJSONBodyLimit
	}
	return func(c *gin.Context) {
		if c.Request == nil {
			c.Next()
			return
		}
		switch c.Request.Method {
		case http.MethodPost, http.MethodPut, http.MethodPatch:
		default:
			c.Next()
			return
		}
		contentType := strings.ToLower(strings.TrimSpace(c.GetHeader("Content-Type")))
		if strings.HasPrefix(contentType, "multipart/") {
			c.Next()
			return
		}
		if c.Request.ContentLength > limit {
			c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, model.APIError{Error: "request body is too large"})
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
		c.Next()
	}
}

func authMiddleware(accessToken string) gin.HandlerFunc {
	// 未配置应用访问令牌时保持现有无鉴权行为，避免破坏默认本地体验。
	expectedToken := strings.TrimSpace(accessToken)
	if expectedToken == "" {
		return func(c *gin.Context) {
			c.Next()
		}
	}

	return func(c *gin.Context) {
		authHeader := strings.TrimSpace(c.GetHeader("Authorization"))
		token := strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
		if !strings.HasPrefix(authHeader, "Bearer ") || token == "" || token != expectedToken {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "unauthorized",
			})
			return
		}
		c.Next()
	}
}
