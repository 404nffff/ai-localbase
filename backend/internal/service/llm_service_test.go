package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"ai-localbase/internal/model"
)

func TestChatDegradedResponseUsesUpstreamErrorMessageOnEmptyChoices(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{
			"id":      "chatcmpl-test",
			"object":  "chat.completion",
			"created": 123,
			"model":   "test-model",
			"choices": []any{},
			"error": map[string]any{
				"message": "upstream returned detailed error",
			},
		}); err != nil {
			t.Fatalf("encode response: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	llm := &LLMService{client: server.Client(), streamClient: server.Client()}
	resp, err := llm.Chat(model.ChatCompletionRequest{
		Config: model.ChatModelConfig{
			Provider: "openai-compatible",
			BaseURL:  server.URL,
			Model:    "test-model",
		},
		Messages: []model.ChatMessage{{Role: "user", Content: "你好"}},
	})
	if err != nil {
		t.Fatalf("chat request: %v", err)
	}

	if got := metadataString(resp.Metadata, "upstreamError"); got != "upstream returned detailed error" {
		t.Fatalf("expected upstream error message to be preserved, got %q", got)
	}
}

func TestChatDegradedResponseUsesUpstreamErrorMessageOnHTTPFailure(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		if err := json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"message": "invalid upstream api key",
			},
		}); err != nil {
			t.Fatalf("encode response: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	llm := &LLMService{client: server.Client(), streamClient: server.Client()}
	resp, err := llm.Chat(model.ChatCompletionRequest{
		Config: model.ChatModelConfig{
			Provider: "openai-compatible",
			BaseURL:  server.URL,
			Model:    "test-model",
		},
		Messages: []model.ChatMessage{{Role: "user", Content: "你好"}},
	})
	if err != nil {
		t.Fatalf("chat request: %v", err)
	}

	if got := metadataString(resp.Metadata, "upstreamError"); got != "invalid upstream api key" {
		t.Fatalf("expected upstream error message to be preserved, got %q", got)
	}
}

func metadataString(metadata map[string]any, key string) string {
	value, _ := metadata[key].(string)
	return value
}
