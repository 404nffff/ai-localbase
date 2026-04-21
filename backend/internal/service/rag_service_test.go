package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"ai-localbase/internal/model"
	"ai-localbase/internal/util"
)

func TestRagServiceChunkText(t *testing.T) {
	rag := NewRagService()
	input := strings.Repeat("知识库检索能力验证。", 120)

	chunks := rag.ChunkText(input)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}

	for index, chunk := range chunks {
		if strings.TrimSpace(chunk) == "" {
			t.Fatalf("chunk %d should not be empty", index)
		}
	}
}

func TestRagServiceBuildDocumentChunks(t *testing.T) {
	rag := NewRagService()
	document := model.Document{
		ID:              "doc-1",
		KnowledgeBaseID: "kb-1",
		Name:            "demo.md",
	}

	chunks := rag.BuildDocumentChunks(document, strings.Repeat("RAG 文档切分测试。", 100))
	if len(chunks) == 0 {
		t.Fatal("expected non-empty document chunks")
	}

	first := chunks[0]
	if first.DocumentID != document.ID {
		t.Fatalf("expected document id %s, got %s", document.ID, first.DocumentID)
	}
	if first.KnowledgeBaseID != document.KnowledgeBaseID {
		t.Fatalf("expected knowledge base id %s, got %s", document.KnowledgeBaseID, first.KnowledgeBaseID)
	}
}

func TestRagServiceEmbedTextsFallback(t *testing.T) {
	rag := NewRagService()
	cfg := model.EmbeddingModelConfig{
		Provider: "ollama",
		BaseURL:  "http://127.0.0.1:0",
		Model:    "demo-embedding-model",
	}

	embeddings, err := rag.EmbedTexts(t.Context(), cfg, []string{"redis 缓存", "qdrant 检索"}, 8)
	if err != nil {
		t.Fatalf("expected fallback embeddings without error, got %v", err)
	}
	if len(embeddings) != 2 {
		t.Fatalf("expected 2 embeddings, got %d", len(embeddings))
	}
	for index, vector := range embeddings {
		if len(vector) != 8 {
			t.Fatalf("embedding %d expected dimension 8, got %d", index, len(vector))
		}
	}
}

func TestRagServiceBuildContext(t *testing.T) {
	rag := NewRagService()
	contextText, sources := rag.BuildContext([]RetrievedChunk{
		{
			DocumentChunk: DocumentChunk{
				ID:              "chunk-1",
				KnowledgeBaseID: "kb-1",
				DocumentID:      "doc-1",
				DocumentName:    "demo.md",
				Text:            "这是一个用于回答问题的片段。",
				Index:           0,
			},
			Score: 0.92,
		},
	})

	if !strings.Contains(contextText, "demo.md") {
		t.Fatalf("expected context to contain document name, got %s", contextText)
	}
	if len(sources) != 1 {
		t.Fatalf("expected 1 source, got %d", len(sources))
	}
	if sources[0]["chunkId"] != "chunk-1" {
		t.Fatalf("expected chunkId chunk-1, got %s", sources[0]["chunkId"])
	}
}

func TestExtractDocumentTextFromMarkdown(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "demo.md")
	content := "# 标题\n\n第一段内容。\n第二段内容。\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write markdown file: %v", err)
	}

	text, err := util.ExtractDocumentText(path)
	if err != nil {
		t.Fatalf("extract markdown text: %v", err)
	}

	if !strings.Contains(text, "第一段内容。") {
		t.Fatalf("expected extracted text to contain markdown content, got %q", text)
	}
}

func TestExtractContentPreviewFromMarkdown(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "preview.md")
	content := strings.Repeat("用于摘要生成的内容。", 20)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write markdown file: %v", err)
	}

	preview := util.ExtractContentPreview(path)
	if !strings.Contains(preview, "用于摘要生成的内容") {
		t.Fatalf("expected preview to contain file content, got %q", preview)
	}
	if len([]rune(preview)) > 123 {
		t.Fatalf("expected preview to be truncated to a reasonable length, got %d runes", len([]rune(preview)))
	}
}

func TestAppServiceIndexDocumentWithExtractedText(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "indexed.md")
	content := strings.Repeat("真实文本抽取后进入索引链路。", 80)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write indexed markdown file: %v", err)
	}

	service := NewAppService(nil, NewAppStateStore(""), nil, model.ServerConfig{})
	knowledgeBases := service.ListKnowledgeBases()
	if len(knowledgeBases) == 0 {
		t.Fatal("expected default knowledge base")
	}

	document := model.Document{
		ID:              "doc-indexed",
		KnowledgeBaseID: knowledgeBases[0].ID,
		Name:            "indexed.md",
		Path:            path,
		Status:          "processing",
	}

	indexed, err := service.IndexDocument(document)
	if err != nil {
		t.Fatalf("index document: %v", err)
	}

	if indexed.Status != "indexed" {
		t.Fatalf("expected indexed status, got %s", indexed.Status)
	}
	if !strings.Contains(indexed.ContentPreview, "真实文本抽取后进入索引链路") {
		t.Fatalf("expected content preview to come from extracted text, got %q", indexed.ContentPreview)
	}
}

func TestAppServiceIndexDocumentCreatesMarkdownArchive(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "indexed-source.md")
	content := strings.Repeat("索引后应该产出 markdown 归档文件。", 80)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write indexed markdown file: %v", err)
	}

	service := NewAppService(nil, NewAppStateStore(""), nil, model.ServerConfig{})
	knowledgeBases := service.ListKnowledgeBases()
	if len(knowledgeBases) == 0 {
		t.Fatal("expected default knowledge base")
	}

	document := model.Document{
		ID:              "doc-indexed-archive",
		KnowledgeBaseID: knowledgeBases[0].ID,
		Name:            "indexed-source.md",
		Path:            path,
		Status:          "processing",
	}

	indexed, err := service.IndexDocument(document)
	if err != nil {
		t.Fatalf("index document: %v", err)
	}

	markdownPath := mustReadMarkdownPathFromDocument(t, indexed)
	if filepath.Ext(markdownPath) != ".md" {
		t.Fatalf("expected markdown archive path to end with .md, got %q", markdownPath)
	}

	archivedContent, err := os.ReadFile(markdownPath)
	if err != nil {
		t.Fatalf("read markdown archive file %q: %v", markdownPath, err)
	}
	if !strings.Contains(string(archivedContent), "索引后应该产出 markdown 归档文件") {
		t.Fatalf("expected markdown archive to contain indexed content, got %q", string(archivedContent))
	}
}

func TestAppServiceRewriteDocumentContentRefreshesMarkdownArchive(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rewrite-source.md")
	originalContent := strings.Repeat("初始内容用于写入 markdown 归档。", 80)
	if err := os.WriteFile(path, []byte(originalContent), 0o644); err != nil {
		t.Fatalf("write source markdown file: %v", err)
	}

	service := NewAppService(nil, NewAppStateStore(""), nil, model.ServerConfig{})
	knowledgeBases := service.ListKnowledgeBases()
	if len(knowledgeBases) == 0 {
		t.Fatal("expected default knowledge base")
	}

	document := model.Document{
		ID:              "doc-rewrite-archive",
		KnowledgeBaseID: knowledgeBases[0].ID,
		Name:            "rewrite-source.md",
		Path:            path,
		Status:          "processing",
	}

	indexed, err := service.IndexDocument(document)
	if err != nil {
		t.Fatalf("index document: %v", err)
	}
	originalMarkdownPath := mustReadMarkdownPathFromDocument(t, indexed)

	rewrittenContent := strings.Repeat("重写后的内容需要同步刷新 markdown 归档。", 80)
	updated, err := service.RewriteDocumentContent(knowledgeBases[0].ID, indexed.ID, rewrittenContent)
	if err != nil {
		t.Fatalf("rewrite document content: %v", err)
	}
	updatedMarkdownPath := mustReadMarkdownPathFromDocument(t, updated)
	if updatedMarkdownPath != originalMarkdownPath {
		t.Fatalf("expected rewrite to keep markdown archive path %q, got %q", originalMarkdownPath, updatedMarkdownPath)
	}

	archivedContent, err := os.ReadFile(updatedMarkdownPath)
	if err != nil {
		t.Fatalf("read updated markdown archive %q: %v", updatedMarkdownPath, err)
	}
	if !strings.Contains(string(archivedContent), "重写后的内容需要同步刷新 markdown 归档") {
		t.Fatalf("expected markdown archive to contain rewritten content, got %q", string(archivedContent))
	}
	if strings.Contains(string(archivedContent), "初始内容用于写入 markdown 归档") {
		t.Fatalf("expected markdown archive to drop stale content, got %q", string(archivedContent))
	}
}

func mustReadMarkdownPathFromDocument(t *testing.T, document model.Document) string {
	t.Helper()

	payload, err := json.Marshal(document)
	if err != nil {
		t.Fatalf("marshal document for markdownPath assertion: %v", err)
	}

	var documentMap map[string]any
	if err := json.Unmarshal(payload, &documentMap); err != nil {
		t.Fatalf("decode document json payload: %v", err)
	}

	rawPath, ok := documentMap["markdownPath"]
	if !ok {
		t.Fatalf("expected indexed document to expose markdownPath for export archive contract, got %#v", documentMap)
	}

	markdownPath, ok := rawPath.(string)
	if !ok {
		t.Fatalf("expected markdownPath to be string, got %#v", rawPath)
	}
	markdownPath = strings.TrimSpace(markdownPath)
	if markdownPath == "" {
		t.Fatalf("expected markdownPath to be non-empty, got %#v", documentMap)
	}
	return markdownPath
}

func TestBuildSparseVector(t *testing.T) {
	vector := BuildSparseVector("混合 Hybrid Search 支持 iPhone14 Pro Max")
	if len(vector.Indices) == 0 {
		t.Fatal("expected sparse vector indices")
	}
	if len(vector.Indices) != len(vector.Values) {
		t.Fatalf("expected indices and values length match, got %d and %d", len(vector.Indices), len(vector.Values))
	}
	if len(vector.Indices) < 5 {
		t.Fatalf("expected more tokens, got %d", len(vector.Indices))
	}
}

func TestRRFFusion(t *testing.T) {
	dense := []SearchResult{
		{ID: "a", Score: 0.9},
		{ID: "b", Score: 0.8},
		{ID: "c", Score: 0.7},
	}
	sparse := []SearchResult{
		{ID: "b", Score: 0.95},
		{ID: "d", Score: 0.6},
		{ID: "a", Score: 0.55},
	}

	merged := rrfFusion(dense, sparse, 4)
	if len(merged) < 3 {
		t.Fatalf("expected merged results, got %d", len(merged))
	}
	if merged[0].ID != "a" && merged[0].ID != "b" {
		t.Fatalf("expected top1 to be a or b, got %s", merged[0].ID)
	}
	if merged[0].ID == "b" && merged[1].ID != "a" {
		t.Fatalf("expected a to rank near top, got %s", merged[1].ID)
	}
}

func TestMultiQueryDeduplication(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/points/search") {
			call := atomic.AddInt32(&calls, 1)
			var resp qdrantSearchResponse
			if call == 1 {
				resp.Result = []struct {
					ID      any            `json:"id"`
					Score   float64        `json:"score"`
					Payload map[string]any `json:"payload"`
				}{
					{
						ID:    "chunk-1",
						Score: 0.9,
						Payload: map[string]any{
							"chunk_id":          "chunk-1",
							"text":              "片段一",
							"document_id":       "doc-1",
							"document_name":     "Doc 1",
							"knowledge_base_id": "kb-1",
							"chunk_index":       0,
						},
					},
					{
						ID:    "chunk-2",
						Score: 0.8,
						Payload: map[string]any{
							"chunk_id":          "chunk-2",
							"text":              "片段二",
							"document_id":       "doc-2",
							"document_name":     "Doc 2",
							"knowledge_base_id": "kb-1",
							"chunk_index":       1,
						},
					},
				}
			} else {
				resp.Result = []struct {
					ID      any            `json:"id"`
					Score   float64        `json:"score"`
					Payload map[string]any `json:"payload"`
				}{
					{
						ID:    "chunk-1",
						Score: 0.95,
						Payload: map[string]any{
							"chunk_id":          "chunk-1",
							"text":              "片段一",
							"document_id":       "doc-1",
							"document_name":     "Doc 1",
							"knowledge_base_id": "kb-1",
							"chunk_index":       0,
						},
					},
					{
						ID:    "chunk-3",
						Score: 0.7,
						Payload: map[string]any{
							"chunk_id":          "chunk-3",
							"text":              "片段三",
							"document_id":       "doc-3",
							"document_name":     "Doc 3",
							"knowledge_base_id": "kb-1",
							"chunk_index":       2,
						},
					},
				}
			}
			if err := json.NewEncoder(w).Encode(resp); err != nil {
				t.Fatalf("encode qdrant response: %v", err)
			}
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)

	rag := NewRagService()
	qdrant := NewQdrantService(model.ServerConfig{
		QdrantURL:        server.URL,
		QdrantVectorSize: 4,
		QdrantDistance:   "cosine",
	})
	rag.SetQdrantService(qdrant)

	results, err := rag.MultiQuerySearch(
		t.Context(),
		[]string{"  Foo", "foo", "Bar"},
		"kb-1",
		3,
		0,
		model.EmbeddingModelConfig{Provider: "openai"},
	)
	if err != nil {
		t.Fatalf("multi query search: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 unique chunks, got %d", len(results))
	}
	if results[0].ID != "chunk-1" {
		t.Fatalf("expected top chunk-1, got %s", results[0].ID)
	}
	if results[0].Score != 0.95 {
		t.Fatalf("expected chunk-1 score 0.95, got %v", results[0].Score)
	}
}

func TestLLMQueryRewriterParsing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/chat/completions") {
			resp := openAIChatResponse{
				ID:      "chatcmpl-test",
				Object:  "chat.completion",
				Created: 123,
				Model:   "test-model",
				Choices: []model.ChatCompletionChoice{
					{
						Index: 0,
						Message: model.ChatMessage{
							Role:    "assistant",
							Content: "- 查询一\n• 查询二\n\n* 查询三\n- 查询一",
						},
					},
				},
			}
			if err := json.NewEncoder(w).Encode(resp); err != nil {
				t.Fatalf("encode chat response: %v", err)
			}
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)

	llm := &LLMService{client: server.Client()}
	rewriter := NewLLMQueryRewriter(llm, 3)
	rewriter.SetChatConfigProvider(func() model.ChatModelConfig {
		return model.ChatModelConfig{
			Provider: "openai",
			BaseURL:  server.URL,
			Model:    "test-model",
		}
	})

	result, err := rewriter.Rewrite(t.Context(), "原始问题", []string{"上下文1", "上下文2"})
	if err != nil {
		t.Fatalf("rewrite query: %v", err)
	}
	if len(result.RewrittenQueries) != 4 {
		t.Fatalf("expected 4 queries, got %d", len(result.RewrittenQueries))
	}
	assertContains := func(target string) {
		for _, item := range result.RewrittenQueries {
			if item == target {
				return
			}
		}
		t.Fatalf("expected queries to contain %s", target)
	}
	assertContains("查询一")
	assertContains("查询二")
	assertContains("查询三")
	assertContains("原始问题")
}
