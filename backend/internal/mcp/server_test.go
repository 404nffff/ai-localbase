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

func newProtocolTestServer() (*Server, *gin.Engine) {
	registry := NewToolRegistry(ToolDefinition{
		Name:            "list_knowledge_bases",
		Description:     "list knowledge bases",
		InputSchema:     emptyObjectSchema(),
		ReadOnly:        true,
		PermissionLevel: ToolPermissionReadOnly,
		Handler:         noopToolHandler,
	})
	server := NewServer(registry, staticTokenProvider{config: model.AppConfig{
		MCP: model.MCPConfig{Token: "test-token"},
	}}, nil, model.ServerConfig{
		EnableAuth:           true,
		EnableMCP:            true,
		EnableMCPLegacyToken: true,
		MCPBasePath:          "/mcp",
	})
	router := gin.New()
	server.RegisterRoutes(router.Group("/mcp"))
	return server, router
}

func performProtocolRequest(router http.Handler, body string, contentType, accept string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-token")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	return resp
}

func TestMCPJSONRPCProtocolBoundaries(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_, router := newProtocolTestServer()

	invalidVersion := performProtocolRequest(router, `{"jsonrpc":"1.0","id":1,"method":"ping"}`, "application/json", "application/json")
	if invalidVersion.Code != http.StatusOK || !strings.Contains(invalidVersion.Body.String(), "invalid json-rpc request") {
		t.Fatalf("expected invalid JSON-RPC envelope response, got status=%d body=%s", invalidVersion.Code, invalidVersion.Body.String())
	}

	ping := performProtocolRequest(router, `{"jsonrpc":"2.0","id":2,"method":"ping"}`, "application/json", "application/json")
	if ping.Code != http.StatusOK || !strings.Contains(ping.Body.String(), `"jsonrpc":"2.0"`) {
		t.Fatalf("expected ping response, got status=%d body=%s", ping.Code, ping.Body.String())
	}

	notification := performProtocolRequest(router, `{"jsonrpc":"2.0","method":"notifications/initialized"}`, "application/json", "application/json")
	if notification.Code != http.StatusAccepted || notification.Body.Len() != 0 {
		t.Fatalf("expected empty 202 notification response, got status=%d body=%s", notification.Code, notification.Body.String())
	}

	unsupportedContentType := performProtocolRequest(router, `{"jsonrpc":"2.0","id":3,"method":"ping"}`, "text/plain", "application/json")
	if unsupportedContentType.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("expected unsupported content type status 415, got %d", unsupportedContentType.Code)
	}

	unsupportedAccept := performProtocolRequest(router, `{"jsonrpc":"2.0","id":4,"method":"ping"}`, "application/json", "text/event-stream")
	if unsupportedAccept.Code != http.StatusNotAcceptable {
		t.Fatalf("expected unacceptable response format status 406, got %d", unsupportedAccept.Code)
	}
}

func TestStartImportJobScopesFollowJobType(t *testing.T) {
	definition := ToolDefinition{Name: "start_import_job", PermissionLevel: ToolPermissionWrite}
	tests := []struct {
		name     string
		args     map[string]any
		expected string
	}{
		{name: "import", expected: scopeMCPUpload},
		{name: "reindex", args: map[string]any{"jobType": "reindex"}, expected: scopeMCPWrite},
		{name: "eval dataset", args: map[string]any{"jobType": "eval_dataset"}, expected: scopeMCPEval},
		{name: "batch index", args: map[string]any{"jobType": "batch_index"}, expected: scopeMCPUpload},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			scopes := requiredScopesForToolCall(definition, test.args)
			if len(scopes) != 1 || scopes[0] != test.expected {
				t.Fatalf("expected %q scope, got %v", test.expected, scopes)
			}
		})
	}
}

func TestSanitizeMCPErrorHidesDeploymentDetails(t *testing.T) {
	message := sanitizeMCPError("qdrant request failed for collection kb-secret at /app/data/state.json using https://internal.example/v1 with ailb_sk_secret")
	for _, secret := range []string{"kb-secret", "/app/data/state.json", "https://internal.example/v1", "ailb_sk_secret"} {
		if strings.Contains(message, secret) {
			t.Fatalf("expected %q to be redacted in %q", secret, message)
		}
	}
	if !strings.Contains(message, "vector collection") || !strings.Contains(message, "<redacted-url>") {
		t.Fatalf("expected redaction markers, got %q", message)
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
