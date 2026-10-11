package handler

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

// This optional UI extension does not change the upstream database schema.
// Files are private, keyed ONLY by the authenticated subject, and atomically replaced.
const coverMaxInput = 5 << 20
const coverMaxBody = 7 << 20
const coverMaxStored = 3 << 20

type DashboardCover struct {
	Version   int    `json:"version"`
	ImageData string `json:"image_data"`
	PositionX int    `json:"position_x"`
	PositionY int    `json:"position_y"`
	Mask      int    `json:"mask"`
}

func defaultDashboardCover() DashboardCover {
	return DashboardCover{Version: 1, PositionX: 70, PositionY: 50, Mask: 78}
}

type DashboardCoverHandler struct {
	dir   string
	mu    sync.Mutex
	slots chan struct{}
}

func NewDashboardCoverHandler(dir string) *DashboardCoverHandler {
	if dir == "" {
		dir = os.Getenv("SAKURA_COVER_DIR")
	}
	if dir == "" {
		dir = filepath.Join("data", "dashboard-covers")
	}
	return &DashboardCoverHandler{dir: dir, slots: make(chan struct{}, 2)}
}

func (h *DashboardCoverHandler) file(c *gin.Context) (string, bool) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Unauthorized(c, "User not authenticated")
		return "", false
	}
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	return filepath.Join(h.dir, strconv.FormatInt(subject.UserID, 10)+".json"), true
}

func readDashboardCover(file string) (DashboardCover, error) {
	cover := defaultDashboardCover()
	f, err := os.Open(file)
	if errors.Is(err, os.ErrNotExist) {
		return cover, nil
	}
	if err != nil {
		return cover, err
	}
	defer f.Close()
	err = json.NewDecoder(io.LimitReader(f, coverMaxStored)).Decode(&cover)
	return cover, err
}

func (h *DashboardCoverHandler) Get(c *gin.Context) {
	file, ok := h.file(c)
	if !ok {
		return
	}
	cover, err := readDashboardCover(file)
	if err != nil {
		response.Error(c, 500, "Cover storage read failed")
		return
	}
	response.Success(c, cover)
}

func normalizeDashboardCover(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}
	if len(raw) > (coverMaxInput*4/3)+100 {
		return "", errors.New("image exceeds 5 MB")
	}
	header, encoded, ok := strings.Cut(raw, ",")
	allowed := map[string]string{"data:image/jpeg;base64": "jpeg", "data:image/png;base64": "png", "data:image/webp;base64": "webp"}
	expected, permitted := allowed[header]
	if !ok || !permitted {
		return "", errors.New("use a JPG, PNG or WebP image")
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(data) > coverMaxInput {
		return "", errors.New("invalid or oversized image")
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || format != expected {
		return "", errors.New("image content does not match its type")
	}
	if cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 8192 || cfg.Height > 8192 || int64(cfg.Width)*int64(cfg.Height) > 16000000 {
		return "", errors.New("image must be at most 16 megapixels and 8192 pixels per side")
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return "", errors.New("image decode failed")
	}
	w, height := cfg.Width, cfg.Height
	if w > 1920 {
		height = max(1, height*1920/w)
		w = 1920
	}
	if height > 1080 {
		w = max(1, w*1080/height)
		height = 1080
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, height))
	draw.Draw(dst, dst.Bounds(), &image.Uniform{C: color.RGBA{247, 246, 251, 255}}, image.Point{}, draw.Src)
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Over, nil)
	var out bytes.Buffer
	if err = jpeg.Encode(&out, dst, &jpeg.Options{Quality: 85}); err != nil {
		return "", err
	}
	if out.Len() > 2<<20 {
		return "", errors.New("image is too complex; choose a smaller image")
	}
	// Re-encoding strips EXIF, trailing payloads, animation and untrusted metadata.
	return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(out.Bytes()), nil
}

func (h *DashboardCoverHandler) Put(c *gin.Context) {
	file, ok := h.file(c)
	if !ok {
		return
	}
	select {
	case h.slots <- struct{}{}:
		defer func() { <-h.slots }()
	default:
		response.Error(c, 429, "Please retry the upload shortly")
		return
	}
	var input struct {
		ImageData *string `json:"image_data"`
		PositionX *int    `json:"position_x"`
		PositionY *int    `json:"position_y"`
		Mask      *int    `json:"mask"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, coverMaxBody)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		response.BadRequest(c, "Invalid or oversized cover request")
		return
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		response.BadRequest(c, "Expected one cover object")
		return
	}
	if input.PositionX == nil || input.PositionY == nil || input.Mask == nil || *input.PositionX < 0 || *input.PositionX > 100 || *input.PositionY < 0 || *input.PositionY > 100 || *input.Mask < 60 || *input.Mask > 95 {
		response.BadRequest(c, "Invalid image position or mask")
		return
	}
	var normalized string
	if input.ImageData != nil {
		var err error
		normalized, err = normalizeDashboardCover(*input.ImageData)
		if err != nil {
			response.BadRequest(c, err.Error())
			return
		}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	cover, err := readDashboardCover(file)
	if err != nil {
		response.Error(c, 500, "Cover storage read failed")
		return
	}
	cover.PositionX, cover.PositionY, cover.Mask = *input.PositionX, *input.PositionY, *input.Mask
	if input.ImageData != nil {
		cover.ImageData = normalized
	}
	if err = os.MkdirAll(h.dir, 0700); err != nil {
		response.Error(c, 500, "Cover storage unavailable")
		return
	}
	temp, err := os.CreateTemp(h.dir, ".cover-*")
	if err != nil {
		response.Error(c, 500, "Cover storage unavailable")
		return
	}
	defer os.Remove(temp.Name())
	err = json.NewEncoder(temp).Encode(cover)
	if err == nil {
		err = temp.Sync()
	}
	closeErr := temp.Close()
	if err != nil || closeErr != nil {
		response.Error(c, 500, "Cover save failed")
		return
	}
	if err = os.Rename(temp.Name(), file); err != nil {
		response.Error(c, 500, "Cover save failed")
		return
	}
	response.Success(c, cover)
}

func (h *DashboardCoverHandler) Delete(c *gin.Context) {
	file, ok := h.file(c)
	if !ok {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := os.Remove(file); err != nil && !errors.Is(err, os.ErrNotExist) {
		response.Error(c, 500, "Cover reset failed")
		return
	}
	response.Success(c, defaultDashboardCover())
}
