package handler

import (
	"errors"
	"net/http"
	"strconv"

	"ai-localbase/internal/service"
	"github.com/gin-gonic/gin"
)

// WikiChanges 返回实际发布的文档修订；单库路径优先，全局路径可按库筛选。
// @Summary 查看整理后的文档记录
// @Tags Wiki
// @Param id path string true "知识库 ID"
// @Param knowledgeBaseId query string false "全局入口的可选知识库筛选"
// @Param offset query int false "非负记录偏移，默认 0"
// @Param limit query int false "每页 1–100 条，默认 6"
// @Success 200 {object} map[string]interface{} "items、total、nextCursor"
// @Failure 400 {object} map[string]interface{} "分页参数无效"
// @Failure 404 {object} map[string]interface{} "知识库不存在"
// @Router /api/knowledge-bases/{id}/wiki/changes [get]
// @Router /api/wiki/changes [get]
func (h *AppHandler) WikiChanges(c *gin.Context) {
	offset, offsetErr := strconv.Atoi(c.DefaultQuery("offset", "0"))
	limit, limitErr := strconv.Atoi(c.DefaultQuery("limit", "6"))
	if offsetErr != nil || limitErr != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "分页参数必须为整数"})
		return
	}
	kbID := c.Param("id")
	if kbID == "" {
		kbID = c.Query("knowledgeBaseId")
	}
	result, err := h.appService.WikiChanges(kbID, offset, limit)
	if err != nil {
		wikiHTTPError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// wikiRequest 将路径、查询和请求体统一交给业务契约；路径范围始终优先于请求体。
func (h *AppHandler) wikiRequest(c *gin.Context, action string) {
	arguments := map[string]any{}
	if c.Request.Method == http.MethodPut || c.Request.Method == http.MethodPatch || (c.Request.Method == http.MethodPost && action != "wiki.job.retry") {
		if err := c.ShouldBindJSON(&arguments); err != nil || arguments == nil {
			c.JSON(400, gin.H{"error": "请求体必须为 JSON 对象", "code": "invalid_input"})
			return
		}
	}
	for key, values := range c.Request.URL.Query() {
		if len(values) > 0 {
			arguments[key] = values[0]
		}
	}
	for _, key := range []string{"offset", "limit", "revision"} {
		if value, ok := arguments[key].(string); ok {
			number, err := strconv.Atoi(value)
			if err != nil {
				c.JSON(400, gin.H{"error": "分页参数必须为整数", "code": "invalid_page"})
				return
			}
			arguments[key] = number
		}
	}
	arguments["knowledgeBaseId"] = c.Param("id")
	for _, key := range []string{"jobId", "pageId", "documentId", "checksum", "issueId"} {
		if value := c.Param(key); value != "" {
			arguments[key] = value
		}
	}
	result, status, err := h.appService.CallWikiTool(action, arguments)
	if err != nil {
		wikiHTTPError(c, err)
		return
	}
	c.JSON(status, result)
}

// wikiHTTPError 保留既有 error 字段并提供可稳定判断的业务 code。
func wikiHTTPError(c *gin.Context, err error) {
	var business *service.WikiError
	if errors.As(err, &business) {
		c.JSON(business.Status, gin.H{"error": business.Message, "code": business.Code})
		return
	}
	c.JSON(500, gin.H{"error": err.Error(), "code": "wiki_failed"})
}

// WikiGet 返回知识页状态与近期任务。
// @Summary 读取知识页状态
// @Param id path string true "知识库 ID"
// @Success 200 {object} map[string]interface{}
// @Failure 404 {object} map[string]interface{}
// @Router /api/knowledge-bases/{id}/wiki [get]
func (h *AppHandler) WikiGet(c *gin.Context) { h.wikiRequest(c, "wiki.get") }

// WikiConfigure 启停自动整理，启用不会自动回填全库。
// @Summary 设置知识页开关
// @Param id path string true "知识库 ID"
// @Param body body object true "enabled:boolean 必填"
// @Success 200 {object} map[string]interface{}
// @Failure 400,404 {object} map[string]interface{}
// @Router /api/knowledge-bases/{id}/wiki [put]
func (h *AppHandler) WikiConfigure(c *gin.Context) { h.wikiRequest(c, "wiki.configure") }

// WikiCreateJob 创建 ingest 或 lint 任务，相同请求返回原任务。
// @Summary 创建整理或体检任务
// @Param id path string true "知识库 ID"
// @Param body body object true "kind、requestKey 必填；ingest 需要 documentIds；lint 可传 semantic"
// @Success 200,202 {object} map[string]interface{}
// @Failure 400,404,409 {object} map[string]interface{}
// @Router /api/knowledge-bases/{id}/wiki/jobs [post]
func (h *AppHandler) WikiCreateJob(c *gin.Context) { h.wikiRequest(c, "wiki.jobs") }

// WikiGetJob 读取本库任务进度，不返回内部提取缓存。
// @Summary 读取整理任务
// @Param id path string true "知识库 ID"
// @Param jobId path string true "任务 ID"
// @Success 200 {object} map[string]interface{}
// @Failure 404 {object} map[string]interface{}
// @Router /api/knowledge-bases/{id}/wiki/jobs/{jobId} [get]
func (h *AppHandler) WikiGetJob(c *gin.Context) { h.wikiRequest(c, "wiki.job.get") }

// WikiRetryJob 显式重试失败阶段，最多三次执行。
// @Summary 重试整理任务
// @Param id path string true "知识库 ID"
// @Param jobId path string true "任务 ID"
// @Success 202 {object} map[string]interface{}
// @Failure 404,409 {object} map[string]interface{}
// @Router /api/knowledge-bases/{id}/wiki/jobs/{jobId}/retry [post]
func (h *AppHandler) WikiRetryJob(c *gin.Context) { h.wikiRequest(c, "wiki.job.retry") }

// WikiListPages 按类型、有效性和标题筛选，游标是结果集偏移。
// @Summary 浏览知识页
// @Param id path string true "知识库 ID"
// @Param pageType query string false "topic/decision/source-summary/question"
// @Param freshness query string false "fresh/stale/missing-source"
// @Param query query string false "标题或别名"
// @Param cursor query string false "下一页游标"
// @Param limit query int false "默认 20，最多 100"
// @Success 200 {object} map[string]interface{}
// @Failure 400,404 {object} map[string]interface{}
// @Router /api/knowledge-bases/{id}/wiki/pages [get]
func (h *AppHandler) WikiListPages(c *gin.Context) { h.wikiRequest(c, "wiki.page.list") }

// WikiGetPage 按 Unicode 码点分页读取历史修订；nextOffset 为零表示完成。
// @Summary 读取知识页正文与来源
// @Param id path string true "知识库 ID"
// @Param pageId path string true "页面 ID"
// @Param revision query int false "零或不传表示当前修订"
// @Param offset query int false "码点偏移"
// @Param limit query int false "默认 20000，最多 50000"
// @Success 200 {object} map[string]interface{}
// @Failure 400,404 {object} map[string]interface{}
// @Router /api/knowledge-bases/{id}/wiki/pages/{pageId} [get]
func (h *AppHandler) WikiGetPage(c *gin.Context) { h.wikiRequest(c, "wiki.page.get") }

// WikiGetSource 只返回本库仍存在的原始资料历史版本。
// @Summary 读取来源版本
// @Param id path string true "知识库 ID"
// @Param documentId path string true "原始资料 ID"
// @Param checksum path string true "正文校验和"
// @Param offset query int false "码点偏移"
// @Param limit query int false "默认 20000，最多 50000"
// @Success 200 {object} map[string]interface{}
// @Failure 400,404 {object} map[string]interface{}
// @Router /api/knowledge-bases/{id}/wiki/sources/{documentId}/revisions/{checksum} [get]
func (h *AppHandler) WikiGetSource(c *gin.Context) { h.wikiRequest(c, "wiki.source.get") }

// WikiCapture 从已保存消息读取回答和证据，无来源回答只能形成待核实页。
// @Summary 沉淀已保存回答
// @Param id path string true "知识库 ID"
// @Param body body object true "conversationId、messageId、requestKey 必填；targetPageId 可选"
// @Success 200,202 {object} map[string]interface{}
// @Failure 400,404,409 {object} map[string]interface{}
// @Router /api/knowledge-bases/{id}/wiki/capture [post]
func (h *AppHandler) WikiCapture(c *gin.Context) { h.wikiRequest(c, "wiki.capture") }

// WikiListIssues 返回结构与语义疑点及处理理由。
// @Summary 浏览内容问题
// @Param id path string true "知识库 ID"
// @Param status query string false "open/resolved/dismissed"
// @Param cursor query string false "下一页游标"
// @Param limit query int false "默认 20，最多 100"
// @Success 200 {object} map[string]interface{}
// @Failure 400,404 {object} map[string]interface{}
// @Router /api/knowledge-bases/{id}/wiki/issues [get]
func (h *AppHandler) WikiListIssues(c *gin.Context) { h.wikiRequest(c, "wiki.issue.list") }

// WikiUpdateIssue 保存处理状态和必填理由。
// @Summary 处理内容问题
// @Param id path string true "知识库 ID"
// @Param issueId path string true "问题 ID"
// @Param body body object true "status:open/resolved/dismissed；reason 必填"
// @Success 200 {object} map[string]interface{}
// @Failure 400,404 {object} map[string]interface{}
// @Router /api/knowledge-bases/{id}/wiki/issues/{issueId} [patch]
func (h *AppHandler) WikiUpdateIssue(c *gin.Context) { h.wikiRequest(c, "wiki.issue.update") }

// WikiExport 导出知识页 Markdown、目录和任务日志，不包含全部原始资料。
// @Summary 导出知识页 ZIP
// @Param id path string true "知识库 ID"
// @Produce application/zip
// @Success 200 {file} binary
// @Failure 404,500 {object} map[string]interface{}
// @Router /api/knowledge-bases/{id}/wiki/export [get]
func (h *AppHandler) WikiExport(c *gin.Context) {
	data, err := h.appService.ExportWiki(c.Param("id"))
	if err != nil {
		wikiHTTPError(c, err)
		return
	}
	c.Header("Content-Disposition", `attachment; filename="wiki.zip"`)
	c.Data(200, "application/zip", data)
}

// WikiProgress 返回全部库的整理统计，可选读取指定库的分页任务；不启动或重试任务。
// @Summary 查看知识库整理总进度
// @Produce json
// @Param knowledgeBaseId query string false "指定库任务；留空仅返回全部库汇总"
// @Param status query string false "queued/running/succeeded/failed/partial/needs_split/canceled"
// @Param offset query int false "任务偏移，默认 0"
// @Param limit query int false "每页任务数，默认 20，最多 100"
// @Success 200 {object} map[string]interface{} "items 为全量库统计，jobs 为指定库任务分页"
// @Failure 400,401,404 {object} map[string]interface{}
// @Router /api/wiki/progress [get]
func (h *AppHandler) WikiProgress(c *gin.Context) {
	offset, offsetErr := strconv.Atoi(c.DefaultQuery("offset", "0"))
	limit, limitErr := strconv.Atoi(c.DefaultQuery("limit", "20"))
	if offsetErr != nil || limitErr != nil {
		c.JSON(400, gin.H{"error": "分页参数必须为整数", "code": "invalid_page"})
		return
	}
	result, err := h.appService.WikiProgress(c.Query("knowledgeBaseId"), c.Query("status"), offset, limit)
	if err != nil {
		wikiHTTPError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// WikiRetryFailed 提交本库全部失败任务的重试，并明确返回部分提交与跳过原因。
// @Summary 批量重试本库失败任务
// @Param id path string true "知识库 ID"
// @Success 200 {object} service.WikiRetryBatchResult "提交统计，不代表任务执行成功"
// @Failure 404 {object} map[string]interface{} "知识库不存在"
// @Failure 409 {object} map[string]interface{} "知识库未启用"
// @Router /api/knowledge-bases/{id}/wiki/retry-failed [post]
func (h *AppHandler) WikiRetryFailed(c *gin.Context) {
	result, err := h.appService.RetryFailedWikiJobs(c.Param("id"))
	if err != nil {
		wikiHTTPError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}
