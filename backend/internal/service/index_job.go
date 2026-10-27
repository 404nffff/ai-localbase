package service

import (
	"fmt"
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
	if s.jobs == nil {
		s.jobs = map[string]*indexJobState{}
	}
	state := &indexJobState{job: job, uploadIDs: ids}
	s.jobs[job.ID] = state
	s.jobsMu.Unlock()

	go s.runIndexJob(state)
	return cloneIndexJob(job), nil
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

func (s *AppService) CancelIndexJob(jobID string) (model.IndexJob, error) {
	state, err := s.lookupIndexJob(jobID)
	if err != nil {
		return model.IndexJob{}, err
	}
	state.mu.Lock()
	state.canceled = true
	if state.job.Status == indexJobQueued {
		state.job.Status = indexJobCanceled
		state.job.UpdatedAt = util.NowRFC3339()
	}
	cloned := cloneIndexJob(state.job)
	state.mu.Unlock()
	return cloned, nil
}

func (s *AppService) RetryIndexJob(jobID string) (model.IndexJob, error) {
	state, err := s.lookupIndexJob(jobID)
	if err != nil {
		return model.IndexJob{}, err
	}
	state.mu.Lock()
	failedIDs := make([]string, 0)
	for index, item := range state.job.Items {
		if !item.Success && strings.TrimSpace(item.UploadID) != "" {
			failedIDs = append(failedIDs, item.UploadID)
			_ = index
		}
	}
	if state.job.Status == indexJobRunning {
		state.mu.Unlock()
		return model.IndexJob{}, fmt.Errorf("job is still running")
	}
	remaining := failedIDs
	if len(remaining) == 0 {
		remaining = append([]string{}, state.uploadIDs...)
	}
	kbID := state.job.KnowledgeBaseID
	state.mu.Unlock()
	return s.StartBatchIndexJob(kbID, remaining)
}

func (s *AppService) runIndexJob(state *indexJobState) {
	if s == nil || state == nil {
		return
	}
	state.mu.Lock()
	state.job.Status = indexJobRunning
	state.job.UpdatedAt = util.NowRFC3339()
	uploadIDs := append([]string{}, state.uploadIDs...)
	kbID := state.job.KnowledgeBaseID
	state.mu.Unlock()

	for _, uploadID := range uploadIDs {
		state.mu.Lock()
		if state.canceled {
			state.job.Status = indexJobCanceled
			state.job.UpdatedAt = util.NowRFC3339()
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

		item := s.indexStagedUpload(kbID, uploadID)
		state.mu.Lock()
		state.job.Items = append(state.job.Items, item)
		state.job.Completed++
		if !item.Success {
			state.job.Failed++
		}
		state.job.UpdatedAt = util.NowRFC3339()
		state.mu.Unlock()
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
