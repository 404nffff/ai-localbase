package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"ai-localbase/internal/model"
)

type sensitiveMCPAppService struct {
	listToolsAppService
	detail model.DocumentDetailResponse
	health model.KnowledgeBaseHealthResponse
}

func (s sensitiveMCPAppService) GetDocumentDetail(string, string, string) (model.DocumentDetailResponse, error) {
	return s.detail, nil
}

func (s sensitiveMCPAppService) GetKnowledgeBaseHealth(string) (model.KnowledgeBaseHealthResponse, error) {
	return s.health, nil
}

func (s sensitiveMCPAppService) ListEvalRuns(string, string) []model.EvalRunSummary {
	return nil
}

type listToolsAppService struct {
	AppServiceReader
	knowledgeBases []model.KnowledgeBase
	documents      []model.Document
	conversations  []model.ConversationListItem
	jobs           []model.MCPJob
}

func (s listToolsAppService) ListKnowledgeBases() []model.KnowledgeBase {
	return s.knowledgeBases
}

func (s listToolsAppService) GetKnowledgeBaseDocuments(string) ([]model.Document, error) {
	return s.documents, nil
}

func (s listToolsAppService) ListConversations() ([]model.ConversationListItem, error) {
	return s.conversations, nil
}

func (s listToolsAppService) ListRecentMCPJobs(int) []model.MCPJob {
	return s.jobs
}

func TestListToolsExposeIdentifiersInTextResults(t *testing.T) {
	appService := listToolsAppService{
		knowledgeBases: []model.KnowledgeBase{{
			ID:   "kb-123",
			Name: "产品手册\n2026",
			Documents: []model.Document{{
				ID: "doc-123",
			}},
		}},
		documents: []model.Document{{
			ID:     "doc-123",
			Name:   "部署指南\n第一版",
			Status: "indexed",
		}},
		conversations: []model.ConversationListItem{{
			ID:           "conversation-123",
			Title:        "部署问题",
			MessageCount: 3,
		}},
		jobs: []model.MCPJob{{
			ID:       "job-123",
			Type:     "import",
			Status:   "succeeded",
			Progress: 100,
			Summary:  "文档导入完成",
		}},
	}

	tests := []struct {
		name     string
		args     map[string]any
		expected []string
		dataKey  string
	}{
		{name: "list_knowledge_bases", expected: []string{"产品手册 2026", "kb-123", "文档: 1 篇"}, dataKey: "items"},
		{name: "list_documents", args: map[string]any{"knowledgeBaseId": "kb-123"}, expected: []string{"部署指南 第一版", "doc-123", "状态: indexed"}, dataKey: "items"},
		{name: "list_conversations", expected: []string{"部署问题", "conversation-123", "消息: 3 条"}, dataKey: "items"},
		{name: "list_recent_jobs", expected: []string{"文档导入完成", "job-123", "状态: succeeded", "进度: 100%"}, dataKey: "jobs"},
	}

	definitions := NewReadOnlyTools(appService)
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var definition ToolDefinition
			for _, candidate := range definitions {
				if candidate.Name == test.name {
					definition = candidate
					break
				}
			}
			if definition.Handler == nil {
				t.Fatalf("tool %q not found", test.name)
			}

			result, err := definition.Handler(context.Background(), test.args)
			if err != nil {
				t.Fatalf("call tool: %v", err)
			}
			if len(result.Content) != 1 {
				t.Fatalf("expected one text content item, got %+v", result.Content)
			}
			for _, expected := range test.expected {
				if !strings.Contains(result.Content[0].Text, expected) {
					t.Errorf("expected text to contain %q, got %q", expected, result.Content[0].Text)
				}
			}
			if result.Data[test.dataKey] == nil {
				t.Fatalf("expected structured data key %q to remain available", test.dataKey)
			}
		})
	}
}

func TestMCPListLabelCollapsesUserProvidedWhitespace(t *testing.T) {
	if got := mcpListLabel("  knowledge\n\tbase  ", "fallback"); got != "knowledge base" {
		t.Fatalf("expected normalized label, got %q", got)
	}
	if got := mcpListLabel(" \n\t", "fallback"); got != "fallback" {
		t.Fatalf("expected fallback label, got %q", got)
	}
}

func TestGenerateEvalDatasetIsWriteToolWithEvalScope(t *testing.T) {
	var definition ToolDefinition
	for _, candidate := range NewReadOnlyTools(listToolsAppService{}) {
		if candidate.Name == "generate_eval_dataset" {
			definition = candidate
			break
		}
	}
	if definition.Name == "" {
		t.Fatal("generate_eval_dataset tool not found")
	}
	if definition.ReadOnly {
		t.Fatal("generate_eval_dataset must not be advertised as read-only")
	}
	if definition.PermissionLevel != ToolPermissionWrite {
		t.Fatalf("expected write permission level, got %q", definition.PermissionLevel)
	}

	scopes := requiredScopesForTool(definition)
	if len(scopes) != 1 || scopes[0] != scopeMCPEval {
		t.Fatalf("expected mcp:eval scope, got %v", scopes)
	}
}

func TestStartImportJobSchemaSupportsAllLongTaskTypes(t *testing.T) {
	var definition ToolDefinition
	for _, candidate := range NewReadOnlyTools(listToolsAppService{}) {
		if candidate.Name == "start_import_job" {
			definition = candidate
			break
		}
	}
	if definition.Name == "" {
		t.Fatal("start_import_job tool not found")
	}
	properties, ok := definition.InputSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected object schema properties, got %#v", definition.InputSchema["properties"])
	}
	for _, name := range []string{"jobType", "documentId", "maxPerDocument", "uploadIds", "concurrency"} {
		if properties[name] == nil {
			t.Errorf("expected long-task property %q", name)
		}
	}
	jobType, ok := properties["jobType"].(map[string]any)
	if !ok {
		t.Fatalf("expected jobType schema, got %#v", properties["jobType"])
	}
	if got := jobType["enum"]; got == nil {
		t.Fatalf("expected jobType enum, got %#v", jobType)
	}
}

func TestMCPDocumentResultsDoNotExposeInternalFields(t *testing.T) {
	appService := sensitiveMCPAppService{
		detail: model.DocumentDetailResponse{
			KnowledgeBaseID: "kb-safe",
			Document: model.Document{
				ID:              "doc-safe",
				KnowledgeBaseID: "kb-safe",
				Name:            "部署文档",
				Path:            "/app/data/uploads/private.pdf",
				IndexError:      "qdrant request failed at http://qdrant:6333/collections/private",
				Status:          "failed",
			},
			Diagnostics: model.DocumentIndexDiagnostics{ChunkCount: 2},
			RawContent:  "文档内容",
			Summary:     "文档摘要",
		},
		health: model.KnowledgeBaseHealthResponse{
			KnowledgeBaseID: "kb-safe",
			Name:            "知识库",
			Status:          "attention",
			Documents: []model.KnowledgeBaseDocumentHealth{{
				DocumentID:     "doc-safe",
				DocumentName:   "部署文档",
				IndexError:     "open /var/lib/ai-localbase/uploads/private.pdf: permission denied",
				Recommendation: "建议重建索引。",
			}},
		},
	}

	for _, name := range []string{"get_document_detail", "summarize_document"} {
		definition := findToolDefinition(t, NewReadOnlyTools(appService), name)
		result, err := definition.Handler(context.Background(), map[string]any{
			"knowledgeBaseId": "kb-safe",
			"documentId":      "doc-safe",
		})
		if err != nil {
			t.Fatalf("call %s: %v", name, err)
		}
		encoded, err := json.Marshal(result.Data)
		if err != nil {
			t.Fatalf("encode %s result: %v", name, err)
		}
		payload := string(encoded)
		for _, forbidden := range []string{"indexError", "qdrant request failed", "/app/data", "/var/lib", "http://qdrant"} {
			if strings.Contains(payload, forbidden) {
				t.Errorf("%s result exposed internal value %q: %s", name, forbidden, payload)
			}
		}
	}

	quality := findToolDefinition(t, NewReadOnlyTools(appService), "inspect_knowledge_base_quality")
	result, err := quality.Handler(context.Background(), map[string]any{"knowledgeBaseId": "kb-safe"})
	if err != nil {
		t.Fatalf("call inspect_knowledge_base_quality: %v", err)
	}
	encoded, err := json.Marshal(result.Data)
	if err != nil {
		t.Fatalf("encode quality result: %v", err)
	}
	payload := string(encoded)
	for _, forbidden := range []string{"indexError", "permission denied", "/var/lib"} {
		if strings.Contains(payload, forbidden) {
			t.Errorf("quality result exposed internal value %q: %s", forbidden, payload)
		}
	}

	job := sanitizeMCPJob(model.MCPJob{Result: map[string]any{
		"document": model.Document{
			Path:       "/app/data/uploads/private.pdf",
			IndexError: "open /app/data/uploads/private.pdf: permission denied",
		},
	}})
	encoded, err = json.Marshal(job.Result)
	if err != nil {
		t.Fatalf("encode job result: %v", err)
	}
	payload = string(encoded)
	for _, forbidden := range []string{"indexError", "/app/data", "permission denied"} {
		if strings.Contains(payload, forbidden) {
			t.Errorf("job result exposed internal value %q: %s", forbidden, payload)
		}
	}
}

func findToolDefinition(t *testing.T, definitions []ToolDefinition, name string) ToolDefinition {
	t.Helper()
	for _, definition := range definitions {
		if definition.Name == name {
			return definition
		}
	}
	t.Fatalf("tool %q not found", name)
	return ToolDefinition{}
}
