package claude

import (
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/xutil"
	"strings"
)

func (t *Translator) observeResultMessage(message map[string]any) Result {
	events := t.startActiveTurnIfNeeded()
	if t.activeTurn == nil {
		return Result{Events: events}
	}
	if !t.activeTurn.AgentMessageCompleted {
		if text := strings.TrimSpace(xutil.LookupStringFromAny(message["result"])); text != "" {
			itemID := t.nextItemID()
			t.activeTurn.AgentMessageCompleted = true
			t.activeTurn.LastAssistantText = text
			events = append(events,
				agentproto.Event{
					Kind:      agentproto.EventItemStarted,
					CommandID: t.activeTurn.CommandID,
					ThreadID:  t.activeTurn.ThreadID,
					TurnID:    t.activeTurn.TurnID,
					ItemID:    itemID,
					ItemKind:  "agent_message",
				},
				agentproto.Event{
					Kind:      agentproto.EventItemCompleted,
					CommandID: t.activeTurn.CommandID,
					ThreadID:  t.activeTurn.ThreadID,
					TurnID:    t.activeTurn.TurnID,
					ItemID:    itemID,
					ItemKind:  "agent_message",
					Status:    "completed",
					Metadata:  map[string]any{"text": text},
				},
			)
		}
	}
	if usage := buildClaudeTokenUsage(message, t.threadUsage[t.activeTurn.ThreadID]); usage != nil {
		t.threadUsage[t.activeTurn.ThreadID] = agentproto.CloneThreadTokenUsage(usage)
		events = append(events, agentproto.Event{
			Kind:       agentproto.EventThreadTokenUsageUpdated,
			CommandID:  t.activeTurn.CommandID,
			ThreadID:   t.activeTurn.ThreadID,
			TurnID:     t.activeTurn.TurnID,
			TokenUsage: usage,
		})
	}
	status, errorMessage, problem := t.resultCompletion(message)
	// A native success can yield to background work rather than end the remote
	// turn. Keep ownership until the tasks notify and the root produces its next
	// result; completing here loses the ensuing report and releases queued input.
	if status == "completed" && len(t.activeTurn.BackgroundTasks) != 0 {
		t.currentMessage = nil
		t.activeTurn.AgentMessageCompleted = false
		t.activeTurn.FallbackAgentMessageUsed = false
		return Result{Events: events}
	}
	events = append(events, agentproto.Event{
		Kind:                 agentproto.EventTurnCompleted,
		CommandID:            t.activeTurn.CommandID,
		Initiator:            t.activeTurn.Initiator,
		ThreadID:             t.activeTurn.ThreadID,
		TurnID:               t.activeTurn.TurnID,
		TurnCompletionOrigin: agentproto.TurnCompletionOriginRuntime,
		Status:               status,
		ErrorMessage:         errorMessage,
		Problem:              problem,
	})
	completedTurn := t.activeTurn
	turnID := completedTurn.TurnID
	t.completedTurn = completedTurn
	t.activeTurn = nil
	t.currentMessage = nil
	for requestID, request := range t.pendingRequests {
		if request != nil && request.TurnID == turnID {
			delete(t.pendingRequests, requestID)
		}
	}
	for toolUseID, tool := range t.toolStates {
		if tool != nil && tool.TurnID == turnID {
			delete(t.toolStates, toolUseID)
		}
	}
	return Result{Events: events}
}

func (t *Translator) resultCompletion(message map[string]any) (string, string, *agentproto.ErrorInfo) {
	subtype := strings.TrimSpace(xutil.LookupStringFromAny(message["subtype"]))
	resultText := strings.TrimSpace(xutil.LookupStringFromAny(message["result"]))
	if t.activeTurn != nil && t.activeTurn.InterruptRequested && subtype == "error_during_execution" {
		return "interrupted", "", nil
	}
	if subtype == "success" {
		return "completed", "", nil
	}
	errorMessage := xutil.FirstNonEmpty(resultText, "Claude turn failed.")
	return "failed", errorMessage, buildFailureProblem(
		"claude_turn_failed",
		errorMessage,
		xutil.CompactJSON(message["errors"]),
		t.activeTurn.ThreadID,
		t.activeTurn.TurnID,
	)
}
