package codexmanaged

import (
	"context"
	"fmt"
	"path/filepath"
)

// SelectLocalWithEnv verifies the configured binary and published private
// releases without registry access, installation, or waiting for an updater.
// Releases are published atomically; staging directories are never selected.
func SelectLocalWithEnv(ctx context.Context, configuredBinary, root string, env []string) (string, error) {
	resolved, err := configuredCodexBinary(configuredBinary, env)
	if err != nil {
		return "", fmt.Errorf("resolve configured Codex for startup: %w", err)
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve private Codex install root: %w", err)
	}
	return selectLocal(ctx, resolved, root, operations{
		readVersion: func(ctx context.Context, binary string) (string, error) {
			return readBinaryVersionWithEnv(ctx, binary, env)
		},
	})
}

func selectLocal(ctx context.Context, configuredBinary, root string, ops operations) (string, error) {
	configured, err := verifyCandidate(ctx, configuredBinary, ops)
	if err != nil {
		return "", fmt.Errorf("verify configured Codex for startup: %w", err)
	}
	best, err := newestPrivate(ctx, root, ops)
	if err != nil {
		return "", err
	}
	if best.binary == "" || configured.version.newerThan(best.version) {
		best = configured
	}
	return best.binary, nil
}
