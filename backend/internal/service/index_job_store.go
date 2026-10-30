package service

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"ai-localbase/internal/model"
	"ai-localbase/internal/util"
)

// persistedIndexJob 把任务、取消标记和每个上传的稳定文档 ID 一并保存。
type persistedIndexJob struct {
	Job       model.IndexJob
	UploadIDs []string
	Canceled  bool
	Documents map[string]model.Document
}

// indexJobDir 与应用状态分开保存，Wiki 快照不会覆盖普通导入检查点。
func (s *AppService) indexJobDir() string {
	if s.store != nil && s.store.Path() != "" {
		return s.store.Path() + ".jobs"
	}
	return filepath.Join(s.staging.dir, "jobs")
}

// saveIndexJobLocked 调用方持有任务锁，成功返回才允许继续执行下一条。
func (s *AppService) saveIndexJobLocked(state *indexJobState) error {
	return writeDurableJSON(filepath.Join(s.indexJobDir(), state.job.ID+".json"), persistedIndexJob{cloneIndexJob(state.job), state.uploadIDs, state.canceled, state.documents})
}

// launchIndexJob 与 Close 共用锁，保证 Wait 不与新任务 Add 竞争。
func (s *AppService) launchIndexJob(state *indexJobState) error {
	s.jobsMu.Lock()
	defer s.jobsMu.Unlock()
	if s.jobsClosed {
		return fmt.Errorf("index service is closing")
	}
	state.mu.Lock()
	if state.running {
		state.mu.Unlock()
		return fmt.Errorf("index job is already running")
	}
	state.running = true
	state.mu.Unlock()
	s.jobsWorkers.Add(1)
	go func() {
		defer s.jobsWorkers.Done()
		defer func() { state.mu.Lock(); state.running = false; state.mu.Unlock() }()
		s.runIndexJob(state)
	}()
	return nil
}

// recoverIndexJobs 恢复检查点，running 仅重做未确认条目，取消与终态绝不自动复活。
func (s *AppService) recoverIndexJobs() {
	paths, err := filepath.Glob(filepath.Join(s.indexJobDir(), "*.json"))
	if err != nil {
		log.Printf("list index jobs: %v", err)
		return
	}
	for _, path := range paths {
		body, err := os.ReadFile(path)
		var stored persistedIndexJob
		if err == nil {
			err = json.Unmarshal(body, &stored)
		}
		if err == nil && (stored.Job.ID == "" || filepath.Base(path) != stored.Job.ID+".json" || stored.Documents == nil) {
			err = fmt.Errorf("invalid index checkpoint")
		}
		if err != nil {
			log.Printf("recover index job %s: %v", filepath.Base(path), err)
			continue
		}
		util.ObserveID(stored.Job.ID)
		for _, doc := range stored.Documents {
			util.ObserveID(doc.ID)
		}
		state := &indexJobState{job: &stored.Job, uploadIDs: stored.UploadIDs, canceled: stored.Canceled, documents: stored.Documents}
		s.jobsMu.Lock()
		s.jobs[state.job.ID] = state
		s.jobsMu.Unlock()
		if !state.canceled && (state.job.Status == indexJobQueued || state.job.Status == indexJobRunning) {
			if err := s.launchIndexJob(state); err != nil {
				log.Printf("resume index job: %v", err)
			}
		}
	}
}

// indexJobCheckpoint 发生持久化错误时停止任务，保留旧磁盘检查点供重启恢复。
func (s *AppService) indexJobCheckpoint(state *indexJobState) bool {
	if err := s.saveIndexJobLocked(state); err != nil {
		state.job.Status = indexJobFailed
		state.job.CurrentFileName = "检查点保存失败：" + err.Error()
		log.Printf("persist index job %s: %v", state.job.ID, err)
		return false
	}
	return true
}

// indexStagedJobUpload 使用稳定文档 ID 跨越“发布成功、检查点未写”的崩溃窗口。
func (s *AppService) indexStagedJobUpload(state *indexJobState, kbID, uploadID string) model.IndexJobItem {
	document := state.documents[uploadID]
	item := model.IndexJobItem{UploadID: uploadID, FileName: document.Name}
	if existing, err := s.GetDocument(kbID, document.ID); err == nil {
		if existing.Path != document.Path {
			item.ErrorCode, item.Error = indexErrorFailed, "checkpoint document identity conflicts with existing document"
			return item
		}
		item.Success, item.DocumentID = true, existing.ID
		return item
	}
	err := s.staging.claimUploads([]string{uploadID}, state.job.ID)
	if err == nil {
		err = s.staging.copyForJob(uploadID, state.job.ID, document.Path)
	}
	if err == nil {
		var info os.FileInfo
		info, err = os.Stat(document.Path)
		if err == nil {
			document.Size = info.Size()
			document.SizeLabel = util.FormatFileSize(info.Size())
		}
	}
	if err == nil {
		// 取消和最终发布共享任务锁，取消先发生时不允许发布候选文档。
		s.indexPublishGuards.Store(document.ID, func() (func(), error) {
			state.mu.Lock()
			if state.canceled {
				state.mu.Unlock()
				return nil, fmt.Errorf("index job canceled")
			}
			return state.mu.Unlock, nil
		})
		defer s.indexPublishGuards.Delete(document.ID)
		_, err = s.IndexDocumentWithLog(document, model.OperationSourceWeb, state.job.ID)
	}
	if err != nil {
		item.ErrorCode, item.Error = classifyIndexErrorCode(err), err.Error()
		return item
	}
	item.Success, item.DocumentID = true, document.ID
	return item
}
