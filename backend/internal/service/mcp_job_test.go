package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"ai-localbase/internal/model"
)

func TestMCPJobTerminalStatusIsNotOverwritten(t *testing.T) {
	service := &AppService{
		mcpJobs: map[string]model.MCPJob{
			"job_cancelled": {
				ID:       "job_cancelled",
				Type:     "import",
				Status:   "cancelled",
				Progress: 10,
				Summary:  "任务已取消。",
			},
		},
	}

	service.updateMCPJob("job_cancelled", func(job *model.MCPJob) {
		job.Status = "running"
		job.Progress = 70
		job.Summary = "正在注册并索引文档。"
	})
	service.completeMCPJob("job_cancelled", "succeeded", 100, "导入完成。", map[string]any{"ok": true}, "")

	job := service.mcpJobs["job_cancelled"]
	if job.Status != "cancelled" {
		t.Fatalf("expected cancelled job to keep terminal status, got %+v", job)
	}
	if job.Progress != 10 || job.Result != nil {
		t.Fatalf("expected cancelled job details to stay unchanged, got %+v", job)
	}
}

func TestCancelMCPJobAddsBestEffortWarning(t *testing.T) {
	service := &AppService{
		mcpJobs: map[string]model.MCPJob{
			"job_running": {
				ID:       "job_running",
				Type:     "import",
				Status:   "running",
				Progress: 70,
				Summary:  "正在注册并索引文档。",
			},
		},
	}

	job, err := service.CancelMCPJob("job_running")
	if err != nil {
		t.Fatalf("cancel job: %v", err)
	}
	if job.Status != "cancelled" {
		t.Fatalf("expected cancelled job, got %+v", job)
	}
	if len(job.Warnings) != 1 || !strings.Contains(job.Warnings[0], "best-effort") {
		t.Fatalf("expected best-effort warning, got %+v", job.Warnings)
	}
}

func TestShutdownJobsCancelsAndWaitsForWorkers(t *testing.T) {
	service := &AppService{
		mcpJobs:       map[string]model.MCPJob{},
		mcpJobCancels: map[string]context.CancelFunc{},
	}
	job := model.MCPJob{ID: "job_shutdown", Status: "running"}
	jobContext, cancelJob := context.WithCancel(context.Background())
	if !service.registerMCPJob(job, cancelJob) {
		t.Fatal("expected job registration to succeed")
	}
	workerDone := make(chan struct{})
	go func() {
		defer service.mcpJobWG.Done()
		defer close(workerDone)
		<-jobContext.Done()
	}()

	shutdownContext, cancelShutdown := context.WithTimeout(context.Background(), time.Second)
	defer cancelShutdown()
	if err := service.ShutdownJobs(shutdownContext); err != nil {
		t.Fatalf("shutdown jobs: %v", err)
	}
	select {
	case <-workerDone:
	case <-time.After(time.Second):
		t.Fatal("expected worker to exit during shutdown")
	}

	if service.registerMCPJob(model.MCPJob{ID: "job_after_shutdown"}, func() {}) {
		t.Fatal("expected new jobs to be rejected after shutdown")
	}
}

func TestPruneMCPJobsPreservesActiveJobs(t *testing.T) {
	service := &AppService{
		mcpJobs:       map[string]model.MCPJob{},
		mcpJobCancels: map[string]context.CancelFunc{},
	}
	for index := 0; index < 50; index++ {
		id := "job_terminal_" + string(rune('a'+index))
		service.mcpJobs[id] = model.MCPJob{ID: id, Status: "succeeded", UpdatedAt: "2026-01-01T00:00:00Z"}
	}
	service.mcpJobs["job_active"] = model.MCPJob{ID: "job_active", Status: "running"}
	service.mcpJobCancels["job_active"] = func() {}

	service.mcpJobMu.Lock()
	service.pruneMCPJobsLocked()
	service.mcpJobMu.Unlock()

	if _, ok := service.mcpJobs["job_active"]; !ok {
		t.Fatal("expected active job to remain tracked")
	}
	if _, ok := service.mcpJobCancels["job_active"]; !ok {
		t.Fatal("expected active job cancellation to remain tracked")
	}
}
