package orchestrator

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/claude"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
)

func TestClaudeBackgroundContinuationReachesSurfaceBeforeQueueDispatch(t *testing.T) {
	now := time.Date(2026, 11, 9, 1, 28, 58, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupQueuedStartNoticeSurface(t, svc)
	surface.Backend = agentproto.BackendClaude
	svc.root.Instances["inst-1"].Backend = agentproto.BackendClaude
	translator := claude.NewTranslator("inst-1")
	var allProjected []eventcontract.Event
	observe := func(payload map[string]any) []eventcontract.Event {
		t.Helper()
		line, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		translated, err := translator.ObserveServer(line)
		if err != nil {
			t.Fatal(err)
		}
		var projected []eventcontract.Event
		for _, event := range translated.Events {
			projected = append(projected, svc.ApplyAgentEvent("inst-1", event)...)
		}
		allProjected = append(allProjected, projected...)
		return projected
	}
	observe(map[string]any{"type": "system", "subtype": "init", "session_id": "thread-1", "permissionMode": "default"})
	action := func(messageID, text string) []eventcontract.Event {
		return svc.ApplySurfaceAction(control.Action{
			Kind: control.ActionTextMessage, SurfaceSessionID: surface.SurfaceSessionID,
			ChatID: surface.ChatID, ActorUserID: surface.ActorUserID, MessageID: messageID, Text: text,
		})
	}
	first := action("msg-1", "Run and review the research")
	command := findPromptSendCommand(first)
	if command == nil {
		t.Fatalf("first request did not dispatch: %#v", first)
	}
	if _, err := translator.TranslateCommand(*command); err != nil {
		t.Fatal(err)
	}
	observe(map[string]any{"type": "stream_event", "event": map[string]any{
		"type": "message_start", "message": map[string]any{"id": "msg-first", "content": []any{}},
	}})
	activeQueueID := surface.ActiveQueueItemID
	if activeQueueID == "" {
		t.Fatal("first request has no active queue item")
	}
	second := action("msg-2", "Run the next request")
	if findPromptSendCommand(second) != nil || len(surface.QueuedQueueItemIDs) != 1 {
		t.Fatalf("second request did not wait behind first: events=%#v queued=%#v", second, surface.QueuedQueueItemIDs)
	}
	observe(map[string]any{"type": "assistant", "parent_tool_use_id": nil,
		"message": map[string]any{"id": "msg-tool", "content": []any{
			map[string]any{"type": "tool_use", "id": "tool-review", "name": "Task", "input": map[string]any{
				"run_in_background": true, "description": "Independent review", "prompt": "Review the results",
			}},
		}},
	})
	observe(map[string]any{"type": "system", "subtype": "task_started", "session_id": "thread-1",
		"task_id": "review", "run_id": "run-review", "tool_use_id": "tool-review", "is_backgrounded": true,
	})
	yield := observe(map[string]any{"type": "result", "subtype": "success", "session_id": "thread-1",
		"result": "Both remaining items depend on the review arriving.", "terminal_reason": "completed",
	})
	if findPromptSendCommand(yield) != nil || surface.ActiveQueueItemID != activeQueueID || len(surface.QueuedQueueItemIDs) != 1 {
		t.Fatalf("intermediate result prematurely advanced queue: events=%#v active=%q queued=%#v", yield, surface.ActiveQueueItemID, surface.QueuedQueueItemIDs)
	}
	notification := observe(map[string]any{"type": "system", "subtype": "task_notification", "session_id": "thread-1",
		"task_id": "review", "run_id": "run-review", "tool_use_id": "tool-review", "status": "completed",
	})
	if findPromptSendCommand(notification) != nil || surface.ActiveQueueItemID != activeQueueID {
		t.Fatal("notification advanced queue before the report")
	}
	observe(map[string]any{"type": "stream_event", "parent_tool_use_id": nil, "event": map[string]any{
		"type": "message_start", "message": map[string]any{"id": "msg-report", "content": []any{}},
	}})
	const report = "All three directions reviewed and archived. Four decisions require your input."
	observe(map[string]any{"type": "assistant", "parent_tool_use_id": nil,
		"message": map[string]any{"id": "msg-report", "content": []any{map[string]any{"type": "text", "text": report}}},
	})
	finished := observe(map[string]any{"type": "result", "subtype": "success", "session_id": "thread-1", "result": report,
		"origin": map[string]any{"kind": "task-notification", "producer": "session-task", "runId": "run-review"},
	})
	if next := findPromptSendCommand(finished); next == nil || next.Origin.MessageID != "msg-2" {
		t.Fatalf("final report did not release queued next request: %#v", finished)
	}
	reports := 0
	for _, event := range allProjected {
		if event.Kind == eventcontract.KindBlockCommitted && event.Block != nil && event.Block.Text == report {
			reports++
			if !event.Block.Final || event.SurfaceSessionID != surface.SurfaceSessionID {
				t.Fatalf("report lacks final destination: %#v", event)
			}
		}
	}
	if reports != 1 {
		t.Fatalf("Lark-bound report blocks=%d, want exactly one", reports)
	}
}
