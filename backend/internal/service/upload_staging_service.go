package service

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
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
	stagedUploadStatusStaged      = "staged"
	stagedUploadStatusProcessing  = "processing"
	stagedUploadStatusConsumed    = "consumed"
	stagedUploadStatusDeleted     = "deleted"
	defaultStagedUploadTTL        = 30 * time.Minute
	defaultStagedMaxFiles         = 8
	defaultStagedMaxBytesPerOwner = 256 * 1024 * 1024
	defaultStagedMaxBytes         = 1024 * 1024 * 1024
)

var ErrUploadStagingQuotaExceeded = errors.New("upload staging quota exceeded")

type UploadStagingLimits struct {
	MaxFilesPerPrincipal int
	MaxBytesPerPrincipal int64
	MaxBytes             int64
}

type stagingQuotaReservation struct {
	principalKey string
	size         int64
}

type UploadStagingService struct {
	rootDir string
	ttl     time.Duration
	limits  UploadStagingLimits

	mu           sync.RWMutex
	items        map[string]model.StagedUpload
	reservations map[string]stagingQuotaReservation
}

func NewUploadStagingService(rootDir string, ttl time.Duration) *UploadStagingService {
	return NewUploadStagingServiceWithLimits(rootDir, ttl, UploadStagingLimits{
		MaxFilesPerPrincipal: defaultStagedMaxFiles,
		MaxBytesPerPrincipal: defaultStagedMaxBytesPerOwner,
		MaxBytes:             defaultStagedMaxBytes,
	})
}

func NewUploadStagingServiceWithLimits(rootDir string, ttl time.Duration, limits UploadStagingLimits) *UploadStagingService {
	trimmedRoot := strings.TrimSpace(rootDir)
	if trimmedRoot == "" {
		trimmedRoot = filepath.Join("data", "staging")
	}
	if ttl <= 0 {
		ttl = defaultStagedUploadTTL
	}
	return &UploadStagingService{
		rootDir:      trimmedRoot,
		ttl:          ttl,
		limits:       limits,
		items:        map[string]model.StagedUpload{},
		reservations: map[string]stagingQuotaReservation{},
	}
}

func (s *UploadStagingService) StageMultipartFile(file *multipart.FileHeader, source string) (model.StagedUpload, error) {
	return s.StageMultipartFileAs(file, source, AuthPrincipal{})
}

func (s *UploadStagingService) StageMultipartFileAs(file *multipart.FileHeader, source string, owner AuthPrincipal) (model.StagedUpload, error) {
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

	return s.stageFromReader(file.Filename, file.Size, opened, source, owner)
}

func (s *UploadStagingService) StageBytes(fileName string, content []byte, source string) (model.StagedUpload, error) {
	return s.StageBytesAs(fileName, content, source, AuthPrincipal{})
}

func (s *UploadStagingService) StageBytesAs(fileName string, content []byte, source string, owner AuthPrincipal) (model.StagedUpload, error) {
	if s == nil {
		return model.StagedUpload{}, fmt.Errorf("upload staging service is nil")
	}
	return s.stageFromReader(fileName, int64(len(content)), strings.NewReader(string(content)), source, owner)
}

func (s *UploadStagingService) Get(uploadID string) (model.StagedUpload, error) {
	return s.get(uploadID, false)
}

func (s *UploadStagingService) get(uploadID string, allowProcessing bool) (model.StagedUpload, error) {
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
	if item.Status != stagedUploadStatusStaged && !(allowProcessing && item.Status == stagedUploadStatusProcessing) {
		return model.StagedUpload{}, fmt.Errorf("staged upload is not available")
	}
	return item, nil
}

// Claim atomically reserves a staged upload for one indexing attempt.
func (s *UploadStagingService) Claim(uploadID string) (model.StagedUpload, error) {
	return s.ClaimAs(uploadID, AuthPrincipal{})
}

func (s *UploadStagingService) ClaimAs(uploadID string, owner AuthPrincipal) (model.StagedUpload, error) {
	if s == nil {
		return model.StagedUpload{}, fmt.Errorf("staged upload service is nil")
	}
	trimmedID := strings.TrimSpace(uploadID)
	if trimmedID == "" {
		return model.StagedUpload{}, fmt.Errorf("upload id is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.items[trimmedID]
	if !ok {
		return model.StagedUpload{}, fmt.Errorf("staged upload not found")
	}
	if isStagedUploadExpired(item) {
		return model.StagedUpload{}, fmt.Errorf("staged upload expired")
	}
	if item.Status != stagedUploadStatusStaged {
		return model.StagedUpload{}, fmt.Errorf("staged upload is not available")
	}
	if !stagedUploadOwnerMatches(item, owner) {
		return model.StagedUpload{}, fmt.Errorf("staged upload is not owned by this principal")
	}
	item.Status = stagedUploadStatusProcessing
	s.items[trimmedID] = item
	return item, nil
}

func (s *UploadStagingService) Release(uploadID string) error {
	if s == nil {
		return fmt.Errorf("staged upload service is nil")
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
	if item.Status == stagedUploadStatusProcessing {
		item.Status = stagedUploadStatusStaged
		s.items[trimmedID] = item
	}
	return nil
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

// CopyTo copies a staged upload into a permanent application data directory.
// The staged source remains available until the caller completes indexing.
func (s *UploadStagingService) CopyTo(uploadID, destinationDir string) (string, error) {
	if s == nil {
		return "", fmt.Errorf("upload staging service is nil")
	}
	staged, err := s.get(uploadID, true)
	if err != nil {
		return "", err
	}
	trimmedDestinationDir := strings.TrimSpace(destinationDir)
	if trimmedDestinationDir == "" {
		return "", fmt.Errorf("destination directory is required")
	}
	if err := os.MkdirAll(trimmedDestinationDir, 0o755); err != nil {
		return "", fmt.Errorf("create permanent upload directory: %w", err)
	}

	fileName := util.SanitizeFilename(staged.FileName)
	if fileName == "" {
		fileName = "upload"
	}
	destination := filepath.Join(trimmedDestinationDir, fmt.Sprintf("%s_%s", util.NextID("upload"), fileName))
	temporary, err := os.CreateTemp(trimmedDestinationDir, ".staged-copy-*")
	if err != nil {
		return "", fmt.Errorf("create permanent upload file: %w", err)
	}
	temporaryPath := temporary.Name()
	cleanupTemporary := true
	defer func() {
		if cleanupTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()

	source, err := os.Open(staged.Path)
	if err != nil {
		_ = temporary.Close()
		return "", fmt.Errorf("open staged upload: %w", err)
	}
	_, copyErr := io.Copy(temporary, source)
	closeSourceErr := source.Close()
	closeTemporaryErr := temporary.Close()
	if copyErr != nil {
		return "", fmt.Errorf("copy staged upload: %w", copyErr)
	}
	if closeSourceErr != nil {
		return "", fmt.Errorf("close staged upload: %w", closeSourceErr)
	}
	if closeTemporaryErr != nil {
		return "", fmt.Errorf("close permanent upload: %w", closeTemporaryErr)
	}
	if err := os.Rename(temporaryPath, destination); err != nil {
		return "", fmt.Errorf("commit permanent upload: %w", err)
	}
	cleanupTemporary = false
	return destination, nil
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
	activePaths := map[string]struct{}{}

	now := time.Now().UTC()
	s.mu.Lock()
	for id, item := range s.items {
		expiresAt, err := time.Parse(time.RFC3339, item.ExpiresAt)
		if err != nil || !expiresAt.After(now) {
			items = append(items, expiredItem{id: id, path: item.Path})
			delete(s.items, id)
			continue
		}
		activePaths[filepath.Clean(item.Path)] = struct{}{}
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

	entries, err := os.ReadDir(s.rootDir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("scan staging directory: %w", err)
	}
	cutoff := now.Add(-s.ttl)
	for _, entry := range entries {
		if !entry.Type().IsRegular() {
			continue
		}
		path := filepath.Join(s.rootDir, entry.Name())
		if _, ok := activePaths[filepath.Clean(path)]; ok {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("inspect staged file %s: %w", entry.Name(), err)
		}
		if info.ModTime().After(cutoff) {
			continue
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("cleanup orphaned staged file %s: %w", entry.Name(), err)
		}
	}
	return nil
}

func (s *UploadStagingService) stageFromReader(fileName string, sizeHint int64, reader io.Reader, source string, owner AuthPrincipal) (model.StagedUpload, error) {
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
	principalKey := stagedUploadPrincipalKey(owner)
	reservedSize := sizeHint
	if reservedSize < 0 {
		reservedSize = 0
	}
	s.mu.Lock()
	if err := s.checkQuotaLocked(principalKey, reservedSize); err != nil {
		s.mu.Unlock()
		return model.StagedUpload{}, err
	}
	if s.reservations == nil {
		s.reservations = map[string]stagingQuotaReservation{}
	}
	s.reservations[uploadID] = stagingQuotaReservation{principalKey: principalKey, size: reservedSize}
	s.mu.Unlock()
	reservationActive := true
	defer func() {
		if !reservationActive {
			return
		}
		s.mu.Lock()
		delete(s.reservations, uploadID)
		s.mu.Unlock()
	}()
	temporary, err := os.CreateTemp(s.rootDir, ".staged-upload-*")
	if err != nil {
		return model.StagedUpload{}, fmt.Errorf("create temporary staged file: %w", err)
	}
	temporaryPath := temporary.Name()
	cleanupTemporary := true
	defer func() {
		if cleanupTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()

	hasher := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(temporary, hasher), reader)
	closeErr := temporary.Close()
	if copyErr != nil {
		return model.StagedUpload{}, fmt.Errorf("write staged file: %w", copyErr)
	}
	if closeErr != nil {
		return model.StagedUpload{}, fmt.Errorf("close staged file: %w", closeErr)
	}
	if written == 0 && sizeHint == 0 {
		return model.StagedUpload{}, fmt.Errorf("staged file is empty")
	}

	createdAt := time.Now().UTC()
	staged := model.StagedUpload{
		ID:            uploadID,
		FileName:      trimmedName,
		Size:          written,
		SizeLabel:     util.FormatFileSize(written),
		SHA256:        hex.EncodeToString(hasher.Sum(nil)),
		CreatedAt:     createdAt.Format(time.RFC3339),
		ExpiresAt:     createdAt.Add(s.ttl).Format(time.RFC3339),
		Status:        stagedUploadStatusStaged,
		Source:        strings.TrimSpace(source),
		OwnerUserID:   strings.TrimSpace(owner.UserID),
		OwnerAPIKeyID: strings.TrimSpace(owner.APIKeyID),
	}

	s.mu.Lock()
	delete(s.reservations, uploadID)
	reservationActive = false
	if err := s.checkQuotaLocked(principalKey, written); err != nil {
		s.mu.Unlock()
		return model.StagedUpload{}, err
	}
	storedName := fmt.Sprintf("%s_%s", uploadID, util.SanitizeFilename(trimmedName))
	destination := filepath.Join(s.rootDir, storedName)
	if err := os.Rename(temporaryPath, destination); err != nil {
		s.mu.Unlock()
		return model.StagedUpload{}, fmt.Errorf("commit staged file: %w", err)
	}
	cleanupTemporary = false
	staged.Path = destination
	s.items[staged.ID] = staged
	s.mu.Unlock()

	return staged, nil
}

func (s *UploadStagingService) checkQuotaLocked(principalKey string, size int64) error {
	if s == nil {
		return fmt.Errorf("upload staging service is nil")
	}
	activeFiles := 0
	activeBytes := int64(0)
	totalBytes := int64(0)
	now := time.Now().UTC()
	for _, item := range s.items {
		if item.Status != stagedUploadStatusStaged && item.Status != stagedUploadStatusProcessing {
			continue
		}
		if expiresAt, err := time.Parse(time.RFC3339, item.ExpiresAt); err == nil && !expiresAt.After(now) {
			continue
		}
		totalBytes += item.Size
		if stagedUploadPrincipalKeyFromItem(item) != principalKey {
			continue
		}
		activeFiles++
		activeBytes += item.Size
	}
	for _, reservation := range s.reservations {
		totalBytes += reservation.size
		if reservation.principalKey != principalKey {
			continue
		}
		activeFiles++
		activeBytes += reservation.size
	}

	if s.limits.MaxFilesPerPrincipal > 0 && activeFiles >= s.limits.MaxFilesPerPrincipal {
		return fmt.Errorf("%w: principal file limit reached (%d)", ErrUploadStagingQuotaExceeded, s.limits.MaxFilesPerPrincipal)
	}
	if s.limits.MaxBytesPerPrincipal > 0 && activeBytes > s.limits.MaxBytesPerPrincipal-size {
		return fmt.Errorf("%w: principal byte limit reached (%s)", ErrUploadStagingQuotaExceeded, util.FormatFileSize(s.limits.MaxBytesPerPrincipal))
	}
	if s.limits.MaxBytes > 0 && totalBytes > s.limits.MaxBytes-size {
		return fmt.Errorf("%w: global byte limit reached (%s)", ErrUploadStagingQuotaExceeded, util.FormatFileSize(s.limits.MaxBytes))
	}
	return nil
}

func stagedUploadPrincipalKey(owner AuthPrincipal) string {
	if apiKeyID := strings.TrimSpace(owner.APIKeyID); apiKeyID != "" {
		return "api-key:" + apiKeyID
	}
	if userID := strings.TrimSpace(owner.UserID); userID != "" {
		return "user:" + userID
	}
	if authType := strings.TrimSpace(owner.AuthType); authType != "" {
		return "auth:" + authType
	}
	return "anonymous"
}

func stagedUploadPrincipalKeyFromItem(item model.StagedUpload) string {
	if apiKeyID := strings.TrimSpace(item.OwnerAPIKeyID); apiKeyID != "" {
		return "api-key:" + apiKeyID
	}
	if userID := strings.TrimSpace(item.OwnerUserID); userID != "" {
		return "user:" + userID
	}
	return "anonymous"
}

func stagedUploadOwnerMatches(item model.StagedUpload, owner AuthPrincipal) bool {
	if strings.TrimSpace(owner.AuthType) == "" {
		return true
	}
	if hasScope(owner.Scopes, "mcp:admin") {
		return true
	}
	if strings.TrimSpace(owner.APIKeyID) != "" {
		return strings.TrimSpace(item.OwnerAPIKeyID) == strings.TrimSpace(owner.APIKeyID)
	}
	return strings.TrimSpace(item.OwnerAPIKeyID) == "" && strings.TrimSpace(item.OwnerUserID) == strings.TrimSpace(owner.UserID)
}

func hasScope(scopes []string, required string) bool {
	required = strings.ToLower(strings.TrimSpace(required))
	for _, scope := range scopes {
		if strings.ToLower(strings.TrimSpace(scope)) == required {
			return true
		}
	}
	return false
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
