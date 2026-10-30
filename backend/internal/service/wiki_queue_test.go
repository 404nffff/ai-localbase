package service

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"ai-localbase/internal/model"
)

// TestWikiQueueSettings 验证局部更新、边界、旧状态默认值、持久化失败与空库重启。
func TestWikiQueueSettings(t *testing.T) {
	dir := t.TempDir()
	store := NewAppStateStore(filepath.Join(dir, "state.json"))
	legacy := defaultAppState(model.ServerConfig{})
	legacy.Config.Wiki.Queue = model.WikiQueueSettings{}
	if err := store.Save(persistentAppState{Config: legacy.Config, KnowledgeBases: legacy.KnowledgeBases}); err != nil {
		t.Fatal(err)
	}
	s := NewAppService(nil, store, nil, model.ServerConfig{StagingDir: dir})
	t.Cleanup(func() { _ = s.Close() })
	if got := s.GetConfig().Wiki.Queue; got.Concurrency != 5 || got.Paused {
		t.Fatalf("legacy defaults: %+v", got)
	}
	paused := true
	if _, err := s.UpdateWikiQueue(model.WikiQueueUpdate{Paused: &paused}); err != nil {
		t.Fatal(err)
	}
	for _, limit := range []int{1, 20, 2} {
		got, err := s.UpdateWikiQueue(model.WikiQueueUpdate{Concurrency: &limit})
		if err != nil || got.Concurrency != limit || !got.Paused {
			t.Fatalf("partial update %d: %+v %v", limit, got, err)
		}
	}
	for _, limit := range []int{-1, 0, 21} {
		if _, err := s.UpdateWikiQueue(model.WikiQueueUpdate{Concurrency: &limit}); err == nil {
			t.Fatalf("accepted %d", limit)
		}
	}
	if _, err := s.UpdateWikiQueue(model.WikiQueueUpdate{}); err == nil {
		t.Fatal("accepted empty request")
	}
	want := model.WikiQueueSettings{Paused: true, Concurrency: 2}
	cfg := s.GetConfig()
	// 普通配置携带旧队列快照，也不能改写专用接口保存的设置。
	_, err := s.UpdateConfig(model.ConfigUpdateRequest{Chat: cfg.Chat, Embedding: cfg.Embedding, Wiki: &model.WikiConfig{ModelTimeoutSeconds: 300, Queue: model.WikiQueueSettings{Concurrency: 5}}})
	if err != nil || s.GetConfig().Wiki.Queue != want {
		t.Fatalf("ordinary config overwrote queue: %v", err)
	}
	// 用文件阻断存储目录，真实触发保存失败，不替换持久化实现。
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("file"), 0600); err != nil {
		t.Fatal(err)
	}
	s.store = NewAppStateStore(filepath.Join(blocker, "state.json"))
	paused = false
	if _, err := s.UpdateWikiQueue(model.WikiQueueUpdate{Paused: &paused}); err == nil || s.GetConfig().Wiki.Queue != want {
		t.Fatalf("failed persistence changed queue: %v", err)
	}
	s.store = store
	// 删除全部库后的重启也须保留队列控制，避免自动恢复整理。
	if err := store.Save(persistentAppState{Config: s.GetConfig(), KnowledgeBases: map[string]model.KnowledgeBase{}}); err != nil {
		t.Fatal(err)
	}
	restarted := NewAppService(nil, store, nil, model.ServerConfig{StagingDir: dir})
	t.Cleanup(func() { _ = restarted.Close() })
	if got := restarted.GetConfig().Wiki.Queue; got != want {
		t.Fatalf("restart lost queue: %+v", got)
	}
}

// TestWikiQueueDynamicControl 用真实调度与HTTP模型验证暂停、增减并发及当前任务自然完成。
func TestWikiQueueDynamicControl(t *testing.T) {
	entered := make(chan struct{}, 4)
	release := make(chan struct{}, 4)
	var once sync.Once
	s, firstKB, doc := wikiFixture(t, func(model.ChatCompletionRequest) (string, int) {
		entered <- struct{}{}
		<-release
		return `{"pages":[],"issues":[]}`, http.StatusOK
	})
	// 清理先解除模型阻塞，再由fixture关闭服务，失败断言也不会挂起。
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	paused, limit := true, 1
	if _, err := s.UpdateWikiQueue(model.WikiQueueUpdate{Paused: &paused, Concurrency: &limit}); err != nil {
		t.Fatal(err)
	}
	jobs := []model.WikiJob{}
	for i := 0; i < 4; i++ {
		kbID := firstKB
		if i > 0 {
			kb, err := s.CreateKnowledgeBase(model.KnowledgeBaseInput{Name: fmt.Sprintf("队列验证%d", i)})
			if err != nil {
				t.Fatal(err)
			}
			kbID = kb.ID
		}
		source := doc
		source.ID, source.KnowledgeBaseID = fmt.Sprintf("queue-source-%d", i), kbID
		s.AddDocument(kbID, source)
		job, _, err := s.StartWikiJob(kbID, model.WikiJobRequest{DocumentIDs: []string{source.ID}, RequestKey: source.ID})
		if err != nil {
			t.Fatal(err)
		}
		jobs = append(jobs, job)
	}
	assertNoRequest := func() {
		t.Helper()
		select {
		case <-entered:
			t.Fatal("unexpected model request")
		case <-time.After(100 * time.Millisecond):
		}
	}
	awaitRequest := func() {
		t.Helper()
		select {
		case <-entered:
		case <-time.After(3 * time.Second):
			t.Fatal("queued task did not start")
		}
	}
	awaitActive := func(want int) {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			s.wikiRunMu.Lock()
			active := s.wikiActiveJobs
			s.wikiRunMu.Unlock()
			if active == want {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatalf("active count did not become %d", want)
	}
	assertNoRequest()
	for _, job := range jobs {
		got, err := s.GetWikiJob(job.KnowledgeBaseID, job.ID)
		if err != nil || got.Status != "queued" || got.ModelCalls != 0 {
			t.Fatalf("paused job changed: %+v %v", got, err)
		}
	}
	paused = false
	if _, err := s.UpdateWikiQueue(model.WikiQueueUpdate{Paused: &paused}); err != nil {
		t.Fatal(err)
	}
	awaitRequest()
	assertNoRequest()
	limit = 2
	if _, err := s.UpdateWikiQueue(model.WikiQueueUpdate{Concurrency: &limit}); err != nil {
		t.Fatal(err)
	}
	awaitRequest()
	limit = 1
	if _, err := s.UpdateWikiQueue(model.WikiQueueUpdate{Concurrency: &limit}); err != nil {
		t.Fatal(err)
	}
	release <- struct{}{}
	awaitActive(1)
	assertNoRequest()
	paused = true
	if _, err := s.UpdateWikiQueue(model.WikiQueueUpdate{Paused: &paused}); err != nil {
		t.Fatal(err)
	}
	release <- struct{}{}
	awaitActive(0)
	assertNoRequest()
	// 暂停后两个已领取任务仍成功，其余两个保持排队。
	counts := map[string]int{}
	for _, job := range jobs {
		got, err := s.GetWikiJob(job.KnowledgeBaseID, job.ID)
		if err != nil {
			t.Fatal(err)
		}
		counts[got.Status]++
	}
	if counts["succeeded"] != 2 || counts["queued"] != 2 {
		t.Fatalf("pause interrupted running jobs: %+v", counts)
	}
	paused = false
	if _, err := s.UpdateWikiQueue(model.WikiQueueUpdate{Paused: &paused}); err != nil {
		t.Fatal(err)
	}
	awaitRequest()
	assertNoRequest()
	release <- struct{}{}
	awaitRequest()
	release <- struct{}{}
	for _, job := range jobs {
		if got := waitWikiJob(t, s, job.KnowledgeBaseID, job.ID); got.Status != "succeeded" {
			t.Fatalf("resume failed: %+v", got)
		}
	}
}

// TestWikiQueueRestartPaused 验证关闭能唤醒暂停等待者，重启保留任务且需显式恢复。
func TestWikiQueueRestartPaused(t *testing.T) {
	s, kbID, doc := wikiFixture(t, func(model.ChatCompletionRequest) (string, int) {
		return `{"pages":[],"issues":[]}`, http.StatusOK
	})
	paused := true
	if _, err := s.UpdateWikiQueue(model.WikiQueueUpdate{Paused: &paused}); err != nil {
		t.Fatal(err)
	}
	job, _, err := s.StartWikiJob(kbID, model.WikiJobRequest{DocumentIDs: []string{doc.ID}, RequestKey: "paused-restart"})
	if err != nil {
		t.Fatal(err)
	}
	closed := make(chan struct{})
	go func() { _ = s.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		t.Fatal("close blocked on paused queue")
	}
	if _, err := s.UpdateWikiQueue(model.WikiQueueUpdate{Paused: &paused}); err == nil {
		t.Fatal("closed service accepted update")
	}
	restarted := NewAppService(nil, s.store, nil, s.serverConfig)
	t.Cleanup(func() { _ = restarted.Close() })
	time.Sleep(100 * time.Millisecond)
	got, err := restarted.GetWikiJob(kbID, job.ID)
	if err != nil || got.Status != "queued" || !restarted.GetConfig().Wiki.Queue.Paused {
		t.Fatalf("restart resumed paused job: %+v %v", got, err)
	}
	paused = false
	if _, err := restarted.UpdateWikiQueue(model.WikiQueueUpdate{Paused: &paused}); err != nil {
		t.Fatal(err)
	}
	if got := waitWikiJob(t, restarted, kbID, job.ID); got.Status != "succeeded" {
		t.Fatalf("restart resume: %+v", got)
	}
}
