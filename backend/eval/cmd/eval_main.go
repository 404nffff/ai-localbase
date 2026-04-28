package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"ai-localbase/eval/offline"
	"ai-localbase/eval/report"
	"ai-localbase/internal/config"
	"ai-localbase/internal/model"
	"ai-localbase/internal/service"
)

type realEvalRuntime struct {
	appService      *service.AppService
	llmService      *service.LLMService
	casesByQuestion map[string]offline.GroundTruthCase
	realLLM         bool
}

func main() {
	var (
		dataset      = flag.String("dataset", "eval/data/ground_truth_v1.small.json", "评估数据集 JSON 文件路径")
		outputDir    = flag.String("output", "eval/results", "报告输出目录")
		hitThreshold = flag.Float64("hit-threshold", 0.5, "命中文本匹配阈值 (0-1)")
		mockMode     = flag.Bool("mock", true, "使用 mock 检索和生成函数（用于 CI/测试）")
		realLLM      = flag.Bool("real-llm", false, "真实模式下调用真实 LLM 生成答案")
	)
	flag.Parse()

	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Printf("[eval] 开始评估，数据集: %s", *dataset)

	ds, err := offline.LoadDataset(*dataset)
	if err != nil {
		log.Fatalf("[eval] 加载数据集失败: %v", err)
	}
	if err := ds.Validate(); err != nil {
		log.Fatalf("[eval] 数据集验证失败: %v", err)
	}
	log.Printf("[eval] 已加载 %d 个用例", len(ds.Cases))

	var retrievalFn offline.RetrievalFunc
	var generationFn offline.GenerationFunc
	runIDPrefix := "eval"

	if *mockMode {
		log.Println("[eval] 使用 mock 模式")
		retrievalFn = mockRetrieval
		generationFn = mockGeneration
	} else {
		log.Printf("[eval] 使用真实模式，real-llm=%v", *realLLM)
		runtime, err := newRealEvalRuntime(context.Background(), ds, *realLLM)
		if err != nil {
			log.Fatalf("[eval] 初始化真实评估模式失败: %v", err)
		}
		log.Printf("[eval] 真实模式配置: evalKnowledgeBaseID=%q retrieval(topKDoc=%d candidateDoc=%d topKKB=%d candidateAll=%d perDocLimit=%d autoExpand=%v)",
			runtime.appService.ServerConfig().EvalKnowledgeBaseID,
			runtime.appService.ServerConfig().RetrievalTopKDocument,
			runtime.appService.ServerConfig().RetrievalCandidateTopKDocument,
			runtime.appService.ServerConfig().RetrievalTopKKnowledgeBase,
			runtime.appService.ServerConfig().RetrievalCandidateTopKAllDocs,
			runtime.appService.ServerConfig().RetrievalMaxChunksPerDocument,
			runtime.appService.ServerConfig().RetrievalEnableAutoExpand,
		)
		retrievalFn = runtime.retrieval
		generationFn = runtime.generation
		runIDPrefix = "baseline"
	}

	cfg := offline.EvaluatorConfig{
		HitThreshold:   *hitThreshold,
		MaxConcurrency: 1,
	}
	evaluator := offline.NewEvaluator(retrievalFn, generationFn, cfg)

	ctx := context.Background()
	results, err := evaluator.Run(ctx, ds)
	if err != nil {
		log.Fatalf("[eval] 评估运行失败: %v", err)
	}
	log.Printf("[eval] 评估完成，共 %d 个用例", len(results))

	runID := fmt.Sprintf("%s-%s", runIDPrefix, time.Now().Format("20060102-150405"))
	rpt := report.BuildReport(runID, *dataset, results, ds)

	jsonPath := filepath.Join(*outputDir, runID+".json")
	if err := rpt.WriteJSON(jsonPath); err != nil {
		log.Fatalf("[eval] 写入 JSON 报告失败: %v", err)
	}
	log.Printf("[eval] JSON 报告已写入: %s", jsonPath)

	mdPath := filepath.Join(*outputDir, runID+".md")
	if err := rpt.WriteMarkdown(mdPath); err != nil {
		log.Fatalf("[eval] 写入 Markdown 报告失败: %v", err)
	}
	log.Printf("[eval] Markdown 报告已写入: %s", mdPath)

	printSummary(rpt)

	if rpt.Metrics.HitRate < 0.5 {
		log.Printf("[eval] 警告: HitRate=%.2f%% 低于 50%%，评估不通过", rpt.Metrics.HitRate*100)
		os.Exit(1)
	}
}

func newRealEvalRuntime(ctx context.Context, ds *offline.Dataset, realLLM bool) (*realEvalRuntime, error) {
	serverConfig := config.LoadServerConfig()
	if err := os.MkdirAll(serverConfig.UploadDir, 0o755); err != nil {
		return nil, fmt.Errorf("创建上传目录失败: %w", err)
	}

	stateStore := service.NewAppStateStore(serverConfig.StateFile)
	loadedState, err := stateStore.Load()
	if err != nil {
		return nil, fmt.Errorf("读取 app-state 失败 (%s): %w", serverConfig.StateFile, err)
	}
	if loadedState == nil {
		return nil, fmt.Errorf("app-state 不存在: %s", serverConfig.StateFile)
	}
	if len(loadedState.KnowledgeBases) == 0 {
		return nil, fmt.Errorf("app-state 中不存在可用知识库: %s", serverConfig.StateFile)
	}

	qdrantService := service.NewQdrantService(serverConfig)
	if qdrantService == nil || !qdrantService.IsEnabled() {
		return nil, fmt.Errorf("Qdrant 未启用，请检查配置 QDRANT_URL=%q", serverConfig.QdrantURL)
	}
	if err := qdrantService.Ping(ctx); err != nil {
		return nil, fmt.Errorf("Qdrant 不可用 (%s): %w", serverConfig.QdrantURL, err)
	}
	log.Printf("[eval] qdrant connected: %s", serverConfig.QdrantURL)

	appService := service.NewAppService(qdrantService, stateStore, nil, serverConfig)
	if _, err := appService.ResolveKnowledgeBaseID(""); err != nil {
		return nil, fmt.Errorf("未找到可用于评估的知识库: %w", err)
	}

	casesByQuestion := make(map[string]offline.GroundTruthCase, len(ds.Cases))
	for _, gtCase := range ds.Cases {
		if _, exists := casesByQuestion[gtCase.Question]; !exists {
			casesByQuestion[gtCase.Question] = gtCase
		}
	}

	return &realEvalRuntime{
		appService:      appService,
		llmService:      service.NewLLMService(),
		casesByQuestion: casesByQuestion,
		realLLM:         realLLM,
	}, nil
}

func (r *realEvalRuntime) retrieval(ctx context.Context, question string) ([]offline.RetrievedChunkInfo, time.Duration, error) {
	startedAt := time.Now()
	gtCase, ok := r.casesByQuestion[question]
	if !ok {
		return nil, time.Since(startedAt), fmt.Errorf("数据集中未找到问题对应的用例: %s", question)
	}

	knowledgeBaseID, err := r.resolveKnowledgeBaseID(gtCase)
	if err != nil {
		return nil, time.Since(startedAt), err
	}

	req := model.ChatCompletionRequest{
		KnowledgeBaseID: knowledgeBaseID,
		Messages: []model.ChatMessage{{
			Role:    "user",
			Content: question,
		}},
		Embedding: r.appService.CurrentEmbeddingConfig(),
	}

	chunks, err := r.appService.EvaluateRetrieve(req)
	latency := time.Since(startedAt)
	if err != nil {
		return nil, latency, fmt.Errorf("真实检索失败 (kb=%s): %w", knowledgeBaseID, err)
	}

	result := make([]offline.RetrievedChunkInfo, 0, len(chunks))
	for _, chunk := range chunks {
		result = append(result, offline.RetrievedChunkInfo{
			DocumentID: chunk.DocumentID,
			ChunkID:    chunk.ID,
			Text:       chunk.Text,
			Score:      chunk.Score,
		})
	}
	return result, latency, nil
}

func (r *realEvalRuntime) generation(ctx context.Context, question string, chunks []offline.RetrievedChunkInfo) (string, time.Duration, error) {
	startedAt := time.Now()
	if !r.realLLM {
		return buildSummaryAnswer(question, chunks), time.Since(startedAt), nil
	}

	chatConfig := r.appService.CurrentChatConfig()
	if strings.TrimSpace(chatConfig.Model) == "" {
		answer := buildSummaryAnswer(question, chunks) + "\n\n[degraded] 未配置 Chat 模型，已回退为检索摘要回答。"
		return answer, time.Since(startedAt), nil
	}

	prompt := buildRealLLMPrompt(question, chunks)
	resp, err := r.llmService.Chat(model.ChatCompletionRequest{
		Messages: []model.ChatMessage{{Role: "user", Content: prompt}},
		Config:   chatConfig,
	})
	latency := time.Since(startedAt)
	if err != nil {
		answer := buildSummaryAnswer(question, chunks) + fmt.Sprintf("\n\n[degraded] LLM 调用失败，已回退为检索摘要回答：%v", err)
		return answer, latency, nil
	}
	if len(resp.Choices) == 0 {
		answer := buildSummaryAnswer(question, chunks) + "\n\n[degraded] LLM 返回空结果，已回退为检索摘要回答。"
		return answer, latency, nil
	}

	answer := strings.TrimSpace(resp.Choices[0].Message.Content)
	if answer == "" {
		answer = buildSummaryAnswer(question, chunks)
	}
	if degraded, _ := resp.Metadata["degraded"].(bool); degraded {
		if upstream, _ := resp.Metadata["upstreamError"].(string); strings.TrimSpace(upstream) != "" {
			answer += "\n\n[degraded] " + upstream
		} else {
			answer += "\n\n[degraded] 已使用本地降级响应。"
		}
	}
	return answer, latency, nil
}

func (r *realEvalRuntime) resolveKnowledgeBaseID(gtCase offline.GroundTruthCase) (string, error) {
	if configured := strings.TrimSpace(r.appService.ServerConfig().EvalKnowledgeBaseID); configured != "" {
		return r.appService.ResolveKnowledgeBaseID(configured)
	}
	if len(gtCase.SourceDocuments) > 0 {
		candidate := strings.TrimSpace(gtCase.SourceDocuments[0].KnowledgeBaseID)
		if candidate != "" {
			return r.appService.ResolveKnowledgeBaseID(candidate)
		}
	}
	if kbID, err := r.appService.ResolveKnowledgeBaseID("kb-1"); err == nil {
		return kbID, nil
	}
	return r.appService.ResolveKnowledgeBaseID("")
}

func buildSummaryAnswer(question string, chunks []offline.RetrievedChunkInfo) string {
	if len(chunks) == 0 {
		return fmt.Sprintf("基于检索上下文的摘要回答：问题“%s”未检索到可用上下文。", question)
	}

	lines := make([]string, 0, minInt(len(chunks), 3)+1)
	lines = append(lines, fmt.Sprintf("基于检索上下文的摘要回答：问题“%s”的相关内容如下。", question))
	for i, chunk := range chunks {
		if i >= 3 {
			break
		}
		text := strings.TrimSpace(chunk.Text)
		if len([]rune(text)) > 180 {
			text = string([]rune(text)[:180]) + "..."
		}
		lines = append(lines, fmt.Sprintf("%d. [doc=%s chunk=%s score=%.4f] %s", i+1, chunk.DocumentID, chunk.ChunkID, chunk.Score, text))
	}
	return strings.Join(lines, "\n")
}

func buildRealLLMPrompt(question string, chunks []offline.RetrievedChunkInfo) string {
	if len(chunks) == 0 {
		return fmt.Sprintf("请直接回答用户问题。如果缺少上下文，请明确说明。\n问题：%s", question)
	}

	parts := make([]string, 0, len(chunks))
	for i, chunk := range chunks {
		parts = append(parts, fmt.Sprintf("[%d][doc=%s chunk=%s score=%.4f]\n%s", i+1, chunk.DocumentID, chunk.ChunkID, chunk.Score, chunk.Text))
	}
	return fmt.Sprintf("请严格基于以下检索上下文回答问题；如果上下文不足，请明确说明，不要编造。\n\n问题：%s\n\n检索上下文：\n%s", question, strings.Join(parts, "\n\n"))
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func printSummary(rpt *report.Report) {
	fmt.Println()
	fmt.Println("====== RAG 评估摘要 ======")
	fmt.Printf("RunID:          %s\n", rpt.RunID)
	fmt.Printf("总用例数:       %d\n", rpt.Metrics.TotalCases)
	fmt.Printf("命中率:         %.2f%%\n", rpt.Metrics.HitRate*100)
	fmt.Printf("MRR:            %.4f\n", rpt.Metrics.MRR)
	fmt.Printf("检索时延 P50:   %.0fms\n", rpt.Metrics.RetrievalLatencyP50Ms)
	fmt.Printf("检索时延 P95:   %.0fms\n", rpt.Metrics.RetrievalLatencyP95Ms)
	fmt.Printf("生成时延 P50:   %.0fms\n", rpt.Metrics.GenerationLatencyP50Ms)
	fmt.Printf("生成时延 P95:   %.0fms\n", rpt.Metrics.GenerationLatencyP95Ms)
	fmt.Println("=========================")
}

func mockRetrieval(ctx context.Context, question string) ([]offline.RetrievedChunkInfo, time.Duration, error) {
	latency := 10 * time.Millisecond
	chunks := []offline.RetrievedChunkInfo{
		{
			ChunkID:    "mock-chunk-1",
			DocumentID: "mock-doc-1",
			Text:       "这是一个模拟检索结果，用于测试评估框架。" + question,
			Score:      0.85,
		},
	}
	return chunks, latency, nil
}

func mockGeneration(ctx context.Context, question string, chunks []offline.RetrievedChunkInfo) (string, time.Duration, error) {
	latency := 50 * time.Millisecond
	answer := fmt.Sprintf("这是关于 '%s' 的模拟回答。", question)
	return answer, latency, nil
}
