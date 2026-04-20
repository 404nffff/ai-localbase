package router

import (
	"net/http"
	"strings"

	"ai-localbase/internal/handler"

	"github.com/gin-gonic/gin"
)

func NewRouter(appHandler *handler.AppHandler, mcpHandler *handler.MCPHandler, accessToken string) *gin.Engine {
	r := gin.New()
	// 本地开发场景仅信任回环地址代理，避免 Gin 输出“trust all proxies”警告。
	_ = r.SetTrustedProxies([]string{"127.0.0.1", "::1"})
	r.Use(gin.Logger(), gin.Recovery(), corsMiddleware())

	r.GET("/", appHandler.Root)
	r.GET("/health", appHandler.Health)
	r.POST("/upload", appHandler.Upload)

	api := r.Group("/api")
	api.Use(authMiddleware(accessToken))
	{
		api.GET("/auth/verify", appHandler.VerifyAccessToken)
		api.GET("/config", appHandler.GetConfig)
		api.PUT("/config", appHandler.UpdateConfig)
		api.GET("/conversations", appHandler.ListConversations)
		api.GET("/conversations/:id", appHandler.GetConversation)
		api.PUT("/conversations/:id", appHandler.SaveConversation)
		api.DELETE("/conversations/:id", appHandler.DeleteConversation)
		api.GET("/knowledge-bases", appHandler.ListKnowledgeBases)
		api.POST("/knowledge-bases", appHandler.CreateKnowledgeBase)
		api.DELETE("/knowledge-bases/:id", appHandler.DeleteKnowledgeBase)
		api.GET("/knowledge-bases/:id/documents", appHandler.ListDocuments)
		api.POST("/knowledge-bases/:id/documents", appHandler.UploadToKnowledgeBase)
		api.DELETE("/knowledge-bases/:id/documents/:documentId", appHandler.DeleteDocument)
	}

	v1 := r.Group("/v1")
	v1.Use(authMiddleware(accessToken))
	{
		v1.POST("/chat/completions", appHandler.ChatCompletions)
		v1.POST("/chat/completions/stream", appHandler.ChatCompletionsStream)
	}

	mcp := r.Group("/mcp")
	mcp.Use(authMiddleware(accessToken))
	{
		mcp.POST("", mcpHandler.Handle)
	}

	return r
}

func corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

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
