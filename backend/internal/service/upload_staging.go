package service

import (
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"ai-localbase/internal/model"
	"ai-localbase/internal/util"
)

const defaultStagedUploadTTL = 30 * time.Minute

type stagedUploadRecord struct {
	ID        string
	FileName  string
	Path      string
	Size      int64
	CreatedAt time.Time
	Consumed  bool
	JobID     string
}

// UploadStagingService 把大文件先落到暂存目录，避免同步索引堵住上传请求。
type UploadStagingService struct {
	dir     string
	ttl     time.Duration
	mu      sync.Mutex
	items   map[string]*stagedUploadRecord
	loadErr error
}

func (s *AppService) StageUploadedFile(file *multipart.FileHeader) (model.StagedUpload, error) {
	if s == nil || s.staging == nil {
		return model.StagedUpload{}, fmt.Errorf("upload staging is not configured")
	}
	return s.staging.Stage(file)
}

// NewUploadStagingService 恢复暂存清单及 ID 序列；清单损坏保留错误，后续上传不能假装恢复成功。
func NewUploadStagingService(dir string) *UploadStagingService {
	trimmed := strings.TrimSpace(dir)
	if trimmed == "" {
		trimmed = "data/staging"
	}
	service := &UploadStagingService{
		dir:   trimmed,
		ttl:   defaultStagedUploadTTL,
		items: map[string]*stagedUploadRecord{},
	}
	paths, err := filepath.Glob(filepath.Join(trimmed, "upl-*.json"))
	if err != nil {
		service.loadErr = err
		return service
	}
	for _, path := range paths {
		body, err := os.ReadFile(path)
		var record stagedUploadRecord
		if err == nil {
			err = json.Unmarshal(body, &record)
		}
		if err == nil && (record.ID == "" || filepath.Base(path) != record.ID+".json" || filepath.Dir(record.Path) != filepath.Clean(trimmed)) {
			err = fmt.Errorf("invalid upload manifest")
		}
		if err != nil {
			service.loadErr = err
			return service
		}
		service.items[record.ID] = &record
		util.ObserveID(record.ID)
	}
	return service
}

// Stage 先同步上传正文并保存清单，再公布暂存 ID；登记失败移除本次文件并返回错误。
func (s *UploadStagingService) Stage(file *multipart.FileHeader) (model.StagedUpload, error) {
	if s == nil {
		return model.StagedUpload{}, fmt.Errorf("upload staging is not configured")
	}
	if s.loadErr != nil {
		return model.StagedUpload{}, fmt.Errorf("load staged uploads: %w", s.loadErr)
	}
	if file == nil {
		return model.StagedUpload{}, fmt.Errorf("missing file field 'file'")
	}
	s.cleanupExpired()
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return model.StagedUpload{}, fmt.Errorf("prepare staging directory: %w", err)
	}

	id := util.NextID("upl")
	safeName := util.SanitizeFilename(file.Filename)
	destination := filepath.Join(s.dir, id+"_"+safeName)
	if err := saveMultipartFile(file, destination); err != nil {
		return model.StagedUpload{}, err
	}

	now := time.Now().UTC()
	record := &stagedUploadRecord{
		ID:        id,
		FileName:  file.Filename,
		Path:      destination,
		Size:      file.Size,
		CreatedAt: now,
	}
	s.mu.Lock()
	if err := writeDurableJSON(filepath.Join(s.dir, id+".json"), record); err != nil {
		s.mu.Unlock()
		os.Remove(destination)
		return model.StagedUpload{}, err
	}
	s.items[id] = record
	s.mu.Unlock()

	return model.StagedUpload{
		ID:        id,
		FileName:  file.Filename,
		Size:      file.Size,
		SizeLabel: util.FormatFileSize(file.Size),
		CreatedAt: now.Format(time.RFC3339),
	}, nil
}

// Consume 供同步入口领取未过期上传，拒绝已消费或已归属后台任务的文件。
func (s *UploadStagingService) Consume(uploadID, destination string) (string, int64, error) {
	if s == nil {
		return "", 0, fmt.Errorf("upload staging is not configured")
	}
	s.cleanupExpired()
	id := strings.TrimSpace(uploadID)
	s.mu.Lock()
	record := s.items[id]
	if record == nil || record.Consumed || record.JobID != "" {
		s.mu.Unlock()
		return "", 0, fmt.Errorf("staged upload not found")
	}
	if time.Since(record.CreatedAt) > s.ttl {
		s.mu.Unlock()
		return "", 0, fmt.Errorf("staged upload expired")
	}
	record.Consumed = true
	sourcePath := record.Path
	fileName := record.FileName
	size := record.Size
	s.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		s.mu.Lock()
		record.Consumed = false
		s.mu.Unlock()
		return "", 0, fmt.Errorf("prepare upload directory: %w", err)
	}
	if err := copyFile(sourcePath, destination); err != nil {
		s.mu.Lock()
		record.Consumed = false
		s.mu.Unlock()
		return "", 0, err
	}
	_ = os.Remove(sourcePath)
	return fileName, size, nil
}

func (s *UploadStagingService) FileName(uploadID string) string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	record := s.items[strings.TrimSpace(uploadID)]
	if record == nil {
		return ""
	}
	return record.FileName
}

// cleanupExpired 只清理未分配任务的过期上传，失败与取消任务的文件留给显式重试。
func (s *UploadStagingService) cleanupExpired() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for id, record := range s.items {
		if record == nil {
			delete(s.items, id)
			continue
		}
		if record.JobID == "" && (record.Consumed || now.Sub(record.CreatedAt) > s.ttl) {
			_ = os.Remove(record.Path)
			_ = os.Remove(filepath.Join(s.dir, id+".json"))
			delete(s.items, id)
		}
	}
}

// saveMultipartFile 将上传流写入目标并同步文件，写入或同步错误直接返回。
func saveMultipartFile(file *multipart.FileHeader, destination string) error {
	src, err := file.Open()
	if err != nil {
		return fmt.Errorf("open uploaded file: %w", err)
	}
	defer src.Close()
	dst, err := os.Create(destination)
	if err != nil {
		return fmt.Errorf("create staged file: %w", err)
	}
	defer dst.Close()
	if _, err := io.Copy(dst, src); err != nil {
		_ = os.Remove(destination)
		return fmt.Errorf("write staged file: %w", err)
	}
	return dst.Sync()
}

func copyFile(source, destination string) error {
	src, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open staged file: %w", err)
	}
	defer src.Close()
	dst, err := os.Create(destination)
	if err != nil {
		return fmt.Errorf("create destination file: %w", err)
	}
	defer dst.Close()
	if _, err := io.Copy(dst, src); err != nil {
		_ = os.Remove(destination)
		return fmt.Errorf("copy staged file: %w", err)
	}
	return nil
}
