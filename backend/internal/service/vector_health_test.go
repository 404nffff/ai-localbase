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

// TestDocumentVectorHealth 验证实测零值、缺失集合及异常不会被本地分块数替代。
func TestDocumentVectorHealth(t *testing.T) {
	for _, test := range []struct {
		name                      string
		collectionStatus, size    int
		countBody, source, status string
	}{
		{"actual", 200, 3, `{"result":{"count":2}}`, "actual", "ok"},
		{"zero", 200, 3, `{"result":{"count":0}}`, "actual", "ok"},
		{"missing", 404, 3, "", "unknown", "collection_missing"},
		{"dimension", 200, 5, "", "unknown", "dimension_mismatch"},
		{"unavailable", 500, 3, "", "unknown", "unavailable"},
		{"malformed", 200, 3, `{"result":{}}`, "unknown", "unavailable"},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/points/count") {
					var request struct {
						Exact  bool           `json:"exact"`
						Filter map[string]any `json:"filter"`
					}
					if err := json.NewDecoder(r.Body).Decode(&request); err != nil || !request.Exact || request.Filter == nil {
						t.Errorf("invalid count request: %+v %v", request, err)
					}
					_, _ = w.Write([]byte(test.countBody))
					return
				}
				w.WriteHeader(test.collectionStatus)
				_ = json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"config": map[string]any{"params": map[string]any{"vectors": map[string]any{"size": test.size, "distance": "Cosine"}}}}})
			}))
			defer server.Close()
			qdrant := NewQdrantService(model.ServerConfig{QdrantURL: server.URL, QdrantVectorSize: 3})
			got := qdrant.InspectDocumentVectors(context.Background(), model.Document{ID: "doc-1", KnowledgeBaseID: "kb-1", Status: "indexed", ChunkCount: 99})
			if got.Source != test.source || got.Status != test.status {
				t.Fatalf("unexpected health: %+v", got)
			}
			if test.name == "actual" && got.Count != 2 {
				t.Fatalf("expected actual count, got %+v", got)
			}
			if test.name == "zero" && got.Count != 0 {
				t.Fatalf("expected zero, got %+v", got)
			}
		})
	}
}
