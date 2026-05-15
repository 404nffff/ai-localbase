package handler

import (
	"net/http"

	"ai-localbase/internal/model"
	"ai-localbase/internal/service"

	"github.com/gin-gonic/gin"
)

// MCPHandler 负责处理 /mcp 单端点 JSON-RPC 请求。
type MCPHandler struct {
	mcpService *service.MCPService
}

// NewMCPHandler 创建 MCP 协议入口处理器。
func NewMCPHandler(mcpService *service.MCPService) *MCPHandler {
	return &MCPHandler{mcpService: mcpService}
}

// Handle godoc
// @Summary 处理 MCP JSON-RPC 请求
// @Description 统一处理 /mcp 单端点的 JSON-RPC over HTTP 请求，当前支持 initialize。
// @Tags MCP
// @Accept json
// @Produce json
// @Param request body model.JSONRPCRequest true "MCP JSON-RPC 请求"
// @Success 200 {object} model.JSONRPCResponse
// @Router /mcp [post]
func (h *MCPHandler) Handle(c *gin.Context) {
	var req model.JSONRPCRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, model.JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      nil,
			Error: &model.JSONRPCError{
				Code:    -32700,
				Message: "parse error",
			},
		})
		return
	}

	// MCP Streamable HTTP 规范要求纯 notification 输入返回 202 且不带响应体。
	if req.ID == nil {
		c.Status(http.StatusAccepted)
		return
	}

	c.JSON(http.StatusOK, h.mcpService.Handle(req))
}

// CallTool godoc
// @Summary 通过普通 API 调用 MCP 工具
// @Description 复用 MCP tools/call 能力，但以 /api/mcp/tools/:name/call 暴露结构化 JSON 响应。
// @Tags MCP
// @Accept json
// @Produce json
// @Param name path string true "MCP 工具名称"
// @Param request body model.MCPAPIToolCallRequest true "MCP 工具调用请求"
// @Success 200 {object} map[string]interface{}
// @Router /api/mcp/tools/{name}/call [post]
func (h *MCPHandler) CallTool(c *gin.Context) {
	var req model.MCPAPIToolCallRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid mcp tool call request body"})
		return
	}

	name := c.Param("name")
	result, err := h.mcpService.CallTool(name, req.Arguments)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"name":              name,
		"content":           result["content"],
		"structuredContent": result["structuredContent"],
		"isError":           result["isError"],
	})
}
