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
	writeHTTPClient  *http.Client
}

type QdrantPoint struct {
	ID           any            `json:"id"`
	Vector       any            `json:"vector"`
	SparseVector SparseVector   `json:"-"`
	Payload      map[string]any `json:"payload"`
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
	Vectors       any                                 `json:"vectors"`
	SparseVectors map[string]qdrantSparseVectorConfig `json:"sparse_vectors,omitempty"`
}

type qdrantCollectionInfoResponse struct {
	Result struct {
		Config struct {
			Params struct {
				Vectors       json.RawMessage            `json:"vectors"`
				SparseVectors map[string]json.RawMessage `json:"sparse_vectors"`
			} `json:"params"`
		} `json:"config"`
	} `json:"result"`
}

type qdrantCollectionsResponse struct {
	Result struct {
		Collections []struct {
			Name string `json:"name"`
		} `json:"collections"`
	} `json:"result"`
}

type qdrantVectorConfig struct {
	Size     int    `json:"size"`
	Distance string `json:"distance"`
}

type qdrantSparseVectorConfig struct{}

type qdrantCollectionMode string

const (
	qdrantCollectionLegacyUnnamed qdrantCollectionMode = "legacy_unnamed"
	qdrantCollectionNamed         qdrantCollectionMode = "named"
)

type qdrantCollectionSchema struct {
	Mode      qdrantCollectionMode
	Dense     qdrantVectorConfig
	HasSparse bool
}

type qdrantPointUpsertRequest struct {
	Points []QdrantPoint `json:"points"`
}

type qdrantPointDeleteRequest struct {
	Filter map[string]any `json:"filter"`
}

type qdrantSearchRequest struct {
	Vector      any            `json:"vector"`
	Limit       int            `json:"limit"`
	Filter      map[string]any `json:"filter,omitempty"`
	WithPayload bool           `json:"with_payload"`
}

type qdrantQueryRequest struct {
	Query       any            `json:"query"`
	Using       string         `json:"using,omitempty"`
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

// Qdrant 的 search 与 query 响应分别返回数组和 points 包装，统一解码供上层复用。
func (r *qdrantSearchResponse) UnmarshalJSON(data []byte) error {
	var raw struct {
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if len(raw.Result) == 0 || string(raw.Result) == "null" {
		r.Result = nil
		return nil
	}

	var points []struct {
		ID      any            `json:"id"`
		Score   float64        `json:"score"`
		Payload map[string]any `json:"payload"`
	}
	if err := json.Unmarshal(raw.Result, &points); err == nil {
		r.Result = points
		return nil
	}

	var queryResult struct {
		Points []struct {
			ID      any            `json:"id"`
			Score   float64        `json:"score"`
			Payload map[string]any `json:"payload"`
		} `json:"points"`
	}
	if err := json.Unmarshal(raw.Result, &queryResult); err != nil {
		return err
	}
	r.Result = queryResult.Points
	return nil
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
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          16,
		MaxIdleConnsPerHost:   4,
		IdleConnTimeout:       60 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	return &QdrantService{
		baseURL:          baseURL,
		apiKey:           strings.TrimSpace(cfg.QdrantAPIKey),
		collectionPrefix: strings.TrimSpace(cfg.QdrantCollectionPrefix),
		vectorSize:       cfg.QdrantVectorSize,
		distance:         normalizeQdrantDistance(cfg.QdrantDistance),
		httpClient: &http.Client{
			Timeout:   timeout,
			Transport: transport,
		},
		writeHTTPClient: &http.Client{
			Transport: transport,
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

	collectionName := s.CollectionName(knowledgeBaseID)
	if existing, err := s.getCollectionSchema(ctx, collectionName); err == nil {
		return s.validateCollectionSchema(collectionName, existing)
	} else if !isQdrantNotFound(err) {
		return err
	}

	body := qdrantCollectionRequest{
		Vectors: map[string]qdrantVectorConfig{
			qdrantDenseVectorName: {
				Size:     s.vectorSize,
				Distance: s.distance,
			},
		},
		SparseVectors: map[string]qdrantSparseVectorConfig{
			qdrantSparseVectorName: {},
		},
	}

	_, err := s.doJSON(ctx, http.MethodPut, "/collections/"+url.PathEscape(collectionName), body)
	if err != nil && isQdrantConflict(err) {
		existing, getErr := s.getCollectionSchema(ctx, collectionName)
		if getErr != nil {
			return getErr
		}
		return s.validateCollectionSchema(collectionName, existing)
	}
	return err
}

// getCollectionSchema 识别历史 unnamed dense 与 named dense/sparse，禁止通过删除集合解决差异。
func (s *QdrantService) getCollectionSchema(ctx context.Context, collectionName string) (qdrantCollectionSchema, error) {
	responseBody, err := s.doJSON(ctx, http.MethodGet, "/collections/"+url.PathEscape(collectionName), nil)
	if err != nil {
		return qdrantCollectionSchema{}, err
	}

	var response qdrantCollectionInfoResponse
	if err := json.Unmarshal(responseBody, &response); err != nil {
		return qdrantCollectionSchema{}, fmt.Errorf("decode qdrant collection info: %w", err)
	}

	var legacy qdrantVectorConfig
	if err := json.Unmarshal(response.Result.Config.Params.Vectors, &legacy); err == nil && legacy.Size > 0 {
		return qdrantCollectionSchema{
			Mode:  qdrantCollectionLegacyUnnamed,
			Dense: legacy,
		}, nil
	}

	var named map[string]qdrantVectorConfig
	if err := json.Unmarshal(response.Result.Config.Params.Vectors, &named); err != nil {
		return qdrantCollectionSchema{}, fmt.Errorf("decode qdrant vector schema: %w", err)
	}
	dense, ok := named[qdrantDenseVectorName]
	if !ok {
		return qdrantCollectionSchema{}, fmt.Errorf("qdrant named collection %s is missing dense vector", collectionName)
	}
	_, hasSparse := response.Result.Config.Params.SparseVectors[qdrantSparseVectorName]
	return qdrantCollectionSchema{
		Mode:      qdrantCollectionNamed,
		Dense:     dense,
		HasSparse: hasSparse,
	}, nil
}

func (s *QdrantService) validateCollectionSchema(collectionName string, existing qdrantCollectionSchema) error {
	expectedDistance := normalizeQdrantDistance(s.distance)
	actualDistance := normalizeQdrantDistance(existing.Dense.Distance)
	if existing.Dense.Size != s.vectorSize || actualDistance != expectedDistance {
		return fmt.Errorf(
			"qdrant collection schema mismatch for %s: expected vector size %d distance %s, got size %d distance %s",
			collectionName,
			s.vectorSize,
			expectedDistance,
			existing.Dense.Size,
			actualDistance,
		)
	}
	return nil
}

func (s *QdrantService) DeleteCollection(ctx context.Context, knowledgeBaseID string) error {
	if !s.IsEnabled() {
		return nil
	}

	return s.DeleteCollectionByName(ctx, s.CollectionName(knowledgeBaseID))
}

// ListCollections 返回当前 Qdrant 实例中的 collection 名称，用于受控恢复时清理旧索引。
func (s *QdrantService) ListCollections(ctx context.Context) ([]string, error) {
	if !s.IsEnabled() {
		return nil, nil
	}

	responseBody, err := s.doJSON(ctx, http.MethodGet, "/collections", nil)
	if err != nil {
		return nil, err
	}

	var response qdrantCollectionsResponse
	if err := json.Unmarshal(responseBody, &response); err != nil {
		return nil, fmt.Errorf("decode qdrant collections response: %w", err)
	}

	collections := make([]string, 0, len(response.Result.Collections))
	for _, collection := range response.Result.Collections {
		name := strings.TrimSpace(collection.Name)
		if name == "" {
			continue
		}
		collections = append(collections, name)
	}
	sort.Strings(collections)
	return collections, nil
}

// DeleteCollectionByName 按真实 collection 名称删除，避免恢复流程重复拼接前缀。
func (s *QdrantService) DeleteCollectionByName(ctx context.Context, collectionName string) error {
	if !s.IsEnabled() {
		return nil
	}

	collectionName = strings.TrimSpace(collectionName)
	if collectionName == "" {
		return nil
	}

	_, err := s.doJSON(ctx, http.MethodDelete, "/collections/"+url.PathEscape(collectionName), nil)
	if err != nil && isQdrantNotFound(err) {
		return nil
	}

	return err
}

const (
	qdrantUpsertBatchSize          = 100
	qdrantDenseVectorName          = "dense"
	qdrantSparseVectorName         = "sparse"
	qdrantPayloadRetrievalChannels = "_retrieval_channels"
	qdrantPayloadDenseRank         = "_dense_rank"
	qdrantPayloadSparseRank        = "_sparse_rank"
)

func (s *QdrantService) UpsertPoints(ctx context.Context, knowledgeBaseID string, points []QdrantPoint) error {
	if !s.IsEnabled() || len(points) == 0 {
		return nil
	}

	collectionName := s.CollectionName(knowledgeBaseID)
	schema, err := s.getCollectionSchema(ctx, collectionName)
	if err != nil {
		return fmt.Errorf("read qdrant collection schema: %w", err)
	}
	if err := s.validateCollectionSchema(collectionName, schema); err != nil {
		return err
	}

	collPath := "/collections/" + url.PathEscape(collectionName) + "/points"
	for i := 0; i < len(points); i += qdrantUpsertBatchSize {
		end := i + qdrantUpsertBatchSize
		if end > len(points) {
			end = len(points)
		}
		batch, err := prepareQdrantPoints(points[i:end], schema)
		if err != nil {
			return fmt.Errorf("prepare upsert batch [%d:%d]: %w", i, end, err)
		}
		if _, err := s.doJSONWithClient(ctx, s.writeHTTPClient, http.MethodPut, collPath, qdrantPointUpsertRequest{Points: batch}); err != nil {
			return fmt.Errorf("upsert batch [%d:%d]: %w", i, end, err)
		}
	}
	return nil
}

// prepareQdrantPoints 根据真实集合模式生成 wire payload，避免先失败再猜测 legacy。
func prepareQdrantPoints(points []QdrantPoint, schema qdrantCollectionSchema) ([]QdrantPoint, error) {
	prepared := make([]QdrantPoint, 0, len(points))
	for _, point := range points {
		dense := extractDenseVector(point.Vector)
		if len(dense) == 0 {
			return nil, fmt.Errorf("point %v is missing dense vector", point.ID)
		}

		next := point
		if schema.Mode == qdrantCollectionLegacyUnnamed {
			next.Vector = dense
		} else {
			next.Vector = qdrantPointVectors(dense, point.SparseVector, schema.HasSparse)
		}
		prepared = append(prepared, next)
	}
	return prepared, nil
}

func qdrantPointVectors(dense []float64, sparse SparseVector, includeSparse bool) map[string]any {
	vectors := map[string]any{qdrantDenseVectorName: dense}
	if includeSparse && len(sparse.Indices) > 0 && len(sparse.Values) > 0 {
		vectors[qdrantSparseVectorName] = map[string]any{
			"indices": sparse.Indices,
			"values":  sparse.Values,
		}
	}
	return vectors
}

func extractDenseVector(vector any) []float64 {
	switch typed := vector.(type) {
	case []float64:
		return typed
	case map[string]any:
		if dense, ok := typed[qdrantDenseVectorName].([]float64); ok {
			return dense
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

	results, err := s.queryWithBody(ctx, knowledgeBaseID, qdrantQueryRequest{
		Query:       vector,
		Using:       qdrantDenseVectorName,
		Limit:       limit,
		Filter:      filter,
		WithPayload: true,
	})
	if err == nil {
		return results, nil
	}
	if !isQdrantCompatibilityError(err) {
		return nil, err
	}

	return s.searchWithBody(ctx, knowledgeBaseID, qdrantSearchRequest{
		Vector:      vector,
		Limit:       limit,
		Filter:      filter,
		WithPayload: true,
	})
}

func (s *QdrantService) querySparse(ctx context.Context, knowledgeBaseID string, vector SparseVector, limit int, filter map[string]any) ([]QdrantSearchResult, error) {
	if len(vector.Indices) == 0 || len(vector.Values) == 0 {
		return nil, nil
	}
	return s.queryWithBody(ctx, knowledgeBaseID, qdrantQueryRequest{
		Query: map[string]any{
			"indices": vector.Indices,
			"values":  vector.Values,
		},
		Using:       qdrantSparseVectorName,
		Limit:       limit,
		Filter:      filter,
		WithPayload: true,
	})
}

func (s *QdrantService) searchWithBody(ctx context.Context, knowledgeBaseID string, body qdrantSearchRequest) ([]QdrantSearchResult, error) {
	var responseBody []byte
	err := retryWithBackoff(ctx, 3, 200*time.Millisecond, func() error {
		var err error
		responseBody, err = s.doJSON(ctx, http.MethodPost, "/collections/"+url.PathEscape(s.CollectionName(knowledgeBaseID))+"/points/search", body)
		return err
	})
	if err != nil {
		return nil, err
	}
	return decodeQdrantSearchResults(responseBody)
}

func (s *QdrantService) queryWithBody(ctx context.Context, knowledgeBaseID string, body qdrantQueryRequest) ([]QdrantSearchResult, error) {
	var responseBody []byte
	err := retryWithBackoff(ctx, 3, 200*time.Millisecond, func() error {
		var err error
		responseBody, err = s.doJSON(ctx, http.MethodPost, "/collections/"+url.PathEscape(s.CollectionName(knowledgeBaseID))+"/points/query", body)
		return err
	})
	if err != nil {
		return nil, err
	}
	return decodeQdrantSearchResults(responseBody)
}

func decodeQdrantSearchResults(responseBody []byte) ([]QdrantSearchResult, error) {
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
	searchLimit := minInt(topK*2, 64)
	if searchLimit < topK {
		searchLimit = topK
	}

	var denseResults []SearchResult
	var sparseResults []SearchResult
	var denseErr error
	var sparseErr error

	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		results, err := s.Search(ctx, params.CollectionName, denseVector, searchLimit, filter)
		if err != nil {
			denseErr = err
			return
		}
		denseResults = applyScoreThreshold(results, float64(params.ScoreThreshold))
	}()

	if len(params.SparseVector.Indices) > 0 && len(params.SparseVector.Values) > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results, err := s.querySparse(ctx, params.CollectionName, params.SparseVector, searchLimit, filter)
			if err != nil {
				sparseErr = err
				return
			}
			sparseResults = applyScoreThreshold(results, float64(params.ScoreThreshold))
		}()
	}

	wg.Wait()
	if denseErr != nil {
		return nil, denseErr
	}
	if sparseErr != nil {
		return denseResults, nil
	}
	if len(sparseResults) == 0 {
		return denseResults, nil
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
	denseRanks := make(map[string]int)
	sparseRanks := make(map[string]int)
	channels := make(map[string]map[string]struct{})
	addResults := func(results []SearchResult, channel string, ranks map[string]int) {
		for idx, item := range results {
			rank := float64(idx + 1)
			scores[item.ID] += 1.0 / (k + rank)
			ranks[item.ID] = idx + 1
			if _, ok := channels[item.ID]; !ok {
				channels[item.ID] = make(map[string]struct{})
			}
			channels[item.ID][channel] = struct{}{}
			if _, ok := payloads[item.ID]; !ok {
				payloads[item.ID] = clonePayload(item.Payload)
			}
		}
	}

	addResults(denseResults, qdrantDenseVectorName, denseRanks)
	addResults(sparseResults, qdrantSparseVectorName, sparseRanks)

	merged := make([]SearchResult, 0, len(scores))
	for id, score := range scores {
		payload := payloads[id]
		payload[qdrantPayloadRetrievalChannels] = sortedChannelList(channels[id])
		payload[qdrantPayloadDenseRank] = denseRanks[id]
		payload[qdrantPayloadSparseRank] = sparseRanks[id]
		merged = append(merged, SearchResult{
			ID:      id,
			Score:   score,
			Payload: payload,
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

func clonePayload(payload map[string]any) map[string]any {
	cloned := make(map[string]any, len(payload)+3)
	for key, value := range payload {
		cloned[key] = value
	}
	return cloned
}

func sortedChannelList(channels map[string]struct{}) []string {
	result := make([]string, 0, len(channels))
	for channel := range channels {
		result = append(result, channel)
	}
	sort.Strings(result)
	return result
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

func (s *QdrantService) doJSON(ctx context.Context, method, requestPath string, payload any) ([]byte, error) {
	return s.doJSONWithClient(ctx, s.httpClient, method, requestPath, payload)
}

func (s *QdrantService) doJSONWithClient(ctx context.Context, client *http.Client, method, requestPath string, payload any) ([]byte, error) {
	if s == nil {
		return nil, fmt.Errorf("qdrant service is not initialized")
	}
	if client == nil {
		return nil, fmt.Errorf("qdrant http client is not initialized")
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

	resp, err := client.Do(req)
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

func isQdrantConflict(err error) bool {
	requestErr, ok := err.(*qdrantRequestError)
	return ok && requestErr.StatusCode == http.StatusConflict
}

func isQdrantCompatibilityError(err error) bool {
	requestErr, ok := err.(*qdrantRequestError)
	if !ok {
		return false
	}
	return requestErr.StatusCode == http.StatusBadRequest ||
		requestErr.StatusCode == http.StatusNotFound ||
		requestErr.StatusCode == http.StatusUnprocessableEntity
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
