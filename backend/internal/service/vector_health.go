package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"ai-localbase/internal/model"
)

// VectorHealth 描述真实远端索引状态；unknown 表示查询失败而非数量为零。
type VectorHealth struct {
	Count  int
	Source string
	Status string
}

// InspectDocumentVectors 查询集合结构及当前文档的实际向量数量，不创建或修复集合。
// 复用当前版本对具名与旧式未命名向量的兼容；远端错误不暴露响应正文。
func (s *QdrantService) InspectDocumentVectors(ctx context.Context, document model.Document) VectorHealth {
	if !s.IsEnabled() || document.Status != "indexed" {
		return VectorHealth{Source: "not_applicable", Status: "not_applicable"}
	}
	schema, err := s.getCollectionSchema(ctx, s.CollectionName(document.KnowledgeBaseID))
	if err != nil {
		if isQdrantNotFound(err) {
			return VectorHealth{Source: "unknown", Status: "collection_missing"}
		}
		return VectorHealth{Source: "unknown", Status: "unavailable"}
	}
	if schema.Dense.Size != s.vectorSize {
		return VectorHealth{Source: "unknown", Status: "dimension_mismatch"}
	}
	filter := generationFilter(document.ID, document.IndexGeneration)
	count, err := s.CountPointsByFilter(ctx, document.KnowledgeBaseID, filter)
	if err != nil {
		return VectorHealth{Source: "unknown", Status: "unavailable"}
	}
	return VectorHealth{Count: count, Source: "actual", Status: "ok"}
}

// CountPointsByFilter 通过 Qdrant exact count 读取数量，不下载原文、向量或 point ID。
func (s *QdrantService) CountPointsByFilter(ctx context.Context, knowledgeBaseID string, filter map[string]any) (int, error) {
	response, err := s.doJSON(ctx, http.MethodPost, "/collections/"+url.PathEscape(s.CollectionName(knowledgeBaseID))+"/points/count", map[string]any{"filter": filter, "exact": true})
	if err != nil {
		return 0, err
	}
	var result struct {
		Result struct {
			Count *int `json:"count"`
		} `json:"result"`
	}
	if err := json.Unmarshal(response, &result); err != nil {
		return 0, fmt.Errorf("decode qdrant count: %w", err)
	}
	if result.Result.Count == nil || *result.Result.Count < 0 {
		return 0, fmt.Errorf("qdrant count response has no valid count")
	}
	return *result.Result.Count, nil
}
