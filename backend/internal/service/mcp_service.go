package service

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"ai-localbase/internal/model"
	"ai-localbase/internal/util"
)

const (
	mcpProtocolVersion   = "2025-03-26"
	mcpServerName        = "ai-localbase-mcp"
	mcpServerVersion     = "0.1.0"
	mcpKnowledgeBasesURI = "ai-localbase://knowledge-bases"
)

// MCPService 负责承载 /mcp 单端点的协议分发逻辑。
type MCPService struct {
	appService *AppService
	llmService *LLMService
}

// NewMCPService 创建 MCP 协议服务，后续 tools/call 复用现有聊天与检索服务。
func NewMCPService(appService *AppService, llmService *LLMService) *MCPService {
	return &MCPService{
		appService: appService,
		llmService: llmService,
	}
}

// Handle 根据 JSON-RPC method 分发当前支持的 MCP 方法。
func (s *MCPService) Handle(req model.JSONRPCRequest) model.JSONRPCResponse {
	switch req.Method {
	case "initialize":
		return model.JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  s.Initialize(),
		}
	case "tools/list":
		return model.JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]any{
				"tools": s.ToolsList(),
			},
		}
	case "resources/list":
		return model.JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]any{
				"resources": s.ResourcesList(),
			},
		}
	case "resources/templates/list":
		return model.JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]any{
				"resourceTemplates": s.ResourceTemplatesList(),
			},
		}
	case "resources/read":
		result, err := s.ReadResource(req.Params)
		if err != nil {
			return model.JSONRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error: &model.JSONRPCError{
					Code:    -32602,
					Message: err.Error(),
				},
			}
		}
		return model.JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  result,
		}
	case "tools/call":
		result, err := s.handleToolCall(req.Params)
		if err != nil {
			return model.JSONRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error: &model.JSONRPCError{
					Code:    -32602,
					Message: err.Error(),
				},
			}
		}
		return model.JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  result,
		}
	default:
		return model.JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &model.JSONRPCError{
				Code:    -32601,
				Message: "method not found",
			},
		}
	}
}

// Initialize 返回 MCP 初始化阶段需要的最小能力声明。
func (s *MCPService) Initialize() map[string]any {
	return map[string]any{
		"protocolVersion": mcpProtocolVersion,
		"capabilities": map[string]any{
			"tools":     map[string]any{},
			"resources": map[string]any{},
		},
		"serverInfo": map[string]any{
			"name":    mcpServerName,
			"version": mcpServerVersion,
		},
	}
}

// ResourcesList 返回当前可被 MCP 客户端读取的资源清单。
func (s *MCPService) ResourcesList() []map[string]any {
	knowledgeBases := s.appService.ListKnowledgeBases()
	resources := make([]map[string]any, 0, len(knowledgeBases)+1)
	resources = append(resources, map[string]any{
		"uri":         mcpKnowledgeBasesURI,
		"name":        "知识库列表",
		"description": "返回当前系统中的知识库列表",
		"mimeType":    "application/json",
	})
	for _, knowledgeBase := range knowledgeBases {
		resources = append(resources, map[string]any{
			"uri":         fmt.Sprintf("%s/%s", mcpKnowledgeBasesURI, knowledgeBase.ID),
			"name":        knowledgeBase.Name,
			"description": knowledgeBase.Description,
			"mimeType":    "application/json",
		})
	}
	return resources
}

// ResourceTemplatesList 返回知识库资源模板，便于客户端按知识库 ID 读取详情。
func (s *MCPService) ResourceTemplatesList() []map[string]any {
	return []map[string]any{
		{
			"uriTemplate": fmt.Sprintf("%s/{knowledgeBaseId}", mcpKnowledgeBasesURI),
			"name":        "知识库详情",
			"description": "按 knowledgeBaseId 读取单个知识库详情",
			"mimeType":    "application/json",
		},
	}
}

// ReadResource 读取 MCP resources/read 请求的资源正文。
func (s *MCPService) ReadResource(params map[string]any) (map[string]any, error) {
	args, err := decodeMCPArguments[model.MCPResourceReadParams](params)
	if err != nil {
		return nil, fmt.Errorf("invalid params")
	}

	uri := strings.TrimSpace(args.URI)
	if uri == "" {
		return nil, fmt.Errorf("invalid params")
	}

	switch {
	case uri == mcpKnowledgeBasesURI:
		text, err := marshalMCPResourceText(s.appService.ListKnowledgeBases())
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"contents": []map[string]any{{
				"uri":      uri,
				"mimeType": "application/json",
				"text":     text,
			}},
		}, nil
	case strings.HasPrefix(uri, mcpKnowledgeBasesURI+"/"):
		knowledgeBaseID := strings.TrimPrefix(uri, mcpKnowledgeBasesURI+"/")
		for _, knowledgeBase := range s.appService.ListKnowledgeBases() {
			if knowledgeBase.ID == knowledgeBaseID {
				text, err := marshalMCPResourceText(knowledgeBase)
				if err != nil {
					return nil, err
				}
				return map[string]any{
					"contents": []map[string]any{{
						"uri":      uri,
						"mimeType": "application/json",
						"text":     text,
					}},
				}, nil
			}
		}
		return nil, fmt.Errorf("resource not found")
	default:
		return nil, fmt.Errorf("resource not found")
	}
}

// ToolsList 返回当前阶段已开放的 MCP tools 元数据。
func (s *MCPService) ToolsList() []model.MCPTool {
	return []model.MCPTool{
		newMCPTool(
			"chat.ask",
			"基于当前知识库上下文执行一次非流式问答",
			[]model.MCPToolParameter{
				{Name: "message", Type: "string", Required: true, Description: "用户问题或指令"},
				{Name: "knowledgeBaseId", Type: "string", Required: false, Description: "限定检索的知识库 ID；为空时跨全部知识库"},
				{Name: "documentId", Type: "string", Required: false, Description: "限定检索的文档 ID"},
				{Name: "conversationId", Type: "string", Required: false, Description: "用于复用或记录上下文的会话 ID"},
			},
			[]model.MCPToolResponse{
				{Name: "content", Type: "string", Description: "模型回答正文"},
				{Name: "sources", Type: "array<object>", Description: "回答引用的检索来源"},
				{Name: "knowledgeBaseId", Type: "string", Description: "本次请求指定的知识库 ID"},
				{Name: "documentId", Type: "string", Description: "本次请求指定的文档 ID"},
				{Name: "degraded", Type: "boolean", Description: "模型调用是否进入降级响应"},
				{Name: "model", Type: "string", Description: "实际使用的模型名称"},
			},
		),
		newMCPTool(
			"knowledge_base.search",
			"对知识库执行检索并返回命中的片段",
			[]model.MCPToolParameter{
				{Name: "query", Type: "string", Required: true, Description: "检索关键词或自然语言问题"},
				{Name: "knowledgeBaseId", Type: "string", Required: false, Description: "限定检索的知识库 ID；为空时跨全部知识库"},
				{Name: "documentId", Type: "string", Required: false, Description: "限定检索的文档 ID"},
				{Name: "topK", Type: "integer", Required: false, Description: "返回条数上限；为空时使用服务默认值"},
			},
			[]model.MCPToolResponse{
				{Name: "items", Type: "array<object>", Description: "命中的片段列表，包含 knowledgeBaseId、documentId、documentName、chunkId、text、score、index"},
			},
		),
		newMCPTool(
			"knowledge_base.create",
			"创建一个新的知识库；同名时复用已有知识库",
			[]model.MCPToolParameter{
				{Name: "name", Type: "string", Required: true, Description: "知识库名称"},
				{Name: "description", Type: "string", Required: false, Description: "知识库描述"},
			},
			[]model.MCPToolResponse{
				{Name: "id", Type: "string", Description: "知识库 ID"},
				{Name: "knowledgeBaseId", Type: "string", Description: "知识库 ID，供后续工具调用使用"},
				{Name: "name", Type: "string", Description: "知识库名称"},
				{Name: "description", Type: "string", Description: "知识库描述"},
				{Name: "documents", Type: "array<object>", Description: "知识库下的文档元数据"},
				{Name: "createdAt", Type: "string", Description: "知识库创建时间，RFC3339 格式"},
				{Name: "created", Type: "boolean", Description: "true 表示新建，false 表示复用同名知识库"},
			},
		),
		newMCPTool(
			"document.upload",
			"向指定知识库上传文本内容文档并建立索引",
			[]model.MCPToolParameter{
				{Name: "knowledgeBaseId", Type: "string", Required: true, Description: "目标知识库 ID"},
				{Name: "content", Type: "string", Required: true, Description: "要写入并索引的文本内容"},
				{Name: "filename", Type: "string", Required: false, Description: "文档文件名；为空时自动生成 Markdown 文件名"},
			},
			[]model.MCPToolResponse{
				{Name: "message", Type: "string", Description: "上传结果消息"},
				{Name: "knowledgeBaseId", Type: "string", Description: "实际写入的知识库 ID"},
				{Name: "uploaded", Type: "object", Description: "上传后的文档元数据"},
			},
		),
		newMCPTool(
			"document.append",
			"向指定知识库中的文档追加文本内容并重建索引",
			[]model.MCPToolParameter{
				{Name: "knowledgeBaseId", Type: "string", Required: true, Description: "目标知识库 ID"},
				{Name: "documentId", Type: "string", Required: true, Description: "目标文档 ID"},
				{Name: "content", Type: "string", Required: true, Description: "追加到原文末尾的文本内容"},
			},
			[]model.MCPToolResponse{
				{Name: "message", Type: "string", Description: "追加结果消息"},
				{Name: "knowledgeBaseId", Type: "string", Description: "目标知识库 ID"},
				{Name: "updated", Type: "object", Description: "更新后的文档元数据"},
			},
		),
		newMCPTool(
			"document.update",
			"用完整文本覆盖指定知识库中的文档内容并重建索引",
			[]model.MCPToolParameter{
				{Name: "knowledgeBaseId", Type: "string", Required: true, Description: "目标知识库 ID"},
				{Name: "documentId", Type: "string", Required: true, Description: "目标文档 ID"},
				{Name: "content", Type: "string", Required: true, Description: "覆盖原文的完整文本内容"},
			},
			[]model.MCPToolResponse{
				{Name: "message", Type: "string", Description: "更新结果消息"},
				{Name: "knowledgeBaseId", Type: "string", Description: "目标知识库 ID"},
				{Name: "updated", Type: "object", Description: "更新后的文档元数据"},
			},
		),
		newMCPTool(
			"document.delete",
			"删除指定知识库中的文档",
			[]model.MCPToolParameter{
				{Name: "knowledgeBaseId", Type: "string", Required: true, Description: "目标知识库 ID"},
				{Name: "documentId", Type: "string", Required: true, Description: "目标文档 ID"},
			},
			[]model.MCPToolResponse{
				{Name: "message", Type: "string", Description: "删除结果消息"},
				{Name: "knowledgeBaseId", Type: "string", Description: "目标知识库 ID"},
				{Name: "deleted", Type: "object", Description: "被删除的文档元数据"},
			},
		),
		newMCPTool(
			"knowledge_base.list",
			"列出当前系统中的知识库名称和知识库 ID",
			[]model.MCPToolParameter{},
			[]model.MCPToolResponse{
				{Name: "items", Type: "array<object>", Description: "知识库列表，每项包含 id、knowledgeBaseId、name、description、createdAt、documentCount"},
			},
		),
	}
}

func newMCPTool(name string, description string, parameters []model.MCPToolParameter, response []model.MCPToolResponse) model.MCPTool {
	properties := make(map[string]any, len(parameters))
	required := make([]string, 0, len(parameters))
	for _, parameter := range parameters {
		properties[parameter.Name] = map[string]any{
			"type":        parameter.Type,
			"description": parameter.Description,
		}
		if parameter.Required {
			required = append(required, parameter.Name)
		}
	}

	inputSchema := map[string]any{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		inputSchema["required"] = required
	}

	return model.MCPTool{
		Name:        name,
		Description: description,
		InputSchema: inputSchema,
		Invocation: model.MCPToolInvocation{
			JSONRPC: model.MCPToolJSONRPCInvocation{
				Method: "tools/call",
				Params: map[string]any{
					"name":      name,
					"arguments": map[string]any{},
				},
			},
			HTTP: model.MCPToolHTTPInvocation{
				Method: "POST",
				Path:   fmt.Sprintf("/api/mcp/tools/%s/call", name),
				Body: map[string]any{
					"arguments": map[string]any{},
				},
			},
		},
		Parameters: parameters,
		Response:   response,
	}
}

// CallChatAsk 执行 chat.ask 工具，复用现有非流式聊天链路。
func (s *MCPService) CallChatAsk(arguments map[string]any) (map[string]any, error) {
	args, err := decodeMCPArguments[model.MCPChatAskArguments](arguments)
	if err != nil {
		return nil, fmt.Errorf("invalid params")
	}
	if strings.TrimSpace(args.Message) == "" {
		return nil, fmt.Errorf("invalid params")
	}

	req := model.ChatCompletionRequest{
		ConversationID:  strings.TrimSpace(args.ConversationID),
		KnowledgeBaseID: strings.TrimSpace(args.KnowledgeBaseID),
		DocumentID:      strings.TrimSpace(args.DocumentID),
		Messages: []model.ChatMessage{{
			Role:    "user",
			Content: strings.TrimSpace(args.Message),
		}},
	}

	preparedReq, sources, err := s.prepareChatRequest(req)
	if err != nil {
		return nil, err
	}

	response, err := s.llmService.Chat(preparedReq)
	if err != nil {
		return nil, err
	}
	if response.Metadata == nil {
		response.Metadata = map[string]any{}
	}
	response.Metadata["sources"] = sources
	response.Metadata["knowledgeBaseId"] = req.KnowledgeBaseID
	response.Metadata["documentId"] = req.DocumentID

	assistantMessage := firstAssistantChoice(response)
	if assistantMessage == nil {
		return nil, fmt.Errorf("empty assistant response")
	}

	if req.ConversationID != "" {
		if _, saveErr := s.appService.SaveConversation(model.SaveConversationRequest{
			ID:              req.ConversationID,
			Title:           "",
			KnowledgeBaseID: req.KnowledgeBaseID,
			DocumentID:      req.DocumentID,
			Messages:        buildStoredConversationMessages(req.Messages, assistantMessage.Content, response.Metadata),
		}); saveErr != nil {
			return nil, saveErr
		}
	}

	degraded, _ := response.Metadata["degraded"].(bool)
	return map[string]any{
		"content":         assistantMessage.Content,
		"sources":         sources,
		"knowledgeBaseId": req.KnowledgeBaseID,
		"documentId":      req.DocumentID,
		"degraded":        degraded,
		"model":           response.Model,
	}, nil
}

func (s *MCPService) handleToolCall(params map[string]any) (map[string]any, error) {
	callParams, err := decodeMCPArguments[model.MCPToolCallParams](params)
	if err != nil {
		return nil, fmt.Errorf("invalid params")
	}

	return s.CallTool(strings.TrimSpace(callParams.Name), callParams.Arguments)
}

// CallTool 执行单个 MCP 工具并返回 MCP tool result 格式，供 JSON-RPC 与普通 API 入口复用。
func (s *MCPService) CallTool(name string, arguments map[string]any) (map[string]any, error) {
	switch strings.TrimSpace(name) {
	case "chat.ask":
		result, err := s.CallChatAsk(arguments)
		if err != nil {
			return nil, err
		}
		return buildMCPToolResult("chat.ask", result)
	case "knowledge_base.search":
		result, err := s.CallKnowledgeBaseSearch(arguments)
		if err != nil {
			return nil, err
		}
		return buildMCPToolResult("knowledge_base.search", result)
	case "knowledge_base.create":
		result, err := s.CallKnowledgeBaseCreate(arguments)
		if err != nil {
			return nil, err
		}
		return buildMCPToolResult("knowledge_base.create", result)
	case "document.upload":
		result, err := s.CallDocumentUpload(arguments)
		if err != nil {
			return nil, err
		}
		return buildMCPToolResult("document.upload", result)
	case "document.append":
		result, err := s.CallDocumentAppend(arguments)
		if err != nil {
			return nil, err
		}
		return buildMCPToolResult("document.append", result)
	case "document.update":
		result, err := s.CallDocumentUpdate(arguments)
		if err != nil {
			return nil, err
		}
		return buildMCPToolResult("document.update", result)
	case "document.delete":
		result, err := s.CallDocumentDelete(arguments)
		if err != nil {
			return nil, err
		}
		return buildMCPToolResult("document.delete", result)
	case "knowledge_base.list":
		result, err := s.CallKnowledgeBaseList(arguments)
		if err != nil {
			return nil, err
		}
		return buildMCPToolResult("knowledge_base.list", result)
	default:
		return nil, fmt.Errorf("tool not found")
	}
}

// CallKnowledgeBaseSearch 执行 knowledge_base.search 工具，复用现有检索链路。
func (s *MCPService) CallKnowledgeBaseSearch(arguments map[string]any) (map[string]any, error) {
	args, err := decodeMCPArguments[model.MCPKnowledgeBaseSearchArguments](arguments)
	if err != nil {
		return nil, fmt.Errorf("invalid params")
	}
	if strings.TrimSpace(args.Query) == "" {
		return nil, fmt.Errorf("invalid params")
	}

	req := model.ChatCompletionRequest{
		KnowledgeBaseID: strings.TrimSpace(args.KnowledgeBaseID),
		DocumentID:      strings.TrimSpace(args.DocumentID),
		Messages: []model.ChatMessage{{
			Role:    "user",
			Content: strings.TrimSpace(args.Query),
		}},
	}

	chunks, err := s.appService.EvaluateRetrieve(req)
	if err != nil {
		return nil, err
	}

	topK := args.TopK
	if topK > 0 && len(chunks) > topK {
		chunks = chunks[:topK]
	}

	items := make([]map[string]any, 0, len(chunks))
	for _, chunk := range chunks {
		items = append(items, map[string]any{
			"knowledgeBaseId": chunk.KnowledgeBaseID,
			"documentId":      chunk.DocumentID,
			"documentName":    chunk.DocumentName,
			"chunkId":         chunk.ID,
			"text":            chunk.Text,
			"score":           chunk.Score,
			"index":           chunk.Index,
		})
	}

	return map[string]any{"items": items}, nil
}

// CallKnowledgeBaseCreate 执行 knowledge_base.create 工具，复用现有知识库创建链路。
func (s *MCPService) CallKnowledgeBaseCreate(arguments map[string]any) (map[string]any, error) {
	args, err := decodeMCPArguments[model.MCPKnowledgeBaseCreateArguments](arguments)
	if err != nil {
		return nil, fmt.Errorf("invalid params")
	}
	name := strings.TrimSpace(args.Name)
	if name == "" {
		return nil, fmt.Errorf("invalid params")
	}

	for _, knowledgeBase := range s.appService.ListKnowledgeBases() {
		if strings.EqualFold(strings.TrimSpace(knowledgeBase.Name), name) {
			return map[string]any{
				"id":              knowledgeBase.ID,
				"knowledgeBaseId": knowledgeBase.ID,
				"name":            knowledgeBase.Name,
				"description":     knowledgeBase.Description,
				"documents":       knowledgeBase.Documents,
				"createdAt":       knowledgeBase.CreatedAt,
				"created":         false,
			}, nil
		}
	}

	knowledgeBase, err := s.appService.CreateKnowledgeBase(model.KnowledgeBaseInput{
		Name:        name,
		Description: strings.TrimSpace(args.Description),
	})
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"id":              knowledgeBase.ID,
		"knowledgeBaseId": knowledgeBase.ID,
		"name":            knowledgeBase.Name,
		"description":     knowledgeBase.Description,
		"documents":       knowledgeBase.Documents,
		"createdAt":       knowledgeBase.CreatedAt,
		"created":         true,
	}, nil
}

// CallKnowledgeBaseList 返回知识库名称和 ID 的结构化清单，供客户端复核当前映射。
func (s *MCPService) CallKnowledgeBaseList(arguments map[string]any) (map[string]any, error) {
	knowledgeBases := s.appService.ListKnowledgeBases()
	items := make([]map[string]any, 0, len(knowledgeBases))
	for _, knowledgeBase := range knowledgeBases {
		items = append(items, map[string]any{
			"id":              knowledgeBase.ID,
			"knowledgeBaseId": knowledgeBase.ID,
			"name":            knowledgeBase.Name,
			"description":     knowledgeBase.Description,
			"createdAt":       knowledgeBase.CreatedAt,
			"documentCount":   len(knowledgeBase.Documents),
		})
	}
	return map[string]any{"items": items}, nil
}

// CallDocumentUpload 执行 document.upload 工具，客户端可通过多次调用实现目录上传。
func (s *MCPService) CallDocumentUpload(arguments map[string]any) (map[string]any, error) {
	startedAt := time.Now().UTC()
	correlationID := util.NextID("op")
	args, err := decodeMCPArguments[model.MCPDocumentUploadArguments](arguments)
	if err != nil {
		s.recordMCPUploadLog(correlationID, model.OperationStatusFailed, "validate", "", "", 0, startedAt, err)
		return nil, fmt.Errorf("invalid params")
	}

	knowledgeBaseID := strings.TrimSpace(args.KnowledgeBaseID)
	filename := strings.TrimSpace(args.Filename)
	content := args.Content
	if knowledgeBaseID == "" || strings.TrimSpace(content) == "" {
		s.recordMCPUploadLog(correlationID, model.OperationStatusFailed, "validate", knowledgeBaseID, filename, int64(len([]byte(content))), startedAt, fmt.Errorf("invalid params"))
		return nil, fmt.Errorf("invalid params")
	}
	if filename == "" {
		filename = fmt.Sprintf("mcp-content-%d.md", util.NowUnixNano())
	}
	if err := validateMCPUploadFile(filename, s.appService.GetConfig()); err != nil {
		s.recordMCPUploadLog(correlationID, model.OperationStatusFailed, "validate", knowledgeBaseID, filename, int64(len([]byte(content))), startedAt, err)
		return nil, err
	}

	resolvedKnowledgeBaseID, err := s.appService.ResolveKnowledgeBaseID(knowledgeBaseID)
	if err != nil {
		s.recordMCPUploadLog(correlationID, model.OperationStatusFailed, "resolve_knowledge_base", knowledgeBaseID, filename, int64(len([]byte(content))), startedAt, err)
		return nil, err
	}

	uploadDir := util.KnowledgeBaseUploadDir(s.appService.serverConfig.UploadDir, resolvedKnowledgeBaseID)
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		s.recordMCPUploadLog(correlationID, model.OperationStatusFailed, "prepare_upload_dir", resolvedKnowledgeBaseID, filename, int64(len([]byte(content))), startedAt, err)
		return nil, fmt.Errorf("failed to prepare upload directory")
	}

	contentBytes := []byte(content)
	storedName := fmt.Sprintf("%d_%s", util.NowUnixNano(), util.SanitizeFilename(filename))
	destination := util.BuildKnowledgeBaseUploadPath(s.appService.serverConfig.UploadDir, resolvedKnowledgeBaseID, storedName)
	if err := os.WriteFile(destination, contentBytes, 0o644); err != nil {
		s.recordMCPUploadLog(correlationID, model.OperationStatusFailed, "save_file", resolvedKnowledgeBaseID, filename, int64(len(contentBytes)), startedAt, err)
		return nil, fmt.Errorf("failed to save uploaded file")
	}
	s.recordMCPUploadLog(correlationID, model.OperationStatusSuccess, "save_file", resolvedKnowledgeBaseID, filename, int64(len(contentBytes)), startedAt, nil)

	document := model.Document{
		ID:              util.NextID("doc"),
		KnowledgeBaseID: resolvedKnowledgeBaseID,
		Name:            filename,
		Size:            int64(len(contentBytes)),
		SizeLabel:       util.FormatFileSize(int64(len(contentBytes))),
		UploadedAt:      util.NowRFC3339(),
		Status:          "processing",
		Path:            destination,
		ContentPreview:  util.ExtractContentPreview(destination),
	}

	uploaded, err := s.appService.IndexDocumentWithLog(document, model.OperationSourceMCP, correlationID)
	if err != nil {
		_ = os.Remove(destination)
		return nil, err
	}

	return map[string]any{
		"message":         "file uploaded successfully",
		"knowledgeBaseId": resolvedKnowledgeBaseID,
		"uploaded":        uploaded,
	}, nil
}

func (s *MCPService) recordMCPUploadLog(correlationID string, status string, stage string, knowledgeBaseID string, filename string, size int64, startedAt time.Time, uploadErr error) {
	if s == nil || s.appService == nil {
		return
	}
	finishedAt := time.Now().UTC()
	message := "文件上传完成"
	errText := ""
	if uploadErr != nil {
		message = "文件上传失败"
		errText = uploadErr.Error()
	}
	s.appService.RecordOperationLog(model.OperationLogEntry{
		CorrelationID:   correlationID,
		Operation:       model.OperationUploadFile,
		Source:          model.OperationSourceMCP,
		Status:          status,
		KnowledgeBaseID: strings.TrimSpace(knowledgeBaseID),
		DocumentName:    strings.TrimSpace(filename),
		FileSize:        size,
		SizeLabel:       util.FormatFileSize(size),
		Stage:           stage,
		Message:         message,
		Error:           errText,
		StartedAt:       startedAt.Format(time.RFC3339),
		FinishedAt:      finishedAt.Format(time.RFC3339),
		DurationMs:      finishedAt.Sub(startedAt).Milliseconds(),
		CreatedAt:       finishedAt.Format(time.RFC3339),
	})
}

// CallDocumentAppend 执行 document.append 工具，向现有文档尾部追加文本并重建整篇索引。
func (s *MCPService) CallDocumentAppend(arguments map[string]any) (map[string]any, error) {
	args, err := decodeMCPArguments[model.MCPDocumentAppendArguments](arguments)
	if err != nil {
		return nil, fmt.Errorf("invalid params")
	}

	knowledgeBaseID := strings.TrimSpace(args.KnowledgeBaseID)
	documentID := strings.TrimSpace(args.DocumentID)
	appendContent := args.Content
	if knowledgeBaseID == "" || documentID == "" || strings.TrimSpace(appendContent) == "" {
		return nil, fmt.Errorf("invalid params")
	}

	document, err := s.appService.GetDocument(knowledgeBaseID, documentID)
	if err != nil {
		return nil, err
	}
	originalContent, err := os.ReadFile(document.Path)
	if err != nil {
		return nil, fmt.Errorf("failed to read document file")
	}

	updated, err := s.appService.RewriteDocumentContent(knowledgeBaseID, documentID, string(originalContent)+appendContent)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"message":         "document appended",
		"knowledgeBaseId": knowledgeBaseID,
		"updated":         updated,
	}, nil
}

// CallDocumentUpdate 执行 document.update 工具，用完整文本覆盖现有文档并重建索引。
func (s *MCPService) CallDocumentUpdate(arguments map[string]any) (map[string]any, error) {
	args, err := decodeMCPArguments[model.MCPDocumentUpdateArguments](arguments)
	if err != nil {
		return nil, fmt.Errorf("invalid params")
	}

	knowledgeBaseID := strings.TrimSpace(args.KnowledgeBaseID)
	documentID := strings.TrimSpace(args.DocumentID)
	content := args.Content
	if knowledgeBaseID == "" || documentID == "" || strings.TrimSpace(content) == "" {
		return nil, fmt.Errorf("invalid params")
	}

	updated, err := s.appService.RewriteDocumentContent(knowledgeBaseID, documentID, content)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"message":         "document updated",
		"knowledgeBaseId": knowledgeBaseID,
		"updated":         updated,
	}, nil
}

// CallDocumentDelete 执行 document.delete 工具，复用现有文档删除链路。
func (s *MCPService) CallDocumentDelete(arguments map[string]any) (map[string]any, error) {
	args, err := decodeMCPArguments[model.MCPDocumentDeleteArguments](arguments)
	if err != nil {
		return nil, fmt.Errorf("invalid params")
	}

	knowledgeBaseID := strings.TrimSpace(args.KnowledgeBaseID)
	documentID := strings.TrimSpace(args.DocumentID)
	if knowledgeBaseID == "" || documentID == "" {
		return nil, fmt.Errorf("invalid params")
	}

	removedDocument, err := s.appService.DeleteDocument(knowledgeBaseID, documentID)
	if err != nil {
		return nil, err
	}
	if err := RemoveDocumentFiles(removedDocument); err != nil {
		return nil, err
	}

	return map[string]any{
		"message":         "document deleted",
		"knowledgeBaseId": knowledgeBaseID,
		"deleted":         removedDocument,
	}, nil
}

func (s *MCPService) prepareChatRequest(req model.ChatCompletionRequest) (model.ChatCompletionRequest, []map[string]string, error) {
	if len(req.Messages) == 0 {
		return model.ChatCompletionRequest{}, nil, fmt.Errorf("messages cannot be empty")
	}

	retrievalContext, retrievalSources, err := s.appService.BuildRetrievalContext(req)
	if err != nil {
		return model.ChatCompletionRequest{}, nil, err
	}
	contextSummary, sources, err := s.appService.BuildChatContext(req)
	if err != nil {
		return model.ChatCompletionRequest{}, nil, err
	}

	allSources := append(retrievalSources, sources...)
	contextParts := make([]string, 0, 2)
	if strings.TrimSpace(retrievalContext) != "" {
		contextParts = append(contextParts, "检索命中的文档片段：\n"+retrievalContext)
	}
	if strings.TrimSpace(contextSummary) != "" {
		contextParts = append(contextParts, contextSummary)
	}

	preparedReq := req
	preparedReq.Config = s.appService.CurrentChatConfig()
	preparedReq.Config.ContextMessageLimit = s.appService.ContextMessageLimit()
	preparedReq.Messages = s.appService.TrimChatMessages(req.Messages)
	if len(contextParts) > 0 {
		systemPrompt := strings.Join([]string{
			"你是 AI LocalBase 知识库助手。请仅基于给定上下文回答，信息不足时明确说明。",
			"",
			"## 上下文",
			strings.Join(contextParts, "\n\n"),
		}, "\n")
		preparedReq.Messages = append([]model.ChatMessage{{
			Role:    "system",
			Content: systemPrompt,
		}}, preparedReq.Messages...)
	}

	return preparedReq, allSources, nil
}

func buildStoredConversationMessages(messages []model.ChatMessage, assistantContent string, metadata map[string]any) []model.StoredChatMessage {
	stored := make([]model.StoredChatMessage, 0, len(messages)+1)
	for index, message := range messages {
		stored = append(stored, model.StoredChatMessage{
			ID:        fmt.Sprintf("msg_%d_%d", time.Now().UnixNano(), index),
			Role:      strings.TrimSpace(message.Role),
			Content:   message.Content,
			CreatedAt: time.Now().UTC().Format(time.RFC3339),
		})
	}

	assistantMessage := model.StoredChatMessage{
		ID:        fmt.Sprintf("msg_%d_assistant", time.Now().UnixNano()),
		Role:      "assistant",
		Content:   assistantContent,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if len(metadata) > 0 {
		assistantMessage.Metadata = metadata
	}

	return append(stored, assistantMessage)
}

func firstAssistantChoice(response model.ChatCompletionResponse) *model.ChatMessage {
	for _, choice := range response.Choices {
		if strings.EqualFold(strings.TrimSpace(choice.Message.Role), "assistant") {
			message := choice.Message
			return &message
		}
	}
	return nil
}

func decodeMCPArguments[T any](arguments map[string]any) (T, error) {
	var result T
	body, err := json.Marshal(arguments)
	if err != nil {
		return result, err
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return result, err
	}
	return result, nil
}

func validateMCPUploadFile(filename string, cfg model.AppConfig) error {
	ext := strings.ToLower(filepath.Ext(strings.TrimSpace(filename)))
	allowed := map[string]struct{}{
		".txt": {},
		".md":  {},
		".csv": {},
	}
	if _, ok := allowed[ext]; !ok {
		return fmt.Errorf("unsupported file type: %s", ext)
	}
	if IsSensitiveStructuredFileExtension(ext) && !IsLocalOllamaConfig(cfg.Chat, cfg.Embedding) {
		return fmt.Errorf("%s requires local ollama chat and embedding models", ext)
	}
	return nil
}

func marshalMCPResourceText(value any) (string, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("failed to marshal resource")
	}
	return string(body), nil
}

func buildMCPToolResult(toolName string, structuredContent map[string]any) (map[string]any, error) {
	text, err := summarizeMCPToolResult(toolName, structuredContent)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"content": []map[string]any{{
			"type": "text",
			"text": text,
		}},
		"structuredContent": structuredContent,
		"isError":           false,
	}, nil
}

func summarizeMCPToolResult(toolName string, structuredContent map[string]any) (string, error) {
	switch toolName {
	case "chat.ask":
		if content, ok := structuredContent["content"].(string); ok && strings.TrimSpace(content) != "" {
			return content, nil
		}
	case "knowledge_base.search":
		if items, ok := structuredContent["items"].([]map[string]any); ok {
			return fmt.Sprintf("共检索到 %d 条结果", len(items)), nil
		}
	case "knowledge_base.create":
		name, _ := structuredContent["name"].(string)
		knowledgeBaseID, _ := structuredContent["knowledgeBaseId"].(string)
		created, _ := structuredContent["created"].(bool)
		if created {
			return fmt.Sprintf("已创建知识库 %s（%s）", name, knowledgeBaseID), nil
		}
		return fmt.Sprintf("已复用已有知识库 %s（%s）", name, knowledgeBaseID), nil
	case "knowledge_base.list":
		if items, ok := structuredContent["items"].([]map[string]any); ok {
			names := make([]string, 0, len(items))
			for _, item := range items {
				name, _ := item["name"].(string)
				knowledgeBaseID, _ := item["knowledgeBaseId"].(string)
				if strings.TrimSpace(name) == "" || strings.TrimSpace(knowledgeBaseID) == "" {
					continue
				}
				names = append(names, fmt.Sprintf("%s（%s）", name, knowledgeBaseID))
			}
			return fmt.Sprintf("共 %d 个知识库：%s", len(items), strings.Join(names, "、")), nil
		}
	case "document.upload":
		if uploaded, ok := structuredContent["uploaded"].(model.Document); ok {
			return fmt.Sprintf("已上传文档 %s", uploaded.Name), nil
		}
	case "document.append":
		if updated, ok := structuredContent["updated"].(model.Document); ok {
			return fmt.Sprintf("已向文档 %s 追加内容", updated.Name), nil
		}
	case "document.update":
		if updated, ok := structuredContent["updated"].(model.Document); ok {
			return fmt.Sprintf("已更新文档 %s", updated.Name), nil
		}
	case "document.delete":
		if deleted, ok := structuredContent["deleted"].(model.Document); ok {
			return fmt.Sprintf("已删除文档 %s", deleted.Name), nil
		}
	}

	body, err := json.Marshal(structuredContent)
	if err != nil {
		return "", fmt.Errorf("failed to marshal tool result")
	}
	return string(body), nil
}
