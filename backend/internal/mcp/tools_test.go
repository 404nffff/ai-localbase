package mcp

import (
	"context"
	"strings"
	"testing"

	"ai-localbase/internal/model"
)

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
