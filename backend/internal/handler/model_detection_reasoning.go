package handler

import (
	"fmt"
	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/gin-gonic/gin"
	"slices"
	"strings"
)

func validDetectionEffort(effort, platform string) bool {
	switch effort {
	case "", "none", "low", "medium", "high", "max":
		return true
	case "minimal", "xhigh":
		return platform != "anthropic" && platform != "antigravity"
	}
	return false
}

// The site's gateway applies normal model mappings and protocol compatibility.
func applyDetectionEffort(payload gin.H, platform, effort string) error {
	if !validDetectionEffort(effort, platform) {
		return fmt.Errorf("unsupported reasoning effort")
	}
	if effort == "" {
		return nil
	}
	if platform != "anthropic" && platform != "antigravity" {
		payload["reasoning"] = gin.H{"effort": effort}
		return nil
	}
	if effort == "none" {
		payload["thinking"] = gin.H{"type": "disabled"}
		return nil
	}
	budgets := map[string]int{"low": 2048, "medium": 4096, "high": 8192, "max": 10000}
	payload["thinking"] = gin.H{"type": "enabled", "budget_tokens": budgets[effort]}
	model, _ := payload["model"].(string)
	levels := claude.EffortLevelsForModel(model)
	if len(levels) > 0 {
		if !slices.Contains(levels, effort) {
			return fmt.Errorf("unsupported model reasoning effort")
		}
		payload["output_config"] = gin.H{"effort": effort}
		if !strings.Contains(model, "opus-4-5") && !strings.Contains(model, "opus-4.5") {
			payload["thinking"] = gin.H{"type": "adaptive"}
		}
	}
	return nil
}
