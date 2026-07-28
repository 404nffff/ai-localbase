package handler

import (
	"context"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"ai-localbase/internal/mcp"
	"ai-localbase/internal/model"
	"ai-localbase/internal/service"
	"ai-localbase/internal/util"

	"github.com/gin-gonic/gin"
)

type AppHandler struct {
	serverConfig model.ServerConfig
	appService   *service.AppService
	llmService   *service.LLMService
	toolPlanner  *mcp.ToolUsePlanner
}

func NewAppHandler(serverConfig model.ServerConfig, appService *service.AppService, llmService *service.LLMService, toolPlanner *mcp.ToolUsePlanner) *AppHandler {
	return &AppHandler{
		serverConfig: serverConfig,
		appService:   appService,
		llmService:   llmService,
		toolPlanner:  toolPlanner,
	}
}

func (h *AppHandler) Root(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"name":    "AI LocalBase Backend",
		"version": "v0.3.0",
		"status":  "running",
	})
}

func (h *AppHandler) Health(c *gin.Context) {
	c.JSON(http.StatusOK, model.HealthResponse{
		Status: "ok",
		Name:   "ai-localbase-backend",
		Config: h.appService.GetHealthConfigMap(h.serverConfig),
	})
}

func (h *AppHandler) GetConfig(c *gin.Context) {
	c.JSON(http.StatusOK, h.appService.GetPublicConfig())
}

func (h *AppHandler) ResetMCPToken(c *gin.Context) {
	mcpConfig, err := h.appService.ResetMCPToken()
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "disabled") {
			writeError(c, http.StatusForbidden, err.Error())
			return
		}
		writeError(c, http.StatusInternalServerError, err.Error())
		return
	}

	c.JSON(http.StatusOK, gin.H{"mcp": mcpConfig})
}

func (h *AppHandler) CreateMCPDangerConfirmation(c *gin.Context) {
	var req model.MCPDangerConfirmationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, "invalid mcp danger confirmation request body")
		return
	}

	confirmation, err := h.appService.CreateMCPDangerConfirmation(req)
	if err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}
	c.JSON(http.StatusCreated, confirmation)
}

func (h *AppHandler) ListConversations(c *gin.Context) {
	items, err := h.appService.ListConversations()
	if err != nil {
		writeError(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (h *AppHandler) GetConversation(c *gin.Context) {
	conversation, err := h.appService.GetConversation(c.Param("id"))
	if err != nil {
		writeError(c, http.StatusInternalServerError, err.Error())
		return
	}
	if conversation == nil {
		writeError(c, http.StatusNotFound, "conversation not found")
		return
	}
	c.JSON(http.StatusOK, conversation)
}

func (h *AppHandler) SaveConversation(c *gin.Context) {
	var req model.SaveConversationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, "invalid conversation request body")
		return
	}
	if strings.TrimSpace(req.ID) == "" {
		req.ID = c.Param("id")
	}
	conversation, err := h.appService.SaveConversation(req)
	if err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}
	c.JSON(http.StatusOK, conversation)
}

func (h *AppHandler) DeleteConversation(c *gin.Context) {
	if err := h.appService.DeleteConversation(c.Param("id")); err != nil {
		writeError(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "conversation deleted", "id": c.Param("id")})
}

func (h *AppHandler) EditMessage(c *gin.Context) {
	var req model.EditMessageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, "invalid edit message request body")
		return
	}

	conversation, err := h.appService.EditMessage(c.Param("id"), c.Param("msgId"), req)
	if err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}

	c.JSON(http.StatusOK, gin.H{"conversation": conversation})
}

func (h *AppHandler) DeleteMessage(c *gin.Context) {
	conversation, err := h.appService.DeleteMessage(c.Param("id"), c.Param("msgId"))
	if err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}

	c.JSON(http.StatusOK, gin.H{"conversation": conversation})
}

func (h *AppHandler) RegenerateMessage(c *gin.Context) {
	conversationID := c.Param("id")
	messageID := c.Param("msgId")

	conversation, err := h.appService.GetConversation(conversationID)
	if err != nil {
		writeError(c, http.StatusInternalServerError, err.Error())
		return
	}
	if conversation == nil {
		writeError(c, http.StatusNotFound, "conversation not found")
		return
	}

	messageIndex := -1
	for i, msg := range conversation.Messages {
		if msg.ID == messageID {
			messageIndex = i
			break
		}
	}
	if messageIndex == -1 {
		writeError(c, http.StatusNotFound, "message not found")
		return
	}
	if messageIndex == 0 {
		writeError(c, http.StatusBadRequest, "cannot regenerate first message")
		return
	}

	previousMessage := conversation.Messages[messageIndex-1]
	if strings.ToLower(strings.TrimSpace(previousMessage.Role)) != "user" {
		writeError(c, http.StatusBadRequest, "can only regenerate assistant responses following user messages")
		return
	}

	truncatedMessages := conversation.Messages[:messageIndex]
	chatMessages := make([]model.ChatMessage, 0, len(truncatedMessages))
	for _, msg := range truncatedMessages {
		if strings.ToLower(strings.TrimSpace(msg.Role)) == "system" {
			continue
		}
		chatMessages = append(chatMessages, model.ChatMessage{
			Role:    msg.Role,
			Content: msg.Content,
		})
	}

	req := model.ChatCompletionRequest{
		ConversationID:  conversationID,
		KnowledgeBaseID: conversation.KnowledgeBaseID,
		DocumentID:      conversation.DocumentID,
		Messages:        chatMessages,
	}

	preparedReq, sources, err := h.prepareChatRequest(c.Request.Context(), req)
	if err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}

	response, err := h.llmService.Chat(preparedReq)
	if err != nil {
		writeError(c, http.StatusBadGateway, err.Error())
		return
	}

	if response.Metadata == nil {
		response.Metadata = map[string]any{}
	}
	assistantMessage := firstAssistantChoice(response)
	if assistantMessage != nil {
		sources = calibrateCitationSources(latestUserQuestion(req.Messages), assistantMessage.Content, sources)
	}
	response.Metadata["sources"] = sources
	response.Metadata["knowledgeBaseId"] = req.KnowledgeBaseID
	response.Metadata["documentId"] = req.DocumentID
	response.Metadata["toolUse"] = buildToolUseMetadata(sources)

	if assistantMessage != nil {
		updatedConversation, saveErr := h.appService.SaveConversation(model.SaveConversationRequest{
			ID:              conversationID,
			Title:           conversation.Title,
			KnowledgeBaseID: conversation.KnowledgeBaseID,
			DocumentID:      conversation.DocumentID,
			Messages:        buildStoredConversationMessages(chatMessages, assistantMessage.Content, response.Metadata),
		})
		if saveErr != nil {
			writeError(c, http.StatusInternalServerError, saveErr.Error())
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"conversation": updatedConversation,
			"response":     response,
		})
		return
	}

	writeError(c, http.StatusInternalServerError, "failed to regenerate response")
}

func (h *AppHandler) ExportConversation(c *gin.Context) {
	conversationID := c.Param("id")

	markdown, err := h.appService.ExportConversation(conversationID)
	if err != nil {
		writeError(c, http.StatusInternalServerError, err.Error())
		return
	}

	c.Header("Content-Type", "text/markdown; charset=utf-8")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"conversation-%s.md\"", conversationID))
	c.String(http.StatusOK, markdown)
}

func (h *AppHandler) UpdateConfig(c *gin.Context) {
	var req model.ConfigUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, "invalid config request body")
		return
	}

	if _, err := h.appService.UpdateConfig(req); err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}

	c.JSON(http.StatusOK, h.appService.GetPublicConfig())
}

func (h *AppHandler) ListKnowledgeBases(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"items": h.appService.ListKnowledgeBases()})
}

func (h *AppHandler) CreateKnowledgeBase(c *gin.Context) {
	var req model.KnowledgeBaseInput
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, "invalid knowledge base request body")
		return
	}

	knowledgeBase, err := h.appService.CreateKnowledgeBase(req)
	if err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}

	c.JSON(http.StatusCreated, knowledgeBase)
}

func (h *AppHandler) DeleteKnowledgeBase(c *gin.Context) {
	remaining, err := h.appService.DeleteKnowledgeBase(c.Param("id"))
	if err != nil {
		writeError(c, http.StatusNotFound, err.Error())
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":   "knowledge base deleted",
		"remaining": remaining,
	})
}

func (h *AppHandler) GetKnowledgeBaseHealth(c *gin.Context) {
	health, err := h.appService.GetKnowledgeBaseHealth(c.Param("id"))
	if err != nil {
		writeError(c, http.StatusNotFound, err.Error())
		return
	}

	c.JSON(http.StatusOK, health)
}

func (h *AppHandler) ListDocuments(c *gin.Context) {
	items, err := h.appService.GetKnowledgeBaseDocuments(c.Param("id"))
	if err != nil {
		writeError(c, http.StatusNotFound, err.Error())
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"knowledgeBaseId": c.Param("id"),
		"items":           items,
	})
}

func (h *AppHandler) ListEvalDatasets(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"items": h.appService.ListEvalDatasets(c.Query("knowledgeBaseId")),
	})
}

func (h *AppHandler) ListEvalRuns(c *gin.Context) {
	c.JSON(http.StatusOK, model.EvalRunListResponse{
		Items: h.appService.ListEvalRuns(c.Query("knowledgeBaseId"), c.Query("datasetId")),
	})
}

func (h *AppHandler) GenerateEvalDataset(c *gin.Context) {
	var req model.GenerateEvalDatasetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, "invalid eval dataset request body")
		return
	}

	response, err := h.appService.GenerateEvalDataset(req)
	if err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}

	c.JSON(http.StatusOK, response)
}

func (h *AppHandler) GetEvalDataset(c *gin.Context) {
	dataset, err := h.appService.GetEvalDataset(c.Param("datasetId"))
	if err != nil {
		writeError(c, http.StatusNotFound, err.Error())
		return
	}

	c.JSON(http.StatusOK, dataset)
}

func (h *AppHandler) AddEvalDatasetCandidate(c *gin.Context) {
	var req model.AddEvalDatasetCandidateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, "invalid eval candidate payload")
		return
	}

	response, err := h.appService.AddEvalDatasetCandidate(req)
	if err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}

	c.JSON(http.StatusOK, response)
}

func (h *AppHandler) UpdateEvalDatasetItem(c *gin.Context) {
	var req model.UpdateEvalDatasetItemRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, "invalid eval dataset item payload")
		return
	}

	response, err := h.appService.UpdateEvalDatasetItem(c.Param("datasetId"), c.Param("itemId"), req)
	if err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}

	c.JSON(http.StatusOK, response)
}

func (h *AppHandler) DeleteEvalDatasetItem(c *gin.Context) {
	response, err := h.appService.DeleteEvalDatasetItem(c.Param("datasetId"), c.Param("itemId"))
	if err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}

	c.JSON(http.StatusOK, response)
}

func (h *AppHandler) RunEvalDataset(c *gin.Context) {
	var req model.RunEvalDatasetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, "invalid eval run request body")
		return
	}

	response, err := h.appService.RunEvalDataset(c.Param("datasetId"), req)
	if err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}

	c.JSON(http.StatusOK, response)
}

func (h *AppHandler) DeleteEvalDataset(c *gin.Context) {
	if err := h.appService.DeleteEvalDataset(c.Param("datasetId")); err != nil {
		writeError(c, http.StatusNotFound, err.Error())
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "eval dataset deleted",
		"id":      c.Param("datasetId"),
	})
}

func (h *AppHandler) DebugRetrieve(c *gin.Context) {
	var req model.RetrievalDebugRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, "invalid retrieval debug request body")
		return
	}
	req.KnowledgeBaseID = c.Param("id")

	response, err := h.appService.DebugRetrieve(req)
	if err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}

	c.JSON(http.StatusOK, response)
}

func (h *AppHandler) UploadToKnowledgeBase(c *gin.Context) {
	h.handleUpload(c, c.Param("id"))
}

func (h *AppHandler) Upload(c *gin.Context) {
	h.handleUpload(c, "")
}

func (h *AppHandler) StageUpload(c *gin.Context) {
	file, ok := h.uploadFileFromRequest(c)
	if !ok {
		return
	}
	if err := validateUploadFile(file, h.appService.GetConfig(), h.serverConfig.MaxUploadBytes); err != nil {
		writeUploadValidationError(c, err)
		return
	}
	staged, err := h.appService.StageUpload(file, "http")
	if err != nil {
		writeError(c, http.StatusBadGateway, err.Error())
		return
	}
	c.JSON(http.StatusOK, model.StageUploadResponse{
		Message:  "file staged successfully",
		Staged:   staged,
		UploadID: staged.ID,
	})
}

func (h *AppHandler) DeleteDocument(c *gin.Context) {
	removedDocument, err := h.appService.DeleteDocument(c.Param("id"), c.Param("documentId"))
	if err != nil {
		writeError(c, http.StatusNotFound, err.Error())
		return
	}

	_ = os.Remove(removedDocument.Path)

	c.JSON(http.StatusOK, gin.H{
		"message":         "document deleted",
		"knowledgeBaseId": c.Param("id"),
		"documentId":      c.Param("documentId"),
	})
}

func (h *AppHandler) GetDocumentDetail(c *gin.Context) {
	detail, err := h.appService.GetDocumentDetail(c.Param("id"), c.Param("documentId"), c.Query("focusChunkId"))
	if err != nil {
		writeError(c, http.StatusNotFound, err.Error())
		return
	}

	c.JSON(http.StatusOK, detail)
}

func (h *AppHandler) ReindexDocument(c *gin.Context) {
	document, err := h.appService.ReindexDocument(c.Param("id"), c.Param("documentId"))
	if err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":         "document reindexed",
		"knowledgeBaseId": c.Param("id"),
		"document":        document,
	})
}

func (h *AppHandler) ChatCompletions(c *gin.Context) {
	var req model.ChatCompletionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, "invalid chat request body")
		return
	}

	preparedReq, sources, err := h.prepareChatRequest(c.Request.Context(), req)
	if err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}

	response, err := h.llmService.Chat(preparedReq)
	if err != nil {
		writeError(c, http.StatusBadGateway, err.Error())
		return
	}

	if response.Metadata == nil {
		response.Metadata = map[string]any{}
	}
	assistantMessage := firstAssistantChoice(response)
	if assistantMessage != nil {
		sources = calibrateCitationSources(latestUserQuestion(req.Messages), assistantMessage.Content, sources)
	}
	response.Metadata["sources"] = sources
	response.Metadata["knowledgeBaseId"] = req.KnowledgeBaseID
	response.Metadata["documentId"] = req.DocumentID
	response.Metadata["toolUse"] = buildToolUseMetadata(sources)

	if assistantMessage != nil {
		_, saveErr := h.appService.SaveConversation(model.SaveConversationRequest{
			ID:              req.ConversationID,
			Title:           "",
			KnowledgeBaseID: req.KnowledgeBaseID,
			DocumentID:      req.DocumentID,
			Messages:        buildStoredConversationMessages(req.Messages, assistantMessage.Content, response.Metadata),
		})
		if saveErr != nil {
			writeError(c, http.StatusInternalServerError, saveErr.Error())
			return
		}
	}

	c.JSON(http.StatusOK, response)
}

func (h *AppHandler) ChatCompletionsStream(c *gin.Context) {
	var req model.ChatCompletionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, "invalid chat request body")
		return
	}

	preparedReq, sources, err := h.prepareChatRequest(c.Request.Context(), req)
	if err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}

	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		writeError(c, http.StatusInternalServerError, "streaming is not supported")
		return
	}

	initialMeta := gin.H{
		"sources":         []map[string]string{},
		"knowledgeBaseId": req.KnowledgeBaseID,
		"documentId":      req.DocumentID,
		"toolUse":         []model.ToolUseMetadata{},
	}
	c.SSEvent("meta", initialMeta)
	flusher.Flush()

	assistantContent := strings.Builder{}
	streamErr := h.llmService.StreamChat(preparedReq, func(chunk string) error {
		assistantContent.WriteString(chunk)
		c.SSEvent("chunk", gin.H{"content": chunk})
		flusher.Flush()
		return nil
	})
	if streamErr != nil {
		c.SSEvent("error", gin.H{"error": streamErr.Error()})
		flusher.Flush()
		return
	}

	fullAssistantContent := assistantContent.String()
	sources = calibrateCitationSources(latestUserQuestion(req.Messages), fullAssistantContent, sources)
	responseMetadata := map[string]any{
		"sources":         sources,
		"knowledgeBaseId": req.KnowledgeBaseID,
		"documentId":      req.DocumentID,
		"toolUse":         buildToolUseMetadata(sources),
	}
	_, saveErr := h.appService.SaveConversation(model.SaveConversationRequest{
		ID:              req.ConversationID,
		Title:           "",
		KnowledgeBaseID: req.KnowledgeBaseID,
		DocumentID:      req.DocumentID,
		Messages:        buildStoredConversationMessages(req.Messages, fullAssistantContent, responseMetadata),
	})
	if saveErr != nil {
		c.SSEvent("error", gin.H{"error": saveErr.Error()})
		flusher.Flush()
		return
	}

	c.SSEvent("done", gin.H{"content": fullAssistantContent, "metadata": responseMetadata})
	flusher.Flush()
}

func (h *AppHandler) prepareChatRequest(ctx context.Context, req model.ChatCompletionRequest) (model.ChatCompletionRequest, []map[string]string, error) {
	if len(req.Messages) == 0 {
		return model.ChatCompletionRequest{}, nil, fmt.Errorf("messages cannot be empty")
	}

	latestQuestion := latestUserQuestion(req.Messages)
	skipKnowledgeRetrieval := isDirectConversationMessage(latestQuestion)
	retrievalContext := ""
	retrievalSources := []map[string]string(nil)
	toolUseContext := ""
	toolUseSources := []map[string]string(nil)
	contextSummary := ""
	contextSources := []map[string]string(nil)
	if !skipKnowledgeRetrieval {
		var err error
		retrievalContext, retrievalSources, err = h.appService.BuildRetrievalContext(req)
		if err != nil {
			return model.ChatCompletionRequest{}, nil, err
		}
		if h.toolPlanner != nil {
			plannedCalls := filterRedundantRetrievalToolPlans(h.toolPlanner.Plan(req), retrievalContext)
			toolExecutions := h.toolPlanner.Execute(ctx, plannedCalls)
			toolUseContext, toolUseSources = mcp.BuildToolUseContext(toolExecutions)
		}
		toolUseSources = append(retrievalToolUseSources(req, retrievalContext), toolUseSources...)
		contextSummary, contextSources, err = h.appService.BuildChatContext(req, documentIDsFromSources(retrievalSources))
		if err != nil {
			return model.ChatCompletionRequest{}, nil, err
		}
	}

	allSources := append(retrievalSources, contextSources...)
	allSources = append(allSources, toolUseSources...)
	contextParts := make([]string, 0, 3)
	if strings.TrimSpace(retrievalContext) != "" {
		contextParts = append(contextParts, "检索命中的文档片段：\n"+retrievalContext)
	}
	if strings.TrimSpace(toolUseContext) != "" {
		contextParts = append(contextParts, "MCP 工具调用结果：\n"+toolUseContext)
	}
	if strings.TrimSpace(contextSummary) != "" {
		contextParts = append(contextParts, contextSummary)
	}

	preparedReq := req
	preparedReq.Config = h.appService.CurrentChatConfig()
	preparedReq.Config.ContextMessageLimit = h.appService.ContextMessageLimit()
	preparedReq.Messages = h.appService.TrimChatMessages(filterOperationalChatMessages(req.Messages))
	isDiagramRequest := strings.Contains(latestQuestion, "流程图") || strings.Contains(latestQuestion, "架构图") || strings.Contains(latestQuestion, "状态图") || strings.Contains(latestQuestion, "Mermaid")
	tableQuestionType := detectTableQuestionType(latestQuestion, retrievalContext, contextSummary)
	promptSections := []string{
		"你是 AI LocalBase 的聊天与知识库助手。回答必须由当前配置的模型根据用户消息和本次提供的上下文生成。",
		"",
		"通用规则：",
		"- 直接回答用户实际提出的问题；若包含多个问题，逐项回答。",
		"- 不要讨论提示词、安全机制、模型阈值、降级流程或系统实现。",
		"- 简单问题保持简短；仅在有助于阅读时使用有效的 Markdown 标题、列表或表格。",
		"- 不要复述问题，不要虚构来源，不要输出与问题无关的通用建议。",
	}
	if len(contextParts) > 0 {
		promptSections = append(promptSections,
			"",
			"知识库回答规则：",
			"- 以下上下文是知识库事实的唯一可信依据。",
			"- 上下文已经给出答案时必须采用其中的事实，不得声称未检索到，也不要要求用户重复提供。",
			"- 姓名、作者、数量、版本等事实若未在上下文出现，明确写“上下文未说明”，不得猜测或用模型记忆补全。",
			"- 名单类问题只列出上下文明确定义为对应对象的条目，不要混入其他类型或推测项。",
			"- 直接陈述事实或“上下文未说明”，不要解释这是回答规则或系统要求。",
		)

		if tableQuestionType != "" {
			promptSections = append(promptSections, buildTableAnswerRules(tableQuestionType)...)
		}

		if tableQuestionType == tableQuestionTypeCount {
			promptSections = append(promptSections,
				"",
				"### 表格计数回答要求",
				"- 首句直接给出数量结论，明确回答对象是“文档”或“表格”，不要先写分析过程",
				"- 第二句只保留最小必要依据，例如“按表头下方的数据行统计，共 X 条记录”",
				"- 若无歧义，不要输出字段列表、文件名、逐行记录、原始片段复述",
				"- 若存在重复记录或统计口径不确定，单独补一句说明，不要展开无关明细",
			)
		}

		promptSections = append(promptSections,
			"",
			"上下文：",
			strings.Join(contextParts, "\n\n"),
		)
	}
	if isDiagramRequest {
		promptSections = append(promptSections,
			"",
			"### Mermaid 输出规则",
			"- 只输出一句简短标题和一个 Mermaid 代码块，不要添加额外解释。",
			"- 使用标准 Mermaid 围栏，每条节点、连线、classDef、style、subgraph 和 end 语句单独一行。",
			"- 禁止压缩 Mermaid 语句；无法保证语法正确时改用普通 Markdown 有序列表。",
		)
	}

	preparedReq.Messages = append([]model.ChatMessage{{
		Role:    "system",
		Content: strings.Join(promptSections, "\n"),
	}}, preparedReq.Messages...)

	return preparedReq, allSources, nil
}

func isDirectConversationMessage(question string) bool {
	normalized := strings.ToLower(strings.Trim(strings.TrimSpace(question), " ，,。.!！?？~～"))
	if normalized == "" || len([]rune(normalized)) > 24 {
		return false
	}
	for _, item := range []string{
		"你好", "您好", "嗨", "哈喽", "hello", "hi", "hey", "早上好", "下午好", "晚上好",
		"你是谁", "你是什么", "介绍一下你", "自我介绍", "who are you", "what are you",
	} {
		if normalized == item {
			return true
		}
	}
	return false
}

func filterRedundantRetrievalToolPlans(plans []mcp.PlannedToolCall, retrievalContext string) []mcp.PlannedToolCall {
	if strings.TrimSpace(retrievalContext) == "" || len(plans) == 0 {
		return plans
	}
	filtered := make([]mcp.PlannedToolCall, 0, len(plans))
	for _, plan := range plans {
		if plan.ToolName == "search_knowledge_base" || plan.ToolName == "search_document" {
			continue
		}
		filtered = append(filtered, plan)
	}
	return filtered
}

func retrievalToolUseSources(req model.ChatCompletionRequest, retrievalContext string) []map[string]string {
	if strings.TrimSpace(retrievalContext) == "" {
		return nil
	}
	toolName := ""
	switch {
	case strings.TrimSpace(req.DocumentID) != "":
		toolName = "search_document"
	case strings.TrimSpace(req.KnowledgeBaseID) != "":
		toolName = "search_knowledge_base"
	default:
		return nil
	}
	return []map[string]string{{
		"toolName":        toolName,
		"permissionLevel": "read-only",
		"status":          "ok",
	}}
}

func filterOperationalChatMessages(messages []model.ChatMessage) []model.ChatMessage {
	filtered := make([]model.ChatMessage, 0, len(messages))
	for _, message := range messages {
		if strings.EqualFold(strings.TrimSpace(message.Role), "assistant") && service.IsLegacyOperationalAssistantContent(message.Content) {
			continue
		}
		filtered = append(filtered, message)
	}
	return filtered
}

func documentIDsFromSources(sources []map[string]string) []string {
	ids := make([]string, 0, len(sources))
	seen := make(map[string]struct{}, len(sources))
	for _, source := range sources {
		documentID := strings.TrimSpace(source["documentId"])
		if documentID == "" {
			continue
		}
		if _, exists := seen[documentID]; exists {
			continue
		}
		seen[documentID] = struct{}{}
		ids = append(ids, documentID)
	}
	return ids
}

const (
	tableQuestionTypeCount = "count"
	tableQuestionTypeList  = "list"
)

func latestUserQuestion(messages []model.ChatMessage) string {
	for index := len(messages) - 1; index >= 0; index-- {
		if strings.EqualFold(strings.TrimSpace(messages[index].Role), "user") {
			return strings.TrimSpace(messages[index].Content)
		}
	}
	return ""
}

func detectTableQuestionType(question, retrievalContext, contextSummary string) string {
	if !looksLikeStructuredTableContext(retrievalContext, contextSummary) {
		return ""
	}
	if isTableCountQuestion(question) {
		return tableQuestionTypeCount
	}
	if isTableListQuestion(question) {
		return tableQuestionTypeList
	}
	return ""
}

func looksLikeStructuredTableContext(retrievalContext, contextSummary string) bool {
	combined := retrievalContext + "\n" + contextSummary
	return strings.Contains(combined, "字段：") && strings.Contains(combined, "数据行数：")
}

func isTableCountQuestion(question string) bool {
	trimmed := strings.TrimSpace(question)
	if trimmed == "" {
		return false
	}
	countMarkers := []string{"多少", "几", "数量", "总数", "共", "总共有"}
	entityMarkers := []string{"员工", "老师", "教师", "人员", "记录", "行", "条", "名单"}
	if !containsAny(trimmed, countMarkers) {
		return false
	}
	return containsAny(trimmed, entityMarkers)
}

func isTableListQuestion(question string) bool {
	trimmed := strings.TrimSpace(question)
	if trimmed == "" {
		return false
	}
	listMarkers := []string{"有哪些", "列出", "名单", "分别是", "都有谁", "分别是谁"}
	return containsAny(trimmed, listMarkers)
}

func containsAny(text string, markers []string) bool {
	for _, marker := range markers {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func buildTableAnswerRules(questionType string) []string {
	rules := []string{
		"",
		"### 表格问答附加规则",
		"- 先回答问题本身，再补充最小必要依据；不要把检索片段直接改写成长段流水账",
		"- 非用户明确要求时，不要罗列全部字段、全部记录、文件内部过程信息",
		"- 对表格类问题优先使用短句、列表或表格，不要把多个字段拼成一整段",
		"- 若上下文出现重复片段，只保留一次结论和一次依据",
	}
	if questionType == tableQuestionTypeList {
		rules = append(rules,
			"- 若用户要求列举名单，先给总数，再按列表列出名称；无关字段不要混入名单中",
		)
	}
	return rules
}

func (h *AppHandler) handleUpload(c *gin.Context, candidateKnowledgeBaseID string) {
	file, ok := h.uploadFileFromRequest(c)
	if !ok {
		return
	}

	if err := validateUploadFile(file, h.appService.GetConfig(), h.serverConfig.MaxUploadBytes); err != nil {
		writeUploadValidationError(c, err)
		return
	}

	resolvedCandidateID := strings.TrimSpace(candidateKnowledgeBaseID)
	if resolvedCandidateID == "" {
		resolvedCandidateID = c.PostForm("knowledgeBaseId")
	}

	knowledgeBaseID, err := h.appService.ResolveKnowledgeBaseID(resolvedCandidateID)
	if err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}

	storedName := fmt.Sprintf("%d_%s", util.NowUnixNano(), util.SanitizeFilename(file.Filename))
	destination := filepath.Join(h.serverConfig.UploadDir, storedName)
	if err := c.SaveUploadedFile(file, destination); err != nil {
		writeError(c, http.StatusInternalServerError, "failed to save uploaded file")
		return
	}

	document := model.Document{
		ID:              util.NextID("doc"),
		KnowledgeBaseID: knowledgeBaseID,
		Name:            file.Filename,
		Size:            file.Size,
		SizeLabel:       util.FormatFileSize(file.Size),
		UploadedAt:      util.NowRFC3339(),
		Status:          "processing",
		Path:            destination,
		ContentPreview:  util.ExtractContentPreview(destination),
	}

	uploaded, err := h.appService.IndexDocument(document)
	if err != nil {
		_ = os.Remove(destination)
		writeError(c, http.StatusBadGateway, err.Error())
		return
	}

	c.JSON(http.StatusOK,
		model.UploadResponse{
			Message:       "file uploaded successfully",
			KnowledgeBase: knowledgeBaseID,
			Uploaded:      uploaded,
		})
}

func buildToolUseMetadata(sources []map[string]string) []model.ToolUseMetadata {
	items := make([]model.ToolUseMetadata, 0)
	for _, source := range sources {
		toolName := strings.TrimSpace(source["toolName"])
		if toolName == "" {
			continue
		}
		items = append(items, model.ToolUseMetadata{
			ToolName:        toolName,
			PermissionLevel: source["permissionLevel"],
		})
	}
	return items
}

type scoredCitationSource struct {
	source     map[string]string
	score      int
	index      int
	answerHits int
	queryHits  int
}

func calibrateCitationSources(question, answer string, sources []map[string]string) []map[string]string {
	if len(sources) == 0 {
		return nil
	}

	answerTerms := citationTerms(answer)
	queryTerms := citationTerms(question)
	scored := make([]scoredCitationSource, 0, len(sources))
	for index, source := range sources {
		if sourceAlwaysCitable(source) {
			next := cloneStringMap(source)
			next["citationConfidence"] = "high"
			scored = append(scored, scoredCitationSource{
				source: next,
				score:  1000 - index,
				index:  index,
			})
			continue
		}

		text := citationSourceText(source)
		answerHits := citationHitCount(answerTerms, text)
		queryHits := citationHitCount(queryTerms, text)
		rawScore := parseCitationRawScore(source["score"])
		score := answerHits*6 + queryHits*2 + int(rawScore*3)
		if !sourcePassesCitationGate(answerHits, queryHits, rawScore, len(answerTerms)) {
			continue
		}

		next := cloneStringMap(source)
		next["citationConfidence"] = citationConfidence(answerHits, queryHits, rawScore)
		scored = append(scored, scoredCitationSource{
			source:     next,
			score:      score,
			index:      index,
			answerHits: answerHits,
			queryHits:  queryHits,
		})
	}

	if len(scored) == 0 {
		return nil
	}

	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score == scored[j].score {
			return scored[i].index < scored[j].index
		}
		return scored[i].score > scored[j].score
	})

	limit := 4
	out := make([]map[string]string, 0, minInt(len(scored), limit))
	seen := make(map[string]struct{}, len(scored))
	for _, item := range scored {
		key := strings.TrimSpace(item.source["chunkId"])
		if key == "" {
			key = strings.TrimSpace(item.source["documentId"]) + ":" + strings.TrimSpace(item.source["snippet"])
		}
		if key != "" {
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
		}
		out = append(out, item.source)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func sourceAlwaysCitable(source map[string]string) bool {
	if strings.TrimSpace(source["toolName"]) != "" {
		return true
	}
	switch strings.TrimSpace(source["sourceType"]) {
	case "structured-data":
		return true
	default:
		return false
	}
}

func citationSourceText(source map[string]string) string {
	parts := []string{
		source["snippet"],
		source["documentName"],
		source["chunkKind"],
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func sourcePassesCitationGate(answerHits, queryHits int, rawScore float64, answerTermCount int) bool {
	if answerHits >= 2 {
		return true
	}
	if answerHits >= 1 && queryHits >= 1 {
		return true
	}
	if answerTermCount == 0 && queryHits >= 2 && rawScore >= 0.65 {
		return true
	}
	return false
}

func citationConfidence(answerHits, queryHits int, rawScore float64) string {
	switch {
	case answerHits >= 3:
		return "high"
	case answerHits >= 2 || (answerHits >= 1 && queryHits >= 2):
		return "medium"
	case rawScore >= 0.8:
		return "medium"
	default:
		return "low"
	}
}

func parseCitationRawScore(value string) float64 {
	parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil {
		return 0
	}
	if parsed < 0 {
		return 0
	}
	if parsed > 1 {
		return 1
	}
	return parsed
}

func citationHitCount(terms []string, text string) int {
	if len(terms) == 0 || strings.TrimSpace(text) == "" {
		return 0
	}
	lowered := strings.ToLower(text)
	hits := 0
	for _, term := range terms {
		if strings.Contains(lowered, term) {
			hits++
		}
	}
	return hits
}

func citationTerms(text string) []string {
	text = strings.ToLower(strings.TrimSpace(text))
	if text == "" {
		return nil
	}

	terms := make([]string, 0)
	var wordRunes []rune
	var hanRunes []rune
	flushWord := func() {
		if len(wordRunes) >= 2 {
			terms = append(terms, string(wordRunes))
		}
		wordRunes = wordRunes[:0]
	}
	flushHan := func() {
		if len(hanRunes) >= 2 {
			maxN := minInt(4, len(hanRunes))
			for n := 2; n <= maxN; n++ {
				for i := 0; i+n <= len(hanRunes); i++ {
					terms = append(terms, string(hanRunes[i:i+n]))
				}
			}
		}
		hanRunes = hanRunes[:0]
	}

	for _, r := range text {
		switch {
		case unicode.In(r, unicode.Han):
			flushWord()
			hanRunes = append(hanRunes, r)
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			flushHan()
			wordRunes = append(wordRunes, r)
		default:
			flushWord()
			flushHan()
		}
	}
	flushWord()
	flushHan()

	stopTerms := map[string]struct{}{
		"什么": {}, "多少": {}, "几个": {}, "如何": {}, "怎么": {}, "是否": {},
		"是谁": {}, "哪些": {}, "有没有": {}, "请问": {}, "告诉": {}, "一下": {},
		"当前": {}, "核心": {}, "观点": {}, "信息": {}, "文档": {}, "内容": {},
		"回答": {}, "来源": {}, "显示": {}, "可以": {}, "进行": {}, "相关": {},
		"the": {}, "and": {}, "for": {}, "with": {}, "what": {}, "which": {},
		"who": {}, "how": {}, "where": {}, "when": {}, "is": {}, "are": {},
	}
	out := make([]string, 0, len(terms))
	seen := make(map[string]struct{}, len(terms))
	for _, term := range terms {
		term = strings.TrimSpace(strings.ToLower(term))
		if len([]rune(term)) < 2 {
			continue
		}
		if _, stop := stopTerms[term]; stop {
			continue
		}
		if _, exists := seen[term]; exists {
			continue
		}
		seen[term] = struct{}{}
		out = append(out, term)
	}
	return out
}

func cloneStringMap(input map[string]string) map[string]string {
	if input == nil {
		return nil
	}
	out := make(map[string]string, len(input)+1)
	for key, value := range input {
		out[key] = value
	}
	return out
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

const uploadRequestOverheadBytes int64 = 1024 * 1024

func (h *AppHandler) uploadFileFromRequest(c *gin.Context) (*multipart.FileHeader, bool) {
	if h.serverConfig.MaxUploadBytes > 0 {
		c.Request.Body = http.MaxBytesReader(
			c.Writer,
			c.Request.Body,
			h.serverConfig.MaxUploadBytes+uploadRequestOverheadBytes,
		)
	}

	file, err := c.FormFile("file")
	if err != nil {
		if strings.Contains(err.Error(), "request body too large") {
			writeError(c, http.StatusRequestEntityTooLarge, maxUploadSizeMessage(h.serverConfig.MaxUploadBytes))
			return nil, false
		}
		writeError(c, http.StatusBadRequest, "missing file field 'file'")
		return nil, false
	}

	return file, true
}

func validateUploadFile(file *multipart.FileHeader, cfg model.AppConfig, maxUploadBytes int64) error {
	if maxUploadBytes > 0 && file.Size > maxUploadBytes {
		return &uploadSizeError{
			Size:     file.Size,
			MaxBytes: maxUploadBytes,
		}
	}

	ext := strings.ToLower(filepath.Ext(file.Filename))
	allowed := map[string]struct{}{
		".txt": {},
		".md":  {},
		".pdf": {},
	}
	if service.IsSensitiveStructuredFileExtension(ext) {
		if !service.IsLocalOllamaConfig(cfg.Chat, cfg.Embedding) {
			return errSensitiveStructuredFileRequiresLocalOllama(ext)
		}
		allowed[ext] = struct{}{}
	}

	if _, ok := allowed[ext]; !ok {
		return errUnsupportedFileType(ext)
	}

	return nil
}

func writeUploadValidationError(c *gin.Context, err error) {
	var sizeErr *uploadSizeError
	if errors.As(err, &sizeErr) {
		writeError(c, http.StatusRequestEntityTooLarge, err.Error())
		return
	}
	writeError(c, http.StatusBadRequest, err.Error())
}

func maxUploadSizeMessage(maxUploadBytes int64) string {
	if maxUploadBytes <= 0 {
		return "uploaded file is too large"
	}
	return fmt.Sprintf("uploaded file is too large, max size is %s", util.FormatFileSize(maxUploadBytes))
}

type uploadSizeError struct {
	Size     int64
	MaxBytes int64
}

func (e *uploadSizeError) Error() string {
	return fmt.Sprintf(
		"uploaded file is too large: %s, max size is %s",
		util.FormatFileSize(e.Size),
		util.FormatFileSize(e.MaxBytes),
	)
}

func errUnsupportedFileType(ext string) error {
	if ext == "" {
		return fmt.Errorf("unsupported file type: missing extension, allowed types are .txt, .md, .pdf")
	}

	return &fileTypeError{Extension: ext}
}

func errSensitiveStructuredFileRequiresLocalOllama(ext string) error {
	return fmt.Errorf("sensitive structured file type %s requires local ollama for both chat and embedding", ext)
}

type fileTypeError struct {
	Extension string
}

func (e *fileTypeError) Error() string {
	return "unsupported file type: " + e.Extension + ", allowed types are .txt, .md, .pdf"
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
	stored = append(stored, assistantMessage)
	return stored
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

func writeError(c *gin.Context, statusCode int, message string) {
	requestID := strings.TrimSpace(c.GetHeader("X-Request-Id"))
	if requestID == "" {
		requestID = strings.TrimSpace(c.GetString("requestId"))
	}

	c.JSON(statusCode, model.APIError{
		Error: model.ErrorDetail{
			Code:      errorCodeFromStatus(statusCode),
			Message:   strings.TrimSpace(message),
			RequestID: requestID,
		},
	})
}

func errorCodeFromStatus(statusCode int) string {
	switch statusCode {
	case http.StatusBadRequest:
		return "bad_request"
	case http.StatusUnauthorized:
		return "unauthorized"
	case http.StatusForbidden:
		return "forbidden"
	case http.StatusNotFound:
		return "not_found"
	case http.StatusConflict:
		return "conflict"
	case http.StatusTooManyRequests:
		return "rate_limited"
	case http.StatusBadGateway:
		return "upstream_error"
	case http.StatusServiceUnavailable:
		return "service_unavailable"
	case http.StatusGatewayTimeout:
		return "timeout"
	default:
		if statusCode >= http.StatusInternalServerError {
			return "internal_error"
		}
		return "request_failed"
	}
}
