package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

// Model choices belong to a workspace and provider, independently of a live
// attachment. Runtime config observations must not overwrite user preferences.
func (s *Service) rememberCodexModelChoice(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord, action control.Action, mutate func(*state.ModelConfigRecord)) {
	if !s.surfaceIsHeadless(surface) || s.surfaceBackend(surface) != agentproto.BackendCodex {
		return
	}
	if action.Kind != control.ActionModelCommand && action.Kind != control.ActionReasoningCommand {
		return
	}
	workspace := s.surfaceCurrentWorkspaceKey(surface)
	s.updateWorkspaceDefaults(workspace, s.surfaceWorkspaceDefaultsContract(surface, inst), func(current *state.ModelConfigRecord) {
		mutate(current)
	})
}

func (s *Service) modelPreferenceFeedback(surface *state.SurfaceConsoleRecord, temporary, remembered string) string {
	if s.surfaceIsHeadless(surface) && s.surfaceBackend(surface) == agentproto.BackendCodex {
		return remembered
	}
	return temporary
}

func (s *Service) ModelPreferences() map[string]state.ModelConfigRecord {
	result := make(map[string]state.ModelConfigRecord)
	for key, value := range s.root.WorkspaceDefaults {
		if strings.HasPrefix(key, string(agentproto.BackendCodex)+"\x00") {
			value.AccessMode = ""
			if !modelConfigRecordEmpty(value) {
				result[key] = value
			}
		}
	}
	return result
}

func (s *Service) MaterializeModelPreferences(entries map[string]state.ModelConfigRecord) {
	for key, value := range entries {
		if !strings.HasPrefix(key, string(agentproto.BackendCodex)+"\x00") {
			continue
		}
		value.Model = strings.TrimSpace(value.Model)
		value.ReasoningEffort = strings.TrimSpace(value.ReasoningEffort)
		value.AccessMode = ""
		if !modelConfigRecordEmpty(value) {
			s.root.WorkspaceDefaults[key] = value
		}
	}
}
