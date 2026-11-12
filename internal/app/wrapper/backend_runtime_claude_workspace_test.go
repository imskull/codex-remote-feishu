package wrapper

import (
	"path/filepath"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestClaudeResumeAndSideForkUseOriginalWorkspaceOwnership(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configDir)
	workspaceRoot := filepath.Join(t.TempDir(), "workspace")
	researchCWD := filepath.Join(workspaceRoot, ".manual-strategy-research", "strategy")
	otherWorkspace := filepath.Join(t.TempDir(), "other-workspace")
	for _, fixture := range []struct {
		threadID, originalWorkspace, latestCWD string
	}{
		{"parent", workspaceRoot, researchCWD},
		// A foreign session must remain rejected even after changing into this workspace.
		{"foreign", otherWorkspace, workspaceRoot},
	} {
		writeWrapperClaudeSessionFile(t, configDir, fixture.originalWorkspace, fixture.threadID, []map[string]any{
			{"type": "user", "cwd": fixture.originalWorkspace, "message": map[string]any{"role": "user", "content": "initial prompt"}},
			{"type": "user", "cwd": fixture.latestCWD, "message": map[string]any{"role": "user", "content": "follow-up"}},
		})
	}
	runtime := &claudeBackendRuntime{workspaceRoot: workspaceRoot}
	for _, fork := range []bool{false, true} {
		name := "resume"
		if fork {
			name = "side-fork"
		}
		t.Run(name, func(t *testing.T) {
			for _, threadID := range []string{"parent", "foreign"} {
				plan := agentproto.PromptDispatchPlan{ExecutionMode: agentproto.PromptExecutionModeResumeExisting, ExecutionThreadID: threadID, CWD: workspaceRoot}
				var target *claudeLaunchResumeTarget
				var found bool
				var err error
				if fork {
					plan.ExecutionMode = agentproto.PromptExecutionModeForkEphemeral
					plan.SourceThreadID, plan.ExecutionThreadID = threadID, ""
					target, found, err = runtime.lookupForkSourceTarget(plan)
				} else {
					// Side-session open uses this same resolver for its parent.
					target, found, err = runtime.lookupStoredResumeTarget(plan)
				}
				if threadID == "foreign" {
					info, ok := err.(agentproto.ErrorInfo)
					if !ok || info.Code != "claude_resume_workspace_mismatch" || found || target != nil {
						t.Fatalf("foreign session accepted: target=%#v found=%v err=%v", target, found, err)
					}
					continue
				}
				if err != nil || !found || target == nil {
					t.Fatalf("session from original workspace rejected after cwd change: found=%v err=%v", found, err)
				}
				if target.ThreadID != threadID || target.CWD != researchCWD {
					t.Fatalf("incorrect resolved target: %#v", target)
				}
			}
		})
	}
}
