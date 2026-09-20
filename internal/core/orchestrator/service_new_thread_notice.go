package orchestrator

import (
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func (s *Service) newThreadReadyNotice(surface *state.SurfaceConsoleRecord, code, text string) []eventcontract.Event {
	summary := s.resolveNextPromptSummary(s.root.Instances[surface.AttachedInstanceID], surface, "", "", state.ModelConfigRecord{})
	return []eventcontract.Event{surfaceEventFromPayload(surface, eventcontract.NoticePayload{
		Notice: control.Notice{
			Code:           code,
			Sections:       []control.FeishuCardTextSection{{Lines: []string{text}}},
			PromptSettings: &summary,
		},
	}, eventcontract.EventMeta{})}
}
