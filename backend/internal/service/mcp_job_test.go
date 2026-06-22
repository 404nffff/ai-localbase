package service

import (
	"strings"
	"testing"

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
