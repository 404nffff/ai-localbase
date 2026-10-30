package service

import (
	"fmt"
	"strings"

	"ai-localbase/internal/model"
)

const (
	KnowledgeScopeNone     = "none"
	KnowledgeScopeSelected = "selected"
	KnowledgeScopeAll      = "all"
)

// ResolveKnowledgeScope makes the retrieval boundary explicit. An omitted
// scope remains backwards compatible when an older client sends an id, but it
// never turns an unscoped request into a full-database search.
func ResolveKnowledgeScope(rawScope, knowledgeBaseID, documentID string) (string, error) {
	scope := strings.ToLower(strings.TrimSpace(rawScope))
	hasKnowledgeBase := strings.TrimSpace(knowledgeBaseID) != ""
	hasDocument := strings.TrimSpace(documentID) != ""

	if scope == "" {
		if hasKnowledgeBase || hasDocument {
			return KnowledgeScopeSelected, nil
		}
		return KnowledgeScopeNone, nil
	}

	switch scope {
	case KnowledgeScopeNone:
		if hasKnowledgeBase || hasDocument {
			return "", fmt.Errorf("knowledgeScope none cannot include knowledgeBaseId or documentId")
		}
		return scope, nil
	case KnowledgeScopeSelected:
		if !hasKnowledgeBase && !hasDocument {
			return "", fmt.Errorf("knowledgeScope selected requires knowledgeBaseId or documentId")
		}
		return scope, nil
	case KnowledgeScopeAll:
		if hasKnowledgeBase || hasDocument {
			return "", fmt.Errorf("knowledgeScope all cannot include knowledgeBaseId or documentId")
		}
		return scope, nil
	default:
		return "", fmt.Errorf("invalid knowledgeScope %q; expected none, selected, or all", rawScope)
	}
}

func resolveKnowledgeScope(req model.ChatCompletionRequest) (string, error) {
	return ResolveKnowledgeScope(req.KnowledgeScope, req.KnowledgeBaseID, req.DocumentID)
}

func normalizeStoredKnowledgeScope(scope, knowledgeBaseID, documentID string) string {
	resolved, err := ResolveKnowledgeScope(scope, knowledgeBaseID, documentID)
	if err == nil {
		return resolved
	}
	if strings.TrimSpace(knowledgeBaseID) != "" || strings.TrimSpace(documentID) != "" {
		return KnowledgeScopeSelected
	}
	return KnowledgeScopeNone
}

func (s *AppService) ResolveKnowledgeScope(req model.ChatCompletionRequest) (string, error) {
	return resolveKnowledgeScope(req)
}

// NormalizeKnowledgeRequest 统一入口标识并校验库和文档归属；无范围默认普通聊天。
func (s *AppService) NormalizeKnowledgeRequest(req model.ChatCompletionRequest) (model.ChatCompletionRequest, error) {
	req.ConversationID = strings.TrimSpace(req.ConversationID)
	req.KnowledgeBaseID = strings.TrimSpace(req.KnowledgeBaseID)
	req.DocumentID = strings.TrimSpace(req.DocumentID)
	scope, err := ResolveKnowledgeScope(req.KnowledgeScope, req.KnowledgeBaseID, req.DocumentID)
	if err != nil {
		return req, err
	}
	req.KnowledgeScope = scope
	if scope == KnowledgeScopeSelected {
		if req.DocumentID != "" {
			// 旧客户端允许仅传文档 ID，先定位所属库；同时传库时必须严格校验归属。
			if req.KnowledgeBaseID == "" {
				s.state.Mu.RLock()
				for id, kb := range s.state.KnowledgeBases {
					for _, document := range kb.Documents {
						if document.ID == req.DocumentID {
							req.KnowledgeBaseID = id
						}
					}
				}
				s.state.Mu.RUnlock()
			}
			document, err := s.GetDocument(req.KnowledgeBaseID, req.DocumentID)
			if err != nil {
				return req, err
			}
			req.KnowledgeBaseID = document.KnowledgeBaseID
		} else if _, err := s.GetKnowledgeBaseDocuments(req.KnowledgeBaseID); err != nil {
			return req, err
		}
	}
	return req, nil
}
