package service

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"

	"ai-localbase/internal/model"
)

const (
	indexErrorDuplicate               = "duplicate_document"
	indexErrorSourceMissing           = "source_missing"
	indexErrorSourceUnreadable        = "source_unreadable"
	indexErrorVectorDimensionMismatch = "vector_dimension_mismatch"
	indexErrorFailed                  = "index_failed"
)

// DuplicateDocumentError 表示同一知识库已有相同正文，避免重复写入向量。
type DuplicateDocumentError struct {
	Existing model.Document
}

func (e *DuplicateDocumentError) Error() string {
	if e == nil || strings.TrimSpace(e.Existing.ID) == "" {
		return "document already exists in knowledge base"
	}
	return fmt.Sprintf("document already exists in knowledge base: %s", e.Existing.ID)
}

func hashDocumentContent(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

func classifyIndexErrorCode(err error) string {
	if err == nil {
		return ""
	}
	var duplicate *DuplicateDocumentError
	if errorAs(err, &duplicate) {
		return indexErrorDuplicate
	}
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "no such file") || strings.Contains(message, "file does not exist"):
		return indexErrorSourceMissing
	case strings.Contains(message, "permission denied") || strings.Contains(message, "extract document text"):
		if _, statErr := os.Stat(message); statErr != nil && os.IsNotExist(statErr) {
			return indexErrorSourceMissing
		}
		if strings.Contains(message, "no such file") {
			return indexErrorSourceMissing
		}
		if strings.Contains(message, "extract") {
			return indexErrorSourceUnreadable
		}
		return indexErrorSourceUnreadable
	case strings.Contains(message, "dimension") || strings.Contains(message, "vector size"):
		return indexErrorVectorDimensionMismatch
	default:
		return indexErrorFailed
	}
}

func errorAs(err error, target **DuplicateDocumentError) bool {
	if err == nil || target == nil {
		return false
	}
	duplicate, ok := err.(*DuplicateDocumentError)
	if !ok {
		return false
	}
	*target = duplicate
	return true
}

func (s *AppService) findDuplicateDocument(knowledgeBaseID, checksum, excludeDocumentID string) (model.Document, bool) {
	if s == nil || strings.TrimSpace(checksum) == "" {
		return model.Document{}, false
	}
	s.state.Mu.RLock()
	defer s.state.Mu.RUnlock()
	kb, ok := s.state.KnowledgeBases[knowledgeBaseID]
	if !ok {
		return model.Document{}, false
	}
	for _, document := range kb.Documents {
		if document.ID == excludeDocumentID {
			continue
		}
		if document.ContentChecksum == checksum {
			return document, true
		}
	}
	return model.Document{}, false
}
