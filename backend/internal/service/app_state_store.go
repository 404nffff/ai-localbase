package service

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"ai-localbase/internal/model"
)

type persistentAppState struct {
	Config         model.AppConfig                `json:"config"`
	KnowledgeBases map[string]model.KnowledgeBase `json:"knowledgeBases"`
}

type AppStateStore struct {
	path string
	// 同一存储实例串行替换状态，避免并发写入争用目标文件。
	mu sync.Mutex
}

func NewAppStateStore(path string) *AppStateStore {
	return &AppStateStore{path: path}
}

func (s *AppStateStore) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

func (s *AppStateStore) Load() (*persistentAppState, error) {
	if s == nil || s.path == "" {
		return nil, nil
	}

	content, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read app state: %w", err)
	}

	var state persistentAppState
	if err := json.Unmarshal(content, &state); err != nil {
		return nil, fmt.Errorf("decode app state: %w", err)
	}
	if state.KnowledgeBases == nil {
		state.KnowledgeBases = map[string]model.KnowledgeBase{}
	}
	return &state, nil
}

func (s *AppStateStore) Save(state persistentAppState) error {
	if s == nil || s.path == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("create app state directory: %w", err)
	}

	content, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode app state: %w", err)
	}

	// 每次写入使用独立且仅当前用户可读的临时文件，失败时清理未发布的快照。
	tempFile, err := os.CreateTemp(filepath.Dir(s.path), filepath.Base(s.path)+"-*.tmp")
	if err != nil {
		return fmt.Errorf("create app state temp file: %w", err)
	}
	defer os.Remove(tempFile.Name())
	if _, err := tempFile.Write(content); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("write app state temp file: %w", err)
	}
	// Windows 替换文件前必须关闭句柄，确保新状态已经完整写入。
	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("close app state temp file: %w", err)
	}
	if err := os.Rename(tempFile.Name(), s.path); err != nil {
		return fmt.Errorf("replace app state file: %w", err)
	}
	return nil
}
