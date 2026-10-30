package service

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"ai-localbase/internal/model"
	"ai-localbase/internal/util"
)

const (
	documentDetailRawContentLimit = 20000
	documentDetailChunkLimit      = 30
	documentDetailChunkTextLimit  = 1200
)

// GetDocumentDetail 返回受限长度的原文、分块预览和实时索引诊断。
func (s *AppService) GetDocumentDetail(knowledgeBaseID, documentID, focusChunkID string) (model.DocumentDetailResponse, error) {
	if s == nil || s.rag == nil {
		return model.DocumentDetailResponse{}, fmt.Errorf("app service is nil")
	}

	document, err := s.GetDocument(strings.TrimSpace(knowledgeBaseID), strings.TrimSpace(documentID))
	if err != nil {
		return model.DocumentDetailResponse{}, err
	}
	content, err := util.ExtractDocumentText(document.Path)
	if err != nil {
		return model.DocumentDetailResponse{}, fmt.Errorf("extract document text: %w", err)
	}

	chunks := s.rag.BuildDocumentChunks(document, content)
	return buildDocumentDetailResponse(s, document, content, chunks, focusChunkID), nil
}

// GetKnowledgeBaseHealth 汇总知识库及其文档的索引健康状态。
func (s *AppService) GetKnowledgeBaseHealth(knowledgeBaseID string) (model.KnowledgeBaseHealthResponse, error) {
	knowledgeBaseID = strings.TrimSpace(knowledgeBaseID)
	if knowledgeBaseID == "" {
		return model.KnowledgeBaseHealthResponse{}, fmt.Errorf("knowledge base id is required")
	}
	if s == nil || s.state == nil {
		return model.KnowledgeBaseHealthResponse{}, fmt.Errorf("app service is nil")
	}

	s.state.Mu.RLock()
	kb, ok := s.state.KnowledgeBases[knowledgeBaseID]
	if ok {
		kb.Documents = append([]model.Document(nil), kb.Documents...)
	}
	s.state.Mu.RUnlock()
	if !ok {
		return model.KnowledgeBaseHealthResponse{}, fmt.Errorf("knowledge base not found")
	}

	metrics := model.KnowledgeBaseHealthMetrics{
		DocumentCount:     len(kb.Documents),
		QdrantEnabled:     s.qdrant != nil && s.qdrant.IsEnabled(),
		VectorCountSource: "not_applicable",
		VectorCountStatus: "not_applicable",
	}
	documents := make([]model.KnowledgeBaseDocumentHealth, 0, len(kb.Documents))
	needsReindexCount := 0
	for _, document := range kb.Documents {
		item := s.buildKnowledgeBaseDocumentHealth(document)
		documents = append(documents, item)

		switch document.Status {
		case "indexed":
			metrics.IndexedCount++
		case "processing":
			metrics.ProcessingCount++
		}
		if strings.TrimSpace(document.IndexError) != "" || document.Status == "failed" {
			metrics.FailedCount++
		}
		if !item.RawContentAvailable {
			metrics.EmptyContentCount++
		}
		if item.NeedsReindex {
			needsReindexCount++
		}
		metrics.ChunkCount += item.ChunkCount
		metrics.VectorCount += item.VectorCount
		// 任一文档查询失败，汇总数量只能视为部分结果，不能显示为精确总数。
		if item.VectorCountSource == "unknown" {
			metrics.VectorCountSource = "unknown"
			metrics.VectorCountStatus = item.VectorCountStatus
		} else if item.VectorCountSource == "actual" && metrics.VectorCountSource != "unknown" {
			metrics.VectorCountSource = "actual"
			metrics.VectorCountStatus = "ok"
		}
		metrics.SummaryChunkCount += item.SummaryChunkCount
		metrics.StructuredRowCount += item.StructuredRowCount
		metrics.RawContentChars += item.RawContentChars
		if isLaterRFC3339(item.IndexedAt, metrics.LastIndexedAt) {
			metrics.LastIndexedAt = item.IndexedAt
		}
	}

	score := knowledgeBaseHealthScore(metrics, needsReindexCount)
	return model.KnowledgeBaseHealthResponse{
		KnowledgeBaseID: kb.ID,
		Name:            kb.Name,
		Status:          knowledgeBaseHealthStatus(score, metrics, needsReindexCount),
		Score:           score,
		Metrics:         metrics,
		Recommendations: knowledgeBaseHealthRecommendations(metrics, needsReindexCount),
		Documents:       documents,
	}, nil
}

// ReindexDocument 使用现有源文件重建单个文档，不删除或重建 Qdrant 集合。
func (s *AppService) ReindexDocument(knowledgeBaseID, documentID string) (model.Document, error) {
	if s == nil {
		return model.Document{}, fmt.Errorf("app service is nil")
	}

	if s.qdrant == nil || !s.qdrant.IsEnabled() {
		return model.Document{}, fmt.Errorf("重建向量失败：Qdrant 未启用")
	}

	document, err := s.GetDocument(strings.TrimSpace(knowledgeBaseID), strings.TrimSpace(documentID))
	if err != nil {
		return model.Document{}, err
	}
	if document.Wiki != nil {
		return s.indexWikiPage(knowledgeBaseID, documentID)
	}
	unlock := s.lockDocument(knowledgeBaseID, documentID)
	defer unlock()
	document, err = s.GetDocument(knowledgeBaseID, documentID)
	if err != nil {
		return model.Document{}, err
	}
	if strings.TrimSpace(document.Path) == "" {
		return model.Document{}, fmt.Errorf("document path is empty")
	}

	content, err := util.ExtractDocumentText(document.Path)
	if err != nil {
		if persistErr := s.persistReindexError(document, err, false); persistErr != nil {
			return model.Document{}, fmt.Errorf("extract document text: %w; persist reindex error: %v", err, persistErr)
		}
		return model.Document{}, fmt.Errorf("extract document text: %w", err)
	}
	originalChecksum, originalStatus := document.ContentChecksum, document.Status
	// 手工重建同样接入来源版本，不能让磁盘变化绕过知识页失效。
	document.ContentChecksum = hashDocumentContent(content)
	document, err = s.snapshotWikiSource(document, content)
	if err != nil {
		return model.Document{}, err
	}
	if err := s.recordWikiSourceChange(document); err != nil {
		return model.Document{}, err
	}
	markdownPath, err := s.writeMarkdownArchive(document, content)
	if err != nil {
		if persistErr := s.persistReindexError(document, err, false); persistErr != nil {
			return model.Document{}, fmt.Errorf("%w; persist reindex error: %v", err, persistErr)
		}
		return model.Document{}, err
	}
	document.MarkdownPath = markdownPath
	document.Size = int64(len([]byte(content)))
	document.SizeLabel = util.FormatFileSize(document.Size)

	document.Status = "processing"
	document.IndexError = ""
	indexed, err := s.reindexExistingDocument(document, content)
	if err != nil {
		document.Status = originalStatus
		if persistErr := s.persistReindexError(document, err, originalChecksum != document.ContentChecksum); persistErr != nil {
			return model.Document{}, fmt.Errorf("%w; persist reindex error: %v", err, persistErr)
		}
		return model.Document{}, err
	}
	s.enqueueWikiSource(indexed)
	return indexed, nil
}

// persistReindexError 保留重建失败证据；旧 points 已删除时同步标记文档索引不可用。
func (s *AppService) persistReindexError(document model.Document, indexErr error, pointsDeleted bool) error {
	if s == nil || indexErr == nil {
		return nil
	}
	document.IndexError = indexErr.Error()
	document.IndexErrorCode = classifyIndexErrorCode(indexErr)
	if pointsDeleted {
		document.Status = "failed"
		document.ChunkCount = 0
		document.IndexedAt = ""
	}
	_, err := s.ReplaceDocument(document.KnowledgeBaseID, document)
	return err
}

// buildDocumentDetailResponse 截断原文和分块预览，额外保留引用目标；向量数量只采用远端实测。
func buildDocumentDetailResponse(s *AppService, document model.Document, content string, chunks []DocumentChunk, focusChunkID string) model.DocumentDetailResponse {
	rawContent := strings.TrimSpace(content)
	rawContentTruncated := false
	if len([]rune(rawContent)) > documentDetailRawContentLimit {
		rawContent = truncateDocumentRunes(rawContent, documentDetailRawContentLimit)
		rawContentTruncated = true
	}

	chunkPreviews := make([]model.DocumentChunkPreview, 0, minInt(len(chunks), documentDetailChunkLimit))
	for index, chunk := range chunks {
		if index >= documentDetailChunkLimit {
			continue
		}
		chunkPreviews = append(chunkPreviews, buildDocumentChunkPreview(document, chunk))
	}

	focusChunkID = strings.TrimSpace(focusChunkID)
	if focusChunkID != "" && !documentChunkPreviewContains(chunkPreviews, focusChunkID) {
		for _, chunk := range chunks {
			if chunk.ID == focusChunkID {
				chunkPreviews = append(chunkPreviews, buildDocumentChunkPreview(document, chunk))
				break
			}
		}
	}

	vectorHealth := VectorHealth{Source: "not_applicable", Status: "not_applicable"}
	if s != nil {
		vectorHealth = s.qdrant.InspectDocumentVectors(context.Background(), document)
	}
	summary := strings.TrimSpace(document.ContentPreview)
	if summary == "" && len(chunks) > 0 {
		summary = util.BuildContentPreviewFromText(chunks[0].Text)
	}
	summaryChunkCount, structuredRowCount := structuredTableMetrics(document, content)

	return model.DocumentDetailResponse{
		KnowledgeBaseID: document.KnowledgeBaseID,
		Document:        document,
		Diagnostics: model.DocumentIndexDiagnostics{
			RawContentChars:       len([]rune(content)),
			ChunkCount:            len(chunks),
			VectorCount:           vectorHealth.Count,
			VectorCountSource:     vectorHealth.Source,
			VectorCountStatus:     vectorHealth.Status,
			SummaryChunkCount:     summaryChunkCount,
			StructuredRowCount:    structuredRowCount,
			RawContentAvailable:   strings.TrimSpace(content) != "",
			QdrantEnabled:         s != nil && s.qdrant != nil && s.qdrant.IsEnabled(),
			RawContentTruncated:   rawContentTruncated,
			ChunkPreviewTruncated: len(chunks) > documentDetailChunkLimit,
		},
		RawContent: rawContent,
		Summary:    summary,
		Chunks:     chunkPreviews,
	}
}

func buildDocumentChunkPreview(document model.Document, chunk DocumentChunk) model.DocumentChunkPreview {
	return model.DocumentChunkPreview{
		ID:    chunk.ID,
		Index: chunk.Index,
		Kind:  documentChunkKind(document, chunk.Text),
		Text:  truncateDocumentRunes(strings.TrimSpace(chunk.Text), documentDetailChunkTextLimit),
	}
}

// buildKnowledgeBaseDocumentHealth 对照原文、分块与实际向量状态生成建议，查询失败保留 unknown。
func (s *AppService) buildKnowledgeBaseDocumentHealth(document model.Document) model.KnowledgeBaseDocumentHealth {
	item := model.KnowledgeBaseDocumentHealth{
		DocumentID:   document.ID,
		DocumentName: document.Name,
		Status:       document.Status,
		IndexedAt:    document.IndexedAt,
		IndexError:   document.IndexError,
		ChunkCount:   document.ChunkCount,
	}

	content, err := util.ExtractDocumentText(document.Path)
	if err == nil {
		item.RawContentChars = len([]rune(content))
		item.RawContentAvailable = strings.TrimSpace(content) != ""
		if s != nil && s.rag != nil {
			item.ChunkCount = len(s.rag.BuildDocumentChunks(document, content))
		}
		item.SummaryChunkCount, item.StructuredRowCount = structuredTableMetrics(document, content)
	} else {
		item.Recommendation = "无法读取原始文件，建议检查文件是否仍存在后重新上传。"
	}

	vectorHealth := s.qdrant.InspectDocumentVectors(context.Background(), document)
	item.VectorCount, item.VectorCountSource, item.VectorCountStatus = vectorHealth.Count, vectorHealth.Source, vectorHealth.Status
	item.NeedsReindex = documentNeedsReindex(document, item)
	if item.Recommendation == "" {
		item.Recommendation = documentHealthRecommendation(document, item)
	}
	return item
}

func documentChunkPreviewContains(chunks []model.DocumentChunkPreview, chunkID string) bool {
	for _, chunk := range chunks {
		if chunk.ID == chunkID {
			return true
		}
	}
	return false
}

// documentNeedsReindex 判断已确认的索引缺失、维度或数量不符，不将连接失败等同于零向量。
func documentNeedsReindex(document model.Document, health model.KnowledgeBaseDocumentHealth) bool {
	if health.VectorCountStatus == "collection_missing" || health.VectorCountStatus == "dimension_mismatch" || (health.VectorCountSource == "actual" && health.VectorCount != document.ChunkCount) {
		return true
	}
	if strings.TrimSpace(document.IndexError) != "" || document.Status != "indexed" {
		return true
	}
	if !health.RawContentAvailable || health.ChunkCount == 0 {
		return true
	}
	return strings.TrimSpace(document.IndexedAt) == ""
}

// documentHealthRecommendation 按远端诊断、索引错误和原文状态给出可执行处理建议。
func documentHealthRecommendation(document model.Document, health model.KnowledgeBaseDocumentHealth) string {
	switch {
	case health.VectorCountStatus == "unavailable":
		return "向量数量查询失败，请检查 Qdrant 连接后重试；当前数量未知。"
	case health.VectorCountStatus == "collection_missing":
		return "Qdrant 集合不存在，请重建索引。"
	case health.VectorCountStatus == "dimension_mismatch":
		return "向量维度与当前配置不一致，请核对模型及集合后迁移索引。"
	case health.VectorCountSource == "actual" && health.VectorCount != document.ChunkCount:
		return "实际向量数量与已记录分块数量不一致，建议重建索引。"
	case strings.TrimSpace(document.IndexError) != "":
		return "索引失败，建议查看错误信息后重建索引。"
	case document.Status == "processing":
		return "文档仍在处理中，完成后再观察健康度。"
	case document.Status != "indexed":
		return "文档尚未完成索引，建议重建索引。"
	case !health.RawContentAvailable:
		return "原文不可读或为空，建议重新上传文档。"
	case health.ChunkCount == 0:
		return "未生成 chunk，建议重建索引或检查文件内容。"
	default:
		return ""
	}
}

// knowledgeBaseHealthScore 汇总本地索引完整性与远端可确认程度；未知计数扣分但不视为实测零。
func knowledgeBaseHealthScore(metrics model.KnowledgeBaseHealthMetrics, needsReindexCount int) int {
	if metrics.DocumentCount == 0 {
		return 100
	}
	score := 100
	score -= metrics.FailedCount * 25
	score -= metrics.ProcessingCount * 10
	score -= metrics.EmptyContentCount * 15
	score -= needsReindexCount * 12
	if metrics.ChunkCount == 0 {
		score -= 25
	}
	if metrics.VectorCountSource == "unknown" || (metrics.VectorCountSource == "actual" && metrics.IndexedCount > 0 && metrics.VectorCount == 0) {
		score -= 20
	}
	if score < 0 {
		return 0
	}
	if score > 100 {
		return 100
	}
	return score
}

// knowledgeBaseHealthStatus 优先标记远端未知为 attention，避免部分计数显示健康。
func knowledgeBaseHealthStatus(score int, metrics model.KnowledgeBaseHealthMetrics, needsReindexCount int) string {
	switch {
	case metrics.VectorCountSource == "unknown":
		return "attention"
	case metrics.DocumentCount == 0:
		return "empty"
	case metrics.FailedCount > 0 || score < 60:
		return "attention"
	case metrics.ProcessingCount > 0 || needsReindexCount > 0 || score < 85:
		return "warning"
	default:
		return "healthy"
	}
}

// knowledgeBaseHealthRecommendations 汇总重建需求与查询限制，明确区分未知数量和实际为零。
func knowledgeBaseHealthRecommendations(metrics model.KnowledgeBaseHealthMetrics, needsReindexCount int) []string {
	if metrics.DocumentCount == 0 {
		return []string{"当前知识库暂无文档，上传文档后可生成索引健康度。"}
	}

	recommendations := make([]string, 0, 4)
	if metrics.FailedCount > 0 {
		recommendations = append(recommendations, fmt.Sprintf("%d 份文档索引失败，建议查看文档详情并重建索引。", metrics.FailedCount))
	}
	if metrics.ProcessingCount > 0 {
		recommendations = append(recommendations, fmt.Sprintf("%d 份文档仍在处理中，请等待完成后再评估检索效果。", metrics.ProcessingCount))
	}
	if metrics.EmptyContentCount > 0 {
		recommendations = append(recommendations, fmt.Sprintf("%d 份文档原文为空或不可读，建议重新上传。", metrics.EmptyContentCount))
	}
	if needsReindexCount > 0 {
		recommendations = append(recommendations, fmt.Sprintf("%d 份文档建议重建索引。", needsReindexCount))
	}
	if metrics.VectorCountSource == "unknown" {
		recommendations = append(recommendations, "Qdrant 诊断未能确认向量数量，请查看文档诊断状态，勿将未知数量视为零。")
	} else if metrics.VectorCountSource == "actual" && metrics.IndexedCount > 0 && metrics.VectorCount == 0 {
		recommendations = append(recommendations, "Qdrant 已启用但未统计到向量，建议重建相关文档索引。")
	}
	if len(recommendations) == 0 {
		recommendations = append(recommendations, "知识库索引状态良好，可继续观察实际检索命中质量。")
	}
	return recommendations
}

func isLaterRFC3339(candidate, current string) bool {
	candidate = strings.TrimSpace(candidate)
	if candidate == "" {
		return false
	}
	if strings.TrimSpace(current) == "" {
		return true
	}
	candidateTime, candidateErr := time.Parse(time.RFC3339, candidate)
	currentTime, currentErr := time.Parse(time.RFC3339, current)
	if candidateErr != nil || currentErr != nil {
		return candidate > current
	}
	return candidateTime.After(currentTime)
}

// structuredTableMetrics 复用现有 CSV/XLSX 抽取文本中的统计标记，避免修改 RAG 分块契约。
func structuredTableMetrics(document model.Document, content string) (int, int) {
	extension := strings.ToLower(filepath.Ext(document.Name))
	if extension != ".csv" && extension != ".xlsx" {
		return 0, 0
	}

	const rowCountMarker = "数据行数："
	summaryCount := 0
	rowCount := 0
	remainder := content
	for {
		markerIndex := strings.Index(remainder, rowCountMarker)
		if markerIndex < 0 {
			break
		}
		remainder = remainder[markerIndex+len(rowCountMarker):]
		var currentRows int
		if _, err := fmt.Sscanf(remainder, "%d", &currentRows); err == nil && currentRows >= 0 {
			summaryCount++
			rowCount += currentRows
		}
	}
	return summaryCount, rowCount
}

func documentChunkKind(document model.Document, text string) string {
	extension := strings.ToLower(filepath.Ext(document.Name))
	if extension != ".csv" && extension != ".xlsx" {
		return "text"
	}
	if strings.Contains(text, "字段：") && strings.Contains(text, "数据行数：") {
		return "structured_summary"
	}
	if strings.Contains(text, "行：") {
		return "structured_row"
	}
	return "text"
}

func truncateDocumentRunes(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "..."
}
