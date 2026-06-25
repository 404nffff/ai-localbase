package retrieval

import (
	"sort"
	"strings"

	"ai-localbase/internal/service"
)

// FilterRelevantChunks 基于查询证据词和相关性阈值过滤检索结果
func FilterRelevantChunks(query string, chunks []service.RetrievedChunk) []service.RetrievedChunk {
	if len(chunks) == 0 {
		return nil
	}
	terms := queryEvidenceTerms(query)
	if len(terms) == 0 {
		return chunks
	}
	factSpecs := parseFactQuerySpecs(query)

	isStructuredQuery := strings.Contains(query, ".xlsx") || strings.Contains(query, ".csv") ||
		strings.Contains(query, ".xls") || strings.Contains(query, "工作表") ||
		strings.Contains(query, "表格")

	if isStructuredQuery {
		return chunks
	}

	filtered := make([]service.RetrievedChunk, 0, len(chunks))
	factFiltered := make([]service.RetrievedChunk, 0, len(chunks))
	preferStructuredSummary := shouldPreferStructuredSummary(query)

	for _, chunk := range chunks {
		if preferStructuredSummary && (chunk.Kind == "structured_summary" || chunk.Kind == "structured_row") {
			filtered = append(filtered, chunk)
			continue
		}
		if len(factSpecs) > 0 && factEvidenceScore(query, chunk) >= 5 {
			factFiltered = append(factFiltered, chunk)
			continue
		}
		hits := evidenceHitCount(terms, chunk.Text)
		coverage := float64(hits) / float64(len(terms))
		rawScore := chunkRawScore(chunk)

		switch {
		case hits >= 2:
			filtered = append(filtered, chunk)
		case coverage >= 0.25:
			filtered = append(filtered, chunk)
		case hits >= 1 && rawScore >= 0.55:
			filtered = append(filtered, chunk)
		case rawScore >= 0.82 && queryEvidenceCoverage(query, []service.RetrievedChunk{chunk}) > 0:
			filtered = append(filtered, chunk)
		}
	}

	if len(factFiltered) > 0 {
		sortFactEvidenceChunks(query, factFiltered)
		return factFiltered
	}

	if len(filtered) > 0 {
		if len(factSpecs) > 0 {
			sortFactEvidenceChunks(query, filtered)
		}
		return filtered
	}
	return nil
}

func sortFactEvidenceChunks(query string, chunks []service.RetrievedChunk) {
	sort.SliceStable(chunks, func(i, j int) bool {
		leftScore := factEvidenceScore(query, chunks[i])
		rightScore := factEvidenceScore(query, chunks[j])
		if leftScore == rightScore {
			return chunkRawScore(chunks[i]) > chunkRawScore(chunks[j])
		}
		return leftScore > rightScore
	})
}

func chunkRawScore(chunk service.RetrievedChunk) float64 {
	if chunk.RawScore > 0 {
		return chunk.RawScore
	}
	return chunk.Score
}

func queryEvidenceCoverage(query string, chunks []service.RetrievedChunk) float64 {
	terms := queryEvidenceTerms(query)
	if len(terms) == 0 {
		return 1.0
	}
	matchedTerms := make(map[string]bool)
	for _, chunk := range chunks {
		lowerText := strings.ToLower(chunk.Text)
		for _, term := range terms {
			if strings.Contains(lowerText, strings.ToLower(term)) {
				matchedTerms[term] = true
			}
		}
	}
	return float64(len(matchedTerms)) / float64(len(terms))
}

func factEvidenceScore(query string, chunk service.RetrievedChunk) int {
	specs := parseFactQuerySpecs(query)
	if len(specs) == 0 {
		return 0
	}
	score := 0
	lowerText := strings.ToLower(chunk.Text)
	for _, spec := range specs {
		if strings.Contains(lowerText, strings.ToLower(spec.subject)) {
			score += 5
		}
		if strings.Contains(lowerText, strings.ToLower(spec.attr)) {
			score += 3
		}
	}
	return score
}

func evidenceHitCount(terms []string, text string) int {
	if len(terms) == 0 {
		return 0
	}
	lowerText := strings.ToLower(text)
	hits := 0
	for _, term := range terms {
		if strings.Contains(lowerText, strings.ToLower(term)) {
			hits++
		}
	}
	return hits
}

func queryEvidenceTerms(query string) []string {
	query = strings.TrimSpace(query)
	if len(query) == 0 {
		return nil
	}

	stopWords := map[string]bool{
		"是": true, "的": true, "了": true, "在": true, "有": true, "和": true,
		"吗": true, "呢": true, "啊": true, "么": true, "嘛": true,
		"什么": true, "如何": true, "怎么": true, "怎样": true, "哪些": true,
		"为什么": true, "为啥": true, "多少": true, "几个": true,
		"the": true, "is": true, "are": true, "was": true, "were": true,
		"a": true, "an": true, "and": true, "or": true, "but": true,
		"in": true, "on": true, "at": true, "to": true, "for": true,
		"of": true, "with": true, "by": true, "from": true,
		"what": true, "how": true, "why": true, "where": true, "when": true,
	}

	runes := []rune(query)
	var terms []string
	var current []rune

	for _, r := range runes {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r >= 0x4E00 {
			current = append(current, r)
		} else {
			if len(current) > 0 {
				word := string(current)
				if !stopWords[strings.ToLower(word)] && len(word) >= 2 {
					terms = append(terms, word)
				}
				current = nil
			}
		}
	}
	if len(current) > 0 {
		word := string(current)
		if !stopWords[strings.ToLower(word)] && len(word) >= 2 {
			terms = append(terms, word)
		}
	}

	return terms
}

type factQuerySpec struct {
	subject string
	attr    string
}

func parseFactQuerySpecs(query string) []factQuerySpec {
	query = strings.TrimSpace(query)
	if len(query) == 0 {
		return nil
	}

	patterns := []struct {
		keywords []string
		extract  func(string, string) *factQuerySpec
	}{
		{
			keywords: []string{"的校长", "的院长", "的主任", "的负责人", "的创始人", "的CEO"},
			extract: func(q, kw string) *factQuerySpec {
				idx := strings.Index(q, kw)
				if idx > 0 {
					subject := extractSubject(q[:idx])
					attr := strings.TrimPrefix(kw, "的")
					if subject != "" {
						return &factQuerySpec{subject: subject, attr: attr}
					}
				}
				return nil
			},
		},
		{
			keywords: []string{"校长是", "院长是", "主任是", "负责人是", "创始人是", "CEO是"},
			extract: func(q, kw string) *factQuerySpec {
				idx := strings.Index(q, kw)
				if idx > 0 {
					subject := extractSubject(q[:idx])
					attr := strings.TrimSuffix(kw, "是")
					if subject != "" {
						return &factQuerySpec{subject: subject, attr: attr}
					}
				}
				return nil
			},
		},
	}

	var specs []factQuerySpec
	for _, pattern := range patterns {
		for _, kw := range pattern.keywords {
			if strings.Contains(query, kw) {
				if spec := pattern.extract(query, kw); spec != nil {
					specs = append(specs, *spec)
				}
			}
		}
	}
	return specs
}

func extractSubject(prefix string) string {
	prefix = strings.TrimSpace(prefix)
	runes := []rune(prefix)
	if len(runes) == 0 {
		return ""
	}
	var subject []rune
	for i := len(runes) - 1; i >= 0; i-- {
		r := runes[i]
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r >= 0x4E00 {
			subject = append([]rune{r}, subject...)
		} else {
			break
		}
	}
	return string(subject)
}

func shouldPreferStructuredSummary(query string) bool {
	q := strings.ToLower(query)
	keywords := []string{"统计", "汇总", "总计", "概览", "总共", "一共", "多少条", "多少个", "多少行"}
	for _, kw := range keywords {
		if strings.Contains(q, kw) {
			return true
		}
	}
	return false
}
