package service

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"testing"

	"ai-localbase/internal/model"
)

// TestWikiSemanticLintRejectsUnreadAndChangedEvidence 防止模型越过覆盖范围，或把旧结论保存到新证据版本。
func TestWikiSemanticLintRejectsUnreadAndChangedEvidence(t *testing.T) {
	for _, scenario := range []string{"unread", "page-changed", "source-changed"} {
		t.Run(scenario, func(t *testing.T) {
			var duringCall func() string
			var modelCalls atomic.Int32
			s, id, source := wikiFixture(t, func(model.ChatCompletionRequest) (string, int) {
				modelCalls.Add(1)
				return duringCall(), 200
			})
			count := 1
			if scenario == "unread" {
				count = 16
			}
			changes := []model.WikiPageChange{}
			for i := 0; i < count; i++ {
				changes = append(changes, model.WikiPageChange{Title: fmt.Sprintf("待核实 %d", i), PageType: "question", Markdown: "退款期限需要核实", SourceRefs: []model.WikiSourceRef{{DocumentID: source.ID, Checksum: source.ContentChecksum, Quote: "退款期限为七天。"}}})
			}
			ids, err := s.publishWikiPlan(id, "", model.WikiPlan{Pages: changes[:min(count, 15)]})
			if err != nil {
				t.Fatal(err)
			}
			if count > 15 {
				more, err := s.publishWikiPlan(id, "", model.WikiPlan{Pages: changes[15:]})
				if err != nil {
					t.Fatal(err)
				}
				ids = append(ids, more...)
			}
			duringCall = func() string {
				if scenario != "unread" {
					if err := s.commitWikiChange(id, func(kb *model.KnowledgeBase) error {
						for i := range kb.Documents {
							doc := &kb.Documents[i]
							if scenario == "page-changed" && doc.ID == ids[0] {
								doc.Wiki.Revision++
							}
							if scenario == "source-changed" && doc.ID == source.ID {
								doc.ContentChecksum = "changed"
							}
						}
						return nil
					}); err != nil {
						t.Error(err)
					}
				}
				body, _ := json.Marshal(map[string]any{"issues": []model.WikiIssue{{Type: "unsupported-claim", PageIDs: []string{ids[len(ids)-1]}, Reason: "需要进一步核实"}}})
				return string(body)
			}
			// 使用已登记的真实任务，确保不会在模型调用前因任务不存在而提前失败。
			job, _, err := s.StartWikiJob(id, model.WikiJobRequest{Kind: "lint", RequestKey: "semantic-check", Semantic: true})
			if err != nil {
				t.Fatal(err)
			}
			job = waitWikiJob(t, s, id, job.ID)
			expectedError := map[string]string{
				"unread":         "semantic issue references an unread page",
				"page-changed":   "semantic page changed during inspection",
				"source-changed": "semantic source changed during inspection",
			}[scenario]
			if job.Status != "partial" || job.CheckedPages != 0 || modelCalls.Load() != 1 || job.Error != expectedError {
				t.Fatalf("invalid evidence accepted: %+v", job)
			}
			result, _ := s.WikiIssues(id, "", 0, 100)
			for _, issue := range result["items"].([]model.WikiIssue) {
				if issue.Type == "unsupported-claim" {
					t.Fatal("invalid semantic result was saved")
				}
			}
		})
	}
}

// TestWikiExportUsesShanghaiTime 验证导出日志跨年时转换时区，元数据仍保留机器交换格式。
func TestWikiExportUsesShanghaiTime(t *testing.T) {
	s, id, _ := wikiFixture(t, func(model.ChatCompletionRequest) (string, int) { return `{"pages":[]}`, 200 })
	if err := s.commitWikiChange(id, func(kb *model.KnowledgeBase) error {
		kb.Wiki.Jobs = []model.WikiJob{{ID: "export-time", Status: "succeeded", UpdatedAt: "2026-12-31T16:05:06Z"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	data, err := s.ExportWiki(id)
	if err != nil {
		t.Fatal(err)
	}
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	var logContent string
	for _, entry := range archive.File {
		if entry.Name != "log.md" {
			continue
		}
		reader, err := entry.Open()
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(reader)
		_ = reader.Close()
		if err != nil {
			t.Fatal(err)
		}
		logContent = string(body)
	}
	if !strings.Contains(logContent, "2027-01-01 00:05:06") || strings.Contains(logContent, "16:05:06Z") {
		t.Fatalf("unexpected exported time: %s", logContent)
	}
}

// TestWikiLintPartialAndIssueLifecycle 验证结构结果不会因语义模型失败丢失，处理状态按版本保存。
func TestWikiLintPartialAndIssueLifecycle(t *testing.T) {
	s, id, _ := wikiFixture(t, func(model.ChatCompletionRequest) (string, int) { return "", 503 })
	ids, err := s.publishWikiPlan(id, "", model.WikiPlan{Pages: []model.WikiPageChange{{Title: "待核实", PageType: "question", Markdown: "需要更多证据"}}})
	if err != nil {
		t.Fatal(err)
	}
	job, _, err := s.StartWikiJob(id, model.WikiJobRequest{Kind: "lint", RequestKey: "lint-1", Semantic: true})
	if err != nil {
		t.Fatal(err)
	}
	job = waitWikiJob(t, s, id, job.ID)
	if job.Status != "partial" || job.SemanticStatus != "unavailable" || job.TotalPages != 1 {
		t.Fatalf("coverage: %+v", job)
	}
	result, err := s.WikiIssues(id, "open", 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	issues := result["items"].([]model.WikiIssue)
	if len(issues) != 1 || issues[0].Type != "orphan" {
		t.Fatalf("issues: %+v", issues)
	}
	if _, err = s.UpdateWikiIssue(id, issues[0].ID, "dismissed", ""); err == nil {
		t.Fatal("empty reason accepted")
	}
	if _, err = s.UpdateWikiIssue(id, issues[0].ID, "dismissed", "此页作为入口独立维护"); err != nil {
		t.Fatal(err)
	}
	job, _, err = s.StartWikiJob(id, model.WikiJobRequest{Kind: "lint", RequestKey: "lint-2"})
	if err != nil {
		t.Fatal(err)
	}
	job = waitWikiJob(t, s, id, job.ID)
	if job.Status != "succeeded" {
		t.Fatalf("structure: %+v", job)
	}
	result, _ = s.WikiIssues(id, "dismissed", 0, 20)
	if result["total"] != 1 {
		t.Fatal("same evidence reopened issue")
	}
	data, err := s.ExportWiki(id)
	if err != nil {
		t.Fatal(err)
	}
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, entry := range archive.File {
		names = append(names, entry.Name)
	}
	if len(names) != 3 || !strings.Contains(strings.Join(names, "|"), "pages/"+ids[0]+".md") {
		t.Fatalf("export: %v", names)
	}
}

// TestWikiToolContract 校验 MCP 工具发现和单库参数约束。
func TestWikiToolContract(t *testing.T) {
	s, id, _ := wikiFixture(t, func(model.ChatCompletionRequest) (string, int) { return `{"pages":[]}`, 200 })
	for _, tool := range wikiMCPTools() {
		if !strings.HasPrefix(tool.Name, "wiki.") || len(tool.Parameters) == 0 || tool.Parameters[0].Name != "knowledgeBaseId" || !tool.Parameters[0].Required {
			t.Fatalf("invalid discovery: %+v", tool)
		}
	}
	if _, _, err := s.CallWikiTool("wiki.configure", map[string]any{"knowledgeBaseId": id}); err == nil {
		t.Fatal("missing enabled accepted")
	}
	if _, _, err := s.CallWikiTool("wiki.page.list", map[string]any{}); err == nil {
		t.Fatal("empty scope accepted")
	}
	if _, _, err := s.CallWikiTool("wiki.jobs", map[string]any{"knowledgeBaseId": id, "kind": "capture", "requestKey": "bad"}); err == nil {
		t.Fatal("unverified capture accepted")
	}
	result, status, err := s.CallWikiTool("wiki.get", map[string]any{"knowledgeBaseId": id})
	if err != nil || status != 200 || result["enabled"] != true {
		t.Fatalf("get: %v %d %v", result, status, err)
	}
}
