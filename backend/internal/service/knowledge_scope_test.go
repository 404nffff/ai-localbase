package service

import (
	"path/filepath"
	"testing"

	"ai-localbase/internal/model"
)

// TestKnowledgeScopeBoundary 覆盖默认不检索、显式全库、空白归一和跨库文档拒绝。
func TestKnowledgeScopeBoundary(t *testing.T) {
	s := NewAppService(nil, nil, nil, model.ServerConfig{})
	kb := s.ListKnowledgeBases()[0]
	s.AddDocument(kb.ID, model.Document{ID: "scope-doc", KnowledgeBaseID: kb.ID})
	other, err := s.CreateKnowledgeBase(model.KnowledgeBaseInput{Name: "other-scope"})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		scope, kb, doc, want string
		invalid              bool
	}{
		{"", "", "", "none", false}, {"all", "", "", "all", false},
		{"", " " + kb.ID + " ", " scope-doc ", "selected", false},
		{"", "", "scope-doc", "selected", false},
		{"selected", other.ID, "scope-doc", "", true},
		{"selected", "", "", "", true}, {"none", kb.ID, "", "", true}, {"all", kb.ID, "", "", true},
	} {
		got, err := s.NormalizeKnowledgeRequest(model.ChatCompletionRequest{KnowledgeScope: test.scope, KnowledgeBaseID: test.kb, DocumentID: test.doc})
		if (err != nil) != test.invalid || (!test.invalid && got.KnowledgeScope != test.want) {
			t.Fatalf("%+v: got %+v, %v", test, got, err)
		}
	}
	chunks, err := s.EvaluateRetrieve(model.ChatCompletionRequest{Messages: []model.ChatMessage{{Role: "user", Content: "不应请求嵌入模型"}}})
	if err != nil || len(chunks) != 0 {
		t.Fatalf("ordinary chat retrieved: %v %v", chunks, err)
	}
}

// TestConversationScopeSurvivesRestart 验证显式全库选择经过 SQLite 保存、重启和列表读取仍可区分。
func TestConversationScopeSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chat.db")
	store, err := NewSQLiteChatHistoryStore(path)
	if err != nil {
		t.Fatal(err)
	}
	conversation := model.Conversation{ID: "scope-conversation", Title: "范围", KnowledgeScope: "all", Messages: []model.StoredChatMessage{{ID: "scope-message", Role: "user", Content: "测试"}}}
	if err := store.SaveConversation(conversation); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = NewSQLiteChatHistoryStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	got, err := store.GetConversation(conversation.ID)
	if err != nil || got == nil || got.KnowledgeScope != "all" {
		t.Fatalf("scope lost: %+v %v", got, err)
	}
	items, err := store.ListConversations()
	if err != nil || len(items) != 1 || items[0].KnowledgeScope != "all" {
		t.Fatalf("list scope lost: %+v %v", items, err)
	}
}
