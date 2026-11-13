package wrapper

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"

	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

func TestManagedCodexDefaultSelectionDoesNotWaitForUpdater(t *testing.T) {
	root := t.TempDir()
	// Build a real executable so the default selector must verify --version on
	// Windows as well as Unix, without depending on an installed Codex.
	source := filepath.Join(root, "codex.go")
	if err := os.WriteFile(source, []byte(`package main
import ("fmt"; "os")
func main() {
 if len(os.Args) != 2 || os.Args[1] != "--version" { os.Exit(2) }
 fmt.Println("codex-cli 0.159.3")
}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(root, "codex")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	if output, err := exec.Command("go", "build", "-o", binary, source).CombinedOutput(); err != nil {
		t.Fatalf("build version helper: %v\n%s", err, output)
	}
	privateRoot := filepath.Join(root, "codex-runtime")
	if err := os.MkdirAll(privateRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	lock, err := relayruntime.AcquireLock(context.Background(), filepath.Join(privateRoot, "update.lock"), true)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()
	app := New(Config{Source: "headless", Managed: true, Lifetime: "daemon-owned", CodexRealBinary: binary, RuntimePaths: relayruntime.Paths{StateDir: root}})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	got, err := app.selectCodexChildBinary(ctx, os.Environ(), nil)
	if err != nil || got != binary {
		t.Fatalf("default startup blocked by active updater: binary=%q err=%v", got, err)
	}
	if _, err := os.Stat(filepath.Join(privateRoot, "check.json")); !os.IsNotExist(err) {
		t.Fatalf("startup touched update check state: %v", err)
	}
}

func TestManagedCodexSelectionIsLimitedToOwnedHeadlessChildren(t *testing.T) {
	for _, cfg := range []Config{
		{Source: "vscode", Managed: true, Lifetime: "host-bound"},
		{Source: "headless", Lifetime: "standalone"},
		{Source: "headless", Managed: true, Lifetime: "host-bound"},
	} {
		cfg.CodexRealBinary = "/original/codex"
		app := New(cfg)
		got, err := app.selectCodexChildBinary(context.Background(), nil, func(context.Context, string, string, []string) (string, error) {
			t.Fatal("external client triggered a private update")
			return "", nil
		})
		if err != nil || got != cfg.CodexRealBinary {
			t.Fatalf("got %q, %v", got, err)
		}
	}
}

func TestManagedCodexSelectionUsesPrivateRootAndRestoredEnvironment(t *testing.T) {
	root := t.TempDir()
	app := New(Config{Source: "headless", Managed: true, Lifetime: "daemon-owned", CodexRealBinary: "/original/codex", RuntimePaths: relayruntime.Paths{StateDir: root}})
	env := []string{"HTTPS_PROXY=http://localhost:1234", "CODEX_HOME=/original"}
	got, err := app.selectCodexChildBinary(context.Background(), env, func(ctx context.Context, configured, privateRoot string, childEnv []string) (string, error) {
		if configured != "/original/codex" || privateRoot != filepath.Join(root, "codex-runtime") || !reflect.DeepEqual(childEnv, env) {
			t.Fatalf("wrong update material: %s %s %v", configured, privateRoot, childEnv)
		}
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 5*time.Second {
			t.Fatal("update exceeds child startup watchdog budget")
		}
		return "/private/codex", nil
	})
	if err != nil || got != "/private/codex" {
		t.Fatalf("got %q, %v", got, err)
	}
	_, err = app.selectCodexChildBinary(context.Background(), env, func(context.Context, string, string, []string) (string, error) {
		return "", errors.New("install failed")
	})
	if err == nil {
		t.Fatal("known failed update fell back to old binary")
	}
}

func TestManagedCodexRestartPreparationFailureKeepsCurrentChild(t *testing.T) {
	app := New(Config{Source: "headless", Managed: true, Lifetime: "daemon-owned", Args: []string{"app-server"}})
	stopped := false
	current := &childSession{cancel: func() { stopped = true }}
	next, err := app.restartChildSession(context.Background(), restartRequest{}, current, nil, nil, nil, nil, nil, nil, nil, 0, nil, nil, nil)
	if err == nil || stopped || next != current {
		t.Fatalf("failed update stopped current child: next=%p current=%p stopped=%v err=%v", next, current, stopped, err)
	}
}
