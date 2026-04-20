package service

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"ai-localbase/internal/model"
)

func TestNewAppServiceSyncsIDCounterFromPersistedState(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "persisted.json")
	store := NewAppStateStore(statePath)
	persisted := persistentAppState{
		Config: model.AppConfig{},
		KnowledgeBases: map[string]model.KnowledgeBase{
			"kb-3": {
				ID:          "kb-3",
				Name:        "测试知识库",
				Description: "用于验证 ID 计数器同步",
				CreatedAt:   "2026-04-20T00:00:00Z",
				Documents: []model.Document{
					{
						ID:              "doc-4",
						KnowledgeBaseID: "kb-3",
						Name:            "demo.md",
					},
				},
			},
		},
	}
	if err := store.Save(persisted); err != nil {
		t.Fatalf("save persisted state: %v", err)
	}

	service := NewAppService(nil, store, nil, model.ServerConfig{})
	created, err := service.CreateKnowledgeBase(model.KnowledgeBaseInput{
		Name:        "后续知识库",
		Description: "验证知识库创建不会重用旧编号",
	})
	if err != nil {
		t.Fatalf("create knowledge base: %v", err)
	}

	if suffix := numericIDSuffix(t, created.ID); suffix <= 4 {
		t.Fatalf("expected created knowledge base id suffix > 4, got %s", created.ID)
	}
}

func numericIDSuffix(t *testing.T, id string) int {
	t.Helper()

	index := strings.LastIndex(strings.TrimSpace(id), "-")
	if index < 0 || index == len(id)-1 {
		t.Fatalf("invalid id format: %s", id)
	}

	value, err := strconv.Atoi(id[index+1:])
	if err != nil {
		t.Fatalf("parse id suffix %s: %v", id, err)
	}
	return value
}
