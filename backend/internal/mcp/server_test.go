package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ai-localbase/internal/model"
	"ai-localbase/internal/service"

	"github.com/gin-gonic/gin"
)

type staticTokenProvider struct {
	config model.AppConfig
}

func noopToolHandler(_ context.Context, _ map[string]any) (ToolCallResult, error) {
	return ToolCallResult{}, nil
}

func (p staticTokenProvider) GetConfig() model.AppConfig {
	return p.config
}

func TestMCPRejectsEmptyCompatibleToken(t *testing.T) {
	gin.SetMode(gin.TestMode)

	registry := NewToolRegistry(ToolDefinition{
		Name:            "list_knowledge_bases",
		Description:     "list knowledge bases",
		InputSchema:     emptyObjectSchema(),
		ReadOnly:        true,
		PermissionLevel: ToolPermissionReadOnly,
		Handler:         noopToolHandler,
	})
	server := NewServer(registry, staticTokenProvider{config: model.AppConfig{
		MCP: model.MCPConfig{Token: ""},
	}}, nil, model.ServerConfig{
		EnableAuth:           true,
		EnableMCP:            true,
		EnableMCPLegacyToken: true,
		MCPBasePath:          "/mcp",
	})

	router := gin.New()
	server.RegisterRoutes(router.Group("/mcp"))

	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer anything")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d, body=%s", resp.Code, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), "mcp token is not configured") {
		t.Fatalf("expected empty token error, got %s", resp.Body.String())
	}
}

func TestMCPRateLimitIsolatedPerAPIKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := &Server{
		requestsPerMin: 1,
		rateBuckets:    map[string]mcpRateBucket{},
	}
	firstContext, firstRecorder := newRateLimitTestContext("10.0.0.1:1000")
	firstAuth := authContext{Principal: service.AuthPrincipal{APIKeyID: "key-a"}}
	if !server.allowRequest(firstContext, firstAuth) {
		t.Fatal("expected first API key request to be allowed")
	}
	if server.allowRequest(firstContext, firstAuth) {
		t.Fatal("expected the second request for the same API key to be rejected")
	}
	if firstRecorder.Code != http.StatusTooManyRequests {
		t.Fatalf("expected rate limit status 429, got %d", firstRecorder.Code)
	}

	secondContext, secondRecorder := newRateLimitTestContext("10.0.0.2:1000")
	if !server.allowRequest(secondContext, authContext{Principal: service.AuthPrincipal{APIKeyID: "key-b"}}) {
		t.Fatalf("expected another API key to have an independent bucket, status=%d", secondRecorder.Code)
	}
}

func TestMCPToolArgumentLogsOmitStringValues(t *testing.T) {
	summary := summarizeToolArguments(map[string]any{
		"query":         "private question",
		"apiKey":        "secret-api-key",
		"contentBase64": "c2Vuc2l0aXZl",
	})
	for _, forbidden := range []string{"private question", "secret-api-key", "c2Vuc2l0aXZl"} {
		if strings.Contains(summary, forbidden) {
			t.Fatalf("expected tool argument summary to omit %q, got %s", forbidden, summary)
		}
	}
	if !strings.Contains(summary, `"chars"`) {
		t.Fatalf("expected tool argument summary to preserve string lengths, got %s", summary)
	}
}

func newRateLimitTestContext(remoteAddr string) (*gin.Context, *httptest.ResponseRecorder) {
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodGet, "/mcp", nil)
	context.Request.RemoteAddr = remoteAddr
	return context, recorder
}
