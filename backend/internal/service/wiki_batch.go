package service

import (
	"fmt"

	"ai-localbase/internal/model"
)

// wikiSourceStates 按当前资料版本计算整理状态，包含尚未创建任务的历史资料。
// 成功版本优先，失败任务不覆盖同版本已经完成的整理成果。
func wikiSourceStates(kb model.KnowledgeBase) map[string]string {
	states := map[string]string{}
	for _, doc := range kb.Documents {
		if doc.Wiki != nil {
			continue
		}
		status := "pending"
		if kb.Wiki != nil {
			for _, job := range kb.Wiki.Jobs {
				if job.Kind != "ingest" || job.SourceVersions[doc.ID] == "" || job.SourceVersions[doc.ID] != doc.ContentChecksum {
					continue
				}
				if job.Status == "succeeded" {
					status = "succeeded"
					break
				}
				if job.Status == "running" || job.Status == "queued" {
					status = job.Status
				}
				if status == "pending" && (job.Status == "failed" || job.Status == "needs_split") {
					status = "failed"
				}
			}
		}
		states[doc.ID] = status
	}
	return states
}

// WikiBatchItem 返回每份资料的真实提交结果；复用历史任务不伪装成新提交。
type WikiBatchItem struct {
	DocumentID string `json:"documentId"`      // 本次选择的原始资料 ID。
	Name       string `json:"name"`            // 提交时的资料名称。
	JobID      string `json:"jobId,omitempty"` // 创建或复用的任务，跳过或失败时可为空。
	Status     string `json:"status"`          // 实际任务状态；排队不代表整理成功。
	Error      string `json:"error,omitempty"` // 单份提交失败原因，不影响其余资料提交。
}

// StartWikiBatch 将所选资料逐份排队；空列表只处理当前待整理资料。
// 保留现有幂等和累计执行记录，批量重试复用原任务及提取缓存。
func (s *AppService) StartWikiBatch(kbID string, ids []string) ([]WikiBatchItem, error) {
	kb, err := s.WikiSnapshot(kbID)
	if err != nil {
		return nil, err
	}
	if kb.Wiki != nil && !kb.Wiki.Enabled {
		return nil, &WikiError{409, "disabled", "请先开启本库自动整理"}
	}
	if !wikiAutoCompileReady(s.GetConfig().Chat) {
		return nil, &WikiError{409, "model_required", "请先配置第三方聊天模型"}
	}
	states := wikiSourceStates(kb)
	documents := map[string]model.Document{}
	for _, doc := range kb.Documents {
		if doc.Wiki == nil {
			documents[doc.ID] = doc
		}
	}
	if len(ids) == 0 {
		for _, doc := range kb.Documents {
			if states[doc.ID] == "pending" || states[doc.ID] == "failed" {
				ids = append(ids, doc.ID)
			}
		}
	}
	ids = uniqueNonEmpty(ids)
	// 先校验完整范围，避免非法 ID 在已排队部分之后才被发现。
	for _, id := range ids {
		if _, ok := documents[id]; !ok {
			return nil, &WikiError{404, "not_found", "所选原始资料不存在"}
		}
	}
	items := make([]WikiBatchItem, 0, len(ids))
	for _, id := range ids {
		doc := documents[id]
		item := WikiBatchItem{DocumentID: id, Name: doc.Name, Status: states[id]}
		if item.Status == "succeeded" || item.Status == "queued" || item.Status == "running" {
			items = append(items, item)
			continue
		}
		// 复用该资料版本最近的失败任务（包括旧多资料任务），避免重复建任务并丢失提取进度。
		var previous *model.WikiJob
		if kb.Wiki != nil {
			for i := len(kb.Wiki.Jobs) - 1; i >= 0; i-- {
				job := &kb.Wiki.Jobs[i]
				// 未记录当前资料版本的任务不属于该资料；空校验值不能被当作相同版本。
				version, belongsToJob := job.SourceVersions[id]
				if job.Kind == "ingest" && belongsToJob && version != "" && version == doc.ContentChecksum && (job.Status == "failed" || job.Status == "needs_split") {
					previous = job
					break
				}
			}
		}
		var job model.WikiJob
		if previous != nil {
			job, err = s.GetWikiJob(kbID, previous.ID)
			if err == nil && job.Status != "queued" && job.Status != "running" && job.Status != "succeeded" {
				job, err = s.RetryWikiJob(kbID, previous.ID)
			}
		} else {
			job, _, err = s.StartWikiJob(kbID, model.WikiJobRequest{Kind: "ingest", DocumentIDs: []string{id}, RequestKey: fmt.Sprintf("batch:%s:%s", id, doc.ContentChecksum)})
		}
		if err != nil {
			item.Status, item.Error = "failed", err.Error()
		} else {
			item.JobID, item.Status = job.ID, job.Status
		}
		items = append(items, item)
	}
	return items, nil
}
