package service

import (
	"fmt"
	"log"
	"os"
	"strings"
	"sync"

	"ai-localbase/internal/model"
	"ai-localbase/internal/util"
)

const (
	indexJobQueued    = "queued"
	indexJobRunning   = "running"
	indexJobSucceeded = "succeeded"
	indexJobFailed    = "failed"
	indexJobCanceled  = "canceled"
	indexJobPartial   = "partial-failed"
)

// StartBatchIndexJob 为每个上传预分配稳定文档 ID，先保存任务及上传归属，再启动后台索引。
// 接受失败返回错误；已保存的取消记录保留供重启核对，不将部分落盘冒充整批接受。
func (s *AppService) StartBatchIndexJob(knowledgeBaseID string, uploadIDs []string) (model.IndexJob, error) {
	if s == nil {
		return model.IndexJob{}, fmt.Errorf("app service is nil")
	}
	if s.staging == nil {
		return model.IndexJob{}, fmt.Errorf("upload staging is not configured")
	}
	kbID, err := s.ResolveKnowledgeBaseID(knowledgeBaseID)
	if err != nil {
		return model.IndexJob{}, err
	}
	ids := uniqueNonEmpty(uploadIDs)
	if len(ids) == 0 {
		return model.IndexJob{}, fmt.Errorf("uploadIds cannot be empty")
	}

	now := util.NowRFC3339()
	job := &model.IndexJob{
		ID:              util.NextID("job"),
		KnowledgeBaseID: kbID,
		Status:          indexJobQueued,
		Total:           len(ids),
		Items:           make([]model.IndexJobItem, 0, len(ids)),
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	s.jobsMu.Lock()
	if s.jobsClosed {
		s.jobsMu.Unlock()
		return model.IndexJob{}, fmt.Errorf("index service is closing")
	}
	if s.jobs == nil {
		s.jobs = map[string]*indexJobState{}
	}
	state := &indexJobState{job: job, uploadIDs: ids, documents: map[string]model.Document{}}
	for _, id := range ids {
		name := s.staging.FileName(id)
		docID := util.NextID("doc")
		state.documents[id] = model.Document{ID: docID, KnowledgeBaseID: kbID, Name: name, UploadedAt: now, Status: "processing", Path: util.BuildKnowledgeBaseUploadPath(s.serverConfig.UploadDir, kbID, docID+"_"+util.SanitizeFilename(name))}
	}
	if err := s.saveIndexJobLocked(state); err != nil {
		s.jobsMu.Unlock()
		return model.IndexJob{}, err
	}
	if err := s.staging.claimUploads(ids, job.ID); err != nil {
		state.canceled = true
		state.job.Status = indexJobCanceled
		if saveErr := s.saveIndexJobLocked(state); saveErr != nil {
			err = fmt.Errorf("%w; save cancellation: %v", err, saveErr)
		}
		s.jobsMu.Unlock()
		return model.IndexJob{}, err
	}
	s.jobs[job.ID] = state
	s.jobsMu.Unlock()

	state.mu.Lock()
	snapshot := cloneIndexJob(job)
	state.mu.Unlock()
	if err := s.launchIndexJob(state); err != nil {
		return model.IndexJob{}, err
	}
	return snapshot, nil
}

func (s *AppService) GetIndexJob(jobID string) (model.IndexJob, error) {
	state, err := s.lookupIndexJob(jobID)
	if err != nil {
		return model.IndexJob{}, err
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	return cloneIndexJob(state.job), nil
}

// CancelIndexJob 与最终文档发布共用任务锁，先发生的取消阻止后续发布。
// 持久化失败返回错误，进程内仍保持取消；调用方不能将此错误视为重启后也已取消。
func (s *AppService) CancelIndexJob(jobID string) (model.IndexJob, error) {
	state, err := s.lookupIndexJob(jobID)
	if err != nil {
		return model.IndexJob{}, err
	}
	state.mu.Lock()
	if state.job.Status != indexJobQueued && state.job.Status != indexJobRunning {
		result := cloneIndexJob(state.job)
		state.mu.Unlock()
		return result, nil
	}
	state.canceled = true
	if state.job.Status == indexJobQueued || state.job.Status == indexJobRunning {
		state.job.Status = indexJobCanceled
		state.job.UpdatedAt = util.NowRFC3339()
	}
	err = s.saveIndexJobLocked(state)
	cloned := cloneIndexJob(state.job)
	state.mu.Unlock()
	return cloned, err
}

// RetryIndexJob 仅重试已停止的失败或取消任务，保留成功条目，禁止重跑成功任务。
// 排队检查点保存失败时恢复原内存状态，保存成功才启动工作协程。
func (s *AppService) RetryIndexJob(jobID string) (model.IndexJob, error) {
	state, err := s.lookupIndexJob(jobID)
	if err != nil {
		return model.IndexJob{}, err
	}
	state.mu.Lock()
	if state.running || state.job.Status == indexJobRunning || state.job.Status == indexJobQueued {
		state.mu.Unlock()
		return model.IndexJob{}, fmt.Errorf("job is still active")
	}
	if state.job.Status == indexJobSucceeded {
		state.mu.Unlock()
		return model.IndexJob{}, fmt.Errorf("job already succeeded")
	}
	// 明确重试仅保留成功检查点；取消任务不会在启动恢复时自行复活。
	previousJob, previousCanceled := cloneIndexJob(state.job), state.canceled
	items := make([]model.IndexJobItem, 0, len(state.job.Items))
	for _, item := range state.job.Items {
		if item.Success {
			items = append(items, item)
		}
	}
	state.job.Items = items
	state.job.Completed, state.job.Failed = len(items), 0
	state.job.Status, state.canceled = indexJobQueued, false
	state.job.UpdatedAt = util.NowRFC3339()
	if err := s.saveIndexJobLocked(state); err != nil {
		*state.job, state.canceled = previousJob, previousCanceled
		state.mu.Unlock()
		return model.IndexJob{}, err
	}
	result := cloneIndexJob(state.job)
	state.mu.Unlock()
	if err := s.launchIndexJob(state); err != nil {
		return model.IndexJob{}, err
	}
	return result, nil
}

// runIndexJob 串行处理未确认上传，每条结果落盘后才释放暂存副本。
// 检查点错误立即停止；重启通过稳定文档 ID 核对已发布内容，避免重复导入。
func (s *AppService) runIndexJob(state *indexJobState) {
	if s == nil || state == nil {
		return
	}
	state.mu.Lock()
	if state.canceled {
		state.mu.Unlock()
		return
	}
	state.job.Status = indexJobRunning
	if !s.indexJobCheckpoint(state) {
		state.mu.Unlock()
		return
	}
	state.job.UpdatedAt = util.NowRFC3339()
	uploadIDs := append([]string{}, state.uploadIDs...)
	kbID := state.job.KnowledgeBaseID
	state.mu.Unlock()

	for _, uploadID := range uploadIDs {
		state.mu.Lock()
		completed := false
		for _, item := range state.job.Items {
			if item.UploadID == uploadID {
				completed = true
				break
			}
		}
		if completed {
			state.mu.Unlock()
			continue
		}
		if state.canceled {
			state.job.Status = indexJobCanceled
			state.job.UpdatedAt = util.NowRFC3339()
			s.indexJobCheckpoint(state)
			state.mu.Unlock()
			return
		}
		fileName := ""
		if s.staging != nil {
			fileName = s.staging.FileName(uploadID)
		}
		state.job.CurrentFileName = fileName
		state.job.UpdatedAt = util.NowRFC3339()
		state.mu.Unlock()

		item := s.indexStagedJobUpload(state, kbID, uploadID)
		state.mu.Lock()
		state.job.Items = append(state.job.Items, item)
		state.job.Completed++
		if !item.Success {
			state.job.Failed++
		}
		state.job.UpdatedAt = util.NowRFC3339()
		if !s.indexJobCheckpoint(state) {
			state.mu.Unlock()
			return
		}
		state.mu.Unlock()
		if item.Success {
			if err := s.staging.releaseCompletedUpload(uploadID, state.job.ID); err != nil {
				log.Printf("completed upload cleanup pending: %v", err)
			}
		}
	}

	state.mu.Lock()
	defer state.mu.Unlock()
	state.job.CurrentFileName = ""
	switch {
	case state.canceled:
		state.job.Status = indexJobCanceled
	case state.job.Failed == 0:
		state.job.Status = indexJobSucceeded
	case state.job.Failed == state.job.Total:
		state.job.Status = indexJobFailed
	default:
		state.job.Status = indexJobPartial
	}
	state.job.UpdatedAt = util.NowRFC3339()
	s.indexJobCheckpoint(state)
}

func (s *AppService) indexStagedUpload(knowledgeBaseID, uploadID string) model.IndexJobItem {
	item := model.IndexJobItem{
		UploadID: uploadID,
		FileName: s.staging.FileName(uploadID),
	}
	storedName := fmt.Sprintf("%d_%s", util.NowUnixNano(), util.SanitizeFilename(item.FileName))
	destination := util.BuildKnowledgeBaseUploadPath(s.serverConfig.UploadDir, knowledgeBaseID, storedName)
	fileName, size, err := s.staging.Consume(uploadID, destination)
	if err != nil {
		item.ErrorCode = indexErrorFailed
		item.Error = err.Error()
		return item
	}
	item.FileName = fileName
	document := model.Document{
		ID:              util.NextID("doc"),
		KnowledgeBaseID: knowledgeBaseID,
		Name:            fileName,
		Size:            size,
		SizeLabel:       util.FormatFileSize(size),
		UploadedAt:      util.NowRFC3339(),
		Status:          "processing",
		Path:            destination,
		ContentPreview:  util.ExtractContentPreview(destination),
	}
	indexed, err := s.IndexDocumentWithLog(document, model.OperationSourceWeb, util.NextID("op"))
	if err != nil {
		_ = os.Remove(destination)
		item.ErrorCode = classifyIndexErrorCode(err)
		item.Error = err.Error()
		return item
	}
	item.Success = true
	item.DocumentID = indexed.ID
	return item
}

type indexJobState struct {
	mu        sync.Mutex
	job       *model.IndexJob
	uploadIDs []string
	canceled  bool
	documents map[string]model.Document
	running   bool
}

func (s *AppService) lookupIndexJob(jobID string) (*indexJobState, error) {
	if s == nil {
		return nil, fmt.Errorf("app service is nil")
	}
	s.jobsMu.Lock()
	defer s.jobsMu.Unlock()
	state := s.jobs[strings.TrimSpace(jobID)]
	if state == nil {
		return nil, fmt.Errorf("job not found")
	}
	return state, nil
}

func cloneIndexJob(job *model.IndexJob) model.IndexJob {
	if job == nil {
		return model.IndexJob{}
	}
	cloned := *job
	cloned.Items = append([]model.IndexJobItem{}, job.Items...)
	return cloned
}

func uniqueNonEmpty(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	return result
}
