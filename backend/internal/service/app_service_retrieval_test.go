package service

import (
	"context"
	"math"
	"strings"
	"testing"

	"ai-localbase/internal/model"
)

func TestResolveRetrievalParams(t *testing.T) {
	t.Run("document scope", func(t *testing.T) {
		params := resolveRetrievalParams(model.ChatCompletionRequest{DocumentID: "doc-1"})
		if params.candidateTopK != ragSearchCandidateTopKDocument {
			t.Fatalf("expected document candidateTopK %d, got %d", ragSearchCandidateTopKDocument, params.candidateTopK)
		}
		if params.finalTopK != ragSearchTopKDocument {
			t.Fatalf("expected document finalTopK %d, got %d", ragSearchTopKDocument, params.finalTopK)
		}
		if params.perDocumentLimit != ragSearchTopKDocument {
			t.Fatalf("expected document perDocumentLimit %d, got %d", ragSearchTopKDocument, params.perDocumentLimit)
		}
	})

	t.Run("all documents scope", func(t *testing.T) {
		params := resolveRetrievalParams(model.ChatCompletionRequest{KnowledgeBaseID: "kb-1"})
		if params.candidateTopK != ragSearchCandidateTopKAllDocs {
			t.Fatalf("expected all-docs candidateTopK %d, got %d", ragSearchCandidateTopKAllDocs, params.candidateTopK)
		}
		if params.finalTopK != ragSearchTopKKnowledgeBase {
			t.Fatalf("expected all-docs finalTopK %d, got %d", ragSearchTopKKnowledgeBase, params.finalTopK)
		}
		if params.perDocumentLimit != ragMaxChunksPerDocument {
			t.Fatalf("expected all-docs perDocumentLimit %d, got %d", ragMaxChunksPerDocument, params.perDocumentLimit)
		}
	})
}

func TestSelectWithMMRRespectsPerDocumentLimit(t *testing.T) {
	candidates := []RetrievedChunk{
		{DocumentChunk: DocumentChunk{DocumentID: "doc-a", Text: "武汉大学 师资 规模", Index: 0}, Score: 0.98},
		{DocumentChunk: DocumentChunk{DocumentID: "doc-a", Text: "武汉大学 教学 团队", Index: 1}, Score: 0.96},
		{DocumentChunk: DocumentChunk{DocumentID: "doc-b", Text: "师资 结构 与 职称", Index: 0}, Score: 0.95},
		{DocumentChunk: DocumentChunk{DocumentID: "doc-c", Text: "高层次 人才 平台", Index: 0}, Score: 0.94},
	}

	selected := selectWithMMR(candidates, 3, 1)
	if len(selected) != 3 {
		t.Fatalf("expected selected size 3, got %d", len(selected))
	}

	counter := map[string]int{}
	for _, item := range selected {
		counter[item.DocumentID]++
	}
	for docID, count := range counter {
		if count > 1 {
			t.Fatalf("expected per-document limit to be respected, doc %s selected %d times", docID, count)
		}
	}
}

func TestRerankCandidatesBoostsKeywordCoverage(t *testing.T) {
	query := "武汉大学 师资"
	candidates := []RetrievedChunk{
		{DocumentChunk: DocumentChunk{DocumentID: "doc-redis", Text: "Redis 缓存 集群 高可用"}, Score: 0.90},
		{DocumentChunk: DocumentChunk{DocumentID: "doc-whu", Text: "武汉大学 师资 规模 与 职称结构"}, Score: 0.89},
		{DocumentChunk: DocumentChunk{DocumentID: "doc-misc", Text: "数据库 连接 池 参数"}, Score: 0.10},
	}

	service := &AppService{}
	ranked := service.rerankCandidates(context.Background(), candidates, query)
	if len(ranked) != len(candidates) {
		t.Fatalf("expected ranked size %d, got %d", len(candidates), len(ranked))
	}
	if ranked[0].DocumentID != "doc-whu" {
		t.Fatalf("expected keyword-related doc to rank first, got %s", ranked[0].DocumentID)
	}
}

func TestCosineSimilarity(t *testing.T) {
	vecA := []float32{1, 0, 0}
	vecB := []float32{1, 0, 0}
	vecC := []float32{0, 1, 0}

	if got := cosineSimilarity(vecA, vecB); math.Abs(float64(got-1)) > 1e-6 {
		t.Fatalf("expected cosine similarity 1, got %f", got)
	}
	if got := cosineSimilarity(vecA, vecC); math.Abs(float64(got)) > 1e-6 {
		t.Fatalf("expected cosine similarity 0, got %f", got)
	}
}

func TestEmbeddingRerankerOrder(t *testing.T) {
	reranker := &EmbeddingReranker{}
	reranker.embed = func(ctx context.Context, cfg model.EmbeddingModelConfig, texts []string, vectorSize int) ([][]float64, error) {
		if len(texts) == 1 {
			return [][]float64{{1, 0}}, nil
		}
		vectors := make([][]float64, 0, len(texts))
		for _, text := range texts {
			if text == "match" {
				vectors = append(vectors, []float64{1, 0})
			} else {
				vectors = append(vectors, []float64{0, 1})
			}
		}
		return vectors, nil
	}

	candidates := []RetrievedChunk{
		{DocumentChunk: DocumentChunk{DocumentID: "doc-1", Text: "match", Index: 0}, Score: 0.1},
		{DocumentChunk: DocumentChunk{DocumentID: "doc-2", Text: "other", Index: 0}, Score: 0.9},
	}
	result, err := reranker.Rerank(context.Background(), "query", candidates)
	if err != nil {
		t.Fatalf("expected rerank success, got %v", err)
	}
	if len(result) != len(candidates) {
		t.Fatalf("expected ranked size %d, got %d", len(candidates), len(result))
	}
	if result[0].DocumentID != "doc-1" {
		t.Fatalf("expected embedding-related doc to rank first, got %s", result[0].DocumentID)
	}
}

func TestIsLowConfidenceSelection(t *testing.T) {
	t.Run("low scores", func(t *testing.T) {
		chunks := []RetrievedChunk{
			{DocumentChunk: DocumentChunk{DocumentID: "doc-1", Text: "随机片段"}, Score: 0.12},
			{DocumentChunk: DocumentChunk{DocumentID: "doc-2", Text: "无关内容"}, Score: 0.10},
		}
		if !isLowConfidenceSelection("武汉大学 师资", chunks) {
			t.Fatal("expected low confidence when scores are too low")
		}
	})

	t.Run("good scores and entity coverage", func(t *testing.T) {
		chunks := []RetrievedChunk{
			{DocumentChunk: DocumentChunk{DocumentID: "doc-1", Text: "武汉大学 师资 规模 超过 3800 人"}, Score: 0.85},
			{DocumentChunk: DocumentChunk{DocumentID: "doc-2", Text: "师资 结构 包含 教授 与 青年人才"}, Score: 0.72},
		}
		if isLowConfidenceSelection("武汉大学 师资", chunks) {
			t.Fatal("expected confident selection when scores and coverage are sufficient")
		}
	})
}

func TestDeduplicateRetrievedChunks(t *testing.T) {
	chunks := []RetrievedChunk{
		{DocumentChunk: DocumentChunk{DocumentID: "doc-1", DocumentName: "employees.csv", Text: "文件：employees.csv。字段：姓名、职称。数据行数：4。"}, Score: 0.99},
		{DocumentChunk: DocumentChunk{DocumentID: "doc-1", DocumentName: "employees.csv", Text: "文件：employees.csv。字段：姓名、职称。数据行数：4。"}, Score: 0.95},
		{DocumentChunk: DocumentChunk{DocumentID: "doc-1", DocumentName: "employees.csv", Text: "第2行：姓名：张三。职称：高级职称。"}, Score: 0.94},
		{DocumentChunk: DocumentChunk{DocumentID: "doc-2", DocumentName: "other.csv", Text: "文件：other.csv。字段：姓名。数据行数：1。"}, Score: 0.90},
	}

	filtered := deduplicateRetrievedChunks(chunks)
	if len(filtered) != 3 {
		t.Fatalf("expected 3 unique chunks, got %d", len(filtered))
	}
	if filtered[0].Text != chunks[0].Text {
		t.Fatalf("expected first chunk to be preserved, got %q", filtered[0].Text)
	}
}

func TestBuildChunkTextDeduplicatesRepeatedChunks(t *testing.T) {
	chunks := []RetrievedChunk{
		{DocumentChunk: DocumentChunk{DocumentID: "doc-1", DocumentName: "employees.csv", Text: "文件：employees.csv。字段：姓名、职称。数据行数：4。", Index: 0}, Score: 0.99},
		{DocumentChunk: DocumentChunk{DocumentID: "doc-1", DocumentName: "employees.csv", Text: "文件：employees.csv。字段：姓名、职称。数据行数：4。", Index: 1}, Score: 0.95},
		{DocumentChunk: DocumentChunk{DocumentID: "doc-1", DocumentName: "employees.csv", Text: "第2行：姓名：张三。职称：高级职称。", Index: 2}, Score: 0.94},
	}

	text := buildChunkText(chunks)
	if strings.Count(text, "字段：姓名、职称。数据行数：4。") != 1 {
		t.Fatalf("expected repeated summary to appear once, got %q", text)
	}
	if !strings.Contains(text, "第2行：姓名：张三。职称：高级职称。") {
		t.Fatalf("expected row detail to be preserved, got %q", text)
	}
}
