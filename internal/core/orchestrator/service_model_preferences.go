package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

// Model choices belong to a chat and provider, independently of a live
// attachment. Runtime config observations must not overwrite user preferences.
func (s *Service) rememberCodexModelChoice(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord, action control.Action, mutate func(*state.ModelConfigRecord)) {
	if !s.surfaceIsHeadless(surface) || s.surfaceBackend(surface) != agentproto.BackendCodex {
		return
	}
	if action.Kind != control.ActionModelCommand && action.Kind != control.ActionReasoningCommand {
		return
	}
	key := s.chatModelPreferenceKey(surface, inst)
	if key == "" {
		return
	}
	current := s.root.ChatModelPreferences[key]
	mutate(&current)
	current.AccessMode = ""
	if modelConfigRecordEmpty(current) {
		delete(s.root.ChatModelPreferences, key)
		return
	}
	s.root.ChatModelPreferences[key] = current
}

func (s *Service) chatModelPreferenceKey(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord) string {
	if surface == nil || !s.surfaceIsHeadless(surface) || s.surfaceBackend(surface) != agentproto.BackendCodex {
		return ""
	}
	contract := s.surfaceWorkspaceDefaultsContract(surface, inst)
	return state.ChatModelPreferenceKey(surface.GatewayID, surface.ChatID, contract.CodexProviderID)
}

func (s *Service) modelPreferenceFeedback(surface *state.SurfaceConsoleRecord, temporary, remembered string) string {
	if s.surfaceIsHeadless(surface) && s.surfaceBackend(surface) == agentproto.BackendCodex {
		return remembered
	}
	return temporary
}

func (s *Service) ModelPreferences() map[string]state.ModelConfigRecord {
	result := make(map[string]state.ModelConfigRecord)
	for key, value := range s.root.ChatModelPreferences {
		result[key] = value
	}
	return result
}

func (s *Service) MaterializeModelPreferences(entries map[string]state.ModelConfigRecord) {
	s.root.ChatModelPreferences = make(map[string]state.ModelConfigRecord)
	for key, value := range entries {
		parts := strings.Split(key, "\x00")
		if len(parts) != 4 || parts[0] != "codex" || key != state.ChatModelPreferenceKey(parts[2], parts[3], parts[1]) {
			continue
		}
		value.Model = strings.TrimSpace(value.Model)
		value.ReasoningEffort = strings.TrimSpace(value.ReasoningEffort)
		value.AccessMode = ""
		if !modelConfigRecordEmpty(value) {
			s.root.ChatModelPreferences[key] = value
		}
	}
}
