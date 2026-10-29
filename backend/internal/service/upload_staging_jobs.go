package service

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// claimUploads 先验证整批身份及有效期，再持久化任务归属；其他任务不能复用同一上传。
func (s *UploadStagingService) claimUploads(ids []string, jobID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loadErr != nil {
		return s.loadErr
	}
	for _, id := range ids {
		r := s.items[id]
		if r == nil || r.Consumed || (r.JobID != "" && r.JobID != jobID) || (r.JobID == "" && time.Since(r.CreatedAt) > s.ttl) {
			return fmt.Errorf("staged upload unavailable: %s", id)
		}
	}
	for _, id := range ids {
		r := s.items[id]
		r.JobID = jobID
		if err := writeDurableJSON(filepath.Join(s.dir, id+".json"), r); err != nil {
			return err
		}
	}
	return nil
}

// copyForJob 保留暂存源文件直到任务检查点确认，进程退出后可以安全重复复制。
func (s *UploadStagingService) copyForJob(uploadID, jobID, destination string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.items[uploadID]
	if r == nil || r.JobID != jobID || r.Consumed {
		return fmt.Errorf("staged upload does not belong to job")
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
		return err
	}
	return copyFile(r.Path, destination)
}

// releaseCompletedUpload 只回收已持久化成功检查点的暂存副本，失败和取消项保留供显式重试。
func (s *UploadStagingService) releaseCompletedUpload(uploadID, jobID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.items[uploadID]
	if r == nil {
		return nil
	}
	if r.JobID != jobID {
		return fmt.Errorf("upload owner changed")
	}
	if err := os.Remove(r.Path); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Remove(filepath.Join(s.dir, uploadID+".json")); err != nil && !os.IsNotExist(err) {
		return err
	}
	delete(s.items, uploadID)
	return nil
}
