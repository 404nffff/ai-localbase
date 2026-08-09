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
	Config         model.AppConfig                         `json:"config"`
	KnowledgeBases map[string]model.KnowledgeBase          `json:"knowledgeBases"`
	EvalDatasets   map[string]model.EvalDataset            `json:"evalDatasets,omitempty"`
	EvalRuns       map[string]model.RunEvalDatasetResponse `json:"evalRuns,omitempty"`
	Auth           model.AuthState                         `json:"auth,omitempty"`
}

type persistedAppStateJSON struct {
	Config         model.AppConfig                         `json:"config"`
	KnowledgeBases map[string]persistedKnowledgeBase       `json:"knowledgeBases"`
	EvalDatasets   map[string]model.EvalDataset            `json:"evalDatasets,omitempty"`
	EvalRuns       map[string]model.RunEvalDatasetResponse `json:"evalRuns,omitempty"`
	Auth           model.AuthState                         `json:"auth,omitempty"`
}

type persistedKnowledgeBase struct {
	ID          string              `json:"id"`
	Name        string              `json:"name"`
	Description string              `json:"description"`
	Documents   []persistedDocument `json:"documents"`
	CreatedAt   string              `json:"createdAt"`
}

type persistedDocument struct {
	ID              string `json:"id"`
	KnowledgeBaseID string `json:"knowledgeBaseId"`
	Name            string `json:"name"`
	Size            int64  `json:"size"`
	SizeLabel       string `json:"sizeLabel"`
	UploadedAt      string `json:"uploadedAt"`
	Status          string `json:"status"`
	Path            string `json:"path"`
	ContentPreview  string `json:"contentPreview"`
	ChunkCount      int    `json:"chunkCount,omitempty"`
	IndexedAt       string `json:"indexedAt,omitempty"`
	IndexError      string `json:"indexError,omitempty"`
}

func (s persistentAppState) MarshalJSON() ([]byte, error) {
	knowledgeBases := make(map[string]persistedKnowledgeBase, len(s.KnowledgeBases))
	for id, knowledgeBase := range s.KnowledgeBases {
		documents := make([]persistedDocument, len(knowledgeBase.Documents))
		for index, document := range knowledgeBase.Documents {
			documents[index] = persistedDocumentFromModel(document)
		}
		knowledgeBases[id] = persistedKnowledgeBase{
			ID:          knowledgeBase.ID,
			Name:        knowledgeBase.Name,
			Description: knowledgeBase.Description,
			Documents:   documents,
			CreatedAt:   knowledgeBase.CreatedAt,
		}
	}
	return json.Marshal(persistedAppStateJSON{
		Config:         s.Config,
		KnowledgeBases: knowledgeBases,
		EvalDatasets:   s.EvalDatasets,
		EvalRuns:       s.EvalRuns,
		Auth:           s.Auth,
	})
}

func (s *persistentAppState) UnmarshalJSON(data []byte) error {
	var raw persistedAppStateJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	s.Config = raw.Config
	s.KnowledgeBases = make(map[string]model.KnowledgeBase, len(raw.KnowledgeBases))
	for id, knowledgeBase := range raw.KnowledgeBases {
		documents := make([]model.Document, len(knowledgeBase.Documents))
		for index, document := range knowledgeBase.Documents {
			documents[index] = documentToModel(document)
		}
		s.KnowledgeBases[id] = model.KnowledgeBase{
			ID:          knowledgeBase.ID,
			Name:        knowledgeBase.Name,
			Description: knowledgeBase.Description,
			Documents:   documents,
			CreatedAt:   knowledgeBase.CreatedAt,
		}
	}
	s.EvalDatasets = raw.EvalDatasets
	s.EvalRuns = raw.EvalRuns
	s.Auth = raw.Auth
	return nil
}

func persistedDocumentFromModel(document model.Document) persistedDocument {
	return persistedDocument{
		ID:              document.ID,
		KnowledgeBaseID: document.KnowledgeBaseID,
		Name:            document.Name,
		Size:            document.Size,
		SizeLabel:       document.SizeLabel,
		UploadedAt:      document.UploadedAt,
		Status:          document.Status,
		Path:            document.Path,
		ContentPreview:  document.ContentPreview,
		ChunkCount:      document.ChunkCount,
		IndexedAt:       document.IndexedAt,
		IndexError:      document.IndexError,
	}
}

func documentToModel(document persistedDocument) model.Document {
	return model.Document{
		ID:              document.ID,
		KnowledgeBaseID: document.KnowledgeBaseID,
		Name:            document.Name,
		Size:            document.Size,
		SizeLabel:       document.SizeLabel,
		UploadedAt:      document.UploadedAt,
		Status:          document.Status,
		Path:            document.Path,
		ContentPreview:  document.ContentPreview,
		ChunkCount:      document.ChunkCount,
		IndexedAt:       document.IndexedAt,
		IndexError:      document.IndexError,
	}
}

type AppStateStore struct {
	path string
	mu   sync.Mutex
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
	if state.EvalDatasets == nil {
		state.EvalDatasets = map[string]model.EvalDataset{}
	}
	if state.EvalRuns == nil {
		state.EvalRuns = map[string]model.RunEvalDatasetResponse{}
	}
	if state.Auth.Users == nil {
		state.Auth.Users = map[string]model.AuthUser{}
	}
	if state.Auth.Sessions == nil {
		state.Auth.Sessions = map[string]model.AuthSession{}
	}
	if state.Auth.APIKeys == nil {
		state.Auth.APIKeys = map[string]model.APIKey{}
	}
	if state.Auth.AppliedPasswordResetTokens == nil {
		state.Auth.AppliedPasswordResetTokens = []string{}
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

	tempFile := s.path + ".tmp"
	if err := os.WriteFile(tempFile, content, 0o600); err != nil {
		return fmt.Errorf("write app state temp file: %w", err)
	}
	if err := os.Rename(tempFile, s.path); err != nil {
		return fmt.Errorf("replace app state file: %w", err)
	}
	return nil
}
