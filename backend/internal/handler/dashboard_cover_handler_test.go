package handler

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
)

func coverFixture(t *testing.T, w, h int) string {
	t.Helper()
	im := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			im.SetRGBA(x, y, color.RGBA{133, 101, 188, 255})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, im); err != nil {
		t.Fatal(err)
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(b.Bytes())
}

func coverTestRouter(dir string) *gin.Engine {
	r := gin.New()
	r.Use(func(c *gin.Context) {
		id, _ := strconv.ParseInt(c.GetHeader("X-Test-User"), 10, 64)
		if id > 0 {
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: id})
		}
	})
	h := NewDashboardCoverHandler(dir)
	r.GET("/cover", h.Get)
	r.PUT("/cover", h.Put)
	r.DELETE("/cover", h.Delete)
	return r
}

func coverRequest(r *gin.Engine, method, id, body string) *httptest.ResponseRecorder {
	q := httptest.NewRequest(method, "/cover", strings.NewReader(body))
	q.Header.Set("X-Test-User", id)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, q)
	return w
}

func TestDashboardCoverPersistenceIsolationReset(t *testing.T) {
	dir := t.TempDir()
	r := coverTestRouter(dir)
	if w := coverRequest(r, "GET", "", ""); w.Code != 401 {
		t.Fatal(w.Code)
	}
	input, _ := json.Marshal(map[string]any{"image_data": coverFixture(t, 2200, 1100), "position_x": 24, "position_y": 61, "mask": 83})
	w := coverRequest(r, "PUT", "11", string(input))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var output struct {
		Data DashboardCover `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &output); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(output.Data.ImageData, "data:image/jpeg;base64,") {
		t.Fatal("not normalized")
	}
	data, _ := base64.StdEncoding.DecodeString(strings.SplitN(output.Data.ImageData, ",", 2)[1])
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width != 1920 || cfg.Height != 960 {
		t.Fatal(cfg, err)
	}
	// A fresh handler represents a new process/device; no browser storage is involved.
	reloaded := coverTestRouter(dir)
	w = coverRequest(reloaded, "GET", "11", "")
	if !strings.Contains(w.Body.String(), `"position_x":24`) {
		t.Fatal(w.Body.String())
	}
	w = coverRequest(reloaded, "GET", "12", "")
	if strings.Contains(w.Body.String(), "data:image") {
		t.Fatal("cross-user cover leak")
	}
	if w.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatal("missing private cache policy")
	}
	w = coverRequest(r, "PUT", "11", `{"position_x":80,"position_y":40,"mask":78}`)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "data:image/jpeg") {
		t.Fatal("position update discarded image")
	}
	for i := 0; i < 2; i++ {
		if w = coverRequest(r, "DELETE", "11", ""); w.Code != 200 {
			t.Fatal(w.Code)
		}
	}
	if _, err = os.Stat(filepath.Join(dir, "11.json")); !os.IsNotExist(err) {
		t.Fatal("reset retained private file")
	}
}

func TestDashboardCoverRejectsInvalidInput(t *testing.T) {
	r := coverTestRouter(t.TempDir())
	cases := []string{
		`{"position_x":101,"position_y":0,"mask":78}`,
		`{"position_x":0,"position_y":0,"mask":0}`,
		`{"position_y":0,"mask":78}`,
		`{"position_x":0,"position_y":0,"mask":78,"user_id":12}`,
		`{"position_x":0,"position_y":0,"mask":78} {}`,
		`{"position_x":0,"position_y":0,"mask":78,"image_data":"data:image/svg+xml;base64,PHN2Zy8+"}`,
		`{"position_x":0,"position_y":0,"mask":78,"image_data":"https://example.com/a.jpg"}`,
		`{"position_x":0,"position_y":0,"mask":78,"image_data":"data:image/png;base64,AAAA"}`,
	}
	for _, body := range cases {
		if w := coverRequest(r, "PUT", "11", body); w.Code != 400 {
			t.Errorf("status %d: %s", w.Code, body)
		}
	}
	tooWide := coverFixture(t, 8193, 1)
	if _, err := normalizeDashboardCover(tooWide); err == nil {
		t.Fatal("oversized dimensions accepted")
	}
	wrongType := strings.Replace(coverFixture(t, 2, 2), "image/png", "image/jpeg", 1)
	if _, err := normalizeDashboardCover(wrongType); err == nil {
		t.Fatal("MIME mismatch accepted")
	}
	if _, err := normalizeDashboardCover("data:image/png;base64," + strings.Repeat("A", coverMaxBody)); err == nil {
		t.Fatal("oversized image accepted")
	}
}

func TestDashboardCoverConcurrentAtomicWrites(t *testing.T) {
	dir := t.TempDir()
	r := coverTestRouter(dir)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := coverRequest(r, "PUT", "1", `{"image_data":"","position_x":70,"position_y":50,"mask":78}`)
			if w.Code != 200 && w.Code != 429 {
				t.Errorf("unexpected status %d: %s", w.Code, w.Body.String())
			}
		}()
	}
	wg.Wait()
	w := coverRequest(r, "GET", "1", "")
	if w.Code != 200 || !json.Valid(w.Body.Bytes()) {
		t.Fatal("partial stored JSON")
	}
	files, _ := filepath.Glob(filepath.Join(dir, ".cover-*"))
	if len(files) > 0 {
		t.Fatal("temporary upload files left behind")
	}
}
