package router

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ai-localbase/internal/model"
)

// TestChatModelProbeUsesRealResult 用真实HTTP调用证明上游错误不能被本地降级文字掩盖。
func TestChatModelProbeUsesRealResult(t *testing.T) {
	engine, app, _, cleanup := newTestRouterWithAccessTokenAndService(t, "")
	defer cleanup()
	for _, tc := range []struct {
		name           string
		useDefault     bool
		temperature    float64
		upstreamStatus int
	}{
		{"rejected-temperature", false, 0.7, 400},
		{"default-temperature", true, 0.7, 200},
		{"zero-temperature", false, 0, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if r.URL.Path != "/v1/chat/completions" || body["model"] != "fixture-model" {
					t.Errorf("probe did not call selected model: %s %+v", r.URL.Path, body)
				}
				value, present := body["temperature"]
				if present == tc.useDefault || (!tc.useDefault && value != tc.temperature) {
					t.Errorf("wrong temperature: %+v", body)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.upstreamStatus)
				if tc.upstreamStatus != 200 {
					_, _ = w.Write([]byte(`{"error":{"message":"model does not support non-default temperature"}}`))
					return
				}
				_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"pong"}}]}`))
			}))
			defer upstream.Close()
			before := app.GetConfig()
			body, _ := json.Marshal(model.TestChatModelRequest{Provider: "openai-compatible", BaseURL: upstream.URL, Model: "fixture-model", Temperature: tc.temperature, UseDefaultTemperature: tc.useDefault})
			response := performRequest(t, engine, http.MethodPost, "/api/config/test-chat-model", bytes.NewReader(body), "application/json")
			var result model.TestModelResponse
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if response.Code != 200 || result.Success != (tc.upstreamStatus == 200) {
				t.Fatalf("false probe result: %d %+v", response.Code, result)
			}
			if tc.upstreamStatus != 200 && !strings.Contains(result.ErrorMessage, "non-default temperature") {
				t.Fatalf("upstream error lost: %+v", result)
			}
			if app.GetConfig() != before {
				t.Fatal("probe unexpectedly saved draft")
			}
		})
	}
}

// TestWikiChangesHTTP 验证文档记录路由的分页、未知库和鉴权边界。
func TestWikiChangesHTTP(t *testing.T) {
	engine, app, _, cleanup := newTestRouterWithAccessTokenAndService(t, "")
	defer cleanup()
	id := app.ListKnowledgeBases()[0].ID
	for _, tc := range []struct {
		path   string
		status int
	}{
		{id + "/wiki/changes", 200}, {"missing/wiki/changes", 404}, {id + "/wiki/changes?offset=bad", 400}, {id + "/wiki/changes?limit=101", 400},
	} {
		response := performRequest(t, engine, http.MethodGet, "/api/knowledge-bases/"+tc.path, nil, "")
		if response.Code != tc.status {
			t.Fatalf("unexpected changes status %d: %s", response.Code, response.Body)
		}
	}
	secured, _, _, closeSecured := newTestRouterWithAccessTokenAndService(t, "fixture-token")
	defer closeSecured()
	for _, tc := range []struct {
		query  string
		status int
	}{{"", 200}, {"?knowledgeBaseId=" + id, 200}, {"?knowledgeBaseId=missing", 404}, {"?offset=bad", 400}, {"?limit=0", 400}} {
		response := performRequest(t, engine, http.MethodGet, "/api/wiki/changes"+tc.query, nil, "")
		if response.Code != tc.status {
			t.Fatalf("global changes status %d: %s", response.Code, response.Body)
		}
	}
	if response := performRequest(t, secured, http.MethodGet, "/api/wiki/changes", nil, ""); response.Code != 401 {
		t.Fatalf("global history bypassed auth: %d", response.Code)
	}
	if response := performRequest(t, secured, http.MethodGet, "/api/knowledge-bases/"+id+"/wiki/changes", nil, ""); response.Code != 401 {
		t.Fatalf("missing auth accepted: %d", response.Code)
	}
}

// TestWikiRetryFailedHTTP 验证批量入口保持鉴权和单库边界。
func TestWikiRetryFailedHTTP(t *testing.T) {
	engine, app, _, cleanup := newTestRouterWithAccessTokenAndService(t, "")
	defer cleanup()
	id := app.ListKnowledgeBases()[0].ID
	for _, tc := range []struct {
		id     string
		status int
	}{{id, 200}, {"missing", 404}} {
		response := performRequest(t, engine, http.MethodPost, "/api/knowledge-bases/"+tc.id+"/wiki/retry-failed", nil, "")
		if response.Code != tc.status {
			t.Fatalf("unexpected status %d: %s", response.Code, response.Body)
		}
	}
	secured, _, _, closeSecured := newTestRouterWithAccessTokenAndService(t, "fixture-token")
	defer closeSecured()
	response := performRequest(t, secured, http.MethodPost, "/api/knowledge-bases/"+id+"/wiki/retry-failed", nil, "")
	if response.Code != 401 {
		t.Fatalf("missing auth accepted: %d", response.Code)
	}
}
