package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ai-localbase/internal/model"

	"github.com/xuri/excelize/v2"
)

func TestTryBuildStructuredDataAnswerCountAndPreview(t *testing.T) {
	service := newStructuredAnswerTestService(t)

	countContent, sources, ok, err := service.TryBuildStructuredDataAnswer(model.ChatCompletionRequest{
		DocumentID: "doc-users",
		Messages:   []model.ChatMessage{{Role: "user", Content: "这个文档有几名员工"}},
	})
	if err != nil || !ok {
		t.Fatalf("expected structured count answer, ok=%v err=%v", ok, err)
	}
	if !strings.Contains(countContent, "**总记录数**：3 条") {
		t.Fatalf("unexpected count answer: %q", countContent)
	}
	if len(sources) != 1 || sources[0]["sourceType"] != "structured-data" {
		t.Fatalf("unexpected structured sources: %#v", sources)
	}

	previewContent, _, ok, err := service.TryBuildStructuredDataAnswer(model.ChatCompletionRequest{
		DocumentID: "doc-users",
		Messages:   []model.ChatMessage{{Role: "user", Content: "展示当前文档的数据表格"}},
	})
	if err != nil || !ok {
		t.Fatalf("expected structured preview answer, ok=%v err=%v", ok, err)
	}
	if !strings.Contains(previewContent, "|姓名|城市|薪资|年龄|") || !strings.Contains(previewContent, "|张三|上海|24000|45|") {
		t.Fatalf("unexpected preview answer: %q", previewContent)
	}
}

func TestTryBuildStructuredDataAnswerFilter(t *testing.T) {
	service := newStructuredAnswerTestService(t)
	content, _, ok, err := service.TryBuildStructuredDataAnswer(model.ChatCompletionRequest{
		DocumentID: "doc-users",
		Messages:   []model.ChatMessage{{Role: "user", Content: "筛选城市是上海的数据"}},
	})
	if err != nil || !ok {
		t.Fatalf("expected structured filter answer, ok=%v err=%v", ok, err)
	}
	if !strings.Contains(content, "**总数**：2 条") || !strings.Contains(content, "|王五|上海|7000|25|") {
		t.Fatalf("unexpected filter answer: %q", content)
	}
	if strings.Contains(content, "|李四|北京|18000|30|") {
		t.Fatalf("unexpected non-matching row: %q", content)
	}
}

func TestTryBuildStructuredDataAnswerExtremumAverageAndGroup(t *testing.T) {
	service := newStructuredAnswerTestService(t)
	tests := []struct {
		question string
		want     []string
	}{
		{question: "薪资最高的是谁", want: []string{"**数值**：24000", "|张三|上海|24000|45|"}},
		{question: "薪资最低的是谁", want: []string{"**数值**：7000", "|王五|上海|7000|25|"}},
		{question: "平均薪资是多少", want: []string{"**有效记录数**：3 条", "**平均值**：16333.33"}},
		{question: "按城市统计分布", want: []string{"|上海|2|", "|北京|1|"}},
	}
	for _, test := range tests {
		content, _, ok, err := service.TryBuildStructuredDataAnswer(model.ChatCompletionRequest{
			DocumentID: "doc-users",
			Messages:   []model.ChatMessage{{Role: "user", Content: test.question}},
		})
		if err != nil || !ok {
			t.Fatalf("expected structured answer for %q, ok=%v err=%v", test.question, ok, err)
		}
		for _, expected := range test.want {
			if !strings.Contains(content, expected) {
				t.Fatalf("expected %q in answer for %q, got %q", expected, test.question, content)
			}
		}
	}
}

func TestTryBuildStructuredDataAnswerAcrossKnowledgeBaseDocuments(t *testing.T) {
	service := newStructuredAnswerTestService(t)
	dir := filepath.Dir(service.state.KnowledgeBases["kb-1"].Documents[0].Path)
	secondPath := filepath.Join(dir, "more-users.csv")
	if err := os.WriteFile(secondPath, []byte("姓名,城市,薪资,年龄\n赵六,深圳,32000,36\n"), 0o644); err != nil {
		t.Fatalf("write second csv fixture: %v", err)
	}

	kb := service.state.KnowledgeBases["kb-1"]
	kb.Documents = append(kb.Documents, model.Document{
		ID:              "doc-more-users",
		KnowledgeBaseID: "kb-1",
		Name:            "more-users.csv",
		Path:            secondPath,
	})
	service.state.KnowledgeBases["kb-1"] = kb

	content, sources, ok, err := service.TryBuildStructuredDataAnswer(model.ChatCompletionRequest{
		KnowledgeBaseID: "kb-1",
		Messages:        []model.ChatMessage{{Role: "user", Content: "谁的工资最高"}},
	})
	if err != nil || !ok {
		t.Fatalf("expected knowledge base structured answer, ok=%v err=%v", ok, err)
	}
	if !strings.Contains(content, "**数值**：32000") || !strings.Contains(content, "|赵六|深圳|32000|36|") {
		t.Fatalf("unexpected cross-document answer: %q", content)
	}
	if len(sources) != 2 {
		t.Fatalf("expected two structured sources, got %#v", sources)
	}

	if _, _, ok, err := service.TryBuildStructuredDataAnswer(model.ChatCompletionRequest{
		Messages: []model.ChatMessage{{Role: "user", Content: "谁的工资最高"}},
	}); err != nil || ok {
		t.Fatalf("expected unscoped multi-document query to fall through, ok=%v err=%v", ok, err)
	}
}

func TestTryBuildStructuredDataAnswerSupportsXLSX(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sales.xlsx")
	workbook := excelize.NewFile()
	sheet := workbook.GetSheetName(0)
	if err := workbook.SetSheetRow(sheet, "A1", &[]any{"产品", "金额"}); err != nil {
		t.Fatalf("write xlsx headers: %v", err)
	}
	if err := workbook.SetSheetRow(sheet, "A2", &[]any{"A", 10}); err != nil {
		t.Fatalf("write xlsx row A: %v", err)
	}
	if err := workbook.SetSheetRow(sheet, "A3", &[]any{"B", 20}); err != nil {
		t.Fatalf("write xlsx row B: %v", err)
	}
	if err := workbook.SaveAs(path); err != nil {
		t.Fatalf("save xlsx fixture: %v", err)
	}
	if err := workbook.Close(); err != nil {
		t.Fatalf("close xlsx fixture: %v", err)
	}

	service := &AppService{state: &model.AppState{KnowledgeBases: map[string]model.KnowledgeBase{
		"kb-xlsx": {
			ID: "kb-xlsx",
			Documents: []model.Document{{
				ID:              "doc-sales",
				KnowledgeBaseID: "kb-xlsx",
				Name:            "sales.xlsx",
				Path:            path,
			}},
		},
	}}}
	content, _, ok, err := service.TryBuildStructuredDataAnswer(model.ChatCompletionRequest{
		DocumentID: "doc-sales",
		Messages:   []model.ChatMessage{{Role: "user", Content: "平均金额是多少"}},
	})
	if err != nil || !ok {
		t.Fatalf("expected xlsx structured answer, ok=%v err=%v", ok, err)
	}
	if !strings.Contains(content, "**平均值**：15") {
		t.Fatalf("unexpected xlsx average answer: %q", content)
	}
}

func TestTryBuildStructuredDataAnswerFallsThroughOrReturnsParseError(t *testing.T) {
	service := newStructuredAnswerTestService(t)
	if _, _, ok, err := service.TryBuildStructuredDataAnswer(model.ChatCompletionRequest{
		DocumentID: "doc-users",
		Messages:   []model.ChatMessage{{Role: "user", Content: "请总结这个文件"}},
	}); err != nil || ok {
		t.Fatalf("expected ambiguous question to fall through, ok=%v err=%v", ok, err)
	}

	invalidPath := filepath.Join(t.TempDir(), "invalid.csv")
	if err := os.WriteFile(invalidPath, []byte("姓名,城市\n\"未闭合,上海\n"), 0o644); err != nil {
		t.Fatalf("write invalid csv fixture: %v", err)
	}
	service.state.KnowledgeBases["kb-1"] = model.KnowledgeBase{
		ID: "kb-1",
		Documents: []model.Document{{
			ID:              "doc-invalid",
			KnowledgeBaseID: "kb-1",
			Name:            "invalid.csv",
			Path:            invalidPath,
		}},
	}
	if _, _, ok, err := service.TryBuildStructuredDataAnswer(model.ChatCompletionRequest{
		DocumentID: "doc-invalid",
		Messages:   []model.ChatMessage{{Role: "user", Content: "展示表格数据"}},
	}); err == nil || !ok {
		t.Fatalf("expected structured parse error with handled intent, ok=%v err=%v", ok, err)
	}
}

func newStructuredAnswerTestService(t *testing.T) *AppService {
	t.Helper()
	path := filepath.Join(t.TempDir(), "users.csv")
	content := strings.Join([]string{
		"姓名,城市,薪资,年龄",
		"张三,上海,24000,45",
		"李四,北京,18000,30",
		"王五,上海,7000,25",
	}, "\n")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write csv fixture: %v", err)
	}

	return &AppService{state: &model.AppState{KnowledgeBases: map[string]model.KnowledgeBase{
		"kb-1": {
			ID:   "kb-1",
			Name: "测试知识库",
			Documents: []model.Document{{
				ID:              "doc-users",
				KnowledgeBaseID: "kb-1",
				Name:            "users.csv",
				Path:            path,
			}},
		},
	}}}
}
