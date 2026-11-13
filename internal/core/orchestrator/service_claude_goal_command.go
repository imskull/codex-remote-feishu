package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

// Claude owns goal evaluation and continuation. Send its native slash command
// through the normal prompt queue, keeping the entire loop under one binding.
func (s *Service) handleClaudeGoalCommand(surface *state.SurfaceConsoleRecord, action control.Action) []eventcontract.Event {
	if inst := s.root.Instances[surface.AttachedInstanceID]; inst != nil {
		if thread := inst.Threads[surface.SelectedThreadID]; thread != nil && (threadIsReview(thread) || thread.TrafficClass == agentproto.TrafficClassInternalHelper) {
			return notice(surface, "goal_thread_unsupported", "当前会话是审阅/辅助会话，不支持 Goal。")
		}
	}
	// A command must never become feedback for an outstanding permission/input
	// request, or absorb staged attachments and cease to be a native slash command.
	if surface.ActiveRequestCapture != nil || activePendingRequest(surface) != nil {
		return notice(surface, "goal_request_pending", "请先处理当前请求，或先 /stop，再发送 /goal。")
	}
	for _, image := range surface.StagedImages {
		if image != nil && image.State == state.ImageStaged && image.ActorUserID == action.ActorUserID {
			return notice(surface, "goal_staged_inputs", "请先发送或撤回暂存的图片/文件，再发送 /goal。")
		}
	}
	for _, file := range surface.StagedFiles {
		if file != nil && file.State == state.FileStaged && file.ActorUserID == action.ActorUserID {
			return notice(surface, "goal_staged_inputs", "请先发送或撤回暂存的图片/文件，再发送 /goal。")
		}
	}
	action.Kind = control.ActionTextMessage
	action.Text = strings.TrimSpace(action.Text)
	if action.Text == "" {
		action.Text = "/goal"
	}
	action.Inputs = nil
	return s.handleText(surface, action)
}
