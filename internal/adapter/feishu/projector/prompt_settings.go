package projector

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

// FormatEffectivePromptSettings returns plain text for resolved prompt settings.
func FormatEffectivePromptSettings(summary control.PromptRouteSummary) string {
	plan := "关闭"
	if strings.EqualFold(strings.TrimSpace(summary.EffectivePlanMode), "on") {
		plan = "开启"
	}
	value := func(s string) string {
		if s = strings.TrimSpace(s); s != "" {
			return s
		}
		return "未知"
	}
	access := "未知"
	if strings.TrimSpace(summary.EffectiveAccessMode) != "" {
		access = agentproto.DisplayAccessModeShort(summary.EffectiveAccessMode)
	}
	return strings.Join([]string{
		"Plan " + plan,
		"模型 " + value(summary.EffectiveModel),
		"推理 " + value(summary.EffectiveReasoningEffort),
		"权限 " + access,
	}, "，")
}
