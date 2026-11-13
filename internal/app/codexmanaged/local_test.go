package codexmanaged

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

func TestSelectLocalDoesNotWaitForUpdaterOrAccessRegistry(t *testing.T) {
	root := t.TempDir()
	configured := configuredFile(t, "0.153.4")
	if err := writeFakeInstall(filepath.Join(root, "releases", "0.159.3"), "0.159.3"); err != nil {
		t.Fatal(err)
	}
	lock, err := relayruntime.AcquireLock(context.Background(), filepath.Join(root, "update.lock"), true)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()
	ops := fakeOperations("0.170.0")
	ops.lookup = func(context.Context) (string, error) { t.Fatal("startup accessed registry"); return "", nil }
	ops.install = func(context.Context, string, string) error { t.Fatal("startup installed Codex"); return nil }
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	binary, err := selectLocal(ctx, configured, root, ops)
	if err != nil || !strings.Contains(binary, filepath.Join("releases", "0.159.3")) {
		t.Fatalf("local selection blocked by active updater: binary=%q err=%v", binary, err)
	}
	if _, err := os.Stat(filepath.Join(root, "check.json")); !os.IsNotExist(err) {
		t.Fatalf("local startup touched update check state: %v", err)
	}
}

func TestSelectLocalVerifiesPrivateReleaseAndConfiguredBinary(t *testing.T) {
	root := t.TempDir()
	configured := configuredFile(t, "0.153.4")
	// A mismatched release must not become the runtime used for admission probes.
	if err := writeFakeInstall(filepath.Join(root, "releases", "0.159.3"), "0.150.0"); err != nil {
		t.Fatal(err)
	}
	binary, err := selectLocal(context.Background(), configured, root, fakeOperations(""))
	if err != nil || binary != configured {
		t.Fatalf("invalid private release selected: binary=%q err=%v", binary, err)
	}
	if err := os.WriteFile(configured, []byte("not a version"), 0o700); err != nil {
		t.Fatal(err)
	}
	if binary, err := selectLocal(context.Background(), configured, root, fakeOperations("")); err == nil || binary != "" {
		t.Fatalf("invalid configured binary admitted: binary=%q err=%v", binary, err)
	}
}
