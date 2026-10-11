package handler

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
)

type accessTestBalance map[int64]float64

func (b accessTestBalance) TotalRecharged(_ context.Context, id int64) (float64, error) {
	return b[id], nil
}

type accessMock struct {
	mu             sync.Mutex
	rule           accessRule
	fail           int
	applyOnFailure bool
	writes         int
}

func accessFixture(t *testing.T) (*AccessProtectionHandler, *accessMock, *gin.Engine) {
	t.Helper()
	m := &accessMock{rule: accessRule{"id": "rule", "action": "block", "enabled": true, "expression": "(ip.src.country eq \"CN\")", "description": "original", "ref": "stable-ref", "logging": map[string]any{"enabled": true}, "version": "1"}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		defer m.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		auth := r.Header.Get("Authorization")
		if auth != "Bearer fixture-token" && auth != "Bearer fixture-token-rotated-valid" && auth != "Bearer fixture-token-readonly-valid" {
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]any{"success": false})
			return
		}
		if auth == "Bearer fixture-token-readonly-valid" && r.Method == http.MethodPatch {
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]any{"success": false})
			return
		}
		if r.Method == "PATCH" {
			m.writes++
			var body accessRule
			_ = json.NewDecoder(r.Body).Decode(&body)
			if m.fail == 0 || m.applyOnFailure {
				body["id"] = "rule"
				body["version"] = "2"
				m.rule = body
			}
			if m.fail != 0 {
				w.WriteHeader(m.fail)
				_ = json.NewEncoder(w).Encode(map[string]any{"success": false})
				return
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": map[string]any{"rules": []accessRule{m.rule, {"id": "other", "action": "skip"}}}})
	}))
	t.Cleanup(server.Close)
	dir := t.TempDir()
	key := make([]byte, 32)
	block, _ := aes.NewCipher(key)
	gcm, _ := cipher.NewGCM(block)
	nonce := make([]byte, gcm.NonceSize())
	cipherFile := filepath.Join(dir, "token")
	keyFile := filepath.Join(dir, "key")
	_ = os.WriteFile(keyFile, []byte(base64.RawURLEncoding.EncodeToString(key)), 0600)
	_ = os.WriteFile(cipherFile, []byte(base64.StdEncoding.EncodeToString(gcm.Seal(nonce, nonce, []byte("fixture-token"), nil))), 0600)
	h := &AccessProtectionHandler{dir: filepath.Join(dir, "state"), reader: accessTestBalance{1: 100, 2: 200}, zoneID: "zone", rulesetID: "set", ruleID: "rule", tokenFile: cipherFile, keyFile: keyFile, apiURL: server.URL, expectedBase: "(ip.src.country eq \"CN\")", client: server.Client()}
	return h, m, accessRouter(h)
}
func accessRouter(h *AccessProtectionHandler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		id, _ := strconv.ParseInt(c.GetHeader("X-Test-User"), 10, 64)
		if id > 0 {
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: id})
			if id == 1 {
				c.Set(string(middleware.ContextKeyUserRole), "admin")
			} else {
				c.Set(string(middleware.ContextKeyUserRole), "user")
			}
		}
	})
	r.GET("/access", h.Get)
	r.POST("/access", h.Put)
	r.PUT("/access/:id", h.Update)
	r.DELETE("/access/:id", h.Delete)
	r.PUT("/access/token", middleware.AdminOnly(), h.RotateToken)
	return r
}
func accessReq(r *gin.Engine, method, path, user, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("X-Test-User", user)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-For", "8.8.8.8")
	req.RemoteAddr = "172.25.0.1:2345"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}
func accessID(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var v struct{ Data struct{ ID string } }
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	return v.Data.ID
}
func TestAccessPublicIP(t *testing.T) {
	for _, s := range []string{"8.8.8.8", "1.1.1.1", "240e::1", "2606:4700:4700::1111"} {
		if _, e := validPublicIP(s); e != nil {
			t.Error(s, e)
		}
	}
	for _, s := range strings.Fields("1.1.1.1/32 example.com 127.0.0.1 10.1.2.3 172.25.0.1 0.1.2.3 100.64.0.1 169.254.1.1 192.0.2.1 198.51.100.2 203.0.113.10 198.18.0.1 224.1.1.1 255.255.255.255 :: ::1 ::ffff:8.8.8.8 fe80::1%eth0 fc00::1 ff02::1 2001:db8::1 2001::1 2002::1 3fff::1 64:ff9b::808:808") {
		if _, e := validPublicIP(s); e == nil {
			t.Error("accepted special address", s)
		}
	}
}
func TestAccessQuotaBoundaries(t *testing.T) {
	for _, c := range []struct {
		v float64
		q int
	}{{0, 0}, {99.99, 0}, {99.99999999, 0}, {100, 1}, {199.99, 1}, {200, 2}, {345.6, 3}} {
		h := &AccessProtectionHandler{reader: accessTestBalance{1: c.v}}
		q, _, err := h.quota(context.Background(), 1)
		if err != nil || q != c.q {
			t.Fatal(c, q, err)
		}
	}
}
func TestAccessLifecycleIsolationAndQuota(t *testing.T) {
	h, m, r := accessFixture(t)
	for _, c := range []struct {
		method, path, user, body string
		status                   int
	}{
		{"GET", "/access", "", "", 401}, {"POST", "/access", "3", `{"ip":"8.8.8.8"}`, 409}, {"PUT", "/access/unknown", "3", `{"ip":"8.8.8.8"}`, 404},
	} {
		if w := accessReq(r, c.method, c.path, c.user, c.body); w.Code != c.status {
			t.Fatal(c, w.Code)
		}
	}
	id := accessID(t, accessReq(r, "POST", "/access", "1", `{"ip":"8.8.8.8"}`))
	if w := accessReq(r, "POST", "/access", "1", `{"ip":"1.1.1.1"}`); w.Code != 409 {
		t.Fatal(w.Code)
	}
	if dup := accessID(t, accessReq(r, "POST", "/access", "1", `{"ip":"8.8.8.8"}`)); dup != id {
		t.Fatal("duplicate submission")
	}
	for _, method := range []string{"PUT", "DELETE"} {
		if w := accessReq(r, method, "/access/"+id, "2", `{"ip":"1.1.1.1"}`); w.Code != 404 {
			t.Fatal("foreign id", w.Code)
		}
	}
	w := accessReq(r, "GET", "/access", "2", "")
	if strings.Contains(w.Body.String(), "8.8.8.8") || !strings.Contains(w.Body.String(), `"current_ip":""`) {
		t.Fatal(w.Body.String())
	}
	accessID(t, accessReq(r, "PUT", "/access/"+id, "1", `{"ip":"1.1.1.1"}`))
	if strings.Contains(m.rule["expression"].(string), "8.8.8.8") {
		t.Fatal("old IP remains", m.rule)
	}
	if w := accessReq(r, "DELETE", "/access/"+id, "1", ""); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if m.rule["expression"] != h.expectedBase || m.rule["ref"] != "stable-ref" || m.rule["description"] != "original" {
		t.Fatal("rule not restored", m.rule)
	}
	s, err := h.load()
	if err != nil || len(s.Records) != 0 || s.Pending != nil || s.BaseRule["expression"] != h.expectedBase {
		t.Fatal(s, err)
	}
}
func TestAccessSharedIPUnion(t *testing.T) {
	h, m, r := accessFixture(t)
	a := accessID(t, accessReq(r, "POST", "/access", "1", `{"ip":"8.8.8.8"}`))
	b := accessID(t, accessReq(r, "POST", "/access", "2", `{"ip":"8.8.8.8"}`))
	c := accessID(t, accessReq(r, "POST", "/access", "2", `{"ip":"1.1.1.1"}`))
	if w := accessReq(r, "PUT", "/access/"+c, "2", `{"ip":"8.8.8.8"}`); w.Code != 409 {
		t.Fatal(w.Code)
	}
	if strings.Count(m.rule["expression"].(string), "8.8.8.8") != 1 {
		t.Fatal(m.rule)
	}
	accessReq(r, "DELETE", "/access/"+a, "1", "")
	if !strings.Contains(m.rule["expression"].(string), "8.8.8.8") {
		t.Fatal("other owner lost exception")
	}
	accessReq(r, "DELETE", "/access/"+b, "2", "")
	accessReq(r, "DELETE", "/access/"+c, "2", "")
	if m.rule["expression"] != h.expectedBase {
		t.Fatal(m.rule)
	}
}
func TestAccessRecoveryAfterFailureAndRestart(t *testing.T) {
	h, m, r := accessFixture(t)
	m.fail = 503
	if w := accessReq(r, "POST", "/access", "1", `{"ip":"8.8.8.8"}`); w.Code != 503 {
		t.Fatal(w.Code)
	}
	s, _ := h.load()
	if s.Pending == nil || len(s.Records) != 0 {
		t.Fatal("no durable intent")
	}
	m.fail = 0
	fresh := &AccessProtectionHandler{dir: h.dir, reader: h.reader, zoneID: h.zoneID, rulesetID: h.rulesetID, ruleID: h.ruleID, tokenFile: h.tokenFile, keyFile: h.keyFile, apiURL: h.apiURL, expectedBase: h.expectedBase, client: h.client}
	w := accessReq(accessRouter(fresh), "GET", "/access", "1", "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	s, _ = fresh.load()
	if s.Pending != nil || len(s.Records) != 1 {
		t.Fatal(s)
	}
	before := m.writes
	accessReq(accessRouter(fresh), "GET", "/access", "1", "")
	if m.writes != before {
		t.Fatal("repeated write")
	}
}
func TestAccessLostResponseAndRejection(t *testing.T) {
	for _, applied := range []bool{false, true} {
		t.Run(strconv.FormatBool(applied), func(t *testing.T) {
			h, m, r := accessFixture(t)
			m.fail = 400
			m.applyOnFailure = applied
			w := accessReq(r, "POST", "/access", "1", `{"ip":"8.8.8.8"}`)
			want := 503
			n := 0
			if applied {
				want = 200
				n = 1
			}
			if w.Code != want {
				t.Fatal(w.Code)
			}
			s, _ := h.load()
			if s.Pending != nil || len(s.Records) != n {
				t.Fatal(s)
			}
		})
	}
}
func TestAccessCrashAfterRemoteBeforeLocalCommit(t *testing.T) {
	h, m, r := accessFixture(t)
	accessID(t, accessReq(r, "POST", "/access", "1", `{"ip":"8.8.8.8"}`))
	s, _ := h.load()
	after := cloneAccessRule(s.BaseRule)
	s.Pending = &accessPending{Before: cloneAccessRule(s.LastRule), After: after, Records: []AccessIPRecord{}}
	if err := h.save(s); err != nil {
		t.Fatal(err)
	}
	m.rule = cloneAccessRule(after)
	m.rule["id"] = "rule"
	writes := m.writes
	if w := accessReq(r, "GET", "/access", "1", ""); w.Code != 200 {
		t.Fatal(w.Code)
	}
	s, _ = h.load()
	if len(s.Records) != 0 || s.Pending != nil || m.writes != writes {
		t.Fatal("did not converge", s)
	}
}
func TestAccessDriftAndLocalFailureNeverOverwrite(t *testing.T) {
	h, m, r := accessFixture(t)
	id := accessID(t, accessReq(r, "POST", "/access", "1", `{"ip":"8.8.8.8"}`))
	m.rule["description"] = "external edit"
	writes := m.writes
	if w := accessReq(r, "DELETE", "/access/"+id, "1", ""); w.Code != 503 {
		t.Fatal(w.Code)
	}
	if m.writes != writes {
		t.Fatal("overwrote drift")
	}
	h.dir = filepath.Join(t.TempDir(), "file")
	_ = os.WriteFile(h.dir, []byte("not a directory"), 0600)
	if w := accessReq(r, "POST", "/access", "1", `{"ip":"8.8.8.8"}`); w.Code != 503 {
		t.Fatal(w.Code)
	}
	if m.writes != writes {
		t.Fatal("wrote CF without durable state")
	}
}
func TestAccessExpressionAndConcurrentQuota(t *testing.T) {
	base := `(ip.src.country eq "CN" or http.host eq "sample.invalid")`
	expr, err := accessExpression(base, []AccessIPRecord{{IP: "8.8.8.8"}, {IP: "1.1.1.1"}, {IP: "8.8.8.8"}})
	if err != nil || expr != "("+base+") and not ip.src in {1.1.1.1 8.8.8.8}" {
		t.Fatal(expr, err)
	}
	h, _, r := accessFixture(t)
	var wg sync.WaitGroup
	for _, ip := range []string{"1.1.1.1", "8.8.8.8"} {
		wg.Add(1)
		go func(ip string) { defer wg.Done(); accessReq(r, "POST", "/access", "1", `{"ip":"`+ip+`"}`) }(ip)
	}
	wg.Wait()
	s, _ := h.load()
	if len(s.Records) != 1 {
		t.Fatal(s)
	}
}

func TestAccessTrustedProxyIP(t *testing.T) {
	t.Setenv("ACCESS_TRUSTED_PROXY_CIDRS", "172.30.88.1/32")
	for _, test := range []struct{ peer, header, want string }{
		{"172.30.88.1:4321", "8.8.8.8", "8.8.8.8"},
		{"172.30.88.2:4321", "8.8.8.8", ""},
		{"172.30.88.1:4321", "8.8.8.8, 1.1.1.1", ""},
		{"172.30.88.1:4321", "10.0.0.1", ""},
		{"172.30.88.1:4321", "240e::1", "240e::1"},
		{"8.8.8.8:4321", "1.1.1.1", "8.8.8.8"},
	} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("GET", "/", nil)
		c.Request.RemoteAddr = test.peer
		c.Request.Header.Set("X-Real-IP", test.header)
		if got := currentIP(c); got != test.want {
			t.Fatalf("%+v got %q", test, got)
		}
	}
}

func TestAccessTokenRotation(t *testing.T) {
	h, _, r := accessFixture(t)
	old, err := h.token()
	if err != nil {
		t.Fatal(err)
	}
	valid := `{"token":"fixture-token-rotated-valid"}`
	if w := accessReq(r, "PUT", "/access/token", "2", valid); w.Code != 403 {
		t.Fatal("user access", w.Code)
	}
	if w := accessReq(r, "PUT", "/access/token", "1", `{"token":"fixture-token-invalid-value"}`); w.Code != 400 {
		t.Fatal("invalid token", w.Code)
	}
	if w := accessReq(r, "PUT", "/access/token", "1", `{"token":"fixture-token-readonly-valid"}`); w.Code != 400 {
		t.Fatal("read-only token", w.Code)
	}
	if got, _ := h.token(); got != old {
		t.Fatal("invalid token replaced old token")
	}
	if _, err := os.Stat(h.rotatedTokenFile()); !os.IsNotExist(err) {
		t.Fatal("failed rotation created token file", err)
	}
	if w := accessReq(r, "PUT", "/access/token", "1", valid); w.Code != 200 || strings.Contains(w.Body.String(), "fixture-token") {
		t.Fatal("rotation response", w.Code, w.Body.String())
	}
	if got, err := h.token(); err != nil || got != "fixture-token-rotated-valid" {
		t.Fatal("rotated token unreadable", err)
	}
	blob, err := os.ReadFile(h.rotatedTokenFile())
	if err != nil || strings.Contains(string(blob), "fixture-token") {
		t.Fatal("token was not encrypted", err)
	}
	fresh := &AccessProtectionHandler{dir: h.dir, reader: h.reader, zoneID: h.zoneID, rulesetID: h.rulesetID, ruleID: h.ruleID, tokenFile: h.tokenFile, keyFile: h.keyFile, apiURL: h.apiURL, expectedBase: h.expectedBase, client: h.client}
	if got, err := fresh.token(); err != nil || got != "fixture-token-rotated-valid" {
		t.Fatal("rotated token lost after restart", err)
	}
	if w := accessReq(accessRouter(fresh), "POST", "/access", "1", `{"ip":"8.8.8.8"}`); w.Code != 200 {
		t.Fatal("IP sync after rotation", w.Code, w.Body.String())
	}
}
