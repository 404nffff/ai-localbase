package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"strings"
	"time"

	"ai-localbase/internal/config"
	"ai-localbase/internal/service"
)

func main() {
	sourcePrefix := flag.String("source-prefix", "", "source Qdrant collection prefix")
	knowledgeBaseID := flag.String("kb", "", "knowledge base id; empty migrates all knowledge bases")
	flag.Parse()

	serverConfig := config.LoadServerConfig()
	if strings.TrimSpace(*sourcePrefix) == "" {
		log.Fatal("source-prefix is required")
	}
	if strings.TrimSpace(*sourcePrefix) == strings.TrimSpace(serverConfig.QdrantCollectionPrefix) {
		log.Fatal("source and target collection prefixes must differ")
	}

	target := service.NewQdrantService(serverConfig)
	sourceConfig := serverConfig
	sourceConfig.QdrantCollectionPrefix = strings.TrimSpace(*sourcePrefix)
	source := service.NewQdrantService(sourceConfig)
	appService := service.NewAppService(target, service.NewAppStateStore(serverConfig.StateFile), nil, serverConfig)
	rag := service.NewRagService()
	embeddingConfig := appService.CurrentEmbeddingConfig()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	requestedKnowledgeBaseID := strings.TrimSpace(*knowledgeBaseID)
	foundKnowledgeBase := false
	total := 0
	for _, knowledgeBase := range appService.ListKnowledgeBases() {
		if requestedKnowledgeBaseID != "" && requestedKnowledgeBaseID != knowledgeBase.ID {
			continue
		}
		foundKnowledgeBase = true
		count, err := service.MigrateQdrantPayloads(ctx, source, target, rag, embeddingConfig, knowledgeBase.ID)
		if err != nil {
			log.Fatalf("migrate knowledge base %s: %v", knowledgeBase.ID, err)
		}
		fmt.Printf("migrated knowledge base %s: %d points\n", knowledgeBase.ID, count)
		total += count
	}
	if requestedKnowledgeBaseID != "" && !foundKnowledgeBase {
		log.Fatalf("knowledge base %s was not found", requestedKnowledgeBaseID)
	}
	if requestedKnowledgeBaseID != "" && total == 0 {
		log.Fatalf("knowledge base %s has no source points", requestedKnowledgeBaseID)
	}
	fmt.Printf("migration complete: %d points\n", total)
}
