package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestBackendDockerfileUsesAppWorkdirAtRuntime(t *testing.T) {
	content := readProjectFile(t, "docker", "backend.Dockerfile")
	parts := strings.Split(content, "FROM alpine:latest")
	if len(parts) != 2 {
		t.Fatalf("expected runtime stage in backend Dockerfile")
	}

	runtimeStage := parts[1]
	if !strings.Contains(runtimeStage, "WORKDIR /app") {
		t.Fatalf("expected runtime stage to use /app as working directory, got:\n%s", runtimeStage)
	}
	if strings.Contains(runtimeStage, "WORKDIR /root/") {
		t.Fatalf("runtime stage must not write persistent data under /root, got:\n%s", runtimeStage)
	}
}

func TestComposeBackendUsesMountedAppDataPaths(t *testing.T) {
	assertComposeHasPersistentPaths(t, "docker-compose.yml", []string{
		"UPLOAD_DIR: /app/data/uploads",
		"STATE_FILE: /app/data/app-state.json",
		"CHAT_HISTORY_FILE: /app/data/chat-history.db",
	})
}

func TestAppComposeBackendUsesMountedAppDataPaths(t *testing.T) {
	assertComposeHasPersistentPaths(t, "docker-compose.app.yml", []string{
		"UPLOAD_DIR: /app/data/uploads",
		"STATE_FILE: /app/data/app-state.json",
		"CHAT_HISTORY_FILE: /app/data/chat-history.db",
	})
}

func TestProdComposeBackendUsesMountedAppDataPaths(t *testing.T) {
	assertComposeHasPersistentPaths(t, "docker-compose.prod.yml", []string{
		"UPLOAD_DIR=/app/data/uploads",
		"STATE_FILE=/app/data/app-state.json",
		"CHAT_HISTORY_FILE=/app/data/chat-history.db",
	})
}

func TestComposeIncludesBundledOllamaStack(t *testing.T) {
	assertComposeHasOllamaStack(t, "docker-compose.yml", []string{
		"ollama:",
		"./ollama_storage:/root/.ollama",
		"OLLAMA_BASE_URL: ${OLLAMA_BASE_URL:-http://ollama:11434}",
		"test: [\"CMD\", \"ollama\", \"list\"]",
	}, []string{
		"ollama-init:",
		`"http://ollama:11434/api/tags"`,
		`"name":"qwen3.5:0.8b"`,
		`"name":"nomic-embed-text"`,
	})
}

func TestAppComposeIncludesBundledOllamaStack(t *testing.T) {
	assertComposeHasOllamaStack(t, "docker-compose.app.yml", []string{
		"ollama:",
		"./ollama_storage:/root/.ollama",
		"OLLAMA_BASE_URL: ${OLLAMA_BASE_URL:-http://ollama:11434}",
		"test: [\"CMD\", \"ollama\", \"list\"]",
	}, []string{
		"ollama-init:",
		`"http://ollama:11434/api/tags"`,
		`"name":"qwen3.5:0.8b"`,
		`"name":"nomic-embed-text"`,
	})
}

func TestProdComposeIncludesBundledOllamaStack(t *testing.T) {
	assertComposeHasOllamaStack(t, "docker-compose.prod.yml", []string{
		"ollama:",
		"ollama_storage:/root/.ollama",
		"OLLAMA_BASE_URL=${OLLAMA_BASE_URL:-http://ollama:11434}",
		"test: [\"CMD\", \"ollama\", \"list\"]",
		"ollama_storage:",
	}, []string{
		"ollama-init:",
		`"http://ollama:11434/api/tags"`,
		`"name":"qwen3.5:0.8b"`,
		`"name":"nomic-embed-text"`,
	})
}

func TestReadmeDocumentsBundledOllamaModelPull(t *testing.T) {
	content := readProjectFile(t, "README.md")
	expected := []string{
		"`ollama`",
		"docker compose exec ollama ollama pull <模型名>",
		"docker compose exec ollama ollama list",
		"`qwen3.5:0.8b`",
		"`nomic-embed-text`",
	}

	for _, item := range expected {
		if !strings.Contains(content, item) {
			t.Fatalf("README.md missing expected Ollama guidance %q", item)
		}
	}
	unexpected := []string{
		"`ollama-init`",
		"自动拉取以下默认模型",
	}
	for _, item := range unexpected {
		if strings.Contains(content, item) {
			t.Fatalf("README.md must not include removed automatic Ollama guidance %q", item)
		}
	}
}

func TestComposeSupportsGlobalAndPerServicePortBindHosts(t *testing.T) {
	assertComposeHasPortBindingControls(t, "docker-compose.yml", []string{
		`"${QDRANT_HTTP_BIND_HOST:-${PORT_BIND_HOST:-127.0.0.1}}:${QDRANT_HTTP_PORT:-6333}:6333"`,
		`"${QDRANT_GRPC_BIND_HOST:-${PORT_BIND_HOST:-127.0.0.1}}:${QDRANT_GRPC_PORT:-6334}:6334"`,
		`"${OLLAMA_BIND_HOST:-${PORT_BIND_HOST:-127.0.0.1}}:${OLLAMA_PORT:-11434}:11434"`,
		`"${BACKEND_BIND_HOST:-${PORT_BIND_HOST:-127.0.0.1}}:${BACKEND_PORT:-8080}:8080"`,
		`"${FRONTEND_BIND_HOST:-${PORT_BIND_HOST:-127.0.0.1}}:${FRONTEND_PORT:-4173}:4173"`,
	})
}

func TestAppComposeSupportsGlobalAndPerServicePortBindHosts(t *testing.T) {
	assertComposeHasPortBindingControls(t, "docker-compose.app.yml", []string{
		`"${QDRANT_HTTP_BIND_HOST:-${PORT_BIND_HOST:-127.0.0.1}}:${QDRANT_HTTP_PORT:-6333}:6333"`,
		`"${QDRANT_GRPC_BIND_HOST:-${PORT_BIND_HOST:-127.0.0.1}}:${QDRANT_GRPC_PORT:-6334}:6334"`,
		`"${OLLAMA_BIND_HOST:-${PORT_BIND_HOST:-127.0.0.1}}:${OLLAMA_PORT:-11434}:11434"`,
		`"${BACKEND_BIND_HOST:-${PORT_BIND_HOST:-127.0.0.1}}:${BACKEND_PORT:-8080}:8080"`,
		`"${FRONTEND_BIND_HOST:-${PORT_BIND_HOST:-127.0.0.1}}:${FRONTEND_PORT:-4173}:4173"`,
	})
}

func TestProdComposeSupportsGlobalAndPerServicePortBindHosts(t *testing.T) {
	assertComposeHasPortBindingControls(t, "docker-compose.prod.yml", []string{
		`"${QDRANT_HTTP_BIND_HOST:-${PORT_BIND_HOST:-127.0.0.1}}:${QDRANT_HTTP_PORT:-6333}:6333"`,
		`"${OLLAMA_BIND_HOST:-${PORT_BIND_HOST:-127.0.0.1}}:${OLLAMA_PORT:-11434}:11434"`,
		`"${BACKEND_BIND_HOST:-${PORT_BIND_HOST:-127.0.0.1}}:${BACKEND_PORT:-8080}:8080"`,
		`"${FRONTEND_BIND_HOST:-${PORT_BIND_HOST:-127.0.0.1}}:${FRONTEND_PORT:-4173}:4173"`,
	})
}

func TestEnvExampleDocumentsPortBindHostVariables(t *testing.T) {
	content := readProjectFile(t, ".env.example")
	expected := []string{
		"PORT_BIND_HOST=127.0.0.1",
		"BACKEND_BIND_HOST=",
		"FRONTEND_BIND_HOST=",
		"OLLAMA_BIND_HOST=",
		"QDRANT_HTTP_BIND_HOST=",
		"QDRANT_GRPC_BIND_HOST=",
	}
	for _, item := range expected {
		if !strings.Contains(content, item) {
			t.Fatalf(".env.example missing expected port bind variable %q", item)
		}
	}
}

func TestReadmeDocumentsPortBindHostControls(t *testing.T) {
	content := readProjectFile(t, "README.md")
	expected := []string{
		"`PORT_BIND_HOST`",
		"`BACKEND_BIND_HOST`",
		"`FRONTEND_BIND_HOST`",
		"`OLLAMA_BIND_HOST`",
		"`QDRANT_HTTP_BIND_HOST`",
		"`QDRANT_GRPC_BIND_HOST`",
		"`127.0.0.1`",
		"`0.0.0.0`",
	}
	for _, item := range expected {
		if !strings.Contains(content, item) {
			t.Fatalf("README.md missing expected port binding guidance %q", item)
		}
	}
}

func assertComposeHasPersistentPaths(t *testing.T, name string, expected []string) {
	t.Helper()

	content := readProjectFile(t, name)
	for _, item := range expected {
		if !strings.Contains(content, item) {
			t.Fatalf("%s missing expected persistent path %q", name, item)
		}
	}
}

func assertComposeHasOllamaStack(t *testing.T, name string, expected []string, unexpected []string) {
	t.Helper()

	content := readProjectFile(t, name)
	for _, item := range expected {
		if !strings.Contains(content, item) {
			t.Fatalf("%s missing expected Ollama stack item %q", name, item)
		}
	}
	for _, item := range unexpected {
		if strings.Contains(content, item) {
			t.Fatalf("%s must not include removed Ollama stack item %q", name, item)
		}
	}
}

func assertComposeHasPortBindingControls(t *testing.T, name string, expected []string) {
	t.Helper()

	content := readProjectFile(t, name)
	for _, item := range expected {
		if !strings.Contains(content, item) {
			t.Fatalf("%s missing expected port binding control %q", name, item)
		}
	}
}

func readProjectFile(t *testing.T, parts ...string) string {
	t.Helper()

	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to resolve current test file")
	}

	projectRoot := filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", "..", ".."))
	targetPath := filepath.Join(append([]string{projectRoot}, parts...)...)
	content, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("read %s: %v", targetPath, err)
	}
	return string(content)
}
