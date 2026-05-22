package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ai-localbase/internal/model"
)

func TestQdrantEnsureCollectionAcceptsExistingMatchingSchema(t *testing.T) {
	var putCalled bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/collections/kb_kb-1" {
			writeQdrantCollectionInfo(t, w, 1024, "Cosine")
			return
		}
		if r.Method == http.MethodPut && r.URL.Path == "/collections/kb_kb-1" {
			putCalled = true
			http.Error(w, "collection already exists", http.StatusConflict)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)

	qdrant := NewQdrantService(model.ServerConfig{
		QdrantURL:              server.URL,
		QdrantCollectionPrefix: "kb_",
		QdrantVectorSize:       1024,
		QdrantDistance:         "Cosine",
	})

	if err := qdrant.EnsureCollection(context.Background(), "kb-1"); err != nil {
		t.Fatalf("ensure existing collection: %v", err)
	}
	if putCalled {
		t.Fatal("expected existing matching collection to be reused without PUT")
	}
}

func TestQdrantEnsureCollectionRejectsExistingDimensionMismatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/collections/kb_kb-1" {
			writeQdrantCollectionInfo(t, w, 768, "Cosine")
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)

	qdrant := NewQdrantService(model.ServerConfig{
		QdrantURL:              server.URL,
		QdrantCollectionPrefix: "kb_",
		QdrantVectorSize:       1024,
		QdrantDistance:         "Cosine",
	})

	err := qdrant.EnsureCollection(context.Background(), "kb-1")
	if err == nil {
		t.Fatal("expected collection schema mismatch error")
	}
	if !strings.Contains(err.Error(), "qdrant collection schema mismatch") {
		t.Fatalf("expected schema mismatch error, got %v", err)
	}
}

func writeQdrantCollectionInfo(t *testing.T, w http.ResponseWriter, size int, distance string) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	err := json.NewEncoder(w).Encode(map[string]any{
		"result": map[string]any{
			"config": map[string]any{
				"params": map[string]any{
					"vectors": map[string]any{
						"size":     size,
						"distance": distance,
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("encode qdrant collection info: %v", err)
	}
}
