package service

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"

	"ai-localbase/internal/model"
)

// SemanticReranker 对候选 chunks 按与 query 的语义相关度重新排序。
type SemanticReranker interface {
	Rerank(ctx context.Context, query string, chunks []RetrievedChunk) ([]RetrievedChunk, error)
}

// KeywordReranker 是默认重排策略，融合向量分、关键词覆盖和结构化片段加权。
type KeywordReranker struct{}

func (KeywordReranker) Rerank(_ context.Context, query string, candidates []RetrievedChunk) ([]RetrievedChunk, error) {
	if len(candidates) == 0 {
		return nil, nil
	}

	ranked := make([]RetrievedChunk, len(candidates))
	copy(ranked, candidates)

	minScore, maxScore := ranked[0].Score, ranked[0].Score
	for _, item := range ranked {
		if item.Score < minScore {
			minScore = item.Score
		}
		if item.Score > maxScore {
			maxScore = item.Score
		}
	}

	for i := range ranked {
		vectorScore := normalizeScore(ranked[i].Score, minScore, maxScore)
		keywordScore := keywordCoverage(query, ranked[i].Text)
		ranked[i].Score = rerankVectorWeight*vectorScore + rerankKeywordWeight*keywordScore + scoreBoost(ranked[i].Text)
	}

	sortRetrievedChunks(ranked)
	return ranked, nil
}

// EmbeddingReranker 基于 embedding cosine similarity 重排。
type EmbeddingReranker struct {
	ragSvc          *RagService
	embeddingConfig func() model.EmbeddingModelConfig
	vectorSize      func() int
	embed           func(ctx context.Context, cfg model.EmbeddingModelConfig, texts []string, vectorSize int) ([][]float64, error)
}

func NewEmbeddingReranker(ragSvc *RagService) *EmbeddingReranker {
	return &EmbeddingReranker{ragSvc: ragSvc}
}

func (r *EmbeddingReranker) SetEmbeddingConfigProvider(provider func() model.EmbeddingModelConfig) {
	r.embeddingConfig = provider
}

func (r *EmbeddingReranker) SetVectorSizeProvider(provider func() int) {
	r.vectorSize = provider
}

func (r *EmbeddingReranker) Rerank(ctx context.Context, query string, chunks []RetrievedChunk) ([]RetrievedChunk, error) {
	if len(chunks) == 0 {
		return nil, nil
	}
	if r == nil {
		return nil, fmt.Errorf("embedding reranker is nil")
	}

	cfg := model.EmbeddingModelConfig{}
	if r.embeddingConfig != nil {
		cfg = r.embeddingConfig()
	}
	vectorSize := 0
	if r.vectorSize != nil {
		vectorSize = r.vectorSize()
	}

	embed := r.embed
	if embed == nil {
		if r.ragSvc == nil {
			return nil, fmt.Errorf("rag service is nil")
		}
		embed = r.ragSvc.EmbedTexts
	}

	queryVectors, err := embed(ctx, cfg, []string{query}, vectorSize)
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}
	if len(queryVectors) == 0 {
		return nil, fmt.Errorf("empty query embedding")
	}

	texts := make([]string, 0, len(chunks))
	for _, chunk := range chunks {
		texts = append(texts, chunk.Text)
	}
	chunkVectors, err := embed(ctx, cfg, texts, vectorSize)
	if err != nil {
		return nil, fmt.Errorf("embed chunks: %w", err)
	}
	if len(chunkVectors) != len(chunks) {
		return nil, fmt.Errorf("embedding size mismatch: %d != %d", len(chunkVectors), len(chunks))
	}

	queryVec := float64ToFloat32(queryVectors[0])
	ranked := make([]RetrievedChunk, len(chunks))
	copy(ranked, chunks)
	for i := range ranked {
		chunkVec := float64ToFloat32(chunkVectors[i])
		similarity := cosineSimilarity(queryVec, chunkVec)
		ranked[i].Score = float64(similarity)
	}

	sortRetrievedChunks(ranked)
	return ranked, nil
}

// LLMReranker 基于 LLM 对每个候选打相关度分。
type LLMReranker struct {
	llmSvc     *LLMService
	chatConfig func() model.ChatModelConfig
}

func (r *LLMReranker) SetChatConfigProvider(provider func() model.ChatModelConfig) {
	r.chatConfig = provider
}

func (r *LLMReranker) Rerank(ctx context.Context, query string, chunks []RetrievedChunk) ([]RetrievedChunk, error) {
	if len(chunks) == 0 {
		return nil, nil
	}
	if r == nil || r.llmSvc == nil {
		return nil, fmt.Errorf("llm service is nil")
	}

	config := model.ChatModelConfig{}
	if r.chatConfig != nil {
		config = r.chatConfig()
	}
	if strings.TrimSpace(config.Model) == "" {
		return nil, fmt.Errorf("chat model config is empty")
	}

	scores := make([]float64, len(chunks))
	sem := make(chan struct{}, 3)
	var wg sync.WaitGroup
	errChan := make(chan error, len(chunks))

	for i, chunk := range chunks {
		wg.Add(1)
		go func(idx int, text string) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				errChan <- ctx.Err()
				return
			}

			prompt := fmt.Sprintf("请评估以下文档与问题的相关度，返回0-10的整数分数。\n问题：%s\n文档：%s\n分数：", query, text)
			resp, err := r.llmSvc.Chat(model.ChatCompletionRequest{
				Messages: []model.ChatMessage{{Role: "user", Content: prompt}},
				Config:   config,
			})
			if err != nil {
				errChan <- err
				return
			}
			if len(resp.Choices) == 0 {
				errChan <- fmt.Errorf("empty llm response")
				return
			}
			score, err := parseLLMScore(resp.Choices[0].Message.Content)
			if err != nil {
				errChan <- err
				return
			}
			scores[idx] = score
		}(i, chunk.Text)
	}

	wg.Wait()
	close(errChan)
	for err := range errChan {
		if err != nil {
			return nil, err
		}
	}

	ranked := make([]RetrievedChunk, len(chunks))
	copy(ranked, chunks)
	for i := range ranked {
		ranked[i].Score = scores[i]
	}

	sortRetrievedChunks(ranked)
	return ranked, nil
}

func sortRetrievedChunks(chunks []RetrievedChunk) {
	sort.Slice(chunks, func(i, j int) bool {
		if chunks[i].Score == chunks[j].Score {
			if chunks[i].DocumentID == chunks[j].DocumentID {
				return chunks[i].Index < chunks[j].Index
			}
			return chunks[i].DocumentID < chunks[j].DocumentID
		}
		return chunks[i].Score > chunks[j].Score
	})
}

func cosineSimilarity(a, b []float32) float32 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return float32(dot / (math.Sqrt(normA) * math.Sqrt(normB)))
}

func parseLLMScore(content string) (float64, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return 0, fmt.Errorf("empty llm score")
	}

	num := strings.Builder{}
	for _, r := range content {
		if (r >= '0' && r <= '9') || r == '.' {
			num.WriteRune(r)
			continue
		}
		if num.Len() == 0 {
			continue
		}
		break
	}
	if num.Len() == 0 {
		return 0, fmt.Errorf("no numeric score in llm response")
	}
	score, err := strconv.ParseFloat(num.String(), 64)
	if err != nil {
		return 0, fmt.Errorf("parse llm score: %w", err)
	}
	if score < 0 {
		score = 0
	}
	if score > 10 {
		score = 10
	}
	return score, nil
}
