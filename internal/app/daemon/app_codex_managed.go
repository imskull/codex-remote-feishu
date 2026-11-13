package daemon

import (
	"context"
	"log"
	"path/filepath"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/codexmanaged"
)

type ensureManagedCodex func(context.Context, string, string, []string) (string, error)

func (a *App) warmManagedCodex(ctx context.Context, startup bool, ensure ensureManagedCodex) {
	a.mu.Lock()
	cfg := a.headlessRuntime
	a.mu.Unlock()
	if strings.TrimSpace(cfg.CodexRealBinary) == "" || strings.TrimSpace(cfg.Paths.StateDir) == "" {
		return
	}
	if ensure == nil {
		if startup {
			// Gateways must not wait for npm or a concurrent network updater.
			ensure = codexmanaged.SelectLocalWithEnv
		} else {
			ensure = codexmanaged.EnsureWithEnv
		}
	}
	timeout := 2 * time.Minute
	if startup {
		timeout = codexOAuthProbeTimeout
	}
	updateCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	binary, err := ensure(updateCtx, cfg.CodexRealBinary, filepath.Join(cfg.Paths.StateDir, "codex-runtime"), cfg.BaseEnv)
	if err != nil {
		log.Printf("private Codex update unavailable: %v", err)
		return
	}
	if startup {
		// Startup probes must use the same installation as the headless pool.
		// Do not replace configuration that changed while the install was running.
		a.mu.Lock()
		if a.headlessRuntime.CodexRealBinary == cfg.CodexRealBinary {
			a.headlessRuntime.CodexRealBinary = binary
		}
		a.mu.Unlock()
	}
}

func (a *App) runManagedCodexUpdates(ctx context.Context) {
	// Network maintenance starts in Run's existing background worker. It never
	// replaces the binary already selected for startup capability/native probes.
	a.warmManagedCodex(ctx, false, nil)
	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.warmManagedCodex(ctx, false, nil)
		}
	}
}
