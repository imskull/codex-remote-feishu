package feishu

import (
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
)

func TestProjectNewThreadSettingsAsPlainText(t *testing.T) {
	for _, tc := range []struct{ model, plan, access, want string }{
		{"gpt-5.6-sol", "off", "full", "Plan 关闭，模型 gpt-5.6-sol，推理 medium，权限 full"},
		{"model **literal** <tag>", "on", "confirm", "Plan 开启，模型 model **literal** <tag>，推理 medium，权限 confirm"},
		{"", "off", "", "Plan 关闭，模型 未知，推理 medium，权限 未知"},
	} {
		ops := NewProjector().ProjectEvent("chat-1", eventcontract.Event{
			Kind: eventcontract.KindNotice,
			Notice: &control.Notice{
				Code:           "new_thread_ready",
				Sections:       []control.FeishuCardTextSection{{Lines: []string{"已准备新建会话。"}}},
				PromptSettings: &control.PromptRouteSummary{EffectiveModel: tc.model, EffectivePlanMode: tc.plan, EffectiveReasoningEffort: "medium", EffectiveAccessMode: tc.access},
			},
		})
		if len(ops) != 1 || ops[0].Kind != OperationSendCard || ops[0].CardBody != "" || !containsCardTextExact(ops[0].CardElements, tc.want) {
			t.Fatalf("missing plain-text settings %q: %#v", tc.want, ops)
		}
		for _, element := range ops[0].CardElements {
			if strings.Contains(markdownContent(element), "medium") || (tc.model != "" && strings.Contains(markdownContent(element), tc.model)) {
				t.Fatalf("settings leaked into markdown: %#v", element)
			}
		}
	}
}
