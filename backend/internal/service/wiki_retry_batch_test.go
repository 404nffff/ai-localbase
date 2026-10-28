package service

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"ai-localbase/internal/model"
)

// TestRetryFailedWikiJobs 验证跨分页快照、来源校验、执行上限和重复提交，使用真实重试方法。
func TestRetryFailedWikiJobs(t *testing.T) {
	s := NewAppService(nil, nil, nil, model.ServerConfig{UploadDir: t.TempDir()})
	t.Cleanup(func() { _ = s.Close() })
	// 暂停消费者以检查提交结果，避免真实后台任务在断言前改变状态。
	s.wikiRunMu.Lock()
	s.wikiClosed = true
	s.wikiRunMu.Unlock()
	s.wikiWorkers.Wait()
	id := s.ListKnowledgeBases()[0].ID
	err := s.commitWikiChange(id, func(kb *model.KnowledgeBase) error {
		for i := 0; i < 65; i++ {
			kb.Wiki.Jobs = append(kb.Wiki.Jobs, model.WikiJob{ID: fmt.Sprintf("retry-%d", i), Status: "failed", Attempt: 1})
		}
		kb.Wiki.Jobs = append(kb.Wiki.Jobs,
			model.WikiJob{ID: "limit", Status: "failed", Attempt: 3},
			model.WikiJob{ID: "changed", Status: "failed", Attempt: 1, SourceVersions: map[string]string{"missing": "old"}},
			model.WikiJob{ID: "success", Status: "succeeded", Attempt: 1},
			model.WikiJob{ID: "partial", Status: "partial", Attempt: 1},
		)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.RetryFailedWikiJobs(id)
	if err != nil || result.Total != 67 || result.Submitted != 65 || result.Skipped != 1 || result.Remaining != 0 || len(result.Failures) != 1 || result.Failures[0].JobID != "changed" {
		t.Fatalf("unexpected batch: %+v, %v", result, err)
	}
	job, err := s.GetWikiJob(id, "retry-64")
	if err != nil || job.Status != "queued" || job.Attempt != 1 {
		t.Fatalf("retry must enqueue without consuming execution attempts: %+v, %v", job, err)
	}
	second, err := s.RetryFailedWikiJobs(id)
	if err != nil || second.Submitted != 0 || second.Total != 2 {
		t.Fatalf("queued jobs retried twice: %+v, %v", second, err)
	}
	if _, err := s.RetryFailedWikiJobs("missing"); err == nil {
		t.Fatal("missing library accepted")
	}
	if err := s.commitWikiChange(id, func(kb *model.KnowledgeBase) error { kb.Wiki.Enabled = false; return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RetryFailedWikiJobs(id); err == nil {
		t.Fatal("disabled library accepted")
	}
}

// TestRetryFailedWikiJobsStopsOnSaveFailure 验证持久化失败立即停止，未提交任务仍保留失败状态。
func TestRetryFailedWikiJobsStopsOnSaveFailure(t *testing.T) {
	dir := t.TempDir()
	s := NewAppService(nil, nil, nil, model.ServerConfig{UploadDir: dir})
	t.Cleanup(func() { _ = s.Close() })
	s.wikiRunMu.Lock()
	s.wikiClosed = true
	s.wikiRunMu.Unlock()
	s.wikiWorkers.Wait()
	id := s.ListKnowledgeBases()[0].ID
	if err := s.commitWikiChange(id, func(kb *model.KnowledgeBase) error {
		kb.Wiki.Jobs = []model.WikiJob{{ID: "one", Status: "failed", Attempt: 1}, {ID: "two", Status: "failed", Attempt: 1}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	blocked := filepath.Join(dir, "not-a-directory")
	if err := os.WriteFile(blocked, []byte("block"), 0600); err != nil {
		t.Fatal(err)
	}
	s.store = NewAppStateStore(filepath.Join(blocked, "state.json"))
	result, err := s.RetryFailedWikiJobs(id)
	if err != nil || result.Total != 2 || result.Submitted != 0 || result.Remaining != 1 || len(result.Failures) != 1 {
		t.Fatalf("unexpected partial result: %+v, %v", result, err)
	}
	for _, jobID := range []string{"one", "two"} {
		job, err := s.GetWikiJob(id, jobID)
		if err != nil || job.Status != "failed" {
			t.Fatalf("unsaved job changed: %+v, %v", job, err)
		}
	}
}
