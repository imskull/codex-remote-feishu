package state

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestAutoAccessOverrideBackendIsolation(t *testing.T) {
	for _, backend := range []agentproto.Backend{agentproto.BackendClaude, agentproto.BackendCodex, agentproto.BackendOpenCode} {
		want := agentproto.AccessModeConfirm
		if backend == agentproto.BackendClaude {
			want = agentproto.AccessModeAuto
		}
		got := NormalizePromptOverrideForBackend(backend, ModelConfigRecord{AccessMode: agentproto.AccessModeAuto})
		if got.AccessMode != want {
			t.Fatalf("backend %s: got %q, want %q", backend, got.AccessMode, want)
		}
	}
	if got := NormalizeClaudeWorkspaceProfileSnapshotRecord(ClaudeWorkspaceProfileSnapshotRecord{AccessMode: agentproto.AccessModeAuto}); got.AccessMode != agentproto.AccessModeAuto {
		t.Fatalf("profile snapshot lost auto: %#v", got)
	}
}
