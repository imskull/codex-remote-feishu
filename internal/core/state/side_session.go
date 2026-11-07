package state

import (
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

// SideSessionRecord is deliberately runtime-only: queued side input must never
// be restored as input to the selected parent conversation.
type SideSessionRecord struct {
	SessionID       string
	InstanceID      string
	ParentThreadID  string
	ThreadID        string
	CWD             string
	Status          string
	OpenedAt        time.Time
	ActiveCommandID string
	ActiveTurnID    string
	Queue           []SideMessage
	Items           map[string]string
	ItemOrder       []string
}

type SideMessage struct {
	CommandID string
	MessageID string
	Inputs    []agentproto.Input
}
