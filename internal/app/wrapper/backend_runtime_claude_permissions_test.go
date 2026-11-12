package wrapper

import (
	"path/filepath"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/adapter/claude"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestClaudeResumeRestoresPermissionMode(t *testing.T) {
	for _, mode := range []string{"auto", "default", "acceptEdits", "plan", "bypassPermissions", ""} {
		t.Run("stored-"+mode, func(t *testing.T) {
			configDir, workspace := t.TempDir(), t.TempDir()
			t.Setenv("CLAUDE_CONFIG_DIR", configDir)
			writeWrapperClaudeSessionFile(t, configDir, workspace, "parent", []map[string]any{
				{"type": "user", "cwd": workspace, "permissionMode": mode, "message": map[string]any{"role": "user", "content": "hello"}},
			})
			for _, initial := range []bool{true, false} {
				r := newBackendRuntime(Config{Backend: agentproto.BackendClaude, WorkspaceRoot: workspace, ResumeThreadID: "parent"}).(*claudeBackendRuntime)
				target := r.consumeLaunchResumeTarget()
				if !initial {
					var err error
					target, err = r.resolveLaunchResumeTarget(agentproto.PromptDispatchPlan{ExecutionMode: agentproto.PromptExecutionModeResumeExisting, ExecutionThreadID: "parent"})
					if err != nil {
						t.Fatal(err)
					}
					if target.PermissionMode != mode {
						t.Fatalf("resolved mode = %q, want %q", target.PermissionMode, mode)
					}
				}
				if err := r.restoreLaunchPermissionMode(target); err != nil {
					t.Fatal(err)
				}
				app := &App{config: Config{WorkspaceRoot: workspace}}
				args, _ := app.buildClaudeChildLaunch(target)
				want := mode
				if want == "" {
					want = "auto"
				}
				assertArgPair(t, args, "--permission-mode", want)
				assertArgPair(t, args, "--resume", "parent")
			}
		})
	}
}

func TestClaudeResumePrefersCurrentPermissionMode(t *testing.T) {
	r := &claudeBackendRuntime{translator: claude.NewTranslator("inst")}
	if _, err := r.ObserveServer([]byte(`{"type":"system","subtype":"init","session_id":"parent","permissionMode":"acceptEdits"}`)); err != nil {
		t.Fatal(err)
	}
	target := &claudeLaunchResumeTarget{ThreadID: "parent"}
	if err := r.restoreLaunchPermissionMode(target); err != nil || target.PermissionMode != "acceptEdits" {
		t.Fatalf("target=%#v err=%v", target, err)
	}
	target.PermissionMode = "plan"
	if err := r.restoreLaunchPermissionMode(target); err != nil || target.PermissionMode != "acceptEdits" {
		t.Fatalf("stale stored mode beat live state: target=%#v err=%v", target, err)
	}
	target.ThreadID = "other"
	target.PermissionMode = "plan"
	if err := r.restoreLaunchPermissionMode(target); err != nil || target.PermissionMode != "plan" {
		t.Fatalf("supplied mode lost: target=%#v err=%v", target, err)
	}
}

func TestClaudeResumeDoesNotInheritAnotherSessionPermissionMode(t *testing.T) {
	configDir, workspace := t.TempDir(), t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configDir)
	writeWrapperClaudeSessionFile(t, configDir, workspace, "target", []map[string]any{
		{"type": "user", "cwd": workspace, "permissionMode": "default"},
	})
	r := &claudeBackendRuntime{workspaceRoot: workspace, translator: claude.NewTranslator("inst")}
	if _, err := r.ObserveServer([]byte(`{"type":"system","subtype":"init","session_id":"other","permissionMode":"bypassPermissions"}`)); err != nil {
		t.Fatal(err)
	}
	target := &claudeLaunchResumeTarget{ThreadID: "target"}
	if err := r.restoreLaunchPermissionMode(target); err != nil || target.PermissionMode != "default" {
		t.Fatalf("target=%#v err=%v", target, err)
	}
	foreignWorkspace := filepath.Join(t.TempDir(), "foreign")
	writeWrapperClaudeSessionFile(t, configDir, foreignWorkspace, "foreign", []map[string]any{
		{"type": "user", "cwd": foreignWorkspace, "permissionMode": "bypassPermissions"},
	})
	if err := r.restoreLaunchPermissionMode(&claudeLaunchResumeTarget{ThreadID: "foreign"}); err == nil {
		t.Fatal("foreign session must be rejected")
	}
}
