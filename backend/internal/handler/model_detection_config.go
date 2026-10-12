package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type detectionPresets struct {
	Version int    `json:"version"`
	Pelican string `json:"pelican"`
	Candy   string `json:"candy"`
}
type detectionGroup struct {
	ID       int64    `json:"id"`
	Name     string   `json:"name"`
	Platform string   `json:"platform"`
	Models   []string `json:"models"`
}

// Reuse upstream services without changing its settings, gateway or billing.
func (h *ModelDetectionHandler) Configure(monitor *ChannelMonitorV2Handler, user *UserHandler) {
	h.monitorConfig = monitor.service.GetConfig
	h.getUser = user.userService.GetByID
}

func (h *ModelDetectionHandler) presets() (detectionPresets, error) {
	p := detectionPresets{Version: 1, Pelican: detectionPelicanPrompt, Candy: detectionCandyPrompt}
	b, err := os.ReadFile(filepath.Join(filepath.Dir(h.file), "presets.json"))
	if errors.Is(err, os.ErrNotExist) {
		return p, nil
	}
	if err != nil {
		return p, err
	}
	err = json.Unmarshal(b, &p)
	return p, err
}

func (h *ModelDetectionHandler) groups(ctx context.Context, userID int64) ([]detectionGroup, error) {
	if h.monitorConfig == nil || h.availableGroups == nil {
		return nil, errors.New("检测配置未就绪")
	}
	cfg, err := h.monitorConfig(ctx)
	if err != nil {
		return nil, err
	}
	groups, err := h.availableGroups(ctx, userID)
	if err != nil {
		return nil, err
	}
	result := []detectionGroup{}
	if cfg == nil || !cfg.Enabled {
		return result, nil
	}
	allowed := map[int64]bool{}
	for _, id := range cfg.GroupIDs {
		allowed[id] = true
	}
	for _, g := range groups {
		if len(allowed) > 0 && !allowed[g.ID] {
			continue
		}
		for _, p := range cfg.Platforms {
			if p.Platform != g.Platform || !p.Enabled {
				continue
			}
			models := []string{}
			seen := map[string]bool{}
			for _, m := range p.Models {
				m = strings.TrimSpace(m)
				if m != "" && m != service.ChannelMonitorV2OtherModel && !seen[m] {
					models = append(models, m)
					seen[m] = true
				}
			}
			result = append(result, detectionGroup{ID: g.ID, Name: g.Name, Platform: g.Platform, Models: models})
		}
	}
	return result, nil
}

func (h *ModelDetectionHandler) Config(c *gin.Context) {
	sub, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	groups, err := h.groups(c, sub.UserID)
	if err != nil {
		response.Error(c, 503, "读取渠道监控配置失败")
		return
	}
	h.mu.Lock()
	p, err := h.presets()
	h.mu.Unlock()
	if err != nil {
		response.Error(c, 500, "读取预设提示词失败")
		return
	}
	c.Header("Cache-Control", "private, no-store")
	response.Success(c, gin.H{"groups": groups, "presets": p})
}

func (h *ModelDetectionHandler) SavePresets(c *gin.Context) {
	role, _ := middleware.GetUserRoleFromContext(c)
	if role != service.RoleAdmin {
		response.Error(c, 403, "仅管理员可修改预设")
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 40<<10)
	var p detectionPresets
	if c.ShouldBindJSON(&p) != nil || strings.TrimSpace(p.Pelican) == "" || strings.TrimSpace(p.Candy) == "" || len(p.Pelican) > 16000 || len(p.Candy) > 16000 {
		response.Error(c, 400, "提示词不能为空，且每项最多 16000 字节")
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	old, err := h.presets()
	if err != nil {
		response.Error(c, 500, "读取预设失败")
		return
	}
	if old.Version != p.Version {
		response.Error(c, 409, "预设已被更新，请刷新后再保存")
		return
	}
	p.Version++
	dir := filepath.Dir(h.file)
	if err = os.MkdirAll(dir, 0700); err == nil {
		var f *os.File
		f, err = os.CreateTemp(dir, "presets-*.tmp")
		if err == nil {
			defer os.Remove(f.Name())
			err = json.NewEncoder(f).Encode(p)
			if err == nil {
				err = f.Sync()
			}
			closeErr := f.Close()
			if err == nil {
				err = closeErr
			}
			if err == nil {
				err = os.Rename(f.Name(), filepath.Join(dir, "presets.json"))
			}
		}
	}
	if err != nil {
		response.Error(c, 500, "保存预设失败")
		return
	}
	response.Success(c, p)
}

// Called under h.mu: one writer and one reusable, user-owned key per group.
// Never reuse an unrelated key or override a user's disabled/restricted key.
func (h *ModelDetectionHandler) dedicatedKey(ctx context.Context, userID, groupID int64) (*service.APIKey, error) {
	if h.keys == nil {
		return nil, errors.New("检测服务未就绪")
	}
	name := fmt.Sprintf("模型检测专用 · #%d", groupID)
	for page := 1; ; page++ {
		keys, paging, err := h.keys.List(ctx, userID, pagination.PaginationParams{Page: page, PageSize: 100}, service.APIKeyListFilters{GroupID: &groupID})
		if err != nil {
			return nil, err
		}
		for _, k := range keys {
			if k.Name == name {
				return h.getKey(ctx, k.ID)
			}
		}
		if len(keys) == 0 || paging == nil || page >= paging.Pages {
			break
		}
	}
	k, err := h.keys.Create(ctx, userID, service.CreateAPIKeyRequest{Name: name, GroupID: &groupID})
	if err != nil {
		return nil, err
	}
	return h.getKey(ctx, k.ID)
}
