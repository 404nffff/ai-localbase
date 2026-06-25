package retrieval

import (
	"context"

	"ai-localbase/internal/service"
)

// QdrantVectorStoreAdapter 将 QdrantService 适配为 VectorStore 接口
type QdrantVectorStoreAdapter struct {
	svc              *service.QdrantService
	knowledgeBaseID  string
}

func NewQdrantVectorStoreAdapter(svc *service.QdrantService, knowledgeBaseID string) *QdrantVectorStoreAdapter {
	return &QdrantVectorStoreAdapter{
		svc:             svc,
		knowledgeBaseID: knowledgeBaseID,
	}
}

func (a *QdrantVectorStoreAdapter) Search(ctx context.Context, vector []float64, limit int, filter map[string]interface{}) ([]Chunk, error) {
	results, err := a.svc.Search(ctx, a.knowledgeBaseID, vector, limit, filter)
	if err != nil {
		return nil, err
	}
	return convertQdrantResultsToChunks(results), nil
}

func (a *QdrantVectorStoreAdapter) HybridSearch(ctx context.Context, query string, vector []float64, limit int, filter map[string]interface{}) ([]Chunk, error) {
	// 转换 vector 为 float32
	denseVec := make([]float32, len(vector))
	for i, v := range vector {
		denseVec[i] = float32(v)
	}

	params := service.HybridSearchParams{
		CollectionName: a.knowledgeBaseID,
		DenseVector:    denseVec,
		SparseVector:   service.SparseVector{}, // 需要稀疏向量时填充
		TopK:           limit,
		Filter:         filter,
	}

	results, err := a.svc.SearchHybrid(ctx, params)
	if err != nil {
		return nil, err
	}
	return convertQdrantResultsToChunks(results), nil
}

// RerankerAdapter 将 SemanticReranker 适配为接口
type RerankerAdapter struct {
	reranker service.SemanticReranker
}

func NewRerankerAdapter(reranker service.SemanticReranker) *RerankerAdapter {
	return &RerankerAdapter{reranker: reranker}
}

func (a *RerankerAdapter) Rerank(ctx context.Context, query string, chunks []Chunk) ([]Chunk, error) {
	retrieved := convertChunksToRetrieved(chunks)
	reranked, err := a.reranker.Rerank(ctx, query, retrieved)
	if err != nil {
		return nil, err
	}
	return convertRetrievedToChunks(reranked), nil
}

// EvidenceGateAdapter 暂时未实现，返回原始结果
type EvidenceGateAdapter struct {
	// 目前系统中没有独立的 EvidenceGate 接口，逻辑在 app_service 中
}

func NewEvidenceGateAdapter() *EvidenceGateAdapter {
	return &EvidenceGateAdapter{}
}

func (a *EvidenceGateAdapter) Filter(ctx context.Context, query string, chunks []Chunk) ([]Chunk, error) {
	// TODO: 从 app_service 中提取 evidence gate 逻辑
	return chunks, nil
}

// 辅助转换函数

func convertQdrantResultsToChunks(results []service.QdrantSearchResult) []Chunk {
	chunks := make([]Chunk, len(results))
	for i, r := range results {
		chunks[i] = Chunk{
			ID:       r.ID,
			Score:    r.Score,
			Metadata: r.Payload,
		}
		// 从 payload 提取字段
		if text, ok := r.Payload["text"].(string); ok {
			chunks[i].Content = text
		}
		if kbID, ok := r.Payload["knowledge_base_id"].(string); ok {
			chunks[i].KnowledgeBaseID = kbID
		}
		if docID, ok := r.Payload["document_id"].(string); ok {
			chunks[i].DocumentID = docID
		}
	}
	return chunks
}

func convertChunksToRetrieved(chunks []Chunk) []service.RetrievedChunk {
	retrieved := make([]service.RetrievedChunk, len(chunks))
	for i, c := range chunks {
		retrieved[i] = service.RetrievedChunk{
			DocumentChunk: service.DocumentChunk{
				ID:              c.ID,
				KnowledgeBaseID: c.KnowledgeBaseID,
				DocumentID:      c.DocumentID,
				Text:            c.Content,
			},
			Score:    c.Score,
			RawScore: c.Score,
		}
		if docName, ok := c.Metadata["document_name"].(string); ok {
			retrieved[i].DocumentName = docName
		}
	}
	return retrieved
}

func convertRetrievedToChunks(retrieved []service.RetrievedChunk) []Chunk {
	chunks := make([]Chunk, len(retrieved))
	for i, r := range retrieved {
		chunks[i] = Chunk{
			ID:              r.ID,
			Content:         r.Text,
			Score:           r.Score,
			KnowledgeBaseID: r.KnowledgeBaseID,
			DocumentID:      r.DocumentID,
			Metadata: map[string]interface{}{
				"document_name": r.DocumentName,
				"index":         r.Index,
				"kind":          r.Kind,
			},
		}
	}
	return chunks
}
