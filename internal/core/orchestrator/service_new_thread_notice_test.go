package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestNewThreadNoticesReportNextPromptSettings(t *testing.T) {
	now := time.Now()
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID: "inst-1", WorkspaceRoot: "/workspace/project", WorkspaceKey: "/workspace/project",
		Source: "headless", Managed: true, Online: true, Threads: map[string]*state.ThreadRecord{},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionModelCommand, SurfaceSessionID: "surface-1", Text: "/model gpt-5.6-sol medium"})
	surface := svc.root.Surfaces["surface-1"]
	surface.PromptOverride.AccessMode = agentproto.AccessModeFullAccess

	for _, code := range []string{"new_thread_ready", "already_new_thread_ready", "new_thread_ready_reset"} {
		if code == "new_thread_ready_reset" {
			surface.DispatchMode = state.DispatchModePausedForLocal
			svc.ApplySurfaceAction(control.Action{Kind: control.ActionTextMessage, SurfaceSessionID: "surface-1", MessageID: "draft-1", Text: "queued draft"})
		}
		events := svc.ApplySurfaceAction(control.Action{Kind: control.ActionNewThread, SurfaceSessionID: "surface-1"})
		if len(events) == 0 {
			t.Fatalf("%s: no reply", code)
		}
		notice := events[len(events)-1].Notice
		if notice == nil || notice.Code != code || notice.PromptSettings == nil {
			t.Fatalf("%s: missing settings: %#v", code, notice)
		}
		got := notice.PromptSettings
		want := svc.SurfaceSnapshot("surface-1").NextPrompt
		if !got.CreateThread || got.EffectiveModel != "gpt-5.6-sol" || got.EffectiveReasoningEffort != "medium" || got.EffectiveAccessMode != want.EffectiveAccessMode || got.EffectivePlanMode != want.EffectivePlanMode {
			t.Fatalf("%s: incorrect settings: %#v, snapshot %#v", code, got, want)
		}
		if notice.Text != "" || len(notice.Sections) != 1 {
			t.Fatalf("%s: expected structured reply: %#v", code, notice)
		}
	}
}
