package service

import (
	"encoding/json"
	"errors"
	"log"
	"sort"
	"strconv"
	"strings"
	"time"

	"ai-localbase/internal/model"
	"ai-localbase/internal/util"
)

// StartWikiJob 固定输入来源版本并持久排队；重复键/相同输入复用原任务。
func (s *AppService) StartWikiJob(kbID string, req model.WikiJobRequest) (model.WikiJob, bool, error) {
	if req.Kind == "" {
		req.Kind = "ingest"
	}
	if !strings.Contains("|ingest|capture|lint|", "|"+req.Kind+"|") || strings.TrimSpace(req.RequestKey) == "" {
		return model.WikiJob{}, false, &WikiError{400, "invalid_job", "kind and requestKey are required"}
	}
	ids := uniqueNonEmpty(req.DocumentIDs)
	sort.Strings(ids)
	if len(ids) > 20 || (req.Kind == "ingest" && len(ids) == 0) {
		return model.WikiJob{}, false, &WikiError{400, "invalid_sources", "choose between 1 and 20 source documents"}
	}
	kb, err := s.WikiSnapshot(kbID)
	if err != nil {
		return model.WikiJob{}, false, err
	}
	if kb.Wiki == nil || !kb.Wiki.Enabled {
		return model.WikiJob{}, false, &WikiError{409, "disabled", "wiki is disabled"}
	}
	versions := map[string]string{}
	for _, id := range ids {
		document, err := s.GetDocument(kbID, id)
		if err != nil || document.Wiki != nil {
			return model.WikiJob{}, false, &WikiError{404, "not_found", "original source not found"}
		}
		body, err := util.ExtractDocumentText(document.Path)
		if err != nil {
			return model.WikiJob{}, false, err
		}
		if expected := req.ExpectedVersions[id]; expected != "" && hashDocumentContent(body) != expected {
			return model.WikiJob{}, false, &WikiError{409, "source_changed", "answer source has changed"}
		}
		document, err = s.snapshotWikiSource(document, body)
		if err != nil {
			return model.WikiJob{}, false, err
		}
		if err := s.recordWikiSourceChange(document); err != nil {
			return model.WikiJob{}, false, err
		}
		versions[id] = document.ContentChecksum
	}
	// 输入身份不包含页面生成后的版本，重复处理同来源不会因自身发布变成新任务。
	input, err := json.Marshal([]any{kbID, req.Kind, versions, req.TargetPageID, req.CaptureText, req.Semantic, "1"})
	if err != nil {
		return model.WikiJob{}, false, err
	}
	fingerprint := hashDocumentContent(string(input))
	if req.Kind == "lint" {
		// 每次显式体检均允许重跑；同一请求键仍保持幂等。
		fingerprint = hashDocumentContent(string(input) + req.RequestKey)
	}
	var job model.WikiJob
	created := false
	err = s.commitWikiChange(kbID, func(kb *model.KnowledgeBase) error {
		if !kb.Wiki.Enabled {
			return &WikiError{409, "disabled", "wiki is disabled"}
		}
		for index, existing := range kb.Wiki.Jobs {
			if existing.RequestKey == req.RequestKey && existing.InputFingerprint != fingerprint {
				return &WikiError{409, "request_conflict", "requestKey already belongs to different input"}
			}
			if existing.RequestKey == req.RequestKey || existing.InputFingerprint == fingerprint {
				if existing.Status == "canceled" {
					existing.Status, existing.Error = "queued", ""
					existing.UpdatedAt = util.NowRFC3339()
					kb.Wiki.Jobs[index] = existing
				}
				job = existing
				if existing.Status == "succeeded" {
					for id, version := range versions {
						if kb.Wiki.PendingSources[id] == version {
							delete(kb.Wiki.PendingSources, id)
						}
					}
				}
				return nil
			}
		}
		now := util.NowRFC3339()
		base := map[string]int{}
		for _, d := range kb.Documents {
			if d.Wiki != nil {
				base[d.ID] = d.Wiki.Revision
			}
		}
		if req.TargetPageID != "" && base[req.TargetPageID] == 0 {
			return &WikiError{404, "not_found", "target wiki page not found"}
		}
		job = model.WikiJob{ID: util.NextID("wiki-job"), KnowledgeBaseID: kbID, Kind: req.Kind, RequestKey: req.RequestKey, InputFingerprint: fingerprint, DocumentIDs: ids, SourceVersions: versions, BaseRevisions: base, Status: "queued", Stage: "read_sources", Semantic: req.Semantic, CaptureText: req.CaptureText, TargetPageID: req.TargetPageID, CreatedAt: now, UpdatedAt: now, Extracts: map[string]string{}}
		kb.Wiki.Jobs = append(kb.Wiki.Jobs, job)
		created = true
		return nil
	})
	if err == nil {
		s.scheduleWikiJobs(kbID)
	}
	return job, created, err
}

// GetWikiJob 只允许从目标库读取任务，避免跨库 ID 混用。
func (s *AppService) GetWikiJob(kbID, jobID string) (model.WikiJob, error) {
	kb, err := s.WikiSnapshot(kbID)
	if err != nil {
		return model.WikiJob{}, err
	}
	if kb.Wiki != nil {
		for _, job := range kb.Wiki.Jobs {
			if job.ID == jobID {
				return job, nil
			}
		}
	}
	return model.WikiJob{}, &WikiError{404, "not_found", "wiki job not found"}
}

// RetryWikiJob 显式重试失败阶段，保留累计次数和提取缓存；来源版本变化要求创建新任务。
func (s *AppService) RetryWikiJob(kbID, jobID string) (model.WikiJob, error) {
	err := s.commitWikiChange(kbID, func(kb *model.KnowledgeBase) error {
		if !kb.Wiki.Enabled {
			return &WikiError{409, "disabled", "wiki is disabled"}
		}
		for i := range kb.Wiki.Jobs {
			job := &kb.Wiki.Jobs[i]
			if job.ID != jobID {
				continue
			}
			if job.Status != "failed" && job.Status != "needs_split" {
				return &WikiError{409, "retry_not_allowed", "only failed or paused jobs can retry"}
			}
			for id, checksum := range job.SourceVersions {
				found := false
				for _, doc := range kb.Documents {
					if doc.ID == id && doc.ContentChecksum == checksum {
						found = true
					}
				}
				if !found {
					return &WikiError{409, "source_changed", "source version changed; create a new job"}
				}
			}
			job.Status, job.Error, job.UpdatedAt = "queued", "", util.NowRFC3339()
			return nil
		}
		return &WikiError{404, "not_found", "wiki job not found"}
	})
	if err != nil {
		return model.WikiJob{}, err
	}
	s.scheduleWikiJobs(kbID)
	return s.GetWikiJob(kbID, jobID)
}

// saveWikiJob 保存阶段与覆盖进度，不能重建已经删除的库或任务。
func (s *AppService) saveWikiJob(job model.WikiJob) error {
	job.UpdatedAt = util.NowRFC3339()
	return s.commitWikiChange(job.KnowledgeBaseID, func(kb *model.KnowledgeBase) error {
		for i := range kb.Wiki.Jobs {
			if kb.Wiki.Jobs[i].ID == job.ID {
				kb.Wiki.Jobs[i] = job
				return nil
			}
		}
		return &WikiError{404, "not_found", "wiki job not found"}
	})
}

// scheduleWikiJobs 为本库启动单个排队消费者，退出与新增任务之间再次检查队列。
func (s *AppService) scheduleWikiJobs(kbID string) {
	s.wikiRunMu.Lock()
	if s.wikiClosed || s.wikiRunning[kbID] {
		s.wikiRunMu.Unlock()
		return
	}
	if s.wikiRunning == nil {
		s.wikiRunning = map[string]bool{}
	}
	s.wikiRunning[kbID] = true
	s.wikiWorkers.Add(1)
	s.wikiRunMu.Unlock()
	go func() {
		persistenceFailed := false
		defer s.wikiWorkers.Done()
		defer func() {
			s.wikiRunMu.Lock()
			delete(s.wikiRunning, kbID)
			closed := s.wikiClosed
			s.wikiRunMu.Unlock()
			if closed || persistenceFailed {
				return
			}
			kb, err := s.WikiSnapshot(kbID)
			if err == nil && kb.Wiki != nil && kb.Wiki.Enabled {
				for _, job := range kb.Wiki.Jobs {
					if job.Status == "queued" {
						s.scheduleWikiJobs(kbID)
						break
					}
				}
			}
		}()
		for {
			s.wikiRunMu.Lock()
			closed := s.wikiClosed
			s.wikiRunMu.Unlock()
			if closed {
				return
			}
			kb, err := s.WikiSnapshot(kbID)
			if err != nil || kb.Wiki == nil || !kb.Wiki.Enabled {
				return
			}
			var next *model.WikiJob
			for _, job := range kb.Wiki.Jobs {
				if job.Status == "queued" {
					copy := job
					next = &copy
					break
				}
			}
			if next == nil {
				return
			}
			if err := s.runWikiJob(*next); err != nil {
				log.Printf("wiki job %s persistence error: %v", next.ID, err)
				// 无法持久化时不忙循环；该任务仍保留，下一次显式操作可重新调度。
				persistenceFailed = true
				return
			}
		}
	}()
}

// runWikiJob 固定本次模型配置，发布前失败不写页面，发布后重试只补索引。
func (s *AppService) runWikiJob(job model.WikiJob) error {
	started := time.Now()
	job.Status, job.Error = "running", ""
	job.Attempt++
	// 每轮显式执行获得独立预算，累计调用数不清零；整理阶段按实际来源规模扩充。
	job.ModelCallLimit = job.ModelCalls + 32
	job.ModelTimeoutSeconds = s.GetConfig().Wiki.ModelTimeoutSeconds
	if err := s.saveWikiJob(job); err != nil {
		return err
	}
	var runErr error
	if job.Kind == "lint" {
		runErr = s.runWikiLint(&job, s.currentChatConfig())
	} else if job.Stage != "index" {
		job.Stage = "read_sources"
		config := s.currentChatConfig()
		var plan model.WikiPlan
		plan, runErr = s.compileWiki(&job, config)
		if runErr == nil {
			job.Stage = "publish"
			if err := s.saveWikiJob(job); err != nil {
				return err
			}
			job.ChangedPageIDs, runErr = s.publishWikiPlan(job.KnowledgeBaseID, job.ID, plan)
			if runErr == nil {
				job.Stage = "index"
			}
		}
	}
	if runErr == nil {
		for _, id := range job.ChangedPageIDs {
			if _, err := s.indexWikiPage(job.KnowledgeBaseID, id); err != nil {
				runErr = err
				break
			}
		}
	}
	if runErr != nil {
		job.Status, job.Error = "failed", runErr.Error()
		if e, ok := runErr.(*WikiError); ok && e.Code == "needs_split" {
			job.Status = "needs_split"
		}
	} else if job.Status != "partial" {
		job.Status, job.Stage = "succeeded", "finalize"
	}
	if err := s.saveWikiJob(job); err != nil {
		return err
	}
	if job.Status == "succeeded" {
		if err := s.commitWikiChange(job.KnowledgeBaseID, func(kb *model.KnowledgeBase) error {
			for id, version := range job.SourceVersions {
				if kb.Wiki.PendingSources[id] == version {
					delete(kb.Wiki.PendingSources, id)
				}
			}
			return nil
		}); err != nil {
			return err
		}
	}
	s.RecordOperationLog(model.OperationLogEntry{Operation: "wiki_" + job.Kind, Source: "wiki", Status: job.Status, KnowledgeBaseID: job.KnowledgeBaseID, CorrelationID: job.ID, DurationMs: time.Since(started).Milliseconds(), CreatedAt: util.NowRFC3339(), Metadata: map[string]any{"stage": job.Stage, "changedPageIds": job.ChangedPageIDs, "modelCalls": job.ModelCalls, "error": job.Error}})
	return nil
}

// enqueueWikiSource 在原始资料成功写入后创建增量任务；未配置第三方模型时跳过自动整理。
func (s *AppService) enqueueWikiSource(document model.Document) {
	if document.Wiki != nil || len(document.SourceRevisions) == 0 || !wikiAutoCompileReady(s.GetConfig().Chat) {
		return
	}
	_, _, err := s.StartWikiJob(document.KnowledgeBaseID, model.WikiJobRequest{Kind: "ingest", DocumentIDs: []string{document.ID}, RequestKey: "source:" + document.ID + ":" + document.ContentChecksum})
	if err != nil {
		log.Printf("wiki source %s waiting for compilation: %v", document.ID, err)
	}
}

// recoverWikiJobs 保留中断失败证据，恢复未执行队列，不自动重试已执行失败任务。
func (s *AppService) recoverWikiJobs() {
	for _, kb := range s.ListKnowledgeBases() {
		if kb.Wiki == nil {
			continue
		}
		err := s.commitWikiChange(kb.ID, func(current *model.KnowledgeBase) error {
			for i := range current.Wiki.Jobs {
				if current.Wiki.Jobs[i].Status == "running" {
					current.Wiki.Jobs[i].Status, current.Wiki.Jobs[i].Error = "failed", "interrupted"
				}
			}
			return nil
		})
		if err != nil {
			log.Printf("wiki recovery %s: %v", kb.ID, err)
			continue
		}
		if kb.Wiki.Enabled {
			s.scheduleWikiJobs(kb.ID)
			// 已持久排队的同版本资料由消费者直接恢复，避免启动时逐份重复读取和保存全站状态。
			// 快照中的 running 已在上方转成 interrupted；它和已有失败都保留显式重试，不重复入队。
			states := wikiSourceStates(kb)
			for _, document := range kb.Documents {
				if states[document.ID] == "queued" || states[document.ID] == "running" || states[document.ID] == "failed" {
					continue
				}
				if kb.Wiki.PendingSources[document.ID] != "" {
					s.enqueueWikiSource(document)
				}
			}
		}
	}
}

// CallWikiTool 是 HTTP 与 MCP 的共同业务契约，所有操作显式限定单库。
func (s *AppService) CallWikiTool(action string, arguments map[string]any) (map[string]any, int, error) {
	var input struct {
		KnowledgeBaseID string   `json:"knowledgeBaseId"`
		Enabled         *bool    `json:"enabled"`
		Kind            string   `json:"kind"`
		DocumentIDs     []string `json:"documentIds"`
		RequestKey      string   `json:"requestKey"`
		Semantic        bool     `json:"semantic"`
		JobID           string   `json:"jobId"`
		PageID          string   `json:"pageId"`
		DocumentID      string   `json:"documentId"`
		Checksum        string   `json:"checksum"`
		PageType        string   `json:"pageType"`
		Freshness       string   `json:"freshness"`
		Query           string   `json:"query"`
		Cursor          string   `json:"cursor"`
		Limit           int      `json:"limit"`
		Offset          int      `json:"offset"`
		Revision        int      `json:"revision"`
		Status          string   `json:"status"`
		IssueID         string   `json:"issueId"`
		Reason          string   `json:"reason"`
		ConversationID  string   `json:"conversationId"`
		MessageID       string   `json:"messageId"`
		TargetPageID    string   `json:"targetPageId"`
	}
	body, err := json.Marshal(arguments)
	if err == nil {
		err = json.Unmarshal(body, &input)
	}
	if err != nil || strings.TrimSpace(input.KnowledgeBaseID) == "" {
		return nil, 400, &WikiError{400, "invalid_input", "knowledgeBaseId and correctly typed arguments are required"}
	}
	offset := 0
	if input.Cursor != "" {
		offset, err = strconv.Atoi(input.Cursor)
		if err != nil || offset < 0 {
			return nil, 400, &WikiError{400, "invalid_page", "invalid cursor"}
		}
	}
	kb := input.KnowledgeBaseID
	var result map[string]any
	status := 200
	switch action {
	case "wiki.get":
		result, err = s.WikiOverview(kb)
	case "wiki.configure":
		if input.Enabled == nil {
			return nil, 400, &WikiError{400, "invalid_input", "enabled is required"}
		}
		_, err = s.ConfigureWiki(kb, *input.Enabled)
		if err == nil {
			result, err = s.WikiOverview(kb)
		}
	case "wiki.ingest", "wiki.lint", "wiki.jobs":
		kind := input.Kind
		if action == "wiki.ingest" {
			kind = "ingest"
		}
		if action == "wiki.lint" {
			kind = "lint"
		}
		if kind != "ingest" && kind != "lint" {
			return nil, 400, &WikiError{400, "invalid_job", "kind must be ingest or lint"}
		}
		var job model.WikiJob
		var created bool
		job, created, err = s.StartWikiJob(kb, model.WikiJobRequest{Kind: kind, DocumentIDs: input.DocumentIDs, RequestKey: input.RequestKey, Semantic: input.Semantic})
		result = map[string]any{"job": job, "created": created}
		if created {
			status = 202
		}
	case "wiki.job.get", "wiki.job.retry":
		var job model.WikiJob
		if action == "wiki.job.get" {
			job, err = s.GetWikiJob(kb, input.JobID)
		} else {
			job, err = s.RetryWikiJob(kb, input.JobID)
			status = 202
		}
		job.Extracts = nil
		job.CaptureText = ""
		result = map[string]any{"job": job}
	case "wiki.page.list":
		result, err = s.WikiPages(kb, input.PageType, input.Freshness, input.Query, offset, input.Limit)
	case "wiki.page.get":
		result, err = s.WikiPageContent(kb, input.PageID, input.Revision, input.Offset, input.Limit)
	case "wiki.source.get":
		result, err = s.WikiSourceContent(kb, input.DocumentID, input.Checksum, input.Offset, input.Limit)
	case "wiki.capture":
		var job model.WikiJob
		var created bool
		job, created, err = s.CaptureWikiAnswer(kb, input.ConversationID, input.MessageID, input.TargetPageID, input.RequestKey)
		job.CaptureText = ""
		job.Extracts = nil
		result = map[string]any{"job": job, "created": created}
		if created {
			status = 202
		}
	case "wiki.issue.list":
		result, err = s.WikiIssues(kb, input.Status, offset, input.Limit)
	case "wiki.issue.update":
		var issue model.WikiIssue
		issue, err = s.UpdateWikiIssue(kb, input.IssueID, input.Status, input.Reason)
		result = map[string]any{"issue": issue}
	default:
		return nil, 404, &WikiError{404, "not_found", "unknown wiki operation"}
	}
	return result, status, err
}

// wikiMCPTools 描述与 HTTP 同源的参数和返回结构，现有工具保持名称与语义。
func wikiMCPTools() []model.MCPTool {
	definitions := []struct {
		name, description  string
		required, optional []string
	}{
		{"wiki.get", "读取知识页开关、数量和近期整理任务", nil, nil},
		{"wiki.configure", "启停本库自动整理，不自动回填历史资料", []string{"enabled"}, nil},
		{"wiki.ingest", "整理指定原始资料；复用已配置聊天模型", []string{"documentIds", "requestKey"}, nil},
		{"wiki.lint", "检查结构及可选语义疑点，返回真实覆盖范围", []string{"requestKey"}, []string{"semantic"}},
		{"wiki.job.get", "读取持久任务阶段与失败原因", []string{"jobId"}, nil},
		{"wiki.job.retry", "显式重试失败或暂停任务，保留累计执行记录", []string{"jobId"}, nil},
		{"wiki.page.list", "分页浏览知识页", nil, []string{"pageType", "freshness", "query", "cursor", "limit"}},
		{"wiki.page.get", "按修订读取完整知识页的 Unicode 分页片段", []string{"pageId"}, []string{"revision", "offset", "limit"}},
		{"wiki.source.get", "读取仍存在的原始资料历史版本", []string{"documentId", "checksum"}, []string{"offset", "limit"}},
		{"wiki.capture", "从服务端保存回答及证据沉淀知识页", []string{"conversationId", "messageId", "requestKey"}, []string{"targetPageId"}},
		{"wiki.issue.list", "分页查看内容问题", nil, []string{"status", "cursor", "limit"}},
		{"wiki.issue.update", "保存问题处理状态及理由", []string{"issueId", "status", "reason"}, nil},
	}
	result := []model.MCPTool{}
	for _, definition := range definitions {
		parameters := []model.MCPToolParameter{{Name: "knowledgeBaseId", Type: "string", Required: true, Description: "当前知识库 ID，禁止跨库写入"}}
		for _, required := range []bool{true, false} {
			names := definition.optional
			if required {
				names = definition.required
			}
			for _, name := range names {
				kind := "string"
				switch name {
				case "enabled", "semantic":
					kind = "boolean"
				case "documentIds":
					kind = "array<string>"
				case "limit", "offset", "revision":
					kind = "integer"
				}
				parameters = append(parameters, model.MCPToolParameter{Name: name, Type: kind, Required: required, Description: name})
			}
		}
		response := []model.MCPToolResponse{{Name: "job", Type: "object", Description: "任务（写入或查询任务时）"}, {Name: "items", Type: "array<object>", Description: "分页结果（列表时）"}, {Name: "nextCursor", Type: "string", Description: "下一页游标"}, {Name: "markdown", Type: "string", Description: "页面正文片段"}, {Name: "content", Type: "string", Description: "来源正文片段"}, {Name: "nextOffset", Type: "integer", Description: "后续正文位置，0 表示完成"}}
		tool := newMCPTool(definition.name, definition.description, parameters, response)
		// MCP 的 JSON Schema 数组使用标准 type/items，而非展示用类型缩写。
		properties := tool.InputSchema["properties"].(map[string]any)
		if _, exists := properties["documentIds"]; exists {
			properties["documentIds"] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "minItems": 1, "maxItems": 20}
		}
		result = append(result, tool)
	}
	return result
}

// WikiRetryFailure 记录未能提交的任务及原因，不将提交失败混同为模型执行失败。
type WikiRetryFailure struct {
	JobID string `json:"jobId"`
	Error string `json:"error"`
}

// WikiRetryBatchResult 汇总本库失败任务快照的提交结果，Submitted 不代表整理成功。
type WikiRetryBatchResult struct {
	Total     int                `json:"total"` // 点击时本库全部失败任务数，不受分页影响。
	Submitted int                `json:"submitted"`
	Skipped   int                `json:"skipped"`   // 保留批量结果字段；手动重试不因累计次数跳过。
	Remaining int                `json:"remaining"` // 因执行故障停止后尚未尝试提交的数量。
	Failures  []WikiRetryFailure `json:"failures"`
}

// RetryFailedWikiJobs 对失败及暂停任务快照逐项提交一次，复用单条重试的来源和状态校验。
// 部分提交不会回滚；业务冲突记录后继续，持久化等系统错误立即停止并返回已发生的结果。
func (s *AppService) RetryFailedWikiJobs(kbID string) (WikiRetryBatchResult, error) {
	result := WikiRetryBatchResult{Failures: []WikiRetryFailure{}}
	kb, err := s.WikiSnapshot(kbID)
	if err != nil {
		return result, err
	}
	if kb.Wiki == nil || !kb.Wiki.Enabled {
		return result, &WikiError{409, "disabled", "本库自动整理已关闭"}
	}
	for _, job := range kb.Wiki.Jobs {
		if job.Status == "failed" || job.Status == "needs_split" {
			result.Total++
		}
	}
	result.Remaining = result.Total
	for _, job := range kb.Wiki.Jobs {
		if job.Status != "failed" && job.Status != "needs_split" {
			continue
		}
		result.Remaining--
		_, err := s.RetryWikiJob(kbID, job.ID)
		if err == nil {
			result.Submitted++
			continue
		}
		result.Failures = append(result.Failures, WikiRetryFailure{JobID: job.ID, Error: err.Error()})
		var business *WikiError
		if !errors.As(err, &business) || business.Status >= 500 {
			break
		}
	}
	return result, nil
}
