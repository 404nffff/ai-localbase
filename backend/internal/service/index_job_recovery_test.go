package service

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"ai-localbase/internal/model"
)

// stagedJobFixture 通过真实暂存入口准备上传，并由本地 HTTP 服务控制嵌入时序。
func stagedJobFixture(t *testing.T, beforeEmbedding func()) (*AppService, string, model.StagedUpload) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if beforeEmbedding != nil {
			beforeEmbedding()
		}
		var request struct {
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		data := []any{}
		for i := range request.Input {
			vector := make([]float64, 768)
			vector[0] = 1
			data = append(data, map[string]any{"index": i, "embedding": vector})
		}
		json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	t.Cleanup(server.Close)
	dir := t.TempDir()
	s := NewAppService(nil, NewAppStateStore(filepath.Join(dir, "state.json")), nil, model.ServerConfig{UploadDir: filepath.Join(dir, "uploads"), StagingDir: filepath.Join(dir, "staging")})
	t.Cleanup(func() { s.Close() })
	s.state.Config.Embedding = model.EmbeddingConfig{Provider: "openai-compatible", BaseURL: server.URL, Model: "fixture"}
	if err := s.saveState(); err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "fixture.md")
	if err != nil {
		t.Fatal(err)
	}
	part.Write([]byte("导入任务检查点用于恢复已发布文档。"))
	writer.Close()
	request := httptest.NewRequest(http.MethodPost, "/", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	if err := request.ParseMultipartForm(1024 * 1024); err != nil {
		t.Fatal(err)
	}
	upload, err := s.StageUploadedFile(request.MultipartForm.File["file"][0])
	if err != nil {
		t.Fatal(err)
	}
	return s, s.ListKnowledgeBases()[0].ID, upload
}

// TestIndexJobRestartCheckpoint 模拟文档已发布而条目检查点未写，恢复不得重复索引。
func TestIndexJobRestartCheckpoint(t *testing.T) {
	s, kbID, upload := stagedJobFixture(t, nil)
	job, err := s.StartBatchIndexJob(kbID, []string{upload.ID})
	if err != nil {
		t.Fatal(err)
	}
	s.jobsWorkers.Wait()
	finished, _ := s.GetIndexJob(job.ID)
	if finished.Status != indexJobSucceeded || finished.Completed != 1 {
		t.Fatalf("job failed: %+v", finished)
	}
	state, _ := s.lookupIndexJob(job.ID)
	state.mu.Lock()
	state.job.Status, state.job.Items, state.job.Completed = indexJobRunning, nil, 0
	if err := s.saveIndexJobLocked(state); err != nil {
		t.Fatal(err)
	}
	state.mu.Unlock()
	s.Close()
	restarted := NewAppService(nil, s.store, nil, s.serverConfig)
	defer restarted.Close()
	restarted.jobsWorkers.Wait()
	recovered, err := restarted.GetIndexJob(job.ID)
	if err != nil || recovered.Status != indexJobSucceeded || recovered.Completed != 1 {
		t.Fatalf("recovery: %+v %v", recovered, err)
	}
	documents, _ := restarted.GetKnowledgeBaseDocuments(kbID)
	if len(documents) != 1 || documents[0].ID != finished.Items[0].DocumentID {
		t.Fatalf("recovery duplicated document: %+v", documents)
	}
	if _, err := restarted.RetryIndexJob(job.ID); err == nil {
		t.Fatal("successful job retried")
	}
}

// TestIndexJobCancelBeforePublish 验证处理中取消、重复提交和取消后重启均不能发布文档。
func TestIndexJobCancelBeforePublish(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	s, kbID, upload := stagedJobFixture(t, func() { once.Do(func() { close(entered) }); <-release })
	job, err := s.StartBatchIndexJob(kbID, []string{upload.ID})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		close(release)
		t.Fatal("embedding did not start")
	}
	if _, err := s.StartBatchIndexJob(kbID, []string{upload.ID}); err == nil {
		close(release)
		t.Fatal("upload claimed twice")
	}
	if _, err := s.CancelIndexJob(job.ID); err != nil {
		close(release)
		t.Fatal(err)
	}
	if _, err := s.RetryIndexJob(job.ID); err == nil {
		close(release)
		t.Fatal("retry raced active canceled worker")
	}
	close(release)
	s.jobsWorkers.Wait()
	documents, _ := s.GetKnowledgeBaseDocuments(kbID)
	if len(documents) != 0 {
		t.Fatal("canceled candidate was published")
	}
	s.Close()
	restarted := NewAppService(nil, s.store, nil, s.serverConfig)
	defer restarted.Close()
	restarted.jobsWorkers.Wait()
	recovered, _ := restarted.GetIndexJob(job.ID)
	if recovered.Status != indexJobCanceled {
		t.Fatalf("canceled job revived: %+v", recovered)
	}
	// 明确请求重试后保留的暂存文件仍可用。
	if _, err := restarted.RetryIndexJob(job.ID); err != nil {
		t.Fatal(err)
	}
	restarted.jobsWorkers.Wait()
	retried, _ := restarted.GetIndexJob(job.ID)
	if retried.Status != indexJobSucceeded {
		t.Fatalf("retry failed: %+v", retried)
	}
}
