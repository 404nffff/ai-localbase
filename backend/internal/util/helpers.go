package util

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

var idCounter atomic.Uint64

func NextID(prefix string) string {
	id := idCounter.Add(1)
	return fmt.Sprintf("%s-%d", prefix, id)
}

// ObserveID 用持久化状态中的现有 ID 推进计数器，避免服务重启后再次分配旧编号。
func ObserveID(id string) {
	parts := strings.Split(strings.TrimSpace(id), "-")
	if len(parts) < 2 {
		return
	}

	sequence, err := strconv.ParseUint(parts[len(parts)-1], 10, 64)
	if err != nil {
		return
	}

	for {
		current := idCounter.Load()
		if sequence <= current {
			return
		}
		if idCounter.CompareAndSwap(current, sequence) {
			return
		}
	}
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
