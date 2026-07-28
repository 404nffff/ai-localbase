package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ai-localbase/internal/model"
	"ai-localbase/internal/service"
	"github.com/gin-gonic/gin"
)

func TestExpectedEmbeddingVectorSizeUsesServerConfig(t *testing.T) {
	appService := service.NewAppService(nil, nil, nil, model.ServerConfig{QdrantVectorSize: 1024})
	handler := NewConfigHandler(appService, nil)
	if actual := handler.expectedEmbeddingVectorSize(); actual != 1024 {
		t.Fatalf("expected configured vector size 1024, got %d", actual)
	}
}

func TestEmbeddingModelResponseIncludesConfiguredVectorSize(t *testing.T) {
	embeddingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"embeddings":[[0.1,0.2,0.3,0.4]]}`))
	}))
	t.Cleanup(embeddingServer.Close)

	appService := service.NewAppService(nil, nil, nil, model.ServerConfig{QdrantVectorSize: 4})
	handler := NewConfigHandler(appService, nil)
	payload, err := json.Marshal(TestEmbeddingModelRequest{
		Provider: "ollama",
		BaseURL:  embeddingServer.URL,
		Model:    "test-embedding",
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodPost, "/api/config/test-embedding-model", bytes.NewReader(payload))
	context.Request.Header.Set("Content-Type", "application/json")
	handler.TestEmbeddingModel(context)

	var response TestModelResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if recorder.Code != http.StatusOK || !response.Success {
		t.Fatalf("expected successful embedding probe, status=%d response=%#v", recorder.Code, response)
	}
	if response.VectorSize != 4 || response.ExpectedVectorSize != 4 {
		t.Fatalf("expected actual and configured dimensions to be 4, got %#v", response)
	}
}

func TestFormatErrorMessageExplainsEmbeddingDimensionMigration(t *testing.T) {
	err := &service.EmbeddingDimensionMismatchError{BatchItem: 0, Expected: 768, Actual: 1024}
	message := formatErrorMessage(err)
	for _, expected := range []string{"1024", "QDRANT_VECTOR_SIZE=768", "QDRANT_COLLECTION_PREFIX", "重新索引"} {
		if !strings.Contains(message, expected) {
			t.Fatalf("expected migration message to contain %q, got %q", expected, message)
		}
	}

	actual, configured := embeddingDimensionDetails(err)
	if actual != 1024 || configured != 768 {
		t.Fatalf("unexpected dimension details: actual=%d configured=%d", actual, configured)
	}
}
