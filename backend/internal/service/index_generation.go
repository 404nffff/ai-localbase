package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	"ai-localbase/internal/model"
	"ai-localbase/internal/util"
)

// indexGenerationIntent 在写入向量之前落盘，重启后据已发布指针决定清理方向。
type indexGenerationIntent struct {
	KnowledgeBaseID string
	DocumentID      string
	Previous        string
	Candidate       string
	Existing        bool
}

// lockDocument 串行同一文档的正文改写、重建和删除，不阻塞其他文档。
func (s *AppService) lockDocument(kbID, docID string) func() {
	value, _ := s.documentLocks.LoadOrStore(kbID+"/"+docID, &sync.Mutex{})
	mu := value.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

// AppendDocumentContent 在同一锁内读取最新正文再追加，防止两个客户端相互覆盖追加内容。
func (s *AppService) AppendDocumentContent(kbID, docID, addition string) (model.Document, error) {
	unlock := s.lockDocument(kbID, docID)
	defer unlock()
	document, err := s.GetDocument(kbID, docID)
	if err != nil {
		return model.Document{}, err
	}
	body, err := os.ReadFile(document.Path)
	if err != nil {
		return model.Document{}, fmt.Errorf("read document for append: %w", err)
	}
	return s.rewriteDocumentContent(kbID, docID, string(body)+addition)
}

// writeDurableJSON 原子保存恢复记录；文件同步失败不能视为已登记。
func writeDurableJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	body, err := json.Marshal(value)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".pending-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(body); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

// beginIndexGeneration 先登记候选代际；无状态存储的测试实例只在内存中隔离。
func (s *AppService) beginIndexGeneration(document *model.Document, existing bool) (func(), error) {
	intent := indexGenerationIntent{document.KnowledgeBaseID, document.ID, document.IndexGeneration, fmt.Sprintf("%d-%s", time.Now().UnixNano(), util.NextID("gen")), existing}
	path := ""
	if s.store != nil && s.store.Path() != "" {
		path = filepath.Join(s.store.Path()+".indexes", intent.Candidate+".json")
		if err := writeDurableJSON(path, intent); err != nil {
			return nil, fmt.Errorf("persist index intent: %w", err)
		}
	}
	document.IndexGeneration = intent.Candidate
	return func() {
		if err := s.cleanupIndexGeneration(intent); err != nil {
			log.Printf("index cleanup pending: %v", err)
			return
		}
		if path != "" {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				log.Printf("remove index intent: %v", err)
			}
		}
	}, nil
}

// generationFilter 兼容历史无代际字段的 points，绝不按整个文档清理新版本。
func generationFilter(docID, generation string) map[string]any {
	condition := map[string]any{"should": []any{
		map[string]any{"is_empty": map[string]any{"key": "index_generation"}},
		map[string]any{"key": "index_generation", "match": map[string]any{"value": ""}},
	}}
	if generation != "" {
		condition = map[string]any{"key": "index_generation", "match": map[string]any{"value": generation}}
	}
	return map[string]any{"must": []any{map[string]any{"key": "document_id", "match": map[string]any{"value": docID}}, condition}}
}

// cleanupIndexGeneration 以持久发布结果为准回收旧代或未发布候选；失败保留日志待恢复。
func (s *AppService) cleanupIndexGeneration(intent indexGenerationIntent) error {
	if s.qdrant == nil || !s.qdrant.IsEnabled() {
		// 停用只能推迟清理，不能证明远端旧代不存在；调用方保留日志供重新启用后恢复。
		return fmt.Errorf("index cleanup deferred: qdrant is disabled")
	}
	doc, lookupErr := s.GetDocument(intent.KnowledgeBaseID, intent.DocumentID)
	generations := []string{intent.Candidate}
	if intent.Existing {
		generations = append(generations, intent.Previous)
	}
	for _, generation := range generations {
		if lookupErr == nil && doc.IndexGeneration == generation {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, err := s.qdrant.doJSON(ctx, http.MethodPost, "/collections/"+url.PathEscape(s.qdrant.CollectionName(intent.KnowledgeBaseID))+"/points/delete?wait=true", map[string]any{"filter": generationFilter(intent.DocumentID, generation)})
		cancel()
		if err != nil && !isQdrantNotFound(err) {
			return err
		}
	}
	return nil
}

// recoverIndexGenerations 启动时恢复精确清理；损坏记录和网络失败显式留痕，不删除恢复证据。
func (s *AppService) recoverIndexGenerations() {
	if s.store == nil || s.store.Path() == "" {
		return
	}
	paths, err := filepath.Glob(filepath.Join(s.store.Path()+".indexes", "*.json"))
	if err != nil {
		log.Printf("list index intents: %v", err)
		return
	}
	for _, path := range paths {
		body, err := os.ReadFile(path)
		var intent indexGenerationIntent
		if err == nil {
			err = json.Unmarshal(body, &intent)
		}
		if err == nil && (intent.DocumentID == "" || intent.KnowledgeBaseID == "" || intent.Candidate == "") {
			err = fmt.Errorf("invalid index intent")
		}
		if err == nil {
			err = s.cleanupIndexGeneration(intent)
		}
		if err == nil {
			err = os.Remove(path)
		}
		if err != nil {
			log.Printf("recover index cleanup %s: %v", filepath.Base(path), err)
		}
	}
}

// publishIndexedDocument 先保存候选快照再切换内存，禁止丢失知识库或重复导入后仍返回成功。
func (s *AppService) publishIndexedDocument(document, previous model.Document) (model.Document, error) {
	if guard, ok := s.indexPublishGuards.Load(document.ID); ok {
		unlock, err := guard.(func() (func(), error))()
		if err != nil {
			return model.Document{}, err
		}
		defer unlock()
	}
	err := s.commitWikiChange(document.KnowledgeBaseID, func(kb *model.KnowledgeBase) error {
		for i, current := range kb.Documents {
			if current.ID == document.ID {
				if previous.ID == "" || current.IndexGeneration != previous.IndexGeneration {
					return fmt.Errorf("document changed during indexing")
				}
				kb.Documents[i] = document
				return nil
			}
			if current.ContentChecksum == document.ContentChecksum && document.ContentChecksum != "" {
				return &DuplicateDocumentError{Existing: current}
			}
		}
		if previous.ID != "" {
			return fmt.Errorf("document deleted during indexing")
		}
		kb.Documents = append([]model.Document{document}, kb.Documents...)
		if kb.Wiki.Enabled && wikiAutoCompileReady(s.state.Config.Chat) {
			kb.Wiki.PendingSources[document.ID] = document.ContentChecksum
		}
		return nil
	})
	return document, err
}
