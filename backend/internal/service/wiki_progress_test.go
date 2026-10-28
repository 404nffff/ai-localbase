package service

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"ai-localbase/internal/model"
)

// TestWikiChanges 验证发布记录跨页面排序、修订分页、真实来源数与库边界。
func TestWikiChanges(t *testing.T) {
	s := NewAppService(nil, nil, nil, model.ServerConfig{UploadDir: t.TempDir()})
	t.Cleanup(func() { _ = s.Close() })
	id := s.ListKnowledgeBases()[0].ID
	if err := s.commitWikiChange(id, func(kb *model.KnowledgeBase) error {
		kb.Documents = []model.Document{
			{ID: "source", Name: "原始资料"},
			{ID: "a", Wiki: &model.WikiPageMetadata{History: []model.WikiRevision{
				{Revision: 1, Title: "首次整理", Path: "private-path", CreatedAt: "2026-10-27T00:00:00Z"},
				{Revision: 2, Title: "最新整理", CreatedAt: "2026-10-28T09:00:00+08:00", SourceRefs: []model.WikiSourceRef{{DocumentID: "source", Quote: "private-quote"}, {DocumentID: "source"}}},
			}}},
			{ID: "b", Wiki: &model.WikiPageMetadata{History: []model.WikiRevision{{Revision: 1, Title: "另一文档", CreatedAt: "2026-10-28T00:30:00Z"}}}},
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	result, err := s.WikiChanges(id, 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	items := result["items"].([]WikiChange)
	if result["total"] != 3 || result["nextCursor"] != "2" || len(items) != 2 || items[0].Revision != 2 || items[0].Title != "最新整理" || items[0].SourceCount != 1 || items[1].PageID != "b" {
		t.Fatalf("wrong records: %+v", result)
	}
	encoded, _ := json.Marshal(result)
	if strings.Contains(string(encoded), "private-") {
		t.Fatal("record leaked internal details")
	}
	last, err := s.WikiChanges(id, 2, 2)
	if err != nil || last["nextCursor"] != "" || last["items"].([]WikiChange)[0].Title != "首次整理" {
		t.Fatalf("wrong last page: %+v %v", last, err)
	}
	if _, err := s.WikiChanges("missing", 0, 6); err == nil {
		t.Fatal("unknown library accepted")
	}
	if _, err := s.WikiChanges(id, -1, 6); err == nil {
		t.Fatal("invalid offset accepted")
	}
	// 另一个库使用相同页面ID，检验全局结果保留真实库归属，且覆盖20条以外的历史。
	other, err := s.CreateKnowledgeBase(model.KnowledgeBaseInput{Name: "另一个知识库"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.commitWikiChange(other.ID, func(kb *model.KnowledgeBase) error {
		history := []model.WikiRevision{}
		for i := 1; i <= 25; i++ {
			history = append(history, model.WikiRevision{Revision: i, Title: "跨库记录", CreatedAt: time.Date(2026, 10, 29, 0, i, 0, 0, time.UTC).Format(time.RFC3339)})
		}
		kb.Documents = []model.Document{{ID: "a", Wiki: &model.WikiPageMetadata{History: history}}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	all, err := s.WikiChanges("", 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	first := all["items"].([]WikiChange)
	if all["total"] != 28 || all["nextCursor"] != "20" || len(first) != 20 || first[0].KnowledgeBaseID != other.ID || first[0].KnowledgeBaseName != other.Name || first[0].Revision != 25 {
		t.Fatalf("global history incomplete: %+v", all)
	}
	next, err := s.WikiChanges("", 20, 20)
	if err != nil || len(next["items"].([]WikiChange)) != 8 || next["nextCursor"] != "" {
		t.Fatalf("global last page incorrect: %+v %v", next, err)
	}
	filtered, err := s.WikiChanges(id, 0, 20)
	if err != nil || filtered["total"] != 3 || filtered["items"].([]WikiChange)[0].KnowledgeBaseID != id {
		t.Fatalf("single library leaked: %+v %v", filtered, err)
	}
}

// TestWikiProgressAllHistory 验证超过旧接口 50 条的任务仍可统计、筛选和分页，无内部正文泄漏。
func TestWikiProgressAllHistory(t *testing.T) {
	s := NewAppService(nil, nil, nil, model.ServerConfig{UploadDir: t.TempDir()})
	id := s.ListKnowledgeBases()[0].ID
	err := s.commitWikiChange(id, func(kb *model.KnowledgeBase) error {
		kb.Wiki.PendingSources["source"] = "version"
		for i := 0; i < 65; i++ {
			status := "succeeded"
			if i < 5 {
				status = "failed"
			}
			created := time.Date(2026, 10, 28, 0, i, 0, 0, time.UTC).Format(time.RFC3339)
			kb.Wiki.Jobs = append(kb.Wiki.Jobs, model.WikiJob{
				ID: fmt.Sprintf("job-%03d", i), Kind: "ingest", Status: status,
				CreatedAt: created, UpdatedAt: created, Attempt: 1,
				Extracts: map[string]string{"source": "internal-extraction"}, CaptureText: "internal-answer",
			})
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.WikiProgress(id, "", 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	var library WikiLibraryProgress
	for _, item := range result["items"].([]WikiLibraryProgress) {
		if item.KnowledgeBaseID == id {
			library = item
		}
	}
	if library.Total != 65 || library.Counts["succeeded"] != 60 || library.Counts["failed"] != 5 || library.PendingCount != 1 {
		t.Fatalf("incomplete counts: %+v", library)
	}
	jobs := result["jobs"].(map[string]any)
	if jobs["total"] != 65 || jobs["nextCursor"] != "20" || jobs["items"].([]WikiJobProgress)[0].ID != "job-064" {
		t.Fatalf("incorrect first page: %+v", jobs)
	}
	filtered, _ := s.WikiProgress(id, "failed", 0, 20)
	if filtered["jobs"].(map[string]any)["total"] != 5 {
		t.Fatal("old failures were hidden by recent-job limit")
	}
	last, _ := s.WikiProgress(id, "", 60, 20)
	if len(last["jobs"].(map[string]any)["items"].([]WikiJobProgress)) != 5 || last["jobs"].(map[string]any)["nextCursor"] != "" {
		t.Fatal("incorrect last page")
	}
	empty, _ := s.WikiProgress(id, "", math.MaxInt, 20)
	if len(empty["jobs"].(map[string]any)["items"].([]WikiJobProgress)) != 0 {
		t.Fatal("out-of-range offset should be empty")
	}
	global, _ := s.WikiProgress("", "", 0, 20)
	if global["jobs"].(map[string]any)["total"] != 0 {
		t.Fatal("summary should not return all job bodies")
	}
	encoded, _ := json.Marshal(result)
	if strings.Contains(string(encoded), "internal-") || strings.Contains(string(encoded), "captureText") || strings.Contains(string(encoded), "extracts") {
		t.Fatal("progress response contains internal text")
	}
	// 修改返回计数不会影响服务端内存。
	library.Counts["failed"] = 900
	again, _ := s.WikiProgress(id, "failed", 0, 20)
	if again["jobs"].(map[string]any)["total"] != 5 {
		t.Fatal("response mutated stored jobs")
	}
}

// TestWikiProgressValidation 验证非法分页、未知库、未知状态与不同业务终态。
func TestWikiProgressValidation(t *testing.T) {
	s := NewAppService(nil, nil, nil, model.ServerConfig{UploadDir: t.TempDir()})
	id := s.ListKnowledgeBases()[0].ID
	for _, tc := range []struct {
		id, status    string
		offset, limit int
	}{
		{"missing", "", 0, 20}, {id, "invalid", 0, 20}, {id, "", -1, 20},
		{id, "", 0, 0}, {id, "", 0, 101},
	} {
		if _, err := s.WikiProgress(tc.id, tc.status, tc.offset, tc.limit); err == nil {
			t.Fatalf("invalid input accepted: %+v", tc)
		}
	}
	if err := s.commitWikiChange(id, func(kb *model.KnowledgeBase) error {
		kb.Wiki.Enabled = false
		for _, status := range []string{"partial", "needs_split", "canceled", "queued", "running"} {
			kb.Wiki.Jobs = append(kb.Wiki.Jobs, model.WikiJob{ID: status, Status: status})
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	result, _ := s.WikiProgress(id, "partial", 0, 20)
	for _, library := range result["items"].([]WikiLibraryProgress) {
		if library.KnowledgeBaseID == id && (library.Enabled || library.Counts["succeeded"] != 0 || library.Total != 5) {
			t.Fatalf("terminal states miscounted: %+v", library)
		}
	}
	if result["jobs"].(map[string]any)["total"] != 1 {
		t.Fatal("status filtering failed")
	}
}
