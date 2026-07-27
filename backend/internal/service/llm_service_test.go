package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ai-localbase/internal/model"
)

func TestOllamaStreamChatPreservesChunkWhitespace(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			http.NotFound(w, r)
			return
		}
		encoder := json.NewEncoder(w)
		for _, content := range []string{"您好", "， ", "欢迎使用", "\n\n**AI LocalBase**"} {
			if err := encoder.Encode(map[string]any{
				"message": map[string]any{"role": "assistant", "content": content},
				"done":    false,
			}); err != nil {
				t.Fatalf("encode stream chunk: %v", err)
			}
		}
		if err := encoder.Encode(map[string]any{"done": true}); err != nil {
			t.Fatalf("encode stream completion: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	service := &LLMService{streamClient: server.Client()}
	var output strings.Builder
	err := service.ollamaStreamChat(
		t.Context(),
		model.ChatModelConfig{BaseURL: server.URL, Model: "test-model"},
		model.ChatCompletionRequest{},
		func(chunk string) error {
			output.WriteString(chunk)
			return nil
		},
	)
	if err != nil {
		t.Fatalf("stream chat: %v", err)
	}
	if output.String() != "您好， 欢迎使用\n\n**AI LocalBase**" {
		t.Fatalf("stream content changed whitespace: %q", output.String())
	}
}

func TestBuildOllamaChatRequestThinkingMode(t *testing.T) {
	enabled := true
	disabled := false
	tests := []struct {
		name     string
		think    *bool
		expected bool
	}{
		{name: "defaults to fast mode", think: nil, expected: false},
		{name: "keeps fast mode", think: &disabled, expected: false},
		{name: "enables thinking mode", think: &enabled, expected: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := buildOllamaChatRequest(
				model.ChatModelConfig{Model: "qwen3.5:0.8b"},
				model.ChatCompletionRequest{Think: tt.think},
				false,
			)
			if payload.Think == nil {
				t.Fatal("expected Ollama thinking mode to be explicit")
			}
			if *payload.Think != tt.expected {
				t.Fatalf("expected think=%v, got %v", tt.expected, *payload.Think)
			}
		})
	}
}

func TestChatResponseDegradationError(t *testing.T) {
	if err := ChatResponseDegradationError(model.ChatCompletionResponse{}); err != nil {
		t.Fatalf("expected normal response without error, got %v", err)
	}

	err := ChatResponseDegradationError(model.ChatCompletionResponse{Metadata: map[string]any{
		"degraded":      true,
		"upstreamError": "model timeout",
	}})
	if err == nil || err.Error() != "model timeout" {
		t.Fatalf("expected degradation error, got %v", err)
	}
}

func TestIsDegradedFallbackContent(t *testing.T) {
	for _, content := range []string{
		"⚠️ AI 模型调用已降级\n\n模型超时",
		"### 结果说明\nAI模型调用已降级至安全阈值，无法回答。",
	} {
		if !IsDegradedFallbackContent(content) {
			t.Fatalf("expected operational degradation content to be detected: %q", content)
		}
	}
	if IsDegradedFallbackContent("模型介绍了系统的降级设计，但当前回答正常。") {
		t.Fatal("expected ordinary content not to be treated as a degraded response")
	}
}
