package orchestrator

import (
	"errors"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestSideAndMainApprovalLanesRemainIndependent(t *testing.T) {
	for _, sideFirst := range []bool{false, true} {
		s, surface, now := sideTestService(t)
		s.bindThreadClaim(surface, "inst", "parent")
		sideID := openTestSide(t, s, surface, "question")
		s.ApplyAgentEvent("inst", agentproto.Event{Kind: agentproto.EventTurnStarted, SideSessionID: sideID, CommandID: surface.SideSession.ActiveCommandID, ThreadID: "fork", TurnID: "side-turn"})
		requests := []agentproto.Event{
			{Kind: agentproto.EventRequestStarted, RequestID: "main-request", ThreadID: "parent", TurnID: "main-turn"},
			{Kind: agentproto.EventRequestStarted, RequestID: "side-request", SideSessionID: sideID, ThreadID: "fork", TurnID: "side-turn"},
		}
		if sideFirst {
			requests[0], requests[1] = requests[1], requests[0]
		}
		for _, request := range requests {
			request.Initiator = agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: "surface"}
			request.Metadata = map[string]any{"requestType": "approval", "title": "Confirm", "body": "Execute tool"}
			events := s.ApplyAgentEvent("inst", request)
			if len(events) != 1 || !pendingRequestIsActive(surface, request.RequestID) {
				t.Fatalf("approval did not render independently: sideFirst=%v request=%s events=%#v", sideFirst, request.RequestID, events)
			}
			s.RecordRequestPromptDelivery(RequestDeliveryReport{SurfaceSessionID: "surface", RequestID: request.RequestID, MessageID: request.RequestID + "-card", DeliveredAt: *now})
			if surface.PendingRequests[request.RequestID].VisibleMessageID == "" {
				t.Fatal("independent lane delivery was ignored")
			}
		}
		for _, request := range requests {
			events := s.ApplySurfaceAction(control.Action{Kind: control.ActionRespondRequest, SurfaceSessionID: "surface", Request: &control.ActionRequestResponse{RequestID: request.RequestID, RequestOptionID: "accept"}})
			commands := sideCommands(events)
			if len(commands) != 1 || commands[0].SideSessionID != request.SideSessionID || commands[0].Target.ThreadID != request.ThreadID || commands[0].Target.TurnID != request.TurnID {
				t.Fatalf("approval retargeted: %#v events=%#v", commands, events)
			}
		}
		// A failed delivery in either lane remains eligible for recovery even
		// while the other lane already has a visible approval.
		s.RecordRequestPromptDeliveryFailure("surface", "side-request", errors.New("delivery failed"))
		if request := s.activePendingRequestNeedingVisibility(surface); request == nil || request.RequestID != "side-request" {
			t.Fatalf("side visibility recovery blocked: %#v", request)
		}
		sideAction(s, "/side quit")
		if surface.PendingRequests["side-request"] != nil || surface.PendingRequests["main-request"] == nil {
			t.Fatal("quit removed main approval or retained side approval")
		}
		_, events := resolveSurfacePendingRequest(surface, "side-request", 0)
		if len(events) == 0 {
			t.Fatal("stale side card remained actionable")
		}
	}
}

func TestSideApprovalResolutionPromotesOnlyItsOwnLane(t *testing.T) {
	s, surface, _ := sideTestService(t)
	id := openTestSide(t, s, surface, "")
	for _, request := range []*state.RequestPromptRecord{
		{RequestID: "main", RequestType: "approval"},
		{RequestID: "side-one", SideSessionID: id, RequestType: "approval"},
		{RequestID: "side-two", SideSessionID: id, RequestType: "approval"},
	} {
		enqueuePendingRequest(surface, request)
	}
	if pendingRequestIsActive(surface, "side-two") {
		t.Fatal("second approval skipped lane FIFO")
	}
	s.resolvePendingRequestOnSurface(surface, surface.PendingRequests["side-one"], agentproto.Event{})
	if !pendingRequestIsActive(surface, "main") || !pendingRequestIsActive(surface, "side-two") {
		t.Fatal("resolution did not preserve independent FIFO lanes")
	}
}

func TestSidePlanResolutionDoesNotChangeParentSettings(t *testing.T) {
	s, surface, _ := sideTestService(t)
	setSurfacePlanModeOverride(surface, state.PlanModeSettingOn)
	request := &state.RequestPromptRecord{SideSessionID: "side", Backend: agentproto.BackendClaude, SemanticKind: control.RequestSemanticPlanConfirmation}
	events := s.handleResolvedRequestPrompt(surface, request, agentproto.Event{Metadata: map[string]any{"decision": "accept"}})
	if len(events) != 0 || !surface.PlanModeOverrideSet || surface.PlanMode != state.PlanModeSettingOn {
		t.Fatalf("side plan mutated parent settings: %#v", events)
	}
}

func TestSideUserInputCancellationTargetsSideSession(t *testing.T) {
	s, surface, _ := sideTestService(t)
	request := &state.RequestPromptRecord{SideSessionID: "side", RequestID: "request", ThreadID: "fork", TurnID: "side-turn"}
	events := s.cancelRequestUserInputTurn(surface, request, control.Action{})
	var command *agentproto.Command
	for _, event := range events {
		if event.Kind == eventcontract.KindAgentCommand {
			command = event.Command
		}
	}
	if command == nil || command.SideSessionID != "side" || command.Target.ThreadID != "fork" || command.Target.TurnID != "side-turn" {
		t.Fatalf("cancel lost side target: %#v", command)
	}
}
