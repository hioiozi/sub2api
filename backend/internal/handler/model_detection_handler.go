package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

const modelDetectionLimit = 20
const modelDetectionBodyLimit = 300 << 10

type ModelDetectionRecord struct {
	ID         string    `json:"id"`
	UserID     int64     `json:"user_id"`
	GroupID    int64     `json:"group_id"`
	Platform   string    `json:"platform"`
	KeyName    string    `json:"key_name"`
	Model      string    `json:"model"`
	Topic      string    `json:"topic"`
	Prompt     string    `json:"prompt"`
	HTML       string    `json:"html"`
	Text       string    `json:"text"`
	Status     string    `json:"status"`
	Error      string    `json:"error,omitempty"`
	DurationMS int       `json:"duration_ms"`
	CreatedAt  time.Time `json:"created_at"`
}

type modelDetectionState struct {
	Generated int                    `json:"generated"`
	Failed    int                    `json:"failed"`
	Items     []ModelDetectionRecord `json:"items"`
}

type ModelDetectionHandler struct {
	file    string
	mu      sync.Mutex
	getKey  func(context.Context, int64) (*service.APIKey, error)
	client  *http.Client
	gateway string
}

func NewModelDetectionHandler(dir string, keys *APIKeyHandler) *ModelDetectionHandler {
	if dir == "" {
		dir = os.Getenv("MODEL_DETECTION_DIR")
	}
	if dir == "" {
		dir = filepath.Join("data", "model-detection")
	}
	h := &ModelDetectionHandler{file: filepath.Join(dir, "state.json")}
	h.setup(keys)
	return h
}

func (h *ModelDetectionHandler) read() (modelDetectionState, error) {
	var state modelDetectionState
	f, err := os.Open(h.file)
	if errors.Is(err, os.ErrNotExist) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	defer f.Close()
	if err = json.NewDecoder(io.LimitReader(f, 12<<20)).Decode(&state); err != nil {
		return state, err
	}
	return state, nil
}

func (h *ModelDetectionHandler) write(state modelDetectionState) error {
	if err := os.MkdirAll(filepath.Dir(h.file), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(h.file), "state-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0600); err == nil {
		err = json.NewEncoder(f).Encode(state)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), h.file)
}

func (h *ModelDetectionHandler) List(c *gin.Context) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	c.Header("Cache-Control", "private, no-store")
	h.mu.Lock()
	state, err := h.read()
	h.mu.Unlock()
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "检测记录读取失败")
		return
	}
	if state.Items == nil {
		state.Items = []ModelDetectionRecord{}
	}
	response.Success(c, state)
}

func (h *ModelDetectionHandler) Delete(c *gin.Context) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	role, _ := middleware.GetUserRoleFromContext(c)
	h.mu.Lock()
	defer h.mu.Unlock()
	state, err := h.read()
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "检测记录读取失败")
		return
	}
	for i, item := range state.Items {
		if item.ID != c.Param("id") {
			continue
		}
		if item.UserID != subject.UserID && role != service.RoleAdmin {
			response.Error(c, http.StatusForbidden, "无权删除该记录")
			return
		}
		if item.Status == "running" {
			response.Error(c, 409, "运行中的检测暂不支持删除")
			return
		}
		state.Items = append(state.Items[:i], state.Items[i+1:]...)
		if err = h.write(state); err != nil {
			response.Error(c, http.StatusInternalServerError, "删除检测记录失败")
			return
		}
		response.Success(c, gin.H{"deleted": true})
		return
	}
	response.Error(c, http.StatusNotFound, "检测记录不存在")
}
