package codex

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/jsonrpcutil"
	"github.com/kxn/codex-remote-feishu/internal/xutil"
)

type codexSideSession struct {
	command      agentproto.Command
	threadID     string
	closed       bool
	turnTemplate map[string]any
}

type codexSideRequest struct {
	sessionID string
	commandID string
	action    string
}

// DisconnectSideSessions drops every temporary execution route while leaving
// native main threads alone. Pending forks retain closed tombstones so their
// eventual responses trigger disposal instead of opening a new side session.
func (t *Translator) DisconnectSideSessions() [][]byte {
	var frames [][]byte
	for id, session := range t.sideSessions {
		if session.closed {
			continue
		}
		if session.threadID == "" {
			session.closed = true
			continue
		}
		outbound, _ := t.translateSideCommand(agentproto.Command{Kind: agentproto.CommandSideSessionClose, SideSessionID: id})
		frames = append(frames, outbound...)
	}
	return frames
}

func (t *Translator) sideRPC(sessionID, commandID, action, method string, params map[string]any) []byte {
	id := t.NextRequest("side-" + action)
	t.sideRequests[id] = codexSideRequest{sessionID: sessionID, commandID: commandID, action: action}
	raw, _ := json.Marshal(map[string]any{"id": id, "method": method, "params": params})
	return append(raw, '\n')
}

func (t *Translator) translateSideCommand(command agentproto.Command) ([][]byte, error) {
	id := strings.TrimSpace(command.SideSessionID)
	if id == "" {
		return nil, fmt.Errorf("side session id required")
	}
	session := t.sideSessions[id]
	switch command.Kind {
	case agentproto.CommandSideSessionOpen:
		if session != nil {
			return nil, fmt.Errorf("side session already exists")
		}
		parent := strings.TrimSpace(command.Target.ThreadID)
		if parent == "" {
			return nil, fmt.Errorf("side session requires parent thread")
		}
		// Only an exact parent template may override the observed contract. The
		// normal prompt fallback can belong to another native thread.
		template := xutil.CloneMap(t.turnStartByThread[parent])
		if template == nil {
			template = map[string]any{}
		}
		// Keep this nonempty even without captured overrides, so directTurnStart
		// cannot later select the global most-recent turn template.
		template["threadId"] = parent
		if err := validateSideThreadConfig(t.observedThreadConfig[parent], template); err != nil {
			return nil, err
		}
		t.sideSessions[id] = &codexSideSession{command: command, turnTemplate: template}
		params := map[string]any{"threadId": parent, "ephemeral": true, "excludeTurns": true}
		// Exclude the unfinished turn; subsequent parent updates never enter the fork.
		if turn := t.activeTurnByThread[parent]; turn != "" {
			params["beforeTurnId"] = turn
		}
		applyCodexResumePolicyToThreadParams(params, command.CodexResume)
		inheritSideThreadConfig(params, t.observedThreadConfig[parent], t.sideSessions[id].turnTemplate)
		return [][]byte{t.sideRPC(id, command.CommandID, "open", "thread/fork", params)}, nil
	case agentproto.CommandSideSessionClose:
		if session != nil {
			session.closed = true
		}
		var frames [][]byte
		if session != nil && session.threadID != "" {
			if turn := t.activeTurnByThread[session.threadID]; turn != "" {
				frames = append(frames, t.sideRPC(id, command.CommandID, "interrupt", "turn/interrupt", map[string]any{"threadId": session.threadID, "turnId": turn}))
			}
			frames = append(frames, t.sideRPC(id, command.CommandID, "close", "thread/unsubscribe", map[string]any{"threadId": session.threadID}))
		} else {
			// A response barrier supplies a correlated outcome even during startup.
			frames = append(frames, t.sideRPC(id, command.CommandID, "close", "thread/loaded/list", map[string]any{}))
		}
		return frames, nil
	}
	if session == nil || session.closed || session.threadID == "" {
		return nil, fmt.Errorf("side session is not open")
	}
	if command.Target.ThreadID != "" && command.Target.ThreadID != session.threadID {
		return nil, fmt.Errorf("side session thread mismatch")
	}
	command.Target.ThreadID = session.threadID
	switch command.Kind {
	case agentproto.CommandPromptSend:
		frame, requestID, err := t.directTurnStart(session.threadID, command, false)
		if err == nil {
			t.sideRequests[requestID] = codexSideRequest{sessionID: id, commandID: command.CommandID, action: "prompt"}
		}
		return [][]byte{frame}, err
	case agentproto.CommandTurnInterrupt:
		turn := t.activeTurnByThread[session.threadID]
		if turn == "" {
			return nil, nil
		}
		return [][]byte{t.sideRPC(id, command.CommandID, "interrupt", "turn/interrupt", map[string]any{"threadId": session.threadID, "turnId": turn})}, nil
	case agentproto.CommandRequestRespond:
		if t.sideApprovalRequests[command.Request.RequestID] != id {
			return nil, fmt.Errorf("side approval request mismatch")
		}
		return t.translateRequestRespond(command)
	default:
		return nil, fmt.Errorf("unsupported side session command %s", command.Kind)
	}
}

func (t *Translator) ObserveServer(raw []byte) (Result, error) {
	var message map[string]any
	if err := json.Unmarshal(raw, &message); err != nil {
		return Result{}, err
	}
	if thread := lookupString(message, "result", "thread", "id"); thread != "" {
		config := lookupMap(message, "result")
		if config["approvalPolicy"] != nil && config["sandbox"] != nil {
			t.observedThreadConfig[thread] = xutil.CloneMap(config)
		}
	}
	if request, ok := t.sideRequests[fmt.Sprint(message["id"])]; ok {
		delete(t.sideRequests, fmt.Sprint(message["id"]))
		if request.action == "prompt" {
			delete(t.pendingSuppressedResponse, fmt.Sprint(message["id"]))
			return t.observeSidePromptResponse(request, message), nil
		}
		return t.observeSideResponse(request, message), nil
	}
	result, err := t.observeServer(raw)
	for i := range result.Events {
		event := &result.Events[i]
		if id := t.sideThreads[event.ThreadID]; id != "" {
			event.SideSessionID = id
			if session := t.sideSessions[id]; session != nil && session.closed && event.Kind == agentproto.EventTurnStarted {
				result.OutboundToCodex = append(result.OutboundToCodex,
					t.sideRPC(id, "", "interrupt", "turn/interrupt", map[string]any{"threadId": event.ThreadID, "turnId": event.TurnID}),
					t.sideRPC(id, "", "cleanup", "thread/unsubscribe", map[string]any{"threadId": event.ThreadID}))
			}
			if session := t.sideSessions[id]; session != nil && session.closed && event.Kind == agentproto.EventTurnCompleted {
				result.OutboundToCodex = append(result.OutboundToCodex, t.sideRPC(id, "", "cleanup", "thread/unsubscribe", map[string]any{"threadId": event.ThreadID}))
			}
			if event.Kind == agentproto.EventRequestStarted {
				t.sideApprovalRequests[event.RequestID] = id
			}
			result.Suppress = true
		}
		if id := t.sideApprovalRequests[event.RequestID]; id != "" {
			event.SideSessionID = id
			result.Suppress = true
			if event.Kind == agentproto.EventRequestResolved {
				delete(t.sideApprovalRequests, event.RequestID)
			}
		}
	}
	// Some side notifications intentionally produce no projected event.
	thread := choose(lookupString(message, "params", "threadId"), lookupString(message, "params", "thread", "id"))
	if t.sideThreads[thread] != "" {
		result.Suppress = true
	}
	return result, err
}

// The turn/start response is the authority for command -> turn identity. Native
// turn/started notifications carry no command ID and may precede this response.
func (t *Translator) observeSidePromptResponse(request codexSideRequest, message map[string]any) Result {
	result := Result{Suppress: true}
	session := t.sideSessions[request.sessionID]
	if session == nil {
		return result
	}
	turnID := lookupString(message, "result", "turn", "id")
	errMsg := jsonrpcutil.ExtractErrorMessage(message)
	if session.closed {
		if turnID != "" {
			result.OutboundToCodex = [][]byte{
				t.sideRPC(request.sessionID, request.commandID, "interrupt", "turn/interrupt", map[string]any{"threadId": session.threadID, "turnId": turnID}),
				t.sideRPC(request.sessionID, request.commandID, "cleanup", "thread/unsubscribe", map[string]any{"threadId": session.threadID}),
			}
		}
		return result
	}
	event := agentproto.Event{Kind: agentproto.EventTurnStarted, SideSessionID: request.sessionID,
		CommandID: request.commandID, ThreadID: session.threadID, TurnID: turnID, Status: "running"}
	if errMsg != "" || turnID == "" {
		delete(t.pendingRemoteTurnByThread, session.threadID)
		event.Kind, event.Status = agentproto.EventTurnCompleted, "failed"
		event.ErrorMessage = choose(errMsg, "Codex turn/start returned no turn id")
		event.TurnCompletionOrigin = agentproto.TurnCompletionOriginTurnStartRejected
	}
	result.Events = []agentproto.Event{event}
	return result
}

func (t *Translator) observeSideResponse(request codexSideRequest, message map[string]any) Result {
	result := Result{Suppress: true}
	if request.action == "interrupt" || request.action == "cleanup" {
		return result
	}
	session := t.sideSessions[request.sessionID]
	event := agentproto.Event{Kind: agentproto.EventSideSessionUpdated, SideSessionID: request.sessionID, CommandID: request.commandID}
	if request.action == "close" {
		event.Status = "closed"
		if session != nil {
			event.ThreadID = session.threadID
		}
		result.Events = []agentproto.Event{event}
		return result
	}
	event.ErrorMessage = jsonrpcutil.ExtractErrorMessage(message)
	event.ThreadID = choose(lookupString(message, "result", "thread", "id"), lookupString(message, "result", "id"))
	if event.ErrorMessage != "" || event.ThreadID == "" {
		event.Status = "failed"
		if event.ErrorMessage == "" {
			event.ErrorMessage = "Codex fork returned no thread id"
		}
		if session != nil {
			session.closed = true
		}
		result.Events = []agentproto.Event{event}
		return result
	}
	if session == nil {
		return result
	}
	session.threadID = event.ThreadID
	t.sideThreads[event.ThreadID] = request.sessionID
	if session.closed {
		result.OutboundToCodex = [][]byte{t.sideRPC(request.sessionID, request.commandID, "cleanup", "thread/unsubscribe", map[string]any{"threadId": event.ThreadID})}
		return result
	}
	parent := session.command.Target.ThreadID
	t.knownThreadCWD[event.ThreadID] = choose(session.command.Target.CWD, t.knownThreadCWD[parent])
	// Freeze the parent's turn configuration, rather than taking a later global template.
	t.turnStartByThread[event.ThreadID] = session.turnTemplate
	t.recordCodexPolicyForThread(event.ThreadID, session.command.CodexResume)
	event.Status = "open"
	result.Events = []agentproto.Event{event}
	return result
}
