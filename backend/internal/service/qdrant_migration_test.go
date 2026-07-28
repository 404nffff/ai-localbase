package service

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"ai-localbase/internal/model"
)

func TestQdrantServiceScrollPointPayloadsPreservesLargeNumericIDs(t *testing.T) {
	const largePointID = "18446744073709551614"

	var (
		mu       sync.Mutex
		requests []qdrantScrollRequest
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/collections/source_kb-1/points/scroll" {
			t.Fatalf("unexpected qdrant request: %s %s", r.Method, r.URL.Path)
		}

		decoder := json.NewDecoder(r.Body)
		decoder.UseNumber()
		var request qdrantScrollRequest
		if err := decoder.Decode(&request); err != nil {
			t.Fatalf("decode scroll request: %v", err)
		}
		mu.Lock()
		requests = append(requests, request)
		requestCount := len(requests)
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		if requestCount == 1 {
			fmt.Fprintf(w, `{"result":{"points":[{"id":%s,"payload":{"text":"first"}}],"next_page_offset":%s}}`, largePointID, largePointID)
			return
		}
		_, _ = w.Write([]byte(`{"result":{"points":[{"id":"point-2","payload":{"text":"second"}}],"next_page_offset":null}}`))
	}))
	t.Cleanup(server.Close)

	qdrant := NewQdrantService(model.ServerConfig{
		QdrantURL:              server.URL,
		QdrantCollectionPrefix: "source_",
		QdrantVectorSize:       4,
	})
	points, err := qdrant.ScrollPointPayloads(t.Context(), "kb-1")
	if err != nil {
		t.Fatalf("scroll point payloads: %v", err)
	}
	if len(points) != 2 {
		t.Fatalf("expected 2 points, got %d", len(points))
	}
	pointID, ok := points[0].ID.(json.Number)
	if !ok || pointID.String() != largePointID {
		t.Fatalf("expected exact numeric point id %s, got %#v", largePointID, points[0].ID)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 2 {
		t.Fatalf("expected 2 scroll requests, got %d", len(requests))
	}
	offset, ok := requests[1].Offset.(json.Number)
	if !ok || offset.String() != largePointID {
		t.Fatalf("expected exact next-page offset %s, got %#v", largePointID, requests[1].Offset)
	}
}

func TestMigrateQdrantPayloadsReembedsAndPreservesPayload(t *testing.T) {
	const largePointID = "18446744073709551614"

	embeddingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/embed" {
			t.Fatalf("unexpected embedding request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"embeddings":[[0.1,0.2,0.3,0.4]]}`))
	}))
	t.Cleanup(embeddingServer.Close)

	var upserted qdrantPointUpsertRequest
	qdrantServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/collections/source_kb-1/points/scroll":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"result":{"points":[{"id":%s,"payload":{"knowledge_base_id":"kb-1","document_id":"doc-1","text":"source text"}}],"next_page_offset":null}}`, largePointID)
		case r.Method == http.MethodPut && r.URL.Path == "/collections/target_kb-1":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"result":true}`))
		case r.Method == http.MethodPut && r.URL.Path == "/collections/target_kb-1/points":
			decoder := json.NewDecoder(r.Body)
			decoder.UseNumber()
			if err := decoder.Decode(&upserted); err != nil {
				t.Fatalf("decode upsert request: %v", err)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"result":true}`))
		default:
			t.Fatalf("unexpected qdrant request: %s %s", r.Method, r.URL.Path)
		}
	}))
	t.Cleanup(qdrantServer.Close)

	baseConfig := model.ServerConfig{
		QdrantURL:        qdrantServer.URL,
		QdrantVectorSize: 4,
	}
	sourceConfig := baseConfig
	sourceConfig.QdrantCollectionPrefix = "source_"
	targetConfig := baseConfig
	targetConfig.QdrantCollectionPrefix = "target_"

	count, err := MigrateQdrantPayloads(
		t.Context(),
		NewQdrantService(sourceConfig),
		NewQdrantService(targetConfig),
		NewRagService(),
		model.EmbeddingModelConfig{
			Provider: "ollama",
			BaseURL:  embeddingServer.URL,
			Model:    "test-embedding",
		},
		"kb-1",
	)
	if err != nil {
		t.Fatalf("migrate qdrant payloads: %v", err)
	}
	if count != 1 || len(upserted.Points) != 1 {
		t.Fatalf("expected one migrated point, count=%d points=%d", count, len(upserted.Points))
	}

	point := upserted.Points[0]
	pointID, ok := point.ID.(json.Number)
	if !ok || pointID.String() != largePointID {
		t.Fatalf("expected exact migrated point id %s, got %#v", largePointID, point.ID)
	}
	if point.Payload["document_id"] != "doc-1" || point.Payload["text"] != "source text" {
		t.Fatalf("expected source payload to be preserved, got %#v", point.Payload)
	}
	vectors, ok := point.Vector.(map[string]any)
	if !ok {
		t.Fatalf("expected named vector payload, got %#v", point.Vector)
	}
	dense, ok := vectors[qdrantDenseVectorName].([]any)
	if !ok || len(dense) != 4 {
		t.Fatalf("expected 4-dimensional dense vector, got %#v", vectors[qdrantDenseVectorName])
	}
	if _, ok := vectors[qdrantSparseVectorName].(map[string]any); !ok {
		t.Fatalf("expected sparse vector to be rebuilt, got %#v", vectors[qdrantSparseVectorName])
	}
}

func TestMigrateQdrantPayloadsRejectsCollectionsWithoutText(t *testing.T) {
	qdrantServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":{"points":[{"id":1,"payload":{"document_id":"doc-1"}}],"next_page_offset":null}}`))
	}))
	t.Cleanup(qdrantServer.Close)

	config := model.ServerConfig{
		QdrantURL:              qdrantServer.URL,
		QdrantCollectionPrefix: "source_",
		QdrantVectorSize:       4,
	}
	_, err := MigrateQdrantPayloads(
		t.Context(),
		NewQdrantService(config),
		NewQdrantService(config),
		NewRagService(),
		model.EmbeddingModelConfig{},
		"kb-1",
	)
	if err == nil || !strings.Contains(err.Error(), "contains no text payloads") {
		t.Fatalf("expected missing text payload error, got %v", err)
	}
}
