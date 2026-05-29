package service

import (
	"strings"
	"testing"

	"ai-localbase/internal/model"
)

func TestSelectEvalChunkCandidatesPrefersUsefulChunks(t *testing.T) {
	chunks := []DocumentChunk{
		{ID: "doc-1-chunk-0", Text: "目录", Index: 0},
		{ID: "doc-1-chunk-1", Text: "AI LocalBase 支持知识库管理、文档上传、检索增强问答和聊天记录持久化。", Index: 1},
		{ID: "doc-1-chunk-2", Text: "这是一个普通说明段落，描述项目的背景信息和使用场景。", Index: 2},
	}

	selected := selectEvalChunkCandidates(chunks, 1)
	if len(selected) != 1 {
		t.Fatalf("expected 1 selected chunk, got %d", len(selected))
	}
	if selected[0].ID != "doc-1-chunk-1" {
		t.Fatalf("expected useful capability chunk, got %s", selected[0].ID)
	}
}

func TestBuildEvalCaseKeepsChunkReferenceShape(t *testing.T) {
	document := model.Document{
		ID:              "doc-1",
		KnowledgeBaseID: "kb-1",
		Name:            "项目说明.md",
	}
	chunk := DocumentChunk{
		ID:              "doc-1-chunk-3",
		KnowledgeBaseID: "kb-1",
		DocumentID:      "doc-1",
		DocumentName:    "项目说明.md",
		Text:            "AI LocalBase 提供本地优先的 RAG 问答能力，支持知识库管理、文档上传和向量检索。",
		Index:           3,
	}

	question := buildEvalQuestion(document, chunk)
	if !strings.Contains(question, "项目说明") {
		t.Fatalf("expected question to include document subject, got %q", question)
	}

	snippets := evalAnswerSnippets(chunk.Text)
	if len(snippets) == 0 {
		t.Fatal("expected at least one answer snippet")
	}
}
