package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"ai-localbase/internal/util"
)

type SourceDocument struct {
	KnowledgeBaseID string `json:"knowledge_base_id"`
	DocumentID      string `json:"document_id"`
	ChunkID         string `json:"chunk_id"`
}

type GroundTruthCase struct {
	ID              string           `json:"id"`
	Question        string           `json:"question"`
	Answer          string           `json:"answer"`
	AnswerSnippets  []string         `json:"answer_snippets"`
	SourceDocuments []SourceDocument `json:"source_documents"`
	AnswerType      string           `json:"answer_type"`
	Difficulty      string           `json:"difficulty"`
	Notes           string           `json:"notes"`
}

type appState struct {
	KnowledgeBases map[string]knowledgeBase `json:"knowledgeBases"`
}

type knowledgeBase struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Documents []document `json:"documents"`
}

type document struct {
	ID              string `json:"id"`
	KnowledgeBaseID string `json:"knowledgeBaseId"`
	Name            string `json:"name"`
	Path            string `json:"path"`
}

func main() {
	uploadsDir := flag.String("uploads", "backend/data/uploads", "上传文档目录")
	statePath := flag.String("state", "backend/data/app-state.json", "应用状态文件路径")
	outputPath := flag.String("out", "backend/eval/data/ground_truth.generated.json", "生成的评估数据集输出路径")
	maxPerDoc := flag.Int("per-doc", 6, "每个文档最多生成的问题数")
	flag.Parse()

	cases, err := generateDataset(*uploadsDir, *statePath, *maxPerDoc)
	if err != nil {
		fmt.Fprintf(os.Stderr, "generate dataset failed: %v\n", err)
		os.Exit(1)
	}
	if len(cases) == 0 {
		fmt.Fprintln(os.Stderr, "no cases generated: 请先准备真实知识库文档和/或 app-state.json")
		os.Exit(1)
	}

	if err := os.MkdirAll(filepath.Dir(*outputPath), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "create output dir failed: %v\n", err)
		os.Exit(1)
	}

	data, err := json.MarshalIndent(cases, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "marshal dataset failed: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(*outputPath, data, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "write dataset failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("generated %d cases -> %s\n", len(cases), *outputPath)
}

func generateDataset(uploadsDir, statePath string, maxPerDoc int) ([]GroundTruthCase, error) {
	stateDocs := loadStateDocuments(statePath)
	files, err := collectSupportedFiles(uploadsDir)
	if err != nil {
		return nil, err
	}

	cases := make([]GroundTruthCase, 0)
	caseIndex := 1

	for _, path := range files {
		text, err := util.ExtractDocumentText(path)
		if err != nil {
			continue
		}
		text = strings.TrimSpace(text)
		if utf8.RuneCountInString(text) < 80 {
			continue
		}

		docID, kbID, docName := matchDocument(path, stateDocs)
		paragraphs := splitParagraphs(text)
		if len(paragraphs) == 0 {
			paragraphs = []string{text}
		}

		candidates := collectParagraphCandidates(paragraphs)
		selected := selectParagraphCandidates(candidates, maxPerDoc)
		for _, candidate := range selected {
			answer := clipRunes(candidate.text, 220)
			question := buildQuestion(docName, candidate)
			snippets := buildSnippets(candidate)
			if len(snippets) == 0 {
				snippets = []string{clipRunes(answer, 40)}
			}

			cases = append(cases, GroundTruthCase{
				ID:             fmt.Sprintf("auto-case-%03d", caseIndex),
				Question:       question,
				Answer:         answer,
				AnswerSnippets: snippets,
				SourceDocuments: []SourceDocument{{
					KnowledgeBaseID: kbID,
					DocumentID:      docID,
					ChunkID:         fmt.Sprintf("%s-chunk-%d", docID, candidate.index),
				}},
				AnswerType: classifyAnswerType(answer),
				Difficulty: classifyDifficulty(answer),
				Notes:      fmt.Sprintf("auto-generated from %s", docName),
			})
			caseIndex++
		}
	}

	sort.Slice(cases, func(i, j int) bool { return cases[i].ID < cases[j].ID })
	return cases, nil
}

type paragraphCandidate struct {
	index      int
	raw        string
	text       string
	title      string
	score      int
	question   string
	topicKey   string
	hasNumber  bool
	hasList    bool
	answerType string
}

func collectParagraphCandidates(paragraphs []string) []paragraphCandidate {
	candidates := make([]paragraphCandidate, 0, len(paragraphs))
	for i, p := range paragraphs {
		candidate, ok := buildParagraphCandidate(p, i)
		if !ok {
			continue
		}
		candidates = append(candidates, candidate)
	}
	return candidates
}

func buildParagraphCandidate(paragraph string, index int) (paragraphCandidate, bool) {
	raw := strings.TrimSpace(paragraph)
	if utf8.RuneCountInString(raw) < 30 {
		return paragraphCandidate{}, false
	}
	text := normalizeInline(raw)
	if !isUsefulParagraph(raw, text) {
		return paragraphCandidate{}, false
	}

	title := extractParagraphTitle(raw)
	hasNumber := containsMeaningfulNumber(text)
	hasList := looksLikeList(raw, text)
	score := paragraphPriorityScore(raw, text, title, hasNumber, hasList, index)
	question, answerType, topicKey := chooseQuestion(text, title)
	if strings.TrimSpace(question) == "" {
		question = fallbackQuestion(title, index)
	}

	return paragraphCandidate{
		index:      index,
		raw:        raw,
		text:       text,
		title:      title,
		score:      score,
		question:   question,
		topicKey:   topicKey,
		hasNumber:  hasNumber,
		hasList:    hasList,
		answerType: answerType,
	}, true
}

func selectParagraphCandidates(candidates []paragraphCandidate, maxPerDoc int) []paragraphCandidate {
	if len(candidates) == 0 || maxPerDoc <= 0 {
		return nil
	}

	sorted := append([]paragraphCandidate(nil), candidates...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].score == sorted[j].score {
			return sorted[i].index < sorted[j].index
		}
		return sorted[i].score > sorted[j].score
	})

	selected := make([]paragraphCandidate, 0, minInt(maxPerDoc, len(sorted)))
	topicSeen := map[string]bool{}
	typeSeen := map[string]int{}

	for _, candidate := range sorted {
		if len(selected) >= maxPerDoc {
			break
		}
		if topicSeen[candidate.topicKey] {
			continue
		}
		if typeSeen[candidate.answerType] >= maxPerDoc/2+1 && len(sorted) > maxPerDoc {
			continue
		}
		selected = append(selected, candidate)
		topicSeen[candidate.topicKey] = true
		typeSeen[candidate.answerType]++
	}

	if len(selected) < maxPerDoc {
		selectedIndexes := map[int]bool{}
		for _, candidate := range selected {
			selectedIndexes[candidate.index] = true
		}
		for _, candidate := range sorted {
			if len(selected) >= maxPerDoc {
				break
			}
			if selectedIndexes[candidate.index] {
				continue
			}
			selected = append(selected, candidate)
		}
	}

	sort.Slice(selected, func(i, j int) bool { return selected[i].index < selected[j].index })
	return selected
}

type stateDocInfo struct {
	kbID    string
	docID   string
	docName string
}

func loadStateDocuments(statePath string) map[string]stateDocInfo {
	result := map[string]stateDocInfo{}
	content, err := os.ReadFile(statePath)
	if err != nil {
		return result
	}
	var state appState
	if err := json.Unmarshal(content, &state); err != nil {
		return result
	}
	for kbID, kb := range state.KnowledgeBases {
		for _, doc := range kb.Documents {
			if strings.TrimSpace(doc.Path) == "" {
				continue
			}
			info := stateDocInfo{
				kbID:    firstNonEmpty(doc.KnowledgeBaseID, kb.ID, kbID),
				docID:   firstNonEmpty(doc.ID, sanitizeID(doc.Name)),
				docName: firstNonEmpty(doc.Name, filepath.Base(doc.Path)),
			}
			for _, key := range candidateDocumentPaths(doc.Path) {
				result[key] = info
			}
		}
	}
	return result
}

func collectSupportedFiles(root string) ([]string, error) {
	files := make([]string, 0)
	info, err := os.Stat(root)
	if err != nil {
		if os.IsNotExist(err) {
			return files, nil
		}
		return nil, err
	}
	if !info.IsDir() {
		return files, nil
	}
	allowed := map[string]bool{".txt": true, ".md": true, ".markdown": true, ".pdf": true}
	err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		if allowed[strings.ToLower(filepath.Ext(path))] {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}

func matchDocument(path string, docs map[string]stateDocInfo) (docID, kbID, docName string) {
	for _, key := range candidateDocumentPaths(path) {
		if info, ok := docs[key]; ok {
			return info.docID, info.kbID, info.docName
		}
	}
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	base = sanitizeID(base)
	return "doc-" + base, "kb-auto", filepath.Base(path)
}

func candidateDocumentPaths(path string) []string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return nil
	}

	clean := filepath.Clean(filepath.FromSlash(trimmed))
	candidates := []string{clean}

	if !filepath.IsAbs(clean) {
		candidates = append(candidates, filepath.Clean(filepath.Join("backend", clean)))
	}

	for _, prefix := range []string{"backend/", "data/", "backend/data/"} {
		normalizedPrefix := filepath.Clean(filepath.FromSlash(prefix))
		normalizedPrefix = strings.TrimSuffix(normalizedPrefix, string(filepath.Separator))
		if strings.HasPrefix(clean, normalizedPrefix+string(filepath.Separator)) {
			rel := strings.TrimPrefix(clean, normalizedPrefix+string(filepath.Separator))
			candidates = append(candidates,
				filepath.Clean(filepath.Join("data", rel)),
				filepath.Clean(filepath.Join("backend", "data", rel)),
			)
		}
	}

	seen := make(map[string]struct{}, len(candidates))
	unique := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		candidate = filepath.Clean(candidate)
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		unique = append(unique, candidate)
	}
	return unique
}

func splitParagraphs(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	parts := regexp.MustCompile(`\n\s*\n+`).Split(text, -1)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	return out
}

func buildQuestion(docName string, candidate paragraphCandidate) string {
	if strings.TrimSpace(candidate.question) != "" {
		return candidate.question
	}
	return fallbackQuestion(strings.TrimSuffix(docName, filepath.Ext(docName)), candidate.index)
}

func buildSnippets(candidate paragraphCandidate) []string {
	segments := snippetSegments(candidate.raw, candidate.text)
	seen := map[string]struct{}{}
	result := make([]string, 0, 2)
	for _, segment := range segments {
		snippet := cleanSnippet(segment)
		if !isGoodSnippet(snippet) {
			continue
		}
		if _, ok := seen[snippet]; ok {
			continue
		}
		seen[snippet] = struct{}{}
		result = append(result, snippet)
		if len(result) >= 2 {
			break
		}
	}
	return result
}

func chooseQuestion(text, title string) (question, answerType, topicKey string) {
	subject := deriveSubject(text, title)
	if subject == "" {
		subject = title
	}
	if subject == "" {
		subject = firstKeyword(text)
	}
	subject = normalizeSubject(subject)
	topicKey = sanitizeID(firstNonEmpty(title, subject, firstKeyword(text)))
	answerType = "general"
	if subject == "" {
		return "", answerType, topicKey
	}

	lowerTitle := strings.ToLower(title)
	lowerText := strings.ToLower(text)

	switch {
	case strings.Contains(lowerTitle, "什么是") || regexp.MustCompile(`^\s*(Redis|缓存击透|缓存击穿|缓存雪崩|武汉大学|武汉⼤学).{0,8}(是|指)`).MatchString(text):
		cleanSubject := trimLeadingQuestionCue(subject)
		if cleanSubject == "" {
			cleanSubject = subject
		}
		return fmt.Sprintf("什么是%s？", cleanSubject), "definition", sanitizeID(cleanSubject)
	case regexp.MustCompile(`工作流程|流程|过程|步骤`).MatchString(title) || regexp.MustCompile(`工作流程|流程`).MatchString(text):
		return fmt.Sprintf("%s的工作流程是什么？", trimTrailingCue(subject, "流程", "工作流程")), "process", topicKey
	case strings.Contains(lowerTitle, "区别") || regexp.MustCompile(`和.*区别`).MatchString(title):
		left, right := splitComparisonSubject(subject, title)
		if left != "" && right != "" {
			return fmt.Sprintf("%s和%s有什么区别？", left, right), "comparison", sanitizeID(left + "-vs-" + right)
		}
		return fmt.Sprintf("%s有什么区别？", trimTrailingCue(subject, "区别")), "comparison", topicKey
	case shouldAskNumericQuestion(text, title, subject):
		metric := detectNumericMetric(text, title)
		target := numericQuestionTarget(subject, title, metric)
		if metric != "" && target != "" {
			return fmt.Sprintf("%s有多少%s？", target, metric), "numeric", sanitizeID(target + "-" + metric)
		}
		return fmt.Sprintf("关于%s，文中提到了哪些关键数字？", trimToQuestionSubject(subject)), "numeric", topicKey
	case looksLikeEnumeration(text, title):
		if strings.Contains(lowerTitle, "优势") || strings.Contains(lowerText, "优势") {
			return fmt.Sprintf("%s有哪些优势？", trimTrailingCue(subject, "的优势", "优势")), "listing", sanitizeID(subject + "-advantages")
		}
		if strings.Contains(lowerTitle, "作用") || strings.Contains(lowerText, "作用") {
			return fmt.Sprintf("%s有哪些作用？", trimTrailingCue(subject, "的作用", "作用")), "listing", sanitizeID(subject + "-functions")
		}
		if strings.Contains(lowerTitle, "学科") || strings.Contains(lowerText, "涵盖") {
			target := numericQuestionTarget(subject, title, "")
			if target == "" {
				target = trimToQuestionSubject(subject)
			}
			return fmt.Sprintf("%s涵盖哪些学科门类？", target), "structure", sanitizeID(target + "-disciplines")
		}
		if strings.Contains(lowerTitle, "方式") || strings.Contains(lowerText, "模式") {
			return fmt.Sprintf("%s有哪些方式或模式？", trimTrailingCue(subject, "的方式", "方式", "的模式", "模式")), "listing", sanitizeID(subject + "-modes")
		}
		return fmt.Sprintf("%s包括哪些要点？", trimToQuestionSubject(subject)), "listing", topicKey
	case regexp.MustCompile(`作用|功能|用于|负责|场景`).MatchString(text) || regexp.MustCompile(`作用|功能`).MatchString(title):
		return fmt.Sprintf("%s有哪些作用？", trimTrailingCue(subject, "的作用", "作用", "功能")), "function", sanitizeID(subject + "-functions")
	case regexp.MustCompile(`优缺点|优点|缺点`).MatchString(text) || regexp.MustCompile(`优缺点`).MatchString(title):
		return fmt.Sprintf("%s有哪些优缺点？", trimTrailingCue(subject, "优缺点", "优点", "缺点")), "proscons", sanitizeID(subject + "-proscons")
	default:
		return fmt.Sprintf("%s主要讲了什么？", trimToQuestionSubject(subject)), answerType, topicKey
	}
}

func fallbackQuestion(subject string, index int) string {
	subject = normalizeSubject(subject)
	if subject != "" {
		return fmt.Sprintf("%s主要讲了什么？", subject)
	}
	return fmt.Sprintf("第 %d 段主要讲了什么？", index+1)
}

func firstKeyword(text string) string {
	text = normalizeInline(text)
	words := regexp.MustCompile(`[\p{Han}A-Za-z0-9_-]{2,}`).FindAllString(text, -1)
	for _, w := range words {
		if isStopKeyword(w) {
			continue
		}
		if utf8.RuneCountInString(w) >= 2 {
			return w
		}
	}
	return ""
}

func isUsefulParagraph(raw, text string) bool {
	if utf8.RuneCountInString(text) < 30 {
		return false
	}
	if isMarkdownTable(text) || isMostlyMarkdown(raw) {
		return false
	}
	if onlyTitleLike(raw, text) {
		return false
	}
	score := 0
	if containsMeaningfulNumber(text) {
		score += 2
	}
	if looksLikeList(raw, text) {
		score += 2
	}
	if regexp.MustCompile(`什么是|是指|指的是|负责|包括|涵盖|支持|具有|分为|提供|优势|作用|机制|流程|区别|优缺点`).MatchString(text) {
		score += 2
	}
	if utf8.RuneCountInString(text) >= 60 {
		score++
	}
	if regexp.MustCompile(`特点|常用命令|命令|联系方式|示例|目录|总结`).MatchString(extractParagraphTitle(raw)) && score < 4 {
		return false
	}
	return score >= 2
}

func paragraphPriorityScore(raw, text, title string, hasNumber, hasList bool, index int) int {
	score := 0
	if hasNumber {
		score += 4
	}
	if hasList {
		score += 3
	}
	if regexp.MustCompile(`什么是|是指|指的是|简介|概况`).MatchString(text) {
		score += 4
	}
	if regexp.MustCompile(`工作流程|流程|机制|步骤`).MatchString(text) {
		score += 4
	}
	if regexp.MustCompile(`优势|作用|区别|优缺点|涵盖|包括|分为|支持|模式`).MatchString(text + " " + title) {
		score += 3
	}
	if regexp.MustCompile(`特点|常用命令|联系方式|示例`).MatchString(title) {
		score -= 3
	}
	if index >= 2 {
		score += 1
	}
	if index >= 5 {
		score += 1
	}
	return score
}

func extractParagraphTitle(raw string) string {
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		line = regexp.MustCompile(`^[#*\-\d.\s]+`).ReplaceAllString(line, "")
		line = strings.TrimSpace(strings.Trim(line, "：:[]（）()"))
		if line != "" {
			return line
		}
	}
	return ""
}

func deriveSubject(text, title string) string {
	title = strings.TrimSpace(title)
	if title != "" {
		cleanTitle := regexp.MustCompile(`^[一二三四五六七八九十0-9、.\s]+`).ReplaceAllString(title, "")
		cleanTitle = regexp.MustCompile(`^(什么是|关于)`).ReplaceAllString(cleanTitle, "")
		cleanTitle = normalizeInline(cleanTitle)
		if idx := strings.IndexAny(cleanTitle, "：:，。；;（("); idx > 0 {
			cleanTitle = strings.TrimSpace(cleanTitle[:idx])
		}
		if strings.Contains(strings.ToLower(cleanTitle), "redis") && strings.Contains(cleanTitle, "变慢") {
			return "Redis变慢"
		}
		if strings.HasPrefix(strings.ToLower(cleanTitle), "redis") {
			rest := strings.TrimSpace(cleanTitle[len("redis"):])
			if rest == "" {
				return "Redis"
			}
			cleanTitle = "Redis" + rest
		}
		cleanTitle = trimTrailingCue(cleanTitle, "的优势", "优势", "的区别", "区别", "的作用", "作用", "的方式", "方式", "的模式", "模式", "的工作流程", "工作流程", "介绍")
		cleanTitle = trimToQuestionSubject(cleanTitle)
		if cleanTitle != "" && !isGenericTopic(cleanTitle) {
			return cleanTitle
		}
	}
	return extractSubjectFromText(text)
}

func normalizeSubject(subject string) string {
	subject = strings.TrimSpace(subject)
	subject = strings.Trim(subject, "：:。；，、[]（）()")
	subject = regexp.MustCompile(`\s+`).ReplaceAllString(subject, "")
	if strings.EqualFold(subject, "redis") {
		return "Redis"
	}
	if strings.HasPrefix(strings.ToLower(subject), "redis") {
		rest := subject[len("redis"):]
		return "Redis" + rest
	}
	return subject
}

func containsMeaningfulNumber(text string) bool {
	return regexp.MustCompile(`\d+\s*(余|个|位|所|年|万|%|秒|学科|学院|国家|槽|种)?`).MatchString(text)
}

func looksLikeList(raw, text string) bool {
	return regexp.MustCompile(`(?m)^\s*[-*•]|[：:](\s*[-*•]|\s*\d+[.、])|\b1[.、]|\b2[.、]|包括|分为|如下|有两种|有\d+种`).MatchString(raw) ||
		regexp.MustCompile(`、`).FindAllString(text, -1) != nil && len(regexp.MustCompile(`、`).FindAllString(text, -1)) >= 2
}

func looksLikeEnumeration(text, title string) bool {
	return looksLikeList(text, text) || regexp.MustCompile(`优势|作用|方式|模式|学科|包括|涵盖|分为|类型|优缺点`).MatchString(title+text)
}

func detectNumericMetric(text, title string) string {
	patterns := []string{"专任教师", "正副教授", "院士", "学科门类", "孔子学院", "重点实验室", "国家重点学科", "科研机构", "合作关系", "学院", "游客", "人才", "哈希槽"}
	for _, p := range patterns {
		if strings.Contains(text, p) || strings.Contains(title, p) {
			return p
		}
	}
	return ""
}

func splitComparisonSubject(subject, title string) (string, string) {
	text := firstNonEmpty(title, subject)
	matches := regexp.MustCompile(`([A-Za-z\p{Han}0-9]+)和([A-Za-z\p{Han}0-9]+)`).FindStringSubmatch(text)
	if len(matches) == 3 {
		return normalizeSubject(matches[1]), normalizeSubject(matches[2])
	}
	return "", ""
}

func snippetSegments(raw, text string) []string {
	segments := make([]string, 0, 6)
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	for _, line := range lines {
		line = normalizeInline(line)
		if isGoodSnippet(line) {
			segments = append(segments, line)
		}
	}
	if len(segments) == 0 {
		clauses := regexp.MustCompile(`[。；;！？?!]`).Split(text, -1)
		for _, clause := range clauses {
			clause = normalizeInline(clause)
			if isGoodSnippet(clause) {
				segments = append(segments, clause)
			}
		}
	}
	sort.SliceStable(segments, func(i, j int) bool {
		return snippetScore(segments[i]) > snippetScore(segments[j])
	})
	return segments
}

func snippetScore(text string) int {
	score := 0
	if containsMeaningfulNumber(text) {
		score += 4
	}
	if regexp.MustCompile(`什么是|是指|指的是|包括|涵盖|支持|具有|负责|作用|优势|区别|优缺点|模式|机制`).MatchString(text) {
		score += 3
	}
	if utf8.RuneCountInString(text) >= 20 && utf8.RuneCountInString(text) <= 60 {
		score += 2
	}
	return score
}

func cleanSnippet(text string) string {
	text = normalizeInline(text)
	text = regexp.MustCompile(`^[#*\-|\d.\s]+`).ReplaceAllString(text, "")
	text = strings.TrimSpace(strings.Trim(text, "：:|"))
	if utf8.RuneCountInString(text) > 60 {
		text = clipRunes(text, 60)
	}
	if utf8.RuneCountInString(text) < 20 {
		return ""
	}
	return text
}

func isGoodSnippet(text string) bool {
	text = cleanSnippet(text)
	if text == "" {
		return false
	}
	if isMarkdownTable(text) || onlyTitleLike(text, text) {
		return false
	}
	return true
}

func isMarkdownTable(text string) bool {
	return strings.Count(text, "|") >= 3 || regexp.MustCompile(`(?m)^\|?[-: ]+\|[-|: ]+$`).MatchString(text)
}

func isMostlyMarkdown(text string) bool {
	stripped := regexp.MustCompile(`[\p{Han}A-Za-z0-9]`).ReplaceAllString(text, "")
	return utf8.RuneCountInString(strings.TrimSpace(stripped)) > utf8.RuneCountInString(text)/2
}

func onlyTitleLike(raw, text string) bool {
	if strings.Contains(raw, "\n") {
		return false
	}
	if utf8.RuneCountInString(text) > 24 {
		return false
	}
	return !regexp.MustCompile(`[，。；;：:]`).MatchString(text)
}

func isStopKeyword(word string) bool {
	stops := map[string]struct{}{
		"文档": {}, "内容": {}, "关于": {}, "特点": {}, "常用命令": {}, "使用场景": {},
		"总结": {}, "学校": {}, "其中": {}, "一个": {}, "这种": {}, "这种情况": {},
	}
	_, ok := stops[word]
	return ok
}

func isGenericTopic(topic string) bool {
	return regexp.MustCompile(`^(特点|常用命令|使用场景|总结|简介|概况|内容)$`).MatchString(topic)
}

func shouldAskNumericQuestion(text, title, subject string) bool {
	if !containsMeaningfulNumber(text) {
		return false
	}
	if detectNumericMetric(text, title) == "" {
		return false
	}
	if regexp.MustCompile(`什么是|是指|指的是|区别|优缺点|作用|工作流程|流程|机制`).MatchString(title + text) {
		return false
	}
	if strings.Contains(subject, "缓存击穿") || strings.Contains(subject, "缓存雪崩") || strings.Contains(subject, "缓存击透") {
		return false
	}
	return true
}

func numericQuestionTarget(subject, title, metric string) string {
	for _, candidate := range []string{title, subject, extractSubjectFromText(title), extractSubjectFromText(subject)} {
		candidate = trimToQuestionSubject(candidate)
		candidate = trimTrailingCue(candidate, metric, "学科门类", "学院", "专任教师", "正副教授", "院士", "重点实验室", "科研机构", "孔子学院", "人才", "合作关系")
		candidate = strings.TrimSpace(candidate)
		if candidate == "" || isGenericTopic(candidate) {
			continue
		}
		if utf8.RuneCountInString(candidate) > 18 {
			continue
		}
		return candidate
	}
	return ""
}

func extractSubjectFromText(text string) string {
	patterns := []string{
		`^(Redis)[是可会]`,
		`^(武汉大学)[（(]`,
		`^(武汉⼤学)[（(]`,
		`^(缓存击透、缓存击穿、缓存雪崩)`,
		`^(缓存击透)`,
		`^(缓存击穿)`,
		`^(缓存雪崩)`,
	}
	for _, pattern := range patterns {
		if match := regexp.MustCompile(pattern).FindStringSubmatch(text); len(match) > 1 {
			return match[1]
		}
	}
	text = normalizeInline(text)
	if strings.Contains(text, "武汉大学") {
		return "武汉大学"
	}
	if strings.Contains(text, "武汉⼤学") {
		return "武汉⼤学"
	}
	if strings.Contains(strings.ToLower(text), "redis") {
		return "Redis"
	}
	if idx := strings.IndexAny(text, "，。；：:（("); idx > 0 {
		text = text[:idx]
	}
	if utf8.RuneCountInString(text) > 18 {
		text = clipRunes(text, 18)
	}
	return firstKeyword(text)
}

func trimLeadingQuestionCue(subject string) string {
	subject = strings.TrimSpace(subject)
	subject = regexp.MustCompile(`^(什么是|关于)`).ReplaceAllString(subject, "")
	return strings.TrimSpace(subject)
}

func trimTrailingCue(subject string, suffixes ...string) string {
	subject = strings.TrimSpace(subject)
	for _, suffix := range suffixes {
		subject = strings.TrimSpace(strings.TrimSuffix(subject, suffix))
	}
	subject = strings.TrimSuffix(subject, "的")
	return strings.TrimSpace(subject)
}

func trimToQuestionSubject(subject string) string {
	subject = trimLeadingQuestionCue(normalizeSubject(subject))
	subject = regexp.MustCompile(`^(百余年来|近年来|目前|如今|如果)`).ReplaceAllString(subject, "")
	subject = trimTrailingCue(subject,
		"的优势", "优势", "的区别", "区别", "的作用", "作用", "的方式", "方式", "的模式", "模式",
		"的工作流程", "工作流程", "流程", "介绍", "概况", "简介", "师资力量雄厚", "学科门类齐全", "科研实力雄厚",
		"与世界上", "回收策略（淘汰策略）", "回收策略", "部署方式", "哨兵模式介绍",
	)
	subject = normalizeInline(subject)
	if idx := strings.IndexAny(subject, "，。；：:（("); idx > 0 {
		subject = strings.TrimSpace(subject[:idx])
	}
	if subject == "" {
		return ""
	}
	if strings.Contains(strings.ToLower(subject), "redis") && subject != "Redis" && !strings.HasPrefix(subject, "Redis") {
		subject = "Redis" + strings.TrimPrefix(strings.ToLower(subject), "redis")
	}
	return strings.TrimSpace(subject)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func classifyAnswerType(answer string) string {
	if regexp.MustCompile(`\b\d+\b`).MatchString(answer) {
		return "numeric"
	}
	if strings.HasPrefix(answer, "是") || strings.HasPrefix(answer, "否") {
		return "yesno"
	}
	if utf8.RuneCountInString(answer) > 90 {
		return "abstractive"
	}
	return "extractive"
}

func classifyDifficulty(answer string) string {
	if utf8.RuneCountInString(answer) > 100 {
		return "medium"
	}
	return "easy"
}

func normalizeInline(s string) string {
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = regexp.MustCompile(`\s+`).ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

func clipRunes(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r)
	}
	return strings.TrimSpace(string(r[:n]))
}

func sanitizeID(s string) string {
	s = strings.ToLower(s)
	s = regexp.MustCompile(`[^a-z0-9_-]+`).ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		return "auto"
	}
	return s
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
