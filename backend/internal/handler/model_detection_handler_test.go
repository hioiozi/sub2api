package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func detectionRouter(h *ModelDetectionHandler) *gin.Engine {
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if c.GetHeader("X-User") == "1" {
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 1})
		}
	})
	r.GET("/", h.List)
	r.POST("/", h.Create)
	r.DELETE("/:id", h.Delete)
	return r
}
func detectionRequest(r *gin.Engine, method, path, body, user string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("X-User", user)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestModelDetectionServerExecutionAndIsolation(t *testing.T) {
	release := make(chan struct{})
	entered := make(chan struct{}, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer private-test-key" || r.URL.Path != "/v1/responses" {
			t.Error("gateway authentication/path")
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["model"] != "test-model" || body["stream"] != true {
			t.Error("generation payload")
		}
		entered <- struct{}{}
		<-release
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"completed","output":[{"content":[{"text":"<html><body><svg></svg></body></html>"}]}]}`))
	}))
	defer upstream.Close()
	h := NewModelDetectionHandler(t.TempDir(), nil)
	h.gateway = upstream.URL
	groupID := int64(8)
	h.getKey = func(_ context.Context, id int64) (*service.APIKey, error) {
		owner := int64(1)
		if id == 2 {
			owner = 2
		}
		return &service.APIKey{UserID: owner, Key: "private-test-key", Status: "active", User: &service.User{Balance: 1}, GroupID: &groupID, Group: &service.Group{Name: "test-group", Platform: "openai"}}, nil
	}
	r := detectionRouter(h)
	input := `{"key_id":1,"model":"test-model","topic":"自定义","prompt":"make SVG","html":"FORGED","status":"generated","duration_ms":999}`
	if w := detectionRequest(r, "GET", "/", "", ""); w.Code != 401 {
		t.Fatal(w.Code)
	}
	if w := detectionRequest(r, "POST", "/", strings.Replace(input, `"key_id":1`, `"key_id":2`, 1), "1"); w.Code != 404 {
		t.Fatal(w.Code)
	}
	w := detectionRequest(r, "POST", "/", input, "1")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("generation not started")
	}
	var created struct {
		Data ModelDetectionRecord `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	if created.Data.Status != "running" || created.Data.HTML != "" || created.Data.KeyName != "test-group" || created.Data.DurationMS != 0 {
		t.Fatal(created.Data)
	}
	if w := detectionRequest(r, "POST", "/", input, "1"); w.Code != 409 {
		t.Fatal(w.Code)
	}
	if w := detectionRequest(r, "DELETE", "/"+created.Data.ID, "", "1"); w.Code != 409 {
		t.Fatal(w.Code)
	}
	close(release)
	deadline := time.Now().Add(3 * time.Second)
	for {
		h.mu.Lock()
		state, err := h.read()
		h.mu.Unlock()
		if err != nil {
			t.Fatal(err)
		}
		if state.Items[0].Status == "generated" {
			if state.Generated != 1 || !strings.Contains(state.Items[0].HTML, "<svg>") {
				t.Fatal(state)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("job not completed")
		}
		time.Sleep(10 * time.Millisecond)
	}
	stored, _ := os.ReadFile(h.file)
	if strings.Contains(string(stored), "private-test-key") || strings.Contains(string(stored), "FORGED") {
		t.Fatal("secret or forged result persisted")
	}
	h.mu.Lock()
	state, _ := h.read()
	state.Items[0].UserID = 2
	_ = h.write(state)
	h.mu.Unlock()
	if w := detectionRequest(r, "DELETE", "/"+created.Data.ID, "", "1"); w.Code != 403 {
		t.Fatal(w.Code)
	}
}

func TestModelDetectionFailureAndRestart(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
	}{
		{"truncated", `{"status":"incomplete","output":[]}`, 200},
		{"partial html", `{"output":[{"content":[{"text":"<html><svg>"}]}]}`, 200},
		{"upstream error", `secret upstream detail`, 502},
	} {
		t.Run(tc.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer upstream.Close()
			h := NewModelDetectionHandler(t.TempDir(), nil)
			h.gateway = upstream.URL
			html, failure := h.requestGeneration(ModelDetectionRecord{Model: "test", Prompt: "test", Topic: "鹈鹕骑自行车"}, "key")
			if html != "" || failure == "" || strings.Contains(failure, "secret") {
				t.Fatal(html, failure)
			}
		})
	}
	dir := t.TempDir()
	h := NewModelDetectionHandler(dir, nil)
	if err := h.write(modelDetectionState{Items: []ModelDetectionRecord{{ID: "interrupted", Status: "running"}}}); err != nil {
		t.Fatal(err)
	}
	restarted := NewModelDetectionHandler(dir, nil)
	state, _ := restarted.read()
	if state.Failed != 1 || state.Items[0].Status != "failed" {
		t.Fatal(state)
	}
}

func TestModelDetectionHTMLCompleteness(t *testing.T) {
	for _, input := range []string{"no html", "<html>partial", "<svg>", "<html>" + strings.Repeat("x", 221<<10) + "</html>"} {
		if detectionHTML(input) != "" {
			t.Fatal("accepted incomplete/oversize")
		}
	}
	if got := detectionHTML("```html\n<html>ok</html>\n```comment"); got != "<html>ok</html>" {
		t.Fatal(got)
	}
}

func TestModelDetectionRawTextAndBalance(t *testing.T) {
	raw := "  我认为答案是 29。\n下面是我的推理。\n"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(gin.H{"output": []gin.H{{"content": []gin.H{{"text": raw}}}}})
	}))
	defer upstream.Close()
	h := NewModelDetectionHandler(t.TempDir(), nil)
	h.gateway = upstream.URL
	text, failure := h.requestGeneration(ModelDetectionRecord{Topic: "糖果问题", Prompt: detectionCandyPrompt}, "key")
	if text != raw || failure != "" {
		t.Fatal("raw answer changed", text, failure)
	}
	groupID := int64(8)
	h.getKey = func(context.Context, int64) (*service.APIKey, error) {
		return &service.APIKey{UserID: 1, Status: "active", User: &service.User{Balance: 0}, GroupID: &groupID, Group: &service.Group{}}, nil
	}
	r := detectionRouter(h)
	if w := detectionRequest(r, "POST", "/", `{"key_id":1,"model":"test","topic":"糖果问题"}`, "1"); w.Code != 403 {
		t.Fatal("zero balance accepted", w.Code)
	}
	if w := detectionRequest(r, "POST", "/", `{"key_id":1,"model":"test","topic":"城市夜景","prompt":"test"}`, "1"); w.Code != 400 {
		t.Fatal("unsupported topic accepted", w.Code)
	}
}

func TestModelDetectionStreamCompletion(t *testing.T) {
	for _, tc := range []struct {
		name, stream, want string
		failed             bool
	}{
		{"responses", "data: {\"type\":\"response.output_text.delta\",\"delta\":\" 21\\n\"}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output\":[{\"content\":[{\"text\":\" 21\\n\"}]}]}}\n\n", " 21\n", false},
		{"anthropic", "data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"回答\\n\"}}\n\ndata: {\"type\":\"message_stop\"}\n\n", "回答\n", false},
		{"disconnect", "data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\n", "", true},
		{"token limit", "data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"max_tokens\"}}\n\ndata: {\"type\":\"message_stop\"}\n\n", "", true},
		{"incomplete", "data: {\"type\":\"response.incomplete\"}\n\n", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, failure := detectionReadStream(strings.NewReader(tc.stream))
			if got != tc.want || (failure != "") != tc.failed {
				t.Fatal(got, failure)
			}
		})
	}
}
