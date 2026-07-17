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

func TestQdrantEnsureCollectionCreatesNamedDenseAndSparseSchema(t *testing.T) {
	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/collections/kb_kb-1":
			http.NotFound(w, r)
		case r.Method == http.MethodPut && r.URL.Path == "/collections/kb_kb-1":
			if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
				t.Fatalf("decode collection request: %v", err)
			}
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	qdrant := newQdrantTestService(server.URL)
	if err := qdrant.EnsureCollection(context.Background(), "kb-1"); err != nil {
		t.Fatalf("ensure new collection: %v", err)
	}

	vectors, ok := requestBody["vectors"].(map[string]any)
	if !ok || vectors["dense"] == nil {
		t.Fatalf("expected named dense vector schema, got %#v", requestBody)
	}
	sparseVectors, ok := requestBody["sparse_vectors"].(map[string]any)
	if !ok || sparseVectors["sparse"] == nil {
		t.Fatalf("expected named sparse vector schema, got %#v", requestBody)
	}
}

func TestQdrantUpsertUsesLegacyDenseVectorForLegacyCollection(t *testing.T) {
	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/collections/kb_kb-1":
			writeQdrantCollectionInfo(t, w, 1024, "Cosine")
		case r.Method == http.MethodPut && r.URL.Path == "/collections/kb_kb-1/points":
			if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
				t.Fatalf("decode point request: %v", err)
			}
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	qdrant := newQdrantTestService(server.URL)
	err := qdrant.UpsertPoints(context.Background(), "kb-1", []QdrantPoint{{
		ID:           "point-1",
		Vector:       make([]float64, 1024),
		SparseVector: SparseVector{Indices: []uint32{3}, Values: []float32{1}},
	}})
	if err != nil {
		t.Fatalf("upsert legacy point: %v", err)
	}

	points := requestBody["points"].([]any)
	vector := points[0].(map[string]any)["vector"]
	if _, ok := vector.([]any); !ok {
		t.Fatalf("expected unnamed dense vector array, got %#v", vector)
	}
}

func TestQdrantUpsertUsesNamedDenseAndSparseVectors(t *testing.T) {
	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/collections/kb_kb-1":
			writeQdrantNamedCollectionInfo(t, w, 1024, "Cosine", true)
		case r.Method == http.MethodPut && r.URL.Path == "/collections/kb_kb-1/points":
			if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
				t.Fatalf("decode point request: %v", err)
			}
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	qdrant := newQdrantTestService(server.URL)
	err := qdrant.UpsertPoints(context.Background(), "kb-1", []QdrantPoint{{
		ID:           "point-1",
		Vector:       make([]float64, 1024),
		SparseVector: SparseVector{Indices: []uint32{3}, Values: []float32{1}},
	}})
	if err != nil {
		t.Fatalf("upsert named point: %v", err)
	}

	points := requestBody["points"].([]any)
	vectors := points[0].(map[string]any)["vector"].(map[string]any)
	if _, ok := vectors["dense"].([]any); !ok {
		t.Fatalf("expected named dense vector, got %#v", vectors)
	}
	if _, ok := vectors["sparse"].(map[string]any); !ok {
		t.Fatalf("expected named sparse vector, got %#v", vectors)
	}
}

func TestQdrantSearchFallsBackToLegacyOnlyForCompatibilityError(t *testing.T) {
	var legacySearchCalled bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/collections/kb_kb-1/points/query":
			http.Error(w, "unknown vector name dense", http.StatusBadRequest)
		case "/collections/kb_kb-1/points/search":
			legacySearchCalled = true
			writeQdrantSearchResponse(t, w, false)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	results, err := newQdrantTestService(server.URL).Search(
		context.Background(),
		"kb-1",
		make([]float64, 1024),
		5,
		nil,
	)
	if err != nil {
		t.Fatalf("search legacy collection: %v", err)
	}
	if !legacySearchCalled || len(results) != 1 || results[0].ID != "point-1" {
		t.Fatalf("expected legacy result, got %#v", results)
	}
}

func TestQdrantSearchReadsNamedQueryResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/collections/kb_kb-1/points/query" {
			http.NotFound(w, r)
			return
		}
		writeQdrantSearchResponse(t, w, true)
	}))
	t.Cleanup(server.Close)

	results, err := newQdrantTestService(server.URL).Search(
		context.Background(),
		"kb-1",
		make([]float64, 1024),
		5,
		nil,
	)
	if err != nil {
		t.Fatalf("search named collection: %v", err)
	}
	if len(results) != 1 || results[0].ID != "point-1" {
		t.Fatalf("unexpected query result: %#v", results)
	}
}

func TestQdrantHybridReturnsDenseWhenSparseQueryFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body qdrantQueryRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode query request: %v", err)
		}
		if body.Using == qdrantSparseVectorName {
			http.Error(w, "sparse vector unavailable", http.StatusBadRequest)
			return
		}
		writeQdrantSearchResponse(t, w, true)
	}))
	t.Cleanup(server.Close)

	results, err := newQdrantTestService(server.URL).SearchHybrid(context.Background(), HybridSearchParams{
		CollectionName: "kb-1",
		DenseVector:    make([]float32, 1024),
		SparseVector:   SparseVector{Indices: []uint32{1}, Values: []float32{1}},
		TopK:           5,
	})
	if err != nil {
		t.Fatalf("hybrid search: %v", err)
	}
	if len(results) != 1 || results[0].ID != "point-1" {
		t.Fatalf("expected dense fallback result, got %#v", results)
	}
}

func TestRRFFusionAddsRetrievalChannelMetadata(t *testing.T) {
	results := rrfFusion(
		[]SearchResult{{ID: "shared", Payload: map[string]any{"text": "dense"}}},
		[]SearchResult{{ID: "shared", Payload: map[string]any{"text": "sparse"}}},
		5,
	)
	if len(results) != 1 {
		t.Fatalf("expected one fused result, got %#v", results)
	}
	channels, ok := results[0].Payload[qdrantPayloadRetrievalChannels].([]string)
	if !ok || strings.Join(channels, ",") != "dense,sparse" {
		t.Fatalf("unexpected retrieval channels: %#v", results[0].Payload)
	}
	if results[0].Payload[qdrantPayloadDenseRank] != 1 || results[0].Payload[qdrantPayloadSparseRank] != 1 {
		t.Fatalf("unexpected rank metadata: %#v", results[0].Payload)
	}
}

func newQdrantTestService(serverURL string) *QdrantService {
	return NewQdrantService(model.ServerConfig{
		QdrantURL:              serverURL,
		QdrantCollectionPrefix: "kb_",
		QdrantVectorSize:       1024,
		QdrantDistance:         "Cosine",
	})
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

func writeQdrantNamedCollectionInfo(t *testing.T, w http.ResponseWriter, size int, distance string, sparse bool) {
	t.Helper()
	params := map[string]any{
		"vectors": map[string]any{
			"dense": map[string]any{
				"size":     size,
				"distance": distance,
			},
		},
	}
	if sparse {
		params["sparse_vectors"] = map[string]any{"sparse": map[string]any{}}
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{
		"result": map[string]any{
			"config": map[string]any{
				"params": params,
			},
		},
	}); err != nil {
		t.Fatalf("encode named qdrant collection info: %v", err)
	}
}

func writeQdrantSearchResponse(t *testing.T, w http.ResponseWriter, queryStyle bool) {
	t.Helper()
	points := []map[string]any{{
		"id":      "point-1",
		"score":   0.9,
		"payload": map[string]any{"text": "result"},
	}}
	result := any(points)
	if queryStyle {
		result = map[string]any{"points": points}
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{"result": result}); err != nil {
		t.Fatalf("encode qdrant search response: %v", err)
	}
}
