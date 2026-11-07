package orchestrator

import (
	"fmt"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func (s *Service) routeSideAction(surface *state.SurfaceConsoleRecord, action control.Action) ([]eventcontract.Event, bool) {
	if action.Kind == control.ActionSideQuit {
		action.Kind, action.Text = control.ActionSideCommand, "/side quit"
	}
	if action.Kind == control.ActionSideCommand {
		if surface.SideSession == nil && strings.TrimSpace(action.Text) != "/side quit" {
			if blocked := s.blockActionForActivePathPicker(surface, action); blocked != nil {
				return blocked, true
			}
			if blocked := s.blockActionForActiveTargetPicker(surface, action); blocked != nil {
				return blocked, true
			}
		}
		return s.handleSideCommand(surface, action), true
	}
	if surface.SideSession == nil {
		return nil, false
	}
	switch action.Kind {
	case control.ActionDetach, control.ActionWorkspaceDetach:
		events := s.closeSideSession(surface, "Side session ended.")
		return append(events, s.ApplySurfaceAction(action)...), true
	case control.ActionTextMessage:
		if surface.ActiveRequestCapture != nil {
			return nil, false
		}
		inputs := append([]agentproto.Input{}, action.Inputs...)
		if len(inputs) == 0 && strings.TrimSpace(action.Text) != "" {
			inputs = append(inputs, agentproto.Input{Type: agentproto.InputText, Text: action.Text})
		}
		if len(inputs) == 0 {
			return nil, true
		}
		return s.enqueueSideMessage(surface, action, inputs), true
	case control.ActionImageMessage:
		return s.enqueueSideMessage(surface, action, []agentproto.Input{{Type: agentproto.InputLocalImage, Path: action.LocalPath, MIMEType: action.MIMEType}}), true
	case control.ActionFileMessage:
		return s.enqueueSideMessage(surface, action, []agentproto.Input{{Type: agentproto.InputText, Text: fmt.Sprintf("Attached file %q at %q", action.FileName, action.LocalPath)}}), true
	case control.ActionStop:
		side := surface.SideSession
		side.Queue = nil
		if side.Status == "opening" || (side.ActiveCommandID != "" && side.ActiveTurnID == "") {
			return s.closeSideSession(surface, "Side startup cancelled. Input returns to the original conversation."), true
		}
		if side.ActiveTurnID == "" {
			return sideNotice(surface, "Side conversation is idle."), true
		}
		event := s.sideCommand(surface, side, agentproto.CommandTurnInterrupt)
		return append([]eventcontract.Event{event}, sideNotice(surface, "Stopping side work. The original task continues.")...), true
	case control.ActionStatus, control.ActionShowCommandHelp, control.ActionShowCommandMenu, control.ActionShowHistory,
		control.ActionRespondRequest, control.ActionControlRequest, control.ActionReactionCreated, control.ActionMessageRecalled:
		return nil, false
	default:
		return sideNotice(surface, "Use /side quit before switching conversation, backend, workspace, or starting another operation."), true
	}
}

func sideNotice(surface *state.SurfaceConsoleRecord, text string) []eventcontract.Event {
	id := ""
	if surface.SideSession != nil {
		id = surface.SideSession.SessionID
	}
	return []eventcontract.Event{{Kind: eventcontract.KindNotice, SurfaceSessionID: surface.SurfaceSessionID,
		Notice: &control.Notice{Code: "side_session", Title: "Side mode", Text: text, SideSessionID: id}}}
}

func (s *Service) sideCommand(surface *state.SurfaceConsoleRecord, side *state.SideSessionRecord, kind agentproto.CommandKind) eventcontract.Event {
	return eventcontract.Event{Kind: eventcontract.KindAgentCommand, SurfaceSessionID: surface.SurfaceSessionID,
		Command: &agentproto.Command{CommandID: s.nextRequestDispatchCommandID(), SideSessionID: side.SessionID,
			Kind: kind, IssuedAt: s.now(), Origin: agentproto.Origin{Surface: surface.SurfaceSessionID, UserID: surface.ActorUserID, ChatID: surface.ChatID},
			Target: agentproto.Target{InstanceID: side.InstanceID, ThreadID: side.ThreadID, TurnID: side.ActiveTurnID, CWD: side.CWD}}}
}

func (s *Service) handleSideCommand(surface *state.SurfaceConsoleRecord, action control.Action) []eventcontract.Event {
	text := strings.TrimSpace(action.Text)
	argument := ""
	if len(text) >= len("/side") {
		argument = strings.TrimSpace(text[len("/side"):])
	}
	if action.SideSessionID != "" && (surface.SideSession == nil || surface.SideSession.SessionID != action.SideSessionID) {
		return notice(surface, "side_expired", "This side session has ended. Use /side to start another.")
	}
	if argument == "quit" {
		return s.closeSideSession(surface, "Side session ended. Input returns to the original conversation.")
	}
	var events []eventcontract.Event
	if surface.SideSession == nil {
		inst := s.root.Instances[surface.AttachedInstanceID]
		if inst == nil || !inst.Online || surface.SelectedThreadID == "" {
			return notice(surface, "side_requires_thread", "Select a connected conversation before using /side.")
		}
		if !inst.Capabilities.SideSession || (inst.Backend != agentproto.BackendCodex && inst.Backend != agentproto.BackendClaude) {
			return notice(surface, "side_unsupported", "This wrapper does not support side sessions. Update it before using /side.")
		}
		if surface.Abandoning || surface.ReviewSession != nil || surface.ActiveRequestCapture != nil || surface.PendingHeadless != nil || surface.PendingTextInput != nil || surface.RouteMode == state.RouteModeNewThreadReady {
			return notice(surface, "side_overlay_busy", "Finish the current exclusive operation before entering side mode.")
		}
		room := s.ensureFeishuRoomContextForSurface(surface)
		if !s.feishuRoomActiveSlotAvailableForSurface(room, surface) {
			return notice(surface, "room_workspace_active", "Another surface is using this room's execution slot.")
		}
		cwd := inst.WorkspaceRoot
		if thread := inst.Threads[surface.SelectedThreadID]; thread != nil && thread.CWD != "" {
			cwd = thread.CWD
		}
		side := &state.SideSessionRecord{SessionID: fmt.Sprintf("side-%d-%s", s.now().UnixNano(), s.nextRequestDispatchCommandID()),
			InstanceID: inst.InstanceID, ParentThreadID: surface.SelectedThreadID, CWD: cwd, Status: "opening", OpenedAt: s.now(), Items: map[string]string{}}
		surface.SideSession = side
		open := s.sideCommand(surface, side, agentproto.CommandSideSessionOpen)
		open.Command.Target.ThreadID = side.ParentThreadID
		side.ActiveCommandID = open.Command.CommandID
		events = append(events, open)
		events = append(events, sideNotice(surface, "Opening a separate conversation. The original task continues. Use /side quit to return.")...)
	}
	if argument == "" {
		if len(events) == 0 {
			return sideNotice(surface, "Side mode is active. Messages stay in this side conversation. Use /side quit to return.")
		}
		return events
	}
	return append(events, s.enqueueSideMessage(surface, action, []agentproto.Input{{Type: agentproto.InputText, Text: argument}})...)
}

func (s *Service) enqueueSideMessage(surface *state.SurfaceConsoleRecord, action control.Action, inputs []agentproto.Input) []eventcontract.Event {
	side := surface.SideSession
	side.Queue = append(side.Queue, state.SideMessage{CommandID: s.nextRequestDispatchCommandID(), MessageID: action.MessageID, Inputs: inputs})
	return s.dispatchSideMessage(surface)
}

func (s *Service) dispatchSideMessage(surface *state.SurfaceConsoleRecord) []eventcontract.Event {
	side := surface.SideSession
	if side == nil || side.Status != "open" || side.ActiveCommandID != "" || len(side.Queue) == 0 {
		return nil
	}
	if !s.feishuRoomActiveSlotAvailableForSurface(s.ensureFeishuRoomContextForSurface(surface), surface) {
		return nil
	}
	message := side.Queue[0]
	side.Queue = side.Queue[1:]
	event := s.sideCommand(surface, side, agentproto.CommandPromptSend)
	event.Command.CommandID = message.CommandID
	event.Command.Origin.MessageID = message.MessageID
	event.Command.Prompt.Inputs = message.Inputs
	side.ActiveCommandID = message.CommandID
	side.OpenedAt = s.now()
	side.Items = map[string]string{}
	side.ItemOrder = nil
	return []eventcontract.Event{event}
}

func (s *Service) closeSideSession(surface *state.SurfaceConsoleRecord, text string) []eventcontract.Event {
	side := surface.SideSession
	if side == nil {
		return sideNotice(surface, "Side mode is already closed.")
	}
	event := s.sideCommand(surface, side, agentproto.CommandSideSessionClose)
	var events []eventcontract.Event
	// Abort only this session's requests; parent approval cards retain their IDs.
	for id, request := range surface.PendingRequests {
		if request.SideSessionID == side.SessionID {
			removePendingRequest(surface, id)
			clearSurfaceRequestCaptureByRequestID(surface, id)
		}
	}
	surface.SideSession = nil
	events = append(events, event)
	return append(events, sideNotice(surface, text)...)
}

// Tagged events are consumed even after quit. Late fork/turn events must never
// acquire main execution ownership or change the selected parent thread.
func (s *Service) applySideEvent(instanceID string, event agentproto.Event) []eventcontract.Event {
	var surface *state.SurfaceConsoleRecord
	for _, candidate := range s.root.Surfaces {
		if side := candidate.SideSession; side != nil && side.InstanceID == instanceID && side.SessionID == event.SideSessionID {
			surface = candidate
			break
		}
	}
	if surface == nil {
		return nil
	}
	side := surface.SideSession
	switch event.Kind {
	case agentproto.EventSideSessionUpdated:
		switch event.Status {
		case "open":
			if side.Status != "opening" || event.ThreadID == "" || event.CommandID != side.ActiveCommandID {
				return nil
			}
			side.ThreadID, side.Status, side.ActiveCommandID = event.ThreadID, "open", ""
			return append(sideNotice(surface, "Side mode is ready. Use /side quit to return."), s.dispatchSideMessage(surface)...)
		case "failed", "closed":
			return s.closeSideSession(surface, "Side session ended. "+event.ErrorMessage+" Input returns to the original conversation.")
		}
	case agentproto.EventTurnStarted:
		if side.ActiveCommandID == "" || event.CommandID != side.ActiveCommandID || event.ThreadID != side.ThreadID || (side.ActiveTurnID != "" && side.ActiveTurnID != event.TurnID) {
			return nil
		}
		side.ActiveTurnID = event.TurnID
	case agentproto.EventItemDelta:
		if event.TurnID == side.ActiveTurnID && event.ItemKind == "agent_message" {
			if _, ok := side.Items[event.ItemID]; !ok {
				side.ItemOrder = append(side.ItemOrder, event.ItemID)
			}
			side.Items[event.ItemID] += event.Delta
		}
	case agentproto.EventItemCompleted:
		if event.TurnID == side.ActiveTurnID && event.ItemKind == "agent_message" {
			text, _ := event.Metadata["text"].(string)
			if text == "" {
				text = event.Preview
			}
			if text != "" {
				if _, ok := side.Items[event.ItemID]; !ok {
					side.ItemOrder = append(side.ItemOrder, event.ItemID)
				}
				side.Items[event.ItemID] = text
			}
		}
	case agentproto.EventRequestStarted:
		if side.ActiveCommandID == "" || side.ActiveTurnID == "" || event.ThreadID != side.ThreadID || event.TurnID != side.ActiveTurnID {
			return nil
		}
		event.Initiator = agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: surface.SurfaceSessionID}
		return s.presentRequestPrompt(instanceID, event)
	case agentproto.EventRequestResolved:
		return s.resolveRequestPrompt(instanceID, event)
	case agentproto.EventTurnCompleted:
		if side.ActiveCommandID == "" || event.ThreadID != side.ThreadID || (event.CommandID != "" && event.CommandID != side.ActiveCommandID) || (event.CommandID == "" && (side.ActiveTurnID == "" || event.TurnID != side.ActiveTurnID)) || (side.ActiveTurnID != "" && event.TurnID != side.ActiveTurnID) {
			return nil
		}
		text := ""
		for _, id := range side.ItemOrder {
			text += side.Items[id] + "\n"
		}
		if event.ErrorMessage != "" {
			text += event.ErrorMessage
		}
		if strings.TrimSpace(text) == "" {
			text = "Side turn " + event.Status + "."
		}
		side.ActiveCommandID, side.ActiveTurnID = "", ""
		events := sideNotice(surface, strings.TrimSpace(text))
		events[0].Notice.Code = "side_session_reply"
		return append(events, s.dispatchSideMessage(surface)...)
	case agentproto.EventSystemError:
		return s.closeSideSession(surface, "Side session failed. "+event.ErrorMessage)
	}
	return nil
}

// HandleSideCommandDispatchFailure never falls back to main execution state,
// including failures from cleanup commands whose overlay was already removed.
func (s *Service) HandleSideCommandDispatchFailure(surfaceID, sessionID, commandID string, err error) []eventcontract.Event {
	surface := s.root.Surfaces[surfaceID]
	if surface == nil || surface.SideSession == nil || surface.SideSession.SessionID != sessionID {
		return nil
	}
	if events := s.restorePendingRequestDispatch(surface, commandID, "dispatch_failed"); len(events) != 0 {
		return events
	}
	return s.closeSideSession(surface, "Side dispatch failed. Input returns to the original conversation.")
}

func (s *Service) tickSideSessions(now time.Time) []eventcontract.Event {
	var events []eventcontract.Event
	for _, surface := range s.root.Surfaces {
		side := surface.SideSession
		if side == nil {
			continue
		}
		if (side.Status == "opening" || (side.ActiveCommandID != "" && side.ActiveTurnID == "")) && now.Sub(side.OpenedAt) > s.config.HeadlessLaunchWait {
			events = append(events, s.closeSideSession(surface, "Side session startup timed out. Input returns to the original conversation.")...)
			continue
		}
		events = append(events, s.dispatchSideMessage(surface)...)
	}
	return events
}
