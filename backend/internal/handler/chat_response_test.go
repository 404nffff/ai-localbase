package handler

import (
	"strings"
	"testing"

	"ai-localbase/internal/mcp"
	"ai-localbase/internal/model"
)

func TestIsDirectConversationMessage(t *testing.T) {
	for _, message := range []string{"你好！", "你是谁", "who are you?"} {
		if !isDirectConversationMessage(message) {
			t.Fatalf("expected direct conversation message: %q", message)
		}
	}
	for _, message := range []string{"列出主要角色", "你好，请总结当前知识库", "AI LocalBase 如何部署？"} {
		if isDirectConversationMessage(message) {
			t.Fatalf("expected knowledge question to use retrieval: %q", message)
		}
	}
}

func TestFilterOperationalChatMessages(t *testing.T) {
	messages := filterOperationalChatMessages([]model.ChatMessage{
		{Role: "assistant", Content: "你好，我是 AI LocalBase 助手。你可以先选择知识库，或者进一步选中某个文档后再提问。"},
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
		if strings.Contains(message.Content, "你可以先选择知识库") {
			t.Fatalf("legacy welcome content leaked into model history: %#v", messages)
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
