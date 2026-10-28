package handler

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"ai-localbase/internal/model"
	"ai-localbase/internal/service"

	"github.com/gin-gonic/gin"
)

// TestChatModel 用草稿配置实际调用模型，失败和本地降级回复均不算连通成功，不保存设置。
// @Summary 测试聊天模型
// @Param config body model.TestChatModelRequest true "当前草稿模型配置"
// @Success 200 {object} model.TestModelResponse "success 表示真实模型调用结果"
// @Failure 400 {object} map[string]string "请求格式错误"
// @Router /api/config/test-chat-model [post]
func (h *AppHandler) TestChatModel(c *gin.Context) {
	var req model.TestChatModelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, "invalid chat model test request")
		return
	}
	startedAt := time.Now()
	response, err := h.llmService.Chat(model.ChatCompletionRequest{
		Messages: []model.ChatMessage{{Role: "user", Content: "ping"}},
		Config: model.ChatModelConfig{
			Provider:              req.Provider,
			BaseURL:               req.BaseURL,
			Model:                 req.Model,
			APIKey:                req.APIKey,
			Temperature:           req.Temperature,
			UseDefaultTemperature: req.UseDefaultTemperature,
		},
	})
	latency := time.Since(startedAt).Milliseconds()
	if err != nil {
		c.JSON(http.StatusOK, model.TestModelResponse{Success: false, LatencyMs: latency, ErrorMessage: err.Error()})
		return
	}
	// 普通聊天的本地说明不是模型回复，必须保留上游失败给用户判断配置。
	if degraded, _ := response.Metadata["degraded"].(bool); degraded {
		c.JSON(http.StatusOK, model.TestModelResponse{Success: false, LatencyMs: latency, ErrorMessage: fmt.Sprint(response.Metadata["upstreamError"])})
		return
	}
	if len(response.Choices) == 0 || strings.TrimSpace(response.Choices[0].Message.Content) == "" {
		c.JSON(http.StatusOK, model.TestModelResponse{Success: false, LatencyMs: latency, ErrorMessage: "model returned empty response"})
		return
	}
	c.JSON(http.StatusOK, model.TestModelResponse{
		Success:   true,
		LatencyMs: latency,
		ModelInfo: fmt.Sprintf("response length %d", len(response.Choices[0].Message.Content)),
	})
}

func (h *AppHandler) TestEmbeddingModel(c *gin.Context) {
	var req model.TestEmbeddingModelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, "invalid embedding model test request")
		return
	}
	startedAt := time.Now()
	vectors, err := h.appService.EmbedProbe(c.Request.Context(), model.EmbeddingModelConfig{
		Provider: req.Provider,
		BaseURL:  req.BaseURL,
		Model:    req.Model,
		APIKey:   req.APIKey,
	})
	latency := time.Since(startedAt).Milliseconds()
	if err != nil {
		c.JSON(http.StatusOK, model.TestModelResponse{Success: false, LatencyMs: latency, ErrorMessage: err.Error()})
		return
	}
	vectorSize := 0
	if len(vectors) > 0 {
		vectorSize = len(vectors[0])
	}
	c.JSON(http.StatusOK, model.TestModelResponse{
		Success:    true,
		LatencyMs:  latency,
		VectorSize: vectorSize,
		ModelInfo:  fmt.Sprintf("vector size %d", vectorSize),
	})
}

func (h *AppHandler) StageUpload(c *gin.Context) {
	file, err := c.FormFile("file")
	if err != nil {
		writeError(c, http.StatusBadRequest, "missing file field 'file'")
		return
	}
	if err := validateUploadFile(file, h.appService.GetConfig(), h.maxUploadBytes()); err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}
	staged, err := h.appService.StageUploadedFile(file)
	if err != nil {
		writeError(c, http.StatusBadGateway, err.Error())
		return
	}
	c.JSON(http.StatusOK, model.StageUploadResponse{
		Message:  "file staged successfully",
		UploadID: staged.ID,
		Staged:   staged,
	})
}

func (h *AppHandler) BatchIndexDocuments(c *gin.Context) {
	var req model.BatchIndexRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, "invalid batch index request")
		return
	}
	job, err := h.appService.StartBatchIndexJob(c.Param("id"), req.UploadIDs)
	if err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"job": job})
}

func (h *AppHandler) GetJobStatus(c *gin.Context) {
	job, err := h.appService.GetIndexJob(c.Param("jobId"))
	if err != nil {
		writeError(c, http.StatusNotFound, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"job": job})
}

func (h *AppHandler) CancelJob(c *gin.Context) {
	job, err := h.appService.CancelIndexJob(c.Param("jobId"))
	if err != nil {
		writeError(c, http.StatusNotFound, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"job": job})
}

func (h *AppHandler) RetryJob(c *gin.Context) {
	job, err := h.appService.RetryIndexJob(c.Param("jobId"))
	if err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"job": job})
}

func (h *AppHandler) EditMessage(c *gin.Context) {
	var req model.EditMessageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, "invalid edit message request")
		return
	}
	conversation, err := h.appService.EditMessage(c.Param("id"), c.Param("msgId"), req.Content)
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
	conversation, err := h.appService.TruncateConversationFromMessage(c.Param("id"), c.Param("msgId"))
	if err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"conversation": conversation})
}

func (h *AppHandler) ExportConversation(c *gin.Context) {
	fileName, content, err := h.appService.ExportConversationMarkdown(c.Param("id"))
	if err != nil {
		writeError(c, http.StatusNotFound, err.Error())
		return
	}
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", fileName))
	c.Data(http.StatusOK, "text/markdown; charset=utf-8", []byte(content))
}

func (h *AppHandler) DebugRetrieve(c *gin.Context) {
	var req model.RetrievalDebugRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, "invalid retrieval debug request")
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

func writeIndexError(c *gin.Context, err error) {
	var duplicate *service.DuplicateDocumentError
	if errors.As(err, &duplicate) {
		writeError(c, http.StatusConflict, err.Error())
		return
	}
	message := err.Error()
	if strings.Contains(message, "not found") || strings.Contains(message, "path is empty") {
		writeError(c, http.StatusBadRequest, message)
		return
	}
	writeError(c, http.StatusBadGateway, message)
}
