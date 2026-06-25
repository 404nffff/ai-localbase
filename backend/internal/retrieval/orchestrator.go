package retrieval

import (
	"context"
	"fmt"
)

// Orchestrator 负责协调整个检索流程
type Orchestrator struct {
	vectorStore  VectorStore
	reranker     Reranker
	evidenceGate EvidenceGate
	config       Config
}

// Config 检索配置
type Config struct {
	// 候选召回配置
	CandidateTopK         int
	CandidateTopKAllDocs  int

	// 最终输出配置
	FinalTopK             int
	MaxChunksPerDocument  int
	MaxContextChars       int

	// 功能开关
	EnableHybridSearch    bool
	EnableSemanticRerank  bool
	EnableQueryRewrite    bool
	EnableAutoExpand      bool
}

// VectorStore 向量存储接口
type VectorStore interface {
	Search(ctx context.Context, vector []float64, limit int, filter map[string]interface{}) ([]Chunk, error)
	HybridSearch(ctx context.Context, query string, vector []float64, limit int, filter map[string]interface{}) ([]Chunk, error)
}

// Reranker 重排序接口
type Reranker interface {
	Rerank(ctx context.Context, query string, chunks []Chunk) ([]Chunk, error)
}

// EvidenceGate 证据门控接口
type EvidenceGate interface {
	Filter(ctx context.Context, query string, chunks []Chunk) ([]Chunk, error)
}

// Chunk 检索到的文档块
type Chunk struct {
	ID              string
	Content         string
	Score           float64
	KnowledgeBaseID string
	DocumentID      string
	Metadata        map[string]interface{}
}

// NewOrchestrator 创建检索编排器
func NewOrchestrator(vectorStore VectorStore, reranker Reranker, evidenceGate EvidenceGate, config Config) *Orchestrator {
	return &Orchestrator{
		vectorStore:  vectorStore,
		reranker:     reranker,
		evidenceGate: evidenceGate,
		config:       config,
	}
}

// Retrieve 执行完整的检索流程
func (o *Orchestrator) Retrieve(ctx context.Context, query string, queryVector []float64, filter map[string]interface{}) ([]Chunk, error) {
	// Stage 1: 召回候选
	candidates, err := o.recallCandidates(ctx, query, queryVector, filter)
	if err != nil {
		return nil, fmt.Errorf("recall candidates: %w", err)
	}

	if len(candidates) == 0 {
		return []Chunk{}, nil
	}

	// Stage 2: 重排序（如果启用）
	reranked := candidates
	if o.config.EnableSemanticRerank && o.reranker != nil {
		reranked, err = o.reranker.Rerank(ctx, query, candidates)
		if err != nil {
			return nil, fmt.Errorf("rerank: %w", err)
		}
	}

	// Stage 3: MMR 选择（多样性）
	selected := o.selectWithMMR(reranked, o.config.FinalTopK, o.config.MaxChunksPerDocument)

	// Stage 4: 证据门控（相关性过滤）
	if o.evidenceGate != nil {
		selected, err = o.evidenceGate.Filter(ctx, query, selected)
		if err != nil {
			return nil, fmt.Errorf("evidence gate: %w", err)
		}
	}

	return selected, nil
}

// recallCandidates 召回候选文档块
func (o *Orchestrator) recallCandidates(ctx context.Context, query string, queryVector []float64, filter map[string]interface{}) ([]Chunk, error) {
	limit := o.config.CandidateTopK
	if filter == nil || len(filter) == 0 {
		// 知识库级别搜索，使用更大的候选数
		limit = o.config.CandidateTopKAllDocs
	}

	if o.config.EnableHybridSearch {
		return o.vectorStore.HybridSearch(ctx, query, queryVector, limit, filter)
	}
	return o.vectorStore.Search(ctx, queryVector, limit, filter)
}

// selectWithMMR 使用 MMR 算法选择多样化的文档块
func (o *Orchestrator) selectWithMMR(chunks []Chunk, topK int, maxPerDoc int) []Chunk {
	if len(chunks) <= topK {
		return chunks
	}

	// 简化版 MMR：按文档分组，每个文档最多保留 maxPerDoc 个
	docCount := make(map[string]int)
	selected := make([]Chunk, 0, topK)

	for _, chunk := range chunks {
		if len(selected) >= topK {
			break
		}
		if docCount[chunk.DocumentID] < maxPerDoc {
			selected = append(selected, chunk)
			docCount[chunk.DocumentID]++
		}
	}

	return selected
}
