package service

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"ai-localbase/internal/model"
	"ai-localbase/internal/util"
)

// wikiRules 是与实现共同版本化的知识维护规则。
//
//go:embed wiki_rules.md
var wikiRules string

// WikiError 提供 HTTP/MCP 一致的业务错误码。
type WikiError struct {
	Status  int
	Code    string
	Message string
}

// Error 返回可展示的业务错误，不携带模型凭证。
func (e *WikiError) Error() string { return e.Message }

// WikiSnapshot 返回独立库快照，调用方修改不会影响运行中状态。
func (s *AppService) WikiSnapshot(kbID string) (model.KnowledgeBase, error) {
	s.state.Mu.RLock()
	defer s.state.Mu.RUnlock()
	kb, ok := s.state.KnowledgeBases[kbID]
	if !ok {
		return model.KnowledgeBase{}, &WikiError{404, "not_found", "knowledge base not found"}
	}
	return cloneKnowledgeBases(map[string]model.KnowledgeBase{kbID: kb})[kbID], nil
}

// commitWikiChange 在同一保存区间内修改候选快照，落盘成功后才发布内存。
// change 只处理本库数据，不在回调中请求模型或递归保存状态。
func (s *AppService) commitWikiChange(kbID string, change func(*model.KnowledgeBase) error) error {
	s.stateSaveMu.Lock()
	defer s.stateSaveMu.Unlock()
	s.state.Mu.Lock()
	defer s.state.Mu.Unlock()
	if _, ok := s.state.KnowledgeBases[kbID]; !ok {
		return &WikiError{404, "not_found", "knowledge base not found"}
	}
	candidate := cloneKnowledgeBases(s.state.KnowledgeBases)
	kb := candidate[kbID]
	if kb.Wiki == nil {
		kb.Wiki = &model.WikiState{Enabled: true, RulesVersion: "1", PendingSources: map[string]string{}}
	}
	if kb.Wiki.PendingSources == nil {
		kb.Wiki.PendingSources = map[string]string{}
	}
	if err := change(&kb); err != nil {
		return err
	}
	candidate[kbID] = kb
	// 回调输入中的映射可能仍由任务执行器持有，提交时再隔离一次所有权。
	candidate = cloneKnowledgeBases(candidate)
	if s.store != nil {
		if err := s.store.Save(persistentAppState{Config: s.state.Config, KnowledgeBases: candidate}); err != nil {
			return fmt.Errorf("persist wiki: %w", err)
		}
	}
	s.state.KnowledgeBases = candidate
	s.retrievalRevision.Add(1)
	return nil
}

// ConfigureWiki 控制本库整理和检索，启用不自动回填历史资料。
func (s *AppService) ConfigureWiki(kbID string, enabled bool) (model.WikiState, error) {
	err := s.commitWikiChange(kbID, func(kb *model.KnowledgeBase) error {
		kb.Wiki.Enabled = enabled
		if !enabled {
			for i := range kb.Wiki.Jobs {
				if kb.Wiki.Jobs[i].Status == "queued" {
					kb.Wiki.Jobs[i].Status = "canceled"
					kb.Wiki.Jobs[i].UpdatedAt = util.NowRFC3339()
				}
			}
		}
		return nil
	})
	if err != nil {
		return model.WikiState{}, err
	}
	kb, err := s.WikiSnapshot(kbID)
	if err != nil {
		return model.WikiState{}, err
	}
	return *kb.Wiki, nil
}

// publishWikiPlan 校验整批来源、页面基线和链接，再一次发布全部页面指针。
// 索引独立执行；未完成索引的新修订不会进入默认问答。
func (s *AppService) publishWikiPlan(kbID, jobID string, plan model.WikiPlan) ([]string, error) {
	changed := make([]string, 0, len(plan.Pages))
	written := make([]string, 0, len(plan.Pages))
	err := s.commitWikiChange(kbID, func(kb *model.KnowledgeBase) error {
		if !kb.Wiki.Enabled {
			return &WikiError{409, "disabled", "wiki is disabled"}
		}
		// 每个来源分段可形成独立主题；整批保留同一次原子发布，不受旧的15页总量限制。
		// 非整理任务仍使用原15页预算，实际任务预算来自服务端已持久化的完整段数。
		pageLimit := 15
		for _, job := range kb.Wiki.Jobs {
			if job.ID == jobID {
				pageLimit = 15 * max(1, job.TotalSegments)
				break
			}
		}
		if len(plan.Pages) > pageLimit {
			return &WikiError{422, "invalid_plan", "generated pages exceed the source segment budget"}
		}
		documents := make(map[string]model.Document, len(kb.Documents))
		names := map[string]string{}
		for _, document := range kb.Documents {
			documents[document.ID] = document
			if document.Wiki != nil {
				for _, name := range append([]string{document.Name}, document.Wiki.Aliases...) {
					names[strings.ToLower(strings.TrimSpace(name))] = document.ID
				}
			}
		}
		for _, job := range kb.Wiki.Jobs {
			if job.ID != jobID {
				continue
			}
			for id, checksum := range job.SourceVersions {
				source, exists := documents[id]
				if !exists || source.Wiki != nil || source.ContentChecksum != checksum {
					return &WikiError{409, "source_changed", "job source changed before publication"}
				}
				body, err := util.ExtractDocumentText(source.Path)
				if err != nil {
					return err
				}
				if hashDocumentContent(body) != checksum {
					return &WikiError{409, "source_changed", "source body changed before publication"}
				}
			}
		}
		// 先分配本批稳定 ID，后解析跨页链接；重复标题不能产生第二张页面。
		keys := map[string]string{}
		seen := map[string]bool{}
		for i := range plan.Pages {
			page := &plan.Pages[i]
			page.Title = strings.TrimSpace(page.Title)
			if page.Title == "" || strings.TrimSpace(page.Markdown) == "" {
				return &WikiError{400, "invalid_plan", "page title and markdown are required"}
			}
			if !slices.Contains([]string{"topic", "decision", "source-summary", "question"}, page.PageType) {
				return &WikiError{400, "invalid_plan", "invalid page type"}
			}
			if len(page.SourceRefs) == 0 && page.PageType != "question" {
				return &WikiError{400, "missing_evidence", "fact pages require original sources"}
			}
			if page.PageID == "" {
				if page.BaseRevision != 0 {
					return &WikiError{400, "invalid_plan", "new pages must start at revision zero"}
				}
				if names[strings.ToLower(page.Title)] != "" {
					return &WikiError{409, "duplicate_topic", "reuse the existing page for this title"}
				}
				page.PageID = util.NextID("wiki")
			} else {
				old, exists := documents[page.PageID]
				if !exists || old.Wiki == nil {
					return &WikiError{404, "not_found", "wiki page not found"}
				}
				if old.Wiki.Revision != page.BaseRevision {
					return &WikiError{409, "conflict", "wiki page changed during compilation"}
				}
			}
			if seen[page.PageID] {
				return &WikiError{400, "invalid_plan", "duplicate page in one plan"}
			}
			seen[page.PageID] = true
			if page.Key != "" {
				if keys[page.Key] != "" {
					return &WikiError{400, "invalid_plan", "duplicate page key"}
				}
				keys[page.Key] = page.PageID
			}
			for _, name := range append([]string{page.Title}, page.Aliases...) {
				name = strings.ToLower(strings.TrimSpace(name))
				if id := names[name]; id != "" && id != page.PageID {
					return &WikiError{409, "duplicate_topic", "page title or alias already exists"}
				}
				names[name] = page.PageID
			}
		}
		for _, page := range plan.Pages {
			for _, ref := range page.SourceRefs {
				source, exists := documents[ref.DocumentID]
				if !exists || source.Wiki != nil || ref.Checksum != source.ContentChecksum {
					return &WikiError{409, "source_changed", "source is missing or changed"}
				}
				body, err := util.ExtractDocumentText(source.Path)
				if err != nil {
					return err
				}
				if hashDocumentContent(body) != ref.Checksum || strings.TrimSpace(ref.Quote) == "" || !strings.Contains(body, ref.Quote) {
					return &WikiError{409, "invalid_evidence", "source quote or version does not match"}
				}
			}
			links := make([]string, 0, len(page.Links))
			for _, link := range page.Links {
				if id := keys[link]; id != "" {
					link = id
				}
				if !seen[link] && documents[link].Wiki == nil {
					return &WikiError{400, "invalid_link", "wiki link target does not exist"}
				}
				links = append(links, link)
			}
			document := documents[page.PageID]
			revision := page.BaseRevision + 1
			filename := strconv.Itoa(revision) + ".md"
			if jobID != "" {
				filename = strconv.Itoa(revision) + "-" + jobID + ".md"
			}
			path := filepath.Join(s.serverConfig.UploadDir, "wiki", kbID, "pages", page.PageID, filename)
			if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
				return err
			}
			// 只写新版本；失败恢复不得覆盖已经发布的历史正文。
			file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
			if os.IsExist(err) && jobID != "" {
				// 只恢复同一任务、尚未被历史指针引用的文件，不能覆盖已发布历史。
				referenced := false
				if document.Wiki != nil {
					for _, history := range document.Wiki.History {
						if history.Path == path {
							referenced = true
						}
					}
				}
				if !referenced {
					file, err = os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0644)
				}
			}
			if err != nil {
				return err
			}
			written = append(written, path)
			_, writeErr := file.WriteString(page.Markdown)
			closeErr := file.Close()
			if writeErr != nil {
				return writeErr
			}
			if closeErr != nil {
				return closeErr
			}
			now := util.NowRFC3339()
			history := []model.WikiRevision{}
			if document.Wiki != nil {
				history = document.Wiki.History
			}
			history = append(history, model.WikiRevision{Revision: revision, Path: path, Title: page.Title, SourceRefs: page.SourceRefs, Links: links, CreatedAt: now})
			document.ID, document.KnowledgeBaseID, document.Name = page.PageID, kbID, page.Title
			document.Kind, document.Path, document.MarkdownPath = "wiki", path, path
			document.ContentChecksum, document.ContentPreview = hashDocumentContent(page.Markdown), util.BuildContentPreviewFromText(page.Markdown)
			document.Size, document.SizeLabel = int64(len(page.Markdown)), util.FormatFileSize(int64(len(page.Markdown)))
			document.Status, document.IndexError = "processing", ""
			if document.UploadedAt == "" {
				document.UploadedAt = now
			}
			document.Wiki = &model.WikiPageMetadata{PageID: page.PageID, Slug: page.PageID, PageType: page.PageType, Aliases: page.Aliases, Revision: revision, SourceRefs: page.SourceRefs, Links: links, Freshness: "fresh", UpdatedAt: now, History: history}
			found := false
			for i := range kb.Documents {
				if kb.Documents[i].ID == page.PageID {
					kb.Documents[i] = document
					found = true
				}
			}
			if !found {
				kb.Documents = append(kb.Documents, document)
			}
			changed = append(changed, page.PageID)
		}
		for i := range kb.Wiki.Jobs {
			if kb.Wiki.Jobs[i].ID == jobID {
				kb.Wiki.Jobs[i].ChangedPageIDs = changed
				kb.Wiki.Jobs[i].Stage = "index"
			}
		}
		for _, issue := range plan.Issues {
			for i, id := range issue.PageIDs {
				if keys[id] != "" {
					issue.PageIDs[i] = keys[id]
				}
			}
			if err := validateWikiIssue(*kb, issue); err != nil {
				return err
			}
			mergeWikiIssue(kb, issue)
		}
		return nil
	})
	if err != nil {
		for _, path := range written {
			if removeErr := os.Remove(path); removeErr != nil {
				return nil, fmt.Errorf("%w; cleanup unpublished page: %v", err, removeErr)
			}
		}
		return nil, err
	}
	return changed, nil
}

// WikiPageContent 按 Unicode 码点分页读取指定历史修订，避免预览截断冒充全文。
func (s *AppService) WikiPageContent(kbID, pageID string, revision, offset, limit int) (map[string]any, error) {
	kb, err := s.WikiSnapshot(kbID)
	if err != nil {
		return nil, err
	}
	if offset < 0 || limit < 0 || limit > 50000 {
		return nil, &WikiError{400, "invalid_page", "invalid text offset or limit"}
	}
	if limit == 0 {
		limit = 20000
	}
	for _, document := range kb.Documents {
		if document.ID != pageID || document.Wiki == nil {
			continue
		}
		if revision == 0 {
			revision = document.Wiki.Revision
		}
		for _, version := range document.Wiki.History {
			if version.Revision != revision {
				continue
			}
			content, err := os.ReadFile(version.Path)
			if err != nil {
				return nil, err
			}
			runes := []rune(string(content))
			if offset > len(runes) {
				return nil, &WikiError{400, "invalid_page", "offset exceeds content length"}
			}
			end := min(offset+limit, len(runes))
			next := 0
			if end < len(runes) {
				next = end
			}
			// 正文来源是否已删除由当前资料列表决定，快照路径不返回客户端。
			for i := range document.Wiki.History {
				document.Wiki.History[i].Path = ""
			}
			missing := []string{}
			for _, ref := range version.SourceRefs {
				exists := false
				for _, source := range kb.Documents {
					if source.ID == ref.DocumentID && source.Wiki == nil {
						exists = true
					}
				}
				if !exists {
					missing = append(missing, ref.DocumentID)
				}
			}
			return map[string]any{"pageId": pageID, "title": version.Title, "revision": revision, "metadata": document.Wiki, "sourceRefs": version.SourceRefs, "links": version.Links, "missingSources": missing, "markdown": string(runes[offset:end]), "totalChars": len(runes), "nextOffset": next, "truncated": next != 0}, nil
		}
	}
	return nil, &WikiError{404, "not_found", "wiki page revision not found"}
}

// snapshotWikiSource 保存启用库的完整来源正文版本；同内容重复处理只复用快照。
// 调用方负责随后保存返回元数据，模型不可修改这些来源文件。
func (s *AppService) snapshotWikiSource(document model.Document, content string) (model.Document, error) {
	kb, err := s.WikiSnapshot(document.KnowledgeBaseID)
	if err != nil {
		return document, err
	}
	if document.Wiki != nil || kb.Wiki == nil || !kb.Wiki.Enabled {
		return document, nil
	}
	checksum := hashDocumentContent(content)
	for _, version := range document.SourceRevisions {
		if version.Checksum == checksum {
			document.ContentChecksum = checksum
			return document, nil
		}
	}
	root := s.serverConfig.UploadDir
	if root == "" {
		root = filepath.Dir(document.Path)
	}
	path := filepath.Join(root, "wiki", document.KnowledgeBaseID, "sources", document.ID, checksum+".md")
	// 同版本并发整理时串行创建快照，避免读到另一请求尚未写完的文件。
	s.stateSaveMu.Lock()
	defer s.stateSaveMu.Unlock()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return document, err
	}
	// 中断留下的同内容快照可以复用，不能覆盖不同正文来掩盖版本冲突。
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if os.IsExist(err) {
		existing, readErr := os.ReadFile(path)
		if readErr != nil {
			return document, readErr
		}
		if string(existing) != content {
			return document, fmt.Errorf("source snapshot content mismatch")
		}
	} else if err != nil {
		return document, err
	} else {
		_, writeErr := file.WriteString(content)
		closeErr := file.Close()
		if writeErr != nil {
			return document, writeErr
		}
		if closeErr != nil {
			return document, closeErr
		}
	}
	document.Kind, document.ContentChecksum = "source", checksum
	document.SourceRevisions = append(document.SourceRevisions, model.SourceRevision{Checksum: checksum, SnapshotPath: path, CreatedAt: util.NowRFC3339()})
	return document, nil
}

// recordWikiSourceChange 将正文变化及依赖页面失效写入同一快照，随后由任务队列处理。
func (s *AppService) recordWikiSourceChange(document model.Document) error {
	if document.Wiki != nil {
		return nil
	}
	kb, err := s.WikiSnapshot(document.KnowledgeBaseID)
	if err != nil {
		return err
	}
	if kb.Wiki == nil {
		return nil
	}
	autoCompileReady := wikiAutoCompileReady(s.GetConfig().Chat)
	return s.commitWikiChange(document.KnowledgeBaseID, func(kb *model.KnowledgeBase) error {
		found := false
		for i := range kb.Documents {
			current := &kb.Documents[i]
			if current.ID == document.ID {
				current.SourceRevisions = document.SourceRevisions
				current.ContentChecksum = document.ContentChecksum
				current.Kind = "source"
				found = true
			}
			if current.Wiki != nil {
				for _, ref := range current.Wiki.SourceRefs {
					if ref.DocumentID == document.ID && ref.Checksum != document.ContentChecksum {
						current.Wiki.Freshness = "stale"
					}
				}
			}
		}
		if !found {
			return &WikiError{404, "not_found", "source was deleted"}
		}
		if kb.Wiki.Enabled && autoCompileReady {
			kb.Wiki.PendingSources[document.ID] = document.ContentChecksum
		} else {
			// 无模型时跳过本次自动整理，避免以后重启时意外补跑。
			delete(kb.Wiki.PendingSources, document.ID)
		}
		return nil
	})
}

// WikiSourceContent 只读取仍存在于本库的来源版本，并分页返回正文而非文件路径。
func (s *AppService) WikiSourceContent(kbID, documentID, checksum string, offset, limit int) (map[string]any, error) {
	kb, err := s.WikiSnapshot(kbID)
	if err != nil {
		return nil, err
	}
	if offset < 0 || limit < 0 || limit > 50000 {
		return nil, &WikiError{400, "invalid_page", "invalid text offset or limit"}
	}
	if limit == 0 {
		limit = 20000
	}
	for _, document := range kb.Documents {
		if document.ID != documentID || document.Wiki != nil {
			continue
		}
		for _, version := range document.SourceRevisions {
			if version.Checksum != checksum {
				continue
			}
			content, err := os.ReadFile(version.SnapshotPath)
			if err != nil {
				return nil, err
			}
			if hashDocumentContent(string(content)) != checksum {
				return nil, fmt.Errorf("source snapshot checksum mismatch")
			}
			runes := []rune(string(content))
			if offset > len(runes) {
				return nil, &WikiError{400, "invalid_page", "offset exceeds content length"}
			}
			end := min(offset+limit, len(runes))
			next := 0
			if end < len(runes) {
				next = end
			}
			return map[string]any{"documentId": documentID, "checksum": checksum, "name": document.Name, "content": string(runes[offset:end]), "totalChars": len(runes), "nextOffset": next, "truncated": next != 0}, nil
		}
	}
	return nil, &WikiError{404, "not_found", "source revision not found"}
}

// wikiPageUsable 同时核对开关、页面修订及每个原始来源，旧索引不能冒充当前版本。
func wikiPageUsable(kb model.KnowledgeBase, page model.Document, revision int) bool {
	if kb.Wiki == nil || !kb.Wiki.Enabled || page.Wiki == nil || page.Wiki.Freshness != "fresh" || page.Wiki.Revision != revision || page.Wiki.IndexedRevision != revision || page.Status != "indexed" || len(page.Wiki.SourceRefs) == 0 {
		return false
	}
	for _, ref := range page.Wiki.SourceRefs {
		found := false
		for _, doc := range kb.Documents {
			if doc.ID == ref.DocumentID && doc.Wiki == nil && doc.ContentChecksum == ref.Checksum {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// filterWikiChunks 在候选及最终缓存命中后校验页面；最终返回前核实完整原文与引文。
func (s *AppService) filterWikiChunks(chunks []RetrievedChunk, evidence bool) []RetrievedChunk {
	result := make([]RetrievedChunk, 0, len(chunks))
	for _, chunk := range chunks {
		kb, err := s.WikiSnapshot(chunk.KnowledgeBaseID)
		if err != nil {
			if chunk.Kind != "wiki" && chunk.IndexGeneration == "" {
				result = append(result, chunk)
			}
			continue
		}
		var doc model.Document
		for _, d := range kb.Documents {
			if d.ID == chunk.DocumentID {
				doc = d
				break
			}
		}
		if chunk.IndexGeneration != doc.IndexGeneration {
			continue
		}
		if doc.Wiki == nil {
			if chunk.Kind == "wiki" {
				continue
			}
			if chunk.SourceChecksum != "" && doc.ContentChecksum != chunk.SourceChecksum {
				continue
			}
			if evidence && doc.ID != "" {
				chunk.SourceChecksum = doc.ContentChecksum
			}
			result = append(result, chunk)
			continue
		}
		if !wikiPageUsable(kb, doc, chunk.WikiRevision) {
			continue
		}
		valid := true
		if evidence {
			for _, ref := range doc.Wiki.SourceRefs {
				for _, source := range kb.Documents {
					if source.ID != ref.DocumentID {
						continue
					}
					body, err := util.ExtractDocumentText(source.Path)
					if err != nil || hashDocumentContent(body) != ref.Checksum || !strings.Contains(body, ref.Quote) {
						valid = false
					}
				}
			}
		}
		if valid {
			chunk.Kind = "wiki"
			chunk.SourceRefs = doc.Wiki.SourceRefs
			result = append(result, chunk)
		}
	}
	return result
}

// wikiAutoCompileReady 仅在第三方聊天连接完整时自动整理，默认 Ollama 不触发后台请求。
// 不要求 APIKey，以兼容无需密钥的模型网关；实际调用失败仍由任务记录。
func wikiAutoCompileReady(config model.ChatConfig) bool {
	provider := strings.TrimSpace(config.Provider)
	if provider == "" || strings.EqualFold(provider, "ollama") {
		return false
	}
	return strings.TrimSpace(config.BaseURL) != "" && strings.TrimSpace(config.Model) != ""
}
