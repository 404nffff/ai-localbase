package handler

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	"ai-localbase/internal/model"

	"github.com/gin-gonic/gin"
)

// BatchIndexRequest 批量索引请求
type BatchIndexRequest struct {
	UploadIDs   []string `json:"uploadIds" binding:"required"`
	Concurrency int      `json:"concurrency,omitempty"` // 并发数，默认3
}

// IndexResult 单个文档的索引结果
type IndexResult struct {
	UploadID   string         `json:"uploadId"`
	DocumentID string         `json:"documentId,omitempty"`
	FileName   string         `json:"fileName"`
	Success    bool           `json:"success"`
	Error      string         `json:"error,omitempty"`
	Document   model.Document `json:"document,omitempty"`
}

// BatchIndexResponse 批量索引响应
type BatchIndexResponse struct {
	Total       int           `json:"total"`
	Successful  int           `json:"successful"`
	Failed      int           `json:"failed"`
	Results     []IndexResult `json:"results"`
	DurationMs  int64         `json:"duration_ms"`
}

// BatchIndexDocuments 批量索引文档
func (h *AppHandler) BatchIndexDocuments(c *gin.Context) {
	knowledgeBaseID := c.Param("id")
	if knowledgeBaseID == "" {
		writeError(c, http.StatusBadRequest, "knowledge base id is required")
		return
	}

	var req BatchIndexRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, fmt.Sprintf("Invalid request: %v", err))
		return
	}

	if len(req.UploadIDs) == 0 {
		writeError(c, http.StatusBadRequest, "uploadIds cannot be empty")
		return
	}

	// 设置默认并发数
	concurrency := req.Concurrency
	if concurrency <= 0 {
		concurrency = 3 // 默认3个并发
	}
	if concurrency > 10 {
		concurrency = 10 // 最大10个并发
	}

	start := time.Now()

	// 批量索引
	results := h.batchIndexFromStaged(knowledgeBaseID, req.UploadIDs, concurrency)

	// 统计结果
	successful := 0
	failed := 0
	for _, r := range results {
		if r.Success {
			successful++
		} else {
			failed++
		}
	}

	response := BatchIndexResponse{
		Total:      len(results),
		Successful: successful,
		Failed:     failed,
		Results:    results,
		DurationMs: time.Since(start).Milliseconds(),
	}

	c.JSON(http.StatusOK, response)
}

// batchIndexFromStaged 从暂存文件批量索引
func (h *AppHandler) batchIndexFromStaged(knowledgeBaseID string, uploadIDs []string, concurrency int) []IndexResult {
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	resultChan := make(chan IndexResult, len(uploadIDs))

	for _, uploadID := range uploadIDs {
		wg.Add(1)
		go func(uid string) {
			defer wg.Done()

			// 获取信号量
			sem <- struct{}{}
			defer func() { <-sem }()

			// 索引单个文档
			result := h.indexSingleStaged(knowledgeBaseID, uid)
			resultChan <- result
		}(uploadID)
	}

	// 等待所有goroutine完成
	wg.Wait()
	close(resultChan)

	// 收集结果
	results := make([]IndexResult, 0, len(uploadIDs))
	for r := range resultChan {
		results = append(results, r)
	}

	return results
}

// indexSingleStaged 索引单个暂存文件
func (h *AppHandler) indexSingleStaged(knowledgeBaseID, uploadID string) IndexResult {
	// 使用现有的 RegisterStagedUpload 方法
	document, err := h.appService.RegisterStagedUpload(uploadID, knowledgeBaseID, "")

	if err != nil {
		return IndexResult{
			UploadID: uploadID,
			FileName: "",
			Success:  false,
			Error:    err.Error(),
		}
	}

	return IndexResult{
		UploadID:   uploadID,
		DocumentID: document.ID,
		FileName:   document.Name,
		Success:    true,
		Document:   document,
	}
}

// DocumentIndexStatus 文档索引状态
type DocumentIndexStatus struct {
	DocumentID  string `json:"documentId"`
	Status      string `json:"status"` // processing, indexed, failed
	ChunkCount  int    `json:"chunkCount,omitempty"`
	IndexedAt   string `json:"indexedAt,omitempty"`
	IndexError  string `json:"indexError,omitempty"`
	ProgressPct int    `json:"progressPct,omitempty"` // 0-100
}

// GetDocumentIndexStatus 获取文档索引状态
func (h *AppHandler) GetDocumentIndexStatus(c *gin.Context) {
	knowledgeBaseID := c.Param("id")
	documentID := c.Param("documentId")

	if knowledgeBaseID == "" || documentID == "" {
		writeError(c, http.StatusBadRequest, "knowledge base id and document id are required")
		return
	}

	// 查找文档详情
	detail, err := h.appService.GetDocumentDetail(knowledgeBaseID, documentID, "")
	if err != nil {
		writeError(c, http.StatusNotFound, "document not found")
		return
	}

	// 构建状态响应
	status := DocumentIndexStatus{
		DocumentID: detail.Document.ID,
		Status:     detail.Document.Status,
		ChunkCount: detail.Document.ChunkCount,
		IndexedAt:  detail.Document.IndexedAt,
		IndexError: detail.Document.IndexError,
	}

	// 简单的进度估算
	switch detail.Document.Status {
	case "processing":
		status.ProgressPct = 50 // 处理中显示50%
	case "indexed":
		status.ProgressPct = 100
	case "failed":
		status.ProgressPct = 0
	default:
		status.ProgressPct = 0
	}

	c.JSON(http.StatusOK, status)
}
