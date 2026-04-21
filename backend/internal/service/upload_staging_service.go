package service

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
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

const (
	stagedUploadStatusStaged   = "staged"
	stagedUploadStatusConsumed = "consumed"
	stagedUploadStatusDeleted  = "deleted"
	defaultStagedUploadTTL     = 30 * time.Minute
)

type UploadStagingService struct {
	rootDir string
	ttl     time.Duration

	mu    sync.RWMutex
	items map[string]model.StagedUpload
}

func NewUploadStagingService(rootDir string, ttl time.Duration) *UploadStagingService {
	trimmedRoot := strings.TrimSpace(rootDir)
	if trimmedRoot == "" {
		trimmedRoot = filepath.Join("data", "staging")
	}
	if ttl <= 0 {
		ttl = defaultStagedUploadTTL
	}
	return &UploadStagingService{
		rootDir: trimmedRoot,
		ttl:     ttl,
		items:   map[string]model.StagedUpload{},
	}
}

func (s *UploadStagingService) StageMultipartFile(file *multipart.FileHeader, source string) (model.StagedUpload, error) {
	if s == nil {
		return model.StagedUpload{}, fmt.Errorf("upload staging service is nil")
	}
	if file == nil {
		return model.StagedUpload{}, fmt.Errorf("staged file is nil")
	}

	opened, err := file.Open()
	if err != nil {
		return model.StagedUpload{}, fmt.Errorf("open staged file: %w", err)
	}
	defer opened.Close()

	return s.stageFromReader(file.Filename, file.Size, opened, source)
}

func (s *UploadStagingService) StageBytes(fileName string, content []byte, source string) (model.StagedUpload, error) {
	if s == nil {
		return model.StagedUpload{}, fmt.Errorf("upload staging service is nil")
	}
	return s.stageFromReader(fileName, int64(len(content)), strings.NewReader(string(content)), source)
}

func (s *UploadStagingService) Get(uploadID string) (model.StagedUpload, error) {
	if s == nil {
		return model.StagedUpload{}, fmt.Errorf("upload staging service is nil")
	}
	trimmedID := strings.TrimSpace(uploadID)
	if trimmedID == "" {
		return model.StagedUpload{}, fmt.Errorf("upload id is required")
	}

	s.mu.RLock()
	item, ok := s.items[trimmedID]
	s.mu.RUnlock()
	if !ok {
		return model.StagedUpload{}, fmt.Errorf("staged upload not found")
	}
	if isStagedUploadExpired(item) {
		return model.StagedUpload{}, fmt.Errorf("staged upload expired")
	}
	if item.Status != stagedUploadStatusStaged {
		return model.StagedUpload{}, fmt.Errorf("staged upload is not available")
	}
	return item, nil
}

func (s *UploadStagingService) MarkConsumed(uploadID string) error {
	if s == nil {
		return fmt.Errorf("upload staging service is nil")
	}
	trimmedID := strings.TrimSpace(uploadID)
	if trimmedID == "" {
		return fmt.Errorf("upload id is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.items[trimmedID]
	if !ok {
		return fmt.Errorf("staged upload not found")
	}
	item.Status = stagedUploadStatusConsumed
	item.ConsumedAt = util.NowRFC3339()
	s.items[trimmedID] = item
	return nil
}

func (s *UploadStagingService) Delete(uploadID string) error {
	if s == nil {
		return fmt.Errorf("upload staging service is nil")
	}
	trimmedID := strings.TrimSpace(uploadID)
	if trimmedID == "" {
		return fmt.Errorf("upload id is required")
	}

	s.mu.Lock()
	item, ok := s.items[trimmedID]
	if ok {
		item.Status = stagedUploadStatusDeleted
		s.items[trimmedID] = item
	}
	delete(s.items, trimmedID)
	s.mu.Unlock()

	if ok && strings.TrimSpace(item.Path) != "" {
		if err := os.Remove(item.Path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("delete staged file: %w", err)
		}
	}
	return nil
}

func (s *UploadStagingService) CleanupExpired() error {
	if s == nil {
		return fmt.Errorf("upload staging service is nil")
	}

	type expiredItem struct {
		id   string
		path string
	}
	items := make([]expiredItem, 0)

	now := time.Now().UTC()
	s.mu.Lock()
	for id, item := range s.items {
		expiresAt, err := time.Parse(time.RFC3339, item.ExpiresAt)
		if err != nil || !expiresAt.After(now) {
			items = append(items, expiredItem{id: id, path: item.Path})
			delete(s.items, id)
		}
	}
	s.mu.Unlock()

	for _, item := range items {
		if strings.TrimSpace(item.path) == "" {
			continue
		}
		if err := os.Remove(item.path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("cleanup staged file %s: %w", item.id, err)
		}
	}
	return nil
}

func (s *UploadStagingService) stageFromReader(fileName string, sizeHint int64, reader io.Reader, source string) (model.StagedUpload, error) {
	trimmedName := strings.TrimSpace(fileName)
	if trimmedName == "" {
		return model.StagedUpload{}, fmt.Errorf("file name is required")
	}
	if err := os.MkdirAll(s.rootDir, 0o755); err != nil {
		return model.StagedUpload{}, fmt.Errorf("create staging directory: %w", err)
	}

	uploadID, err := nextUploadID()
	if err != nil {
		return model.StagedUpload{}, err
	}
	storedName := fmt.Sprintf("%s_%s", uploadID, util.SanitizeFilename(trimmedName))
	destination := filepath.Join(s.rootDir, storedName)

	file, err := os.Create(destination)
	if err != nil {
		return model.StagedUpload{}, fmt.Errorf("create staged file: %w", err)
	}

	hasher := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(file, hasher), reader)
	closeErr := file.Close()
	if copyErr != nil {
		_ = os.Remove(destination)
		return model.StagedUpload{}, fmt.Errorf("write staged file: %w", copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(destination)
		return model.StagedUpload{}, fmt.Errorf("close staged file: %w", closeErr)
	}
	if written == 0 && sizeHint == 0 {
		_ = os.Remove(destination)
		return model.StagedUpload{}, fmt.Errorf("staged file is empty")
	}

	createdAt := time.Now().UTC()
	staged := model.StagedUpload{
		ID:        uploadID,
		FileName:  trimmedName,
		Path:      destination,
		Size:      written,
		SizeLabel: util.FormatFileSize(written),
		SHA256:    hex.EncodeToString(hasher.Sum(nil)),
		CreatedAt: createdAt.Format(time.RFC3339),
		ExpiresAt: createdAt.Add(s.ttl).Format(time.RFC3339),
		Status:    stagedUploadStatusStaged,
		Source:    strings.TrimSpace(source),
	}

	s.mu.Lock()
	s.items[staged.ID] = staged
	s.mu.Unlock()

	return staged, nil
}

func isStagedUploadExpired(item model.StagedUpload) bool {
	expiresAt, err := time.Parse(time.RFC3339, item.ExpiresAt)
	if err != nil {
		return true
	}
	return !expiresAt.After(time.Now().UTC())
}

func nextUploadID() (string, error) {
	buffer := make([]byte, 8)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("generate upload id: %w", err)
	}
	return "upl_" + hex.EncodeToString(buffer), nil
}
