package service

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"ai-localbase/internal/model"
)

// wikiFixture 用本地 HTTP 替代外部模型；真实服务负责保存、编译、发布和索引。
func wikiFixture(t *testing.T, reply func(model.ChatCompletionRequest) (string, int)) (*AppService, string, model.Document) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/embeddings") {
			var request struct {
				Input []string `json:"input"`
			}
			_ = json.NewDecoder(r.Body).Decode(&request)
			data := []any{}
			for i := range request.Input {
				vector := make([]float64, 768)
				vector[0] = 1
				data = append(data, map[string]any{"index": i, "embedding": vector})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
			return
		}
		var request model.ChatCompletionRequest
		_ = json.NewDecoder(r.Body).Decode(&request)
		text, status := reply(request)
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": text}}}})
	}))
	t.Cleanup(server.Close)
	dir := t.TempDir()
	s := NewAppService(nil, NewAppStateStore(filepath.Join(dir, "state.json")), nil, model.ServerConfig{UploadDir: dir})
	t.Cleanup(func() { _ = s.Close() })
	s.state.Config.Chat = model.ChatConfig{Provider: "openai-compatible", BaseURL: server.URL, Model: "configured-model"}
	s.state.Config.Embedding = model.EmbeddingConfig{Provider: "openai-compatible", BaseURL: server.URL, Model: "fixture"}
	id := s.ListKnowledgeBases()[0].ID
	if _, err := s.ConfigureWiki(id, true); err != nil {
		t.Fatal(err)
	}
	body := "退款期限为七天。"
	path := filepath.Join(dir, "source.md")
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	doc := s.AddDocument(id, model.Document{ID: "source-test", KnowledgeBaseID: id, Name: "退款规则", Path: path, ContentChecksum: hashDocumentContent(body)})
	return s, id, doc
}

// waitWikiJob 等待可观察终态，超时给出真实阶段便于定位队列故障。
func waitWikiJob(t *testing.T, s *AppService, kbID, jobID string) model.WikiJob {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		job, err := s.GetWikiJob(kbID, jobID)
		if err != nil {
			t.Fatal(err)
		}
		if job.Status != "queued" && job.Status != "running" {
			return job
		}
		time.Sleep(5 * time.Millisecond)
	}
	job, _ := s.GetWikiJob(kbID, jobID)
	t.Fatalf("job timed out: %+v", job)
	return job
}

// TestWikiJobConfiguredModelAndIdempotency 验证配置复用、完整发布和重复请求不产生新版本。
func TestWikiJobConfiguredModelAndIdempotency(t *testing.T) {
	var calls atomic.Int32
	s, id, doc := wikiFixture(t, func(req model.ChatCompletionRequest) (string, int) {
		calls.Add(1)
		if req.Model != "configured-model" {
			t.Errorf("wrong model: %s", req.Model)
		}
		var input struct {
			Sources []struct {
				DocumentID string `json:"documentId"`
				Checksum   string `json:"checksum"`
				Content    string `json:"content"`
			} `json:"sources"`
		}
		_ = json.Unmarshal([]byte(req.Messages[len(req.Messages)-1].Content), &input)
		if len(input.Sources) != 1 {
			t.Error("missing source")
			return "{}", 200
		}
		source := input.Sources[0]
		plan := model.WikiPlan{Pages: []model.WikiPageChange{{Title: "退款期限", PageType: "topic", Markdown: source.Content, SourceRefs: []model.WikiSourceRef{{DocumentID: source.DocumentID, Checksum: source.Checksum, Quote: source.Content}}}}}
		body, _ := json.Marshal(plan)
		return string(body), 200
	})
	s.state.Config.Wiki.ModelTimeoutSeconds = 240
	req := model.WikiJobRequest{Kind: "ingest", DocumentIDs: []string{doc.ID}, RequestKey: "one"}
	job, created, err := s.StartWikiJob(id, req)
	if err != nil || !created {
		t.Fatalf("start: %v %v", created, err)
	}
	job = waitWikiJob(t, s, id, job.ID)
	if job.ModelTimeoutSeconds != 240 {
		t.Fatalf("task did not capture configured timeout: %d", job.ModelTimeoutSeconds)
	}
	if job.Status != "succeeded" || len(job.ChangedPageIDs) != 1 {
		t.Fatalf("job: %+v", job)
	}
	req.RequestKey = "two"
	repeated, created, err := s.StartWikiJob(id, req)
	if err != nil || created || repeated.ID != job.ID || calls.Load() != 1 {
		t.Fatalf("duplicate: %+v %v %v calls=%d", repeated, created, err, calls.Load())
	}
	kb, _ := s.WikiSnapshot(id)
	if len(kb.Wiki.PendingSources) != 0 {
		t.Fatal("successful input still pending")
	}
	page, _ := s.GetDocument(id, job.ChangedPageIDs[0])
	if page.Wiki.IndexedRevision != 1 {
		t.Fatal("page not indexed")
	}
}

// TestWikiJobRejectsInvalidModelOutput 验证格式错误、降级和单次网络预算，失败后支持第四次显式执行并保留累计次数。
func TestWikiJobRejectsInvalidModelOutput(t *testing.T) {
	for _, output := range []string{"{}", "null", "broken", "http-failure"} {
		t.Run(output, func(t *testing.T) {
			var calls atomic.Int32
			s, id, doc := wikiFixture(t, func(model.ChatCompletionRequest) (string, int) {
				calls.Add(1)
				if output == "http-failure" {
					return "", 503
				}
				return output, 200
			})
			job, _, err := s.StartWikiJob(id, model.WikiJobRequest{DocumentIDs: []string{doc.ID}, RequestKey: "bad"})
			if err != nil {
				t.Fatal(err)
			}
			for attempt := 1; attempt <= 4; attempt++ {
				job = waitWikiJob(t, s, id, job.ID)
				if job.ModelTimeoutSeconds != 180+(attempt-1)*60 {
					t.Fatalf("retry timeout snapshot: %d", job.ModelTimeoutSeconds)
				}
				callsPerAttempt := 2
				if output == "http-failure" {
					callsPerAttempt = 1
				}
				if job.Status != "failed" || job.Attempt != attempt || job.ModelCalls != attempt*callsPerAttempt || calls.Load() != int32(attempt*callsPerAttempt) {
					t.Fatalf("unexpected result %+v calls=%d", job, calls.Load())
				}
				if attempt < 4 {
					// 显式重试必须重新读取设置，不能沿用首次执行的超时。
					s.state.Mu.Lock()
					s.state.Config.Wiki.ModelTimeoutSeconds = 180 + attempt*60
					s.state.Mu.Unlock()
					if _, err = s.RetryWikiJob(id, job.ID); err != nil {
						t.Fatal(err)
					}
				}
			}
			kb, _ := s.WikiSnapshot(id)
			if len(kb.Documents) != 1 {
				t.Fatal("invalid output published")
			}
		})
	}
}

// TestWikiPlanCorrectsInvalidIssues 验证问题格式错误只重做一次，并保留有效问题及其页面关联。
func TestWikiPlanCorrectsInvalidIssues(t *testing.T) {
	for _, invalid := range []string{
		`{"pages":[],"issues":["格式错误"]}`,
		`{"pages":[],"issues":[{"type":"unsupported-claim","reason":"缺少页面"}]}`,
	} {
		t.Run(invalid, func(t *testing.T) {
			var calls atomic.Int32
			s, id, doc := wikiFixture(t, func(req model.ChatCompletionRequest) (string, int) {
				if calls.Add(1) == 1 {
					return invalid, 200
				}
				if !strings.Contains(req.Messages[0].Content, "上次输出未通过格式或来源检查") {
					t.Error("correction request missing validation error")
				}
				var input struct {
					Sources []struct {
						DocumentID string `json:"documentId"`
						Checksum   string `json:"checksum"`
						Content    string `json:"content"`
					} `json:"sources"`
				}
				if err := json.Unmarshal([]byte(req.Messages[len(req.Messages)-1].Content), &input); err != nil || len(input.Sources) != 1 {
					t.Error("correction request missing original source")
					return "{}", 200
				}
				source := input.Sources[0]
				plan := model.WikiPlan{
					Pages:  []model.WikiPageChange{{Key: "refund", Title: "退款期限", PageType: "topic", Markdown: source.Content, SourceRefs: []model.WikiSourceRef{{DocumentID: source.DocumentID, Checksum: source.Checksum, Quote: source.Content}}}},
					Issues: []model.WikiIssue{{Type: "unsupported-claim", PageIDs: []string{"refund"}, Reason: "需要补充依据"}},
				}
				body, _ := json.Marshal(plan)
				return string(body), 200
			})
			job, _, err := s.StartWikiJob(id, model.WikiJobRequest{DocumentIDs: []string{doc.ID}, RequestKey: "correct-issues"})
			if err != nil {
				t.Fatal(err)
			}
			job = waitWikiJob(t, s, id, job.ID)
			if job.Status != "succeeded" || calls.Load() != 2 || len(job.ChangedPageIDs) != 1 {
				t.Fatalf("job: %+v calls=%d", job, calls.Load())
			}
			kb, err := s.WikiSnapshot(id)
			if err != nil {
				t.Fatal(err)
			}
			if len(kb.Wiki.Issues) != 1 || len(kb.Wiki.Issues[0].PageIDs) != 1 || kb.Wiki.Issues[0].PageIDs[0] != job.ChangedPageIDs[0] {
				t.Fatalf("issues: %+v", kb.Wiki.Issues)
			}
		})
	}
}

// TestWikiExtractionRejectsNull 验证长文分段提取缺失结果时停止，不能把未读内容标记为成功。
func TestWikiExtractionRejectsNull(t *testing.T) {
	s, id, doc := wikiFixture(t, func(model.ChatCompletionRequest) (string, int) { return "null", 200 })
	if err := os.WriteFile(doc.Path, []byte(strings.Repeat("正文", 7000)), 0600); err != nil {
		t.Fatal(err)
	}
	job, _, err := s.StartWikiJob(id, model.WikiJobRequest{DocumentIDs: []string{doc.ID}, RequestKey: "null-extract"})
	if err != nil {
		t.Fatal(err)
	}
	job = waitWikiJob(t, s, id, job.ID)
	if job.Status != "failed" || job.ProcessedSegments != 0 || !strings.Contains(job.Error, "JSON array") {
		t.Fatalf("null extraction accepted: %+v", job)
	}
}

// TestWikiLongSourceReadsTail 验证长文尾部进入提取与最终综合，没有预览截断。
func TestWikiLongSourceReadsTail(t *testing.T) {
	var sawTail atomic.Bool
	s, id, doc := wikiFixture(t, func(req model.ChatCompletionRequest) (string, int) {
		input := req.Messages[len(req.Messages)-1].Content
		if strings.HasPrefix(req.Messages[0].Content, "提取") {
			var segment struct {
				Lines []struct {
					Text string `json:"text"`
				} `json:"lines"`
			}
			if err := json.Unmarshal([]byte(input), &segment); err != nil {
				t.Fatal(err)
			}
			parts := []string{}
			for _, line := range segment.Lines {
				parts = append(parts, line.Text)
			}
			input = strings.Join(parts, "\n")
			quote := string([]rune(input)[:1])
			if strings.Contains(input, "尾部唯一条款") {
				quote = "尾部唯一条款"
				sawTail.Store(true)
			}
			b, _ := json.Marshal([]map[string]string{{"claim": quote, "quote": quote}})
			return string(b), 200
		}
		if !strings.Contains(input, "尾部唯一条款") {
			t.Error("tail missing from synthesis")
		}
		return `{"pages":[],"issues":[]}`, 200
	})
	body := strings.Repeat("正", 24000) + "尾部唯一条款"
	if err := os.WriteFile(doc.Path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	job, _, err := s.StartWikiJob(id, model.WikiJobRequest{DocumentIDs: []string{doc.ID}, RequestKey: "long"})
	if err != nil {
		t.Fatal(err)
	}
	job = waitWikiJob(t, s, id, job.ID)
	if job.Status != "succeeded" || job.ProcessedSegments != 7 || job.ModelCalls != 8 || !sawTail.Load() {
		t.Fatalf("incomplete coverage: %+v", job)
	}
}

// TestWikiRecoveryPreservesPublishedStage 验证中断恢复保留索引阶段，不重新生成页面。
func TestWikiRecoveryPreservesPublishedStage(t *testing.T) {
	s, id, _ := wikiFixture(t, func(model.ChatCompletionRequest) (string, int) { t.Error("unexpected model call"); return "{}", 200 })
	if err := s.commitWikiChange(id, func(kb *model.KnowledgeBase) error {
		kb.Wiki.PendingSources = map[string]string{}
		kb.Wiki.Jobs = []model.WikiJob{{ID: "interrupted", KnowledgeBaseID: id, Status: "running", Stage: "index", Attempt: 1}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	reloaded := NewAppService(nil, s.store, nil, s.serverConfig)
	defer reloaded.Close()
	job, err := reloaded.GetWikiJob(id, "interrupted")
	if err != nil || job.Status != "failed" || job.Stage != "index" || job.Error != "interrupted" {
		t.Fatalf("recovery: %+v %v", job, err)
	}
	if _, err = reloaded.RetryWikiJob(id, job.ID); err != nil {
		t.Fatal(err)
	}
	job = waitWikiJob(t, reloaded, id, job.ID)
	if job.Status != "succeeded" || job.ModelCalls != 0 {
		t.Fatalf("index retry: %+v", job)
	}
}

// TestWikiConcurrentRequestDeduplication 验证同来源并发提交只创建一个持久任务。
func TestWikiConcurrentRequestDeduplication(t *testing.T) {
	s, id, doc := wikiFixture(t, func(model.ChatCompletionRequest) (string, int) { return `{"pages":[]}`, 200 })
	s.wikiClosed = true
	var workers sync.WaitGroup
	ids := make(chan string, 8)
	failures := make(chan error, 8)
	for i := 0; i < 8; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			job, _, err := s.StartWikiJob(id, model.WikiJobRequest{DocumentIDs: []string{doc.ID}, RequestKey: "parallel"})
			if err != nil {
				failures <- err
				return
			}
			ids <- job.ID
		}()
	}
	workers.Wait()
	close(ids)
	close(failures)
	for err := range failures {
		t.Error(err)
	}
	first := ""
	for jobID := range ids {
		if first == "" {
			first = jobID
		}
		if first != jobID {
			t.Fatal("duplicate job")
		}
	}
	kb, _ := s.WikiSnapshot(id)
	if len(kb.Wiki.Jobs) != 1 {
		t.Fatalf("jobs=%d", len(kb.Wiki.Jobs))
	}
}

// TestWikiIndexFailureRetriesWithoutModel 验证发布成功后索引失败只恢复索引，不增加页面修订。
func TestWikiIndexFailureRetriesWithoutModel(t *testing.T) {
	var calls atomic.Int32
	s, id, doc := wikiFixture(t, func(req model.ChatCompletionRequest) (string, int) {
		calls.Add(1)
		var input struct {
			Sources []struct {
				DocumentID string `json:"documentId"`
				Checksum   string `json:"checksum"`
				Content    string `json:"content"`
			} `json:"sources"`
		}
		_ = json.Unmarshal([]byte(req.Messages[len(req.Messages)-1].Content), &input)
		ref := input.Sources[0]
		plan := model.WikiPlan{Pages: []model.WikiPageChange{{Title: "退款索引测试", PageType: "topic", Markdown: ref.Content, SourceRefs: []model.WikiSourceRef{{DocumentID: ref.DocumentID, Checksum: ref.Checksum, Quote: ref.Content}}}}}
		body, _ := json.Marshal(plan)
		return string(body), 200
	})
	var fail atomic.Bool
	fail.Store(true)
	qdrant := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/search") {
			_, _ = w.Write([]byte(`{"result":[]}`))
			return
		}
		if fail.Load() {
			w.WriteHeader(400)
			_, _ = w.Write([]byte(`{"status":{"error":"fixture index failure"}}`))
			return
		}
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`{"result":{"config":{"params":{"vectors":{"size":768,"distance":"Cosine"}}}}}`))
			return
		}
		_, _ = w.Write([]byte(`{"result":{"status":"completed"},"status":"ok"}`))
	}))
	defer qdrant.Close()
	s.qdrant = NewQdrantService(model.ServerConfig{QdrantURL: qdrant.URL, QdrantVectorSize: 768, QdrantDistance: "Cosine"})
	job, _, err := s.StartWikiJob(id, model.WikiJobRequest{DocumentIDs: []string{doc.ID}, RequestKey: "index-failure"})
	if err != nil {
		t.Fatal(err)
	}
	job = waitWikiJob(t, s, id, job.ID)
	if job.Status != "failed" || job.Stage != "index" || len(job.ChangedPageIDs) != 1 {
		t.Fatalf("wrong failure stage: %+v", job)
	}
	page, _ := s.GetDocument(id, job.ChangedPageIDs[0])
	if page.Wiki.IndexedRevision != 0 || page.Wiki.Revision != 1 {
		t.Fatalf("index state: %+v", page.Wiki)
	}
	fail.Store(false)
	if _, err = s.RetryWikiJob(id, job.ID); err != nil {
		t.Fatal(err)
	}
	job = waitWikiJob(t, s, id, job.ID)
	page, _ = s.GetDocument(id, job.ChangedPageIDs[0])
	if job.Status != "succeeded" || calls.Load() != 1 || page.Wiki.Revision != 1 || page.Wiki.IndexedRevision != 1 {
		t.Fatalf("retry regenerated: %+v calls=%d page=%+v", job, calls.Load(), page.Wiki)
	}
}

// TestWikiSourceSegmentsCoverage 验证多字节原文在段边界无遗漏、重复或非法字符。
func TestWikiSourceSegmentsCoverage(t *testing.T) {
	for _, size := range []int{4000, 4001, 11999, 12000, 12001, 24001} {
		body := []rune(strings.Repeat("中🙂", size/2) + strings.Repeat("尾", size%2))
		segments := wikiSourceSegments("doc", "version", body, nil)
		var joined strings.Builder
		keys := map[string]bool{}
		for _, segment := range segments {
			if len([]rune(segment.text)) > 4000 || keys[segment.key] {
				t.Fatalf("invalid segment for size %d: %s", size, segment.key)
			}
			keys[segment.key] = true
			joined.WriteString(segment.text)
		}
		if joined.String() != string(body) {
			t.Fatalf("source coverage changed for size %d", size)
		}
	}
}

// TestWikiExtractionResumesLegacyAndRangeCache 验证升级后复用两种已完成缓存，只请求剩余原文。
func TestWikiExtractionResumesLegacyAndRangeCache(t *testing.T) {
	var calls atomic.Int32
	s, id, doc := wikiFixture(t, func(req model.ChatCompletionRequest) (string, int) {
		calls.Add(1)
		input := req.Messages[len(req.Messages)-1].Content
		if strings.HasPrefix(req.Messages[0].Content, "提取") {
			var segment struct {
				Lines []struct {
					Text string `json:"text"`
				} `json:"lines"`
			}
			if err := json.Unmarshal([]byte(input), &segment); err != nil {
				t.Fatal(err)
			}
			parts := []string{}
			for _, line := range segment.Lines {
				parts = append(parts, line.Text)
			}
			input = strings.Join(parts, "\n")
			if input != "尾部条款" {
				t.Errorf("completed segment requested again, length=%d", len([]rune(input)))
			}
			return `[{"claim":"尾部条款","quote":"尾部条款"}]`, 200
		}
		for _, quote := range []string{"甲", "乙", "尾部条款"} {
			if !strings.Contains(input, quote) {
				t.Errorf("cached evidence missing: %s", quote)
			}
		}
		return `{"pages":[],"issues":[]}`, 200
	})
	s.wikiClosed = true
	body := strings.Repeat("甲", 12000) + strings.Repeat("乙", 4000) + "尾部条款"
	if err := os.WriteFile(doc.Path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	job, _, err := s.StartWikiJob(id, model.WikiJobRequest{DocumentIDs: []string{doc.ID}, RequestKey: "resume-segments"})
	if err != nil {
		t.Fatal(err)
	}
	prefix := doc.ID + ":" + job.SourceVersions[doc.ID] + ":"
	job.Extracts[prefix+"0"] = `[{"claim":"甲","quote":"甲"}]`
	job.Extracts[prefix+"runes:12000:16000"] = `[{"claim":"乙","quote":"乙"}]`
	job.ModelCalls = 2
	if err := s.runWikiJob(job); err != nil {
		t.Fatal(err)
	}
	job, _ = s.GetWikiJob(id, job.ID)
	if job.Status != "succeeded" || calls.Load() != 2 || job.ModelCalls != 4 || job.ProcessedSegments != 3 {
		t.Fatalf("cache recovery failed: %+v new calls=%d", job, calls.Load())
	}
}

// TestWikiExtractionCorrectsQuote 验证伪造引文必须纠正，且纠正后仍覆盖尾段。
func TestWikiExtractionCorrectsQuote(t *testing.T) {
	var extracts atomic.Int32
	var corrected atomic.Bool
	s, id, doc := wikiFixture(t, func(req model.ChatCompletionRequest) (string, int) {
		input := req.Messages[len(req.Messages)-1].Content
		if strings.HasPrefix(req.Messages[0].Content, "提取") {
			var segment struct {
				Lines []struct {
					Text string `json:"text"`
				} `json:"lines"`
			}
			if err := json.Unmarshal([]byte(input), &segment); err != nil {
				t.Fatal(err)
			}
			parts := []string{}
			for _, line := range segment.Lines {
				parts = append(parts, line.Text)
			}
			input = strings.Join(parts, "\n")
			if extracts.Add(1) == 1 {
				return `[{"claim":"不存在的条款","quote":"伪造引文"}]`, 200
			}
			if strings.Contains(req.Messages[0].Content, "上次提取未通过校验") {
				corrected.Store(true)
			}
			quote := string([]rune(input)[:1])
			b, _ := json.Marshal([]map[string]string{{"claim": quote, "quote": quote}})
			return string(b), 200
		}
		if !strings.Contains(input, "乙") {
			t.Error("tail was skipped after correction")
		}
		return `{"pages":[],"issues":[]}`, 200
	})
	if err := os.WriteFile(doc.Path, []byte(strings.Repeat("甲", 4000)+"乙"), 0600); err != nil {
		t.Fatal(err)
	}
	job, _, err := s.StartWikiJob(id, model.WikiJobRequest{DocumentIDs: []string{doc.ID}, RequestKey: "correct-extract"})
	if err != nil {
		t.Fatal(err)
	}
	job = waitWikiJob(t, s, id, job.ID)
	if job.Status != "succeeded" || extracts.Load() != 3 || !corrected.Load() || job.ProcessedSegments != 2 {
		t.Fatalf("extraction correction failed: %+v", job)
	}
}

// TestWikiExtractionKeepsRequestBudget 验证缩小分段不会重置累计预算，预算耗尽前的成果仍持久保存。
func TestWikiExtractionKeepsRequestBudget(t *testing.T) {
	var calls atomic.Int32
	s, id, doc := wikiFixture(t, func(model.ChatCompletionRequest) (string, int) {
		calls.Add(1)
		return `[{"claim":"甲","quote":"甲"}]`, 200
	})
	s.wikiClosed = true
	if err := os.WriteFile(doc.Path, []byte(strings.Repeat("甲", 4000)+"乙"), 0600); err != nil {
		t.Fatal(err)
	}
	job, _, err := s.StartWikiJob(id, model.WikiJobRequest{DocumentIDs: []string{doc.ID}, RequestKey: "segment-budget"})
	if err != nil {
		t.Fatal(err)
	}
	job.ModelCalls = 31
	job.ModelCallLimit = 32
	segments := wikiSourceSegments(doc.ID, job.SourceVersions[doc.ID], []rune(strings.Repeat("甲", 4000)+"乙"), job.Extracts)
	if _, err := s.extractWikiSegment(&job, s.currentChatConfig(), segments[0]); err != nil {
		t.Fatal(err)
	}
	_, err = s.extractWikiSegment(&job, s.currentChatConfig(), segments[1])
	budgetError, ok := err.(*WikiError)
	if !ok || budgetError.Code != "needs_split" || job.ModelCalls != 32 || calls.Load() != 1 || len(job.Extracts) != 1 {
		t.Fatalf("budget or completed cache was lost: %+v", job)
	}
}

// TestWikiModelGateAcrossLibraries 通过六个库的真实 HTTP 请求验证全站五路上限、等待预算、失败释放与关闭唤醒。
func TestWikiModelGateAcrossLibraries(t *testing.T) {
	for _, name := range []string{"success", "upstream_error", "close"} {
		t.Run(name, func(t *testing.T) {
			entered := make(chan struct{}, 6)
			release := make(chan struct{})
			var releaseOnce sync.Once
			var active, peak atomic.Int32
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			defer unblock()
			s, firstKB, doc := wikiFixture(t, func(model.ChatCompletionRequest) (string, int) {
				current := active.Add(1)
				defer active.Add(-1)
				for old := peak.Load(); current > old; old = peak.Load() {
					if peak.CompareAndSwap(old, current) {
						break
					}
				}
				entered <- struct{}{}
				<-release
				if name == "upstream_error" {
					return "", http.StatusServiceUnavailable
				}
				return `{"pages":[],"issues":[]}`, 200
			})
			jobs := make([]model.WikiJob, 0, 6)
			// 前五个请求保持未响应，确保第六个库在所有名额占用时进入队列。
			for i := 0; i < 6; i++ {
				kbID := firstKB
				if i > 0 {
					kb, err := s.CreateKnowledgeBase(model.KnowledgeBaseInput{Name: fmt.Sprintf("并发验证%d", i)})
					if err != nil {
						t.Fatal(err)
					}
					kbID = kb.ID
				}
				source := doc
				source.ID, source.KnowledgeBaseID = fmt.Sprintf("gate-source-%d", i), kbID
				s.AddDocument(kbID, source)
				job, _, err := s.StartWikiJob(kbID, model.WikiJobRequest{DocumentIDs: []string{source.ID}, RequestKey: source.ID})
				if err != nil {
					t.Fatal(err)
				}
				jobs = append(jobs, job)
				if i < 5 {
					select {
					case <-entered:
					case <-time.After(3 * time.Second):
						t.Fatalf("parallel request %d did not start", i+1)
					}
				}
			}
			select {
			case <-entered:
				t.Fatal("sixth model request escaped shared gate")
			case <-time.After(100 * time.Millisecond):
			}
			last := jobs[5]
			waiting, err := s.GetWikiJob(last.KnowledgeBaseID, last.ID)
			if err != nil || waiting.Status != "running" || waiting.ModelCalls != 0 || peak.Load() != 5 {
				t.Fatalf("waiting or concurrency incorrect: %+v, peak=%d, error=%v", waiting, peak.Load(), err)
			}
			if name == "close" {
				closed := make(chan struct{})
				go func() { _ = s.Close(); close(closed) }()
				last = waitWikiJob(t, s, last.KnowledgeBaseID, last.ID)
				if last.Status != "failed" || last.ModelCalls != 0 || !strings.Contains(last.Error, "closing") {
					t.Fatalf("waiting request did not stop: %+v", last)
				}
				unblock()
				select {
				case <-closed:
				case <-time.After(time.Second):
					t.Fatal("service close did not finish")
				}
			} else {
				unblock()
			}
			for i, job := range jobs {
				finished := waitWikiJob(t, s, job.KnowledgeBaseID, job.ID)
				expectedStatus, expectedCalls := "succeeded", 1
				if name == "upstream_error" {
					expectedStatus = "failed"
				} else if name == "close" && i == 5 {
					expectedStatus, expectedCalls = "failed", 0
				}
				if finished.Status != expectedStatus || finished.ModelCalls != expectedCalls {
					t.Fatalf("job did not finish correctly: %+v", finished)
				}
			}
			if peak.Load() != 5 {
				t.Fatalf("concurrency limit changed while releasing slots: %d", peak.Load())
			}
		})
	}
}

// TestWikiSameLibraryRemainsSerial 验证全站还有空闲名额时，同库第二份资料仍等待第一份完成发布。
func TestWikiSameLibraryRemainsSerial(t *testing.T) {
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	s, kbID, doc := wikiFixture(t, func(model.ChatCompletionRequest) (string, int) {
		entered <- struct{}{}
		<-release
		return `{"pages":[],"issues":[]}`, http.StatusOK
	})
	first, _, err := s.StartWikiJob(kbID, model.WikiJobRequest{DocumentIDs: []string{doc.ID}, RequestKey: "same-library-first"})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("first request did not start")
	}
	secondDoc := doc
	secondDoc.ID = "same-library-second-source"
	s.AddDocument(kbID, secondDoc)
	second, _, err := s.StartWikiJob(kbID, model.WikiJobRequest{DocumentIDs: []string{secondDoc.ID}, RequestKey: "same-library-second"})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
		t.Fatal("same-library jobs ran concurrently")
	case <-time.After(100 * time.Millisecond):
	}
	waiting, err := s.GetWikiJob(kbID, second.ID)
	if err != nil || waiting.Status != "queued" || waiting.ModelCalls != 0 {
		t.Fatalf("same-library job did not remain queued: %+v, %v", waiting, err)
	}
	unblock()
	for _, job := range []model.WikiJob{first, second} {
		finished := waitWikiJob(t, s, kbID, job.ID)
		if finished.Status != "succeeded" || finished.ModelCalls != 1 {
			t.Fatalf("same-library job failed after release: %+v", finished)
		}
	}
}

// TestWikiRecoveryReusesQueuedSources 验证恢复直接复用已排队任务，同时仍为漏建任务的待整理资料补入队。
func TestWikiRecoveryReusesQueuedSources(t *testing.T) {
	s, kbID, doc := wikiFixture(t, func(model.ChatCompletionRequest) (string, int) {
		t.Error("recovery fixture must not call the model")
		return "", http.StatusInternalServerError
	})
	// 只验证持久队列恢复，关闭消费者以便精确检查排队结果。
	s.wikiRunMu.Lock()
	s.wikiClosed = true
	s.wikiRunMu.Unlock()
	queued, _, err := s.StartWikiJob(kbID, model.WikiJobRequest{DocumentIDs: []string{doc.ID}, RequestKey: "recovery-queued"})
	if err != nil {
		t.Fatal(err)
	}
	pending, err := s.GetDocument(kbID, doc.ID)
	if err != nil {
		t.Fatal(err)
	}
	pending.ID = "recovery-pending-source"
	pending.Path = filepath.Join(t.TempDir(), "pending.md")
	if err := os.WriteFile(pending.Path, []byte("退款期限为七天。"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.commitWikiChange(kbID, func(kb *model.KnowledgeBase) error {
		kb.Documents = append(kb.Documents, pending)
		kb.Wiki.PendingSources[pending.ID] = pending.ContentChecksum
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// 已排队任务绑定原版本；磁盘外部变化应留给执行阶段校验，恢复不得重新入库或创建另一任务。
	if err := os.WriteFile(doc.Path, []byte("资料已被外部改动。"), 0600); err != nil {
		t.Fatal(err)
	}
	s.recoverWikiJobs()
	kb, err := s.WikiSnapshot(kbID)
	if err != nil {
		t.Fatal(err)
	}
	if len(kb.Wiki.Jobs) != 2 {
		t.Fatalf("queued source duplicated or missing source not enqueued: %+v", kb.Wiki.Jobs)
	}
	for _, job := range kb.Wiki.Jobs {
		if job.Status != "queued" || job.Attempt != 0 || job.ModelCalls != 0 {
			t.Fatalf("recovery executed or changed pending task: %+v", job)
		}
		if job.ID != queued.ID && (len(job.DocumentIDs) != 1 || job.DocumentIDs[0] != pending.ID) {
			t.Fatalf("recovery created an unrelated task: %+v", job)
		}
	}
}
