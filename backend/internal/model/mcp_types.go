package model

// MCPToolCallParams 描述 tools/call 的基础调用参数。
type MCPToolCallParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

// JSONRPCRequest 表示 MCP HTTP 入口接收的 JSON-RPC 请求基础结构。
type JSONRPCRequest struct {
	JSONRPC string         `json:"jsonrpc"`
	ID      any            `json:"id"`
	Method  string         `json:"method"`
	Params  map[string]any `json:"params"`
}

// JSONRPCResponse 表示 MCP HTTP 入口返回的 JSON-RPC 响应基础结构。
type JSONRPCResponse struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      any           `json:"id"`
	Result  any           `json:"result,omitempty"`
	Error   *JSONRPCError `json:"error,omitempty"`
}

// JSONRPCError 表示 JSON-RPC 协议中的标准错误对象。
type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// MCPTool 描述 MCP tools/list 返回的单个工具元信息。
type MCPTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

// MCPChatAskArguments 描述 chat.ask 工具的输入参数。
type MCPChatAskArguments struct {
	Message         string `json:"message"`
	KnowledgeBaseID string `json:"knowledgeBaseId,omitempty"`
	DocumentID      string `json:"documentId,omitempty"`
	ConversationID  string `json:"conversationId,omitempty"`
}

// MCPKnowledgeBaseSearchArguments 描述 knowledge_base.search 工具的输入参数。
type MCPKnowledgeBaseSearchArguments struct {
	Query           string `json:"query"`
	KnowledgeBaseID string `json:"knowledgeBaseId,omitempty"`
	DocumentID      string `json:"documentId,omitempty"`
	TopK            int    `json:"topK,omitempty"`
}

// MCPKnowledgeBaseCreateArguments 描述 knowledge_base.create 工具的输入参数。
type MCPKnowledgeBaseCreateArguments struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// MCPDocumentUploadArguments 描述 document.upload 工具的输入参数。
type MCPDocumentUploadArguments struct {
	KnowledgeBaseID string `json:"knowledgeBaseId"`
	Filename        string `json:"filename,omitempty"`
	Content         string `json:"content"`
}

// MCPDocumentAppendArguments 描述 document.append 工具的输入参数。
type MCPDocumentAppendArguments struct {
	KnowledgeBaseID string `json:"knowledgeBaseId"`
	DocumentID      string `json:"documentId"`
	Content         string `json:"content"`
}

// MCPDocumentUpdateArguments 描述 document.update 工具的输入参数。
type MCPDocumentUpdateArguments struct {
	KnowledgeBaseID string `json:"knowledgeBaseId"`
	DocumentID      string `json:"documentId"`
	Content         string `json:"content"`
}

// MCPDocumentDeleteArguments 描述 document.delete 工具的输入参数。
type MCPDocumentDeleteArguments struct {
	KnowledgeBaseID string `json:"knowledgeBaseId"`
	DocumentID      string `json:"documentId"`
}

// MCPResourceReadParams 描述 resources/read 的输入参数。
type MCPResourceReadParams struct {
	URI string `json:"uri"`
}
