package router

import (
	"net/http"
	"strings"

	"ai-localbase/internal/model"

	"github.com/gin-gonic/gin"
)

const defaultMaxJSONBodyBytes int64 = 4 * 1024 * 1024

func requestBodyLimitMiddleware(limit int64) gin.HandlerFunc {
	if limit <= 0 {
		limit = defaultMaxJSONBodyBytes
	}

	return func(c *gin.Context) {
		if !requestHasBodyLimit(c.Request) {
			c.Next()
			return
		}
		if c.Request.ContentLength > limit {
			abortRequestBodyTooLarge(c)
			return
		}

		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
		c.Next()
	}
}

func requestHasBodyLimit(request *http.Request) bool {
	if request == nil {
		return false
	}
	switch request.Method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
	default:
		return false
	}
	contentType := strings.ToLower(strings.TrimSpace(request.Header.Get("Content-Type")))
	return !strings.HasPrefix(contentType, "multipart/")
}

func abortRequestBodyTooLarge(c *gin.Context) {
	c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, model.APIError{
		Error: model.ErrorDetail{
			Code:    "request_body_too_large",
			Message: "request body is too large",
		},
	})
}
