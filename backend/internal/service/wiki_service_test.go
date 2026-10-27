package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ai-localbase/internal/model"
)

// TestWikiPublishAndRevision 验证真实页面发布、历史分页和乐观版本检查。
func TestWikiPublishAndRevision(t *testing.T) {
	dir := t.TempDir()
	s := NewAppService(nil, NewAppStateStore(filepath.Join(dir, "state.json")), nil, model.ServerConfig{UploadDir: dir})
	kbID := s.ListKnowledgeBases()[0].ID
	if _, err := s.ConfigureWiki(kbID, true); err != nil {
		t.Fatal(err)
	}
	body := "批量索引使用异步任务，单文件接口保持同步。"
	path := filepath.Join(dir, "source.md")
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	s.AddDocument(kbID, model.Document{ID: "source-1", KnowledgeBaseID: kbID, Path: path, ContentChecksum: hashDocumentContent(body)})
	change := model.WikiPageChange{PageType: "topic", Title: "索引机制", Markdown: "索引方式：异步任务。", SourceRefs: []model.WikiSourceRef{{DocumentID: "source-1", Checksum: hashDocumentContent(body), Quote: "批量索引使用异步任务"}}}
	ids, err := s.publishWikiPlan(kbID, "", model.WikiPlan{Pages: []model.WikiPageChange{change}})
	if err != nil || len(ids) != 1 {
		t.Fatalf("publish: %v %v", ids, err)
	}
	first, err := s.WikiPageContent(kbID, ids[0], 1, 0, 2)
	if err != nil || first["markdown"] != "索引" || first["nextOffset"] != 2 {
		t.Fatalf("unicode page: %#v %v", first, err)
	}
	change.PageID, change.BaseRevision, change.Markdown = ids[0], 1, "索引方式：批量异步，单文件同步。"
	if _, err := s.publishWikiPlan(kbID, "", model.WikiPlan{Pages: []model.WikiPageChange{change}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.publishWikiPlan(kbID, "", model.WikiPlan{Pages: []model.WikiPageChange{change}}); err == nil {
		t.Fatal("stale revision accepted")
	}
	old, err := s.WikiPageContent(kbID, ids[0], 1, 0, 0)
	if err != nil || old["markdown"] != "索引方式：异步任务。" {
		t.Fatalf("history changed: %v %v", old, err)
	}
	reloaded := NewAppService(nil, s.store, nil, model.ServerConfig{UploadDir: dir})
	latest, err := reloaded.WikiPageContent(kbID, ids[0], 0, 0, 0)
	if err != nil || latest["revision"] != 2 {
		t.Fatalf("reload: %#v %v", latest, err)
	}
}

// TestWikiPublishFailurePreservesState 对磁盘失败和坏链接进行故障注入，验证整批不发布。
func TestWikiPublishFailurePreservesState(t *testing.T) {
	dir := t.TempDir()
	s := NewAppService(nil, nil, nil, model.ServerConfig{UploadDir: dir})
	kbID := s.ListKnowledgeBases()[0].ID
	if _, err := s.ConfigureWiki(kbID, true); err != nil {
		t.Fatal(err)
	}
	good := model.WikiPageChange{Key: "one", PageType: "question", Title: "待核实一", Markdown: "需要补充证据"}
	bad := model.WikiPageChange{Key: "two", PageType: "question", Title: "待核实二", Markdown: "需要核实", Links: []string{"unknown"}}
	if _, err := s.publishWikiPlan(kbID, "", model.WikiPlan{Pages: []model.WikiPageChange{good, bad}}); err == nil {
		t.Fatal("broken link accepted")
	}
	kb, _ := s.WikiSnapshot(kbID)
	if len(kb.Documents) != 0 {
		t.Fatal("partial batch became visible")
	}
	parent := filepath.Join(dir, "not-a-directory")
	if err := os.WriteFile(parent, []byte("block"), 0600); err != nil {
		t.Fatal(err)
	}
	s.store = NewAppStateStore(filepath.Join(parent, "state.json"))
	if _, err := s.publishWikiPlan(kbID, "", model.WikiPlan{Pages: []model.WikiPageChange{good}}); err == nil {
		t.Fatal("save failure ignored")
	}
	kb, _ = s.WikiSnapshot(kbID)
	if len(kb.Documents) != 0 {
		t.Fatal("failed save changed memory")
	}
}

// TestWikiSnapshotIsolation 保证页面、任务及问题的嵌套数据不会穿透快照修改。
func TestWikiSnapshotIsolation(t *testing.T) {
	s := NewAppService(nil, nil, nil, model.ServerConfig{UploadDir: t.TempDir()})
	kbID := s.ListKnowledgeBases()[0].ID
	err := s.commitWikiChange(kbID, func(kb *model.KnowledgeBase) error {
		kb.Wiki.Enabled = true
		kb.Wiki.PendingSources["doc-1"] = "original"
		kb.Wiki.Jobs = []model.WikiJob{{ID: "job-1", SourceVersions: map[string]string{"doc-1": "original"}}}
		kb.Wiki.Issues = []model.WikiIssue{{PageIDs: []string{"page-1"}}}
		kb.Documents = []model.Document{{ID: "page-1", Wiki: &model.WikiPageMetadata{Aliases: []string{"原名"}, History: []model.WikiRevision{{SourceRefs: []model.WikiSourceRef{{Quote: "原文"}}}}}}}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, _ := s.WikiSnapshot(kbID)
	snapshot.Wiki.PendingSources["doc-1"] = "changed"
	snapshot.Wiki.Jobs[0].SourceVersions["doc-1"] = "changed"
	snapshot.Wiki.Issues[0].PageIDs[0] = "changed"
	snapshot.Documents[0].Wiki.Aliases[0] = "改名"
	snapshot.Documents[0].Wiki.History[0].SourceRefs[0].Quote = "改文"
	actual, _ := s.WikiSnapshot(kbID)
	if actual.Wiki.PendingSources["doc-1"] != "original" || actual.Wiki.Jobs[0].SourceVersions["doc-1"] != "original" || actual.Wiki.Issues[0].PageIDs[0] != "page-1" || actual.Documents[0].Wiki.Aliases[0] != "原名" || actual.Documents[0].Wiki.History[0].SourceRefs[0].Quote != "原文" {
		t.Fatal("snapshot mutated live state")
	}
}

// TestWikiRejectsUnsupportedEvidence 验证事实来源、重名和同批链接的边界。
func TestWikiRejectsUnsupportedEvidence(t *testing.T) {
	s := NewAppService(nil, nil, nil, model.ServerConfig{UploadDir: t.TempDir()})
	kbID := s.ListKnowledgeBases()[0].ID
	if _, err := s.ConfigureWiki(kbID, true); err != nil {
		t.Fatal(err)
	}
	for _, p := range []model.WikiPageChange{
		{Title: "无来源", PageType: "topic", Markdown: "声称事实"},
		{Title: "外库", PageType: "topic", Markdown: "声称事实", SourceRefs: []model.WikiSourceRef{{DocumentID: "outside", Checksum: "unknown", Quote: "不存在"}}},
	} {
		if _, err := s.publishWikiPlan(kbID, "", model.WikiPlan{Pages: []model.WikiPageChange{p}}); err == nil {
			t.Fatal("unsupported fact accepted")
		}
	}
	pages := []model.WikiPageChange{{Key: "a", Title: "问题甲", PageType: "question", Markdown: "待查", Links: []string{"b"}}, {Key: "b", Title: "问题乙", PageType: "question", Markdown: "待查", Links: []string{"a"}}}
	ids, err := s.publishWikiPlan(kbID, "", model.WikiPlan{Pages: pages})
	if err != nil || len(ids) != 2 {
		t.Fatalf("batch links: %v %v", ids, err)
	}
	kb, _ := s.WikiSnapshot(kbID)
	if !strings.HasPrefix(kb.Documents[0].Wiki.Links[0], "wiki-") {
		t.Fatal("local key not resolved")
	}
	if _, err := s.publishWikiPlan(kbID, "", model.WikiPlan{Pages: []model.WikiPageChange{{Title: "问题甲", PageType: "question", Markdown: "重复"}}}); err == nil {
		t.Fatal("duplicate page accepted")
	}
}

// TestWikiSourceLifecycle 验证来源更新保留旧版本、使页面失效，删除后不残留快照。
func TestWikiSourceLifecycle(t *testing.T) {
	dir := t.TempDir()
	embedding := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		vector := make([]float64, 768)
		vector[0] = 1
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"index": 0, "embedding": vector}}})
	}))
	defer embedding.Close()
	s := NewAppService(nil, nil, nil, model.ServerConfig{UploadDir: dir, QdrantVectorSize: 3})
	s.state.Config.Embedding = model.EmbeddingConfig{Provider: "openai-compatible", BaseURL: embedding.URL, Model: "fixture"}
	// 生命周期测试不启动整理，队列执行由专门的本地模型桩测试覆盖。
	s.wikiRunMu.Lock()
	s.wikiClosed = true
	s.wikiRunMu.Unlock()
	t.Cleanup(func() { _ = s.Close() })
	kbID := s.ListKnowledgeBases()[0].ID
	if _, err := s.ConfigureWiki(kbID, true); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "source.md")
	old := "退款期限是七天。"
	if err := os.WriteFile(path, []byte(old), 0600); err != nil {
		t.Fatal(err)
	}
	doc, err := s.snapshotWikiSource(model.Document{ID: "source-life", KnowledgeBaseID: kbID, Path: path, Name: "退款规则"}, old)
	if err != nil {
		t.Fatal(err)
	}
	s.AddDocument(kbID, doc)
	page := model.WikiPageChange{PageType: "topic", Title: "退款", Markdown: old, SourceRefs: []model.WikiSourceRef{{DocumentID: doc.ID, Checksum: doc.ContentChecksum, Quote: old}}}
	ids, err := s.publishWikiPlan(kbID, "", model.WikiPlan{Pages: []model.WikiPageChange{page}})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := s.RewriteDocumentContent(kbID, doc.ID, "退款期限变更为十四天。")
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.SourceRevisions) != 2 {
		t.Fatalf("expected two revisions: %#v", updated.SourceRevisions)
	}
	view, err := s.WikiSourceContent(kbID, doc.ID, doc.ContentChecksum, 0, 0)
	if err != nil || view["content"] != old {
		t.Fatalf("old version unavailable: %#v %v", view, err)
	}
	kb, _ := s.WikiSnapshot(kbID)
	for _, d := range kb.Documents {
		if d.ID == ids[0] && d.Wiki.Freshness != "stale" {
			t.Fatal("updated source did not stale page")
		}
	}
	removed, err := s.DeleteDocument(kbID, doc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := RemoveDocumentFiles(removed); err != nil {
		t.Fatal(err)
	}
	if _, err := s.WikiSourceContent(kbID, doc.ID, doc.ContentChecksum, 0, 0); err == nil {
		t.Fatal("deleted source still readable")
	}
	for _, version := range removed.SourceRevisions {
		if _, err := os.Stat(version.SnapshotPath); !os.IsNotExist(err) {
			t.Fatalf("snapshot retained: %v", err)
		}
	}
	kb, _ = s.WikiSnapshot(kbID)
	for _, d := range kb.Documents {
		if d.ID == ids[0] && d.Wiki.Freshness != "missing-source" {
			t.Fatal("deleted source still valid")
		}
	}
}

// TestWikiRetrievalRejectsObsoleteEvidence 验证旧修订、未索引、停用、来源漂移与删除均不能召回。
func TestWikiRetrievalRejectsObsoleteEvidence(t *testing.T) {
	s, id, doc := wikiFixture(t, func(model.ChatCompletionRequest) (string, int) { return `{"pages":[]}`, 200 })
	page := model.WikiPageChange{Title: "退款说明", PageType: "topic", Markdown: "七天", SourceRefs: []model.WikiSourceRef{{DocumentID: doc.ID, Checksum: doc.ContentChecksum, Quote: "退款期限为七天。"}}}
	ids, err := s.publishWikiPlan(id, "", model.WikiPlan{Pages: []model.WikiPageChange{page}})
	if err != nil {
		t.Fatal(err)
	}
	chunk := RetrievedChunk{DocumentChunk: DocumentChunk{ID: "wiki-chunk", KnowledgeBaseID: id, DocumentID: ids[0], Kind: "wiki", WikiRevision: 1, Text: "七天"}, Score: 1}
	if len(s.filterWikiChunks([]RetrievedChunk{chunk}, true)) != 0 {
		t.Fatal("unindexed page accepted")
	}
	if _, err := s.indexWikiPage(id, ids[0]); err != nil {
		t.Fatal(err)
	}
	valid := s.filterWikiChunks([]RetrievedChunk{chunk}, true)
	if len(valid) != 1 {
		t.Fatal("valid page excluded")
	}
	text, refs := s.rag.BuildContext(valid)
	if !strings.Contains(text, "退款期限为七天。") || len(refs) != 2 || refs[1]["sourceChecksum"] != doc.ContentChecksum {
		t.Fatalf("missing evidence: %s %+v", text, refs)
	}
	old := chunk
	old.WikiRevision = 0
	if len(s.filterWikiChunks([]RetrievedChunk{old}, true)) != 0 {
		t.Fatal("old index accepted")
	}
	if _, err := s.ConfigureWiki(id, false); err != nil {
		t.Fatal(err)
	}
	if len(s.filterWikiChunks([]RetrievedChunk{chunk}, true)) != 0 {
		t.Fatal("disabled page accepted")
	}
	if _, err := s.ConfigureWiki(id, true); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(doc.Path, []byte("已修改为十四天"), 0600); err != nil {
		t.Fatal(err)
	}
	if len(s.filterWikiChunks([]RetrievedChunk{chunk}, true)) != 0 {
		t.Fatal("changed source accepted")
	}
	if _, err := s.DeleteDocument(id, doc.ID); err != nil {
		t.Fatal(err)
	}
	if len(s.filterWikiChunks([]RetrievedChunk{chunk}, false)) != 0 {
		t.Fatal("deleted source accepted")
	}
}

// TestWikiCaptureReadsSavedAnswer 验证沉淀使用已保存的回答，并拒绝证据版本变化。
func TestWikiCaptureReadsSavedAnswer(t *testing.T) {
	s, id, doc := wikiFixture(t, func(model.ChatCompletionRequest) (string, int) { return `{"pages":[]}`, 200 })
	store, err := NewSQLiteChatHistoryStore(filepath.Join(t.TempDir(), "chat.db"))
	if err != nil {
		t.Fatal(err)
	}
	s.chatHistory = store
	message := model.StoredChatMessage{ID: "answer", Role: "assistant", Content: "退款为七天", Metadata: map[string]any{"sources": []map[string]string{{"knowledgeBaseId": id, "documentId": doc.ID, "sourceChecksum": doc.ContentChecksum, "quote": "退款期限为七天。"}}}}
	if _, err := s.SaveConversation(model.SaveConversationRequest{ID: "chat", KnowledgeBaseID: id, Messages: []model.StoredChatMessage{message}}); err != nil {
		t.Fatal(err)
	}
	job, _, err := s.CaptureWikiAnswer(id, "chat", message.ID, "", "capture-1")
	if err != nil {
		t.Fatal(err)
	}
	job = waitWikiJob(t, s, id, job.ID)
	if job.Status != "succeeded" || job.CaptureText != message.Content || job.SourceVersions[doc.ID] != doc.ContentChecksum {
		t.Fatalf("capture: %+v", job)
	}
	if err := os.WriteFile(doc.Path, []byte("新规定"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.CaptureWikiAnswer(id, "chat", message.ID, "", "capture-2"); err == nil {
		t.Fatal("outdated answer accepted")
	}
	if _, _, err := s.CaptureWikiAnswer("another", "chat", message.ID, "", "capture-3"); err == nil {
		t.Fatal("cross-library answer accepted")
	}
}

// TestWikiDefaultEnabledPreservesOptOut 验证新库与旧库默认启用，但重启不覆盖显式关闭。
func TestWikiDefaultEnabledPreservesOptOut(t *testing.T) {
	dir := t.TempDir()
	store := NewAppStateStore(filepath.Join(dir, "state.json"))
	s := NewAppService(nil, store, nil, model.ServerConfig{UploadDir: dir})
	t.Cleanup(func() { _ = s.Close() })
	initial := s.ListKnowledgeBases()[0]
	if initial.Wiki == nil || !initial.Wiki.Enabled {
		t.Fatal("default library should enable Wiki")
	}
	created, err := s.CreateKnowledgeBase(model.KnowledgeBaseInput{Name: "新增知识库"})
	if err != nil || created.Wiki == nil || !created.Wiki.Enabled {
		t.Fatalf("new library: %+v %v", created.Wiki, err)
	}
	if _, err := s.ConfigureWiki(created.ID, false); err != nil {
		t.Fatal(err)
	}
	// 模拟旧版数据缺少 Wiki 字段，验证读取迁移不会回填历史任务。
	s.state.Mu.Lock()
	initial.Wiki = nil
	s.state.KnowledgeBases[initial.ID] = initial
	s.state.Mu.Unlock()
	if err := s.saveState(); err != nil {
		t.Fatal(err)
	}
	reloaded := NewAppService(nil, store, nil, model.ServerConfig{UploadDir: dir})
	t.Cleanup(func() { _ = reloaded.Close() })
	legacy, _ := reloaded.WikiSnapshot(initial.ID)
	closed, _ := reloaded.WikiSnapshot(created.ID)
	if !legacy.Wiki.Enabled || len(legacy.Wiki.Jobs) != 0 || closed.Wiki.Enabled {
		t.Fatalf("migration changed intent: legacy=%+v closed=%+v", legacy.Wiki, closed.Wiki)
	}
}

// TestWikiAutoCompileSkipsMissingThirdParty 验证原文入库与更新成功，无配置不产生自动任务或恢复待办。
func TestWikiAutoCompileSkipsMissingThirdParty(t *testing.T) {
	for _, cfg := range []model.ChatConfig{
		{},
		{Provider: "ollama", BaseURL: "http://localhost:11434", Model: "default"},
		{Provider: "openai-compatible", BaseURL: " ", Model: "fixture"},
		{Provider: "openai-compatible", BaseURL: "http://localhost", Model: " "},
	} {
		t.Run(cfg.Provider+cfg.Model, func(t *testing.T) {
			s, id, doc := wikiFixture(t, func(model.ChatCompletionRequest) (string, int) {
				t.Error("unconfigured automatic compilation called model")
				return "{}", 500
			})
			s.state.Mu.Lock()
			s.state.Config.Chat = cfg
			s.state.Mu.Unlock()
			indexed, err := s.IndexDocument(doc)
			if err != nil || indexed.Status != "indexed" {
				t.Fatalf("original indexing failed: %s %v", indexed.Status, err)
			}
			if _, err := s.RewriteDocumentContent(id, doc.ID, "退款期限变更为十四天。"); err != nil {
				t.Fatal(err)
			}
			s.recoverWikiJobs()
			view, err := s.WikiOverview(id)
			kb, _ := s.WikiSnapshot(id)
			if err != nil || view["enabled"] != true || view["autoCompileReady"] != false || len(kb.Wiki.Jobs) != 0 || len(kb.Wiki.PendingSources) != 0 {
				t.Fatalf("skip failed: %+v %v", view, err)
			}
		})
	}
	if !wikiAutoCompileReady(model.ChatConfig{Provider: "openai-compatible", BaseURL: "http://localhost", Model: "fixture"}) {
		t.Fatal("keyless compatible gateway must be accepted")
	}
}
