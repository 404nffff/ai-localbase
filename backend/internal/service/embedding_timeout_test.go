package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"ai-localbase/internal/model"
)

func TestEmbeddingTimeoutConfiguration(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  time.Duration
	}{
		{"", 180 * time.Second},
		{"300", 300 * time.Second},
		{" 1 ", time.Second},
		{"3600", time.Hour},
		{"0", 180 * time.Second},
		{"-1", 180 * time.Second},
		{"invalid", 180 * time.Second},
		{"3601", 180 * time.Second},
		{"999999999999999999999", 180 * time.Second},
	} {
		t.Run(tc.value, func(t *testing.T) {
			t.Setenv("EMBEDDING_TIMEOUT_SECONDS", tc.value)
			rag := NewRagService()
			transport := rag.client.Transport.(*http.Transport)
			if rag.client.Timeout != tc.want || transport.ResponseHeaderTimeout != tc.want {
				t.Fatalf("want both timeouts %v, got client=%v header=%v", tc.want, rag.client.Timeout, transport.ResponseHeaderTimeout)
			}
		})
	}
}

func TestEmbeddingBatchesPreserveOrder(t *testing.T) {
	for _, provider := range []string{"ollama", "openai-compatible"} {
		for _, fresh := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/fresh=%t", provider, fresh), func(t *testing.T) {
				var batches []int
				var received []string
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					var request openAIEmbeddingRequest
					if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
						t.Error(err)
						http.Error(w, "bad request", http.StatusBadRequest)
						return
					}
					batches = append(batches, len(request.Input))
					vectors := make([][]float64, len(request.Input))
					data := make([]map[string]any, len(request.Input))
					for i, text := range request.Input {
						received = append(received, text)
						vectors[i] = []float64{float64(len(received)), 1}
						data[i] = map[string]any{"index": i, "embedding": vectors[i]}
					}
					if provider == "ollama" {
						_ = json.NewEncoder(w).Encode(ollamaEmbedResponse{Embeddings: vectors})
					} else {
						_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
					}
				}))
				defer server.Close()
				rag := NewRagService()
				defer rag.client.CloseIdleConnections()
				embed := rag.EmbedTexts
				if fresh {
					embed = rag.EmbedTextsFresh
				}
				texts := make([]string, 17)
				for i := range texts {
					texts[i] = fmt.Sprintf("知识片段-%d", i)
				}
				vectors, err := embed(t.Context(), model.EmbeddingModelConfig{Provider: provider, BaseURL: server.URL, Model: "fixture"}, texts, 2)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(batches, []int{8, 8, 1}) || !reflect.DeepEqual(received, texts) || len(vectors) != len(texts) {
					t.Fatalf("unexpected batches/order: %v, %v, %v", batches, received, vectors)
				}
				for i, vector := range vectors {
					if vector[0] != float64(i+1) || vector[1] != 1 {
						t.Fatalf("vector %d out of order: %v", i, vector)
					}
				}
			})
		}
	}
}

func TestEmbeddingRequestDeadline(t *testing.T) {
	for _, phase := range []string{"headers", "body", "caller-cancel"} {
		t.Run(phase, func(t *testing.T) {
			t.Setenv("EMBEDDING_TIMEOUT_SECONDS", "1")
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if phase == "body" {
					w.WriteHeader(http.StatusOK)
					w.(http.Flusher).Flush()
				}
				if phase == "caller-cancel" {
					cancel()
				}
				select {
				case <-r.Context().Done():
				case <-time.After(3 * time.Second):
					_, _ = w.Write([]byte(`{"embeddings":[[1,2]]}`))
				}
			}))
			defer server.Close()
			rag := NewRagService()
			defer rag.client.CloseIdleConnections()
			vectors, err := rag.EmbedTextsFresh(ctx, model.EmbeddingModelConfig{Provider: "ollama", BaseURL: server.URL, Model: "fixture"}, []string{"超时检查"}, 2)
			if vectors != nil || err == nil {
				t.Fatalf("request should fail without substitute vectors: %v, %v", vectors, err)
			}
			if phase == "caller-cancel" {
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("cancellation cause lost: %v", err)
				}
			} else {
				var timeout interface{ Timeout() bool }
				if !errors.As(err, &timeout) || !timeout.Timeout() {
					t.Fatalf("timeout cause lost: %v", err)
				}
			}
		})
	}
}
