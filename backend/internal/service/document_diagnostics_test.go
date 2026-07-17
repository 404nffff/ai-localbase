package service

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ai-localbase/internal/model"
	"ai-localbase/internal/util"
)

func TestGetDocumentDetailLimitsContentAndIncludesFocusedChunk(t *testing.T) {
	service := NewAppService(nil, nil, nil, model.ServerConfig{})
	knowledgeBaseID := service.ListKnowledgeBases()[0].ID
	var contentBuilder strings.Builder
	for index := 0; index < 2500; index++ {
		fmt.Fprintf(&contentBuilder, "第%d段文档诊断内容需要保留可读分块并限制响应体积。\n", index+1)
	}
	content := contentBuilder.String()
	path := filepath.Join(t.TempDir(), "diagnostics.md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write diagnostics document: %v", err)
	}

	document := model.Document{
		ID:              "doc-diagnostics",
		KnowledgeBaseID: knowledgeBaseID,
		Name:            "diagnostics.md",
		Path:            path,
		Status:          "indexed",
		ContentPreview:  "诊断摘要",
		IndexedAt:       util.NowRFC3339(),
	}
	chunks := service.rag.BuildDocumentChunks(document, content)
	if len(chunks) <= documentDetailChunkLimit {
		t.Fatalf("expected more than %d chunks, got %d", documentDetailChunkLimit, len(chunks))
	}
	document.ChunkCount = len(chunks)
	service.AddDocument(knowledgeBaseID, document)

	focusChunkID := chunks[len(chunks)-1].ID
	detail, err := service.GetDocumentDetail(knowledgeBaseID, document.ID, focusChunkID)
	if err != nil {
		t.Fatalf("get document detail: %v", err)
	}
	if !detail.Diagnostics.RawContentTruncated {
		t.Fatal("expected raw content to be truncated")
	}
	if !detail.Diagnostics.ChunkPreviewTruncated {
		t.Fatal("expected chunk preview to be truncated")
	}
	if len([]rune(strings.TrimSuffix(detail.RawContent, "..."))) != documentDetailRawContentLimit {
		t.Fatalf("expected %d visible raw content runes, got %d", documentDetailRawContentLimit, len([]rune(detail.RawContent)))
	}
	if len(detail.Chunks) != documentDetailChunkLimit+1 {
		t.Fatalf("expected limited chunks plus focused chunk, got %d", len(detail.Chunks))
	}
	if !documentChunkPreviewContains(detail.Chunks, focusChunkID) {
		t.Fatalf("expected focused chunk %q in response", focusChunkID)
	}
	if detail.Chunks[0].Kind != "text" {
		t.Fatalf("expected text chunk kind, got %q", detail.Chunks[0].Kind)
	}
	if detail.Summary != document.ContentPreview {
		t.Fatalf("expected persisted summary %q, got %q", document.ContentPreview, detail.Summary)
	}
	if detail.Diagnostics.VectorCount != 0 || detail.Diagnostics.QdrantEnabled {
		t.Fatalf("expected disabled qdrant diagnostics, got %#v", detail.Diagnostics)
	}
}

func TestGetKnowledgeBaseHealthReportsHealthyIndexedDocument(t *testing.T) {
	service := NewAppService(nil, nil, nil, model.ServerConfig{})
	knowledgeBase := service.ListKnowledgeBases()[0]
	content := strings.Repeat("健康文档必须有原文、分块和索引时间。", 80)
	path := filepath.Join(t.TempDir(), "healthy.md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write healthy document: %v", err)
	}
	document := model.Document{
		ID:              "doc-healthy",
		KnowledgeBaseID: knowledgeBase.ID,
		Name:            "healthy.md",
		Path:            path,
		Status:          "indexed",
		IndexedAt:       util.NowRFC3339(),
	}
	document.ChunkCount = len(service.rag.BuildDocumentChunks(document, content))
	service.AddDocument(knowledgeBase.ID, document)

	health, err := service.GetKnowledgeBaseHealth(knowledgeBase.ID)
	if err != nil {
		t.Fatalf("get knowledge base health: %v", err)
	}
	if health.Status != "healthy" || health.Score != 100 {
		t.Fatalf("expected healthy score 100, got status=%q score=%d", health.Status, health.Score)
	}
	if health.Metrics.DocumentCount != 1 || health.Metrics.IndexedCount != 1 {
		t.Fatalf("unexpected health metrics: %#v", health.Metrics)
	}
	if len(health.Documents) != 1 || health.Documents[0].NeedsReindex {
		t.Fatalf("expected one healthy document, got %#v", health.Documents)
	}
	if health.Documents[0].ChunkCount == 0 || !health.Documents[0].RawContentAvailable {
		t.Fatalf("expected raw content and chunks, got %#v", health.Documents[0])
	}
}

func TestDocumentDiagnosticsReportStructuredTableMetrics(t *testing.T) {
	service := NewAppService(nil, nil, nil, model.ServerConfig{})
	knowledgeBase := service.ListKnowledgeBases()[0]
	path := filepath.Join(t.TempDir(), "employees.csv")
	if err := os.WriteFile(path, []byte("姓名,部门\n张三,研发\n李四,产品\n王五,测试\n"), 0o644); err != nil {
		t.Fatalf("write csv document: %v", err)
	}
	document := model.Document{
		ID:              "doc-employees",
		KnowledgeBaseID: knowledgeBase.ID,
		Name:            "employees.csv",
		Path:            path,
		Status:          "indexed",
		IndexedAt:       util.NowRFC3339(),
	}
	service.AddDocument(knowledgeBase.ID, document)

	detail, err := service.GetDocumentDetail(knowledgeBase.ID, document.ID, "")
	if err != nil {
		t.Fatalf("get structured document detail: %v", err)
	}
	if detail.Diagnostics.StructuredRowCount != 3 || detail.Diagnostics.SummaryChunkCount != 1 {
		t.Fatalf("unexpected structured diagnostics: %#v", detail.Diagnostics)
	}
	if len(detail.Chunks) == 0 || detail.Chunks[0].Kind != "structured_summary" {
		t.Fatalf("expected structured summary chunk, got %#v", detail.Chunks)
	}

	health, err := service.GetKnowledgeBaseHealth(knowledgeBase.ID)
	if err != nil {
		t.Fatalf("get structured knowledge base health: %v", err)
	}
	if health.Metrics.StructuredRowCount != 3 || health.Metrics.SummaryChunkCount != 1 {
		t.Fatalf("unexpected structured health metrics: %#v", health.Metrics)
	}
}

func TestGetKnowledgeBaseHealthReportsMissingSourceAndIndexError(t *testing.T) {
	service := NewAppService(nil, nil, nil, model.ServerConfig{})
	knowledgeBase := service.ListKnowledgeBases()[0]
	service.AddDocument(knowledgeBase.ID, model.Document{
		ID:              "doc-missing",
		KnowledgeBaseID: knowledgeBase.ID,
		Name:            "missing.md",
		Path:            filepath.Join(t.TempDir(), "missing.md"),
		Status:          "failed",
		IndexError:      "source missing",
	})

	health, err := service.GetKnowledgeBaseHealth(knowledgeBase.ID)
	if err != nil {
		t.Fatalf("get knowledge base health: %v", err)
	}
	if health.Status != "attention" || health.Score >= 60 {
		t.Fatalf("expected attention health below 60, got status=%q score=%d", health.Status, health.Score)
	}
	if health.Metrics.FailedCount != 1 || health.Metrics.EmptyContentCount != 1 {
		t.Fatalf("unexpected failed metrics: %#v", health.Metrics)
	}
	if len(health.Documents) != 1 || !health.Documents[0].NeedsReindex {
		t.Fatalf("expected missing document to require reindex, got %#v", health.Documents)
	}
	if !strings.Contains(health.Documents[0].Recommendation, "无法读取原始文件") {
		t.Fatalf("expected missing source recommendation, got %q", health.Documents[0].Recommendation)
	}
}

func TestReindexDocumentReplacesMetadataWithoutDuplicatingDocument(t *testing.T) {
	service := NewAppService(nil, nil, nil, model.ServerConfig{UploadDir: t.TempDir()})
	knowledgeBase := service.ListKnowledgeBases()[0]
	path := filepath.Join(t.TempDir(), "reindex.md")
	if err := os.WriteFile(path, []byte(strings.Repeat("旧内容。", 100)), 0o644); err != nil {
		t.Fatalf("write old document: %v", err)
	}
	service.AddDocument(knowledgeBase.ID, model.Document{
		ID:              "doc-reindex",
		KnowledgeBaseID: knowledgeBase.ID,
		Name:            "reindex.md",
		Path:            path,
		Status:          "indexed",
		ChunkCount:      1,
		IndexedAt:       "2026-01-01T00:00:00Z",
		ContentPreview:  "旧内容",
	})

	newContent := strings.Repeat("重建后使用新的正文和分块。", 120)
	if err := os.WriteFile(path, []byte(newContent), 0o644); err != nil {
		t.Fatalf("write new document: %v", err)
	}
	indexed, err := service.ReindexDocument(knowledgeBase.ID, "doc-reindex")
	if err != nil {
		t.Fatalf("reindex document: %v", err)
	}
	if indexed.Status != "indexed" || indexed.ChunkCount == 0 {
		t.Fatalf("expected indexed document with chunks, got %#v", indexed)
	}
	if indexed.IndexedAt == "" || indexed.IndexedAt == "2026-01-01T00:00:00Z" {
		t.Fatalf("expected refreshed indexedAt, got %q", indexed.IndexedAt)
	}
	if indexed.IndexError != "" || !strings.Contains(indexed.ContentPreview, "重建后使用新的正文") {
		t.Fatalf("unexpected reindexed metadata: %#v", indexed)
	}
	if indexed.MarkdownPath == "" {
		t.Fatal("expected markdown archive path")
	}
	if _, err := os.Stat(indexed.MarkdownPath); err != nil {
		t.Fatalf("expected markdown archive to exist: %v", err)
	}

	documents, err := service.GetKnowledgeBaseDocuments(knowledgeBase.ID)
	if err != nil {
		t.Fatalf("get knowledge base documents: %v", err)
	}
	if len(documents) != 1 || documents[0].ID != indexed.ID {
		t.Fatalf("expected one replaced document, got %#v", documents)
	}
}

func TestReindexDocumentPersistsFailureEvidenceBeforeDeletingPoints(t *testing.T) {
	service := NewAppService(nil, nil, nil, model.ServerConfig{})
	knowledgeBase := service.ListKnowledgeBases()[0]
	service.AddDocument(knowledgeBase.ID, model.Document{
		ID:              "doc-reindex-missing",
		KnowledgeBaseID: knowledgeBase.ID,
		Name:            "missing.md",
		Path:            filepath.Join(t.TempDir(), "missing.md"),
		Status:          "indexed",
		ChunkCount:      2,
		IndexedAt:       "2026-01-01T00:00:00Z",
	})

	if _, err := service.ReindexDocument(knowledgeBase.ID, "doc-reindex-missing"); err == nil {
		t.Fatal("expected reindex to fail for missing source")
	}
	stored, err := service.GetDocument(knowledgeBase.ID, "doc-reindex-missing")
	if err != nil {
		t.Fatalf("get failed document: %v", err)
	}
	if stored.Status != "indexed" || stored.ChunkCount != 2 {
		t.Fatalf("expected existing index metadata to remain before point deletion, got %#v", stored)
	}
	if strings.TrimSpace(stored.IndexError) == "" {
		t.Fatal("expected reindex failure evidence to be persisted")
	}
}
