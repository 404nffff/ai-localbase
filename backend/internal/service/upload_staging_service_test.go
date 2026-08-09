package service

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"ai-localbase/internal/model"
)

func TestNewAppServiceDerivesStagingDirectoryFromUploadDirectory(t *testing.T) {
	uploadDir := filepath.Join(t.TempDir(), "uploads")
	appService := NewAppService(nil, nil, nil, model.ServerConfig{UploadDir: uploadDir})

	want := filepath.Join(filepath.Dir(uploadDir), "staging")
	if appService.staging.rootDir != want {
		t.Fatalf("expected staging directory %q, got %q", want, appService.staging.rootDir)
	}
}

func TestUploadStagingCopyToKeepsSourceUntilConsumed(t *testing.T) {
	rootDir := t.TempDir()
	destinationDir := t.TempDir()
	staging := NewUploadStagingService(rootDir, time.Hour)

	staged, err := staging.StageBytes("guide.md", []byte("staged content"), "test")
	if err != nil {
		t.Fatalf("stage upload: %v", err)
	}

	destination, err := staging.CopyTo(staged.ID, destinationDir)
	if err != nil {
		t.Fatalf("copy staged upload: %v", err)
	}
	if _, err := os.Stat(staged.Path); err != nil {
		t.Fatalf("expected staged source to remain before consume: %v", err)
	}
	content, err := os.ReadFile(destination)
	if err != nil {
		t.Fatalf("read permanent upload: %v", err)
	}
	if string(content) != "staged content" {
		t.Fatalf("unexpected permanent upload content: %q", content)
	}

	if err := staging.Delete(staged.ID); err != nil {
		t.Fatalf("delete consumed staging source: %v", err)
	}
	if _, err := os.Stat(staged.Path); !os.IsNotExist(err) {
		t.Fatalf("expected staged source to be deleted, got %v", err)
	}
}

func TestUploadStagingCleanupRemovesExpiredOrphanFiles(t *testing.T) {
	rootDir := t.TempDir()
	staging := NewUploadStagingService(rootDir, time.Minute)
	orphanPath := filepath.Join(rootDir, "orphan-upload.md")
	if err := os.WriteFile(orphanPath, []byte("orphan"), 0o644); err != nil {
		t.Fatalf("write orphan file: %v", err)
	}
	expiredAt := time.Now().Add(-2 * time.Minute)
	if err := os.Chtimes(orphanPath, expiredAt, expiredAt); err != nil {
		t.Fatalf("age orphan file: %v", err)
	}

	if err := staging.CleanupExpired(); err != nil {
		t.Fatalf("cleanup staging: %v", err)
	}
	if _, err := os.Stat(orphanPath); !os.IsNotExist(err) {
		t.Fatalf("expected expired orphan file to be removed, got %v", err)
	}
}

func TestUploadStagingClaimAllowsOnlyOneConcurrentConsumer(t *testing.T) {
	staging := NewUploadStagingService(t.TempDir(), time.Hour)
	staged, err := staging.StageBytes("guide.md", []byte("staged content"), "test")
	if err != nil {
		t.Fatalf("stage upload: %v", err)
	}

	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, claimErr := staging.Claim(staged.ID)
			results <- claimErr
		}()
	}
	wg.Wait()
	close(results)

	var successes, failures int
	for err := range results {
		if err == nil {
			successes++
		} else {
			failures++
		}
	}
	if successes != 1 || failures != 1 {
		t.Fatalf("expected one claim success and one failure, successes=%d failures=%d", successes, failures)
	}
	if err := staging.Release(staged.ID); err != nil {
		t.Fatalf("release claimed upload: %v", err)
	}
}
