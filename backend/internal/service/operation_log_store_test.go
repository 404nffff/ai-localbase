package service

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"ai-localbase/internal/model"
)

func TestSQLiteOperationLogStoreAppendAndList(t *testing.T) {
	store := newTestOperationLogStore(t)

	entry := model.OperationLogEntry{
		ID:              "log-1",
		CorrelationID:   "op-1",
		Operation:       model.OperationIndexDocument,
		Source:          model.OperationSourceWeb,
		Status:          model.OperationStatusSuccess,
		KnowledgeBaseID: "kb-1",
		DocumentID:      "doc-1",
		DocumentName:    "demo.md",
		Metadata:        map[string]any{"stage": "test"},
		StartedAt:       "2026-05-22T00:00:00Z",
		FinishedAt:      "2026-05-22T00:00:01Z",
		DurationMs:      1000,
		CreatedAt:       "2026-05-22T00:00:01Z",
	}

	if err := store.Append(entry); err != nil {
		t.Fatalf("append operation log: %v", err)
	}

	result, err := store.List(model.OperationLogListQuery{KnowledgeBaseID: "kb-1"})
	if err != nil {
		t.Fatalf("list operation logs: %v", err)
	}
	if result.Total != 1 || len(result.Items) != 1 {
		t.Fatalf("expected one operation log, got total=%d items=%d", result.Total, len(result.Items))
	}
	if result.Items[0].DocumentName != "demo.md" {
		t.Fatalf("expected document name demo.md, got %q", result.Items[0].DocumentName)
	}
	if result.Items[0].Metadata["stage"] != "test" {
		t.Fatalf("expected metadata to be decoded, got %#v", result.Items[0].Metadata)
	}
}

func TestSQLiteOperationLogStoreFiltersAndPaginates(t *testing.T) {
	store := newTestOperationLogStore(t)
	now := time.Date(2026, 5, 22, 0, 0, 0, 0, time.UTC)
	for index := 0; index < 3; index++ {
		entry := model.OperationLogEntry{
			ID:              fmt.Sprintf("log-%d", index),
			CorrelationID:   "op-filter",
			Operation:       model.OperationUploadFile,
			Source:          model.OperationSourceWeb,
			Status:          model.OperationStatusSuccess,
			KnowledgeBaseID: "kb-filter",
			CreatedAt:       now.Add(time.Duration(index) * time.Second).Format(time.RFC3339),
			StartedAt:       now.Format(time.RFC3339),
			FinishedAt:      now.Format(time.RFC3339),
		}
		if index == 0 {
			entry.Status = model.OperationStatusFailed
		}
		if err := store.Append(entry); err != nil {
			t.Fatalf("append operation log %d: %v", index, err)
		}
	}

	result, err := store.List(model.OperationLogListQuery{
		KnowledgeBaseID: "kb-filter",
		Status:          model.OperationStatusSuccess,
		Limit:           1,
	})
	if err != nil {
		t.Fatalf("list operation logs: %v", err)
	}
	if result.Total != 2 || len(result.Items) != 1 {
		t.Fatalf("expected total=2 and one page item, got total=%d items=%d", result.Total, len(result.Items))
	}
	if result.Items[0].ID != "log-2" {
		t.Fatalf("expected latest matching item log-2, got %q", result.Items[0].ID)
	}
}

func TestSQLiteOperationLogStorePrunesOldEntries(t *testing.T) {
	store := newTestOperationLogStore(t)
	store.retention = 2
	now := time.Date(2026, 5, 22, 0, 0, 0, 0, time.UTC)

	for index := 0; index < 3; index++ {
		if err := store.Append(model.OperationLogEntry{
			ID:            fmt.Sprintf("log-prune-%d", index),
			CorrelationID: "op-prune",
			Operation:     model.OperationUploadFile,
			Source:        model.OperationSourceWeb,
			Status:        model.OperationStatusSuccess,
			StartedAt:     now.Format(time.RFC3339),
			FinishedAt:    now.Format(time.RFC3339),
			CreatedAt:     now.Add(time.Duration(index) * time.Second).Format(time.RFC3339),
		}); err != nil {
			t.Fatalf("append operation log %d: %v", index, err)
		}
	}

	result, err := store.List(model.OperationLogListQuery{Limit: 10})
	if err != nil {
		t.Fatalf("list operation logs: %v", err)
	}
	if result.Total != 2 {
		t.Fatalf("expected two retained logs, got %d", result.Total)
	}
	if result.Items[0].ID != "log-prune-2" || result.Items[1].ID != "log-prune-1" {
		t.Fatalf("expected newest two logs, got %#v", result.Items)
	}
}

func newTestOperationLogStore(t *testing.T) *SQLiteOperationLogStore {
	t.Helper()
	store, err := NewSQLiteOperationLogStore(filepath.Join(t.TempDir(), "operation-logs.db"))
	if err != nil {
		t.Fatalf("create operation log store: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close operation log store: %v", err)
		}
	})
	return store
}
