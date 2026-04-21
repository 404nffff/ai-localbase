package router

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"ai-localbase/internal/handler"
	"ai-localbase/internal/model"
	"ai-localbase/internal/service"
)

type qdrantCollectionState struct {
	points []service.QdrantPoint
}

type qdrantTestServer struct {
	mu          sync.Mutex
	collections map[string]*qdrantCollectionState
}

type embeddingTestResponse struct {
	Data []struct {
		Embedding []float64 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
}

type chatTestResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index   int `json:"index"`
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

func TestRouterConfigEndpoints(t *testing.T) {
	engine, _, cleanup := newTestRouter(t)
	defer cleanup()

	updatePayload := map[string]any{
		"chat": map[string]any{
			"provider":    "ollama",
			"baseUrl":     "http://chat.local/v1",
			"model":       "llama3.2",
			"apiKey":      "",
			"temperature": 0.4,
		},
		"embedding": map[string]any{
			"provider": "openai-compatible",
			"baseUrl":  "http://embed.local/v1",
			"model":    "bge-m3",
			"apiKey":   "embed-key",
		},
	}

	resp := performJSONRequest(t, engine, http.MethodPut, "/api/config", updatePayload)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d, body=%s", resp.Code, resp.Body.String())
	}

	var updated model.AppConfig
	decodeJSONResponse(t, resp.Body.Bytes(), &updated)
	if updated.Chat.BaseURL != "http://chat.local/v1" {
		t.Fatalf("expected chat baseUrl to be updated, got %s", updated.Chat.BaseURL)
	}
	if updated.Embedding.Model != "bge-m3" {
		t.Fatalf("expected embedding model to be updated, got %s", updated.Embedding.Model)
	}

	resp = performRequest(t, engine, http.MethodGet, "/api/config", nil, "")
	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d, body=%s", resp.Code, resp.Body.String())
	}

	var fetched model.AppConfig
	decodeJSONResponse(t, resp.Body.Bytes(), &fetched)
	if fetched.Chat.Temperature != 0.4 {
		t.Fatalf("expected persisted chat temperature 0.4, got %v", fetched.Chat.Temperature)
	}
	if fetched.Embedding.APIKey != "embed-key" {
		t.Fatalf("expected persisted embedding apiKey, got %s", fetched.Embedding.APIKey)
	}
}

func TestRouterRejectSensitiveStructuredUploadWithoutLocalOllama(t *testing.T) {
	engine, _, cleanup := newTestRouter(t)
	defer cleanup()

	updatePayload := map[string]any{
		"chat": map[string]any{
			"provider":    "openai-compatible",
			"baseUrl":     "http://chat.remote/v1",
			"model":       "gpt-test",
			"apiKey":      "chat-key",
			"temperature": 0.4,
		},
		"embedding": map[string]any{
			"provider": "openai-compatible",
			"baseUrl":  "http://embed.remote/v1",
			"model":    "bge-m3",
			"apiKey":   "embed-key",
		},
	}
	resp := performJSONRequest(t, engine, http.MethodPut, "/api/config", updatePayload)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d, body=%s", resp.Code, resp.Body.String())
	}

	listResp := performRequest(t, engine, http.MethodGet, "/api/knowledge-bases", nil, "")
	if listResp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d, body=%s", listResp.Code, listResp.Body.String())
	}
	var kbList struct {
		Items []model.KnowledgeBase `json:"items"`
	}
	decodeJSONResponse(t, listResp.Body.Bytes(), &kbList)
	if len(kbList.Items) == 0 {
		t.Fatal("expected default knowledge base")
	}

	uploadResp := performMultipartUpload(
		t,
		engine,
		http.MethodPost,
		fmt.Sprintf("/api/knowledge-bases/%s/documents", kbList.Items[0].ID),
		"sensitive.csv",
		"姓名,部门\n张三,销售部\n",
	)
	if uploadResp.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d, body=%s", uploadResp.Code, uploadResp.Body.String())
	}
	if !strings.Contains(uploadResp.Body.String(), "requires local ollama") {
		t.Fatalf("expected local ollama policy error, got %s", uploadResp.Body.String())
	}
}

func TestRouterKnowledgeBaseExportReturnsZip(t *testing.T) {
	engine, _, cleanup := newTestRouter(t)
	defer cleanup()

	kbID := mustListFirstKnowledgeBaseID(t, engine)

	uploadResp := performMultipartUpload(
		t,
		engine,
		http.MethodPost,
		fmt.Sprintf("/api/knowledge-bases/%s/documents", kbID),
		"export-notes.md",
		strings.Repeat("用于导出 ZIP 的 markdown 文档内容。", 20),
	)
	if uploadResp.Code != http.StatusOK {
		t.Fatalf("expected upload status 200, got %d, body=%s", uploadResp.Code, uploadResp.Body.String())
	}

	exportResp := performRequest(t, engine, http.MethodGet, fmt.Sprintf("/api/knowledge-bases/%s/export", kbID), nil, "")
	if exportResp.Code != http.StatusOK {
		t.Fatalf("expected export status 200, got %d, body=%s", exportResp.Code, exportResp.Body.String())
	}

	contentType := exportResp.Header().Get("Content-Type")
	if !strings.Contains(contentType, "application/zip") {
		t.Fatalf("expected export content type to contain application/zip, got %q", contentType)
	}

	contentDisposition := exportResp.Header().Get("Content-Disposition")
	if !strings.Contains(contentDisposition, "attachment;") || !strings.Contains(contentDisposition, ".zip") {
		t.Fatalf("expected export content disposition to contain attachment zip filename, got %q", contentDisposition)
	}

	entries := readZipEntries(t, exportResp.Body.Bytes())
	if _, ok := entries["manifest.json"]; !ok {
		t.Fatalf("expected export zip to include manifest.json, got entries=%v", mapKeys(entries))
	}

	hasDocumentMarkdown := false
	for entryPath := range entries {
		if strings.HasPrefix(entryPath, "documents/") && strings.HasSuffix(entryPath, ".md") {
			hasDocumentMarkdown = true
			break
		}
	}
	if !hasDocumentMarkdown {
		t.Fatalf("expected export zip to include documents/*.md, got entries=%v", mapKeys(entries))
	}
}

func TestRouterKnowledgeBaseExportSkipsMissingMarkdownArchive(t *testing.T) {
	engine, appService, _, cleanup := newTestRouterWithAccessTokenAndService(t, "")
	defer cleanup()

	knowledgeBases := appService.ListKnowledgeBases()
	if len(knowledgeBases) == 0 {
		t.Fatal("expected default knowledge base")
	}
	kbID := knowledgeBases[0].ID

	legacyPath := filepath.Join(t.TempDir(), "legacy-source.md")
	if err := os.WriteFile(legacyPath, []byte("遗留文档正文"), 0o644); err != nil {
		t.Fatalf("write legacy document source: %v", err)
	}
	legacyDocument := appService.AddDocument(kbID, model.Document{
		ID:              "doc-legacy-no-markdown-archive",
		KnowledgeBaseID: kbID,
		Name:            "legacy-source.md",
		Path:            legacyPath,
		Status:          "indexed",
	})

	forcedMarkdownPath := filepath.Join(t.TempDir(), "missing-markdown-archive.md")
	if setDocumentMarkdownPathIfExists(&legacyDocument, forcedMarkdownPath) {
		if _, err := appService.ReplaceDocument(kbID, legacyDocument); err != nil {
			t.Fatalf("replace legacy document with forced markdownPath: %v", err)
		}
	}

	exportResp := performRequest(t, engine, http.MethodGet, fmt.Sprintf("/api/knowledge-bases/%s/export", kbID), nil, "")
	if exportResp.Code != http.StatusOK {
		t.Fatalf("expected export status 200, got %d, body=%s", exportResp.Code, exportResp.Body.String())
	}

	entries := readZipEntries(t, exportResp.Body.Bytes())
	manifestBytes, ok := entries["manifest.json"]
	if !ok {
		t.Fatalf("expected export zip to include manifest.json, got entries=%v", mapKeys(entries))
	}

	manifestDocuments := readManifestDocuments(t, manifestBytes)
	if len(manifestDocuments) == 0 {
		t.Fatalf("expected manifest documents to be non-empty, got %#v", manifestDocuments)
	}

	hasReason := false
	for _, item := range manifestDocuments {
		reason, _ := item["reason"].(string)
		if strings.TrimSpace(reason) != "" {
			hasReason = true
			break
		}
	}
	if !hasReason {
		t.Fatalf("expected manifest to include reason for missing markdown archive, got %#v", manifestDocuments)
	}
}

func TestRouterKnowledgeBaseExportReturnsNotFoundWhenKnowledgeBaseMissing(t *testing.T) {
	engine, _, cleanup := newTestRouter(t)
	defer cleanup()

	resp := performRequest(t, engine, http.MethodGet, "/api/knowledge-bases/kb-not-found/export", nil, "")
	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected status 404 for missing knowledge base export, got %d, body=%s", resp.Code, resp.Body.String())
	}
}

func TestRouterUploadRetrievalAndChatE2E(t *testing.T) {
	engine, modelBaseURL, cleanup := newTestRouter(t)
	defer cleanup()

	listResp := performRequest(t, engine, http.MethodGet, "/api/knowledge-bases", nil, "")
	if listResp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d, body=%s", listResp.Code, listResp.Body.String())
	}

	var kbList struct {
		Items []model.KnowledgeBase `json:"items"`
	}
	decodeJSONResponse(t, listResp.Body.Bytes(), &kbList)
	if len(kbList.Items) == 0 {
		t.Fatal("expected default knowledge base")
	}
	knowledgeBaseID := kbList.Items[0].ID

	documentContent := `# Redis 核心特点

Redis 是一个开源的内存数据结构存储系统，可用作数据库、缓存和消息代理。

## 主要特性

Redis 支持字符串、哈希、列表、集合、有序集合等多种数据结构。
Redis 具有极高的读写性能，单机每秒可处理数十万次请求。
Redis 支持数据持久化，可将内存中的数据保存到磁盘，重启后恢复。
Redis 支持主从复制，可实现读写分离与高可用部署。
Redis 提供发布订阅功能，支持消息传递模式。
Redis 支持 Lua 脚本，可实现原子性复杂操作。
Redis 内置事务支持，通过 MULTI/EXEC 命令实现。
Redis 支持过期时间设置，适合用作会话缓存或临时数据存储。

## 常见应用场景

缓存加速：将热点数据存入 Redis，减少数据库压力，提升响应速度。
计数器：利用 INCR 命令实现高并发下的精确计数，如页面浏览量统计。
排行榜：使用有序集合实现实时排行榜功能，支持按分数快速查询。
分布式锁：通过 SET NX 命令实现分布式锁，保证多节点下的互斥访问。
消息队列：使用列表结构实现简单的消息队列，支持生产者消费者模式。
会话管理：将用户会话数据存入 Redis，实现跨服务器的会话共享。
`
	uploadResp := performMultipartUpload(
		t,
		engine,
		http.MethodPost,
		fmt.Sprintf("/api/knowledge-bases/%s/documents", knowledgeBaseID),
		"redis-notes.md",
		documentContent,
	)
	if uploadResp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d, body=%s", uploadResp.Code, uploadResp.Body.String())
	}

	var uploadResult model.UploadResponse
	decodeJSONResponse(t, uploadResp.Body.Bytes(), &uploadResult)
	if uploadResult.Uploaded.Status != "indexed" {
		t.Fatalf("expected uploaded document status indexed, got %s", uploadResult.Uploaded.Status)
	}
	if !strings.Contains(uploadResult.Uploaded.ContentPreview, "Redis") {
		t.Fatalf("expected content preview to contain indexed text, got %q", uploadResult.Uploaded.ContentPreview)
	}

	chatPayload := map[string]any{
		"conversationId":  "conv-e2e-1",
		"model":           "chat-test-model",
		"knowledgeBaseId": knowledgeBaseID,
		"documentId":      uploadResult.Uploaded.ID,
		"config": map[string]any{
			"provider":    "ollama",
			"baseUrl":     modelBaseURL,
			"model":       "chat-test-model",
			"apiKey":      "",
			"temperature": 0.2,
		},
		"messages": []map[string]string{{
			"role":    "user",
			"content": "请说明 Redis 的核心特点",
		}},
	}

	resp := performJSONRequest(t, engine, http.MethodPost, "/v1/chat/completions", chatPayload)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d, body=%s", resp.Code, resp.Body.String())
	}

	var chatResult model.ChatCompletionResponse
	decodeJSONResponse(t, resp.Body.Bytes(), &chatResult)
	if len(chatResult.Choices) == 0 {
		t.Fatal("expected chat choices")
	}
	answer := chatResult.Choices[0].Message.Content
	if !strings.Contains(answer, "Redis") {
		t.Fatalf("expected answer to mention Redis, got %q", answer)
	}

	sources, ok := chatResult.Metadata["sources"].([]any)
	if !ok || len(sources) == 0 {
		t.Fatalf("expected retrieval sources in metadata, got %#v", chatResult.Metadata["sources"])
	}
}

func TestRouterStructuredCSVCountQuestionUsesCondensedAnswerRules(t *testing.T) {
	engine, modelBaseURL, cleanup := newTestRouter(t)
	defer cleanup()

	listResp := performRequest(t, engine, http.MethodGet, "/api/knowledge-bases", nil, "")
	if listResp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d, body=%s", listResp.Code, listResp.Body.String())
	}

	var kbList struct {
		Items []model.KnowledgeBase `json:"items"`
	}
	decodeJSONResponse(t, listResp.Body.Bytes(), &kbList)
	if len(kbList.Items) == 0 {
		t.Fatal("expected default knowledge base")
	}
	knowledgeBaseID := kbList.Items[0].ID

	csvContent := "姓名,性别,职称,教龄\n张三,男,高级职称,20\n李四,女,中级职称,8\n王五,男,无职称,4\n赵六,女,助教,1\n"
	uploadResp := performMultipartUpload(
		t,
		engine,
		http.MethodPost,
		fmt.Sprintf("/api/knowledge-bases/%s/documents", knowledgeBaseID),
		"employees.csv",
		csvContent,
	)
	if uploadResp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d, body=%s", uploadResp.Code, uploadResp.Body.String())
	}

	var uploadResult model.UploadResponse
	decodeJSONResponse(t, uploadResp.Body.Bytes(), &uploadResult)

	chatPayload := map[string]any{
		"conversationId":  "conv-e2e-csv-count",
		"model":           "chat-test-model",
		"knowledgeBaseId": knowledgeBaseID,
		"documentId":      uploadResult.Uploaded.ID,
		"config": map[string]any{
			"provider":    "ollama",
			"baseUrl":     modelBaseURL,
			"model":       "chat-test-model",
			"apiKey":      "",
			"temperature": 0.2,
		},
		"messages": []map[string]string{{
			"role":    "user",
			"content": "这个文档有多少名员工",
		}},
	}

	resp := performJSONRequest(t, engine, http.MethodPost, "/v1/chat/completions", chatPayload)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d, body=%s", resp.Code, resp.Body.String())
	}

	var chatResult model.ChatCompletionResponse
	decodeJSONResponse(t, resp.Body.Bytes(), &chatResult)
	if len(chatResult.Choices) == 0 {
		t.Fatal("expected chat choices")
	}
	answer := chatResult.Choices[0].Message.Content
	if !strings.Contains(answer, "该文档中共有 4 名员工") {
		t.Fatalf("expected concise count answer, got %q", answer)
	}
	if strings.Count(answer, "4 名员工") != 1 {
		t.Fatalf("expected count conclusion to appear once, got %q", answer)
	}
	if strings.Contains(answer, "字段：") {
		t.Fatalf("expected field list to be omitted for count question, got %q", answer)
	}
}

func TestRouterProtectedRoutesRequireBearerToken(t *testing.T) {
	engine, _, cleanup := newTestRouterWithAccessToken(t, "app-access-token")
	defer cleanup()

	rootResp := performRequest(t, engine, http.MethodGet, "/", nil, "")
	if rootResp.Code != http.StatusOK {
		t.Fatalf("expected root to remain public, got %d, body=%s", rootResp.Code, rootResp.Body.String())
	}

	healthResp := performRequest(t, engine, http.MethodGet, "/health", nil, "")
	if healthResp.Code != http.StatusOK {
		t.Fatalf("expected health to remain public, got %d, body=%s", healthResp.Code, healthResp.Body.String())
	}

	var healthBody struct {
		Status       string `json:"status"`
		AuthRequired bool   `json:"auth_required"`
	}
	decodeJSONResponse(t, healthResp.Body.Bytes(), &healthBody)
	if !healthBody.AuthRequired {
		t.Fatalf("expected /health to expose auth_required=true when access token is configured, got %#v", healthBody)
	}

	configResp := performRequest(t, engine, http.MethodGet, "/api/config", nil, "")
	if configResp.Code != http.StatusUnauthorized {
		t.Fatalf("expected protected config route to require token, got %d, body=%s", configResp.Code, configResp.Body.String())
	}

	verifyResp := performRequest(t, engine, http.MethodGet, "/api/auth/verify", nil, "")
	if verifyResp.Code != http.StatusUnauthorized {
		t.Fatalf("expected auth verify route to require token, got %d, body=%s", verifyResp.Code, verifyResp.Body.String())
	}

	chatResp := performAuthorizedJSONRequest(t, engine, http.MethodPost, "/v1/chat/completions", map[string]any{
		"conversationId": "conv-auth-unauthorized",
		"messages": []map[string]string{{
			"role":    "user",
			"content": "你好",
		}},
	}, "wrong-token")
	if chatResp.Code != http.StatusUnauthorized {
		t.Fatalf("expected chat route to reject wrong token, got %d, body=%s", chatResp.Code, chatResp.Body.String())
	}
}

func TestMCPRouteRequiresBearerToken(t *testing.T) {
	engine, _, cleanup := newTestRouterWithAccessToken(t, "app-access-token")
	defer cleanup()

	resp := performJSONRequest(t, engine, http.MethodPost, "/mcp", map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params":  map[string]any{},
	})
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("expected mcp route to require bearer token with 401, got %d, body=%s", resp.Code, resp.Body.String())
	}
}

func TestMCPInitializeReturnsJSONRPCResult(t *testing.T) {
	engine, _, cleanup := newTestRouterWithAccessToken(t, "app-access-token")
	defer cleanup()

	resp := performAuthorizedJSONRequest(t, engine, http.MethodPost, "/mcp", map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2025-03-26",
			"capabilities":    map[string]any{},
			"clientInfo": map[string]any{
				"name":    "test-client",
				"version": "1.0.0",
			},
		},
	}, "app-access-token")
	if resp.Code != http.StatusOK {
		t.Fatalf("expected authorized /mcp initialize to return 200 json-rpc result, got %d, body=%s", resp.Code, resp.Body.String())
	}

	var rpcResp map[string]any
	decodeJSONResponse(t, resp.Body.Bytes(), &rpcResp)
	if rpcResp["jsonrpc"] != "2.0" {
		t.Fatalf("expected jsonrpc version 2.0, got %#v", rpcResp["jsonrpc"])
	}
	if _, ok := rpcResp["result"]; !ok {
		t.Fatalf("expected json-rpc response to contain result, got %#v", rpcResp)
	}
}

func TestMCPInitializedNotificationReturnsAcceptedWithoutBody(t *testing.T) {
	engine, _, cleanup := newTestRouterWithAccessToken(t, "app-access-token")
	defer cleanup()

	resp := performAuthorizedJSONRequest(t, engine, http.MethodPost, "/mcp", map[string]any{
		"jsonrpc": "2.0",
		"method":  "notifications/initialized",
		"params":  map[string]any{},
	}, "app-access-token")
	if resp.Code != http.StatusAccepted {
		t.Fatalf("expected initialized notification to return 202, got %d, body=%s", resp.Code, resp.Body.String())
	}
	if strings.TrimSpace(resp.Body.String()) != "" {
		t.Fatalf("expected initialized notification response body to be empty, got %q", resp.Body.String())
	}
}

func TestMCPResourcesListReturnsKnowledgeBaseResources(t *testing.T) {
	engine, _, cleanup := newTestRouterWithAccessToken(t, "app-access-token")
	defer cleanup()

	resp := performAuthorizedJSONRequest(t, engine, http.MethodPost, "/mcp", map[string]any{
		"jsonrpc": "2.0",
		"id":      101,
		"method":  "resources/list",
		"params":  map[string]any{},
	}, "app-access-token")
	if resp.Code != http.StatusOK {
		t.Fatalf("expected resources/list to return 200, got %d, body=%s", resp.Code, resp.Body.String())
	}

	var rpcResp struct {
		Result struct {
			Resources []struct {
				URI string `json:"uri"`
			} `json:"resources"`
		} `json:"result"`
		Error *model.JSONRPCError `json:"error"`
	}
	decodeJSONResponse(t, resp.Body.Bytes(), &rpcResp)
	if rpcResp.Error != nil {
		t.Fatalf("expected resources/list to return result, got error=%+v", *rpcResp.Error)
	}
	if len(rpcResp.Result.Resources) == 0 {
		t.Fatal("expected resources/list to return at least one resource")
	}
	if !strings.HasPrefix(rpcResp.Result.Resources[0].URI, "ai-localbase://knowledge-bases") {
		t.Fatalf("expected first resource uri to use ai-localbase knowledge base scheme, got %q", rpcResp.Result.Resources[0].URI)
	}
}

func TestMCPResourceTemplatesListReturnsKnowledgeBaseTemplate(t *testing.T) {
	engine, _, cleanup := newTestRouterWithAccessToken(t, "app-access-token")
	defer cleanup()

	resp := performAuthorizedJSONRequest(t, engine, http.MethodPost, "/mcp", map[string]any{
		"jsonrpc": "2.0",
		"id":      102,
		"method":  "resources/templates/list",
		"params":  map[string]any{},
	}, "app-access-token")
	if resp.Code != http.StatusOK {
		t.Fatalf("expected resources/templates/list to return 200, got %d, body=%s", resp.Code, resp.Body.String())
	}

	var rpcResp struct {
		Result struct {
			ResourceTemplates []struct {
				URITemplate string `json:"uriTemplate"`
			} `json:"resourceTemplates"`
		} `json:"result"`
		Error *model.JSONRPCError `json:"error"`
	}
	decodeJSONResponse(t, resp.Body.Bytes(), &rpcResp)
	if rpcResp.Error != nil {
		t.Fatalf("expected resources/templates/list to return result, got error=%+v", *rpcResp.Error)
	}
	if len(rpcResp.Result.ResourceTemplates) == 0 {
		t.Fatal("expected resources/templates/list to return at least one template")
	}
	if rpcResp.Result.ResourceTemplates[0].URITemplate != "ai-localbase://knowledge-bases/{knowledgeBaseId}" {
		t.Fatalf("expected knowledge base template uri, got %q", rpcResp.Result.ResourceTemplates[0].URITemplate)
	}
}

func TestMCPResourcesReadReturnsKnowledgeBaseListJSON(t *testing.T) {
	engine, _, cleanup := newTestRouterWithAccessToken(t, "app-access-token")
	defer cleanup()

	resp := performAuthorizedJSONRequest(t, engine, http.MethodPost, "/mcp", map[string]any{
		"jsonrpc": "2.0",
		"id":      103,
		"method":  "resources/read",
		"params": map[string]any{
			"uri": "ai-localbase://knowledge-bases",
		},
	}, "app-access-token")
	if resp.Code != http.StatusOK {
		t.Fatalf("expected resources/read to return 200, got %d, body=%s", resp.Code, resp.Body.String())
	}

	var rpcResp struct {
		Result struct {
			Contents []struct {
				URI  string `json:"uri"`
				Text string `json:"text"`
			} `json:"contents"`
		} `json:"result"`
		Error *model.JSONRPCError `json:"error"`
	}
	decodeJSONResponse(t, resp.Body.Bytes(), &rpcResp)
	if rpcResp.Error != nil {
		t.Fatalf("expected resources/read to return result, got error=%+v", *rpcResp.Error)
	}
	if len(rpcResp.Result.Contents) == 0 {
		t.Fatal("expected resources/read to return at least one content item")
	}
	if rpcResp.Result.Contents[0].URI != "ai-localbase://knowledge-bases" {
		t.Fatalf("expected resources/read uri to match request, got %q", rpcResp.Result.Contents[0].URI)
	}
	if !strings.Contains(rpcResp.Result.Contents[0].Text, "默认知识库") {
		t.Fatalf("expected resources/read text to contain default knowledge base, got %q", rpcResp.Result.Contents[0].Text)
	}
}

func TestMCPToolsListReturnsExpectedTools(t *testing.T) {
	engine, _, cleanup := newTestRouterWithAccessToken(t, "app-access-token")
	defer cleanup()

	resp := performAuthorizedJSONRequest(t, engine, http.MethodPost, "/mcp", map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/list",
		"params":  map[string]any{},
	}, "app-access-token")
	if resp.Code != http.StatusOK {
		t.Fatalf("expected tools/list to return 200, got %d, body=%s", resp.Code, resp.Body.String())
	}

	var rpcResp struct {
		JSONRPC string `json:"jsonrpc"`
		Result  struct {
			Tools []model.MCPTool `json:"tools"`
		} `json:"result"`
		Error *model.JSONRPCError `json:"error"`
	}
	decodeJSONResponse(t, resp.Body.Bytes(), &rpcResp)
	if rpcResp.Error != nil {
		t.Fatalf("expected tools/list to return result, got error=%+v", *rpcResp.Error)
	}
	if rpcResp.JSONRPC != "2.0" {
		t.Fatalf("expected jsonrpc version 2.0, got %q", rpcResp.JSONRPC)
	}
	if len(rpcResp.Result.Tools) != 7 {
		t.Fatalf("expected 7 tools, got %d", len(rpcResp.Result.Tools))
	}
	if rpcResp.Result.Tools[0].Name != "chat.ask" {
		t.Fatalf("expected first tool chat.ask, got %q", rpcResp.Result.Tools[0].Name)
	}
	if rpcResp.Result.Tools[1].Name != "knowledge_base.search" {
		t.Fatalf("expected second tool knowledge_base.search, got %q", rpcResp.Result.Tools[1].Name)
	}
	if rpcResp.Result.Tools[2].Name != "knowledge_base.create" {
		t.Fatalf("expected third tool knowledge_base.create, got %q", rpcResp.Result.Tools[2].Name)
	}
	if rpcResp.Result.Tools[3].Name != "document.upload" {
		t.Fatalf("expected fourth tool document.upload, got %q", rpcResp.Result.Tools[3].Name)
	}
	if rpcResp.Result.Tools[4].Name != "document.append" {
		t.Fatalf("expected fifth tool document.append, got %q", rpcResp.Result.Tools[4].Name)
	}
	if rpcResp.Result.Tools[5].Name != "document.update" {
		t.Fatalf("expected sixth tool document.update, got %q", rpcResp.Result.Tools[5].Name)
	}
	if rpcResp.Result.Tools[6].Name != "document.delete" {
		t.Fatalf("expected seventh tool document.delete, got %q", rpcResp.Result.Tools[6].Name)
	}
}

func TestMCPToolsCallChatAskReturnsContent(t *testing.T) {
	engine, _, cleanup := newTestRouterWithAccessToken(t, "app-access-token")
	defer cleanup()

	resp := performAuthorizedJSONRequest(t, engine, http.MethodPost, "/mcp", map[string]any{
		"jsonrpc": "2.0",
		"id":      3,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "chat.ask",
			"arguments": map[string]any{
				"message": "请说明 Redis 的核心特点",
			},
		},
	}, "app-access-token")
	if resp.Code != http.StatusOK {
		t.Fatalf("expected tools/call chat.ask to return 200, got %d, body=%s", resp.Code, resp.Body.String())
	}

	var rpcResp struct {
		Result struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
			StructuredContent struct {
				Content string `json:"content"`
			} `json:"structuredContent"`
		} `json:"result"`
		Error *model.JSONRPCError `json:"error"`
	}
	decodeJSONResponse(t, resp.Body.Bytes(), &rpcResp)
	if rpcResp.Error != nil {
		t.Fatalf("expected chat.ask to return result, got error=%+v", *rpcResp.Error)
	}
	if len(rpcResp.Result.Content) == 0 {
		t.Fatal("expected chat.ask to return content blocks")
	}
	if !strings.Contains(rpcResp.Result.StructuredContent.Content, "Redis") {
		t.Fatalf("expected chat.ask structured content to mention Redis, got %q", rpcResp.Result.StructuredContent.Content)
	}
}

func TestMCPToolsCallKnowledgeBaseSearchReturnsItems(t *testing.T) {
	engine, _, cleanup := newTestRouterWithAccessToken(t, "app-access-token")
	defer cleanup()

	listResp := performAuthorizedRequest(t, engine, http.MethodGet, "/api/knowledge-bases", nil, "", "app-access-token")
	if listResp.Code != http.StatusOK {
		t.Fatalf("expected knowledge base list 200, got %d, body=%s", listResp.Code, listResp.Body.String())
	}

	var kbList struct {
		Items []model.KnowledgeBase `json:"items"`
	}
	decodeJSONResponse(t, listResp.Body.Bytes(), &kbList)
	if len(kbList.Items) == 0 {
		t.Fatal("expected default knowledge base")
	}

	uploadResp := performAuthorizedMultipartUpload(
		t,
		engine,
		http.MethodPost,
		fmt.Sprintf("/api/knowledge-bases/%s/documents", kbList.Items[0].ID),
		"redis-mcp.md",
		"# Redis\n\nRedis 是一个高性能内存数据库，支持多种数据结构。",
		"app-access-token",
	)
	if uploadResp.Code != http.StatusOK {
		t.Fatalf("expected upload 200, got %d, body=%s", uploadResp.Code, uploadResp.Body.String())
	}

	var uploadResult model.UploadResponse
	decodeJSONResponse(t, uploadResp.Body.Bytes(), &uploadResult)

	resp := performAuthorizedJSONRequest(t, engine, http.MethodPost, "/mcp", map[string]any{
		"jsonrpc": "2.0",
		"id":      4,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "knowledge_base.search",
			"arguments": map[string]any{
				"query":           "Redis",
				"knowledgeBaseId": kbList.Items[0].ID,
				"documentId":      uploadResult.Uploaded.ID,
			},
		},
	}, "app-access-token")
	if resp.Code != http.StatusOK {
		t.Fatalf("expected knowledge_base.search to return 200, got %d, body=%s", resp.Code, resp.Body.String())
	}

	var rpcResp struct {
		Result struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
			StructuredContent struct {
				Items []map[string]any `json:"items"`
			} `json:"structuredContent"`
		} `json:"result"`
		Error *model.JSONRPCError `json:"error"`
	}
	decodeJSONResponse(t, resp.Body.Bytes(), &rpcResp)
	if rpcResp.Error != nil {
		t.Fatalf("expected knowledge_base.search to return result, got error=%+v", *rpcResp.Error)
	}
	if len(rpcResp.Result.Content) == 0 {
		t.Fatal("expected knowledge_base.search to return content blocks")
	}
	if len(rpcResp.Result.StructuredContent.Items) == 0 {
		t.Fatal("expected knowledge_base.search to return at least one item")
	}
	text, _ := rpcResp.Result.StructuredContent.Items[0]["text"].(string)
	if !strings.Contains(text, "Redis") {
		t.Fatalf("expected first search item to mention Redis, got %#v", rpcResp.Result.StructuredContent.Items[0])
	}
}

func TestMCPToolsCallKnowledgeBaseCreateReturnsKnowledgeBase(t *testing.T) {
	engine, _, cleanup := newTestRouterWithAccessToken(t, "app-access-token")
	defer cleanup()

	resp := performAuthorizedJSONRequest(t, engine, http.MethodPost, "/mcp", map[string]any{
		"jsonrpc": "2.0",
		"id":      7,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "knowledge_base.create",
			"arguments": map[string]any{
				"name":        "MCP 新知识库",
				"description": "通过 MCP 创建的知识库",
			},
		},
	}, "app-access-token")
	if resp.Code != http.StatusOK {
		t.Fatalf("expected knowledge_base.create to return 200, got %d, body=%s", resp.Code, resp.Body.String())
	}

	var rpcResp struct {
		Result struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
			StructuredContent struct {
				ID              string `json:"id"`
				KnowledgeBaseID string `json:"knowledgeBaseId"`
				Name            string `json:"name"`
				Created         bool   `json:"created"`
			} `json:"structuredContent"`
		} `json:"result"`
		Error *model.JSONRPCError `json:"error"`
	}
	decodeJSONResponse(t, resp.Body.Bytes(), &rpcResp)
	if rpcResp.Error != nil {
		t.Fatalf("expected knowledge_base.create to return result, got error=%+v", *rpcResp.Error)
	}
	if len(rpcResp.Result.Content) == 0 {
		t.Fatal("expected knowledge_base.create to return content blocks")
	}
	if rpcResp.Result.StructuredContent.ID == "" {
		t.Fatal("expected created knowledge base id")
	}
	if rpcResp.Result.StructuredContent.KnowledgeBaseID != rpcResp.Result.StructuredContent.ID {
		t.Fatalf("expected knowledgeBaseId to equal id, got id=%q knowledgeBaseId=%q", rpcResp.Result.StructuredContent.ID, rpcResp.Result.StructuredContent.KnowledgeBaseID)
	}
	if !rpcResp.Result.StructuredContent.Created {
		t.Fatal("expected first knowledge_base.create call to create knowledge base")
	}
	if rpcResp.Result.StructuredContent.Name != "MCP 新知识库" {
		t.Fatalf("expected created knowledge base name, got %q", rpcResp.Result.StructuredContent.Name)
	}

	resp = performAuthorizedJSONRequest(t, engine, http.MethodPost, "/mcp", map[string]any{
		"jsonrpc": "2.0",
		"id":      8,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "knowledge_base.create",
			"arguments": map[string]any{
				"name":        "MCP 新知识库",
				"description": "重复创建应直接返回现有知识库",
			},
		},
	}, "app-access-token")
	if resp.Code != http.StatusOK {
		t.Fatalf("expected repeated knowledge_base.create to return 200, got %d, body=%s", resp.Code, resp.Body.String())
	}

	var repeatedRPCResp struct {
		Result struct {
			StructuredContent struct {
				ID              string `json:"id"`
				KnowledgeBaseID string `json:"knowledgeBaseId"`
				Created         bool   `json:"created"`
			} `json:"structuredContent"`
		} `json:"result"`
		Error *model.JSONRPCError `json:"error"`
	}
	decodeJSONResponse(t, resp.Body.Bytes(), &repeatedRPCResp)
	if repeatedRPCResp.Error != nil {
		t.Fatalf("expected repeated knowledge_base.create to return result, got error=%+v", *repeatedRPCResp.Error)
	}
	if repeatedRPCResp.Result.StructuredContent.Created {
		t.Fatal("expected repeated knowledge_base.create to reuse existing knowledge base")
	}
	if repeatedRPCResp.Result.StructuredContent.KnowledgeBaseID != rpcResp.Result.StructuredContent.KnowledgeBaseID {
		t.Fatalf("expected repeated knowledge base id %q, got %q", rpcResp.Result.StructuredContent.KnowledgeBaseID, repeatedRPCResp.Result.StructuredContent.KnowledgeBaseID)
	}
}

func TestMCPToolsCallDocumentUploadReturnsUploadedDocument(t *testing.T) {
	engine, _, cleanup := newTestRouterWithAccessToken(t, "app-access-token")
	defer cleanup()

	listResp := performAuthorizedRequest(t, engine, http.MethodGet, "/api/knowledge-bases", nil, "", "app-access-token")
	if listResp.Code != http.StatusOK {
		t.Fatalf("expected knowledge base list 200, got %d, body=%s", listResp.Code, listResp.Body.String())
	}

	var kbList struct {
		Items []model.KnowledgeBase `json:"items"`
	}
	decodeJSONResponse(t, listResp.Body.Bytes(), &kbList)
	if len(kbList.Items) == 0 {
		t.Fatal("expected default knowledge base")
	}

	resp := performAuthorizedJSONRequest(t, engine, http.MethodPost, "/mcp", map[string]any{
		"jsonrpc": "2.0",
		"id":      8,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "document.upload",
			"arguments": map[string]any{
				"knowledgeBaseId": kbList.Items[0].ID,
				"content":         "# MCP 上传文档\n\nRedis 可以作为缓存和消息代理。",
			},
		},
	}, "app-access-token")
	if resp.Code != http.StatusOK {
		t.Fatalf("expected document.upload to return 200, got %d, body=%s", resp.Code, resp.Body.String())
	}

	var rpcResp struct {
		Result struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
			StructuredContent model.UploadResponse `json:"structuredContent"`
		} `json:"result"`
		Error *model.JSONRPCError `json:"error"`
	}
	decodeJSONResponse(t, resp.Body.Bytes(), &rpcResp)
	if rpcResp.Error != nil {
		t.Fatalf("expected document.upload to return result, got error=%+v", *rpcResp.Error)
	}
	if len(rpcResp.Result.Content) == 0 {
		t.Fatal("expected document.upload to return content blocks")
	}
	if rpcResp.Result.StructuredContent.KnowledgeBase != kbList.Items[0].ID {
		t.Fatalf("expected uploaded knowledgeBaseId %q, got %q", kbList.Items[0].ID, rpcResp.Result.StructuredContent.KnowledgeBase)
	}
	if rpcResp.Result.StructuredContent.Uploaded.ID == "" {
		t.Fatal("expected uploaded document id")
	}
	if rpcResp.Result.StructuredContent.Uploaded.Name == "" {
		t.Fatal("expected uploaded document name to be generated")
	}
	if rpcResp.Result.StructuredContent.Uploaded.Status == "" {
		t.Fatal("expected uploaded document status")
	}
}

func TestMCPToolsCallDocumentDeleteReturnsDeletedDocument(t *testing.T) {
	engine, _, cleanup := newTestRouterWithAccessToken(t, "app-access-token")
	defer cleanup()

	listResp := performAuthorizedRequest(t, engine, http.MethodGet, "/api/knowledge-bases", nil, "", "app-access-token")
	if listResp.Code != http.StatusOK {
		t.Fatalf("expected knowledge base list 200, got %d, body=%s", listResp.Code, listResp.Body.String())
	}

	var kbList struct {
		Items []model.KnowledgeBase `json:"items"`
	}
	decodeJSONResponse(t, listResp.Body.Bytes(), &kbList)
	if len(kbList.Items) == 0 {
		t.Fatal("expected default knowledge base")
	}

	uploadResp := performAuthorizedJSONRequest(t, engine, http.MethodPost, "/mcp", map[string]any{
		"jsonrpc": "2.0",
		"id":      9,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "document.upload",
			"arguments": map[string]any{
				"knowledgeBaseId": kbList.Items[0].ID,
				"content":         "# 待删除文档\n\n这是一份待删除的 MCP 文档。",
			},
		},
	}, "app-access-token")
	if uploadResp.Code != http.StatusOK {
		t.Fatalf("expected document.upload to return 200, got %d, body=%s", uploadResp.Code, uploadResp.Body.String())
	}

	var uploadRPCResp struct {
		Result struct {
			StructuredContent model.UploadResponse `json:"structuredContent"`
		} `json:"result"`
		Error *model.JSONRPCError `json:"error"`
	}
	decodeJSONResponse(t, uploadResp.Body.Bytes(), &uploadRPCResp)
	if uploadRPCResp.Error != nil {
		t.Fatalf("expected document.upload to return result, got error=%+v", *uploadRPCResp.Error)
	}

	deleteResp := performAuthorizedJSONRequest(t, engine, http.MethodPost, "/mcp", map[string]any{
		"jsonrpc": "2.0",
		"id":      10,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "document.delete",
			"arguments": map[string]any{
				"knowledgeBaseId": kbList.Items[0].ID,
				"documentId":      uploadRPCResp.Result.StructuredContent.Uploaded.ID,
			},
		},
	}, "app-access-token")
	if deleteResp.Code != http.StatusOK {
		t.Fatalf("expected document.delete to return 200, got %d, body=%s", deleteResp.Code, deleteResp.Body.String())
	}

	var rpcResp struct {
		Result struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
			StructuredContent struct {
				Message         string         `json:"message"`
				KnowledgeBaseID string         `json:"knowledgeBaseId"`
				Deleted         model.Document `json:"deleted"`
			} `json:"structuredContent"`
		} `json:"result"`
		Error *model.JSONRPCError `json:"error"`
	}
	decodeJSONResponse(t, deleteResp.Body.Bytes(), &rpcResp)
	if rpcResp.Error != nil {
		t.Fatalf("expected document.delete to return result, got error=%+v", *rpcResp.Error)
	}
	if len(rpcResp.Result.Content) == 0 {
		t.Fatal("expected document.delete to return content blocks")
	}
	if rpcResp.Result.StructuredContent.Message != "document deleted" {
		t.Fatalf("expected delete message 'document deleted', got %q", rpcResp.Result.StructuredContent.Message)
	}
	if rpcResp.Result.StructuredContent.KnowledgeBaseID != kbList.Items[0].ID {
		t.Fatalf("expected delete knowledgeBaseId %q, got %q", kbList.Items[0].ID, rpcResp.Result.StructuredContent.KnowledgeBaseID)
	}
	if rpcResp.Result.StructuredContent.Deleted.ID != uploadRPCResp.Result.StructuredContent.Uploaded.ID {
		t.Fatalf("expected deleted document id %q, got %q", uploadRPCResp.Result.StructuredContent.Uploaded.ID, rpcResp.Result.StructuredContent.Deleted.ID)
	}
}

func TestMCPToolsCallDocumentAppendReturnsUpdatedDocument(t *testing.T) {
	engine, _, cleanup := newTestRouterWithAccessToken(t, "app-access-token")
	defer cleanup()

	listResp := performAuthorizedRequest(t, engine, http.MethodGet, "/api/knowledge-bases", nil, "", "app-access-token")
	if listResp.Code != http.StatusOK {
		t.Fatalf("expected knowledge base list 200, got %d, body=%s", listResp.Code, listResp.Body.String())
	}

	var kbList struct {
		Items []model.KnowledgeBase `json:"items"`
	}
	decodeJSONResponse(t, listResp.Body.Bytes(), &kbList)
	if len(kbList.Items) == 0 {
		t.Fatal("expected default knowledge base")
	}

	// 先上传初始文档，后续 append 应复用同一 documentId 并重建索引。
	uploadResp := performAuthorizedJSONRequest(t, engine, http.MethodPost, "/mcp", map[string]any{
		"jsonrpc": "2.0",
		"id":      11,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "document.upload",
			"arguments": map[string]any{
				"knowledgeBaseId": kbList.Items[0].ID,
				"filename":        "append-target.md",
				"content":         "# Redis\n\n原始内容：支持字符串。",
			},
		},
	}, "app-access-token")
	if uploadResp.Code != http.StatusOK {
		t.Fatalf("expected document.upload to return 200, got %d, body=%s", uploadResp.Code, uploadResp.Body.String())
	}

	var uploadRPCResp struct {
		Result struct {
			StructuredContent model.UploadResponse `json:"structuredContent"`
		} `json:"result"`
		Error *model.JSONRPCError `json:"error"`
	}
	decodeJSONResponse(t, uploadResp.Body.Bytes(), &uploadRPCResp)
	if uploadRPCResp.Error != nil {
		t.Fatalf("expected document.upload to return result, got error=%+v", *uploadRPCResp.Error)
	}

	appendResp := performAuthorizedJSONRequest(t, engine, http.MethodPost, "/mcp", map[string]any{
		"jsonrpc": "2.0",
		"id":      12,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "document.append",
			"arguments": map[string]any{
				"knowledgeBaseId": kbList.Items[0].ID,
				"documentId":      uploadRPCResp.Result.StructuredContent.Uploaded.ID,
				"content":         "\n\n追加内容：支持发布订阅。",
			},
		},
	}, "app-access-token")
	if appendResp.Code != http.StatusOK {
		t.Fatalf("expected document.append to return 200, got %d, body=%s", appendResp.Code, appendResp.Body.String())
	}

	var appendRPCResp struct {
		Result struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
			StructuredContent struct {
				Message         string         `json:"message"`
				KnowledgeBaseID string         `json:"knowledgeBaseId"`
				Updated         model.Document `json:"updated"`
			} `json:"structuredContent"`
		} `json:"result"`
		Error *model.JSONRPCError `json:"error"`
	}
	decodeJSONResponse(t, appendResp.Body.Bytes(), &appendRPCResp)
	if appendRPCResp.Error != nil {
		t.Fatalf("expected document.append to return result, got error=%+v", *appendRPCResp.Error)
	}
	if appendRPCResp.Result.StructuredContent.Message != "document appended" {
		t.Fatalf("expected append message 'document appended', got %q", appendRPCResp.Result.StructuredContent.Message)
	}
	if appendRPCResp.Result.StructuredContent.Updated.ID != uploadRPCResp.Result.StructuredContent.Uploaded.ID {
		t.Fatalf("expected appended document id %q, got %q", uploadRPCResp.Result.StructuredContent.Uploaded.ID, appendRPCResp.Result.StructuredContent.Updated.ID)
	}
	if !strings.Contains(appendRPCResp.Result.StructuredContent.Updated.ContentPreview, "发布订阅") {
		t.Fatalf("expected appended preview to contain new content, got %q", appendRPCResp.Result.StructuredContent.Updated.ContentPreview)
	}

	// 追加后原内容和新增内容都应能从同一文档片段中检索到。
	searchOriginalResp := performAuthorizedJSONRequest(t, engine, http.MethodPost, "/mcp", map[string]any{
		"jsonrpc": "2.0",
		"id":      13,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "knowledge_base.search",
			"arguments": map[string]any{
				"knowledgeBaseId": kbList.Items[0].ID,
				"documentId":      uploadRPCResp.Result.StructuredContent.Uploaded.ID,
				"query":           "原始内容",
			},
		},
	}, "app-access-token")
	if searchOriginalResp.Code != http.StatusOK {
		t.Fatalf("expected original search to return 200, got %d, body=%s", searchOriginalResp.Code, searchOriginalResp.Body.String())
	}

	var searchOriginalRPCResp struct {
		Result struct {
			StructuredContent struct {
				Items []map[string]any `json:"items"`
			} `json:"structuredContent"`
		} `json:"result"`
		Error *model.JSONRPCError `json:"error"`
	}
	decodeJSONResponse(t, searchOriginalResp.Body.Bytes(), &searchOriginalRPCResp)
	if searchOriginalRPCResp.Error != nil {
		t.Fatalf("expected original search result, got error=%+v", *searchOriginalRPCResp.Error)
	}
	if len(searchOriginalRPCResp.Result.StructuredContent.Items) == 0 {
		t.Fatal("expected original search to return at least one item")
	}
	originalText, _ := searchOriginalRPCResp.Result.StructuredContent.Items[0]["text"].(string)
	if !strings.Contains(originalText, "原始内容") {
		t.Fatalf("expected search result to contain original text, got %#v", searchOriginalRPCResp.Result.StructuredContent.Items[0])
	}

	searchAppendResp := performAuthorizedJSONRequest(t, engine, http.MethodPost, "/mcp", map[string]any{
		"jsonrpc": "2.0",
		"id":      14,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "knowledge_base.search",
			"arguments": map[string]any{
				"knowledgeBaseId": kbList.Items[0].ID,
				"documentId":      uploadRPCResp.Result.StructuredContent.Uploaded.ID,
				"query":           "发布订阅",
			},
		},
	}, "app-access-token")
	if searchAppendResp.Code != http.StatusOK {
		t.Fatalf("expected append search to return 200, got %d, body=%s", searchAppendResp.Code, searchAppendResp.Body.String())
	}

	var searchAppendRPCResp struct {
		Result struct {
			StructuredContent struct {
				Items []map[string]any `json:"items"`
			} `json:"structuredContent"`
		} `json:"result"`
		Error *model.JSONRPCError `json:"error"`
	}
	decodeJSONResponse(t, searchAppendResp.Body.Bytes(), &searchAppendRPCResp)
	if searchAppendRPCResp.Error != nil {
		t.Fatalf("expected append search result, got error=%+v", *searchAppendRPCResp.Error)
	}
	if len(searchAppendRPCResp.Result.StructuredContent.Items) == 0 {
		t.Fatal("expected append search to return at least one item")
	}
	appendText, _ := searchAppendRPCResp.Result.StructuredContent.Items[0]["text"].(string)
	if !strings.Contains(appendText, "发布订阅") {
		t.Fatalf("expected search result to contain appended text, got %#v", searchAppendRPCResp.Result.StructuredContent.Items[0])
	}
}

func TestMCPToolsCallDocumentUpdateReturnsUpdatedDocument(t *testing.T) {
	engine, _, cleanup := newTestRouterWithAccessToken(t, "app-access-token")
	defer cleanup()

	listResp := performAuthorizedRequest(t, engine, http.MethodGet, "/api/knowledge-bases", nil, "", "app-access-token")
	if listResp.Code != http.StatusOK {
		t.Fatalf("expected knowledge base list 200, got %d, body=%s", listResp.Code, listResp.Body.String())
	}

	var kbList struct {
		Items []model.KnowledgeBase `json:"items"`
	}
	decodeJSONResponse(t, listResp.Body.Bytes(), &kbList)
	if len(kbList.Items) == 0 {
		t.Fatal("expected default knowledge base")
	}

	// 先上传旧版本文档，update 应整篇覆盖并移除旧内容检索命中。
	uploadResp := performAuthorizedJSONRequest(t, engine, http.MethodPost, "/mcp", map[string]any{
		"jsonrpc": "2.0",
		"id":      15,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "document.upload",
			"arguments": map[string]any{
				"knowledgeBaseId": kbList.Items[0].ID,
				"filename":        "update-target.md",
				"content":         "# Redis\n\n旧内容：支持字符串。",
			},
		},
	}, "app-access-token")
	if uploadResp.Code != http.StatusOK {
		t.Fatalf("expected document.upload to return 200, got %d, body=%s", uploadResp.Code, uploadResp.Body.String())
	}

	var uploadRPCResp struct {
		Result struct {
			StructuredContent model.UploadResponse `json:"structuredContent"`
		} `json:"result"`
		Error *model.JSONRPCError `json:"error"`
	}
	decodeJSONResponse(t, uploadResp.Body.Bytes(), &uploadRPCResp)
	if uploadRPCResp.Error != nil {
		t.Fatalf("expected document.upload to return result, got error=%+v", *uploadRPCResp.Error)
	}

	updateResp := performAuthorizedJSONRequest(t, engine, http.MethodPost, "/mcp", map[string]any{
		"jsonrpc": "2.0",
		"id":      16,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "document.update",
			"arguments": map[string]any{
				"knowledgeBaseId": kbList.Items[0].ID,
				"documentId":      uploadRPCResp.Result.StructuredContent.Uploaded.ID,
				"content":         "# Redis\n\n新内容：支持有序集合。",
			},
		},
	}, "app-access-token")
	if updateResp.Code != http.StatusOK {
		t.Fatalf("expected document.update to return 200, got %d, body=%s", updateResp.Code, updateResp.Body.String())
	}

	var updateRPCResp struct {
		Result struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
			StructuredContent struct {
				Message         string         `json:"message"`
				KnowledgeBaseID string         `json:"knowledgeBaseId"`
				Updated         model.Document `json:"updated"`
			} `json:"structuredContent"`
		} `json:"result"`
		Error *model.JSONRPCError `json:"error"`
	}
	decodeJSONResponse(t, updateResp.Body.Bytes(), &updateRPCResp)
	if updateRPCResp.Error != nil {
		t.Fatalf("expected document.update to return result, got error=%+v", *updateRPCResp.Error)
	}
	if updateRPCResp.Result.StructuredContent.Message != "document updated" {
		t.Fatalf("expected update message 'document updated', got %q", updateRPCResp.Result.StructuredContent.Message)
	}
	if !strings.Contains(updateRPCResp.Result.StructuredContent.Updated.ContentPreview, "有序集合") {
		t.Fatalf("expected updated preview to contain replacement text, got %q", updateRPCResp.Result.StructuredContent.Updated.ContentPreview)
	}

	searchNewResp := performAuthorizedJSONRequest(t, engine, http.MethodPost, "/mcp", map[string]any{
		"jsonrpc": "2.0",
		"id":      17,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "knowledge_base.search",
			"arguments": map[string]any{
				"knowledgeBaseId": kbList.Items[0].ID,
				"documentId":      uploadRPCResp.Result.StructuredContent.Uploaded.ID,
				"query":           "有序集合",
			},
		},
	}, "app-access-token")
	if searchNewResp.Code != http.StatusOK {
		t.Fatalf("expected update search to return 200, got %d, body=%s", searchNewResp.Code, searchNewResp.Body.String())
	}

	var searchNewRPCResp struct {
		Result struct {
			StructuredContent struct {
				Items []map[string]any `json:"items"`
			} `json:"structuredContent"`
		} `json:"result"`
		Error *model.JSONRPCError `json:"error"`
	}
	decodeJSONResponse(t, searchNewResp.Body.Bytes(), &searchNewRPCResp)
	if searchNewRPCResp.Error != nil {
		t.Fatalf("expected update search result, got error=%+v", *searchNewRPCResp.Error)
	}
	if len(searchNewRPCResp.Result.StructuredContent.Items) == 0 {
		t.Fatal("expected update search to return at least one item")
	}
	newText, _ := searchNewRPCResp.Result.StructuredContent.Items[0]["text"].(string)
	if !strings.Contains(newText, "有序集合") {
		t.Fatalf("expected updated search result to contain new text, got %#v", searchNewRPCResp.Result.StructuredContent.Items[0])
	}
	if strings.Contains(newText, "字符串") {
		t.Fatalf("expected updated search result to exclude old text, got %#v", searchNewRPCResp.Result.StructuredContent.Items[0])
	}
}

func TestMCPUnknownMethodReturnsJSONRPCError(t *testing.T) {
	engine, _, cleanup := newTestRouterWithAccessToken(t, "app-access-token")
	defer cleanup()

	resp := performAuthorizedJSONRequest(t, engine, http.MethodPost, "/mcp", map[string]any{
		"jsonrpc": "2.0",
		"id":      5,
		"method":  "unknown/method",
		"params":  map[string]any{},
	}, "app-access-token")
	if resp.Code != http.StatusOK {
		t.Fatalf("expected unknown method to return 200, got %d, body=%s", resp.Code, resp.Body.String())
	}

	var rpcResp struct {
		Error *model.JSONRPCError `json:"error"`
	}
	decodeJSONResponse(t, resp.Body.Bytes(), &rpcResp)
	if rpcResp.Error == nil {
		t.Fatal("expected unknown method to return json-rpc error")
	}
	if rpcResp.Error.Code != -32601 {
		t.Fatalf("expected unknown method error code -32601, got %d", rpcResp.Error.Code)
	}
}

func TestMCPUnknownToolReturnsJSONRPCError(t *testing.T) {
	engine, _, cleanup := newTestRouterWithAccessToken(t, "app-access-token")
	defer cleanup()

	resp := performAuthorizedJSONRequest(t, engine, http.MethodPost, "/mcp", map[string]any{
		"jsonrpc": "2.0",
		"id":      6,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "unknown.tool",
			"arguments": map[string]any{},
		},
	}, "app-access-token")
	if resp.Code != http.StatusOK {
		t.Fatalf("expected unknown tool to return 200, got %d, body=%s", resp.Code, resp.Body.String())
	}

	var rpcResp struct {
		Error *model.JSONRPCError `json:"error"`
	}
	decodeJSONResponse(t, resp.Body.Bytes(), &rpcResp)
	if rpcResp.Error == nil {
		t.Fatal("expected unknown tool to return json-rpc error")
	}
	if rpcResp.Error.Message != "tool not found" {
		t.Fatalf("expected unknown tool error message 'tool not found', got %q", rpcResp.Error.Message)
	}
}

func TestRouterProtectedRoutesAcceptValidBearerToken(t *testing.T) {
	engine, _, cleanup := newTestRouterWithAccessToken(t, "app-access-token")
	defer cleanup()

	configResp := performAuthorizedRequest(t, engine, http.MethodGet, "/api/config", nil, "", "app-access-token")
	if configResp.Code != http.StatusOK {
		t.Fatalf("expected config route to accept valid token, got %d, body=%s", configResp.Code, configResp.Body.String())
	}

	verifyResp := performAuthorizedRequest(t, engine, http.MethodGet, "/api/auth/verify", nil, "", "app-access-token")
	if verifyResp.Code != http.StatusOK {
		t.Fatalf("expected auth verify route to accept valid token, got %d, body=%s", verifyResp.Code, verifyResp.Body.String())
	}

	var verifyBody struct {
		Status string `json:"status"`
	}
	decodeJSONResponse(t, verifyResp.Body.Bytes(), &verifyBody)
	if verifyBody.Status != "ok" {
		t.Fatalf("expected auth verify route to return status ok, got %#v", verifyBody)
	}

	chatResp := performAuthorizedJSONRequest(t, engine, http.MethodPost, "/v1/chat/completions", map[string]any{
		"conversationId": "conv-auth-authorized",
		"messages": []map[string]string{{
			"role":    "user",
			"content": "请说明 Redis 的核心特点",
		}},
	}, "app-access-token")
	if chatResp.Code != http.StatusOK {
		t.Fatalf("expected chat route to accept valid token, got %d, body=%s", chatResp.Code, chatResp.Body.String())
	}

	var chatResult model.ChatCompletionResponse
	decodeJSONResponse(t, chatResp.Body.Bytes(), &chatResult)
	if len(chatResult.Choices) == 0 {
		t.Fatal("expected chat choices for authorized request")
	}
}

func newTestRouter(t *testing.T) (*http.ServeMux, string, func()) {
	engine, _, modelBaseURL, cleanup := newTestRouterWithAccessTokenAndService(t, "")
	return engine, modelBaseURL, cleanup
}

func newTestRouterWithAccessToken(t *testing.T, accessToken string) (*http.ServeMux, string, func()) {
	engine, _, modelBaseURL, cleanup := newTestRouterWithAccessTokenAndService(t, accessToken)
	return engine, modelBaseURL, cleanup
}

func newTestRouterWithAccessTokenAndService(t *testing.T, accessToken string) (*http.ServeMux, *service.AppService, string, func()) {
	t.Helper()

	uploadDir := t.TempDir()
	chatHistoryPath := filepath.Join(t.TempDir(), "chat-history.db")
	chatHistoryStore, err := service.NewSQLiteChatHistoryStore(chatHistoryPath)
	if err != nil {
		t.Fatalf("create chat history store: %v", err)
	}
	qdrantState := &qdrantTestServer{collections: map[string]*qdrantCollectionState{}}
	qdrantHTTP := httptest.NewServer(http.HandlerFunc(qdrantState.handle))
	modelHTTP := httptest.NewServer(http.HandlerFunc(handleModelAPI))

	serverConfig := model.ServerConfig{
		Port:                   "0",
		UploadDir:              uploadDir,
		QdrantURL:              qdrantHTTP.URL,
		AccessToken:            accessToken,
		QdrantCollectionPrefix: "kb_",
		QdrantVectorSize:       8,
		QdrantDistance:         "Cosine",
		QdrantTimeoutSeconds:   5,
	}

	qdrantService := service.NewQdrantService(serverConfig)
	appService := service.NewAppService(qdrantService, service.NewAppStateStore(""), chatHistoryStore, serverConfig)
	_, err = appService.UpdateConfig(model.ConfigUpdateRequest{
		Chat: model.ChatConfig{
			Provider:    "ollama",
			BaseURL:     modelHTTP.URL,
			Model:       "chat-test-model",
			APIKey:      "",
			Temperature: 0.2,
		},
		Embedding: model.EmbeddingConfig{
			Provider: "ollama",
			BaseURL:  modelHTTP.URL,
			Model:    "embedding-test-model",
			APIKey:   "",
		},
	})
	if err != nil {
		t.Fatalf("update config: %v", err)
	}

	appHandler := handler.NewAppHandler(serverConfig, appService, service.NewLLMService())
	mcpService := service.NewMCPService(appService, service.NewLLMService())
	mcpHandler := handler.NewMCPHandler(mcpService)
	ginEngine := NewRouter(appHandler, mcpHandler, serverConfig.AccessToken)

	mux := http.NewServeMux()
	mux.Handle("/", ginEngine)

	cleanup := func() {
		_ = chatHistoryStore.Close()
		modelHTTP.Close()
		qdrantHTTP.Close()
		_ = os.RemoveAll(uploadDir)
	}
	return mux, appService, modelHTTP.URL, cleanup
}

func (s *qdrantTestServer) handle(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	writeJSON := func(status int, payload any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(payload)
	}

	requestPath := strings.TrimPrefix(r.URL.Path, "/")
	segments := strings.Split(requestPath, "/")
	if len(segments) == 0 || segments[0] != "collections" {
		writeJSON(http.StatusNotFound, map[string]any{"error": "not found"})
		return
	}

	if r.Method == http.MethodGet && len(segments) == 1 {
		writeJSON(http.StatusOK, map[string]any{"result": []any{}})
		return
	}

	if len(segments) < 2 {
		writeJSON(http.StatusNotFound, map[string]any{"error": "missing collection"})
		return
	}

	collectionName := segments[1]
	if _, ok := s.collections[collectionName]; !ok {
		s.collections[collectionName] = &qdrantCollectionState{}
	}
	collection := s.collections[collectionName]

	switch {
	case r.Method == http.MethodPut && len(segments) == 2:
		writeJSON(http.StatusOK, map[string]any{"result": true})
		return
	case r.Method == http.MethodDelete && len(segments) == 2:
		delete(s.collections, collectionName)
		writeJSON(http.StatusOK, map[string]any{"result": true})
		return
	case r.Method == http.MethodPut && len(segments) == 3 && segments[2] == "points":
		var req struct {
			Points []service.QdrantPoint `json:"points"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		collection.points = append([]service.QdrantPoint(nil), req.Points...)
		writeJSON(http.StatusOK, map[string]any{"result": map[string]any{"status": "acknowledged"}})
		return
	case r.Method == http.MethodPost && len(segments) == 4 && segments[2] == "points" && segments[3] == "delete":
		var req struct {
			Filter map[string]any `json:"filter"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		filtered := make([]service.QdrantPoint, 0, len(collection.points))
		for _, point := range collection.points {
			if matchesFilter(point.Payload, req.Filter) {
				continue
			}
			filtered = append(filtered, point)
		}
		collection.points = filtered
		writeJSON(http.StatusOK, map[string]any{"result": map[string]any{"status": "acknowledged"}})
		return
	case r.Method == http.MethodPost && len(segments) == 4 && segments[2] == "points" && segments[3] == "search":
		var req struct {
			Filter map[string]any `json:"filter"`
			Limit  int            `json:"limit"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		limit := req.Limit
		if limit <= 0 {
			limit = 5
		}

		results := make([]map[string]any, 0, len(collection.points))
		for index, point := range collection.points {
			if !matchesFilter(point.Payload, req.Filter) {
				continue
			}
			results = append(results, map[string]any{
				"id":      point.ID,
				"score":   0.99 - float64(index)*0.01,
				"payload": point.Payload,
			})
			if len(results) >= limit {
				break
			}
		}
		writeJSON(http.StatusOK, map[string]any{"result": results})
		return
	default:
		writeJSON(http.StatusNotFound, map[string]any{"error": "unsupported path"})
		return
	}
}

func matchesFilter(payload map[string]any, filter map[string]any) bool {
	if len(filter) == 0 {
		return true
	}
	must, ok := filter["must"].([]any)
	if !ok {
		if typed, ok := filter["must"].([]map[string]any); ok {
			for _, condition := range typed {
				if !matchCondition(payload, condition) {
					return false
				}
			}
			return true
		}
		return true
	}

	for _, item := range must {
		condition, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if !matchCondition(payload, condition) {
			return false
		}
	}
	return true
}

func matchCondition(payload map[string]any, condition map[string]any) bool {
	key, _ := condition["key"].(string)
	match, _ := condition["match"].(map[string]any)
	value := fmt.Sprint(match["value"])
	return fmt.Sprint(payload[key]) == value
}

func handleModelAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	switch r.URL.Path {
	case "/embeddings":
		var req struct {
			Input []string `json:"input"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		response := embeddingTestResponse{}
		for index := range req.Input {
			item := struct {
				Embedding []float64 `json:"embedding"`
				Index     int       `json:"index"`
			}{
				Embedding: []float64{1, 0, 0, 0, 0, 0, 0, 0},
				Index:     index,
			}
			response.Data = append(response.Data, item)
		}
		_ = json.NewEncoder(w).Encode(response)
	// Ollama native embedding
	case "/api/embed":
		var embedReq struct {
			Input []string `json:"input"`
		}
		_ = json.NewDecoder(r.Body).Decode(&embedReq)
		embeddings := make([][]float64, len(embedReq.Input))
		for i := range embedReq.Input {
			embeddings[i] = []float64{1, 0, 0, 0, 0, 0, 0, 0}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"embeddings": embeddings})
	// OpenAI-compatible chat
	case "/chat/completions":
		body, _ := io.ReadAll(r.Body)
		content := "已基于检索上下文回答：Redis 是高性能内存数据库。"
		if bytes.Contains(body, []byte("这个文档有多少名员工")) && bytes.Contains(body, []byte("数据行数：4")) {
			content = "该文档中共有 4 名员工。\n统计依据：按表头下方的数据行统计，共 4 条员工记录。"
		} else if !bytes.Contains(body, []byte("Redis")) {
			content = "已收到请求，但未检测到上下文。"
		}
		response := chatTestResponse{
			ID:      "chatcmpl-test",
			Object:  "chat.completion",
			Created: 1,
			Model:   "chat-test-model",
			Choices: []struct {
				Index   int `json:"index"`
				Message struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				} `json:"message"`
			}{
				{
					Index: 0,
					Message: struct {
						Role    string `json:"role"`
						Content string `json:"content"`
					}{
						Role:    "assistant",
						Content: content,
					},
				},
			},
		}
		_ = json.NewEncoder(w).Encode(response)
	// Ollama native chat
	case "/api/chat":
		body, _ := io.ReadAll(r.Body)
		content := "已基于检索上下文回答：Redis 是高性能内存数据库。"
		if bytes.Contains(body, []byte("这个文档有多少名员工")) && bytes.Contains(body, []byte("数据行数：4")) {
			content = "该文档中共有 4 名员工。\n统计依据：按表头下方的数据行统计，共 4 条员工记录。"
		} else if !bytes.Contains(body, []byte("Redis")) {
			content = "已收到请求，但未检测到上下文。"
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model": "chat-test-model",
			"message": map[string]any{
				"role":    "assistant",
				"content": content,
			},
			"done": true,
		})
	default:
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"message": "not found"}})
	}
}

func performJSONRequest(t *testing.T, handler http.Handler, method, target string, payload any) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal json request: %v", err)
	}
	return performRequest(t, handler, method, target, bytes.NewReader(body), "application/json")
}

func performAuthorizedJSONRequest(t *testing.T, handler http.Handler, method, target string, payload any, accessToken string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal json request: %v", err)
	}
	return performAuthorizedRequest(t, handler, method, target, bytes.NewReader(body), "application/json", accessToken)
}

func performMultipartUpload(t *testing.T, handler http.Handler, method, target, filename, content string) *httptest.ResponseRecorder {
	t.Helper()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	fileWriter, err := writer.CreateFormFile("file", filepath.Base(filename))
	if err != nil {
		t.Fatalf("create multipart file: %v", err)
	}
	if _, err := fileWriter.Write([]byte(content)); err != nil {
		t.Fatalf("write multipart content: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	return performRequest(t, handler, method, target, body, writer.FormDataContentType())
}

func performAuthorizedMultipartUpload(t *testing.T, handler http.Handler, method, target, filename, content, accessToken string) *httptest.ResponseRecorder {
	t.Helper()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	fileWriter, err := writer.CreateFormFile("file", filepath.Base(filename))
	if err != nil {
		t.Fatalf("create multipart file: %v", err)
	}
	if _, err := fileWriter.Write([]byte(content)); err != nil {
		t.Fatalf("write multipart content: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	return performAuthorizedRequest(t, handler, method, target, body, writer.FormDataContentType(), accessToken)
}

func performRequest(t *testing.T, handler http.Handler, method, target string, body io.Reader, contentType string) *httptest.ResponseRecorder {
	return performAuthorizedRequest(t, handler, method, target, body, contentType, "")
}

func performAuthorizedRequest(t *testing.T, handler http.Handler, method, target string, body io.Reader, contentType, accessToken string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, body)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+accessToken)
	}
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	return resp
}

func decodeJSONResponse(t *testing.T, body []byte, target any) {
	t.Helper()
	if err := json.Unmarshal(body, target); err != nil {
		t.Fatalf("decode json response: %v, body=%s", err, string(body))
	}
}

func mustListFirstKnowledgeBaseID(t *testing.T, engine http.Handler) string {
	t.Helper()

	listResp := performRequest(t, engine, http.MethodGet, "/api/knowledge-bases", nil, "")
	if listResp.Code != http.StatusOK {
		t.Fatalf("expected list knowledge bases status 200, got %d, body=%s", listResp.Code, listResp.Body.String())
	}

	var kbList struct {
		Items []model.KnowledgeBase `json:"items"`
	}
	decodeJSONResponse(t, listResp.Body.Bytes(), &kbList)
	if len(kbList.Items) == 0 {
		t.Fatal("expected default knowledge base")
	}
	return kbList.Items[0].ID
}

func readZipEntries(t *testing.T, zipBytes []byte) map[string][]byte {
	t.Helper()

	reader, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		t.Fatalf("open export zip: %v", err)
	}

	entries := make(map[string][]byte, len(reader.File))
	for _, file := range reader.File {
		fileReader, err := file.Open()
		if err != nil {
			t.Fatalf("open zip entry %q: %v", file.Name, err)
		}

		content, readErr := io.ReadAll(fileReader)
		closeErr := fileReader.Close()
		if readErr != nil {
			t.Fatalf("read zip entry %q: %v", file.Name, readErr)
		}
		if closeErr != nil {
			t.Fatalf("close zip entry %q: %v", file.Name, closeErr)
		}
		entries[file.Name] = content
	}

	return entries
}

func readManifestDocuments(t *testing.T, manifestBytes []byte) []map[string]any {
	t.Helper()

	var manifest map[string]any
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatalf("decode manifest json: %v, manifest=%s", err, string(manifestBytes))
	}

	rawDocuments, ok := manifest["documents"]
	if !ok {
		t.Fatalf("expected manifest to include documents, got %#v", manifest)
	}

	documentList, ok := rawDocuments.([]any)
	if !ok {
		t.Fatalf("expected manifest.documents to be array, got %#v", rawDocuments)
	}

	manifestDocuments := make([]map[string]any, 0, len(documentList))
	for _, item := range documentList {
		document, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("expected manifest document item to be object, got %#v", item)
		}
		manifestDocuments = append(manifestDocuments, document)
	}
	return manifestDocuments
}

func mapKeys(data map[string][]byte) []string {
	keys := make([]string, 0, len(data))
	for key := range data {
		keys = append(keys, key)
	}
	return keys
}

func setDocumentMarkdownPathIfExists(document *model.Document, markdownPath string) bool {
	payload, err := json.Marshal(document)
	if err != nil {
		return false
	}

	var documentMap map[string]any
	if err := json.Unmarshal(payload, &documentMap); err != nil {
		return false
	}

	if _, ok := documentMap["markdownPath"]; !ok {
		return false
	}
	documentMap["markdownPath"] = markdownPath

	encoded, err := json.Marshal(documentMap)
	if err != nil {
		return false
	}

	if err := json.Unmarshal(encoded, document); err != nil {
		return false
	}
	return true
}
