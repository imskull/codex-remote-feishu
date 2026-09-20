package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestRememberModelForNewSessionAcrossWorkspacesInSameChat(t *testing.T) {
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
	key := state.ChatModelPreferenceKey("", "chat-1", "")
	if preferences[key].Model != "gpt-6-astra" || preferences[key].ReasoningEffort != "medium" {
		t.Fatalf("model choice not remembered: %#v", preferences)
	}
	surface := svc.root.Surfaces["surface-1"]
	svc.finalizeDetachedSurface(surface)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID: "inst-2", WorkspaceRoot: "/workspace/other", WorkspaceKey: "/workspace/other",
		Source: "headless", Managed: true, Online: true, Threads: map[string]*state.ThreadRecord{},
	})
	attach.InstanceID = "inst-2"
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
		if surface.ClaimedWorkspaceKey != "/workspace/other" {
			t.Fatalf("prompt did not switch workspace: %#v", item)
		}
	}
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionModelCommand, SurfaceSessionID: "surface-1", Text: "/model clear"})
	if len(svc.ModelPreferences()) != 0 {
		t.Fatalf("clear must forget saved choice: %#v", svc.ModelPreferences())
	}
}

func TestRestoredModelPreferenceFollowsChatInsteadOfWorkspace(t *testing.T) {
	now := time.Now()
	svc := newServiceForTest(&now)
	key := state.ChatModelPreferenceKey("app-1", "chat-1", "")
	svc.MaterializeModelPreferences(map[string]state.ModelConfigRecord{key: {Model: "gpt-6-astra", ReasoningEffort: "high"}})
	svc.UpsertInstance(&state.InstanceRecord{InstanceID: "inst-1", WorkspaceRoot: "/workspace/other", WorkspaceKey: "/workspace/other", Source: "headless", Managed: true, Online: true, Threads: map[string]*state.ThreadRecord{}})
	for _, tc := range []struct{ surface, gateway, chat, actor, model string }{
		{"surface-1", "app-1", "chat-1", "user-2", "gpt-6-astra"},
		{"surface-2", "app-1", "chat-2", "user-2", defaultModel},
		{"surface-3", "app-2", "chat-1", "user-2", defaultModel},
	} {
		svc.MaterializeSurface(tc.surface, tc.gateway, tc.chat, tc.actor)
		svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: tc.surface, GatewayID: tc.gateway, ChatID: tc.chat, ActorUserID: tc.actor, InstanceID: "inst-1"})
		svc.ApplySurfaceAction(control.Action{Kind: control.ActionNewThread, SurfaceSessionID: tc.surface})
		summary := svc.SurfaceSnapshot(tc.surface).NextPrompt
		if !summary.CreateThread || summary.EffectiveModel != tc.model {
			t.Fatalf("chat preference resolution failed for %#v: %#v", tc, summary)
		}
		svc.finalizeDetachedSurface(svc.root.Surfaces[tc.surface])
	}
}

func TestModelPreferencesRestoreAndIsolation(t *testing.T) {
	now := time.Now()
	svc := newServiceForTest(&now)
	key := state.ChatModelPreferenceKey("app-1", "chat-1", "")
	svc.MaterializeModelPreferences(map[string]state.ModelConfigRecord{key: {Model: "gpt-6-astra", ReasoningEffort: "medium", AccessMode: agentproto.AccessModeFullAccess}})
	for _, tc := range []struct{ gateway, chat, provider, model string }{
		{"app-1", "chat-1", "", "gpt-6-astra"},
		{"app-1", "chat-2", "", ""},
		{"app-2", "chat-1", "", ""},
		{"app-1", "chat-1", "other", ""},
	} {
		got := svc.ModelPreferences()[state.ChatModelPreferenceKey(tc.gateway, tc.chat, tc.provider)]
		if got.Model != tc.model || got.AccessMode != "" {
			t.Fatalf("preference isolation failed for %#v: %#v", tc, got)
		}
	}
}
