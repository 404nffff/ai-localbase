package service

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"ai-localbase/internal/model"
)

// compileWikiBatches 按事实段分批综合，草稿只在内存中合并，全部通过后才统一发布。
// 当前工作稿和可容纳的完整基线进入模型；较大主题保留在服务端，后续生成关联页。
// 不裁剪来源或已完成草稿，不放宽发布冲突检查，全部批次成功后才返回发布计划。
func (s *AppService) compileWikiBatches(job *model.WikiJob, config model.ChatModelConfig, kb model.KnowledgeBase, sources []wikiSourceInput, directory []map[string]any, selected map[string]bool) (model.WikiPlan, error) {
	batches := [][]wikiSourceInput{{}}
	size := 0
	for _, source := range sources {
		encoded, err := json.Marshal(source)
		if err != nil {
			return model.WikiPlan{}, err
		}
		length := len([]rune(string(encoded)))
		if size > 0 && size+length > 18000 {
			batches = append(batches, nil)
			size = 0
		}
		batches[len(batches)-1] = append(batches[len(batches)-1], source)
		size += length
	}
	plan := model.WikiPlan{Pages: []model.WikiPageChange{}}
	// 已完成的主题留在服务端，后续上下文只携带当前工作稿，发布时再合成整批。
	frozen := model.WikiPlan{Pages: []model.WikiPageChange{}}
	protected := map[string]map[string]any{}
	freezeDraft := func() {
		for _, page := range plan.Pages {
			id := page.PageID
			if id == "" {
				id = page.Key
			}
			protected[id] = map[string]any{"pageId": page.PageID, "key": page.Key, "title": page.Title, "reason": "已完成主题，后续内容请创建关联专题页"}
		}
		frozen.Pages = append(frozen.Pages, plan.Pages...)
		frozen.Issues = append(frozen.Issues, plan.Issues...)
		plan = model.WikiPlan{Pages: []model.WikiPageChange{}}
	}
	for batchIndex, batch := range batches {
		// 估算本批完整工作稿及其原始基线；接近预算时自动结束当前主题组。
		working, err := json.Marshal(plan)
		if err != nil {
			return plan, err
		}
		workingSize := len([]rune(string(working)))
		for _, page := range plan.Pages {
			if page.PageID == "" {
				continue
			}
			for _, doc := range kb.Documents {
				if doc.ID == page.PageID {
					body, err := os.ReadFile(doc.Path)
					if err != nil {
						return plan, err
					}
					metadata, err := json.Marshal(doc.Wiki.SourceRefs)
					if err != nil {
						return plan, err
					}
					workingSize += len([]rune(string(body))) + len([]rune(string(metadata)))
				}
			}
		}
		if workingSize > 24000 || len(plan.Pages) >= 12 {
			freezeDraft()
		}
		previous := plan
		pages := []map[string]any{}
		read := map[string]bool{}
		// 已进入草稿的页面仍携带完整原始基线；其他关联页由剩余预算决定是否预读。
		required := map[string]bool{}
		if job.TargetPageID != "" {
			required[job.TargetPageID] = true
		}
		for _, page := range previous.Pages {
			if page.PageID != "" {
				required[page.PageID] = true
			}
		}
		payload := map[string]any{"sources": batch, "directory": directory, "relatedPages": pages, "previousPlan": previous, "protectedPages": protected, "batch": batchIndex + 1, "batchCount": len(batches), "captureText": job.CaptureText, "targetPageId": job.TargetPageID}
		// loadBaseline 在发送前读完整正文；目录身份不能当作已读基线。
		loadBaseline := func(id string) error {
			if read[id] || protected[id] != nil {
				return nil
			}
			for _, doc := range kb.Documents {
				if doc.ID != id || doc.Wiki == nil {
					continue
				}
				body, err := os.ReadFile(doc.Path)
				if err != nil {
					return err
				}
				pages = append(pages, map[string]any{"pageId": id, "title": doc.Name, "baseRevision": doc.Wiki.Revision, "markdown": string(body), "sourceRefs": doc.Wiki.SourceRefs, "links": doc.Wiki.Links})
				payload["relatedPages"] = pages
				encoded, err := json.Marshal(payload)
				if err != nil {
					return err
				}
				if len([]rune(string(encoded))) > 54000 {
					// 完整旧页装不下时禁止覆盖；模型用本批原始证据生成关联专题。
					pages = pages[:len(pages)-1]
					payload["relatedPages"] = pages
					protected[id] = map[string]any{"pageId": id, "title": doc.Name, "reason": "完整基线超过本批容量，保留旧页并创建关联专题页"}
					return nil
				}
				read[id] = true
				return nil
			}
			return fmt.Errorf("unknown wiki page %q", id)
		}
		for id := range required {
			if err := loadBaseline(id); err != nil {
				return plan, err
			}
		}
		for _, doc := range kb.Documents {
			if !selected[doc.ID] || read[doc.ID] {
				continue
			}
			if err := loadBaseline(doc.ID); err != nil {
				return plan, err
			}
		}
		instruction := wikiRules + "\n当前按批读取来源。previousPlan 是当前主题组的已校验草稿，输出必须包含其全部页面（保留 key/pageId）并合并本批事实，禁止丢弃有效内容及证据。protectedPages 中的页面完整保存在服务端，本批不可修改，也不要重复返回；请将新增事实按具体主题生成有独立标题的关联专题页，links 引用其 pageId 或 key，禁止沿用其标题和别名。仅 relatedPages 提供了可修改的完整基线。需要其他页面时只返回 {\"readPageIds\":[\"页面ID\"]}，读取后再输出计划。每次最多输出15页，每张新页正文控制在12000字符以内，较长内容按主题分成多页。新页面key须带本批batch编号，避免跨批同键冲突。"
		corrections, reads := 0, 0
		for {
			encoded, err := json.Marshal(payload)
			if err != nil {
				return plan, err
			}
			if len([]rune(string(encoded))) > 60000 {
				return plan, fmt.Errorf("本批目录或来源超过自动整理容量，未发布任何草稿")
			}
			job.Stage, job.TotalPages, job.CheckedPages = "plan", len(directory), len(read)
			text, err := s.wikiModelText(job, config, instruction, string(encoded))
			if err != nil {
				return plan, err
			}
			var request struct {
				ReadPageIDs []string `json:"readPageIds"`
			}
			_ = json.Unmarshal([]byte(strings.TrimSpace(text)), &request)
			candidate, validationErr := parseWikiPlan(text)
			// 模型直接返回未读页的改动时，也先补读再让它重做，绝不发布该候选结果。
			if validationErr == nil {
				for _, page := range candidate.Pages {
					identity := page.PageID
					if identity == "" {
						identity = page.Key
					}
					if protected[identity] != nil {
						validationErr = fmt.Errorf("page %q is protected; create a linked topic page with a distinct title and key", page.Title)
						break
					}
					if page.PageID != "" && !read[page.PageID] {
						request.ReadPageIDs = append(request.ReadPageIDs, page.PageID)
					}
				}
			}
			if len(request.ReadPageIDs) > 0 {
				if reads >= 2 {
					return plan, fmt.Errorf("模型连续请求额外基线，已达到本批两次补读上限")
				}
				for _, id := range request.ReadPageIDs {
					if err := loadBaseline(id); err != nil {
						return plan, err
					}
				}
				reads++
				instruction += "\n请求页面的读取结果已更新：relatedPages 包含可修改的完整基线，baseRevision 使用其版本；protectedPages 标记本批不可修改的页面，请创建关联专题页。"
				continue
			}
			if validationErr == nil && len(candidate.Pages) > 15 {
				validationErr = fmt.Errorf("one source batch must produce at most 15 topic pages")
			}
			if validationErr == nil {
				for _, page := range candidate.Pages {
					if page.PageID == "" && len([]rune(page.Markdown)) > 16000 {
						validationErr = fmt.Errorf("new page %q is too large; split its complete content into linked topic pages", page.Title)
						break
					}
					for _, saved := range frozen.Pages {
						if page.Title == saved.Title {
							validationErr = fmt.Errorf("page %q is already preserved; choose a distinct topic title", page.Title)
							break
						}
					}
				}
			}
			if validationErr == nil {
				validationErr = validateWikiPlanSources(candidate, kb.Documents)
			}
			if validationErr == nil {
				for _, page := range candidate.Pages {
					if page.PageID != "" && page.BaseRevision != job.BaseRevisions[page.PageID] {
						validationErr = fmt.Errorf("page %s baseRevision must be %d", page.PageID, job.BaseRevisions[page.PageID])
						break
					}
				}
			}
			if validationErr == nil {
				for _, old := range previous.Pages {
					found := false
					for _, next := range candidate.Pages {
						if (old.PageID != "" && old.PageID == next.PageID) || (old.PageID == "" && old.Key == next.Key && old.Title == next.Title) {
							found = true
							// 前批已经验证的证据必须保留，防止合并时只剩最后一批来源。
							for _, ref := range old.SourceRefs {
								preserved := false
								for _, current := range next.SourceRefs {
									if current.DocumentID == ref.DocumentID && current.Checksum == ref.Checksum && current.Quote == ref.Quote {
										preserved = true
										break
									}
								}
								if !preserved {
									validationErr = fmt.Errorf("previousPlan page %q must retain its verified source evidence", old.Title)
									break
								}
							}
							break
						}
					}
					if !found {
						validationErr = fmt.Errorf("previousPlan page %q must be preserved", old.Title)
						break
					}
				}
			}
			if validationErr == nil {
				plan = candidate
				break
			}
			if business, ok := validationErr.(*WikiError); ok && business.Code != "invalid_model_evidence" {
				return plan, validationErr
			}
			if corrections >= 1 {
				return plan, fmt.Errorf("wiki plan still invalid after correction: %w", validationErr)
			}
			corrections++
			instruction += "\n上次输出未通过格式或来源检查：" + validationErr.Error() + "。请根据本次输入重新生成完整计划。"
		}
	}
	// 合并服务端保留的完整主题与最后工作稿，一次发布，任何失败都不发布中间结果。
	plan.Pages = append(frozen.Pages, plan.Pages...)
	plan.Issues = append(frozen.Issues, plan.Issues...)
	if job.Kind == "capture" && len(job.DocumentIDs) == 0 {
		for _, page := range plan.Pages {
			if page.PageType != "question" || len(page.SourceRefs) != 0 {
				return plan, fmt.Errorf("an answer without verified sources may only create questions")
			}
		}
	}
	job.Stage = "validate"
	return plan, s.saveWikiJob(*job)
}
