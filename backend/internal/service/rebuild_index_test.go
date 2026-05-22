package service

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverUploadFilesIncludesArchivesWhenRequested(t *testing.T) {
	root := t.TempDir()
	writeRebuildTestFile(t, filepath.Join(root, "a.md"), "A")
	writeRebuildTestFile(t, filepath.Join(root, "skip.tmp"), "ignored")
	writeRebuildTestFile(t, filepath.Join(root, "md", "kb-1", "doc-1.md"), "archive")

	files, err := discoverUploadFiles(root, true)
	if err != nil {
		t.Fatalf("discover upload files: %v", err)
	}

	expected := []string{
		filepath.Join(root, "a.md"),
		filepath.Join(root, "md", "kb-1", "doc-1.md"),
	}
	assertRebuildStringSlicesEqual(t, expected, files)
}

func TestDiscoverUploadFilesCanSkipGeneratedArchives(t *testing.T) {
	root := t.TempDir()
	writeRebuildTestFile(t, filepath.Join(root, "a.md"), "A")
	writeRebuildTestFile(t, filepath.Join(root, "md", "kb-1", "doc-1.md"), "archive")

	files, err := discoverUploadFiles(root, false)
	if err != nil {
		t.Fatalf("discover upload files: %v", err)
	}

	expected := []string{filepath.Join(root, "a.md")}
	assertRebuildStringSlicesEqual(t, expected, files)
}

func TestOriginalUploadNameDropsTimestampPrefix(t *testing.T) {
	got := originalUploadName(filepath.Join("data", "uploads", "1776757219680994827_restore.md"))
	if got != "restore.md" {
		t.Fatalf("expected original upload name restore.md, got %q", got)
	}
}

func writeRebuildTestFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create dir for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func assertRebuildStringSlicesEqual(t *testing.T, expected, actual []string) {
	t.Helper()
	if len(expected) != len(actual) {
		t.Fatalf("expected %d items %#v, got %d %#v", len(expected), expected, len(actual), actual)
	}
	for index := range expected {
		if expected[index] != actual[index] {
			t.Fatalf("item %d expected %q, got %q; all=%#v", index, expected[index], actual[index], actual)
		}
	}
}
