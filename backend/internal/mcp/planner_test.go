package mcp

import (
	"context"
	"testing"

	"ai-localbase/internal/model"
)

func TestToolUsePlannerUsesDocumentSearchForDocumentScope(t *testing.T) {
	registry := NewToolRegistry(
		ToolDefinition{
			Name:            "search_document",
			ReadOnly:        true,
			PermissionLevel: ToolPermissionReadOnly,
			Handler:         noopToolHandler,
		},
		ToolDefinition{
			Name:            "search_knowledge_base",
			ReadOnly:        true,
			PermissionLevel: ToolPermissionReadOnly,
			Handler:         noopToolHandler,
		},
	)
	planner := NewToolUsePlanner(registry)

	plans := planner.Plan(model.ChatCompletionRequest{
		DocumentID: "doc-1",
		Messages: []model.ChatMessage{{
			Role:    "user",
			Content: "请介绍这份文档",
		}},
	})
	if len(plans) != 1 {
		t.Fatalf("expected one plan, got %#v", plans)
	}
	if plans[0].ToolName != "search_document" {
		t.Fatalf("expected search_document, got %#v", plans[0])
	}
	if plans[0].Arguments["documentId"] != "doc-1" {
		t.Fatalf("expected documentId argument, got %#v", plans[0].Arguments)
	}
}

func TestBuildToolUseContextPropagatesToolSources(t *testing.T) {
	contextText, sources := BuildToolUseContext([]ToolUseExecution{{
		ToolName:        "search_document",
		PermissionLevel: ToolPermissionReadOnly,
		Content:         []ToolContent{{Type: "text", Text: "命中文本"}},
		Data: map[string]any{
			"sources": []map[string]string{{
				"knowledgeBaseId": "kb-1",
				"documentId":      "doc-1",
				"documentName":    "demo.md",
				"chunkId":         "chunk-1",
				"snippet":         "命中文本",
			}},
		},
	}})

	if contextText == "" {
		t.Fatal("expected context text")
	}
	if len(sources) != 1 {
		t.Fatalf("expected only the propagated document source, got %#v", sources)
	}
	if sources[0]["documentId"] != "doc-1" || sources[0]["toolName"] != "search_document" {
		t.Fatalf("expected propagated source metadata, got %#v", sources[0])
	}
}

func TestBuildToolUseContextDoesNotExposeToolExecutionsAsSources(t *testing.T) {
	_, sources := BuildToolUseContext([]ToolUseExecution{
		{
			ToolName:        "search_document",
			PermissionLevel: ToolPermissionReadOnly,
			Content:         []ToolContent{{Type: "text", Text: "没有来源元数据的结果"}},
		},
		{
			ToolName:        "search_knowledge_base",
			PermissionLevel: ToolPermissionReadOnly,
			IsError:         true,
			Error:           "search failed",
		},
	})

	if len(sources) != 0 {
		t.Fatalf("expected tool executions not to become citation sources, got %#v", sources)
	}
}

func TestBuildToolUseContextDropsIncompleteDocumentSources(t *testing.T) {
	_, sources := BuildToolUseContext([]ToolUseExecution{{
		ToolName:        "search_document",
		PermissionLevel: ToolPermissionReadOnly,
		Data: map[string]any{
			"sources": []map[string]string{{
				"knowledgeBaseId": "kb-1",
				"documentId":      "doc-1",
				"documentName":    "demo.md",
				"chunkId":         "chunk-1",
			}},
		},
	}})

	if len(sources) != 0 {
		t.Fatalf("expected source without snippet to be dropped, got %#v", sources)
	}
}

func noopToolHandler(_ context.Context, _ map[string]any) (ToolCallResult, error) {
	return ToolCallResult{}, nil
}
