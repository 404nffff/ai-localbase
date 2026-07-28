package handler

import "testing"

func TestCalibrateCitationSourcesFiltersUnrelatedContextCandidates(t *testing.T) {
	sources := []map[string]string{
		{
			"knowledgeBaseId": "kb-1",
			"documentId":      "doc-unrelated",
			"documentName":    "deploy.md",
			"chunkId":         "chunk-unrelated",
			"score":           "0.9400",
			"snippet":         "这是一段关于部署参数、缓存策略和服务端口的文档。",
		},
		{
			"knowledgeBaseId": "kb-1",
			"documentId":      "doc-related",
			"documentName":    "school.md",
			"chunkId":         "chunk-related",
			"score":           "0.8200",
			"snippet":         "武汉大学校长为张三，学校治理结构稳定。",
		},
	}

	filtered := calibrateCitationSources("武汉大学校长是谁", "武汉大学校长是张三。", sources, "kb-1", "")
	if len(filtered) != 1 {
		t.Fatalf("expected one calibrated source, got %#v", filtered)
	}
	if filtered[0]["documentId"] != "doc-related" {
		t.Fatalf("expected related citation source, got %#v", filtered[0])
	}
	if filtered[0]["citationConfidence"] != "" {
		t.Fatalf("expected no synthetic confidence marker, got %#v", filtered[0])
	}
}

func TestCalibrateCitationSourcesDropsSourcesWhenAnswerHasNoEvidenceOverlap(t *testing.T) {
	sources := []map[string]string{{
		"knowledgeBaseId": "kb-1",
		"documentId":      "doc-unrelated",
		"documentName":    "deploy.md",
		"chunkId":         "chunk-unrelated",
		"score":           "0.9800",
		"snippet":         "系统部署参数、缓存策略和服务端口。",
	}}

	filtered := calibrateCitationSources("武汉大学校长是谁", "未找到可靠证据说明武汉大学校长是谁。", sources, "kb-1", "")
	if len(filtered) != 0 {
		t.Fatalf("expected no calibrated sources, got %#v", filtered)
	}
}

func TestCalibrateCitationSourcesDropsNonDocumentAndIncompleteSources(t *testing.T) {
	sources := []map[string]string{
		{
			"toolName": "search_knowledge_base",
			"status":   "ok",
		},
		{
			"knowledgeBaseId": "kb-1",
			"documentId":      "doc-table",
			"documentName":    "teachers.csv",
			"chunkId":         "chunk-table",
		},
	}

	filtered := calibrateCitationSources("谁的薪资最高", "张三的薪资最高，为 24000。", sources, "kb-1", "doc-table")
	if len(filtered) != 0 {
		t.Fatalf("expected non-document and incomplete sources to be dropped, got %#v", filtered)
	}
}

func TestCalibrateCitationSourcesDropsWrongScopeAndGenericOverlap(t *testing.T) {
	sources := []map[string]string{
		{
			"knowledgeBaseId": "kb-other",
			"documentId":      "doc-novel",
			"documentName":    "作品大纲.md",
			"chunkId":         "chunk-novel",
			"score":           "0.9900",
			"snippet":         "林译是破晓小队的核心成员。",
		},
		{
			"knowledgeBaseId": "kb-school",
			"documentId":      "doc-school",
			"documentName":    "武汉大学简介.pdf",
			"chunkId":         "chunk-school",
			"score":           "0.2660",
			"snippet":         "武汉大学与世界上多个国家和地区的大学、科研机构建立了合作关系。",
		},
	}

	answer := "《墟心》的主角林译是破晓小队的领袖，拥有读取深层心理意图的能力，并在世界危机中成长。"
	filtered := calibrateCitationSources("详细介绍", answer, sources, "kb-school", "")
	if len(filtered) != 0 {
		t.Fatalf("expected no citation for an answer unsupported by the selected knowledge base, got %#v", filtered)
	}
}

func TestCalibrateCitationSourcesKeepsShortChineseEntityEvidence(t *testing.T) {
	sources := []map[string]string{{
		"knowledgeBaseId": "kb-school",
		"documentId":      "doc-school",
		"documentName":    "学校简介.pdf",
		"chunkId":         "chunk-school",
		"score":           "0.8200",
		"snippet":         "学校现任校长为张三，负责学校行政工作。",
	}}

	filtered := calibrateCitationSources("校长是谁", "校长是张三。", sources, "kb-school", "")
	if len(filtered) != 1 || filtered[0]["documentId"] != "doc-school" {
		t.Fatalf("expected short Chinese entity evidence to remain citable, got %#v", filtered)
	}
}
