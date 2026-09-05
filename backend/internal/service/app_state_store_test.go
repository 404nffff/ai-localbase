package service

import (
	"os"
	"path/filepath"
	"testing"

	"ai-localbase/internal/model"
)

func TestAppStateStoreSaveAndLoad(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "app-state.json")
	store := NewAppStateStore(statePath)

	state := persistentAppState{
		Config: model.AppConfig{
			Chat: model.ChatConfig{
				Provider:    "ollama",
				BaseURL:     "http://localhost:11434/v1",
				Model:       "llama3.2",
				Temperature: 0.5,
			},
			Embedding: model.EmbeddingConfig{
				Provider: "ollama",
				BaseURL:  "http://localhost:11434/v1",
				Model:    "nomic-embed-text",
			},
		},
		KnowledgeBases: map[string]model.KnowledgeBase{
			"kb-1": {
				ID:        "kb-1",
				Name:      "默认知识库",
				CreatedAt: "2026-03-12T00:00:00Z",
				Documents: []model.Document{{
					ID:   "doc-1",
					Name: "demo.md",
				}},
			},
		},
	}

	if err := store.Save(state); err != nil {
		t.Fatalf("save app state: %v", err)
	}

	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("load app state: %v", err)
	}
	if loaded == nil {
		t.Fatal("expected loaded state")
	}
	if loaded.Config.Chat.Model != "llama3.2" {
		t.Fatalf("expected chat model llama3.2, got %s", loaded.Config.Chat.Model)
	}
	if len(loaded.KnowledgeBases["kb-1"].Documents) != 1 {
		t.Fatalf("expected persisted documents, got %d", len(loaded.KnowledgeBases["kb-1"].Documents))
	}

	// 同时保存到同一路径，验证临时文件不会被其他请求重命名或覆盖。
	const writers = 32
	start := make(chan struct{})
	results := make(chan error, writers)
	for index := 0; index < writers; index++ {
		go func() {
			<-start
			results <- store.Save(state)
		}()
	}
	close(start)
	for index := 0; index < writers; index++ {
		if saveErr := <-results; saveErr != nil {
			t.Errorf("concurrent state save failed: %v", saveErr)
		}
	}
	// 最终文件必须是完整快照，保存过程也不能留下待替换文件。
	loaded, err = store.Load()
	if err != nil || loaded == nil {
		t.Fatalf("load concurrent state snapshot: %v", err)
	}
	remaining, err := filepath.Glob(filepath.Join(filepath.Dir(statePath), "*.tmp"))
	if err != nil || len(remaining) != 0 {
		t.Fatalf("unexpected state temporary files: %v, error: %v", remaining, err)
	}
}

func TestAppStateStoreLoadMissingFile(t *testing.T) {
	store := NewAppStateStore(filepath.Join(t.TempDir(), "missing.json"))
	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("load missing app state: %v", err)
	}
	if loaded != nil {
		t.Fatalf("expected nil state for missing file, got %#v", loaded)
	}
}

func TestNewAppServiceLoadsPersistedState(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "persisted.json")
	store := NewAppStateStore(statePath)
	persisted := persistentAppState{
		Config: model.AppConfig{
			Chat: model.ChatConfig{
				Provider:    "ollama",
				BaseURL:     "http://persisted-chat.local/v1",
				Model:       "persisted-chat-model",
				Temperature: 0.3,
			},
			Embedding: model.EmbeddingConfig{
				Provider: "openai-compatible",
				BaseURL:  "http://persisted-embed.local/v1",
				Model:    "persisted-embed-model",
			},
		},
		KnowledgeBases: map[string]model.KnowledgeBase{
			"kb-persisted": {
				ID:          "kb-persisted",
				Name:        "持久化知识库",
				Description: "来自磁盘状态",
				CreatedAt:   "2026-03-12T00:00:00Z",
			},
		},
	}
	if err := store.Save(persisted); err != nil {
		t.Fatalf("save persisted state: %v", err)
	}

	service := NewAppService(nil, store, nil, model.ServerConfig{})
	config := service.GetConfig()
	if config.Chat.Model != "persisted-chat-model" {
		t.Fatalf("expected persisted chat model, got %s", config.Chat.Model)
	}

	knowledgeBases := service.ListKnowledgeBases()
	if len(knowledgeBases) != 1 || knowledgeBases[0].ID != "kb-persisted" {
		t.Fatalf("expected persisted knowledge base, got %#v", knowledgeBases)
	}
}

func TestNewAppServicePersistsDefaultState(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "default-state.json")
	store := NewAppStateStore(statePath)

	service := NewAppService(nil, store, nil, model.ServerConfig{})
	if service == nil {
		t.Fatal("expected app service")
	}

	content, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read persisted default state: %v", err)
	}
	if len(content) == 0 {
		t.Fatal("expected non-empty persisted state file")
	}
}
