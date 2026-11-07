package orchestrator

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/testutil"
)

func workspacePathActionForTest(t *testing.T, text string) control.Action {
	t.Helper()
	action, ok := control.ParseFeishuTextActionWithoutCatalog(text)
	if !ok {
		t.Fatalf("workspace command %q was not parsed", text)
	}
	action.SurfaceSessionID = "surface-1"
	action.ChatID = "chat-1"
	action.ActorUserID = "user-1"
	return action
}

func TestWorkspacePathCommandStartsUnknownWorkspace(t *testing.T) {
	for _, contract := range []state.SurfaceBackendContract{
		state.HeadlessCodexSurfaceBackendContract("default"),
		state.HeadlessClaudeSurfaceBackendContract("devseek"),
		state.HeadlessOpenCodeSurfaceBackendContract("op_team"),
	} {
		for _, quoted := range []bool{false, true} {
			t.Run(string(contract.Backend)+"/"+map[bool]string{false: "unquoted", true: "quoted"}[quoted], func(t *testing.T) {
				now := time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC)
				svc := newServiceForTest(&now)
				svc.MaterializeSurfaceResumeContract("surface-1", "", "chat-1", "user-1", contract, "", "")
				if contract.Backend == agentproto.BackendOpenCode {
					svc.root.Surfaces["surface-1"].OpenCodeAdmissionRef = &state.OpenCodeAdmissionRef{ProfileRef: state.OpenCodeProfileRef{ID: "op_team", Revision: 7}}
				}
				workspace := filepath.Join(t.TempDir(), "stock agent")
				if err := os.Mkdir(workspace, 0o755); err != nil {
					t.Fatal(err)
				}
				argument := workspace
				if quoted {
					argument = `"` + workspace + `"`
				}
				events := svc.ApplySurfaceAction(workspacePathActionForTest(t, "/workspace "+argument))
				surface := svc.root.Surfaces["surface-1"]
				pending := surface.PendingHeadless
				if pending == nil || !pending.PrepareNewThread || !testutil.SamePath(pending.ThreadCWD, workspace) {
					t.Fatalf("expected headless launch for supplied workspace, pending=%#v, events=%#v", pending, events)
				}
				if pending.Backend != contract.Backend {
					t.Fatalf("launch backend = %q, want %q", pending.Backend, contract.Backend)
				}
				var started bool
				for _, event := range events {
					if event.DaemonCommand != nil && event.DaemonCommand.Kind == control.DaemonCommandStartHeadless {
						started = true
					}
				}
				if !started {
					t.Fatalf("expected headless start command, got %#v", events)
				}
				svc.UpsertInstance(&state.InstanceRecord{
					InstanceID: pending.InstanceID, WorkspaceRoot: workspace, WorkspaceKey: workspace,
					Source: "headless", Managed: true, Online: true, Threads: map[string]*state.ThreadRecord{},
					Backend: pending.Backend, CodexProfileID: pending.CodexProfileID, ClaudeProfileID: pending.ClaudeProfileID,
					OpenCodeProfileID: pending.OpenCodeProfileID, OpenCodeAdmissionRef: state.NormalizeOpenCodeAdmissionRef(pending.OpenCodeAdmissionRef),
				})
				svc.ApplyInstanceConnected(pending.InstanceID)
				if surface.RouteMode != state.RouteModeNewThreadReady || !testutil.SamePath(surface.PreparedThreadCWD, workspace) {
					t.Fatalf("expected new-thread-ready after workspace connects, got %#v", surface)
				}
			})
		}
	}
}

func TestWorkspacePathCommandRejectsInvalidDirectoryWithoutChangingRoute(t *testing.T) {
	for _, kind := range []string{"missing", "file", "relative"} {
		t.Run(kind, func(t *testing.T) {
			now := time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC)
			svc := newServiceForTest(&now)
			workspace := t.TempDir()
			svc.UpsertInstance(&state.InstanceRecord{
				InstanceID: "current", WorkspaceRoot: workspace, WorkspaceKey: workspace,
				Online: true, Threads: map[string]*state.ThreadRecord{},
			})
			svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachWorkspace, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", WorkspaceKey: workspace})
			surface := svc.root.Surfaces["surface-1"]
			instanceID, claim, route := surface.AttachedInstanceID, surface.ClaimedWorkspaceKey, surface.RouteMode
			badPath := filepath.Join(t.TempDir(), kind)
			if kind == "relative" {
				badPath = "relative/stock-agent"
			}
			if kind == "file" {
				if err := os.WriteFile(badPath, []byte("file"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			events := svc.ApplySurfaceAction(workspacePathActionForTest(t, "/workspace "+badPath))
			if surface.AttachedInstanceID != instanceID || surface.ClaimedWorkspaceKey != claim || surface.RouteMode != route || surface.PendingHeadless != nil {
				t.Fatalf("invalid path changed current route: %#v", surface)
			}
			if len(events) == 0 || events[0].Notice == nil {
				t.Fatalf("expected invalid path feedback, got %#v", events)
			}
			if _, err := os.Stat(badPath); kind == "missing" && !os.IsNotExist(err) {
				t.Fatalf("missing directory should not be created, stat error=%v", err)
			}
		})
	}
}

func TestWorkspacePathCommandRejectsVSCodeMode(t *testing.T) {
	now := time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	materializeVSCodeSurfaceForTest(svc, "surface-1")
	events := svc.ApplySurfaceAction(workspacePathActionForTest(t, "/workspace "+t.TempDir()))
	surface := svc.root.Surfaces["surface-1"]
	if surface.ProductMode != state.ProductModeVSCode || surface.PendingHeadless != nil || surface.AttachedInstanceID != "" {
		t.Fatalf("workspace path changed vscode mode or route: %#v", surface)
	}
	if len(events) == 0 || events[0].Notice == nil {
		t.Fatalf("expected vscode rejection feedback, got %#v", events)
	}
}

func TestWorkspacePathCommandPreservesSameWorkspaceSession(t *testing.T) {
	now := time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	workspace := t.TempDir()
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID: "current", WorkspaceRoot: workspace, WorkspaceKey: workspace, Online: true,
		Threads: map[string]*state.ThreadRecord{"existing": {ThreadID: "existing", CWD: workspace}},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachWorkspace, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", WorkspaceKey: workspace})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionUseThread, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", ThreadID: "existing"})
	surface := svc.root.Surfaces["surface-1"]
	if surface.SelectedThreadID != "existing" {
		t.Fatalf("expected selected session before same-workspace command, got %#v", surface)
	}
	events := svc.ApplySurfaceAction(workspacePathActionForTest(t, "/workspace "+workspace))
	if surface.SelectedThreadID != "existing" || surface.RouteMode != state.RouteModePinned || surface.PendingHeadless != nil {
		t.Fatalf("same workspace reset existing session: %#v", surface)
	}
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "workspace_already_attached" {
		t.Fatalf("expected already-attached notice, got %#v", events)
	}
}

func TestWorkspacePathCommandRejectsBusyTarget(t *testing.T) {
	now := time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	workspace := t.TempDir()
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID: "target", WorkspaceRoot: workspace, WorkspaceKey: workspace,
		Online: true, Threads: map[string]*state.ThreadRecord{},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachWorkspace, SurfaceSessionID: "owner", ChatID: "owner-chat", ActorUserID: "owner-user", WorkspaceKey: workspace})
	events := svc.ApplySurfaceAction(workspacePathActionForTest(t, "/workspace "+workspace))
	surface := svc.root.Surfaces["surface-1"]
	if surface.PendingHeadless != nil || surface.AttachedInstanceID != "" || surface.ClaimedWorkspaceKey != "" {
		t.Fatalf("busy workspace was taken over: %#v", surface)
	}
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "workspace_busy" {
		t.Fatalf("expected busy-workspace notice, got %#v", events)
	}
}

func TestWorkspacePathCommandRejectsRunningCurrentRequest(t *testing.T) {
	now := time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	workspace := t.TempDir()
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID: "current", WorkspaceRoot: workspace, WorkspaceKey: workspace,
		Online: true, Threads: map[string]*state.ThreadRecord{},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachWorkspace, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", WorkspaceKey: workspace})
	surface := svc.root.Surfaces["surface-1"]
	surface.ActiveQueueItemID = "running"
	surface.QueueItems = map[string]*state.QueueItemRecord{"running": {Status: state.QueueItemRunning}}
	events := svc.ApplySurfaceAction(workspacePathActionForTest(t, "/workspace "+t.TempDir()))
	if surface.PendingHeadless != nil || surface.AttachedInstanceID != "current" || !testutil.SamePath(surface.ClaimedWorkspaceKey, workspace) || surface.ActiveQueueItemID != "running" {
		t.Fatalf("workspace changed during active request: %#v", surface)
	}
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "thread_switch_running" {
		t.Fatalf("expected running-request rejection, got %#v", events)
	}
}

func TestWorkspacePathCommandSwitchesAttachedWorkspaceToCompatibleOnlineTarget(t *testing.T) {
	now := time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	current, target := t.TempDir(), t.TempDir()
	for instanceID, workspace := range map[string]string{"current": current, "target": target} {
		svc.UpsertInstance(&state.InstanceRecord{
			InstanceID: instanceID, WorkspaceRoot: workspace, WorkspaceKey: workspace, Online: true,
			Threads: map[string]*state.ThreadRecord{instanceID + "-session": {ThreadID: instanceID + "-session", CWD: workspace}},
		})
	}
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachWorkspace, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", WorkspaceKey: current})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionUseThread, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", ThreadID: "current-session"})
	surface := svc.root.Surfaces["surface-1"]
	if surface.AttachedInstanceID != "current" || surface.SelectedThreadID != "current-session" {
		t.Fatalf("expected current workspace and session before switching, got %#v", surface)
	}
	events := svc.ApplySurfaceAction(workspacePathActionForTest(t, "/workspace "+target))
	if surface.AttachedInstanceID != "target" || !testutil.SamePath(surface.ClaimedWorkspaceKey, target) || surface.RouteMode != state.RouteModeNewThreadReady || !testutil.SamePath(surface.PreparedThreadCWD, target) || surface.SelectedThreadID != "" || surface.PendingHeadless != nil {
		t.Fatalf("expected switch to online target in new-thread-ready state, got %#v, events=%#v", surface, events)
	}
	for _, event := range events {
		if event.DaemonCommand != nil && event.DaemonCommand.Kind == control.DaemonCommandStartHeadless {
			t.Fatalf("compatible online target should not launch another runtime: %#v", event)
		}
	}
}
