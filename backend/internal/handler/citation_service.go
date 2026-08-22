package handler

import (
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// scoredCitationSource keeps citation calibration independent from HTTP request handling.
// The handler only supplies the question, answer and candidate sources.
type scoredCitationSource struct {
	source     map[string]string
	score      int
	index      int
	answerHits int
	queryHits  int
}

func calibrateCitationSources(question, answer string, sources []map[string]string, knowledgeBaseID, documentID string) []map[string]string {
	if len(sources) == 0 {
		return nil
	}

	answerTerms := citationTerms(answer)
	queryTerms := citationTerms(question)
	scored := make([]scoredCitationSource, 0, len(sources))
	for index, source := range sources {
		if !isDocumentCitationSource(source) || !citationSourceMatchesScope(source, knowledgeBaseID, documentID) {
			continue
		}

		text := citationSourceText(source)
		answerHits := citationHitCount(answerTerms, text)
		queryHits := citationHitCount(queryTerms, text)
		rawScore := parseCitationRawScore(source["score"])
		score := answerHits*6 + queryHits*2 + int(rawScore*3)
		if !sourcePassesCitationGate(answerHits, queryHits, rawScore, len(answerTerms)) {
			continue
		}

		scored = append(scored, scoredCitationSource{
			source:     cloneStringMap(source),
			score:      score,
			index:      index,
			answerHits: answerHits,
			queryHits:  queryHits,
		})
	}

	if len(scored) == 0 {
		return nil
	}

	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score == scored[j].score {
			return scored[i].index < scored[j].index
		}
		return scored[i].score > scored[j].score
	})

	const limit = 4
	out := make([]map[string]string, 0, minInt(len(scored), limit))
	seen := make(map[string]struct{}, len(scored))
	for _, item := range scored {
		key := strings.TrimSpace(item.source["chunkId"])
		if key == "" {
			key = strings.TrimSpace(item.source["documentId"]) + ":" + strings.TrimSpace(item.source["snippet"])
		}
		if key != "" {
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
		}
		out = append(out, item.source)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func citationSourceMatchesScope(source map[string]string, knowledgeBaseID, documentID string) bool {
	knowledgeBaseID = strings.TrimSpace(knowledgeBaseID)
	documentID = strings.TrimSpace(documentID)
	if knowledgeBaseID != "" && strings.TrimSpace(source["knowledgeBaseId"]) != knowledgeBaseID {
		return false
	}
	if documentID != "" && strings.TrimSpace(source["documentId"]) != documentID {
		return false
	}
	return true
}

func isDocumentCitationSource(source map[string]string) bool {
	for _, key := range []string{"knowledgeBaseId", "documentId", "documentName", "chunkId", "snippet"} {
		if strings.TrimSpace(source[key]) == "" {
			return false
		}
	}
	return true
}

func citationSourceText(source map[string]string) string {
	parts := []string{source["snippet"], source["documentName"], source["chunkKind"]}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func sourcePassesCitationGate(answerHits, queryHits int, rawScore float64, answerTermCount int) bool {
	if answerHits >= 2 {
		return true
	}
	if answerHits >= 1 && queryHits >= 1 {
		return true
	}
	if answerTermCount == 0 && queryHits >= 2 && rawScore >= 0.65 {
		return true
	}
	return false
}

func parseCitationRawScore(value string) float64 {
	parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil {
		return 0
	}
	if parsed < 0 {
		return 0
	}
	if parsed > 1 {
		return 1
	}
	return parsed
}

func citationHitCount(terms []string, text string) int {
	if len(terms) == 0 || strings.TrimSpace(text) == "" {
		return 0
	}
	lowered := strings.ToLower(text)
	hits := 0
	for _, term := range terms {
		if strings.Contains(lowered, term) {
			hits++
		}
	}
	return hits
}

func citationTerms(text string) []string {
	text = strings.ToLower(strings.TrimSpace(text))
	if text == "" {
		return nil
	}

	terms := make([]string, 0)
	var wordRunes []rune
	var hanRunes []rune
	flushWord := func() {
		if len(wordRunes) >= 2 {
			terms = append(terms, string(wordRunes))
		}
		wordRunes = wordRunes[:0]
	}
	flushHan := func() {
		if len(hanRunes) >= 2 {
			maxN := minInt(4, len(hanRunes))
			for n := 2; n <= maxN; n++ {
				for i := 0; i+n <= len(hanRunes); i++ {
					terms = append(terms, string(hanRunes[i:i+n]))
				}
			}
		}
		hanRunes = hanRunes[:0]
	}

	for _, r := range text {
		switch {
		case unicode.In(r, unicode.Han):
			flushWord()
			hanRunes = append(hanRunes, r)
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			flushHan()
			wordRunes = append(wordRunes, r)
		default:
			flushWord()
			flushHan()
		}
	}
	flushWord()
	flushHan()

	stopTerms := map[string]struct{}{
		"什么": {}, "多少": {}, "几个": {}, "如何": {}, "怎么": {}, "是否": {},
		"是谁": {}, "哪些": {}, "有没有": {}, "请问": {}, "告诉": {}, "一下": {},
		"当前": {}, "核心": {}, "观点": {}, "信息": {}, "文档": {}, "内容": {},
		"回答": {}, "来源": {}, "显示": {}, "可以": {}, "进行": {}, "相关": {},
		"一个": {}, "一种": {}, "世界": {}, "同时": {}, "关系": {},
		"the": {}, "and": {}, "for": {}, "with": {}, "what": {}, "which": {},
		"who": {}, "how": {}, "where": {}, "when": {}, "is": {}, "are": {},
	}
	out := make([]string, 0, len(terms))
	seen := make(map[string]struct{}, len(terms))
	for _, term := range terms {
		term = strings.TrimSpace(strings.ToLower(term))
		if len([]rune(term)) < 2 {
			continue
		}
		if _, stop := stopTerms[term]; stop {
			continue
		}
		if _, exists := seen[term]; exists {
			continue
		}
		seen[term] = struct{}{}
		out = append(out, term)
	}
	return out
}

func cloneStringMap(input map[string]string) map[string]string {
	if input == nil {
		return nil
	}
	out := make(map[string]string, len(input)+1)
	for key, value := range input {
		out[key] = value
	}
	return out
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
