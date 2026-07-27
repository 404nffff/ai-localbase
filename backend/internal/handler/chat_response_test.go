package handler

import (
	"strings"
	"testing"

	"ai-localbase/internal/mcp"
	"ai-localbase/internal/model"
)

func TestStreamResponseMetadataRecognizesDegradedReply(t *testing.T) {
	metadata := streamResponseMetadata("⚠️ AI 模型调用已降级\n\n本地模型响应超时")
	if metadata == nil {
		t.Fatal("expected degraded stream metadata")
	}
	if degraded, _ := metadata["degraded"].(bool); !degraded {
		t.Fatalf("expected degraded=true, got %v", metadata["degraded"])
	}
}

func TestBuildLocalAssistantAnswerHandlesGreeting(t *testing.T) {
	content, strategy, ok := buildLocalAssistantAnswer(model.ChatCompletionRequest{
		Messages: []model.ChatMessage{{Role: "user", Content: "你好！"}},
	})
	if !ok || strategy != "greeting-template" {
		t.Fatalf("expected local greeting response, got ok=%v strategy=%q", ok, strategy)
	}
	if content != "你好！请问有什么可以帮你？" {
		t.Fatalf("unexpected greeting response: %q", content)
	}
}

func TestFilterOperationalChatMessages(t *testing.T) {
	messages := filterOperationalChatMessages([]model.ChatMessage{
		{Role: "user", Content: "小说大纲写得怎么样"},
		{Role: "assistant", Content: "⚠️ AI 模型调用已降级\n\n模型超时"},
		{Role: "assistant", Content: "大纲包含六卷结构。"},
		{Role: "user", Content: "主角是谁"},
	})
	if len(messages) != 3 {
		t.Fatalf("expected degraded assistant message to be removed, got %#v", messages)
	}
	for _, message := range messages {
		if strings.Contains(message.Content, "模型调用已降级") {
			t.Fatalf("degraded content leaked into model history: %#v", messages)
		}
	}
}

func TestFilterRedundantRetrievalToolPlans(t *testing.T) {
	plans := []mcp.PlannedToolCall{
		{ToolName: "search_knowledge_base"},
		{ToolName: "custom_read_tool"},
	}

	filtered := filterRedundantRetrievalToolPlans(plans, "retrieved evidence")
	if len(filtered) != 1 || filtered[0].ToolName != "custom_read_tool" {
		t.Fatalf("expected only the non-redundant tool plan, got %#v", filtered)
	}
	if withoutContext := filterRedundantRetrievalToolPlans(plans, ""); len(withoutContext) != len(plans) {
		t.Fatalf("expected retrieval fallback to remain without direct context, got %#v", withoutContext)
	}
}

func TestRetrievalToolUseSourcesPreservesMetadataWithoutSecondSearch(t *testing.T) {
	sources := retrievalToolUseSources(model.ChatCompletionRequest{KnowledgeBaseID: "kb-1"}, "retrieved evidence")
	if len(sources) != 1 || sources[0]["toolName"] != "search_knowledge_base" {
		t.Fatalf("unexpected retrieval tool metadata: %#v", sources)
	}
	if sources[0]["permissionLevel"] != "read-only" {
		t.Fatalf("expected read-only metadata, got %#v", sources[0])
	}
}
