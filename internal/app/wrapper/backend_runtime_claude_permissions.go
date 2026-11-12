package wrapper

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/claudesessionstore"
)

// restoreLaunchPermissionMode runs under r.mu, before PrepareForChildLaunch clears
// the translator's observed state. Claude --resume does not restore this launch option.
func (r *claudeBackendRuntime) restoreLaunchPermissionMode(resume *claudeLaunchResumeTarget) error {
	if resume == nil || strings.TrimSpace(resume.ThreadID) == "" {
		return nil
	}
	snapshot := r.translator.RuntimeStateSnapshot()
	if snapshot.SessionID == strings.TrimSpace(resume.ThreadID) && snapshot.NativePermissionMode != "" {
		resume.PermissionMode = snapshot.NativePermissionMode
		return nil
	}
	if strings.TrimSpace(resume.PermissionMode) != "" {
		return nil
	}
	// Startup recovery has no live translator state. Read the raw native mode:
	// projected access/plan defaults cannot distinguish missing metadata from an
	// explicitly selected confirm mode.
	meta, err := claudesessionstore.ResolveResumeSession(r.workspaceRoot, resume.ThreadID)
	if err != nil {
		return err
	}
	if meta != nil {
		resume.PermissionMode = strings.TrimSpace(meta.NativePermissionMode)
	}
	return nil
}
