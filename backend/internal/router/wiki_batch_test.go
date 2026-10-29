package router

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ai-localbase/internal/model"
)

// TestWikiBatchHTTP 验证真实批量路由、状态回读及鉴权；可显式启用本地浏览器工作台复用同一服务。
// 外部模型和向量存储使用 fixture，页面、HTTP、整理、发布、索引和持久化运行真实实现。
func TestWikiBatchHTTP(t *testing.T) {
	engine, app, _, cleanup := newTestRouterWithAccessTokenAndService(t, "")
	defer cleanup()
	modelHTTP := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req model.ChatCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		var input struct {
			Sources []struct {
				DocumentID string `json:"documentId"`
				Checksum   string `json:"checksum"`
				Content    string `json:"content"`
				Name       string `json:"name"`
			} `json:"sources"`
			Related []struct {
				PageID       string `json:"pageId"`
				BaseRevision int    `json:"baseRevision"`
				Title        string `json:"title"`
			} `json:"relatedPages"`
		}
		_ = json.Unmarshal([]byte(req.Messages[len(req.Messages)-1].Content), &input)
		plan := model.WikiPlan{Pages: []model.WikiPageChange{}, Issues: []model.WikiIssue{}}
		for _, src := range input.Sources {
			page := model.WikiPageChange{Key: src.DocumentID, Title: src.Name + "知识摘要", PageType: "source-summary", Markdown: src.Content, SourceRefs: []model.WikiSourceRef{{DocumentID: src.DocumentID, Checksum: src.Checksum, Quote: src.Content}}}
			for _, related := range input.Related {
				if related.Title == page.Title {
					page.PageID, page.BaseRevision = related.PageID, related.BaseRevision
				}
			}
			plan.Pages = append(plan.Pages, page)
		}
		body, _ := json.Marshal(plan)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": string(body)}}}})
	}))
	defer modelHTTP.Close()
	cfg := app.GetConfig()
	cfg.Chat = model.ChatConfig{Provider: "openai-compatible", BaseURL: modelHTTP.URL, Model: "browser-fixture"}
	if _, err := app.UpdateConfig(model.ConfigUpdateRequest{Chat: cfg.Chat, Embedding: cfg.Embedding}); err != nil {
		t.Fatal(err)
	}
	id := app.ListKnowledgeBases()[0].ID
	if _, err := app.ConfigureWiki(id, true); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for i, name := range []string{"退款指南.md", "交付说明.md"} {
		path := filepath.Join(dir, name)
		body := fmt.Sprintf("# %s\n\n## 服务说明\n\n退款期限为七天。第 %d 份资料。\n\n- 联系客服\n- 提供订单号\n", name, i+1)
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		app.AddDocument(id, model.Document{ID: fmt.Sprintf("source-%d", i+1), KnowledgeBaseID: id, Name: name, Path: path, UploadedAt: time.Now().Format(time.RFC3339)})
	}
	root := "/api/knowledge-bases/" + id + "/wiki"
	if resp := performRequest(t, engine, http.MethodGet, root, nil, ""); resp.Code != 200 || !strings.Contains(resp.Body.String(), `"pendingCount":2`) {
		t.Fatalf("historical sources missing: %s", resp.Body)
	}
	if resp := performRequest(t, engine, http.MethodPost, root+"/ingest-batch", strings.NewReader(`{"documentIds":["missing"]}`), "application/json"); resp.Code != 404 {
		t.Fatal("invalid source accepted")
	}
	resp := performRequest(t, engine, http.MethodPost, root+"/ingest-batch", strings.NewReader(`{"documentIds":["source-1"]}`), "application/json")
	if resp.Code != 200 {
		t.Fatalf("batch: %d %s", resp.Code, resp.Body)
	}
	var result struct {
		Items []struct {
			JobID string `json:"jobId"`
		} `json:"items"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &result); err != nil || len(result.Items) != 1 {
		t.Fatalf("invalid batch response: %s", resp.Body)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		job, err := app.GetWikiJob(id, result.Items[0].JobID)
		if err != nil {
			t.Fatal(err)
		}
		if job.Status == "succeeded" {
			break
		}
		if job.Status == "failed" || time.Now().After(deadline) {
			t.Fatalf("batch failed: %+v", job)
		}
		time.Sleep(10 * time.Millisecond)
	}
	view := performRequest(t, engine, http.MethodGet, root, nil, "")
	if !strings.Contains(view.Body.String(), `"source-1":"succeeded"`) || !strings.Contains(view.Body.String(), `"source-2":"pending"`) {
		t.Fatalf("incorrect source states: %s", view.Body)
	}
	secured, _, _, closeSecured := newTestRouterWithAccessTokenAndService(t, "fixture")
	defer closeSecured()
	if denied := performRequest(t, secured, http.MethodPost, root+"/ingest-batch", bytes.NewBufferString(`{}`), "application/json"); denied.Code != 401 {
		t.Fatal("unauthorized batch accepted")
	}
	// 保存路由必须保留鉴权和基线冲突语义，读取历史仍返回原正文。
	kb, err := app.WikiSnapshot(id)
	if err != nil {
		t.Fatal(err)
	}
	pageID := ""
	for _, document := range kb.Documents {
		if document.Wiki != nil {
			pageID = document.ID
			break
		}
	}
	if pageID == "" {
		t.Fatal("missing published page")
	}
	pageURL := root + "/pages/" + pageID
	edit := `{"baseRevision":1,"title":"退款办理知识页","markdown":"# 退款办理\n\n退款期限为七天。\n\n- 联系客服\n- 提供订单号"}`
	if denied := performRequest(t, secured, http.MethodPut, pageURL, strings.NewReader(edit), "application/json"); denied.Code != 401 {
		t.Fatal("unauthorized edit accepted")
	}
	if saved := performRequest(t, engine, http.MethodPut, pageURL, strings.NewReader(edit), "application/json"); saved.Code != 200 || !strings.Contains(saved.Body.String(), `"revision":2`) {
		t.Fatalf("edit failed: %s", saved.Body)
	}
	if stale := performRequest(t, engine, http.MethodPut, pageURL, strings.NewReader(edit), "application/json"); stale.Code != 409 {
		t.Fatalf("stale edit: %s", stale.Body)
	}
	if invalid := performRequest(t, engine, http.MethodPut, pageURL, strings.NewReader(`{}`), "application/json"); invalid.Code != 400 {
		t.Fatal("empty edit accepted")
	}
	if missing := performRequest(t, engine, http.MethodPut, root+"/pages/missing", strings.NewReader(edit), "application/json"); missing.Code != 404 {
		t.Fatal("missing edit accepted")
	}
	if history := performRequest(t, engine, http.MethodGet, pageURL+"?revision=1", nil, ""); history.Code != 200 || !strings.Contains(history.Body.String(), "第 1 份资料") {
		t.Fatalf("history overwritten: %s", history.Body)
	}
	if os.Getenv("WIKI_BROWSER_WORKBENCH") != "1" {
		return
	}
	// 手动浏览器验收专用缺文件资料，用于验证逐项失败反馈；不污染生产或普通测试。
	app.AddDocument(id, model.Document{ID: "source-missing", KnowledgeBaseID: id, Name: "缺失源文件.md", Path: filepath.Join(dir, "missing.md"), UploadedAt: time.Now().Format(time.RFC3339)})
	listener, err := net.Listen("tcp", "127.0.0.1:18080")
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: engine, ReadHeaderTimeout: 5 * time.Second}
	done := make(chan struct{})
	engine.HandleFunc("/__test/stop", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(204)
		select {
		case <-done:
		default:
			close(done)
		}
	})
	go func() { _ = server.Serve(listener) }()
	t.Log("Browser workbench http://127.0.0.1:18080")
	select {
	case <-done:
	case <-time.After(15 * time.Minute):
	}
	_ = server.Shutdown(context.Background())
}
