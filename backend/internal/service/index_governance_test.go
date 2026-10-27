package service

import (
	"testing"

	"ai-localbase/internal/model"
)

func TestFindDuplicateDocumentUsesChecksum(t *testing.T) {
	service := NewAppService(nil, NewAppStateStore(""), nil, model.ServerConfig{
		UploadDir:  t.TempDir(),
		StagingDir: t.TempDir(),
	})
	kb, err := service.CreateKnowledgeBase(model.KnowledgeBaseInput{Name: "dup-kb"})
	if err != nil {
		t.Fatalf("create knowledge base: %v", err)
	}
	checksum := hashDocumentContent("same-body")
	added := service.AddDocument(kb.ID, model.Document{
		ID:              "doc-1",
		KnowledgeBaseID: kb.ID,
		Name:            "one.md",
		ContentChecksum: checksum,
	})
	if added.ID != "doc-1" {
		t.Fatalf("unexpected added document: %#v", added)
	}

	existing, ok := service.findDuplicateDocument(kb.ID, checksum, "doc-2")
	if !ok || existing.ID != "doc-1" {
		t.Fatalf("expected duplicate of doc-1, got ok=%v %#v", ok, existing)
	}
	if _, self := service.findDuplicateDocument(kb.ID, checksum, "doc-1"); self {
		t.Fatal("reindex of the same document should not count as duplicate")
	}
	if classifyIndexErrorCode(&DuplicateDocumentError{Existing: existing}) != indexErrorDuplicate {
		t.Fatal("expected duplicate error code")
	}
}
