package claude

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/xutil"
)

// Background runs belong to the tool call that launched them, not whichever
// conversation happens to be selected when their notification arrives.
type backgroundTask struct {
	TaskID    string
	ToolUseID string
}

func (t *Translator) observeBackgroundTask(message map[string]any) {
	turn := t.activeTurn
	if turn == nil || strings.TrimSpace(xutil.LookupStringFromAny(message["session_id"])) != turn.ThreadID {
		return
	}
	runID := strings.TrimSpace(xutil.LookupStringFromAny(message["run_id"]))
	taskID := strings.TrimSpace(xutil.LookupStringFromAny(message["task_id"]))
	toolID := strings.TrimSpace(xutil.LookupStringFromAny(message["tool_use_id"]))
	if runID == "" || taskID == "" || toolID == "" {
		return
	}
	switch xutil.LookupStringFromAny(message["subtype"]) {
	case "task_started":
		if !xutil.LookupBoolFromAny(message["is_backgrounded"]) {
			return
		}
		tool := t.toolStates[toolID]
		if tool == nil || tool.TurnID != turn.TurnID {
			return
		}
		if turn.BackgroundTasks == nil {
			turn.BackgroundTasks = map[string]backgroundTask{}
		}
		turn.BackgroundTasks[runID] = backgroundTask{TaskID: taskID, ToolUseID: toolID}
	case "task_notification":
		task, ok := turn.BackgroundTasks[runID]
		if !ok || task.TaskID != taskID || task.ToolUseID != toolID {
			return
		}
		switch xutil.LookupStringFromAny(message["status"]) {
		case "completed", "failed", "stopped":
			delete(turn.BackgroundTasks, runID)
		}
	}
}
