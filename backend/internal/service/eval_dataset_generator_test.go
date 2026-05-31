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

func TestSelectEvalChunkCandidatesKeepsStructuredSummaryPriority(t *testing.T) {
	chunks := []DocumentChunk{
		{ID: "doc-1-chunk-0", Text: "第2行：姓名：张三。薪资：24000。第3行：姓名：李四。薪资：18000。", Index: 0, Kind: "structured_row"},
		{ID: "doc-1-summary-0", Text: "统计摘要：文件《工作簿1.csv》共有2条数据记录。\n统计摘要：字段“薪资”为数值列，非空值2个，最小值18000.00，最大值24000.00，平均值21000.00。", Index: 1, Kind: "structured_summary"},
	}

	selected := selectEvalChunkCandidates(chunks, len(chunks))
	if len(selected) != 2 {
		t.Fatalf("expected 2 selected chunks, got %d", len(selected))
	}
	if selected[0].Kind != "structured_summary" {
		t.Fatalf("expected structured summary first, got %s", selected[0].Kind)
	}
}

func TestBuildStructuredSummaryEvalCasesAreGrounded(t *testing.T) {
	document := model.Document{
		ID:              "doc-1",
		KnowledgeBaseID: "kb-1",
		Name:            "工作簿1.csv",
	}
	chunk := DocumentChunk{
		ID:              "doc-1-summary-0",
		KnowledgeBaseID: "kb-1",
		DocumentID:      "doc-1",
		DocumentName:    "工作簿1.csv",
		Text: strings.Join([]string{
			"统计摘要：文件《工作簿1.csv》共有4条数据记录。",
			"统计摘要：字段“薪资”为数值列，非空值4个，最小值7000.00，最大值24000.00，平均值14250.00。",
			"统计摘要：字段“性别”为类别列，共4个非空值，主要分布为：女(2)、男(2)。",
		}, "\n"),
		Index: 0,
		Kind:  "structured_summary",
	}

	cases := buildEvalCasesFromChunk(document, chunk, 10)
	if len(cases) < 5 {
		t.Fatalf("expected structured summary cases, got %d", len(cases))
	}
	for _, item := range cases {
		if !validateEvalCase(item, document.Name, chunk.Text) {
			t.Fatalf("expected grounded eval case, got %#v", item)
		}
		if strings.Contains(item.Question, "主要讲了什么") || strings.Contains(item.Question, "包括哪些要点") {
			t.Fatalf("expected specific question, got %q", item.Question)
		}
	}

	var foundMax bool
	for _, item := range cases {
		if strings.Contains(item.Question, "最大值") && strings.Contains(item.Answer, "24000.00") {
			foundMax = true
			break
		}
	}
	if !foundMax {
		t.Fatalf("expected max value eval case, got %#v", cases)
	}
}

func TestBuildStructuredRowEvalCasesAnswerExactField(t *testing.T) {
	document := model.Document{
		ID:              "doc-1",
		KnowledgeBaseID: "kb-1",
		Name:            "工作簿1.csv",
	}
	chunk := DocumentChunk{
		ID:              "doc-1-chunk-0",
		KnowledgeBaseID: "kb-1",
		DocumentID:      "doc-1",
		DocumentName:    "工作簿1.csv",
		Text:            "第2行：姓名：张三。性别：男。职称：高级职称。教师编号：111222333111。年龄：45。手机号：15911110011。薪资：24000。教龄：20。",
		Index:           0,
		Kind:            "structured_row",
	}

	cases := buildEvalCasesFromChunk(document, chunk, 5)
	if len(cases) == 0 {
		t.Fatal("expected structured row eval cases")
	}
	for _, item := range cases {
		if !validateEvalCase(item, document.Name, chunk.Text) {
			t.Fatalf("expected grounded row eval case, got %#v", item)
		}
	}
	if cases[0].Question != "《工作簿1.csv》第2行的“姓名”是什么？" {
		t.Fatalf("unexpected first row question: %q", cases[0].Question)
	}
	if cases[0].Answer != "张三" {
		t.Fatalf("expected exact field answer, got %q", cases[0].Answer)
	}
}

func TestBuildEvalCasesSkipsUnstructuredPlainText(t *testing.T) {
	document := model.Document{
		ID:              "doc-1",
		KnowledgeBaseID: "kb-1",
		Name:            "随笔.md",
	}
	chunk := DocumentChunk{
		ID:              "doc-1-chunk-0",
		KnowledgeBaseID: "kb-1",
		DocumentID:      "doc-1",
		DocumentName:    "随笔.md",
		Text:            "这是一段没有标题和明确字段的普通说明文字，只提供零散背景，不适合作为自动评估集的可靠来源。",
		Index:           0,
		Kind:            "text",
	}

	cases := buildEvalCasesFromChunk(document, chunk, 5)
	if len(cases) != 0 {
		t.Fatalf("expected no low-confidence cases, got %#v", cases)
	}
}
