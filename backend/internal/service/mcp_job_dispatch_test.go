package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"ai-localbase/internal/model"
)

func TestGenerateEvalDatasetWithContextHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := (&AppService{}).GenerateEvalDatasetWithContext(ctx, model.GenerateEvalDatasetRequest{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
}

func TestStartMCPImportJobRejectsUnknownJobType(t *testing.T) {
	_, err := (&AppService{}).StartMCPImportJobAs(model.MCPStartImportJobRequest{
		JobType: "unknown",
	}, AuthPrincipal{})
	if err == nil || !strings.Contains(err.Error(), "unsupported MCP job type") {
		t.Fatalf("expected unsupported job type error, got %v", err)
	}
}
