package daemon

import (
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/surfaceresume"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSideSessionRestartKeepsOnlyEndedMarker(t *testing.T) {
	app := New(":0", ":0", nil, serverIdentityForTest())
	store := surfaceresume.NewStore(filepath.Join(t.TempDir(), "resume.json"))
	app.surfaceResumeRuntime.store = store
	app.service.MaterializeSurface("surface-1", "gateway-1", "chat-1", "user-1")
	surface := app.service.Surface("surface-1")
	surface.SideSession = &state.SideSessionRecord{SessionID: "side-secret", ThreadID: "fork-thread", Queue: []state.SideMessage{{CommandID: "never-replay"}}}
	entry, ok := app.currentSurfaceResumeEntryLocked(surface, false)
	if !ok || !entry.SideSessionActive {
		t.Fatalf("missing active marker: %#v", entry)
	}
	if err := store.Put(entry); err != nil {
		t.Fatal(err)
	}
	loaded, err := surfaceresume.LoadStore(store.Path())
	if err != nil {
		t.Fatal(err)
	}
	persisted, ok := loaded.Get("surface-1")
	if !ok || !persisted.SideSessionActive {
		t.Fatal("marker lost in persistence")
	}
	data, err := os.ReadFile(store.Path())
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"side-secret", "fork-thread", "never-replay"} {
		if strings.Contains(string(data), secret) {
			t.Fatalf("persisted temporary routing: %s", secret)
		}
	}
	restarted := New(":0", ":0", nil, serverIdentityForTest())
	restarted.materializeSurfaceResumeEntryLocked(persisted)
	restored := restarted.service.Surface("surface-1")
	if restored == nil || restored.SideSession != nil || !restored.SideSessionEnded {
		t.Fatalf("restart restored side routing or lost ended notice: %#v", restored)
	}
	restored.SideSessionEnded = false
	restarted.surfaceResumeRuntime.store = loaded
	cleared, ok := restarted.currentSurfaceResumeEntryLocked(restored, false)
	if !ok || cleared.SideSessionActive || surfaceresume.SameEntryContent(persisted, cleared) {
		t.Fatal("ended marker cannot be cleared after reporting")
	}
}
