package service

import (
	"errors"
	"testing"

	"ai-localbase/internal/model"
)

func TestValidateChatRequestScopeRejectsKnowledgeBaseChange(t *testing.T) {
	store := newMemoryChatHistoryStore()
	service := &AppService{
		chatHistory: store,
		state: &model.AppState{KnowledgeBases: map[string]model.KnowledgeBase{
			"kb-novel":  {ID: "kb-novel"},
			"kb-school": {ID: "kb-school"},
		}},
	}
	if err := store.SaveConversation(model.Conversation{
		ID:              "conversation-1",
		KnowledgeBaseID: "kb-novel",
		ScopeVersion:    conversationScopeVersion,
		Messages: []model.StoredChatMessage{{
			ID: "message-1", Role: "user", Content: "介绍小说",
		}},
	}); err != nil {
		t.Fatalf("seed conversation: %v", err)
	}

	err := service.ValidateChatRequestScope(model.ChatCompletionRequest{
		ConversationID:  "conversation-1",
		KnowledgeBaseID: "kb-school",
		Messages:        []model.ChatMessage{{Role: "user", Content: "详细介绍"}},
	})
	if !errors.Is(err, ErrConversationScopeMismatch) {
		t.Fatalf("expected conversation scope mismatch, got %v", err)
	}
}

func TestSaveConversationRejectsScopeOverwrite(t *testing.T) {
	store := newMemoryChatHistoryStore()
	service := &AppService{
		chatHistory: store,
		state: &model.AppState{KnowledgeBases: map[string]model.KnowledgeBase{
			"kb-novel":  {ID: "kb-novel"},
			"kb-school": {ID: "kb-school"},
		}},
	}
	if err := store.SaveConversation(model.Conversation{
		ID:              "conversation-1",
		KnowledgeBaseID: "kb-novel",
		ScopeVersion:    conversationScopeVersion,
		Messages:        []model.StoredChatMessage{{ID: "message-1", Role: "user", Content: "介绍小说"}},
	}); err != nil {
		t.Fatalf("seed conversation: %v", err)
	}

	_, err := service.SaveConversation(model.SaveConversationRequest{
		ID:              "conversation-1",
		KnowledgeBaseID: "kb-school",
		Messages:        []model.StoredChatMessage{{ID: "message-2", Role: "user", Content: "详细介绍"}},
	})
	if !errors.Is(err, ErrConversationScopeMismatch) {
		t.Fatalf("expected scope overwrite to be rejected, got %v", err)
	}
}

func TestValidateChatRequestScopeRejectsLegacyConversation(t *testing.T) {
	store := newMemoryChatHistoryStore()
	service := &AppService{
		chatHistory: store,
		state: &model.AppState{KnowledgeBases: map[string]model.KnowledgeBase{
			"kb-school": {ID: "kb-school"},
		}},
	}
	if err := store.SaveConversation(model.Conversation{
		ID:              "conversation-legacy",
		KnowledgeBaseID: "kb-school",
		ScopeVersion:    0,
		Messages: []model.StoredChatMessage{{
			ID: "message-1", Role: "user", Content: "旧会话内容",
		}},
	}); err != nil {
		t.Fatalf("seed legacy conversation: %v", err)
	}

	err := service.ValidateChatRequestScope(model.ChatCompletionRequest{
		ConversationID:  "conversation-legacy",
		KnowledgeBaseID: "kb-school",
		Messages:        []model.ChatMessage{{Role: "user", Content: "继续"}},
	})
	if !errors.Is(err, ErrConversationScopeUpgradeNeeded) {
		t.Fatalf("expected legacy conversation to require a new scope, got %v", err)
	}
}
