package service

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"ai-localbase/internal/model"
	"ai-localbase/internal/util"
)

const (
	defaultRebuildKnowledgeBaseID   = "kb-0"
	defaultRebuildKnowledgeBaseName = "初始知识库"
)

// ErrRebuildConfirmationRequired 表示恢复接口缺少显式确认，调用方必须主动传 confirm=true。
var ErrRebuildConfirmationRequired = fmt.Errorf("rebuild qdrant index requires confirm=true")

// RebuildQdrantIndexRequest 描述从 uploads 重建 Qdrant 索引的受控请求。
type RebuildQdrantIndexRequest struct {
	Confirm           bool
	KnowledgeBaseID   string
	KnowledgeBaseName string
	IncludeArchives   bool
}

// RebuildQdrantIndexResult 汇总恢复结果，供 API 和脚本输出核对。
type RebuildQdrantIndexResult struct {
	KnowledgeBase      model.KnowledgeBase  `json:"knowledgeBase"`
	IndexedDocuments   int                  `json:"indexedDocuments"`
	SkippedFiles       []RebuildSkippedFile `json:"skippedFiles"`
	DeletedCollections []string             `json:"deletedCollections"`
	StateBackupPath    string               `json:"stateBackupPath,omitempty"`
	UploadDir          string               `json:"uploadDir"`
}

// RebuildSkippedFile 记录恢复过程中未导入的文件和原因。
type RebuildSkippedFile struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// RebuildQdrantIndexFromUploads 清空本应用拥有的 Qdrant collections，并从 uploads 目录重建 kb-0。
func (s *AppService) RebuildQdrantIndexFromUploads(ctx context.Context, req RebuildQdrantIndexRequest) (RebuildQdrantIndexResult, error) {
	if !req.Confirm {
		return RebuildQdrantIndexResult{}, ErrRebuildConfirmationRequired
	}
	if s == nil {
		return RebuildQdrantIndexResult{}, fmt.Errorf("app service is nil")
	}
	if s.qdrant == nil || !s.qdrant.IsEnabled() {
		return RebuildQdrantIndexResult{}, fmt.Errorf("qdrant is not enabled")
	}

	kbID := strings.TrimSpace(req.KnowledgeBaseID)
	if kbID == "" {
		kbID = defaultRebuildKnowledgeBaseID
	}
	kbName := strings.TrimSpace(req.KnowledgeBaseName)
	if kbName == "" {
		kbName = defaultRebuildKnowledgeBaseName
	}

	uploadDir := strings.TrimSpace(s.serverConfig.UploadDir)
	if uploadDir == "" {
		return RebuildQdrantIndexResult{}, fmt.Errorf("upload dir is empty")
	}

	backupPath, err := s.backupStateFile()
	if err != nil {
		return RebuildQdrantIndexResult{}, err
	}

	deletedCollections, err := s.deleteOwnedQdrantCollections(ctx)
	if err != nil {
		return RebuildQdrantIndexResult{}, err
	}
	if err := s.qdrant.EnsureCollection(ctx, kbID); err != nil {
		return RebuildQdrantIndexResult{}, fmt.Errorf("ensure rebuild collection %s: %w", kbID, err)
	}

	files, err := discoverUploadFiles(uploadDir, req.IncludeArchives)
	if err != nil {
		return RebuildQdrantIndexResult{}, err
	}

	documents, skipped := s.rebuildDocumentsFromFiles(ctx, kbID, files)
	knowledgeBase := model.KnowledgeBase{
		ID:          kbID,
		Name:        kbName,
		Description: "从 backend/data/uploads 重建的初始知识库",
		Documents:   documents,
		CreatedAt:   time.Now().UTC().Format(time.RFC3339),
	}

	s.state.Mu.Lock()
	s.state.KnowledgeBases = map[string]model.KnowledgeBase{kbID: knowledgeBase}
	s.state.Mu.Unlock()
	syncIDCounterFromState(s.state)

	if err := s.saveState(); err != nil {
		return RebuildQdrantIndexResult{}, fmt.Errorf("save rebuilt app state: %w", err)
	}

	return RebuildQdrantIndexResult{
		KnowledgeBase:      knowledgeBase,
		IndexedDocuments:   len(documents),
		SkippedFiles:       skipped,
		DeletedCollections: deletedCollections,
		StateBackupPath:    backupPath,
		UploadDir:          uploadDir,
	}, nil
}

func (s *AppService) rebuildDocumentsFromFiles(ctx context.Context, kbID string, files []string) ([]model.Document, []RebuildSkippedFile) {
	documents := make([]model.Document, 0, len(files))
	skipped := make([]RebuildSkippedFile, 0)

	for _, path := range files {
		document, skip, ok := s.rebuildDocumentFromFile(ctx, kbID, len(documents)+1, path)
		if !ok {
			skipped = append(skipped, skip)
			continue
		}
		documents = append(documents, document)
	}

	return documents, skipped
}

func (s *AppService) rebuildDocumentFromFile(ctx context.Context, kbID string, sequence int, path string) (model.Document, RebuildSkippedFile, bool) {
	info, err := os.Stat(path)
	if err != nil {
		return model.Document{}, RebuildSkippedFile{Path: path, Reason: "stat file: " + err.Error()}, false
	}
	if info.IsDir() {
		return model.Document{}, RebuildSkippedFile{Path: path, Reason: "is directory"}, false
	}

	content, err := util.ExtractDocumentText(path)
	if err != nil {
		return model.Document{}, RebuildSkippedFile{Path: path, Reason: "extract text: " + err.Error()}, false
	}

	document := model.Document{
		ID:              fmt.Sprintf("doc-%d", sequence),
		KnowledgeBaseID: kbID,
		Name:            originalUploadName(path),
		Size:            info.Size(),
		SizeLabel:       util.FormatFileSize(info.Size()),
		UploadedAt:      time.Now().UTC().Format(time.RFC3339),
		Status:          "processing",
		Path:            path,
		ContentPreview:  util.BuildContentPreviewFromText(content),
	}

	markdownPath, err := s.writeMarkdownArchive(document, content)
	if err != nil {
		return model.Document{}, RebuildSkippedFile{Path: path, Reason: err.Error()}, false
	}
	document.MarkdownPath = markdownPath

	chunks := s.rag.BuildDocumentChunks(document, content)
	if len(chunks) == 0 {
		document.Status = "ready"
		return document, RebuildSkippedFile{}, true
	}

	vectors, err := s.rag.EmbedTexts(ctx, s.currentEmbeddingConfig(), chunkTexts(chunks), s.qdrantVectorSize())
	if err != nil {
		_ = os.Remove(markdownPath)
		return model.Document{}, RebuildSkippedFile{Path: path, Reason: "embed chunks: " + err.Error()}, false
	}
	if err := s.upsertDocumentChunks(kbID, chunks, vectors); err != nil {
		_ = os.Remove(markdownPath)
		return model.Document{}, RebuildSkippedFile{Path: path, Reason: err.Error()}, false
	}

	document.Status = "indexed"
	document.ContentPreview = previewFromChunks(chunks)
	return document, RebuildSkippedFile{}, true
}

func (s *AppService) deleteOwnedQdrantCollections(ctx context.Context) ([]string, error) {
	collections, err := s.qdrant.ListCollections(ctx)
	if err != nil {
		return nil, fmt.Errorf("list qdrant collections: %w", err)
	}

	prefix := strings.TrimSpace(s.serverConfig.QdrantCollectionPrefix)
	targets := make([]string, 0, len(collections))
	for _, collection := range collections {
		if prefix == "" || strings.HasPrefix(collection, prefix) {
			targets = append(targets, collection)
		}
	}
	sort.Strings(targets)

	for _, collection := range targets {
		if err := s.qdrant.DeleteCollectionByName(ctx, collection); err != nil {
			return targets, fmt.Errorf("delete qdrant collection %s: %w", collection, err)
		}
	}
	return targets, nil
}

func (s *AppService) backupStateFile() (string, error) {
	if s == nil || s.store == nil || strings.TrimSpace(s.store.Path()) == "" {
		return "", nil
	}

	sourcePath := s.store.Path()
	content, err := os.ReadFile(sourcePath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("read app state for backup: %w", err)
	}

	backupPath := fmt.Sprintf("%s.bak-%s", sourcePath, time.Now().UTC().Format("20060102150405"))
	if err := os.WriteFile(backupPath, content, 0o600); err != nil {
		return "", fmt.Errorf("write app state backup: %w", err)
	}
	return backupPath, nil
}

func discoverUploadFiles(root string, includeArchives bool) ([]string, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, fmt.Errorf("upload dir is empty")
	}

	files := make([]string, 0)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if !includeArchives && path != root && isMarkdownArchivePath(root, path) {
				return filepath.SkipDir
			}
			return nil
		}
		if !isSupportedRebuildFile(path) {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan upload dir: %w", err)
	}
	sort.Strings(files)
	return files, nil
}

func isMarkdownArchivePath(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	parts := strings.Split(filepath.ToSlash(relative), "/")
	return len(parts) > 0 && parts[0] == "md"
}

func isSupportedRebuildFile(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".txt", ".md", ".pdf", ".csv", ".xlsx":
		return true
	default:
		return false
	}
}

func originalUploadName(path string) string {
	name := filepath.Base(path)
	underscore := strings.Index(name, "_")
	if underscore <= 0 || underscore == len(name)-1 {
		return name
	}
	prefix := name[:underscore]
	for _, r := range prefix {
		if r < '0' || r > '9' {
			return name
		}
	}
	return name[underscore+1:]
}
