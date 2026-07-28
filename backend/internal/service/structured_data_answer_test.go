package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ai-localbase/internal/model"
)

func TestQueryStructuredDataPreview(t *testing.T) {
	service := newStructuredQueryTestService(t)
	result, sources, ok, err := service.QueryStructuredData(model.ChatCompletionRequest{
		DocumentID: "doc-users",
		Messages:   []model.ChatMessage{{Role: "user", Content: "展示当前文档的数据表格"}},
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !ok {
		t.Fatal("expected structured data result")
	}
	if len(sources) != 1 || sources[0]["sourceType"] != "structured-data" {
		t.Fatalf("expected structured source metadata, got %#v", sources)
	}
	if result.Intent != "preview" || result.TotalRows != 3 || result.MatchedRows != 3 {
		t.Fatalf("unexpected preview metadata: %#v", result)
	}
	if len(result.Rows) != 3 || result.Rows[0].Values["姓名"] != "张三" || result.Rows[0].Values["薪资"] != "24000" {
		t.Fatalf("expected structured row data, got %#v", result.Rows)
	}
}

func TestLooksLikeStructuredDataQueryRequiresTableSignal(t *testing.T) {
	if looksLikeStructuredDataQuery("列出主要角色") {
		t.Fatal("expected ordinary document list question not to trigger structured data handling")
	}
	if !looksLikeStructuredDataQuery("列出表格中的所有记录") {
		t.Fatal("expected explicit table record question to trigger structured data handling")
	}
}

func TestQueryStructuredDataFilter(t *testing.T) {
	service := newStructuredQueryTestService(t)
	result, _, ok, err := service.QueryStructuredData(model.ChatCompletionRequest{
		DocumentID: "doc-users",
		Messages:   []model.ChatMessage{{Role: "user", Content: "筛选城市是上海的数据"}},
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !ok {
		t.Fatal("expected structured data result")
	}
	if result.Intent != "filter" || result.FilterField != "城市" || result.FilterValue != "上海" || result.MatchedRows != 2 {
		t.Fatalf("unexpected filter metadata: %#v", result)
	}
	if len(result.Rows) != 2 || result.Rows[0].Values["姓名"] != "张三" || result.Rows[1].Values["姓名"] != "王五" {
		t.Fatalf("expected shanghai rows, got %#v", result.Rows)
	}
	for _, row := range result.Rows {
		if row.Values["城市"] != "上海" {
			t.Fatalf("did not expect non-shanghai row, got %#v", row)
		}
	}
}

func TestQueryStructuredDataMaxAverageAndGroup(t *testing.T) {
	service := newStructuredQueryTestService(t)

	maxResult, _, ok, err := service.QueryStructuredData(model.ChatCompletionRequest{
		DocumentID: "doc-users",
		Messages:   []model.ChatMessage{{Role: "user", Content: "薪资最高的是谁"}},
	})
	if err != nil || !ok {
		t.Fatalf("expected max result, ok=%v err=%v", ok, err)
	}
	if maxResult.Aggregate == nil || maxResult.Aggregate.Operation != "max" || maxResult.Aggregate.Value != 24000 || len(maxResult.Rows) != 1 || maxResult.Rows[0].Values["姓名"] != "张三" {
		t.Fatalf("unexpected max result: %#v", maxResult)
	}

	avgResult, _, ok, err := service.QueryStructuredData(model.ChatCompletionRequest{
		DocumentID: "doc-users",
		Messages:   []model.ChatMessage{{Role: "user", Content: "平均薪资是多少"}},
	})
	if err != nil || !ok {
		t.Fatalf("expected average result, ok=%v err=%v", ok, err)
	}
	if avgResult.Aggregate == nil || avgResult.Aggregate.Operation != "average" || avgResult.Aggregate.Value < 16333.3 || avgResult.Aggregate.Value > 16333.4 {
		t.Fatalf("unexpected average result: %#v", avgResult)
	}

	groupResult, _, ok, err := service.QueryStructuredData(model.ChatCompletionRequest{
		DocumentID: "doc-users",
		Messages:   []model.ChatMessage{{Role: "user", Content: "按城市统计分布"}},
	})
	if err != nil || !ok {
		t.Fatalf("expected group result, ok=%v err=%v", ok, err)
	}
	if len(groupResult.Groups) != 2 || groupResult.Groups[0].Value != "上海" || groupResult.Groups[0].Count != 2 || groupResult.Groups[1].Value != "北京" || groupResult.Groups[1].Count != 1 {
		t.Fatalf("unexpected group result: %#v", groupResult)
	}
}

func TestQueryStructuredDataAcrossKnowledgeBaseTables(t *testing.T) {
	service := newStructuredQueryTestService(t)
	dir := filepath.Dir(service.state.KnowledgeBases["kb-1"].Documents[0].Path)
	morePath := filepath.Join(dir, "more_users.csv")
	content := strings.Join([]string{
		"姓名,城市,薪资,年龄",
		"赵六,深圳,32000,36",
	}, "\n")
	if err := os.WriteFile(morePath, []byte(content), 0o644); err != nil {
		t.Fatalf("write second csv fixture: %v", err)
	}

	kb := service.state.KnowledgeBases["kb-1"]
	kb.Documents = append(kb.Documents, model.Document{
		ID:              "doc-more-users",
		KnowledgeBaseID: "kb-1",
		Name:            "more_users.csv",
		Path:            morePath,
	})
	service.state.KnowledgeBases["kb-1"] = kb

	result, _, ok, err := service.QueryStructuredData(model.ChatCompletionRequest{
		KnowledgeBaseID: "kb-1",
		Messages:        []model.ChatMessage{{Role: "user", Content: "谁的工资最高"}},
	})
	if err != nil || !ok {
		t.Fatalf("expected knowledge-base structured result, ok=%v err=%v", ok, err)
	}
	if result.Aggregate == nil || result.Aggregate.Value != 32000 || len(result.Rows) != 1 || result.Rows[0].Values["姓名"] != "赵六" {
		t.Fatalf("expected highest salary across structured documents, got %#v", result)
	}
}

func TestBuildRetrievalContextDoesNotInjectStructuredAnswer(t *testing.T) {
	service := newStructuredQueryTestService(t)
	service.rag = NewRagService()
	service.queryRewriter = NewLLMQueryRewriter(nil, 3)
	enableQueryRewrite := true
	contextText, sources, err := service.BuildRetrievalContext(model.ChatCompletionRequest{
		DocumentID:         "doc-users",
		EnableQueryRewrite: &enableQueryRewrite,
		Messages:           []model.ChatMessage{{Role: "user", Content: "薪资最高的是谁"}},
	})
	if err != nil {
		t.Fatalf("build retrieval context: %v", err)
	}
	if contextText != "" || len(sources) != 0 {
		t.Fatalf("expected no backend-generated answer without retrieved chunks, context=%q sources=%#v", contextText, sources)
	}
}

func TestBuildRetrievalDebugEvalCandidateFromLowConfidence(t *testing.T) {
	candidate := buildRetrievalDebugEvalCandidate(
		model.ChatCompletionRequest{KnowledgeBaseID: "kb-1"},
		"教师薪资最高是谁",
		true,
		[]RetrievedChunk{{
			DocumentChunk: DocumentChunk{
				ID:              "doc-users-source-rows-0",
				KnowledgeBaseID: "kb-1",
				DocumentID:      "doc-users",
				DocumentName:    "users.csv",
				Text:            "第2行：姓名：张三。薪资：24000。",
			},
			Score: 0.12,
		}},
		"[users.csv#1] 第2行：姓名：张三。薪资：24000。",
	)
	if candidate == nil {
		t.Fatal("expected eval candidate")
	}
	if candidate.Question != "教师薪资最高是谁" {
		t.Fatalf("unexpected question: %q", candidate.Question)
	}
	if candidate.AnswerType != "retrieval-debug-candidate" || candidate.Difficulty != "hard" {
		t.Fatalf("unexpected eval metadata: %#v", candidate)
	}
	if len(candidate.SourceDocuments) != 1 || candidate.SourceDocuments[0].ChunkID != "doc-users-source-rows-0" {
		t.Fatalf("unexpected sources: %#v", candidate.SourceDocuments)
	}
}

func newStructuredQueryTestService(t *testing.T) *AppService {
	t.Helper()
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "users.csv")
	content := strings.Join([]string{
		"姓名,城市,薪资,年龄",
		"张三,上海,24000,45",
		"李四,北京,18000,30",
		"王五,上海,7000,25",
	}, "\n")
	if err := os.WriteFile(csvPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write csv fixture: %v", err)
	}

	return &AppService{
		state: &model.AppState{
			KnowledgeBases: map[string]model.KnowledgeBase{
				"kb-1": {
					ID:   "kb-1",
					Name: "测试知识库",
					Documents: []model.Document{{
						ID:              "doc-users",
						KnowledgeBaseID: "kb-1",
						Name:            "users.csv",
						Path:            csvPath,
					}},
				},
			},
		},
	}
}
