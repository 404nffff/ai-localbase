package service

import (
	"testing"

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
