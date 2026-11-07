package orchestrator

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func sideTestService(t *testing.T) (*Service, *state.SurfaceConsoleRecord, *time.Time) {
	t.Helper()
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	s := newServiceForTest(&now)
	s.UpsertInstance(&state.InstanceRecord{InstanceID: "inst", Backend: agentproto.BackendCodex, Online: true, WorkspaceRoot: "/tmp/side", WorkspaceKey: "/tmp/side",
		CapabilitiesDeclared: true, Capabilities: agentproto.Capabilities{SideSession: true},
		Threads: map[string]*state.ThreadRecord{"parent": {ThreadID: "parent", CWD: "/tmp/side", Loaded: true}}})
	surface := s.ensureSurface(control.Action{SurfaceSessionID: "surface", ChatID: "chat", ActorUserID: "user"})
	surface.AttachedInstanceID, surface.SelectedThreadID, surface.RouteMode = "inst", "parent", state.RouteModePinned
	s.bindThreadClaim(surface, "inst", "parent")
	return s, surface, &now
}

func sideAction(s *Service, text string) []eventcontract.Event {
	return s.ApplySurfaceAction(control.Action{Kind: control.ActionSideCommand, Text: text, SurfaceSessionID: "surface"})
}

func sideCommands(events []eventcontract.Event) []*agentproto.Command {
	var commands []*agentproto.Command
	for _, event := range events {
		if event.Command != nil {
			commands = append(commands, event.Command)
		}
	}
	return commands
}

func openTestSide(t *testing.T, s *Service, surface *state.SurfaceConsoleRecord, message string) string {
	t.Helper()
	events := sideAction(s, "/side "+message)
	commands := sideCommands(events)
	if len(commands) != 1 || commands[0].Kind != agentproto.CommandSideSessionOpen || commands[0].Target.ThreadID != "parent" {
		t.Fatalf("open: %#v", commands)
	}
	id := surface.SideSession.SessionID
	s.ApplyAgentEvent("inst", agentproto.Event{Kind: agentproto.EventSideSessionUpdated, SideSessionID: id, CommandID: commands[0].CommandID, Status: "open", ThreadID: "fork"})
	return id
}

func TestSidePersistentFollowupsAndParentOwnership(t *testing.T) {
	s, surface, _ := sideTestService(t)
	surface.ActiveQueueItemID = "main-queue"
	surface.QueueItems["main-queue"] = &state.QueueItemRecord{ID: "main-queue", Status: state.QueueItemRunning}
	inst := s.root.Instances["inst"]
	inst.ActiveThreadID, inst.ActiveTurnID = "parent", "main-turn"
	id := openTestSide(t, s, surface, "first")
	first := surface.SideSession.ActiveCommandID
	if first == "" || len(surface.SideSession.Queue) != 0 {
		t.Fatal("first message did not dispatch")
	}
	s.ApplyAgentEvent("inst", agentproto.Event{Kind: agentproto.EventTurnStarted, SideSessionID: id, ThreadID: "fork", TurnID: "side-turn", CommandID: first})
	if commands := sideCommands(sideAction(s, "/side")); len(commands) != 0 {
		t.Fatal("reentry created nested side")
	}
	s.ApplySurfaceAction(control.Action{Kind: control.ActionTextMessage, Text: "second", SurfaceSessionID: "surface", MessageID: "message2"})
	if len(surface.SideSession.Queue) != 1 {
		t.Fatal("followup did not queue independently")
	}
	s.ApplyAgentEvent("inst", agentproto.Event{Kind: agentproto.EventItemDelta, SideSessionID: id, ThreadID: "fork", TurnID: "side-turn", ItemID: "answer", ItemKind: "agent_message", Delta: "side result"})
	events := s.ApplyAgentEvent("inst", agentproto.Event{Kind: agentproto.EventTurnCompleted, SideSessionID: id, ThreadID: "fork", TurnID: "side-turn", Status: "completed"})
	commands := sideCommands(events)
	if len(commands) != 1 || commands[0].SideSessionID != id || commands[0].Target.ThreadID != "fork" || commands[0].Prompt.Inputs[0].Text != "second" {
		t.Fatalf("followup route: %#v", commands)
	}
	if len(events) == 0 || events[0].Notice == nil || events[0].Notice.Text != "side result" {
		t.Fatal("missing side response")
	}
	if surface.SelectedThreadID != "parent" || surface.ActiveQueueItemID != "main-queue" || inst.ActiveTurnID != "main-turn" {
		t.Fatal("side changed main execution ownership")
	}
	quit := sideCommands(sideAction(s, "/side quit"))
	if len(quit) != 1 || quit[0].Kind != agentproto.CommandSideSessionClose || surface.SideSession != nil {
		t.Fatal("quit did not remove overlay")
	}
	if commands := sideCommands(sideAction(s, "/side quit")); len(commands) != 0 {
		t.Fatal("repeated quit is not harmless")
	}
	if events := s.ApplyAgentEvent("inst", agentproto.Event{Kind: agentproto.EventThreadFocused, SideSessionID: id, ThreadID: "fork"}); len(events) != 0 {
		t.Fatal("late event escaped side")
	}
	if surface.SelectedThreadID != "parent" || inst.ActiveTurnID != "main-turn" {
		t.Fatal("quit disturbed main")
	}
}

func TestSideStopOnlySideAndRouteSwitchBlocked(t *testing.T) {
	s, surface, _ := sideTestService(t)
	id := openTestSide(t, s, surface, "first")
	s.ApplyAgentEvent("inst", agentproto.Event{Kind: agentproto.EventTurnStarted, SideSessionID: id, CommandID: surface.SideSession.ActiveCommandID, ThreadID: "fork", TurnID: "side-turn"})
	sideAction(s, "/side queued")
	commands := sideCommands(s.ApplySurfaceAction(control.Action{Kind: control.ActionStop, SurfaceSessionID: "surface"}))
	if len(commands) != 1 || commands[0].SideSessionID != id || commands[0].Target.TurnID != "side-turn" || len(surface.SideSession.Queue) != 0 {
		t.Fatal("stop crossed side boundary")
	}
	for _, kind := range []control.ActionKind{control.ActionUseThread, control.ActionModeCommand, control.ActionWorkspaceRoot, control.ActionNewThread, control.ActionReviewCommand} {
		events := s.ApplySurfaceAction(control.Action{Kind: kind, SurfaceSessionID: "surface"})
		if len(sideCommands(events)) != 0 || surface.SelectedThreadID != "parent" || surface.SideSession == nil {
			t.Fatalf("switch escaped gate: %s", kind)
		}
	}
}

func TestSideStartupQuitFailureTimeoutAndDisconnect(t *testing.T) {
	for _, scenario := range []string{"quit", "failed", "timeout", "disconnect", "rejected"} {
		t.Run(scenario, func(t *testing.T) {
			s, surface, now := sideTestService(t)
			sideAction(s, "/side pending")
			id, command := surface.SideSession.SessionID, surface.SideSession.ActiveCommandID
			switch scenario {
			case "quit":
				sideAction(s, "/side quit")
			case "failed":
				s.ApplyAgentEvent("inst", agentproto.Event{Kind: agentproto.EventSideSessionUpdated, SideSessionID: id, Status: "failed", ErrorMessage: "fork failed"})
			case "timeout":
				*now = now.Add(time.Minute)
				s.Tick(*now)
			case "disconnect":
				s.ApplyInstanceDisconnected("inst")
			case "rejected":
				s.HandleCommandRejected("inst", agentproto.CommandAck{CommandID: command, Error: "unsupported"})
			}
			if surface.SideSession != nil || (scenario != "disconnect" && surface.SelectedThreadID != "parent") || len(surface.QueueItems) != 0 {
				t.Fatal("startup cleanup lost parent or replayed side input")
			}
			if events := s.ApplyAgentEvent("inst", agentproto.Event{Kind: agentproto.EventSideSessionUpdated, SideSessionID: id, Status: "open", ThreadID: "late-fork"}); len(events) != 0 {
				t.Fatal("late startup reactivated side")
			}
		})
	}
}

func TestSideUnsupportedAndStaleQuit(t *testing.T) {
	s, surface, _ := sideTestService(t)
	s.root.Instances["inst"].Capabilities.SideSession = false
	events := sideAction(s, "/side private question")
	if len(sideCommands(events)) != 0 || surface.SideSession != nil || events[0].Notice.Code != "side_unsupported" {
		t.Fatal("unsupported wrapper received ordinary /side text")
	}
	s.root.Instances["inst"].Capabilities.SideSession = true
	id := openTestSide(t, s, surface, "")
	events = s.ApplySurfaceAction(control.Action{Kind: control.ActionSideCommand, Text: "/side quit", SideSessionID: "old", SurfaceSessionID: "surface"})
	if len(sideCommands(events)) != 0 || surface.SideSession.SessionID != id {
		t.Fatal("stale quit closed newer session")
	}
	if !strings.Contains(events[0].Notice.Text, "ended") {
		t.Fatal("missing stale feedback")
	}
}

func TestSideLateEventsCannotConsumeNextTurn(t *testing.T) {
	s, surface, _ := sideTestService(t)
	id := openTestSide(t, s, surface, "first")
	first := surface.SideSession.ActiveCommandID
	s.ApplyAgentEvent("inst", agentproto.Event{Kind: agentproto.EventTurnStarted, SideSessionID: id, CommandID: first, ThreadID: "fork", TurnID: "one"})
	sideAction(s, "/side second")
	s.ApplyAgentEvent("inst", agentproto.Event{Kind: agentproto.EventTurnCompleted, SideSessionID: id, ThreadID: "fork", TurnID: "one", Status: "completed"})
	second := surface.SideSession.ActiveCommandID
	for _, event := range []agentproto.Event{
		{Kind: agentproto.EventTurnStarted, TurnID: "one"},
		{Kind: agentproto.EventTurnCompleted, TurnID: "one", Status: "completed"},
		{Kind: agentproto.EventRequestStarted, TurnID: "one", RequestID: "late-request"},
		{Kind: agentproto.EventSideSessionUpdated, ThreadID: "other", Status: "open", CommandID: first},
	} {
		event.SideSessionID = id
		if event.ThreadID == "" {
			event.ThreadID = "fork"
		}
		if events := s.ApplyAgentEvent("inst", event); len(events) != 0 {
			t.Fatalf("late event produced output: %#v", event)
		}
	}
	if surface.SideSession.ActiveCommandID != second || surface.SideSession.ActiveTurnID != "" || surface.SideSession.ThreadID != "fork" || len(surface.PendingRequests) != 0 {
		t.Fatal("late event consumed new turn")
	}
	s.ApplyAgentEvent("inst", agentproto.Event{Kind: agentproto.EventTurnStarted, SideSessionID: id, ThreadID: "fork", TurnID: "two", CommandID: second})
	for _, item := range []struct{ id, text string }{{"z", "first"}, {"a", "second"}} {
		s.ApplyAgentEvent("inst", agentproto.Event{Kind: agentproto.EventItemCompleted, SideSessionID: id, ThreadID: "fork", TurnID: "two", ItemID: item.id, ItemKind: "agent_message", Metadata: map[string]any{"text": item.text}})
	}
	events := s.ApplyAgentEvent("inst", agentproto.Event{Kind: agentproto.EventTurnCompleted, SideSessionID: id, ThreadID: "fork", TurnID: "two", Status: "completed"})
	if len(events) != 1 || events[0].Notice.Text != "first\nsecond" || events[0].Notice.Code != "side_session_reply" {
		t.Fatal("side response text missing or out of order")
	}
}

func TestSideDetachAndTransportFailureKeepExplicitExecutionTarget(t *testing.T) {
	s, surface, _ := sideTestService(t)
	id := openTestSide(t, s, surface, "")
	events := s.ApplySurfaceAction(control.Action{Kind: control.ActionDetach, SurfaceSessionID: "surface"})
	commands := sideCommands(events)
	if len(commands) < 1 || commands[0].Kind != agentproto.CommandSideSessionClose || commands[0].Target.InstanceID != "inst" || commands[0].SideSessionID != id {
		t.Fatal("detach lost side cleanup destination")
	}
	if surface.SideSession != nil {
		t.Fatal("detach retained overlay")
	}
	// A cleanup transport error after the overlay is gone must not fail main work.
	surface.ActiveQueueItemID = "main"
	surface.QueueItems["main"] = &state.QueueItemRecord{ID: "main", Status: state.QueueItemDispatching}
	s.HandleSideCommandDispatchFailure("surface", id, commands[0].CommandID, errors.New("offline"))
	if surface.QueueItems["main"].Status != state.QueueItemDispatching {
		t.Fatal("cleanup failure failed parent queue")
	}
}

func TestSideRestartNoticeIsOnceAndParentRouteRestored(t *testing.T) {
	s, surface, _ := sideTestService(t)
	surface.SideSessionEnded = true
	events := s.ApplySurfaceAction(control.Action{Kind: control.ActionStatus, SurfaceSessionID: "surface"})
	if len(events) < 2 || events[0].Notice == nil || events[0].Notice.Code != "side_session_ended" || surface.SideSessionEnded {
		t.Fatal("missing restart notice")
	}
	if surface.SideSession != nil || surface.SelectedThreadID != "parent" {
		t.Fatal("restart restored side routing")
	}
	for _, event := range s.ApplySurfaceAction(control.Action{Kind: control.ActionStatus, SurfaceSessionID: "surface"}) {
		if event.Notice != nil && event.Notice.Code == "side_session_ended" {
			t.Fatal("restart notice repeated")
		}
	}
}

func TestSideQuitRestoresParentPromptRouting(t *testing.T) {
	s, surface, _ := sideTestService(t)
	openTestSide(t, s, surface, "")
	sideAction(s, "/side quit")
	commands := sideCommands(s.ApplySurfaceAction(control.Action{Kind: control.ActionTextMessage, SurfaceSessionID: "surface", Text: "parent followup", MessageID: "new-parent-message"}))
	if len(commands) != 1 || commands[0].Kind != agentproto.CommandPromptSend || commands[0].SideSessionID != "" || commands[0].Target.ThreadID != "parent" {
		t.Fatalf("parent routing not restored: %#v", commands)
	}
}

func TestSideFreezesFollowSelectionAndPreventsContractRestart(t *testing.T) {
	s, surface, _ := sideTestService(t)
	surface.RouteMode = state.RouteModeFollowLocal
	openTestSide(t, s, surface, "")
	inst := s.root.Instances["inst"]
	inst.Threads["other"] = &state.ThreadRecord{ThreadID: "other", Loaded: true}
	inst.ObservedFocusedThreadID = "other"
	if events := s.reevaluateFollowSurface(surface); len(events) != 0 || surface.SelectedThreadID != "parent" {
		t.Fatal("side follow target changed")
	}
	if events := s.reconcileHeadlessSurfaceContractWithOptions(surface, false); len(events) != 0 {
		t.Fatal("side triggered parent restart")
	}
}

func TestSideMainCompletionDispatchesItsOwnQueue(t *testing.T) {
	s, surface, _ := sideTestService(t)
	mainCommands := sideCommands(s.ApplySurfaceAction(control.Action{Kind: control.ActionTextMessage, SurfaceSessionID: "surface", Text: "main first", MessageID: "main1"}))
	if len(mainCommands) != 1 {
		t.Fatal("main did not start")
	}
	s.BindPendingRemoteCommand("surface", mainCommands[0].CommandID)
	s.ApplyAgentEvent("inst", agentproto.Event{Kind: agentproto.EventTurnStarted, ThreadID: "parent", TurnID: "main-turn", CommandID: mainCommands[0].CommandID, Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: "surface"}})
	s.ApplySurfaceAction(control.Action{Kind: control.ActionTextMessage, SurfaceSessionID: "surface", Text: "main second", MessageID: "main2"})
	id := openTestSide(t, s, surface, "side first")
	activeSide := surface.SideSession.ActiveCommandID
	commands := sideCommands(s.ApplyAgentEvent("inst", agentproto.Event{Kind: agentproto.EventTurnCompleted, ThreadID: "parent", TurnID: "main-turn", Status: "completed"}))
	if len(commands) != 1 || commands[0].Kind != agentproto.CommandPromptSend || commands[0].Target.ThreadID != "parent" || commands[0].SideSessionID != "" {
		t.Fatalf("main completion did not dispatch main queue: %#v", commands)
	}
	if surface.SideSession.SessionID != id || surface.SideSession.ActiveCommandID != activeSide {
		t.Fatal("main completion consumed side queue")
	}
}
