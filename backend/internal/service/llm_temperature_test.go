package service

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"ai-localbase/internal/model"
)

// TestChatTemperatureModes 在真实 HTTP 边界验证两类协议及流式调用的默认温度和零值。
func TestChatTemperatureModes(t *testing.T) {
	for _, provider := range []string{"openai-compatible", "ollama"} {
		for _, stream := range []bool{false, true} {
			for _, useDefault := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/stream=%t/default=%t", provider, stream, useDefault), func(t *testing.T) {
					upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						var body map[string]any
						if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
							t.Error(err)
						}
						options := body
						if provider == "ollama" {
							options, _ = body["options"].(map[string]any)
							if useDefault && body["options"] != nil {
								t.Error("default mode sent options")
							}
						}
						value, present := options["temperature"]
						if present == useDefault || (!useDefault && value != float64(0)) {
							t.Errorf("invalid temperature payload: %+v", body)
						}
						if provider == "ollama" {
							_, _ = w.Write([]byte("{\"message\":{\"role\":\"assistant\",\"content\":\"pong\"}}\n"))
						} else if stream {
							_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"pong\"}}]}\n\ndata: [DONE]\n\n"))
						} else {
							_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"pong"}}]}`))
						}
					}))
					defer upstream.Close()
					llm := NewLLMService()
					defer llm.client.CloseIdleConnections()
					defer llm.streamClient.CloseIdleConnections()
					req := model.ChatCompletionRequest{Config: model.ChatModelConfig{Provider: provider, BaseURL: upstream.URL, Model: "fixture", UseDefaultTemperature: useDefault}, SingleAttempt: true}
					var content string
					if stream {
						if err := llm.StreamChat(req, func(chunk string) error { content += chunk; return nil }); err != nil {
							t.Fatal(err)
						}
					} else {
						response, err := llm.Chat(req)
						if err != nil {
							t.Fatal(err)
						}
						if len(response.Choices) == 0 {
							t.Fatal("model response has no choices")
						}
						content = response.Choices[0].Message.Content
					}
					if content != "pong" {
						t.Fatalf("unexpected model result: %q", content)
					}
				})
			}
		}
	}
}

// TestDefaultTemperaturePersists 验证显式保存后重启仍保留选项，并传给 Wiki 使用的当前模型快照。
func TestDefaultTemperaturePersists(t *testing.T) {
	dir := t.TempDir()
	store := NewAppStateStore(filepath.Join(dir, "state.json"))
	s := NewAppService(nil, store, nil, model.ServerConfig{UploadDir: dir})
	t.Cleanup(func() { _ = s.Close() })
	cfg := s.GetConfig()
	cfg.Chat.UseDefaultTemperature = true
	if _, err := s.UpdateConfig(model.ConfigUpdateRequest{Chat: cfg.Chat, Embedding: cfg.Embedding}); err != nil {
		t.Fatal(err)
	}
	restarted := NewAppService(nil, store, nil, model.ServerConfig{UploadDir: dir})
	t.Cleanup(func() { _ = restarted.Close() })
	if !restarted.GetConfig().Chat.UseDefaultTemperature || !restarted.currentChatConfig().UseDefaultTemperature {
		t.Fatal("saved default temperature option lost")
	}
}
