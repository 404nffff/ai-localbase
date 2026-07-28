package service

import (
	"database/sql"
	"path/filepath"
	"testing"

	"ai-localbase/internal/model"
)

func TestSQLiteChatHistoryStoreMigratesConversationScopeVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chat-history.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open legacy database: %v", err)
	}
	legacySchema := []string{
		`CREATE TABLE conversations (
			id TEXT PRIMARY KEY,
			title TEXT NOT NULL,
			knowledge_base_id TEXT NOT NULL,
			document_id TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE messages (
			id TEXT PRIMARY KEY,
			conversation_id TEXT NOT NULL,
			role TEXT NOT NULL,
			content TEXT NOT NULL,
			created_at TEXT NOT NULL,
			metadata TEXT NOT NULL DEFAULT '{}',
			seq INTEGER NOT NULL
		)`,
		`INSERT INTO conversations VALUES ('legacy', '旧会话', 'kb-school', '', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
	}
	for _, statement := range legacySchema {
		if _, err := db.Exec(statement); err != nil {
			_ = db.Close()
			t.Fatalf("initialize legacy database: %v", err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close legacy database: %v", err)
	}

	store, err := NewSQLiteChatHistoryStore(path)
	if err != nil {
		t.Fatalf("migrate chat history store: %v", err)
	}
	defer store.Close()

	legacy, err := store.GetConversation("legacy")
	if err != nil {
		t.Fatalf("get legacy conversation: %v", err)
	}
	if legacy == nil || legacy.ScopeVersion != 0 {
		t.Fatalf("expected legacy conversation scope version 0, got %#v", legacy)
	}

	if err := store.SaveConversation(model.Conversation{
		ID:              "scoped",
		Title:           "新会话",
		KnowledgeBaseID: "kb-school",
		ScopeVersion:    conversationScopeVersion,
		Messages: []model.StoredChatMessage{{
			ID: "message-1", Role: "user", Content: "介绍资料",
		}},
	}); err != nil {
		t.Fatalf("save scoped conversation: %v", err)
	}
	scoped, err := store.GetConversation("scoped")
	if err != nil {
		t.Fatalf("get scoped conversation: %v", err)
	}
	if scoped == nil || scoped.ScopeVersion != conversationScopeVersion {
		t.Fatalf("expected scope version %d, got %#v", conversationScopeVersion, scoped)
	}
}
