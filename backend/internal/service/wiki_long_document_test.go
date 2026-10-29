package service

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"ai-localbase/internal/model"
)

// TestWikiLongDocumentExpandsCallBudget 通过真实整理流程验证超过32段后继续执行，累计调用不清零。
func TestWikiLongDocumentExpandsCallBudget(t *testing.T) {
	var calls atomic.Int32
	s, id, doc := wikiFixture(t, func(req model.ChatCompletionRequest) (string, int) {
		calls.Add(1)
		if strings.HasPrefix(req.Messages[0].Content, "提取") {
			return `[{"claim":"长文事实","quote":"甲"}]`, 200
		}
		var payload struct {
			Sources []wikiSourceInput `json:"sources"`
		}
		_ = json.Unmarshal([]byte(req.Messages[1].Content), &payload)
		source := payload.Sources[0]
		plan := model.WikiPlan{Pages: []model.WikiPageChange{{Key: "summary-1", Title: "长文摘要", PageType: "topic", Markdown: "全部来源分段已处理。", SourceRefs: []model.WikiSourceRef{{DocumentID: source.DocumentID, Checksum: source.Checksum, Quote: "甲"}}}}}
		data, _ := json.Marshal(plan)
		return string(data), 200
	})
	if err := os.WriteFile(doc.Path, []byte(strings.Repeat("甲", 140000)), 0600); err != nil {
		t.Fatal(err)
	}
	job, _, err := s.StartWikiJob(id, model.WikiJobRequest{DocumentIDs: []string{doc.ID}, RequestKey: "long-budget"})
	if err != nil {
		t.Fatal(err)
	}
	job = waitWikiJob(t, s, id, job.ID)
	if job.Status != "succeeded" || job.TotalSegments != 35 || job.ProcessedSegments != 35 || job.ModelCalls != 36 || calls.Load() != 36 || job.ModelCallLimit <= 36 {
		t.Fatalf("long source stopped or counters reset: %+v calls=%d", job, calls.Load())
	}
}

// TestWikiSegmentsKeepNegationWithConclusion 验证4000字符处否定句保持完整，跨12000字符后原文仍无遗漏。
func TestWikiSegmentsKeepNegationWithConclusion(t *testing.T) {
	body := strings.Repeat("甲", 3980) + "。观察记录只核对既有归档流程，不增加新决策。\n" + strings.Repeat("后续完整记录。", 1800)
	segments := wikiSourceSegments("source", "version", []rune(body), nil)
	var joined strings.Builder
	for _, segment := range segments {
		joined.WriteString(segment.text)
		if strings.Contains(segment.text, "新决策") && !strings.Contains(segment.text, "不增加新决策") {
			t.Fatalf("negation split from conclusion: %s", segment.key)
		}
	}
	if joined.String() != body || len(segments) < 4 {
		t.Fatal("source coverage was changed")
	}
}

// TestWikiOversizedBaselineCreatesLinkedTopic 验证超大旧页不被截断或覆盖，新资料自动形成关联专题。
func TestWikiOversizedBaselineCreatesLinkedTopic(t *testing.T) {
	s, id, doc := wikiFixture(t, func(req model.ChatCompletionRequest) (string, int) {
		var payload struct {
			Sources   []wikiSourceInput         `json:"sources"`
			Related   []map[string]any          `json:"relatedPages"`
			Protected map[string]map[string]any `json:"protectedPages"`
		}
		input := req.Messages[1].Content
		if err := json.Unmarshal([]byte(input), &payload); err != nil {
			t.Fatal(err)
		}
		if len([]rune(input)) > 60000 || len(payload.Related) != 0 || payload.Protected["large-page"] == nil {
			t.Fatalf("oversized baseline was sent or treated as read: %d", len([]rune(input)))
		}
		source := payload.Sources[0]
		plan := model.WikiPlan{Pages: []model.WikiPageChange{{Key: "refund-1", Title: "退款期限补充", PageType: "topic", Markdown: source.Content, Links: []string{"large-page"}, SourceRefs: []model.WikiSourceRef{{DocumentID: source.DocumentID, Checksum: source.Checksum, Quote: source.Content}}}}}
		data, _ := json.Marshal(plan)
		return string(data), 200
	})
	body := strings.Repeat("历史有效内容", 12000)
	path := doc.Path + ".wiki"
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	s.AddDocument(id, model.Document{ID: "large-page", Name: "历史主题", Path: path, Kind: "wiki", Wiki: &model.WikiPageMetadata{Revision: 1}})
	job, _, err := s.StartWikiJob(id, model.WikiJobRequest{DocumentIDs: []string{doc.ID}, TargetPageID: "large-page", RequestKey: "oversized-baseline"})
	if err != nil {
		t.Fatal(err)
	}
	job = waitWikiJob(t, s, id, job.ID)
	if job.Status != "succeeded" || len(job.ChangedPageIDs) != 1 || job.ChangedPageIDs[0] == "large-page" {
		t.Fatalf("linked topic not published: %+v", job)
	}
	old, _ := s.GetDocument(id, "large-page")
	oldBody, _ := os.ReadFile(old.Path)
	page, _ := s.GetDocument(id, job.ChangedPageIDs[0])
	if string(oldBody) != body || old.Wiki.Revision != 1 || len(page.Wiki.Links) != 1 || page.Wiki.Links[0] != old.ID {
		t.Fatal("old content changed or topic link missing")
	}
}

// TestWikiFrozenDraftsPublishAllTopics 验证累计草稿与页数超过旧限额仍保留所有主题，最后一次性发布。
func TestWikiFrozenDraftsPublishAllTopics(t *testing.T) {
	var batches atomic.Int32
	var sawProtected atomic.Bool
	s, id, doc := wikiFixture(t, func(req model.ChatCompletionRequest) (string, int) {
		input := req.Messages[1].Content
		if len([]rune(input)) > 60000 {
			t.Fatalf("request exceeds context budget: %d", len([]rune(input)))
		}
		if strings.HasPrefix(req.Messages[0].Content, "提取") {
			return `[{"claim":"本段完整原文","startLine":1,"endLine":1}]`, 200
		}
		var payload struct {
			Sources   []wikiSourceInput         `json:"sources"`
			Previous  model.WikiPlan            `json:"previousPlan"`
			Protected map[string]map[string]any `json:"protectedPages"`
		}
		_ = json.Unmarshal([]byte(input), &payload)
		if len(payload.Protected) > 0 {
			sawProtected.Store(true)
		}
		batch := batches.Add(1)
		plan := payload.Previous
		// 每批15张独立主题，既验证15页总量限制，也令工作稿超过上下文预留量。
		if len(plan.Pages) != 0 {
			t.Fatal("completed group was resent as working draft")
		}
		source := payload.Sources[0]
		for index := 0; index < 15; index++ {
			key := fmt.Sprintf("topic-%d-%d", batch, index)
			plan.Pages = append(plan.Pages, model.WikiPageChange{Key: key, Title: key, PageType: "topic", Markdown: strings.Repeat("完整事实", 400), SourceRefs: []model.WikiSourceRef{{DocumentID: source.DocumentID, Checksum: source.Checksum, Quote: "甲"}}})
		}
		data, _ := json.Marshal(plan)
		return string(data), 200
	})
	if err := os.WriteFile(doc.Path, []byte(strings.Repeat("甲", 68000)), 0600); err != nil {
		t.Fatal(err)
	}
	// 多页真实索引在race模式下可能超过小任务轮询时限，直接等待同一执行器完成。
	s.wikiClosed = true
	job, _, err := s.StartWikiJob(id, model.WikiJobRequest{DocumentIDs: []string{doc.ID}, RequestKey: "frozen-groups"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.runWikiJob(job); err != nil {
		t.Fatal(err)
	}
	job, err = s.GetWikiJob(id, job.ID)
	if err != nil || job.Status != "succeeded" || batches.Load() < 4 || !sawProtected.Load() || len(job.ChangedPageIDs) != int(batches.Load())*15 {
		t.Fatalf("frozen topics lost: %+v batches=%d", job, batches.Load())
	}
	for _, pageID := range job.ChangedPageIDs {
		page, _ := s.GetDocument(id, pageID)
		if page.Wiki.Revision != 1 || page.Wiki.IndexedRevision != 1 {
			t.Fatalf("intermediate revision published or unindexed: %+v", page.Wiki)
		}
	}
}
