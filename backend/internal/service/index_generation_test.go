package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"ai-localbase/internal/model"
)

// TestIndexGenerationRecovery 验证写入前登记、清理失败留痕及跨代恢复不删当前代际。
func TestIndexGenerationRecovery(t *testing.T) {
	var mu sync.Mutex
	fail := true
	deleted := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("wait") != "true" || !strings.HasSuffix(r.URL.Path, "/points/delete") {
			t.Errorf("cleanup request must wait: %s", r.URL.String())
		}
		mu.Lock()
		defer mu.Unlock()
		if fail {
			w.WriteHeader(503)
			return
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		body, _ := json.Marshal(request)
		deleted = append(deleted, string(body))
		w.Write([]byte(`{"result":{"status":"completed"},"status":"ok"}`))
	}))
	defer server.Close()
	dir := t.TempDir()
	s := NewAppService(nil, NewAppStateStore(filepath.Join(dir, "state.json")), nil, model.ServerConfig{UploadDir: dir, StagingDir: filepath.Join(dir, "staging")})
	defer s.Close()
	s.qdrant = NewQdrantService(model.ServerConfig{QdrantURL: server.URL, QdrantVectorSize: 3})
	kbID := s.ListKnowledgeBases()[0].ID
	doc := s.AddDocument(kbID, model.Document{ID: "doc-generation", KnowledgeBaseID: kbID, IndexGeneration: "old"})
	finish, err := s.beginIndexGeneration(&doc, true)
	if err != nil {
		t.Fatal(err)
	}
	candidate := doc.IndexGeneration
	if _, err := s.ReplaceDocument(kbID, doc); err != nil {
		t.Fatal(err)
	}
	finish()
	paths, _ := filepath.Glob(s.store.Path() + ".indexes/*.json")
	if len(paths) != 1 {
		t.Fatalf("cleanup failure lost journal: %v", paths)
	}
	// 暂时停用 Qdrant 不代表旧代已经删除，恢复记录必须留到连接重新启用。
	qdrant := s.qdrant
	s.qdrant = nil
	s.recoverIndexGenerations()
	paths, _ = filepath.Glob(s.store.Path() + ".indexes/*.json")
	if len(paths) != 1 {
		t.Fatalf("disabled Qdrant lost cleanup journal: %v", paths)
	}
	s.qdrant = qdrant
	// 模拟又发布一代；恢复必须回收候选和更老代，不能误删最新代。
	doc.IndexGeneration = "latest"
	if _, err := s.ReplaceDocument(kbID, doc); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	fail = false
	mu.Unlock()
	s.recoverIndexGenerations()
	paths, _ = filepath.Glob(s.store.Path() + ".indexes/*.json")
	if len(paths) != 0 {
		t.Fatal("cleanup did not finish")
	}
	mu.Lock()
	defer mu.Unlock()
	joined := strings.Join(deleted, "\n")
	if len(deleted) != 2 || !strings.Contains(joined, candidate) || !strings.Contains(joined, `"old"`) || strings.Contains(joined, "latest") {
		t.Fatalf("wrong generation cleanup: %s", joined)
	}
}

// TestFailedReindexRetainsPublishedGeneration 验证嵌入失败不改变当前指针或抹掉旧索引状态。
func TestFailedReindexRetainsPublishedGeneration(t *testing.T) {
	s, kbID, upload := stagedJobFixture(t, nil)
	job, err := s.StartBatchIndexJob(kbID, []string{upload.ID})
	if err != nil {
		t.Fatal(err)
	}
	s.jobsWorkers.Wait()
	completed, _ := s.GetIndexJob(job.ID)
	if completed.Status != indexJobSucceeded {
		t.Fatalf("initial import failed: %+v", completed)
	}
	doc, _ := s.GetDocument(kbID, completed.Items[0].DocumentID)
	failedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":[]}`))
	}))
	defer failedServer.Close()
	s.state.Config.Embedding.BaseURL = failedServer.URL
	if _, err := s.ReindexDocument(kbID, doc.ID); err == nil {
		t.Fatal("embedding failure reported success")
	}
	after, _ := s.GetDocument(kbID, doc.ID)
	if after.IndexGeneration != doc.IndexGeneration || after.Status != "indexed" || after.IndexError == "" {
		t.Fatalf("failed rebuild replaced valid generation: %+v", after)
	}
}

// TestConcurrentAppendRetainsBothUpdates 验证同文档并发追加读取最新正文，不丢失任一增量。
func TestConcurrentAppendRetainsBothUpdates(t *testing.T) {
	s, kbID, upload := stagedJobFixture(t, nil)
	job, err := s.StartBatchIndexJob(kbID, []string{upload.ID})
	if err != nil {
		t.Fatal(err)
	}
	s.jobsWorkers.Wait()
	completed, _ := s.GetIndexJob(job.ID)
	if completed.Status != indexJobSucceeded {
		t.Fatalf("initial job failed: %+v", completed)
	}
	docID := completed.Items[0].DocumentID
	errors := make(chan error, 2)
	for _, suffix := range []string{"\n追加甲：保留业务约束。", "\n追加乙：保留审计证据。"} {
		go func(text string) { _, err := s.AppendDocumentContent(kbID, docID, text); errors <- err }(suffix)
	}
	for i := 0; i < 2; i++ {
		if err := <-errors; err != nil {
			t.Fatal(err)
		}
	}
	doc, _ := s.GetDocument(kbID, docID)
	body, err := os.ReadFile(doc.Path)
	if err != nil || !strings.Contains(string(body), "追加甲") || !strings.Contains(string(body), "追加乙") {
		t.Fatalf("lost append: %s %v", body, err)
	}
}

// TestIndexGenerationVisibilityAndSaveFailure 验证旧代不可召回、状态保存失败不发布新指针。
func TestIndexGenerationVisibilityAndSaveFailure(t *testing.T) {
	dir := t.TempDir()
	s := NewAppService(nil, nil, nil, model.ServerConfig{UploadDir: dir, StagingDir: filepath.Join(dir, "staging")})
	defer s.Close()
	kbID := s.ListKnowledgeBases()[0].ID
	doc := s.AddDocument(kbID, model.Document{ID: "doc-generation", KnowledgeBaseID: kbID, IndexGeneration: "current"})
	chunks := []RetrievedChunk{{DocumentChunk: DocumentChunk{DocumentID: doc.ID, KnowledgeBaseID: kbID, IndexGeneration: "old"}}, {DocumentChunk: DocumentChunk{DocumentID: doc.ID, KnowledgeBaseID: kbID, IndexGeneration: "current"}}}
	got := s.filterWikiChunks(chunks, false)
	if len(got) != 1 || got[0].IndexGeneration != "current" {
		t.Fatalf("stale generation visible: %+v", got)
	}
	blocker := filepath.Join(dir, "blocked")
	if err := os.WriteFile(blocker, []byte("file"), 0600); err != nil {
		t.Fatal(err)
	}
	s.store = NewAppStateStore(filepath.Join(blocker, "state.json"))
	doc.IndexGeneration = "candidate"
	if _, err := s.ReplaceDocument(kbID, doc); err == nil {
		t.Fatal("save failure reported success")
	}
	current, _ := s.GetDocument(kbID, doc.ID)
	if current.IndexGeneration != "current" {
		t.Fatal("failed save published candidate")
	}
	if _, err := s.publishIndexedDocument(model.Document{ID: "new", KnowledgeBaseID: kbID}, model.Document{}); err == nil {
		t.Fatal("new document save failure reported success")
	}
	if _, err := s.GetDocument(kbID, "new"); err == nil {
		t.Fatal("new document leaked before save")
	}
}
