package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestRememberModelForNewSessionAfterReattach(t *testing.T) {
	now := time.Now()
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID: "inst-1", WorkspaceRoot: "/workspace/project", WorkspaceKey: "/workspace/project",
		Source: "headless", Managed: true, Online: true, Threads: map[string]*state.ThreadRecord{},
	})
	attach := control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"}
	svc.ApplySurfaceAction(attach)
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionModelCommand, SurfaceSessionID: "surface-1", Text: "/model gpt-6-astra medium"})
	preferences := svc.ModelPreferences()
	key := state.WorkspaceDefaultsStorageKey("/workspace/project", state.CodexInstanceBackendContract(""))
	if preferences[key].Model != "gpt-6-astra" || preferences[key].ReasoningEffort != "medium" {
		t.Fatalf("model choice not remembered: %#v", preferences)
	}
	surface := svc.root.Surfaces["surface-1"]
	svc.finalizeDetachedSurface(surface)
	svc.ApplySurfaceAction(attach)
	if surface.PromptOverride.Model != "" {
		t.Fatal("reattach should use saved preferences, not retain temporary overrides")
	}
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionNewThread, SurfaceSessionID: "surface-1"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionTextMessage, SurfaceSessionID: "surface-1", MessageID: "msg-1", Text: "hello"})
	if len(surface.QueueItems) != 1 {
		t.Fatalf("expected new-session prompt, got %#v", surface.QueueItems)
	}
	for _, item := range surface.QueueItems {
		if item.FrozenOverride.Model != "gpt-6-astra" || item.FrozenOverride.ReasoningEffort != "medium" {
			t.Fatalf("new session did not inherit saved choice: %#v", item.FrozenOverride)
		}
	}
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionModelCommand, SurfaceSessionID: "surface-1", Text: "/model clear"})
	if len(svc.ModelPreferences()) != 0 {
		t.Fatalf("clear must forget saved choice: %#v", svc.ModelPreferences())
	}
}

func TestModelPreferencesRestoreAndIsolation(t *testing.T) {
	now := time.Now()
	svc := newServiceForTest(&now)
	key := state.WorkspaceDefaultsStorageKey("/workspace/project", state.CodexInstanceBackendContract(""))
	svc.MaterializeModelPreferences(map[string]state.ModelConfigRecord{key: {Model: "gpt-6-astra", ReasoningEffort: "medium", AccessMode: agentproto.AccessModeFullAccess}})
	for _, tc := range []struct{ workspace, provider, model string }{
		{"/workspace/project", "", "gpt-6-astra"},
		{"/workspace/other", "", ""},
		{"/workspace/project", "other", ""},
	} {
		got := svc.ModelPreferences()[state.WorkspaceDefaultsStorageKey(tc.workspace, state.CodexInstanceBackendContract(tc.provider))]
		if got.Model != tc.model || got.AccessMode != "" {
			t.Fatalf("preference isolation failed for %#v: %#v", tc, got)
		}
	}
}
