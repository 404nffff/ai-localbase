package service

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"ai-localbase/internal/model"
	"ai-localbase/internal/util"
)

const (
	defaultEvalCasesPerDocument = 5
	maxEvalCasesPerDocument     = 20
)

func (s *AppService) GenerateEvalDataset(req model.GenerateEvalDatasetRequest) (model.GenerateEvalDatasetResponse, error) {
	if s == nil {
		return model.GenerateEvalDatasetResponse{}, fmt.Errorf("app service is nil")
	}

	maxPerDocument := req.MaxPerDocument
	if maxPerDocument <= 0 {
		maxPerDocument = defaultEvalCasesPerDocument
	}
	if maxPerDocument > maxEvalCasesPerDocument {
		maxPerDocument = maxEvalCasesPerDocument
	}

	documents, err := s.evalDatasetDocuments(req)
	if err != nil {
		return model.GenerateEvalDatasetResponse{}, err
	}
	if len(documents) == 0 {
		return model.GenerateEvalDatasetResponse{}, fmt.Errorf("no documents available for eval dataset generation")
	}

	cases := make([]model.EvalGroundTruthCase, 0, len(documents)*maxPerDocument)
	for _, document := range documents {
		text, err := util.ExtractDocumentText(document.Path)
		if err != nil {
			return model.GenerateEvalDatasetResponse{}, fmt.Errorf("extract document %s: %w", document.ID, err)
		}
		chunks := s.rag.BuildDocumentChunks(document, text)
		selected := selectEvalChunkCandidates(chunks, maxPerDocument)
		for _, chunk := range selected {
			caseID := fmt.Sprintf("auto-%s-%03d", sanitizeEvalIDPart(document.ID), len(cases)+1)
			answer := clipEvalRunes(normalizeEvalWhitespace(chunk.Text), 260)
			snippets := evalAnswerSnippets(chunk.Text)
			if len(snippets) == 0 && answer != "" {
				snippets = []string{clipEvalRunes(answer, 80)}
			}

			cases = append(cases, model.EvalGroundTruthCase{
				ID:             caseID,
				Question:       buildEvalQuestion(document, chunk),
				Answer:         answer,
				AnswerSnippets: snippets,
				SourceDocuments: []model.EvalSourceDocument{{
					KnowledgeBaseID: document.KnowledgeBaseID,
					DocumentID:      document.ID,
					ChunkID:         chunk.ID,
				}},
				AnswerType: classifyEvalAnswerType(chunk.Text),
				Difficulty: classifyEvalDifficulty(chunk.Text),
				Notes:      fmt.Sprintf("auto-generated from %s", document.Name),
			})
		}
	}

	if len(cases) == 0 {
		return model.GenerateEvalDatasetResponse{}, fmt.Errorf("no eval cases generated from selected documents")
	}

	return model.GenerateEvalDatasetResponse{
		KnowledgeBaseID: strings.TrimSpace(req.KnowledgeBaseID),
		DocumentID:      strings.TrimSpace(req.DocumentID),
		Count:           len(cases),
		DocumentCount:   len(documents),
		Items:           cases,
	}, nil
}

func (s *AppService) evalDatasetDocuments(req model.GenerateEvalDatasetRequest) ([]model.Document, error) {
	knowledgeBaseID := strings.TrimSpace(req.KnowledgeBaseID)
	documentID := strings.TrimSpace(req.DocumentID)

	s.state.Mu.RLock()
	defer s.state.Mu.RUnlock()

	kbs := make([]model.KnowledgeBase, 0, len(s.state.KnowledgeBases))
	if knowledgeBaseID != "" {
		kb, ok := s.state.KnowledgeBases[knowledgeBaseID]
		if !ok {
			return nil, fmt.Errorf("knowledge base not found")
		}
		kbs = append(kbs, kb)
	} else {
		for _, kb := range s.state.KnowledgeBases {
			kbs = append(kbs, kb)
		}
		sort.Slice(kbs, func(i, j int) bool {
			return kbs[i].CreatedAt < kbs[j].CreatedAt
		})
	}

	documents := make([]model.Document, 0)
	for _, kb := range kbs {
		for _, document := range kb.Documents {
			if documentID != "" && document.ID != documentID {
				continue
			}
			if strings.TrimSpace(document.Path) == "" {
				continue
			}
			documents = append(documents, document)
		}
	}
	if documentID != "" && len(documents) == 0 {
		return nil, fmt.Errorf("document not found")
	}
	return documents, nil
}

func selectEvalChunkCandidates(chunks []DocumentChunk, maxCount int) []DocumentChunk {
	if len(chunks) == 0 || maxCount <= 0 {
		return nil
	}

	candidates := make([]DocumentChunk, 0, len(chunks))
	for _, chunk := range chunks {
		text := normalizeEvalWhitespace(chunk.Text)
		if utf8.RuneCountInString(text) < 40 {
			continue
		}
		if isLowValueEvalChunk(text) {
			continue
		}
		candidates = append(candidates, chunk)
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		left := evalChunkScore(candidates[i])
		right := evalChunkScore(candidates[j])
		if left == right {
			return candidates[i].Index < candidates[j].Index
		}
		return left > right
	})

	if len(candidates) > maxCount {
		candidates = candidates[:maxCount]
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].Index < candidates[j].Index
	})
	return candidates
}

func evalChunkScore(chunk DocumentChunk) int {
	text := normalizeEvalWhitespace(chunk.Text)
	score := 0
	if chunk.Kind == "structured_summary" {
		score += 5
	}
	if regexp.MustCompile(`什么是|是指|指的是|简介|概述|介绍`).MatchString(text) {
		score += 4
	}
	if regexp.MustCompile(`包括|支持|提供|具有|分为|涵盖|用于|负责`).MatchString(text) {
		score += 3
	}
	if regexp.MustCompile(`流程|步骤|机制|策略|配置`).MatchString(text) {
		score += 3
	}
	if regexp.MustCompile(`\d+\s*(个|条|项|种|年|%|ms|秒|页|MB|GB)?`).MatchString(text) {
		score += 2
	}
	if utf8.RuneCountInString(text) >= 100 {
		score++
	}
	return score
}

func buildEvalQuestion(document model.Document, chunk DocumentChunk) string {
	text := normalizeEvalWhitespace(chunk.Text)
	subject := evalQuestionSubject(document.Name, text)
	if subject == "" {
		subject = strings.TrimSuffix(document.Name, filepathExt(document.Name))
	}

	switch {
	case regexp.MustCompile(`什么是|是指|指的是|简介|概述|介绍`).MatchString(text):
		return fmt.Sprintf("什么是%s？", subject)
	case regexp.MustCompile(`流程|步骤`).MatchString(text):
		return fmt.Sprintf("%s的流程是什么？", subject)
	case regexp.MustCompile(`配置|安装|启动|部署`).MatchString(text):
		return fmt.Sprintf("%s如何配置或使用？", subject)
	case regexp.MustCompile(`包括|支持|提供|具有|涵盖`).MatchString(text):
		return fmt.Sprintf("%s包括哪些要点？", subject)
	case regexp.MustCompile(`区别|对比`).MatchString(text):
		return fmt.Sprintf("%s中提到的区别是什么？", subject)
	default:
		return fmt.Sprintf("%s主要讲了什么？", subject)
	}
}

func evalQuestionSubject(documentName, text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		line = regexp.MustCompile(`^[#*\-\d.\s]+`).ReplaceAllString(line, "")
		line = strings.Trim(line, "：:。；，、[]（）()")
		line = normalizeEvalWhitespace(line)
		if line == "" {
			continue
		}
		if utf8.RuneCountInString(line) <= 28 {
			return line
		}
		if idx := strings.IndexAny(line, "：:，。；;（("); idx > 0 {
			candidate := strings.TrimSpace(line[:idx])
			if utf8.RuneCountInString(candidate) >= 2 && utf8.RuneCountInString(candidate) <= 28 {
				return candidate
			}
		}
		break
	}

	base := strings.TrimSuffix(documentName, filepathExt(documentName))
	base = strings.TrimSpace(base)
	if base != "" {
		return base
	}
	return "该文档"
}

func evalAnswerSnippets(text string) []string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	snippets := make([]string, 0, 2)
	seen := map[string]struct{}{}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		line = strings.Trim(line, "#*-• \t")
		line = normalizeEvalWhitespace(line)
		if utf8.RuneCountInString(line) < 12 {
			continue
		}
		snippet := clipEvalRunes(line, 90)
		if _, ok := seen[snippet]; ok {
			continue
		}
		seen[snippet] = struct{}{}
		snippets = append(snippets, snippet)
		if len(snippets) >= 2 {
			break
		}
	}
	return snippets
}

func classifyEvalAnswerType(text string) string {
	switch {
	case regexp.MustCompile(`\d+\s*(个|条|项|种|年|%|ms|秒|页|MB|GB)?`).MatchString(text):
		return "numeric"
	case regexp.MustCompile(`流程|步骤|阶段`).MatchString(text):
		return "process"
	case regexp.MustCompile(`包括|支持|提供|具有|涵盖|分为`).MatchString(text):
		return "listing"
	default:
		return "extractive"
	}
}

func classifyEvalDifficulty(text string) string {
	length := utf8.RuneCountInString(normalizeEvalWhitespace(text))
	switch {
	case length >= 220:
		return "hard"
	case length >= 120:
		return "medium"
	default:
		return "easy"
	}
}

func isLowValueEvalChunk(text string) bool {
	if strings.HasPrefix(text, "|") && strings.Contains(text, "---") {
		return true
	}
	return regexp.MustCompile(`^(目录|参考|附录|版权|免责声明)$`).MatchString(text)
}

func normalizeEvalWhitespace(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = regexp.MustCompile(`[ \t]+`).ReplaceAllString(text, " ")
	text = regexp.MustCompile(`\n{3,}`).ReplaceAllString(text, "\n\n")
	return strings.TrimSpace(text)
}

func clipEvalRunes(text string, limit int) string {
	text = strings.TrimSpace(text)
	if limit <= 0 || utf8.RuneCountInString(text) <= limit {
		return text
	}
	runes := []rune(text)
	return strings.TrimSpace(string(runes[:limit]))
}

func sanitizeEvalIDPart(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return "case"
	}
	var builder strings.Builder
	lastDash := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			builder.WriteRune(r)
			lastDash = false
		case r == '-', r == '_', r == '.', r == ' ':
			if builder.Len() == 0 || lastDash {
				continue
			}
			builder.WriteByte('-')
			lastDash = true
		}
	}
	out := strings.Trim(builder.String(), "-")
	if out == "" {
		return "case"
	}
	return out
}

func filepathExt(name string) string {
	index := strings.LastIndex(name, ".")
	if index < 0 {
		return ""
	}
	return name[index:]
}
