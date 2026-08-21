package service

import (
	"strings"
	"testing"
	"time"

	"ai-localbase/internal/model"
)

func TestMCPDangerConfirmationCannotCrossAPIKeyEvenForAdmin(t *testing.T) {
	appService := &AppService{
		mcpDangerConfirms: map[string]mcpDangerConfirmationRecord{},
	}
	arguments := map[string]any{"id": "conversation-1"}
	confirmation, err := appService.CreateMCPDangerConfirmationAs(model.MCPDangerConfirmationRequest{
		ToolName:  "delete_conversation",
		Arguments: arguments,
	}, AuthPrincipal{AuthType: "api_key", UserID: "root-user", APIKeyID: "key-a"})
	if err != nil {
		t.Fatalf("create danger confirmation: %v", err)
	}

	arguments["confirmNonce"] = confirmation.ConfirmNonce
	err = appService.ConsumeMCPDangerConfirmationAs(
		"delete_conversation",
		arguments,
		confirmation.ConfirmNonce,
		AuthPrincipal{AuthType: "api_key", UserID: "root-user", APIKeyID: "key-b", Scopes: []string{"mcp:admin"}},
	)
	if err == nil {
		t.Fatal("expected another API key to be denied even with mcp:admin")
	}
}

func TestMCPDangerConfirmationRateLimitIsScopedToPrincipal(t *testing.T) {
	appService := &AppService{
		mcpDangerConfirms: map[string]mcpDangerConfirmationRecord{},
		mcpDangerRates:    map[string][]time.Time{},
	}
	ownerA := AuthPrincipal{AuthType: "api_key", APIKeyID: "key-a"}
	ownerB := AuthPrincipal{AuthType: "api_key", APIKeyID: "key-b"}

	for index := 0; index < mcpDangerConfirmationRateLimit; index++ {
		if _, err := appService.CreateMCPDangerConfirmationAs(model.MCPDangerConfirmationRequest{
			ToolName:  "delete_conversation",
			Arguments: map[string]any{"id": "conversation-a"},
		}, ownerA); err != nil {
			t.Fatalf("create confirmation %d: %v", index, err)
		}
	}
	if _, err := appService.CreateMCPDangerConfirmationAs(model.MCPDangerConfirmationRequest{
		ToolName:  "delete_conversation",
		Arguments: map[string]any{"id": "conversation-a"},
	}, ownerA); err == nil || !strings.Contains(err.Error(), "rate limit") {
		t.Fatalf("expected owner A to be rate limited, got %v", err)
	}
	if _, err := appService.CreateMCPDangerConfirmationAs(model.MCPDangerConfirmationRequest{
		ToolName:  "delete_conversation",
		Arguments: map[string]any{"id": "conversation-b"},
	}, ownerB); err != nil {
		t.Fatalf("expected owner B to have an independent rate limit: %v", err)
	}
}
