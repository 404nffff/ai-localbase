package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"ai-localbase/internal/model"
)

// TestWikiTimeoutConfig 验证旧状态升级、保存重启、边界以及旧客户端保留配置。
func TestWikiTimeoutConfig(t *testing.T) {
	dir := t.TempDir()
	store := NewAppStateStore(filepath.Join(dir, "state.json"))
	state := defaultAppState(model.ServerConfig{})
	state.Config.Wiki = model.WikiConfig{}
	if err := store.Save(persistentAppState{Config: state.Config, KnowledgeBases: state.KnowledgeBases}); err != nil {
		t.Fatal(err)
	}
	s := NewAppService(nil, store, nil, model.ServerConfig{StagingDir: dir})
	t.Cleanup(func() { _ = s.Close() })
	cfg := s.GetConfig()
	if cfg.Wiki.ModelTimeoutSeconds != 180 {
		t.Fatalf("old state default: %d", cfg.Wiki.ModelTimeoutSeconds)
	}
	req := model.ConfigUpdateRequest{Chat: cfg.Chat, Embedding: cfg.Embedding}
	for _, value := range []int{30, 1800, 300} {
		req.Wiki = &model.WikiConfig{ModelTimeoutSeconds: value}
		saved, err := s.UpdateConfig(req)
		if err != nil || saved.Wiki.ModelTimeoutSeconds != value {
			t.Fatalf("save %d: %+v, %v", value, saved.Wiki, err)
		}
	}
	for _, value := range []int{-1, 0, 29, 1801} {
		req.Wiki = &model.WikiConfig{ModelTimeoutSeconds: value}
		if _, err := s.UpdateConfig(req); err == nil || s.GetConfig().Wiki.ModelTimeoutSeconds != 300 {
			t.Fatalf("invalid value %d changed config or was accepted", value)
		}
	}
	req.Wiki = nil
	if saved, err := s.UpdateConfig(req); err != nil || saved.Wiki.ModelTimeoutSeconds != 300 {
		t.Fatalf("legacy client reset timeout: %+v, %v", saved.Wiki, err)
	}
	restarted := NewAppService(nil, store, nil, model.ServerConfig{StagingDir: dir})
	t.Cleanup(func() { _ = restarted.Close() })
	if restarted.GetConfig().Wiki.ModelTimeoutSeconds != 300 {
		t.Fatal("timeout did not survive restart")
	}
}

// TestWikiTimeoutClients 验证三层超时同值，普通聊天及流式默认不被 Wiki 改写。
func TestWikiTimeoutClients(t *testing.T) {
	wiki := newLLMServiceWithTimeout(180*time.Second, 180*time.Second)
	chat := NewLLMService()
	defer wiki.client.CloseIdleConnections()
	defer chat.client.CloseIdleConnections()
	if wiki.client.Timeout != 180*time.Second || wiki.client.Transport.(*http.Transport).ResponseHeaderTimeout != 180*time.Second {
		t.Fatal("Wiki client/header timeout mismatch")
	}
	if chat.client.Timeout != 75*time.Second || chat.client.Transport.(*http.Transport).ResponseHeaderTimeout != 45*time.Second || chat.streamClient.Transport.(*http.Transport).ResponseHeaderTimeout != 45*time.Second {
		t.Fatal("ordinary chat defaults changed")
	}
	// 在真正发出 HTTP 请求的边界检查上下文，避免等待完整的 180 秒。
	wiki.client.Transport = timeoutProbeTransport{t: t}
	_, _ = wiki.Chat(model.ChatCompletionRequest{Config: model.ChatModelConfig{Provider: "openai-compatible", BaseURL: "http://timeout.test", Model: "fixture"}, SingleAttempt: true})
}

// timeoutProbeTransport 只替换网络边界，检查实际 Chat 创建的 deadline。
type timeoutProbeTransport struct{ t *testing.T }

// RoundTrip 核对业务请求的剩余期限，并返回超时错误以终止真实网络访问。
func (p timeoutProbeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	deadline, ok := req.Context().Deadline()
	if remaining := time.Until(deadline); !ok || remaining < 179*time.Second || remaining > 180*time.Second {
		p.t.Errorf("unexpected request deadline: %v, %v", remaining, ok)
	}
	return nil, context.DeadlineExceeded
}

// TestWikiTimeoutDelayedHTTP 用真实延迟响应验证超时取消与足够时长下的成功路径。
func TestWikiTimeoutDelayedHTTP(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			return
		case <-time.After(100 * time.Millisecond):
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"完成"}}]}`))
	}))
	defer server.Close()
	for _, timeout := range []time.Duration{20 * time.Millisecond, time.Second} {
		llm := newLLMServiceWithTimeout(timeout, timeout)
		response, err := llm.Chat(model.ChatCompletionRequest{Config: model.ChatModelConfig{Provider: "openai-compatible", BaseURL: server.URL, Model: "fixture"}, SingleAttempt: true})
		llm.client.CloseIdleConnections()
		if err != nil {
			t.Fatal(err)
		}
		degraded, _ := response.Metadata["degraded"].(bool)
		if degraded != (timeout < 100*time.Millisecond) {
			t.Fatalf("timeout %v degraded=%v", timeout, degraded)
		}
	}
}
