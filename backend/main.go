package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"ai-localbase/internal/config"
	"ai-localbase/internal/handler"
	"ai-localbase/internal/mcp"
	"ai-localbase/internal/model"
	"ai-localbase/internal/router"
	"ai-localbase/internal/service"
)

func main() {
	serverConfig := config.LoadServerConfig()

	if err := validateAuthConfig(serverConfig); err != nil {
		log.Fatal(err)
	}

	if err := os.MkdirAll(serverConfig.UploadDir, 0o755); err != nil {
		log.Fatalf("failed to create upload directory: %v", err)
	}
	if strings.TrimSpace(serverConfig.StagingDir) != "" {
		if err := os.MkdirAll(serverConfig.StagingDir, 0o755); err != nil {
			log.Fatalf("failed to create staging directory: %v", err)
		}
	}

	qdrantService := service.NewQdrantService(serverConfig)
	if qdrantService != nil && qdrantService.IsEnabled() {
		if err := qdrantService.Ping(context.Background()); err != nil {
			log.Printf("qdrant ping failed: %v", err)
		} else {
			log.Printf("qdrant connected: %s", serverConfig.QdrantURL)
		}
	}

	stateStore := service.NewAppStateStore(serverConfig.StateFile)
	chatHistoryStore, err := service.NewSQLiteChatHistoryStore(serverConfig.ChatHistoryFile)
	if err != nil {
		log.Fatalf("failed to initialize sqlite chat history store: %v", err)
	}
	defer func() {
		if closeErr := chatHistoryStore.Close(); closeErr != nil {
			log.Printf("failed to close sqlite chat history store: %v", closeErr)
		}
	}()

	appService := service.NewAppService(qdrantService, stateStore, chatHistoryStore, serverConfig)
	startUploadStagingCleanup(appService)
	authService, err := service.NewAuthService(appService, serverConfig)
	if err != nil {
		log.Fatalf("failed to initialize auth service: %v", err)
	}
	if serverConfig.EnableAuth {
		bootstrap := authService.Bootstrap()
		if bootstrap.SetupRequired {
			log.Printf("Authentication enabled, setup required for username: %s", bootstrap.Username)
		} else {
			log.Printf("Authentication enabled, username: %s", bootstrap.Username)
		}
	}
	llmService := service.NewLLMService()
	mcpRegistry := mcp.DefaultRegistry(appService)
	appHandler := handler.NewAppHandler(serverConfig, appService, llmService)
	configHandler := handler.NewConfigHandler(appService, qdrantService)
	authHandler := handler.NewAuthHandler(authService, serverConfig.EnableAuth)
	mcpServer := mcp.NewServer(mcpRegistry, appService, authService, serverConfig)
	r := router.NewRouter(appHandler, configHandler, authHandler, authService, serverConfig, mcpServer, frontendFS())

	log.Printf("backend server listening on :%s", serverConfig.Port)
	if err := r.Run(":" + serverConfig.Port); err != nil {
		log.Fatalf("failed to start server: %v", err)
	}
}

func validateAuthConfig(serverConfig model.ServerConfig) error {
	if !serverConfig.EnableAuth {
		return nil
	}
	hasResetToken := strings.TrimSpace(serverConfig.AuthResetToken) != ""
	hasResetPassword := strings.TrimSpace(serverConfig.AuthResetPassword) != ""
	if hasResetToken != hasResetPassword {
		return fmt.Errorf("AUTH_RESET_TOKEN and AUTH_RESET_PASSWORD must be set together")
	}
	return nil
}

func startUploadStagingCleanup(appService *service.AppService) {
	if appService == nil {
		return
	}
	if err := appService.CleanupUploadStaging(); err != nil {
		log.Printf("failed to clean upload staging directory at startup: %v", err)
	}

	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			if err := appService.CleanupUploadStaging(); err != nil {
				log.Printf("failed to clean upload staging directory: %v", err)
			}
		}
	}()
}
