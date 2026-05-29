package mcp

import (
	"context"
	"fmt"
	"strings"

	"ai-localbase/internal/model"
)

type ToolUsePlanner struct {
	registry *ToolRegistry
}

type PlannedToolCall struct {
	ToolName        string
	Arguments       map[string]any
	Reason          string
	PermissionLevel ToolPermissionLevel
}

type ToolUseExecution struct {
	ToolName        string              `json:"toolName"`
	Reason          string              `json:"reason"`
	PermissionLevel ToolPermissionLevel `json:"permissionLevel"`
	Arguments       map[string]any      `json:"arguments,omitempty"`
	Data            map[string]any      `json:"data,omitempty"`
	Content         []ToolContent       `json:"content,omitempty"`
	IsError         bool                `json:"isError,omitempty"`
	Error           string              `json:"error,omitempty"`
}

func NewToolUsePlanner(registry *ToolRegistry) *ToolUsePlanner {
	return &ToolUsePlanner{registry: registry}
}

func (p *ToolUsePlanner) Plan(req model.ChatCompletionRequest) []PlannedToolCall {
	if p == nil || p.registry == nil {
		return nil
	}

	question := strings.TrimSpace(latestUserMessageForToolUse(req.Messages))
	if question == "" {
		return nil
	}

	lowerQuestion := strings.ToLower(question)
	plans := make([]PlannedToolCall, 0, 1)

	if req.DocumentID != "" && shouldUseKnowledgeSearch(lowerQuestion) {
		plans = append(plans, PlannedToolCall{
			ToolName:  "search_document",
			Arguments: map[string]any{"documentId": req.DocumentID, "query": question},
			Reason:    "用户正在针对单个文档提问，优先通过 MCP 单文档检索工具补充上下文。",
		})
	} else if req.KnowledgeBaseID != "" && shouldUseKnowledgeSearch(lowerQuestion) {
		plans = append(plans, PlannedToolCall{
			ToolName:  "search_knowledge_base",
			Arguments: map[string]any{"knowledgeBaseId": req.KnowledgeBaseID, "query": question},
			Reason:    "用户正在针对知识库提问，优先通过 MCP 检索工具补充上下文。",
		})
	}

	return p.attachPermissionLevels(plans)
}

func (p *ToolUsePlanner) Execute(ctx context.Context, plans []PlannedToolCall) []ToolUseExecution {
	if p == nil || p.registry == nil || len(plans) == 0 {
		return nil
	}

	executions := make([]ToolUseExecution, 0, len(plans))
	for _, plan := range plans {
		result, err := p.registry.Call(ctx, plan.ToolName, plan.Arguments)
		execution := ToolUseExecution{
			ToolName:        plan.ToolName,
			Reason:          plan.Reason,
			PermissionLevel: plan.PermissionLevel,
			Arguments:       plan.Arguments,
		}
		if err != nil {
			execution.IsError = true
			execution.Error = err.Error()
			executions = append(executions, execution)
			continue
		}
		execution.Content = result.Content
		execution.Data = result.Data
		execution.IsError = result.IsError
		executions = append(executions, execution)
	}

	return executions
}

func BuildToolUseContext(executions []ToolUseExecution) (string, []map[string]string) {
	if len(executions) == 0 {
		return "", nil
	}

	sections := make([]string, 0, len(executions))
	sources := make([]map[string]string, 0, len(executions))
	for _, execution := range executions {
		if execution.IsError {
			sections = append(sections, fmt.Sprintf("[工具 %s 调用失败]\n原因：%s", execution.ToolName, execution.Error))
			sources = append(sources, map[string]string{
				"toolName":        execution.ToolName,
				"permissionLevel": string(execution.PermissionLevel),
				"status":          "error",
			})
			continue
		}

		textParts := make([]string, 0, len(execution.Content))
		for _, item := range execution.Content {
			if strings.TrimSpace(item.Text) != "" {
				textParts = append(textParts, item.Text)
			}
		}
		sections = append(sections, fmt.Sprintf("[工具 %s 输出]\n%s", execution.ToolName, strings.Join(textParts, "\n")))
		sources = append(sources, map[string]string{
			"toolName":        execution.ToolName,
			"permissionLevel": string(execution.PermissionLevel),
			"status":          "ok",
		})
		sources = append(sources, toolDataSources(execution)...)
	}

	return strings.Join(sections, "\n\n"), sources
}

func toolDataSources(execution ToolUseExecution) []map[string]string {
	rawSources, ok := execution.Data["sources"]
	if !ok || rawSources == nil {
		return nil
	}

	output := make([]map[string]string, 0)
	appendSource := func(source map[string]string) {
		if len(source) == 0 {
			return
		}
		item := make(map[string]string, len(source)+3)
		for key, value := range source {
			item[key] = value
		}
		item["toolName"] = execution.ToolName
		item["permissionLevel"] = string(execution.PermissionLevel)
		item["status"] = "ok"
		output = append(output, item)
	}

	switch typed := rawSources.(type) {
	case []map[string]string:
		for _, source := range typed {
			appendSource(source)
		}
	case []map[string]any:
		for _, source := range typed {
			appendSource(stringMapFromAny(source))
		}
	case []any:
		for _, item := range typed {
			if source, ok := item.(map[string]any); ok {
				appendSource(stringMapFromAny(source))
			}
		}
	}
	return output
}

func stringMapFromAny(input map[string]any) map[string]string {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		output[key] = fmt.Sprint(value)
	}
	return output
}

func (p *ToolUsePlanner) attachPermissionLevels(plans []PlannedToolCall) []PlannedToolCall {
	if len(plans) == 0 {
		return plans
	}

	definitions := p.registry.List()
	byName := make(map[string]ToolDefinition, len(definitions))
	for _, definition := range definitions {
		byName[definition.Name] = definition
	}

	for index := range plans {
		if definition, ok := byName[plans[index].ToolName]; ok {
			plans[index].PermissionLevel = definition.PermissionLevel
		}
	}
	return plans
}

func shouldUseKnowledgeSearch(question string) bool {
	if question == "" {
		return false
	}

	markers := []string{
		"是什么",
		"什么是",
		"介绍",
		"说明",
		"总结",
		"概述",
		"有哪些",
		"列出",
		"区别",
		"如何",
		"为什么",
		"redis",
		"文档",
	}
	for _, marker := range markers {
		if strings.Contains(question, marker) {
			return true
		}
	}
	return false
}

func latestUserMessageForToolUse(messages []model.ChatMessage) string {
	for index := len(messages) - 1; index >= 0; index-- {
		if strings.EqualFold(strings.TrimSpace(messages[index].Role), "user") {
			return messages[index].Content
		}
	}
	return ""
}
