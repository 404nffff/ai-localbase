package router

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRequestBodyLimitRejectsOversizedJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(requestBodyLimitMiddleware(8))
	router.POST("/", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"content":"too large"}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected status 413, got %d, body=%s", resp.Code, resp.Body.String())
	}
}

func TestRequestBodyLimitLeavesMultipartUploadsToUploadLimits(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(requestBodyLimitMiddleware(8))
	router.POST("/", func(c *gin.Context) {
		_, _ = io.Copy(io.Discard, c.Request.Body)
		c.Status(http.StatusOK)
	})

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormField("file")
	if err != nil {
		t.Fatalf("create multipart field: %v", err)
	}
	if _, err := part.Write([]byte(strings.Repeat("x", 128))); err != nil {
		t.Fatalf("write multipart field: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected multipart request to bypass JSON limit, got %d, body=%s", resp.Code, resp.Body.String())
	}
}
