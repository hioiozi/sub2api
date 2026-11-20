package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDetectionReasoningTransport(t *testing.T) {
	for _, platform := range []string{"openai", "anthropic", "antigravity"} {
		for _, effort := range []string{"", "none", "low", "medium", "high", "max", "xhigh", "minimal"} {
			t.Run(platform+"/"+effort, func(t *testing.T) {
				calls := 0
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls++
					var body map[string]any
					json.NewDecoder(r.Body).Decode(&body)
					if platform == "openai" {
						if r.URL.Path != "/v1/responses" {
							t.Error(r.URL.Path)
						}
						if effort != "" && body["reasoning"].(map[string]any)["effort"] != effort {
							t.Error(body)
						}
					} else {
						if r.URL.Path != "/v1/messages" {
							t.Error(r.URL.Path)
						}
						if effort == "none" && body["thinking"].(map[string]any)["type"] != "disabled" {
							t.Error(body)
						}
						if effort != "" && effort != "none" {
							b := body["thinking"].(map[string]any)["budget_tokens"].(float64)
							if b < 1024 || b >= body["max_tokens"].(float64) {
								t.Error(body)
							}
							if body["output_config"] != nil {
								t.Error("unknown model received unsupported output_config", body)
							}
						}
					}
					if effort == "" && (body["reasoning"] != nil || body["thinking"] != nil) {
						t.Error("default modified", body)
					}
					w.Header().Set("Content-Type", "application/json")
					w.Write([]byte(`{"output":[{"content":[{"text":"21"}]}],"content":[{"type":"text","text":"21"}]}`))
				}))
				defer server.Close()
				h := NewModelDetectionHandler(t.TempDir(), nil)
				h.gateway = server.URL
				_, failure, _, _ := h.requestAttempt(ModelDetectionRecord{Model: "test-model", Platform: platform, ReasoningEffort: effort, Topic: "糖果问题", Prompt: "test"}, "fixture-key", time.Now().Add(time.Second))
				valid := validDetectionEffort(effort, platform)
				if valid && (failure != "" || calls != 1) {
					t.Fatal(failure, calls)
				}
				if !valid && (failure == "" || calls != 0) {
					t.Fatal(failure, calls)
				}
			})
		}
	}
	h := NewModelDetectionHandler(t.TempDir(), nil)
	w := detectionRequest(detectionRouter(h), "POST", "/", `{"group_id":1,"model":"test","topic":"自定义","prompt":"hello","reasoning_effort":"INVALID"}`, "1")
	if w.Code != 400 || !strings.Contains(w.Body.String(), "思考档位无效") {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestDetectionAdaptiveReasoningTransport(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		thinking := body["thinking"].(map[string]any)
		if r.URL.Path != "/v1/messages" || thinking["type"] != "adaptive" || thinking["budget_tokens"] != nil || body["output_config"].(map[string]any)["effort"] != "high" {
			t.Error(body)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"content":[{"type":"text","text":"21"}]}`))
	}))
	defer server.Close()
	h := NewModelDetectionHandler(t.TempDir(), nil)
	h.gateway = server.URL
	_, failure, _, _ := h.requestAttempt(ModelDetectionRecord{Model: "claude-opus-4-6", Platform: "anthropic", ReasoningEffort: "high", Topic: "糖果问题", Prompt: "test"}, "fixture-key", time.Now().Add(time.Second))
	if failure != "" {
		t.Fatal(failure)
	}
}
