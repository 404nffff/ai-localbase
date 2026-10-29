package service

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"ai-localbase/internal/model"
)

// TestWikiManualEdit 验证人工修改独立发布、历史保留、落盘恢复及索引失败回执。
func TestWikiManualEdit(t *testing.T) {
	s, kbID, source := wikiFixture(t, func(model.ChatCompletionRequest) (string, int) {
		t.Error("人工编辑不应调用聊天模型")
		return "", 500
	})
	ids, err := s.publishWikiPlan(kbID, "", model.WikiPlan{Pages: []model.WikiPageChange{{
		Title: "退款说明", PageType: "topic", Markdown: "原版退款说明。",
		SourceRefs: []model.WikiSourceRef{{DocumentID: source.ID, Checksum: source.ContentChecksum, Quote: "退款期限为七天。"}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	pageID := ids[0]
	input := WikiPageEdit{BaseRevision: 1, Title: "退款办理说明", Markdown: "# 退款办理\n\n退款期限为七天。😀"}
	result, err := s.EditWikiPage(kbID, pageID, input)
	if err != nil || result.Revision != 2 || !result.Indexed {
		t.Fatalf("edit: %+v %v", result, err)
	}
	old, err := s.WikiPageContent(kbID, pageID, 1, 0, 0)
	if err != nil || old["markdown"] != "原版退款说明。" || old["title"] != "退款说明" {
		t.Fatalf("history: %+v %v", old, err)
	}
	reloaded := NewAppService(nil, s.store, nil, s.serverConfig)
	t.Cleanup(func() { _ = reloaded.Close() })
	current, err := reloaded.WikiPageContent(kbID, pageID, 0, 0, 0)
	if err != nil || current["markdown"] != input.Markdown || current["revision"] != 2 {
		t.Fatalf("reload: %+v %v", current, err)
	}
	for _, tc := range []struct {
		name, page string
		edit       WikiPageEdit
		status     int
	}{
		{"旧基线", pageID, input, 409},
		{"缺失页面", "missing", input, 404},
		{"空正文", pageID, WikiPageEdit{BaseRevision: 2, Title: "标题", Markdown: " "}, 400},
		{"缺少基线", pageID, WikiPageEdit{Title: "标题", Markdown: "正文"}, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := s.EditWikiPage(kbID, tc.page, tc.edit)
			var wikiErr *WikiError
			if !errors.As(err, &wikiErr) || wikiErr.Status != tc.status {
				t.Fatalf("expected %d: %v", tc.status, err)
			}
		})
	}
	// 外部向量服务不可用时正文仍已发布，回执不能诱导重复保存。
	failed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"index":0,"embedding":[1,2]}]}`))
	}))
	defer failed.Close()
	s.state.Config.Embedding.BaseURL = failed.URL
	input.BaseRevision, input.Markdown = 2, "人工补充后的正文。"
	result, err = s.EditWikiPage(kbID, pageID, input)
	if err != nil || result.Revision != 3 || result.Indexed || result.IndexError == "" {
		t.Fatalf("index failure: %+v %v", result, err)
	}
	current, err = s.WikiPageContent(kbID, pageID, 0, 0, 0)
	if err != nil || current["markdown"] != input.Markdown {
		t.Fatalf("saved body lost: %+v %v", current, err)
	}
	// 来源过期时沿用发布校验，不能因人工编辑绕过证据版本约束。
	source.ContentChecksum = "changed"
	if _, err := s.ReplaceDocument(kbID, source); err != nil {
		t.Fatal(err)
	}
	input.BaseRevision = 3
	if _, err := s.EditWikiPage(kbID, pageID, input); err == nil {
		t.Fatal("changed source accepted")
	}
}
