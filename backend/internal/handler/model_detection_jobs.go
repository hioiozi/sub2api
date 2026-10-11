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
	}
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
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
	var input struct {
		KeyID  int64  `json:"key_id"`
		Model  string `json:"model"`
		Topic  string `json:"topic"`
		Prompt string `json:"prompt"`
	}
	if c.ShouldBindJSON(&input) != nil {
		response.Error(c, 400, "检测配置格式无效")
		return
	}
	input.Model = strings.TrimSpace(input.Model)
	input.Topic = strings.TrimSpace(input.Topic)
	input.Prompt = strings.TrimSpace(input.Prompt)
	switch input.Topic {
	case "鹈鹕骑自行车":
		input.Prompt = detectionPelicanPrompt
	case "糖果问题":
		input.Prompt = detectionCandyPrompt
	case "自定义":
	default:
		response.Error(c, 400, "请选择鹈鹕骑自行车、糖果问题或自定义")
		return
	}
	if input.KeyID <= 0 || input.Model == "" || len(input.Model) > 120 || input.Topic == "" || len(input.Topic) > 80 || input.Prompt == "" || len(input.Prompt) > 4000 {
		response.Error(c, 400, "请填写分组、模型和提示词")
		return
	}
	if h.getKey == nil {
		response.Error(c, 503, "检测服务未就绪")
		return
	}
	key, err := h.getKey(c.Request.Context(), input.KeyID)
	if err != nil || key == nil || key.UserID != subject.UserID {
		response.Error(c, 404, "检测密钥不存在")
		return
	}
	if !key.IsActive() || key.IsExpired() || key.IsQuotaExhausted() || key.Group == nil || key.GroupID == nil {
		response.Error(c, 400, "请选择有效且绑定分组的密钥")
		return
	}
	if key.User == nil || key.User.Balance <= 0 {
		response.Error(c, 403, "余额不足，请充值后再开始检测")
		return
	}
	// Loopback requests must never silently bypass a user's key IP restrictions.
	if len(key.IPWhitelist) > 0 || len(key.IPBlacklist) > 0 {
		response.Error(c, 400, "后台检测请使用本分组未设置 IP 限制的专用密钥")
		return
	}
	var id [12]byte
	if _, err := rand.Read(id[:]); err != nil {
		response.Error(c, 500, "创建检测任务失败")
		return
	}
	item := ModelDetectionRecord{ID: hex.EncodeToString(id[:]), UserID: subject.UserID, GroupID: *key.GroupID, KeyName: key.Group.Name, Platform: key.Group.Platform, Model: input.Model, Topic: input.Topic, Prompt: input.Prompt, Status: "running", CreatedAt: time.Now().UTC()}
	h.mu.Lock()
	defer h.mu.Unlock()
	state, err := h.read()
	if err != nil {
		response.Error(c, 500, "检测记录读取失败")
		return
	}
	active := 0
	for _, other := range state.Items {
		if other.Status == "running" {
			active++
			if other.UserID == subject.UserID {
				response.Error(c, 409, "已有检测正在运行，请等待完成")
				return
			}
		}
	}
	if active >= 4 {
		response.Error(c, 429, "当前检测任务较多，请稍后重试")
		return
	}
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
	output, failure := h.requestGeneration(item, key)
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
			state.Failed++
		}
		if err := h.write(state); err != nil {
			log.Printf("model detection completion write: %v", err)
		}
		return
	}
}

func (h *ModelDetectionHandler) requestGeneration(item ModelDetectionRecord, key string) (string, string) {
	endpoint := "/v1/responses"
	payload := gin.H{"model": item.Model, "input": []gin.H{{"role": "user", "content": item.Prompt}}, "max_output_tokens": 12000, "stream": true}
	if item.Platform == "anthropic" || item.Platform == "antigravity" {
		endpoint = "/v1/messages"
		payload = gin.H{"model": item.Model, "messages": []gin.H{{"role": "user", "content": item.Prompt}}, "max_tokens": 12000, "stream": true}
	}
	body, _ := json.Marshal(payload)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "POST", h.gateway+endpoint, bytes.NewReader(body))
	if err != nil {
		return "", "创建生成请求失败"
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Anthropic-Version", "2023-06-01")
	resp, err := h.client.Do(req)
	if err != nil {
		return "", "网关连接失败或超过 8 分钟，请查看用量后再决定是否重试。"
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Sprintf("网关返回 HTTP %d，请检查分组、模型、余额及请求日志。", resp.StatusCode)
	}
	output, failure := detectionReadOutput(resp)
	if failure != "" {
		return "", failure
	}
	if strings.TrimSpace(output) == "" || len(output) > 220<<10 {
		return "", "模型返回空内容，或回答超过 220 KB。"
	}
	if item.Topic == "鹈鹕骑自行车" && detectionHTML(output) == "" {
		return "", "模型未返回完整的 HTML/SVG，或作品超过 220 KB。"
	}
	return output, ""
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

const detectionPelicanPrompt = "创建一个完整的单文件 HTML，内容是 SVG 绘制的鹈鹕骑自行车的海边 2D 动画。画面要有明确的角色、海岸背景和连续的骑行动作。CSS 和 SVG 必须内嵌，不引用外部资源。使用 CSS 或 SVG 原生动画，不使用 JavaScript。只输出完整 HTML。"

const detectionCandyPrompt = `在一个黑色的袋子里放有三种口味的糖果，每种糖果有两种不同的形状（圆形和五角星形，不同的形状靠手感可以分辨）。现已知不同口味的糖和不同形状的数量统计如下表。参赛者需要在活动前决定摸出的糖果数目，那么，最少取出多少个糖果才能保证手中同时拥有不同形状的苹果味和桃子味的糖？（同时手中有圆形苹果味匹配五角星桃子味糖果，或者有圆形桃子味匹配五角星苹果味糖果都满足要求）

        苹果味 桃子味 西瓜味
圆形       7     9     8
五角星形   7     6     4

只返回结果数值。`
