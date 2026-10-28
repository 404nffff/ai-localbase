package service

import (
	"sort"
	"strconv"
	"time"
)

// WikiChange 仅展示实际发布的文档修订，不把成功但没有产出的任务伪装成整理成果。
type WikiChange struct {
	KnowledgeBaseID   string `json:"knowledgeBaseId"`
	KnowledgeBaseName string `json:"knowledgeBaseName"`
	PageID            string `json:"pageId"`
	Title             string `json:"title"`
	Revision          int    `json:"revision"`
	CreatedAt         string `json:"createdAt"`
	SourceCount       int    `json:"sourceCount"`
}

// WikiChanges 按时间倒序分页读取已发布历史；空库ID汇总所有库，非空时严格限定该库。
// 只在读锁内提取展示字段，不复制整库正文和任务，也不触发整理。
func (s *AppService) WikiChanges(kbID string, offset, limit int) (map[string]any, error) {
	if offset < 0 || limit < 1 || limit > 100 {
		return nil, &WikiError{400, "invalid_page", "offset 必须非负，limit 必须为 1–100"}
	}
	s.state.Mu.RLock()
	defer s.state.Mu.RUnlock()
	if kbID != "" {
		if _, ok := s.state.KnowledgeBases[kbID]; !ok {
			return nil, &WikiError{404, "not_found", "知识库不存在"}
		}
	}
	items := []WikiChange{}
	for _, kb := range s.state.KnowledgeBases {
		if kbID != "" && kb.ID != kbID {
			continue
		}
		for _, doc := range kb.Documents {
			if doc.Wiki == nil {
				continue
			}
			for _, revision := range doc.Wiki.History {
				sources := map[string]bool{}
				for _, ref := range revision.SourceRefs {
					sources[ref.DocumentID] = true
				}
				items = append(items, WikiChange{KnowledgeBaseID: kb.ID, KnowledgeBaseName: kb.Name, PageID: doc.ID, Title: revision.Title, Revision: revision.Revision, CreatedAt: revision.CreatedAt, SourceCount: len(sources)})
			}
		}
	}
	sort.Slice(items, func(i, j int) bool {
		left, _ := time.Parse(time.RFC3339Nano, items[i].CreatedAt)
		right, _ := time.Parse(time.RFC3339Nano, items[j].CreatedAt)
		if !left.Equal(right) {
			return left.After(right)
		}
		if items[i].KnowledgeBaseID != items[j].KnowledgeBaseID {
			return items[i].KnowledgeBaseID < items[j].KnowledgeBaseID
		}
		if items[i].PageID != items[j].PageID {
			return items[i].PageID < items[j].PageID
		}
		return items[i].Revision > items[j].Revision
	})
	// 页面删除后旧游标可能超出末尾，返回空页并保留总数，供前端回到第一页。
	start := min(offset, len(items))
	end := min(start+limit, len(items))
	next := ""
	if end < len(items) {
		next = strconv.Itoa(end)
	}
	return map[string]any{"items": items[start:end], "total": len(items), "nextCursor": next}, nil
}

// WikiLibraryProgress 是单库全部任务的汇总；计数不受任务列表分页影响。
type WikiLibraryProgress struct {
	KnowledgeBaseID string         `json:"knowledgeBaseId"`
	Name            string         `json:"name"`
	Enabled         bool           `json:"enabled"`
	PendingCount    int            `json:"pendingCount"` // 待整理来源版本，不等同于排队任务数。
	PageCount       int            `json:"pageCount"`
	Total           int            `json:"total"`
	Counts          map[string]int `json:"counts"` // 按真实任务状态计数，部分完成不计为成功。
	UpdatedAt       string         `json:"updatedAt"`
}

// WikiJobProgress 仅返回任务展示字段，不携带提取缓存、来源正文和模型配置。
type WikiJobProgress struct {
	ID                string `json:"id"`
	Kind              string `json:"kind"`
	Status            string `json:"status"`
	Stage             string `json:"stage"`
	Attempt           int    `json:"attempt"`
	Error             string `json:"error,omitempty"`
	TotalSegments     int    `json:"totalSegments"`
	ProcessedSegments int    `json:"processedSegments"`
	TotalPages        int    `json:"totalPages"`
	CheckedPages      int    `json:"checkedPages"`
	SemanticStatus    string `json:"semanticStatus,omitempty"`
	CreatedAt         string `json:"createdAt"`
	UpdatedAt         string `json:"updatedAt"`
}

// WikiProgress 汇总全部知识库，并可按库与状态分页读取任务。
// 空 kbID 只返回汇总；分页只影响 jobs，未知库或非法筛选明确返回业务错误。
// 在读锁内构建独立展示数据，不复制全文，也不触发整理或修改持久状态。
func (s *AppService) WikiProgress(kbID, status string, offset, limit int) (map[string]any, error) {
	if offset < 0 || limit < 1 || limit > 100 {
		return nil, &WikiError{400, "invalid_page", "offset 必须非负，limit 必须为 1–100"}
	}
	switch status {
	case "", "queued", "running", "succeeded", "failed", "partial", "needs_split", "canceled":
	default:
		return nil, &WikiError{400, "invalid_status", "不支持的任务状态"}
	}

	s.state.Mu.RLock()
	defer s.state.Mu.RUnlock()
	if kbID != "" {
		if _, ok := s.state.KnowledgeBases[kbID]; !ok {
			return nil, &WikiError{404, "not_found", "知识库不存在"}
		}
	}

	libraries := make([]WikiLibraryProgress, 0, len(s.state.KnowledgeBases))
	jobs := make([]WikiJobProgress, 0)
	for _, kb := range s.state.KnowledgeBases {
		library := WikiLibraryProgress{KnowledgeBaseID: kb.ID, Name: kb.Name, Enabled: true, Counts: map[string]int{}}
		for _, doc := range kb.Documents {
			if doc.Wiki != nil {
				library.PageCount++
			}
		}
		if kb.Wiki != nil {
			library.Enabled = kb.Wiki.Enabled
			library.PendingCount = len(kb.Wiki.PendingSources)
			library.Total = len(kb.Wiki.Jobs)
			var latest time.Time
			for _, job := range kb.Wiki.Jobs {
				library.Counts[job.Status]++
				updated, _ := time.Parse(time.RFC3339Nano, job.UpdatedAt)
				if updated.After(latest) {
					latest, library.UpdatedAt = updated, job.UpdatedAt
				}
				if kb.ID != kbID || (status != "" && job.Status != status) {
					continue
				}
				jobs = append(jobs, WikiJobProgress{
					ID: job.ID, Kind: job.Kind, Status: job.Status, Stage: job.Stage,
					Attempt: job.Attempt, Error: job.Error,
					TotalSegments: job.TotalSegments, ProcessedSegments: job.ProcessedSegments,
					TotalPages: job.TotalPages, CheckedPages: job.CheckedPages,
					SemanticStatus: job.SemanticStatus, CreatedAt: job.CreatedAt, UpdatedAt: job.UpdatedAt,
				})
			}
		}
		libraries = append(libraries, library)
	}

	// 固定库顺序；任务按创建时间倒序，同时间使用 ID 保证翻页顺序稳定。
	sort.Slice(libraries, func(i, j int) bool { return libraries[i].KnowledgeBaseID < libraries[j].KnowledgeBaseID })
	sort.Slice(jobs, func(i, j int) bool {
		left, _ := time.Parse(time.RFC3339Nano, jobs[i].CreatedAt)
		right, _ := time.Parse(time.RFC3339Nano, jobs[j].CreatedAt)
		if left.Equal(right) {
			return jobs[i].ID > jobs[j].ID
		}
		return left.After(right)
	})
	total := len(jobs)
	start := min(offset, total)
	end := start + min(limit, total-start)
	nextCursor := ""
	if end < total {
		nextCursor = strconv.Itoa(end)
	}
	return map[string]any{
		"items":            libraries,
		"autoCompileReady": wikiAutoCompileReady(s.state.Config.Chat),
		"jobs":             map[string]any{"items": jobs[start:end], "total": total, "nextCursor": nextCursor},
	}, nil
}
