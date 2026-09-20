package daemon

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestWorkspacePreferencesAreNotAssignedToAnArbitraryChat(t *testing.T) {
	dir := t.TempDir()
	oldKey := state.WorkspaceDefaultsStorageKey("/workspace/project", state.CodexInstanceBackendContract(""))
	raw, err := json.Marshal(modelPreferencesFile{Version: 1, Entries: map[string]state.ModelConfigRecord{oldKey: {Model: "gpt-6-astra"}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "model-preferences.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	a := &App{service: orchestrator.NewService(time.Now, orchestrator.Config{}, nil)}
	a.configureModelPreferencesLocked(dir)
	if len(a.service.ModelPreferences()) != 0 {
		t.Fatal("workspace preference leaked into chat scope")
	}
	key := state.ChatModelPreferenceKey("app-1", "chat-1", "")
	a.service.MaterializeModelPreferences(map[string]state.ModelConfigRecord{key: {Model: "gpt-6-astra", ReasoningEffort: "medium"}})
	a.syncModelPreferencesLocked()
	b := &App{service: orchestrator.NewService(time.Now, orchestrator.Config{}, nil)}
	b.configureModelPreferencesLocked(dir)
	if b.service.ModelPreferences()[key].Model != "gpt-6-astra" {
		t.Fatal("legacy state prevented saving new chat preference")
	}
}

func TestModelCommandAutomaticallyPersistsPreference(t *testing.T) {
	dir := t.TempDir()
	a := New(":0", ":0", &recordingGateway{}, agentproto.ServerIdentity{PID: 42, StartedAt: time.Now()})
	a.configureModelPreferencesLocked(dir)
	a.service.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
	a.service.UpsertInstance(&state.InstanceRecord{InstanceID: "inst-1", WorkspaceRoot: "/workspace/project", WorkspaceKey: "/workspace/project", Source: "headless", Managed: true, Online: true, Threads: map[string]*state.ThreadRecord{}})
	a.service.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", InstanceID: "inst-1", ChatID: "chat-1", ActorUserID: "user-1"})
	for _, command := range []string{"/model gpt-6-astra medium", "/model clear"} {
		a.HandleAction(context.Background(), control.Action{Kind: control.ActionModelCommand, GatewayID: "app-1", SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", Text: command})
		b := &App{service: orchestrator.NewService(time.Now, orchestrator.Config{}, nil)}
		b.configureModelPreferencesLocked(dir)
		key := state.ChatModelPreferenceKey("app-1", "chat-1", "")
		got := b.service.ModelPreferences()[key]
		if command == "/model clear" {
			if got != (state.ModelConfigRecord{}) {
				t.Fatalf("clear not persisted: %#v", got)
			}
		} else if got.Model != "gpt-6-astra" || got.ReasoningEffort != "medium" {
			t.Fatalf("model command not persisted: %#v", got)
		}
	}
}

func TestModelPreferencesSurviveRestartAndClear(t *testing.T) {
	dir := t.TempDir()
	key := state.ChatModelPreferenceKey("app-1", "chat-1", "")
	entry := state.ModelConfigRecord{Model: "gpt-6-astra", ReasoningEffort: "medium"}
	path := filepath.Join(dir, "model-preferences.json")
	if err := saveModelPreferences(path, map[string]state.ModelConfigRecord{key: entry}); err != nil {
		t.Fatal(err)
	}
	a := &App{service: orchestrator.NewService(time.Now, orchestrator.Config{}, nil)}
	a.configureModelPreferencesLocked(dir)
	if got := a.service.ModelPreferences()[key]; got != entry {
		t.Fatalf("restart lost model choice: %#v", got)
	}
	// Exercise replacement of an existing file, including on Windows.
	if err := saveModelPreferences(path, map[string]state.ModelConfigRecord{}); err != nil {
		t.Fatal(err)
	}
	b := &App{service: orchestrator.NewService(time.Now, orchestrator.Config{}, nil)}
	b.configureModelPreferencesLocked(dir)
	if len(b.service.ModelPreferences()) != 0 {
		t.Fatal("cleared preference returned after restart")
	}
	if err := os.WriteFile(path, []byte("invalid json"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := &App{service: orchestrator.NewService(time.Now, orchestrator.Config{}, nil)}
	c.configureModelPreferencesLocked(dir)
	c.syncModelPreferencesLocked()
	raw, _ := os.ReadFile(path)
	if string(raw) != "invalid json" {
		t.Fatal("unreadable state must not be overwritten")
	}
}
