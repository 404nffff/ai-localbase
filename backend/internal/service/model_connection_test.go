package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"ai-localbase/internal/model"
)

// TestOpenAIEndpoint 覆盖根地址、显式版本、自定义前缀及完整端点，避免重复补路径。
func TestOpenAIEndpoint(t *testing.T) {
	for _, tc := range []struct{ base, resource, want string }{
		{" https://example.test/ ", "chat/completions", "https://example.test/v1/chat/completions"},
		{"https://example.test", "embeddings", "https://example.test/v1/embeddings"},
		{"https://example.test/v1/", "chat/completions", "https://example.test/v1/chat/completions"},
		{"https://example.test/proxy/v2", "chat/completions", "https://example.test/proxy/v2/chat/completions"},
		{"https://example.test/custom/", "embeddings", "https://example.test/custom/embeddings"},
		{"https://example.test/v1/chat/completions", "chat/completions", "https://example.test/v1/chat/completions"},
		{"https://example.test/v1/embeddings/", "embeddings", "https://example.test/v1/embeddings"},
		{"https://example.test?api-version=2026", "chat/completions", "https://example.test/v1/chat/completions?api-version=2026"},
		{"https://example.test/a%2Fb/v1", "embeddings", "https://example.test/a%2Fb/v1/embeddings"},
	} {
		t.Run(tc.base+tc.resource, func(t *testing.T) {
			got, err := openAIEndpoint(tc.base, tc.resource)
			if err != nil || got != tc.want {
				t.Fatalf("endpoint = %q, %v; want %q", got, err, tc.want)
			}
		})
	}
	if _, err := openAIEndpoint("http://[invalid", "embeddings"); err == nil {
		t.Fatal("malformed URL accepted")
	}
}

// TestModelRootURLRequests 通过真实 HTTP 服务验证聊天、流式和 Embedding 共用补全规则。
func TestModelRootURLRequests(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		body, _ := io.ReadAll(r.Body)
		switch r.URL.Path {
		case "/v1/chat/completions":
			if strings.Contains(string(body), `"stream":true`) {
				_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"完成\"}}]}\n\ndata: [DONE]\n\n")
				return
			}
			_, _ = fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"完成"}}]}`)
		case "/v1/embeddings":
			_, _ = fmt.Fprint(w, `{"data":[{"index":0,"embedding":[1,0]}]}`)
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	llm := &LLMService{client: server.Client(), streamClient: server.Client()}
	req := model.ChatCompletionRequest{Config: model.ChatModelConfig{Provider: "openai-compatible", BaseURL: server.URL, Model: "fixture"}, SingleAttempt: true}
	resp, err := llm.Chat(req)
	if err != nil || resp.Metadata["degraded"] == true || resp.Choices[0].Message.Content != "完成" {
		t.Fatalf("chat: %+v, %v", resp, err)
	}
	var streamed string
	if err := llm.StreamChat(req, func(chunk string) error { streamed += chunk; return nil }); err != nil || streamed != "完成" {
		t.Fatalf("stream: %q, %v", streamed, err)
	}
	rag := NewRagService()
	vectors, err := rag.requestEmbeddings(context.Background(), model.EmbeddingModelConfig{Provider: "openai-compatible", BaseURL: server.URL, Model: "fixture"}, []string{"资料"})
	if err != nil || len(vectors) != 1 || len(vectors[0]) != 2 {
		t.Fatalf("embeddings: %v, %v", vectors, err)
	}
}

// TestModelBodyReadFailures 覆盖正文断线恢复、Wiki 单次预算和收到响应头后超时。
func TestModelBodyReadFailures(t *testing.T) {
	for _, provider := range []string{"openai-compatible", "ollama"} {
		t.Run(provider, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if calls.Add(1)%2 == 1 {
					w.Header().Set("Content-Length", "500")
					_, _ = fmt.Fprint(w, `{"incomplete":`)
					return
				}
				if provider == "ollama" {
					_, _ = fmt.Fprint(w, `{"message":{"role":"assistant","content":"完成"}}`)
				} else {
					_, _ = fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"完成"}}]}`)
				}
			}))
			defer server.Close()
			llm := &LLMService{client: server.Client()}
			req := model.ChatCompletionRequest{Config: model.ChatModelConfig{Provider: provider, BaseURL: server.URL, Model: "fixture"}}
			resp, err := llm.Chat(req)
			if err != nil || resp.Metadata["degraded"] == true || calls.Load() != 2 {
				t.Fatalf("interruption did not recover: %+v, %v, calls=%d", resp, err, calls.Load())
			}
			req.SingleAttempt = true
			resp, err = llm.Chat(req)
			if err != nil || resp.Metadata["degraded"] != true || !strings.Contains(metadataString(resp.Metadata, "upstreamError"), "unexpected EOF") || calls.Load() != 3 {
				t.Fatalf("single attempt lost cause or retried: %+v, %v, calls=%d", resp, err, calls.Load())
			}
		})
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	llm := &LLMService{client: server.Client()}
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	_, err := llm.openAIChat(ctx, model.ChatModelConfig{BaseURL: server.URL, Model: "fixture"}, model.ChatCompletionRequest{SingleAttempt: true})
	if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(describeModelError(err), "读取模型响应失败") || isRetryableModelError(err) {
		t.Fatalf("body timeout classification: %v", err)
	}
}

// TestModelRetryStopsOnPermanentFailure 保证不可重试错误不会消耗剩余请求预算。
func TestModelRetryStopsOnPermanentFailure(t *testing.T) {
	for _, cause := range []error{errors.New("model api error: http 401"), errors.New("invalid model response format"), context.Canceled} {
		calls := 0
		err := retryModelCall(context.Background(), 3, time.Millisecond, func() error {
			calls++
			return cause
		})
		if calls != 1 || !errors.Is(err, cause) {
			t.Fatalf("permanent failure retried: %v, calls=%d", err, calls)
		}
	}
	// 瞬时断线仍受三次总预算约束，不无限重试。
	calls := 0
	err := retryModelCall(context.Background(), 3, time.Millisecond, func() error {
		calls++
		return fmt.Errorf("read response: %w", io.ErrUnexpectedEOF)
	})
	if calls != 3 || !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("retry budget changed: %v, calls=%d", err, calls)
	}
}

// TestWikiContextDeduplicatesEvidence 共享来源只出现一次，不同引文、页面和修订继续保留。
func TestWikiContextDeduplicatesEvidence(t *testing.T) {
	ref := model.WikiSourceRef{DocumentID: "doc-1", Checksum: "v1", Quote: "事实甲"}
	chunk := RetrievedChunk{DocumentChunk: DocumentChunk{ID: "chunk-1", KnowledgeBaseID: "kb-1", DocumentID: "wiki-1", DocumentName: "知识页", Text: "第一段", Kind: "wiki", WikiRevision: 1, SourceRefs: []model.WikiSourceRef{ref, ref}}}
	second := chunk
	second.ID, second.Text = "chunk-2", "第二段"
	second.SourceRefs = append([]model.WikiSourceRef{ref}, model.WikiSourceRef{DocumentID: "doc-1", Checksum: "v1", Quote: "事实乙"})
	third := chunk
	third.DocumentID = "wiki-2"
	fourth := chunk
	fourth.WikiRevision = 2
	text, sources := NewRagService().BuildContext([]RetrievedChunk{chunk, second, third, fourth})
	if len(sources) != 8 || strings.Count(text, "[原始证据 doc-1] 事实甲") != 3 || strings.Count(text, "事实乙") != 1 {
		t.Fatalf("evidence lost or repeated: %d sources, %s", len(sources), text)
	}
}

// TestModelHTTPRetryClassification 通过真实 HTTP 错误验证状态码不受 JSON 或纯文本错误文案影响。
func TestModelHTTPRetryClassification(t *testing.T) {
	for _, provider := range []string{"openai-compatible", "ollama"} {
		for _, status := range []int{401, 429, 502, 503, 504} {
			for _, plain := range []bool{false, true} {
				for _, stream := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s/%d/plain=%t/stream=%t", provider, status, plain, stream), func(t *testing.T) {
						t.Parallel()
						var calls atomic.Int32
						server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
							calls.Add(1)
							w.WriteHeader(status)
							// 永久失败故意包含重试关键词，瞬时失败故意不包含，防止靠文字碰巧通过。
							message := "capacity exhausted"
							if status == http.StatusUnauthorized {
								message = "timeout while validating credentials"
							}
							if plain {
								_, _ = fmt.Fprint(w, message)
							} else if provider == "ollama" {
								_, _ = fmt.Fprintf(w, `{"error":%q}`, message)
							} else {
								_, _ = fmt.Fprintf(w, `{"error":{"message":%q}}`, message)
							}
						}))
						defer server.Close()
						llm := &LLMService{client: server.Client(), streamClient: server.Client()}
						cfg := model.ChatModelConfig{Provider: provider, BaseURL: server.URL, Model: "fixture"}
						var err error
						wantCalls := int32(3)
						if stream {
							wantCalls = 2
							if provider == "ollama" {
								err = llm.ollamaStreamChat(context.Background(), cfg, model.ChatCompletionRequest{}, func(string) error { return nil })
							} else {
								err = llm.openAIStreamChat(context.Background(), cfg, model.ChatCompletionRequest{}, func(string) error { return nil })
							}
						} else if provider == "ollama" {
							_, err = llm.ollamaChat(context.Background(), cfg, model.ChatCompletionRequest{})
						} else {
							_, err = llm.openAIChat(context.Background(), cfg, model.ChatCompletionRequest{})
						}
						if status == http.StatusUnauthorized {
							wantCalls = 1
						}
						if err == nil || calls.Load() != wantCalls {
							t.Fatalf("status %d: calls=%d, want=%d, error=%v", status, calls.Load(), wantCalls, err)
						}
					})
				}
			}
		}
	}
}
