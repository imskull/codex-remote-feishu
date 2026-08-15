package state

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/testutil"
)

func TestResolveWorkspaceKey(t *testing.T) {
	want := testutil.WorkspacePath("data", "dl", "droid")
	input := " " + testutil.WorkspacePath("data", "dl", "work", "..", "droid") + "/ "
	if got := ResolveWorkspaceKey("", input); got != want {
		t.Fatalf("ResolveWorkspaceKey() = %q, want %q", got, want)
	}
	if got := ResolveWorkspaceKey("   "); got != "" {
		t.Fatalf("ResolveWorkspaceKey() = %q, want empty", got)
	}
}

func TestNormalizeWorkspaceKeyForGOOSWindowsStripsExtendedVolumePrefix(t *testing.T) {
	got := normalizeWorkspaceKeyForGOOS("windows", `\\?\D:\AI\campaigns\active`)
	if got != "D:/AI/campaigns/active" {
		t.Fatalf("normalizeWorkspaceKeyForGOOS(windows) = %q, want volume path without extended prefix", got)
	}
}

func TestNormalizeWorkspaceKeyForGOOSWindowsPreservesUNCPath(t *testing.T) {
	got := normalizeWorkspaceKeyForGOOS("windows", `\\?\UNC\fileserver\campaigns\active`)
	if got != "//fileserver/campaigns/active" {
		t.Fatalf("normalizeWorkspaceKeyForGOOS(windows) = %q, want UNC path without extended prefix", got)
	}
}

func TestWorkspaceShortName(t *testing.T) {
	if got := WorkspaceShortName(testutil.WorkspacePath("data", "dl", "work", "..", "droid") + "/"); got != "droid" {
		t.Fatalf("WorkspaceShortName() = %q, want %q", got, "droid")
	}
	if got := WorkspaceShortName("/"); got != "/" {
		t.Fatalf("WorkspaceShortName(root) = %q, want /", got)
	}
}

func TestResolveWorkspaceRootOnHostResolvesSymlink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "real")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	resolved, err := ResolveWorkspaceRootOnHost(filepath.Join(link, ".", ""))
	if err != nil {
		t.Fatalf("ResolveWorkspaceRootOnHost() error = %v", err)
	}
	if !testutil.SamePath(resolved, target) {
		t.Fatalf("ResolveWorkspaceRootOnHost() = %q, want %q", resolved, target)
	}
}
