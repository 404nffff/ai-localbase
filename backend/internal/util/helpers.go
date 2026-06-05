package util

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var idCounters sync.Map

func NextID(prefix string) string {
	id := idCounterForPrefix(prefix).Add(1)
	return fmt.Sprintf("%s-%d", prefix, id)
}

// ObserveID 用持久化状态中的现有 ID 推进计数器，避免服务重启后再次分配旧编号。
func ObserveID(id string) {
	normalizedID := strings.TrimSpace(id)
	index := strings.LastIndex(normalizedID, "-")
	if index <= 0 || index == len(normalizedID)-1 {
		return
	}

	prefix := normalizedID[:index]
	sequence, err := strconv.ParseUint(normalizedID[index+1:], 10, 64)
	if err != nil {
		return
	}

	counter := idCounterForPrefix(prefix)
	for {
		current := counter.Load()
		if sequence <= current {
			return
		}
		if counter.CompareAndSwap(current, sequence) {
			return
		}
	}
}

func idCounterForPrefix(prefix string) *atomic.Uint64 {
	normalizedPrefix := strings.TrimSpace(prefix)
	if normalizedPrefix == "" {
		normalizedPrefix = "id"
	}
	counter, _ := idCounters.LoadOrStore(normalizedPrefix, &atomic.Uint64{})
	return counter.(*atomic.Uint64)
}

func NowUnixNano() int64 {
	return time.Now().UnixNano()
}

func NowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339)
}

func FormatFileSize(size int64) string {
	if size < 1024 {
		return fmt.Sprintf("%d B", size)
	}

	if size < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(size)/1024)
	}

	return fmt.Sprintf("%.1f MB", float64(size)/(1024*1024))
}

func ExtractContentPreview(path string) string {
	content, err := ExtractDocumentText(path)
	if err != nil {
		return "暂未生成摘要"
	}

	return BuildContentPreviewFromText(content)
}

// KnowledgeBaseUploadDir 返回某个知识库的原始上传文件目录，避免不同知识库文件混在同一层。
func KnowledgeBaseUploadDir(uploadDir, knowledgeBaseID string) string {
	return filepath.Join(uploadDir, SanitizeFilename(knowledgeBaseID))
}

// BuildKnowledgeBaseUploadPath 生成知识库隔离后的上传文件完整路径。
func BuildKnowledgeBaseUploadPath(uploadDir, knowledgeBaseID, storedName string) string {
	return filepath.Join(KnowledgeBaseUploadDir(uploadDir, knowledgeBaseID), storedName)
}

func SanitizeFilename(name string) string {
	name = filepath.Base(name)
	name = strings.ReplaceAll(name, " ", "_")
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= 'A' && r <= 'Z':
			return r
		case r >= '0' && r <= '9':
			return r
		case r == '.', r == '-', r == '_':
			return r
		default:
			return '_'
		}
	}, name)
}
