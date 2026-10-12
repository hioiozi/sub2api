package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func TestDetectionConfiguredGroupsIntersectPermissions(t *testing.T) {
	h := NewModelDetectionHandler(t.TempDir(), nil)
	cfg := &service.ChannelMonitorV2Config{Enabled: true, GroupIDs: []int64{1, 2, 3}, Platforms: []service.ChannelMonitorV2PlatformConfig{{Platform: "openai", Enabled: true, Models: []string{"configured-but-unsupported", "__other__", "configured-but-unsupported"}}, {Platform: "anthropic", Enabled: false}}}
	h.monitorConfig = func(context.Context) (*service.ChannelMonitorV2Config, error) { return cfg, nil }
	h.availableGroups = func(context.Context, int64) ([]service.Group, error) {
		return []service.Group{{ID: 1, Platform: "openai"}, {ID: 3, Platform: "anthropic"}, {ID: 4, Platform: "openai"}}, nil
	}
	groups, e := h.groups(context.Background(), 1)
	if e != nil || len(groups) != 1 || groups[0].ID != 1 || len(groups[0].Models) != 1 || groups[0].Models[0] != "configured-but-unsupported" {
		t.Fatal(groups, e)
	}
	cfg.GroupIDs = nil
	groups, e = h.groups(context.Background(), 1)
	if e != nil || len(groups) != 2 {
		t.Fatal(groups, e)
	}
	cfg.Enabled = false
	groups, e = h.groups(context.Background(), 1)
	if e != nil || len(groups) != 0 {
		t.Fatal(groups, e)
	}
}

func TestDetectionRetryBoundAndPersistentCooldown(t *testing.T) {
	for _, tc := range []struct {
		name          string
		status        int
		retryAfter    string
		secondSuccess bool
		calls         int
		cooldown      bool
	}{
		{"retry recovers", 502, "", true, 2, false},
		{"retry exhausted", 503, "", false, 2, true},
		{"rate limited long", 429, "120", false, 1, true},
		{"unsupported", 400, "", false, 1, false},
		{"ambiguous timeout", 504, "", false, 1, false},
		{"partial stream", 200, "", false, 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				n := calls.Add(1)
				if tc.secondSuccess && n == 2 {
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{"output":[{"content":[{"text":"29"}]}]}`))
					return
				}
				if tc.status == 200 {
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = w.Write([]byte("data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\n"))
					return
				}
				w.Header().Set("Retry-After", tc.retryAfter)
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte("private-provider-error"))
			}))
			defer server.Close()
			dir := t.TempDir()
			h := NewModelDetectionHandler(dir, nil)
			h.gateway = server.URL
			h.retryDelay = time.Millisecond
			item := ModelDetectionRecord{ID: "job", UserID: 1, GroupID: 8, Topic: "糖果问题", Status: "running"}
			if e := h.write(modelDetectionState{Items: []ModelDetectionRecord{item}}); e != nil {
				t.Fatal(e)
			}
			h.generate(item, "secret-key")
			state, e := h.read()
			if e != nil {
				t.Fatal(e)
			}
			got := state.Items[0]
			if int(calls.Load()) != tc.calls || got.Attempts != tc.calls || (got.CooldownUntil != nil) != tc.cooldown || strings.Contains(got.Error, "private") {
				t.Fatal(calls.Load(), got)
			}
			if tc.secondSuccess && (got.Status != "generated" || state.Generated != 1) {
				t.Fatal(got)
			}
			if !tc.secondSuccess && (got.Status != "failed" || state.Failed != 1) {
				t.Fatal(got)
			}
			if tc.cooldown {
				// Deleting a work and restarting cannot bypass server-side cooldown.
				state.Items = nil
				_ = h.write(state)
				restarted := NewModelDetectionHandler(dir, nil)
				restarted.getKey = func(context.Context, int64) (*service.APIKey, error) {
					t.Fatal("cooldown must precede key creation")
					return nil, nil
				}
				w := detectionRequest(detectionRouter(restarted), "POST", "/", `{"group_id":8,"model":"test","topic":"糖果问题"}`, "1")
				if w.Code != 429 || w.Header().Get("Retry-After") == "" {
					t.Fatal(w.Code, w.Body.String())
				}
			}
		})
	}
}

func TestDetectionPresetsAdminAndConflict(t *testing.T) {
	h := NewModelDetectionHandler(t.TempDir(), nil)
	r := gin.New()
	r.PUT("/", func(c *gin.Context) { c.Set("user_role", c.GetHeader("X-Role")); h.SavePresets(c) })
	call := func(role string, version int) *httptest.ResponseRecorder {
		body, _ := json.Marshal(detectionPresets{Version: version, Pelican: "exact custom prompt", Candy: "raw answer"})
		req := httptest.NewRequest("PUT", "/", strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Role", role)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	if w := call("user", 1); w.Code != 403 {
		t.Fatal(w.Code)
	}
	if w := call("admin", 1); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := call("admin", 1); w.Code != 409 {
		t.Fatal(w.Code)
	}
	p, e := NewModelDetectionHandler(filepath.Dir(h.file), nil).presets()
	if e != nil || p.Version != 2 || p.Pelican != "exact custom prompt" {
		t.Fatal(p, e)
	}
}
