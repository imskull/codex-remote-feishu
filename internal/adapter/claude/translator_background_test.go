package claude

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func claudeBackgroundTool(t *testing.T, tr *Translator, name, toolID, taskID, runID string) map[string]any {
	t.Helper()
	observeClaude(t, tr, map[string]any{
		"type": "assistant", "parent_tool_use_id": nil,
		"message": map[string]any{"id": "msg-" + toolID, "content": []any{
			map[string]any{"type": "tool_use", "id": toolID, "name": name,
				"input": map[string]any{"run_in_background": true, "description": "Review", "command": "review", "prompt": "Review"}},
		}},
	})
	return map[string]any{
		"type": "system", "subtype": "task_started", "session_id": "session-claude-1",
		"task_id": taskID, "run_id": runID, "tool_use_id": toolID, "is_backgrounded": true,
	}
}

func claudeBackgroundNotification(taskID, runID, toolID string) map[string]any {
	return map[string]any{
		"type": "system", "subtype": "task_notification", "session_id": "session-claude-1",
		"task_id": taskID, "run_id": runID, "tool_use_id": toolID, "status": "completed",
	}
}

func claudeBackgroundResult(text string) map[string]any {
	return map[string]any{
		"type": "result", "subtype": "success", "session_id": "session-claude-1",
		"is_error": false, "stop_reason": "end_turn", "terminal_reason": "completed", "result": text,
	}
}

func assertClaudeBackgroundYield(t *testing.T, tr *Translator, turnID string, result Result) {
	t.Helper()
	for _, event := range result.Events {
		if event.Kind == agentproto.EventTurnCompleted {
			t.Fatalf("background yield completed the owning turn: %#v", event)
		}
	}
	if tr.activeTurn == nil || tr.activeTurn.TurnID != turnID {
		t.Fatalf("background yield lost owner %q: %#v", turnID, tr.activeTurn)
	}
}

func TestClaudeTranslatorBackgroundReviewContinuation(t *testing.T) {
	tr := NewTranslator("inst-1")
	threadID, turnID := startClaudeTurn(t, tr, "default")
	observeClaude(t, tr, claudeBackgroundTool(t, tr, "Task", "tool-review", "review", "run-review"))
	observeClaude(t, tr, claudeBackgroundTool(t, tr, "Bash", "tool-wait", "wait", "run-wait"))
	// Native Bash launch already returns successfully while the task continues.
	observeClaude(t, tr, map[string]any{"type": "user", "message": map[string]any{"content": []any{
		map[string]any{"type": "tool_result", "tool_use_id": "tool-wait", "content": "Running in background", "is_error": false},
	}}})
	if _, err := tr.TranslateCommand(agentproto.Command{
		CommandID: "cmd-next", Kind: agentproto.CommandPromptSend,
		Target: agentproto.Target{ThreadID: threadID},
		Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "Next request"}}},
	}); err != nil {
		t.Fatal(err)
	}
	assertClaudeBackgroundYield(t, tr, turnID, observeClaude(t, tr, claudeBackgroundResult("Both remaining items depend on the review arriving.")))
	observeClaude(t, tr, claudeBackgroundNotification("wait", "run-wait", "tool-wait"))
	assertClaudeBackgroundYield(t, tr, turnID, observeClaude(t, tr, claudeBackgroundResult("Review still running.")))
	// An empty snapshot or task_updated alone must not discard explicit ownership.
	observeClaude(t, tr, map[string]any{"type": "system", "subtype": "background_tasks_changed", "tasks": []any{}})
	observeClaude(t, tr, map[string]any{"type": "system", "subtype": "task_updated", "task_id": "review", "run_id": "run-review", "patch": map[string]any{"status": "completed"}})
	assertClaudeBackgroundYield(t, tr, turnID, observeClaude(t, tr, claudeBackgroundResult("Waiting for notification.")))
	observeClaude(t, tr, claudeBackgroundNotification("review", "run-review", "tool-review"))
	if tr.activeTurn == nil {
		t.Fatal("task notification finalized before the consolidated report")
	}
	observeClaude(t, tr, map[string]any{"type": "stream_event", "parent_tool_use_id": nil, "event": map[string]any{
		"type": "message_start", "message": map[string]any{"id": "msg-report", "content": []any{}},
	}})
	const report = "All three directions reviewed and archived. Choose one of four decisions."
	assistant := observeClaude(t, tr, map[string]any{"type": "assistant", "parent_tool_use_id": nil,
		"message": map[string]any{"id": "msg-report", "content": []any{map[string]any{"type": "text", "text": report}}},
	})
	terminal := claudeBackgroundResult(report)
	terminal["origin"] = map[string]any{"kind": "task-notification", "producer": "session-task", "runId": "run-wait"}
	completed := observeClaude(t, tr, terminal)
	reports, completions := 0, 0
	for _, event := range append(assistant.Events, completed.Events...) {
		if event.Kind == agentproto.EventItemCompleted && event.ItemKind == "agent_message" && event.Metadata["text"] == report {
			reports++
			if event.TurnID != turnID || event.ThreadID != threadID || event.CommandID != "cmd-1" {
				t.Fatalf("report changed ownership: %#v", event)
			}
		}
		if event.Kind == agentproto.EventTurnCompleted {
			completions++
			if event.TurnID != turnID {
				t.Fatalf("completed wrong turn: %#v", event)
			}
		}
	}
	if reports != 1 || completions != 1 || tr.activeTurn != nil {
		t.Fatalf("want one report and completion, got reports=%d completions=%d active=%#v", reports, completions, tr.activeTurn)
	}
	if len(tr.pendingTurns) != 1 || tr.pendingTurns[0].CommandID != "cmd-next" {
		t.Fatalf("continuation consumed the next user command: %#v", tr.pendingTurns)
	}
}

func TestClaudeTranslatorBackgroundNotificationRequiresIdentity(t *testing.T) {
	for _, field := range []string{"session_id", "run_id", "task_id", "tool_use_id"} {
		t.Run(field, func(t *testing.T) {
			tr := NewTranslator("inst-1")
			_, turnID := startClaudeTurn(t, tr, "default")
			observeClaude(t, tr, claudeBackgroundTool(t, tr, "Bash", "tool-bg", "task-bg", "run-bg"))
			notification := claudeBackgroundNotification("task-bg", "run-bg", "tool-bg")
			notification[field] = "wrong"
			observeClaude(t, tr, notification)
			assertClaudeBackgroundYield(t, tr, turnID, observeClaude(t, tr, claudeBackgroundResult("Still waiting")))
			observeClaude(t, tr, claudeBackgroundNotification("task-bg", "run-bg", "tool-bg"))
			observeClaude(t, tr, claudeBackgroundResult("Done"))
			if tr.activeTurn != nil {
				t.Fatal("matching notification failed to release background ownership")
			}
		})
	}
}

func TestClaudeTranslatorBackgroundStartRequiresOwner(t *testing.T) {
	for _, field := range []string{"session_id", "tool_use_id", "is_backgrounded"} {
		t.Run(field, func(t *testing.T) {
			tr := NewTranslator("inst-1")
			startClaudeTurn(t, tr, "default")
			started := claudeBackgroundTool(t, tr, "Bash", "tool-bg", "task-bg", "run-bg")
			if field == "is_backgrounded" {
				started[field] = false
			} else {
				started[field] = "unknown"
			}
			observeClaude(t, tr, started)
			observeClaude(t, tr, claudeBackgroundResult("Ordinary foreground completion"))
			if tr.activeTurn != nil {
				t.Fatal("unowned or foreground task blocked completion")
			}
		})
	}
}

func TestClaudeTranslatorBackgroundErrorAndInterruptTerminate(t *testing.T) {
	for _, interrupt := range []bool{false, true} {
		name := "error"
		if interrupt {
			name = "interrupt"
		}
		t.Run(name, func(t *testing.T) {
			tr := NewTranslator("inst-1")
			threadID, turnID := startClaudeTurn(t, tr, "default")
			observeClaude(t, tr, claudeBackgroundTool(t, tr, "Task", "tool-bg", "task-bg", "run-bg"))
			result := claudeBackgroundResult("Stopped")
			result["subtype"] = "error_during_execution"
			if interrupt {
				if _, err := tr.TranslateCommand(agentproto.Command{Kind: agentproto.CommandTurnInterrupt, Target: agentproto.Target{ThreadID: threadID, TurnID: turnID}}); err != nil {
					t.Fatal(err)
				}
			} else {
				result["subtype"] = "error_during_execution"
				result["is_error"] = true
			}
			terminal := observeClaude(t, tr, result)
			found := false
			for _, event := range terminal.Events {
				found = found || event.Kind == agentproto.EventTurnCompleted
			}
			if !found || tr.activeTurn != nil {
				t.Fatalf("terminal result left background turn active: %#v", terminal.Events)
			}
		})
	}
}

func TestClaudeTranslatorBackgroundRestartClearsOwnership(t *testing.T) {
	tr := NewTranslator("inst-1")
	startClaudeTurn(t, tr, "default")
	observeClaude(t, tr, claudeBackgroundTool(t, tr, "Task", "tool-bg", "task-bg", "run-bg"))
	observeClaude(t, tr, claudeBackgroundResult("Waiting"))
	tr.PrepareForChildLaunch("session-claude-1")
	startClaudeTurn(t, tr, "default")
	observeClaude(t, tr, claudeBackgroundNotification("task-bg", "run-bg", "tool-bg"))
	observeClaude(t, tr, claudeBackgroundResult("Fresh foreground result"))
	if tr.activeTurn != nil {
		t.Fatal("restart retained old task ownership")
	}
}

func TestClaudeTranslatorBackgroundInterruptAckWaitsForNativeResult(t *testing.T) {
	for _, rejected := range []bool{false, true} {
		name := "accepted"
		if rejected {
			name = "rejected"
		}
		t.Run(name, func(t *testing.T) {
			tr := NewTranslator("inst-1")
			threadID, turnID := startClaudeTurn(t, tr, "default")
			observeClaude(t, tr, claudeBackgroundTool(t, tr, "Task", "tool-bg", "task-bg", "run-bg"))
			assertClaudeBackgroundYield(t, tr, turnID, observeClaude(t, tr, claudeBackgroundResult("Waiting")))
			if _, err := tr.TranslateCommand(agentproto.Command{
				CommandID: "cmd-next", Kind: agentproto.CommandPromptSend,
				Target: agentproto.Target{ThreadID: threadID},
				Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "Next request"}}},
			}); err != nil {
				t.Fatal(err)
			}
			frames, err := tr.TranslateCommand(agentproto.Command{
				CommandID: "cmd-stop", Kind: agentproto.CommandTurnInterrupt,
				Target: agentproto.Target{ThreadID: threadID, TurnID: turnID},
			})
			if err != nil || len(frames) != 1 {
				t.Fatalf("interrupt frames=%d err=%v", len(frames), err)
			}
			response := map[string]any{"request_id": decodeFrame(t, frames[0])["request_id"], "subtype": "success", "response": map[string]any{}}
			if rejected {
				response["subtype"] = "error"
				response["error"] = "Interrupt rejected"
			}
			ack := observeClaude(t, tr, map[string]any{"type": "control_response", "response": response})
			assertClaudeBackgroundYield(t, tr, turnID, ack)
			if len(tr.pendingTurns) != 1 || tr.pendingTurns[0].CommandID != "cmd-next" {
				t.Fatalf("interrupt ack consumed pending prompt: %#v", tr.pendingTurns)
			}
			terminal := claudeBackgroundResult("Stopped")
			wantStatus := "interrupted"
			if rejected {
				assertClaudeBackgroundYield(t, tr, turnID, observeClaude(t, tr, claudeBackgroundResult("Review still running")))
				observeClaude(t, tr, claudeBackgroundNotification("task-bg", "run-bg", "tool-bg"))
				observeClaude(t, tr, map[string]any{"type": "assistant", "parent_tool_use_id": nil,
					"message": map[string]any{"id": "msg-after-rejection", "content": []any{map[string]any{"type": "text", "text": "Final review report"}}},
				})
				terminal["result"] = "Final review report"
				wantStatus = "completed"
			} else {
				terminal["subtype"] = "error_during_execution"
				terminal["is_error"] = true
			}
			completed := observeClaude(t, tr, terminal)
			completions := 0
			for _, event := range completed.Events {
				if event.Kind == agentproto.EventTurnCompleted {
					completions++
					if event.TurnID != turnID || event.Status != wantStatus {
						t.Fatalf("wrong native completion: %#v", event)
					}
				}
			}
			if completions != 1 || tr.activeTurn != nil || len(tr.pendingTurns) != 1 || tr.pendingTurns[0].CommandID != "cmd-next" {
				t.Fatalf("native result lost turn boundary: completions=%d active=%#v pending=%#v", completions, tr.activeTurn, tr.pendingTurns)
			}
		})
	}
}
