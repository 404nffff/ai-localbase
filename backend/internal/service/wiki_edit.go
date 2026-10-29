package service

import (
	"errors"
	"strings"

	"ai-localbase/internal/model"
	"ai-localbase/internal/util"
)

// WikiPageEdit 只接收人工调整的标题和正文；来源、链接及页面类型从当前页面继承。
type WikiPageEdit struct {
	BaseRevision int    `json:"baseRevision"` // 完整读取的当前修订，用于拒绝过期覆盖。
	Title        string `json:"title"`        // 修改后的知识页标题，不允许为空。
	Markdown     string `json:"markdown"`     // 完整 Markdown 正文，不能用分页预览覆盖。
}

// WikiPageEditResult 区分正文已发布与检索索引结果，索引失败不能诱导用户重复保存。
type WikiPageEditResult struct {
	PageID     string `json:"pageId"`               // 被修改的原知识页身份。
	Revision   int    `json:"revision"`             // 本次新增的历史修订。
	Indexed    bool   `json:"indexed"`              // 本次索引是否完成。
	IndexError string `json:"indexError,omitempty"` // 正文已保存但索引失败时的实际原因。
}

// EditWikiPage 基于当前修订保存人工编辑，复用来源校验和不可覆盖的历史发布流程。
// 正文发布失败直接返回错误；发布后的索引失败返回成功回执和索引错误，不回滚已保存正文。
func (s *AppService) EditWikiPage(kbID, pageID string, input WikiPageEdit) (WikiPageEditResult, error) {
	result := WikiPageEditResult{}
	if input.BaseRevision < 1 || strings.TrimSpace(input.Title) == "" || strings.TrimSpace(input.Markdown) == "" {
		return result, &WikiError{400, "invalid_edit", "请完整填写标题、正文和当前修订号"}
	}
	kb, err := s.WikiSnapshot(kbID)
	if err != nil {
		return result, err
	}
	var current model.Document
	for _, document := range kb.Documents {
		if document.ID == pageID && document.Wiki != nil {
			current = document
			break
		}
	}
	if current.Wiki == nil {
		return result, &WikiError{404, "not_found", "知识页不存在"}
	}
	if current.Wiki.Revision != input.BaseRevision {
		return result, &WikiError{409, "conflict", "知识页已有新修订，请保留草稿并重新读取当前版本后合并"}
	}
	// 客户端不能替换来源证据；发布时还会再次检查基线，防止读取后发生并发更新。
	change := model.WikiPageChange{
		PageID:       pageID,
		BaseRevision: input.BaseRevision,
		Title:        input.Title,
		Markdown:     input.Markdown,
		PageType:     current.Wiki.PageType,
		Aliases:      current.Wiki.Aliases,
		SourceRefs:   current.Wiki.SourceRefs,
		Links:        current.Wiki.Links,
	}
	_, err = s.publishWikiPlan(kbID, util.NextID("manual"), model.WikiPlan{Pages: []model.WikiPageChange{change}})
	if err != nil {
		var wikiErr *WikiError
		if errors.As(err, &wikiErr) && wikiErr.Code == "conflict" {
			return result, &WikiError{409, "conflict", "知识页已有新修订，请保留草稿并重新读取当前版本后合并"}
		}
		return result, err
	}
	result.PageID = pageID
	result.Revision = input.BaseRevision + 1
	_, err = s.indexWikiPage(kbID, pageID)
	if err != nil {
		result.IndexError = err.Error()
		return result, nil
	}
	result.Indexed = true
	return result, nil
}
