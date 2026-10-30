package service

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"ai-localbase/internal/model"
)

// fixtureEmbeddingConfig 只模拟模型 HTTP 边界，测试仍经过实际嵌入与校验代码。
func fixtureEmbeddingConfig(t *testing.T, dimension func() int) model.EmbeddingModelConfig {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		data := make([]any, 0, len(req.Input))
		for i := range req.Input {
			vector := make([]float64, dimension())
			vector[0] = 1
			data = append(data, map[string]any{"index": i, "embedding": vector})
		}
		json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	t.Cleanup(server.Close)
	return model.EmbeddingModelConfig{Provider: "openai-compatible", BaseURL: server.URL, Model: "fixture"}
}

// configureFixtureEmbedding 为已有文档测试显式配置模型，不再依赖生产哈希兜底。
func configureFixtureEmbedding(t *testing.T, s *AppService) {
	t.Helper()
	cfg := fixtureEmbeddingConfig(t, s.qdrantVectorSize)
	s.state.Config.Embedding = model.EmbeddingConfig{Provider: cfg.Provider, BaseURL: cfg.BaseURL, Model: cfg.Model}
}

// fixtureWritableQdrant 模拟集合结构和写入确认；真实存储验证由本地 Qdrant 集成用例承担。
func fixtureWritableQdrant(t *testing.T, dimension int) *QdrantService {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			fmt.Fprintf(w, `{"result":{"config":{"params":{"vectors":{"size":%d,"distance":"Cosine"}}}}}`, dimension)
			return
		}
		w.Write([]byte(`{"result":{"status":"completed"},"status":"ok"}`))
	}))
	t.Cleanup(server.Close)
	return NewQdrantService(model.ServerConfig{QdrantURL: server.URL, QdrantVectorSize: dimension})
}

// TestEmbeddingValidationAndFreshRebuild 验证向量有效性、缓存重新获取和重建强制刷新。
func TestEmbeddingValidationAndFreshRebuild(t *testing.T) {
	for name, vector := range map[string][]float64{
		"zero": {0, 0}, "nan": {math.NaN(), 1}, "infinity": {math.Inf(1), 1},
		"overflow": {math.MaxFloat64, 1}, "underflow": {math.SmallestNonzeroFloat64, 0}, "dimension": {1},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := normalizeEmbeddings([][]float64{vector}, 2); err == nil {
				t.Fatal("invalid vector accepted")
			}
		})
	}
	rag := NewRagService()
	cfg := fixtureEmbeddingConfig(t, func() int { return 2 })
	key := embeddingCacheKey(cfg.Provider, cfg.BaseURL, cfg.Model, "岗位")
	rag.cache.Set(key, []float64{0, 1})
	cached, err := rag.EmbedTexts(t.Context(), cfg, []string{"岗位"}, 2)
	if err != nil || cached[0][1] != 1 {
		t.Fatalf("cache: %v %v", cached, err)
	}
	fresh, err := rag.EmbedTextsFresh(t.Context(), cfg, []string{"岗位"}, 2)
	if err != nil || fresh[0][0] != 1 || fresh[0][1] != 0 {
		t.Fatalf("fresh rebuild reused cache: %v %v", fresh, err)
	}
	for _, invalid := range [][]float64{{0, 0}, {1, 0, 0}} {
		rag.cache.Set(key, invalid)
		got, err := rag.EmbedTexts(t.Context(), cfg, []string{"岗位"}, 2)
		if err != nil || len(got[0]) != 2 || got[0][0] != 1 {
			t.Fatalf("invalid cache not refreshed: %v %v", got, err)
		}
	}
}

// TestRecallRejectsUnrelatedAndMatchesChinese 验证无关低分过滤、中文字段匹配与融合分数来源。
func TestRecallRejectsUnrelatedAndMatchesChinese(t *testing.T) {
	s := &AppService{}
	for _, score := range []float64{0, .02, -.1} {
		got := s.rerankCandidates(t.Context(), []RetrievedChunk{{DocumentChunk: DocumentChunk{Text: "项目部署流程说明"}, Score: score}}, "何安敏")
		if len(got) != 0 {
			t.Fatalf("unrelated score %.3f returned: %+v", score, got)
		}
	}
	text := "员工的部门来自 hr_employment_record.dept_code，岗位来自 position_code。"
	for _, query := range []string{"员工的部门和岗位信息", "员工 部门 岗位", "hr_employment_record dept_code position_code"} {
		got := s.rerankCandidates(t.Context(), []RetrievedChunk{{DocumentChunk: DocumentChunk{Text: text}, Score: 0}}, query)
		if len(got) != 1 || got[0].Score > .3 {
			t.Fatalf("Chinese/field matching or score incorrect: %q %+v", query, got)
		}
	}
	fused := rrfFusion([]SearchResult{{ID: "bad", Score: .02}, {ID: "good", Score: .8}}, []SearchResult{{ID: "sparse", Score: 12}}, 3)
	for _, result := range fused {
		want := map[string]float64{"bad": .02, "good": .8, "sparse": 0}[result.ID]
		if result.Payload["retrieval_dense_score"] != want {
			t.Fatalf("RRF lost original similarity: %+v", result)
		}
	}
}

// TestEmbeddingLaterBatchFailure 验证后续批次失败不能返回前一批的部分成功或混入替代向量。
func TestEmbeddingLaterBatchFailure(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) > 1 {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":"second batch failed"}`))
			return
		}
		var req struct {
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		data := []any{}
		for i := range req.Input {
			data = append(data, map[string]any{"index": i, "embedding": []float64{1, 0}})
		}
		json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	defer server.Close()
	texts := make([]string, embeddingBatchSize+1)
	for i := range texts {
		texts[i] = fmt.Sprintf("分块%d", i)
	}
	for _, name := range []string{"cached", "fresh"} {
		t.Run(name, func(t *testing.T) {
			calls.Store(0)
			rag := NewRagService()
			embed := rag.EmbedTexts
			if name == "fresh" {
				embed = rag.EmbedTextsFresh
			}
			vectors, err := embed(t.Context(), model.EmbeddingModelConfig{Provider: "openai-compatible", BaseURL: server.URL, Model: "fixture"}, texts, 2)
			if err == nil || vectors != nil || calls.Load() != 2 {
				t.Fatalf("partial success escaped: %v %v calls=%d", vectors, err, calls.Load())
			}
		})
	}
}

// TestDebugAndReindexRequireVectorService 验证服务停用时不能将调试或重建报告为成功。
func TestDebugAndReindexRequireVectorService(t *testing.T) {
	s := NewAppService(nil, nil, nil, model.ServerConfig{UploadDir: t.TempDir()})
	defer s.Close()
	if _, err := s.DebugRetrieve(model.RetrievalDebugRequest{Query: "岗位"}); err == nil {
		t.Fatal("disabled search reported success")
	}
	if _, err := s.ReindexDocument("kb", "doc"); err == nil {
		t.Fatal("disabled rebuild reported success")
	}
}

// TestRebuildVectorsWithLocalQdrant 用临时集合验证写入、读回、调试召回和失败后保留旧代际；不修改挂载原件。
func TestRebuildVectorsWithLocalQdrant(t *testing.T) {
	endpoint := os.Getenv("RECALL_TEST_QDRANT_URL")
	if endpoint == "" {
		t.Skip("set RECALL_TEST_QDRANT_URL to the local test Qdrant")
	}
	var calls atomic.Int32
	var fail atomic.Bool
	embedding := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if fail.Load() {
			w.WriteHeader(400)
			w.Write([]byte(`{"error":"fixture model unavailable"}`))
			return
		}
		var req struct {
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		data := []any{}
		for i, text := range req.Input {
			vector := []float64{0, 0, 1}
			if strings.Contains(text, "岗位") {
				vector = []float64{1, 0, 0}
			}
			if strings.Contains(text, "缓存") {
				vector = []float64{0, 1, 0}
			}
			data = append(data, map[string]any{"index": i, "embedding": vector})
		}
		json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	defer embedding.Close()
	dir := t.TempDir()
	s := NewAppService(nil, NewAppStateStore(filepath.Join(dir, "state.json")), nil, model.ServerConfig{UploadDir: dir})
	defer s.Close()
	s.state.Config.Embedding = model.EmbeddingConfig{Provider: "openai-compatible", BaseURL: embedding.URL, Model: "fixture"}
	s.qdrant = NewQdrantService(model.ServerConfig{QdrantURL: endpoint, QdrantVectorSize: 3, QdrantCollectionPrefix: fmt.Sprintf("recall_test_%d_", time.Now().UnixNano())})
	kbID := s.ListKnowledgeBases()[0].ID
	if err := s.qdrant.EnsureCollection(t.Context(), kbID); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := s.qdrant.doJSON(context.Background(), http.MethodDelete, "/collections/"+s.qdrant.CollectionName(kbID), nil); err != nil {
			t.Error(err)
		}
	}()
	var first model.Document
	for i, content := range []string{"员工的部门和岗位信息来自员工任职记录。", "Redis 缓存过期后重新加载。"} {
		id := fmt.Sprintf("doc-%d", i)
		path := filepath.Join(dir, id+".md")
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		doc := s.AddDocument(kbID, model.Document{ID: id, KnowledgeBaseID: kbID, Path: path, Name: id + ".md", Status: "indexed", ContentChecksum: hashDocumentContent(content)})
		// 注入合法但过时的缓存，重建必须请求模型而非复用它。
		cfg := s.currentEmbeddingConfig()
		s.rag.cache.Set(embeddingCacheKey(cfg.Provider, cfg.BaseURL, cfg.Model, content), []float64{0, 0, 1})
		indexed, err := s.ReindexDocument(kbID, doc.ID)
		if err != nil || indexed.Status != "indexed" {
			t.Fatalf("rebuild: %+v %v", indexed, err)
		}
		if i == 0 {
			first = indexed
		}
	}
	if calls.Load() != 2 {
		t.Fatalf("rebuild did not call actual embedding endpoint: %d", calls.Load())
	}
	body, err := s.qdrant.doJSON(t.Context(), http.MethodPost, "/collections/"+s.qdrant.CollectionName(kbID)+"/points/scroll", map[string]any{"limit": 10, "with_vector": true, "with_payload": true})
	if err != nil {
		t.Fatal(err)
	}
	var readback struct {
		Result struct {
			Points []struct {
				Vector map[string]json.RawMessage `json:"vector"`
			} `json:"points"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &readback); err != nil || len(readback.Result.Points) != 2 {
		t.Fatalf("readback: %s %v", body, err)
	}
	for _, point := range readback.Result.Points {
		var vector []float64
		if err := json.Unmarshal(point.Vector[qdrantDenseVectorName], &vector); err != nil {
			t.Fatal(err)
		}
		if len(vector) != 3 || vector[2] != 0 || vector[0]+vector[1] != 1 {
			t.Fatalf("stored stale or zero vector: %v", vector)
		}
	}
	for query, want := range map[string]int{"员工的部门和岗位信息": 1, "何安敏": 0} {
		got, err := s.DebugRetrieve(model.RetrievalDebugRequest{KnowledgeBaseID: kbID, Query: query})
		if err != nil || got.Count != want {
			t.Fatalf("debug %q: %+v %v", query, got, err)
		}
	}
	fail.Store(true)
	if _, err := s.ReindexDocument(kbID, first.ID); err == nil {
		t.Fatal("failed model reported success")
	}
	stored, err := s.GetDocument(kbID, first.ID)
	if err != nil || stored.IndexGeneration != first.IndexGeneration || stored.Status != "indexed" || stored.IndexError == "" {
		t.Fatalf("failed rebuild lost old index: %+v %v", stored, err)
	}
	fail.Store(false)
	got, err := s.DebugRetrieve(model.RetrievalDebugRequest{KnowledgeBaseID: kbID, Query: "岗位"})
	if err != nil || got.Count != 1 {
		t.Fatalf("old index unavailable: %+v %v", got, err)
	}
	recovered, err := s.ReindexDocument(kbID, first.ID)
	if err != nil || recovered.IndexError != "" || recovered.IndexErrorCode != "" || recovered.IndexGeneration == first.IndexGeneration {
		t.Fatalf("successful retry did not clear failure state: %+v %v", recovered, err)
	}
}
