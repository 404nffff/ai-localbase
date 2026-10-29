package service

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"ai-localbase/internal/model"
)

// TestWikiSourceStatesCurrentVersion 验证未建任务、版本更新、排队和已整理标记，不信任残留待处理指针。
func TestWikiSourceStatesCurrentVersion(t *testing.T) {
	kb := model.KnowledgeBase{Documents: []model.Document{{ID: "new", ContentChecksum: "v1"}, {ID: "done", ContentChecksum: "v1"}, {ID: "changed", ContentChecksum: "v2"}, {ID: "active", ContentChecksum: "v1"}}, Wiki: &model.WikiState{Jobs: []model.WikiJob{
		{Kind: "ingest", Status: "succeeded", SourceVersions: map[string]string{"done": "v1", "changed": "v1"}},
		{Kind: "ingest", Status: "queued", SourceVersions: map[string]string{"active": "v1"}},
	}}}
	states := wikiSourceStates(kb)
	if states["new"] != "pending" || states["done"] != "succeeded" || states["changed"] != "pending" || states["active"] != "queued" {
		t.Fatalf("incorrect states: %v", states)
	}
}

// TestWikiBatchKeepsAttemptHistory 验证超过三次仍复用原任务，保留历史次数与进度。
func TestWikiBatchKeepsAttemptHistory(t *testing.T) {
	s, id, doc := wikiFixture(t, func(model.ChatCompletionRequest) (string, int) { t.Error("unexpected model call"); return "{}", 200 })
	s.wikiClosed = true
	job, _, err := s.StartWikiJob(id, model.WikiJobRequest{DocumentIDs: []string{doc.ID}, RequestKey: "old"})
	if err != nil {
		t.Fatal(err)
	}
	job.Status, job.Attempt = "failed", 3
	if err := s.saveWikiJob(job); err != nil {
		t.Fatal(err)
	}
	items, err := s.StartWikiBatch(id, nil)
	if err != nil || len(items) != 1 || items[0].Error != "" || items[0].Status != "queued" || items[0].JobID != job.ID {
		t.Fatalf("failed job not resumed: %v %v", items, err)
	}
	kb, _ := s.WikiSnapshot(id)
	if len(kb.Wiki.Jobs) != 1 || kb.Wiki.Jobs[0].Attempt != 3 {
		t.Fatal("batch reset retry history")
	}
	if _, err := s.StartWikiBatch(id, []string{doc.ID, "missing"}); err == nil {
		t.Fatal("unknown document accepted")
	}
}

// TestWikiLineEvidence 保证服务端逐字读取连续行，拒绝越界和虚构引文，合并重复证据但不丢事实。
func TestWikiLineEvidence(t *testing.T) {
	body := "第一行：退款。\r\n第二行：七天。\n第三行。"
	facts, err := resolveWikiExtract(`[{"claim":"退款期限","startLine":1,"endLine":2},{"claim":"第二个事实","startLine":1,"endLine":2}]`, body)
	if err != nil || !strings.Contains(facts, "第二个事实") {
		t.Fatalf("line evidence: %s %v", facts, err)
	}
	var entries []map[string]string
	_ = json.Unmarshal([]byte(facts), &entries)
	if len(entries) != 1 || entries[0]["quote"] != "第一行：退款。\r\n第二行：七天。" {
		t.Fatalf("quote changed: %v", entries)
	}
	for _, invalid := range []string{`[{"claim":"x","startLine":0,"endLine":2}]`, `[{"claim":"x","startLine":2,"endLine":4}]`, `[{"claim":"x","quote":"虚构"}]`} {
		if _, err := resolveWikiExtract(invalid, body); err == nil {
			t.Fatalf("invalid evidence accepted: %s", invalid)
		}
	}
}

// TestWikiLargeSourceBatches 验证超过旧 60000 字符总预算的资料分批完整读到尾部，并只发布一次。
func TestWikiLargeSourceBatches(t *testing.T) {
	var batches atomic.Int32
	var sawTail atomic.Bool
	s, id, doc := wikiFixture(t, func(req model.ChatCompletionRequest) (string, int) {
		input := req.Messages[len(req.Messages)-1].Content
		if len([]rune(input)) > 60000 {
			t.Error("unbounded compilation request")
		}
		if strings.HasPrefix(req.Messages[0].Content, "提取") {
			return `[{"claim":"段落事实","startLine":1,"endLine":1}]`, http.StatusOK
		}
		var payload struct {
			Sources  []wikiSourceInput `json:"sources"`
			Previous model.WikiPlan    `json:"previousPlan"`
		}
		if err := json.Unmarshal([]byte(input), &payload); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(input, "尾部唯一条款") {
			sawTail.Store(true)
		}
		n := batches.Add(1)
		plan := payload.Previous
		if len(plan.Pages) == 0 {
			plan.Pages = []model.WikiPageChange{{Key: "summary", Title: "长文全段摘要", PageType: "source-summary"}}
		}
		plan.Pages[0].Markdown += fmt.Sprintf("第%d批内容。\n", n)
		for _, src := range payload.Sources {
			var facts []map[string]string
			if err := json.Unmarshal([]byte(src.Content), &facts); err != nil {
				t.Fatal(err)
			}
			plan.Pages[0].SourceRefs = append(plan.Pages[0].SourceRefs, model.WikiSourceRef{DocumentID: src.DocumentID, Checksum: src.Checksum, Quote: facts[0]["quote"][:3]})
		}
		result, _ := json.Marshal(plan)
		return string(result), 200
	})
	body := strings.Repeat("正", 68000) + "尾部唯一条款"
	if err := os.WriteFile(doc.Path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	job, _, err := s.StartWikiJob(id, model.WikiJobRequest{DocumentIDs: []string{doc.ID}, RequestKey: "large"})
	if err != nil {
		t.Fatal(err)
	}
	job = waitWikiJob(t, s, id, job.ID)
	if job.Status != "succeeded" || !sawTail.Load() || batches.Load() < 4 || len(job.ChangedPageIDs) != 1 {
		t.Fatalf("incomplete large compilation: %+v batches=%d", job, batches.Load())
	}
	page, _ := s.GetDocument(id, job.ChangedPageIDs[0])
	if page.Wiki.Revision != 1 {
		t.Fatal("intermediate draft published")
	}
}

// TestWikiReadsMissingBaseline 验证模型选择目录中的未读页时补读真实正文后重新生成，错误候选不会直接发布。
func TestWikiReadsMissingBaseline(t *testing.T) {
	var calls atomic.Int32
	s, id, doc := wikiFixture(t, func(req model.ChatCompletionRequest) (string, int) {
		var payload struct {
			Sources []wikiSourceInput `json:"sources"`
			Related []map[string]any  `json:"relatedPages"`
		}
		_ = json.Unmarshal([]byte(req.Messages[len(req.Messages)-1].Content), &payload)
		if calls.Add(1) == 2 && (len(payload.Related) != 1 || payload.Related[0]["markdown"] != "历史有效事实") {
			t.Error("baseline was not read")
		}
		src := payload.Sources[0]
		plan := model.WikiPlan{Pages: []model.WikiPageChange{{PageID: "existing-page", BaseRevision: 1, Title: "历史主题", PageType: "topic", Markdown: "历史有效事实\n退款期限为七天。", SourceRefs: []model.WikiSourceRef{{DocumentID: src.DocumentID, Checksum: src.Checksum, Quote: src.Content}}}}}
		result, _ := json.Marshal(plan)
		return string(result), 200
	})
	path := doc.Path + ".wiki"
	if err := os.WriteFile(path, []byte("历史有效事实"), 0600); err != nil {
		t.Fatal(err)
	}
	s.AddDocument(id, model.Document{ID: "existing-page", Name: "历史主题", Path: path, Kind: "wiki", Wiki: &model.WikiPageMetadata{Revision: 1}})
	job, _, err := s.StartWikiJob(id, model.WikiJobRequest{DocumentIDs: []string{doc.ID}, RequestKey: "read-baseline"})
	if err != nil {
		t.Fatal(err)
	}
	job = waitWikiJob(t, s, id, job.ID)
	if job.Status != "succeeded" || calls.Load() != 2 {
		t.Fatalf("baseline recovery: %+v calls=%d", job, calls.Load())
	}
}

// TestWikiBatchDoesNotReuseUnrelatedFailure 验证无校验值的旧资料不会误用其他资料的失败任务。
func TestWikiBatchDoesNotReuseUnrelatedFailure(t *testing.T) {
	s, id, doc := wikiFixture(t, func(model.ChatCompletionRequest) (string, int) {
		t.Error("unexpected model call")
		return "{}", http.StatusOK
	})
	s.wikiClosed = true
	failed, _, err := s.StartWikiJob(id, model.WikiJobRequest{
		DocumentIDs: []string{doc.ID},
		RequestKey:  "unrelated-failure",
	})
	if err != nil {
		t.Fatal(err)
	}
	failed.Status, failed.Attempt = "failed", 3
	if err := s.saveWikiJob(failed); err != nil {
		t.Fatal(err)
	}
	legacy := doc
	legacy.ID = "legacy-without-checksum"
	legacy.ContentChecksum = ""
	s.AddDocument(id, legacy)
	items, err := s.StartWikiBatch(id, []string{legacy.ID})
	if err != nil || len(items) != 1 {
		t.Fatalf("batch result: %v %v", items, err)
	}
	if items[0].Error != "" || items[0].Status != "queued" || items[0].JobID == failed.ID {
		t.Fatalf("unrelated failed job reused: %+v", items[0])
	}
}
