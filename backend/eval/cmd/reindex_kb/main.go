package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"ai-localbase/internal/config"
	"ai-localbase/internal/service"
)

func main() {
	knowledgeBaseID := flag.String("kb", "kb-1", "需要重建索引的 knowledge base id")
	flag.Parse()

	serverConfig := config.LoadServerConfig()
	if err := os.MkdirAll(serverConfig.UploadDir, 0o755); err != nil {
		log.Fatalf("create upload dir: %v", err)
	}

	stateStore := service.NewAppStateStore(serverConfig.StateFile)
	qdrantService := service.NewQdrantService(serverConfig)
	appService := service.NewAppService(qdrantService, stateStore, nil, serverConfig)

	documents, err := appService.ReindexKnowledgeBase(*knowledgeBaseID)
	if err != nil {
		log.Fatalf("reindex knowledge base %s failed: %v", *knowledgeBaseID, err)
	}

	fmt.Printf("reindexed knowledge base %s with %d documents\n", *knowledgeBaseID, len(documents))
	for _, document := range documents {
		fmt.Printf("- %s (%s) status=%s path=%s\n", document.ID, document.Name, document.Status, document.Path)
	}
}
