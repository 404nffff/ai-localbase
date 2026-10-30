package router

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"ai-localbase/internal/model"
)

// TestWikiQueueHTTP 通过真实路由验证局部更新、进度回读、参数校验及既有鉴权。
func TestWikiQueueHTTP(t *testing.T) {
	engine, app, _, cleanup := newTestRouterWithAccessTokenAndService(t, "")
	defer cleanup()
	for _, tc := range []struct {
		body   string
		status int
	}{
		{`{"paused":true}`, 200},
		{`{"concurrency":2}`, 200},
		{`{}`, 400},
		{`{"concurrency":0}`, 400},
		{`{"concurrency":21}`, 400},
		{`{"concurrency":1.5}`, 400},
		{`{"paused":"true"}`, 400},
		{`{"concurrency":null}`, 400},
		{`{`, 400},
	} {
		response := performRequest(t, engine, http.MethodPut, "/api/wiki/queue", strings.NewReader(tc.body), "application/json")
		if response.Code != tc.status {
			t.Fatalf("%s: %d %s", tc.body, response.Code, response.Body)
		}
	}
	response := performRequest(t, engine, http.MethodGet, "/api/wiki/progress", nil, "")
	var result struct {
		Queue model.WikiQueueSettings `json:"queue"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || result.Queue != (model.WikiQueueSettings{Paused: true, Concurrency: 2}) {
		t.Fatalf("queue roundtrip: %s %v", response.Body, err)
	}
	_ = app.Close()
	response = performRequest(t, engine, http.MethodPut, "/api/wiki/queue", strings.NewReader(`{"paused":false}`), "application/json")
	if response.Code != 409 {
		t.Fatalf("closed service: %d", response.Code)
	}
	secured, securedApp, _, closeSecured := newTestRouterWithAccessTokenAndService(t, "queue-test-token")
	defer closeSecured()
	response = performRequest(t, secured, http.MethodPut, "/api/wiki/queue", strings.NewReader(`{"paused":true}`), "application/json")
	if response.Code != 401 || securedApp.GetConfig().Wiki.Queue.Paused {
		t.Fatalf("unauthenticated queue update: %d", response.Code)
	}
}
