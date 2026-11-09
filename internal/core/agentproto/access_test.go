package agentproto

import "testing"

func TestAutoAccessModeHasConservativeCodexPolicies(t *testing.T) {
	if NormalizeAccessMode(" AUTO ") != AccessModeAuto || DisplayAccessModeShort(AccessModeAuto) != "auto" {
		t.Fatal("auto must normalize and display without becoming full access")
	}
	if ApprovalPolicyForAccessMode(AccessModeAuto) != "on-request" || ThreadSandboxForAccessMode(AccessModeAuto) != "workspace-write" || TurnSandboxPolicyForAccessMode(AccessModeAuto)["type"] != "workspaceWrite" {
		t.Fatal("Claude-only auto must never bypass Codex permissions")
	}
}
