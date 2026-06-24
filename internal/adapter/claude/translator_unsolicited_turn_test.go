package claude

import (
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

// TestClaudeTranslatorSynthesizesUnsolicitedTurn covers the spontaneous turn a
// completed background task triggers (a <task-notification> wakes Claude with no
// daemon prompt behind it). The translator must synthesize a turn so the
// streamed assistant message still flows to the remote surface instead of being
// silently dropped for lack of a pending turn.
func TestClaudeTranslatorSynthesizesUnsolicitedTurn(t *testing.T) {
	tr := NewTranslator("inst-1")
	// Establish the session id without issuing any prompt command, mirroring an
	// already-initialized child that later wakes up on a background notification.
	observeClaude(t, tr, map[string]any{
		"type":           "system",
		"subtype":        "init",
		"session_id":     "session-bg-1",
		"cwd":            "/data/dl/droid",
		"model":          "mimo-v2.5-pro",
		"permissionMode": "default",
	})

	// The background task completion is injected as a replayed user message; it
	// must not, by itself, fabricate a turn.
	notice := observeClaude(t, tr, map[string]any{
		"type": "user",
		"message": map[string]any{
			"role": "user",
			"content": []any{
				map[string]any{"type": "text", "text": "<task-notification><task-id>buw7cj6i2</task-id></task-notification>"},
			},
		},
	})
	if len(notice.Events) != 0 {
		t.Fatalf("expected task-notification user frame to emit no events, got %#v", notice.Events)
	}
	if tr.activeTurn != nil || len(tr.pendingTurns) != 0 {
		t.Fatalf("expected no turn before message_start, active=%#v pending=%#v", tr.activeTurn, tr.pendingTurns)
	}

	started := observeClaude(t, tr, map[string]any{
		"type": "stream_event",
		"event": map[string]any{
			"type": "message_start",
			"message": map[string]any{
				"id":      "msg-bg-1",
				"type":    "message",
				"role":    "assistant",
				"model":   "mimo-v2.5-pro",
				"content": []any{},
			},
		},
	})
	if len(started.Events) != 1 || started.Events[0].Kind != agentproto.EventTurnStarted {
		t.Fatalf("expected synthesized turn.started event, got %#v", started.Events)
	}
	turnStarted := started.Events[0]
	if turnStarted.ThreadID != "session-bg-1" {
		t.Fatalf("expected unsolicited turn to use session thread id, got %#v", turnStarted)
	}
	if turnStarted.Initiator.Kind != agentproto.InitiatorUnknown {
		t.Fatalf("expected unsolicited turn initiator to be unknown, got %#v", turnStarted.Initiator)
	}
	turnID := turnStarted.TurnID
	if strings.TrimSpace(turnID) == "" {
		t.Fatalf("expected synthesized turn id, got empty")
	}

	observeClaude(t, tr, map[string]any{
		"type": "stream_event",
		"event": map[string]any{
			"type":  "content_block_start",
			"index": 0,
			"content_block": map[string]any{
				"type": "text",
			},
		},
	})
	delta := observeClaude(t, tr, map[string]any{
		"type": "stream_event",
		"event": map[string]any{
			"type":  "content_block_delta",
			"index": 0,
			"delta": map[string]any{
				"type": "text_delta",
				"text": "后台训练已完成。",
			},
		},
	})
	if len(delta.Events) != 1 || delta.Events[0].Kind != agentproto.EventItemDelta || delta.Events[0].Delta != "后台训练已完成。" {
		t.Fatalf("expected agent_message delta for unsolicited turn, got %#v", delta.Events)
	}
	if delta.Events[0].ThreadID != "session-bg-1" || delta.Events[0].TurnID != turnID {
		t.Fatalf("expected delta routed to synthesized turn, got %#v", delta.Events[0])
	}

	result := observeClaude(t, tr, map[string]any{
		"type":     "result",
		"subtype":  "success",
		"is_error": false,
		"result":   "后台训练已完成。",
	})
	last := result.Events[len(result.Events)-1]
	if last.Kind != agentproto.EventTurnCompleted || last.ThreadID != "session-bg-1" || last.TurnID != turnID {
		t.Fatalf("expected completion for synthesized turn, got %#v", last)
	}
	if tr.activeTurn != nil || len(tr.pendingTurns) != 0 {
		t.Fatalf("expected turn state cleared after completion, active=%#v pending=%#v", tr.activeTurn, tr.pendingTurns)
	}
}

// TestClaudeTranslatorDoesNotSynthesizeTurnBeforeSessionInit guards the gate:
// without a known session id (no init yet), a message_start must not fabricate a
// turn with an unroutable synthetic thread id.
func TestClaudeTranslatorDoesNotSynthesizeTurnBeforeSessionInit(t *testing.T) {
	tr := NewTranslator("inst-1")
	started := observeClaude(t, tr, map[string]any{
		"type": "stream_event",
		"event": map[string]any{
			"type": "message_start",
			"message": map[string]any{
				"id":      "msg-pre-init",
				"type":    "message",
				"role":    "assistant",
				"model":   "mimo-v2.5-pro",
				"content": []any{},
			},
		},
	})
	if len(started.Events) != 0 {
		t.Fatalf("expected no events before session init, got %#v", started.Events)
	}
	if tr.activeTurn != nil || len(tr.pendingTurns) != 0 {
		t.Fatalf("expected no synthesized turn before session init, active=%#v pending=%#v", tr.activeTurn, tr.pendingTurns)
	}
}
