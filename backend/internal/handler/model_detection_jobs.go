package handler

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *ModelDetectionHandler) setup(keys *APIKeyHandler) {
	port, err := strconv.Atoi(os.Getenv("SERVER_PORT"))
	if err != nil || port < 1 || port > 65535 {
		port = 8080
	}
	h.gateway = fmt.Sprintf("http://127.0.0.1:%d", port)
	h.client = &http.Client{Timeout: 8 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	if keys != nil {
		h.getKey = keys.apiKeyService.GetByID
		h.keys = keys.apiKeyService
		h.availableGroups = keys.apiKeyService.GetAvailableGroups
	}
	h.retryDelay = 2 * time.Second
	// A restart must not retry a request that may already have been billed.
	state, err := h.read()
	if err != nil {
		return
	}
	changed := false
	for i := range state.Items {
		if state.Items[i].Status == "running" {
			state.Items[i].Status = "failed"
			state.Items[i].Error = "服务重启，检测已中断；请查看用量后再决定是否重试。"
			state.Failed++
			changed = true
		}
	}
	if changed {
		if err := h.write(state); err != nil {
			log.Printf("model detection recovery: %v", err)
		}
	}
}

// Create accepts a task, never client-supplied output, timing or status.
func (h *ModelDetectionHandler) Create(c *gin.Context) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 24<<10)
	var input struct {
		KeyID   int64  `json:"key_id"`
		GroupID int64  `json:"group_id"`
		Model   string `json:"model"`
		Topic   string `json:"topic"`
		Prompt  string `json:"prompt"`
	}
	if c.ShouldBindJSON(&input) != nil {
		response.Error(c, 400, "检测配置格式无效")
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	presets, presetErr := h.presets()
	if presetErr != nil {
		response.Error(c, 500, "读取预设提示词失败")
		return
	}
	input.Model = strings.TrimSpace(input.Model)
	input.Topic = strings.TrimSpace(input.Topic)
	input.Prompt = strings.TrimSpace(input.Prompt)
	switch input.Topic {
	case "鹈鹕骑自行车":
		input.Prompt = presets.Pelican
	case "糖果问题":
		input.Prompt = presets.Candy
	case "自定义":
	default:
		response.Error(c, 400, "请选择鹈鹕骑自行车、糖果问题或自定义")
		return
	}
	if (input.KeyID <= 0 && input.GroupID <= 0) || input.Model == "" || len(input.Model) > 120 || input.Topic == "" || len(input.Topic) > 80 || input.Prompt == "" || len(input.Prompt) > 16000 {
		response.Error(c, 400, "请填写分组、模型和提示词")
		return
	}
	if h.getKey == nil {
		response.Error(c, 503, "检测服务未就绪")
		return
	}
	// Reject duplicate jobs and cooldowns before creating any credential.
	state, err := h.read()
	if err != nil {
		response.Error(c, 500, "检测任务读取失败")
		return
	}
	active := 0
	for _, other := range state.Items {
		if other.Status == "running" {
			active++
			if other.UserID == subject.UserID {
				response.Error(c, 409, "已有检测正在运行")
				return
			}
		}
	}
	if active >= 4 {
		response.Error(c, 429, "检测任务较多，请稍后再试")
		return
	}
	var key *service.APIKey
	if input.GroupID <= 0 {
		key, err = h.getKey(c, input.KeyID)
		if err != nil || key == nil || key.UserID != subject.UserID || key.GroupID == nil {
			response.Error(c, 404, "检测密钥不存在")
			return
		}
		input.GroupID = *key.GroupID
	}
	scope := fmt.Sprintf("%d:%d", subject.UserID, input.GroupID)
	if until := state.Cooldowns[scope]; until.After(time.Now()) {
		c.Header("Retry-After", strconv.Itoa(int(time.Until(until).Seconds())+1))
		response.Error(c, 429, "该分组检测冷却中，请稍后再试")
		return
	}
	if h.monitorConfig != nil {
		groups, e := h.groups(c, subject.UserID)
		if e != nil {
			response.Error(c, 503, "读取渠道监控配置失败")
			return
		}
		allowed := false
		for _, g := range groups {
			if g.ID == input.GroupID {
				allowed = true
			}
		}
		if !allowed {
			response.Error(c, 403, "该分组未开放检测或暂无使用权限")
			return
		}
	}
	if h.getUser != nil {
		user, e := h.getUser(c, subject.UserID)
		if e != nil || user == nil {
			response.Error(c, 503, "读取用户余额失败")
			return
		}
		if user.Balance <= 0 {
			response.Error(c, 403, "余额不足，请先充值")
			return
		}
	}
	if key == nil {
		key, err = h.dedicatedKey(c, subject.UserID, input.GroupID)
		if err != nil {
			response.Error(c, 400, "检测专用密钥准备失败，请检查密钥数量限制及分组权限")
			return
		}
	}
	if key.UserID != subject.UserID || !key.IsActive() || key.IsExpired() || key.IsQuotaExhausted() || key.Group == nil || key.GroupID == nil || *key.GroupID != input.GroupID {
		response.Error(c, 400, "检测密钥已停用、过期或额度耗尽，请在 API 密钥页面检查")
		return
	}
	if key.User == nil || key.User.Balance <= 0 {
		response.Error(c, 403, "余额不足，请先充值")
		return
	}
	if len(key.IPWhitelist) > 0 || len(key.IPBlacklist) > 0 {
		response.Error(c, 400, "检测专用密钥设置了 IP 限制，请先检查")
		return
	}
	var id [12]byte
	if _, err := rand.Read(id[:]); err != nil {
		response.Error(c, 500, "创建检测任务失败")
		return
	}
	item := ModelDetectionRecord{ID: hex.EncodeToString(id[:]), UserID: subject.UserID, GroupID: *key.GroupID, KeyName: key.Group.Name, Platform: key.Group.Platform, Model: input.Model, Topic: input.Topic, Prompt: input.Prompt, Status: "running", CreatedAt: time.Now().UTC()}
	state.Items = append([]ModelDetectionRecord{item}, state.Items...)
	if len(state.Items) > modelDetectionLimit {
		for i := len(state.Items) - 1; i >= 0; i-- {
			if state.Items[i].Status != "running" {
				state.Items = append(state.Items[:i], state.Items[i+1:]...)
				break
			}
		}
	}
	if err := h.write(state); err != nil {
		response.Error(c, 500, "检测任务保存失败")
		return
	}
	go h.generate(item, key.Key)
	response.Success(c, item)
}

var detectionMarkupStart = regexp.MustCompile(`(?i)<!doctype html|<html\b|<svg\b`)

func detectionHTML(text string) string {
	start := detectionMarkupStart.FindStringIndex(text)
	if start == nil {
		return ""
	}
	source := text[start[0]:]
	lower := strings.ToLower(source)
	closing := "</html>"
	if strings.HasPrefix(lower, "<svg") {
		closing = "</svg>"
	}
	end := strings.LastIndex(lower, closing)
	if end < 0 {
		return ""
	}
	source = source[:end+len(closing)]
	if len(source) > 220<<10 {
		return ""
	}
	return source
}

func (h *ModelDetectionHandler) generate(item ModelDetectionRecord, key string) {
	start := time.Now()
	var output, failure string
	var temporary bool
	var wait time.Duration
	attempts := 0
	deadline := time.Now().Add(8 * time.Minute)
	for attempts < 2 {
		attempts++
		if !h.attemptProgress(item.ID, attempts, nil) {
			return
		}
		output, failure, temporary, wait = h.requestAttempt(item, key, deadline)
		if failure == "" || !temporary || attempts == 2 {
			break
		}
		if wait < h.retryDelay {
			wait = h.retryDelay
		}
		// Long provider cooldowns finish this job instead of tying up a worker.
		if wait > 30*time.Second || time.Now().Add(wait).After(deadline) {
			break
		}
		next := time.Now().Add(wait)
		if !h.attemptProgress(item.ID, attempts, &next) {
			return
		}
		time.Sleep(wait)
		if h.monitorConfig != nil {
			groups, e := h.groups(context.Background(), item.UserID)
			allowed := false
			for _, g := range groups {
				if g.ID == item.GroupID {
					allowed = true
				}
			}
			if e != nil || !allowed {
				failure = "检测分组配置已变化，请刷新后再试"
				temporary = false
				break
			}
		}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	state, err := h.read()
	if err != nil {
		log.Printf("model detection completion read: %v", err)
		return
	}
	for i := range state.Items {
		if state.Items[i].ID != item.ID {
			continue
		}
		state.Items[i].Attempts = attempts
		state.Items[i].RetryAt = nil
		state.Items[i].Text = output
		if item.Topic != "糖果问题" {
			state.Items[i].HTML = detectionHTML(output)
		}
		state.Items[i].Error = failure
		state.Items[i].DurationMS = int(time.Since(start).Milliseconds())
		if failure == "" {
			state.Items[i].Status = "generated"
			state.Generated++
		} else {
			state.Items[i].Status = "failed"
			if temporary {
				if wait < time.Minute {
					wait = time.Minute
				}
				until := time.Now().Add(wait).UTC()
				state.Items[i].CooldownUntil = &until
				if state.Cooldowns == nil {
					state.Cooldowns = map[string]time.Time{}
				}
				for scope, end := range state.Cooldowns {
					if end.Before(time.Now()) {
						delete(state.Cooldowns, scope)
					}
				}
				state.Cooldowns[fmt.Sprintf("%d:%d", item.UserID, item.GroupID)] = until
			}
			state.Failed++
		}
		if err := h.write(state); err != nil {
			log.Printf("model detection completion write: %v", err)
		}
		return
	}
}

func (h *ModelDetectionHandler) requestGeneration(item ModelDetectionRecord, key string) (string, string) {
	output, failure, _, _ := h.requestAttempt(item, key, time.Now().Add(8*time.Minute))
	return output, failure
}

func (h *ModelDetectionHandler) attemptProgress(id string, attempts int, retryAt *time.Time) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	state, err := h.read()
	if err != nil {
		return false
	}
	for i := range state.Items {
		if state.Items[i].ID == id {
			state.Items[i].Attempts = attempts
			state.Items[i].RetryAt = retryAt
			return h.write(state) == nil
		}
	}
	return false
}

func detectionHTTPFailure(resp *http.Response) (string, string, bool, time.Duration) {
	wait := time.Duration(0)
	if seconds, e := strconv.ParseInt(resp.Header.Get("Retry-After"), 10, 32); e == nil && seconds > 0 {
		wait = time.Duration(seconds) * time.Second
	} else if date, e := http.ParseTime(resp.Header.Get("Retry-After")); e == nil && date.After(time.Now()) {
		wait = time.Until(date)
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	// Classify, never echo upstream bodies (may contain credentials/provider details).
	detail := strings.ToLower(string(raw))
	if strings.Contains(detail, "model") && (strings.Contains(detail, "not found") || strings.Contains(detail, "not supported") || strings.Contains(detail, "unsupported") || strings.Contains(detail, "does not exist")) {
		return "", "该分组不支持此模型", false, 0
	}
	switch resp.StatusCode {
	case 400, 422:
		return "", "模型或请求参数不支持", false, 0
	case 401:
		return "", "密钥认证失败", false, 0
	case 402:
		return "", "余额或额度不足", false, 0
	case 403:
		return "", "分组权限或额度受限", false, 0
	case 404:
		return "", "模型或接口不存在", false, 0
	case 429:
		return "", "请求限流，请稍后再试", true, wait
	case 500, 502, 503:
		return "", "上游暂时不可用", true, wait
	case 504, 408:
		return "", "请求超时，请先查看用量", false, 0
	default:
		return "", fmt.Sprintf("请求失败（HTTP %d）", resp.StatusCode), false, 0
	}
}

func (h *ModelDetectionHandler) requestAttempt(item ModelDetectionRecord, key string, deadline time.Time) (string, string, bool, time.Duration) {
	endpoint := "/v1/responses"
	payload := gin.H{"model": item.Model, "input": []gin.H{{"role": "user", "content": item.Prompt}}, "max_output_tokens": 12000, "stream": true}
	if item.Platform == "anthropic" || item.Platform == "antigravity" {
		endpoint = "/v1/messages"
		payload = gin.H{"model": item.Model, "messages": []gin.H{{"role": "user", "content": item.Prompt}}, "max_tokens": 12000, "stream": true}
	}
	body, _ := json.Marshal(payload)
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "POST", h.gateway+endpoint, bytes.NewReader(body))
	if err != nil {
		return "", "创建生成请求失败", false, 0
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Anthropic-Version", "2023-06-01")
	resp, err := h.client.Do(req)
	if err != nil {
		return "", "连接超时或中断，请先查看用量", false, 0
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return detectionHTTPFailure(resp)
	}
	output, failure := detectionReadOutput(resp)
	if failure != "" {
		return "", failure, false, 0
	}
	if strings.TrimSpace(output) == "" || len(output) > 220<<10 {
		return "", "模型返回空内容或回答过大", false, 0
	}
	if item.Topic == "鹈鹕骑自行车" && detectionHTML(output) == "" {
		return output, "模型未返回完整 HTML/SVG", false, 0
	}
	return output, "", false, 0
}

func detectionReadOutput(resp *http.Response) (string, string) {
	if strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		return detectionReadStream(resp.Body)
	}
	var data struct {
		Status     string `json:"status"`
		StopReason string `json:"stop_reason"`
		Output     []struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&data); err != nil {
		return "", "网关响应格式异常或内容过大"
	}
	if (data.Status != "" && data.Status != "completed") || data.StopReason == "max_tokens" {
		return "", "模型输出未完成，请缩短提示词后重试。"
	}
	var parts []string
	for _, out := range data.Output {
		for _, part := range out.Content {
			parts = append(parts, part.Text)
		}
	}
	for _, part := range data.Content {
		parts = append(parts, part.Text)
	}
	return strings.Join(parts, "\n"), ""
}

// Require a terminal success event; a disconnected stream is never a complete answer.
func detectionReadStream(body io.Reader) (string, string) {
	scanner := bufio.NewScanner(io.LimitReader(body, 5<<20))
	scanner.Buffer(make([]byte, 4096), 2<<20)
	var output strings.Builder
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			continue
		}
		var event struct {
			Type     string          `json:"type"`
			Delta    json.RawMessage `json:"delta"`
			Response json.RawMessage `json:"response"`
		}
		if json.Unmarshal([]byte(payload), &event) != nil {
			return "", "模型流式响应格式异常"
		}
		switch event.Type {
		case "response.output_text.delta":
			var delta string
			if json.Unmarshal(event.Delta, &delta) != nil {
				return "", "模型文字片段格式异常"
			}
			output.WriteString(delta)
		case "content_block_delta", "message_delta":
			var delta struct {
				Text       string `json:"text"`
				StopReason string `json:"stop_reason"`
			}
			if json.Unmarshal(event.Delta, &delta) != nil {
				return "", "模型文字片段格式异常"
			}
			if delta.StopReason == "max_tokens" {
				return "", "模型输出达到长度上限，未完成。"
			}
			output.WriteString(delta.Text)
		case "response.completed":
			final, failure := detectionReadOutput(&http.Response{Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(event.Response))})
			if failure != "" {
				return "", failure
			}
			if final != "" {
				return final, ""
			}
			return output.String(), ""
		case "message_stop":
			return output.String(), ""
		case "error", "response.failed", "response.incomplete":
			return "", "模型生成中断，请查看用量后再决定是否重试。"
		}
		if output.Len() > 220<<10 {
			return "", "模型回答超过 220 KB。"
		}
	}
	return "", "模型连接结束但未确认生成完成，请查看用量后再决定是否重试。"
}

const detectionPelicanPrompt = "创建一个html，内容是SVG绘制鹈鹕骑自行车的2d动画。"

const detectionCandyPrompt = `在一个黑色的袋子里放有三种口味的糖果，每种糖果有两种不同的形状（圆形和五角星形，不同的形状靠手感可以分辨）。现已知不同口味的糖和不同形状的数量统计如下表。参赛者需要在活动前决定摸出的糖果数目，那么，最少取出多少个糖果才能保证手中同时拥有不同形状的苹果味和桃子味的糖？（同时手中有圆形苹果味匹配五角星桃子味糖果，或者有圆形桃子味匹配五角星苹果味糖果都满足要求）

        苹果味 桃子味 西瓜味
圆形       7     9     8
五角星形   7     6     4

只返回结果数值。`
