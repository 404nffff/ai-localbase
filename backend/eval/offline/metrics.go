package offline

import (
	"math"
	"sort"
	"strings"
	"time"
)

// CaseResult 单个用例的评估结果
type CaseResult struct {
	CaseID            string
	Question          string
	GroundTruth       GroundTruthCase
	RetrievedChunks   []RetrievedChunkInfo // 检索到的文档信息
	LLMAnswer         string
	RetrievalLatency  time.Duration
	GenerationLatency time.Duration
	HitRank           int     // 第一个命中的排名，-1 表示未命中
	ReciprocalRank    float64 // 1/HitRank，未命中为 0
	Error             string  // 运行时错误信息（若有）
}

// RetrievedChunkInfo 检索结果摘要（不依赖 service 包，避免循环依赖）
type RetrievedChunkInfo struct {
	DocumentID string
	ChunkID    string
	Text       string
	Score      float64
}

// AggregateMetrics 聚合后的评估指标
type AggregateMetrics struct {
	TotalCases           int
	HitRate              float64
	MRR                  float64
	LatencyRetrievalP50  time.Duration
	LatencyRetrievalP95  time.Duration
	LatencyGenerationP50 time.Duration
	LatencyGenerationP95 time.Duration
}

// IsHit 判断单个用例是否命中（支持 doc-id 匹配和文本包含匹配）
// threshold: 文本相似度阈值（0-1），默认 0.5
func IsHit(result CaseResult, gt GroundTruthCase, threshold float64) (hit bool, rank int) {
	// 优先使用 SourceDocuments 中的 DocumentID 精确匹配
	for i, chunk := range result.RetrievedChunks {
		for _, srcDoc := range gt.SourceDocuments {
			if chunk.DocumentID == srcDoc.DocumentID {
				return true, i + 1 // 1-based rank
			}
		}
	}

	// 若无 SourceDocuments，对 AnswerSnippets 中每个片段，检查是否被任一 chunk Text 包含
	if len(gt.SourceDocuments) == 0 && len(gt.AnswerSnippets) > 0 {
		for i, chunk := range result.RetrievedChunks {
			for _, snippet := range gt.AnswerSnippets {
				if strings.Contains(chunk.Text, snippet) {
					return true, i + 1 // 1-based rank
				}
			}
		}
	}

	return false, -1
}

// ComputeHitRate 计算命中率
func ComputeHitRate(results []CaseResult, gts []GroundTruthCase, threshold float64) float64 {
	if len(results) == 0 {
		return 0.0
	}

	hits := 0
	for i, res := range results {
		if i >= len(gts) {
			break
		}
		gt := gts[i]
		if hit, _ := IsHit(res, gt, threshold); hit {
			hits++
		}
	}
	return float64(hits) / float64(len(results))
}

// ComputeMRR 计算 MRR
func ComputeMRR(results []CaseResult, gts []GroundTruthCase, threshold float64) float64 {
	if len(results) == 0 {
		return 0.0
	}

	var sumReciprocalRank float64
	for i, res := range results {
		if i >= len(gts) {
			break
		}
		gt := gts[i]
		if hit, rank := IsHit(res, gt, threshold); hit && rank > 0 {
			sumReciprocalRank += 1.0 / float64(rank)
		}
	}
	return sumReciprocalRank / float64(len(results))
}

// ComputeLatencyPercentiles 计算时延 P50/P95
func ComputeLatencyPercentiles(durations []time.Duration) (p50, p95 time.Duration) {
	if len(durations) == 0 {
		return 0, 0
	}

	sort.Slice(durations, func(i, j int) bool {
		return durations[i] < durations[j]
	})

	n := len(durations)
	// p50: 中位数，偶数取 n/2（上中位）
	p50Index := n / 2
	if p50Index >= n {
		p50Index = n - 1
	}
	// p95: floor((n-1)*0.95) 使得 10 个元素取 index 8 (80ms)
	p95Index := int(math.Floor(float64(n-1) * 0.95))
	if p95Index >= n {
		p95Index = n - 1
	}

	if p50Index < 0 {
		p50Index = 0
	}
	if p95Index < 0 {
		p95Index = 0
	}

	return durations[p50Index], durations[p95Index]
}

// Aggregate 汇总所有用例结果为聚合指标
func Aggregate(results []CaseResult, gts []GroundTruthCase) AggregateMetrics {
	totalCases := len(results)
	if totalCases == 0 {
		return AggregateMetrics{}
	}

	var retrievalLatencies []time.Duration
	var generationLatencies []time.Duration
	for _, res := range results {
		retrievalLatencies = append(retrievalLatencies, res.RetrievalLatency)
		generationLatencies = append(generationLatencies, res.GenerationLatency)
	}

	retrievalP50, retrievalP95 := ComputeLatencyPercentiles(retrievalLatencies)
	generationP50, generationP95 := ComputeLatencyPercentiles(generationLatencies)

	hitRate := ComputeHitRate(results, gts, 0.5) // Default threshold
	mrr := ComputeMRR(results, gts, 0.5)         // Default threshold

	return AggregateMetrics{
		TotalCases:           totalCases,
		HitRate:              hitRate,
		MRR:                  mrr,
		LatencyRetrievalP50:  retrievalP50,
		LatencyRetrievalP95:  retrievalP95,
		LatencyGenerationP50: generationP50,
		LatencyGenerationP95: generationP95,
	}
}
