package config

import (
	"path/filepath"
	"testing"
)

func TestLoadServerConfigDerivesStagingDirectoryFromUploadDirectory(t *testing.T) {
	t.Setenv("UPLOAD_DIR", "/srv/ai-localbase/uploads")
	t.Setenv("STAGING_DIR", "")

	serverConfig := LoadServerConfig()
	if want := filepath.Join("/srv/ai-localbase", "staging"); serverConfig.StagingDir != want {
		t.Fatalf("expected staging directory %q, got %q", want, serverConfig.StagingDir)
	}
}

func TestLoadServerConfigUsesExplicitStagingDirectory(t *testing.T) {
	t.Setenv("UPLOAD_DIR", "/srv/ai-localbase/uploads")
	t.Setenv("STAGING_DIR", "/mnt/ai-localbase/staging")

	serverConfig := LoadServerConfig()
	if serverConfig.StagingDir != "/mnt/ai-localbase/staging" {
		t.Fatalf("expected explicit staging directory, got %q", serverConfig.StagingDir)
	}
}
