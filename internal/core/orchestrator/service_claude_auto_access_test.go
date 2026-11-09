package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestClaudeAutoAccessDefaultAndExplicitChoices(t *testing.T) {
	now := time.Now()
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID: "inst-claude", WorkspaceRoot: "/data/research", WorkspaceKey: "/data/research",
		Backend: agentproto.BackendClaude, Source: "headless", Managed: true, Online: true,
		Threads: map[string]*state.ThreadRecord{"thread-1": {ThreadID: "thread-1", CWD: "/data/research"}},
	})
	svc.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
	surface := svc.root.Surfaces["surface-1"]
	surface.ProductMode = state.ProductModeNormal
	surface.Backend = agentproto.BackendClaude
	surface.AttachedInstanceID = "inst-claude"
	inst := svc.root.Instances["inst-claude"]
	resolve := func() string {
		return svc.resolveFrozenPromptOverride(inst, surface, "thread-1", "/data/research", state.ModelConfigRecord{}).AccessMode
	}
	if got := resolve(); got != agentproto.AccessModeAuto {
		t.Fatalf("fresh Claude prompt default = %q, want auto", got)
	}
	inst.Threads["thread-1"].ObservedAccessMode = agentproto.AccessModeConfirm
	if got := resolve(); got != agentproto.AccessModeConfirm {
		t.Fatalf("observed native choice must survive: %q", got)
	}
	for _, choice := range []string{"auto", "confirm", "full"} {
		svc.ApplySurfaceAction(control.Action{Kind: control.ActionAccessCommand, SurfaceSessionID: "surface-1", Text: "/access " + choice})
		if got := resolve(); got != agentproto.NormalizeAccessMode(choice) {
			t.Fatalf("/access %s: frozen access = %q", choice, got)
		}
	}
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAccessCommand, SurfaceSessionID: "surface-1", Text: "/access clear"})
	inst.Threads["thread-1"].ObservedAccessMode = ""
	if got := resolve(); got != agentproto.AccessModeAuto {
		t.Fatalf("clear without observed choice must restore auto: %q", got)
	}
}

func TestNonClaudeAccessAutoDoesNotMutatePermission(t *testing.T) {
	for _, backend := range []agentproto.Backend{agentproto.BackendCodex, agentproto.BackendOpenCode} {
		now := time.Now()
		svc := newServiceForTest(&now)
		svc.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
		surface := svc.root.Surfaces["surface-1"]
		surface.Backend = backend
		surface.PromptOverride.AccessMode = agentproto.AccessModeConfirm
		svc.handleAccessCommand(surface, control.Action{Kind: control.ActionAccessCommand, SurfaceSessionID: "surface-1", Text: "/access auto"})
		if got := surface.PromptOverride.AccessMode; got != agentproto.AccessModeConfirm {
			t.Fatalf("backend %s: rejected auto mutated permission to %q", backend, got)
		}
	}
}
