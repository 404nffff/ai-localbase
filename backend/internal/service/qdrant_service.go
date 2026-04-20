package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"ai-localbase/internal/model"
)

type QdrantService struct {
	baseURL          string
	apiKey           string
	collectionPrefix string
	vectorSize       int
	distance         string
	httpClient       *http.Client
}

type QdrantPoint struct {
	ID      any            `json:"id"`
	Vector  []float64      `json:"vector"`
	Payload map[string]any `json:"payload"`
}

type QdrantSearchResult struct {
	ID      string
	Score   float64
	Payload map[string]any
}

// SearchResult 对外统一的检索返回结构
type SearchResult = QdrantSearchResult

// HybridSearchParams 混合检索参数
type HybridSearchParams struct {
	CollectionName string
	DenseVector    []float32
	SparseVector   SparseVector
	TopK           int
	ScoreThreshold float32
	Filter         interface{}
}

type qdrantCollectionRequest struct {
	Vectors qdrantVectorConfig `json:"vectors"`
}

type qdrantVectorConfig struct {
	Size     int    `json:"size"`
	Distance string `json:"distance"`
}

type qdrantPointUpsertRequest struct {
	Points []QdrantPoint `json:"points"`
}

type qdrantPointDeleteRequest struct {
	Filter map[string]any `json:"filter"`
}

type qdrantSearchRequest struct {
	Vector      []float64      `json:"vector"`
	Limit       int            `json:"limit"`
	Filter      map[string]any `json:"filter,omitempty"`
	WithPayload bool           `json:"with_payload"`
}

type qdrantSearchResponse struct {
	Result []struct {
		ID      any            `json:"id"`
		Score   float64        `json:"score"`
		Payload map[string]any `json:"payload"`
	} `json:"result"`
}

func NewQdrantService(cfg model.ServerConfig) *QdrantService {
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.QdrantURL), "/")
	if baseURL == "" {
		return nil
	}

	timeout := time.Duration(cfg.QdrantTimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 5 * time.Second
	}

	return &QdrantService{
		baseURL:          baseURL,
		apiKey:           strings.TrimSpace(cfg.QdrantAPIKey),
		collectionPrefix: strings.TrimSpace(cfg.QdrantCollectionPrefix),
		vectorSize:       cfg.QdrantVectorSize,
		distance:         normalizeQdrantDistance(cfg.QdrantDistance),
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

func (s *QdrantService) IsEnabled() bool {
	return s != nil && s.baseURL != ""
}

func (s *QdrantService) CollectionName(knowledgeBaseID string) string {
	if s == nil {
		return ""
	}

	if s.collectionPrefix == "" {
		return knowledgeBaseID
	}

	return s.collectionPrefix + knowledgeBaseID
}

func (s *QdrantService) Ping(ctx context.Context) error {
	if !s.IsEnabled() {
		return nil
	}

	_, err := s.doJSON(ctx, http.MethodGet, "/collections", nil)
	return err
}

func (s *QdrantService) EnsureCollection(ctx context.Context, knowledgeBaseID string) error {
	if !s.IsEnabled() {
		return nil
	}

	body := qdrantCollectionRequest{
		Vectors: qdrantVectorConfig{
			Size:     s.vectorSize,
			Distance: s.distance,
		},
	}

	_, err := s.doJSON(ctx, http.MethodPut, "/collections/"+url.PathEscape(s.CollectionName(knowledgeBaseID)), body)
	return err
}

func (s *QdrantService) DeleteCollection(ctx context.Context, knowledgeBaseID string) error {
	if !s.IsEnabled() {
		return nil
	}

	_, err := s.doJSON(ctx, http.MethodDelete, "/collections/"+url.PathEscape(s.CollectionName(knowledgeBaseID)), nil)
	if err != nil && isQdrantNotFound(err) {
		return nil
	}

	return err
}

const qdrantUpsertBatchSize = 100

func (s *QdrantService) UpsertPoints(ctx context.Context, knowledgeBaseID string, points []QdrantPoint) error {
	if !s.IsEnabled() || len(points) == 0 {
		return nil
	}

	collPath := "/collections/" + url.PathEscape(s.CollectionName(knowledgeBaseID)) + "/points"
	for i := 0; i < len(points); i += qdrantUpsertBatchSize {
		end := i + qdrantUpsertBatchSize
		if end > len(points) {
			end = len(points)
		}
		batch := points[i:end]
		if _, err := s.doJSON(ctx, http.MethodPut, collPath, qdrantPointUpsertRequest{Points: batch}); err != nil {
			return fmt.Errorf("upsert batch [%d:%d]: %w", i, end, err)
		}
	}
	return nil
}

// DeletePointsByDocumentID 按文档 ID 清理旧的向量点，避免文档重建索引后残留过期 chunk。
func (s *QdrantService) DeletePointsByDocumentID(ctx context.Context, knowledgeBaseID, documentID string) error {
	if !s.IsEnabled() || strings.TrimSpace(documentID) == "" {
		return nil
	}

	filter := map[string]any{
		"must": []map[string]any{{
			"key": "document_id",
			"match": map[string]any{
				"value": documentID,
			},
		}},
	}
	_, err := s.doJSON(
		ctx,
		http.MethodPost,
		"/collections/"+url.PathEscape(s.CollectionName(knowledgeBaseID))+"/points/delete",
		qdrantPointDeleteRequest{Filter: filter},
	)
	return err
}

func (s *QdrantService) Search(ctx context.Context, knowledgeBaseID string, vector []float64, limit int, filter map[string]any) ([]QdrantSearchResult, error) {
	if !s.IsEnabled() || len(vector) == 0 {
		return nil, nil
	}
	if limit <= 0 {
		limit = 5
	}

	body := qdrantSearchRequest{
		Vector:      vector,
		Limit:       limit,
		Filter:      filter,
		WithPayload: true,
	}

	var responseBody []byte
	err := retryWithBackoff(ctx, 3, 200*time.Millisecond, func() error {
		var err error
		responseBody, err = s.doJSON(ctx, http.MethodPost, "/collections/"+url.PathEscape(s.CollectionName(knowledgeBaseID))+"/points/search", body)
		return err
	})
	if err != nil {
		return nil, err
	}

	var response qdrantSearchResponse
	if err := json.Unmarshal(responseBody, &response); err != nil {
		return nil, fmt.Errorf("decode qdrant search response: %w", err)
	}

	results := make([]QdrantSearchResult, 0, len(response.Result))
	for _, item := range response.Result {
		results = append(results, QdrantSearchResult{
			ID:      fmt.Sprint(item.ID),
			Score:   item.Score,
			Payload: item.Payload,
		})
	}

	return results, nil
}

// SearchHybrid 执行混合检索（dense + sparse），使用 RRF 融合
// 内部并行执行两路检索，然后用 RRF 合并排名
func (s *QdrantService) SearchHybrid(ctx context.Context, params HybridSearchParams) ([]SearchResult, error) {
	if !s.IsEnabled() || len(params.DenseVector) == 0 {
		return nil, nil
	}

	topK := params.TopK
	if topK <= 0 {
		topK = 5
	}

	var filter map[string]any
	if params.Filter != nil {
		if typed, ok := params.Filter.(map[string]any); ok {
			filter = typed
		}
	}

	denseVector := float32ToFloat64(params.DenseVector)

	var denseResults []SearchResult
	var sparseResults []SearchResult
	var denseErr error
	var sparseErr error

	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		results, err := s.Search(ctx, params.CollectionName, denseVector, topK, filter)
		if err != nil {
			denseErr = err
			return
		}
		denseResults = applyScoreThreshold(results, float64(params.ScoreThreshold))
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		fallbackVector := sparseFallbackVector(params.SparseVector, len(denseVector))
		if len(fallbackVector) == 0 {
			return
		}
		// TODO: 升级 Qdrant SDK 后替换为真实 sparse vector search
		results, err := s.Search(ctx, params.CollectionName, fallbackVector, topK, filter)
		if err != nil {
			sparseErr = err
			return
		}
		sparseResults = applyScoreThreshold(results, float64(params.ScoreThreshold))
	}()

	wg.Wait()
	if denseErr != nil {
		return nil, denseErr
	}
	if sparseErr != nil {
		return nil, sparseErr
	}

	return rrfFusion(denseResults, sparseResults, topK), nil
}

// rrfFusion 使用 RRF 融合
func rrfFusion(denseResults []SearchResult, sparseResults []SearchResult, topK int) []SearchResult {
	const k = 60.0
	if topK <= 0 {
		topK = 5
	}

	scores := make(map[string]float64)
	payloads := make(map[string]map[string]any)
	addResults := func(results []SearchResult) {
		for idx, item := range results {
			rank := float64(idx + 1)
			scores[item.ID] += 1.0 / (k + rank)
			if _, ok := payloads[item.ID]; !ok {
				payloads[item.ID] = item.Payload
			}
		}
	}

	addResults(denseResults)
	addResults(sparseResults)

	merged := make([]SearchResult, 0, len(scores))
	for id, score := range scores {
		merged = append(merged, SearchResult{
			ID:      id,
			Score:   score,
			Payload: payloads[id],
		})
	}

	sort.Slice(merged, func(i, j int) bool {
		if merged[i].Score == merged[j].Score {
			return merged[i].ID < merged[j].ID
		}
		return merged[i].Score > merged[j].Score
	})

	if len(merged) > topK {
		return merged[:topK]
	}
	return merged
}

func applyScoreThreshold(results []SearchResult, threshold float64) []SearchResult {
	if threshold <= 0 {
		return results
	}
	filtered := make([]SearchResult, 0, len(results))
	for _, item := range results {
		if item.Score >= threshold {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func float32ToFloat64(input []float32) []float64 {
	if len(input) == 0 {
		return nil
	}
	output := make([]float64, len(input))
	for i, value := range input {
		output[i] = float64(value)
	}
	return output
}

func sparseFallbackVector(vector SparseVector, vectorSize int) []float64 {
	if vectorSize <= 0 || len(vector.Indices) == 0 || len(vector.Values) == 0 {
		return nil
	}
	fallback := make([]float64, vectorSize)
	for i, idx := range vector.Indices {
		if i >= len(vector.Values) {
			break
		}
		pos := int(idx % uint32(vectorSize))
		fallback[pos] += float64(vector.Values[i])
	}
	normalizeVector(fallback)
	return fallback
}

func (s *QdrantService) doJSON(ctx context.Context, method, requestPath string, payload any) ([]byte, error) {
	if s == nil {
		return nil, fmt.Errorf("qdrant service is not initialized")
	}

	requestURL, err := url.Parse(s.baseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid qdrant base url: %w", err)
	}
	requestURL.Path = path.Join(requestURL.Path, requestPath)

	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("marshal qdrant payload: %w", err)
		}
		body = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, requestURL.String(), body)
	if err != nil {
		return nil, fmt.Errorf("build qdrant request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if s.apiKey != "" {
		req.Header.Set("api-key", s.apiKey)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request qdrant: %w", err)
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read qdrant response: %w", err)
	}

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, &qdrantRequestError{
			StatusCode: resp.StatusCode,
			Body:       strings.TrimSpace(string(responseBody)),
		}
	}

	return responseBody, nil
}

func normalizeQdrantDistance(distance string) string {
	switch strings.ToLower(strings.TrimSpace(distance)) {
	case "dot":
		return "Dot"
	case "euclid":
		return "Euclid"
	case "manhattan":
		return "Manhattan"
	default:
		return "Cosine"
	}
}

func isQdrantNotFound(err error) bool {
	requestErr, ok := err.(*qdrantRequestError)
	return ok && requestErr.StatusCode == http.StatusNotFound
}

type qdrantRequestError struct {
	StatusCode int
	Body       string
}

func (e *qdrantRequestError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("qdrant request failed with status %d", e.StatusCode)
	}

	return fmt.Sprintf("qdrant request failed with status %d: %s", e.StatusCode, e.Body)
}
