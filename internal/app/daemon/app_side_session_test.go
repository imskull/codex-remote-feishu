package daemon

import (
	"context"
	"errors"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestSideCleanupDispatchSurvivesSurfaceDetach(t *testing.T) {
	app := New(":0", ":0", nil, serverIdentityForTest())
	app.service.MaterializeSurface("surface", "", "chat", "user")
	var instance string
	app.sendAgentCommand = func(target string, command agentproto.Command) error {
		instance = target
		if command.SideSessionID != "side" || command.Kind != agentproto.CommandSideSessionClose {
			t.Fatal("lost side cleanup identity")
		}
		return errors.New("disconnected")
	}
	surface := app.service.Surface("surface")
	// The route has already detached when its cleanup command is delivered.
	surface.AttachedInstanceID = ""
	surface.ActiveQueueItemID = "main"
	surface.QueueItems = map[string]*state.QueueItemRecord{"main": {ID: "main", Status: state.QueueItemDispatching}}
	app.mu.Lock()
	defer app.mu.Unlock()
	app.handleUIEventsLocked(context.Background(), []eventcontract.Event{{Kind: eventcontract.KindAgentCommand, SurfaceSessionID: "surface",
		Command: &agentproto.Command{Kind: agentproto.CommandSideSessionClose, SideSessionID: "side", Target: agentproto.Target{InstanceID: "original-instance"}}}})
	if instance != "original-instance" {
		t.Fatal("cleanup was not dispatched to original instance")
	}
	if surface.QueueItems["main"].Status != state.QueueItemDispatching {
		t.Fatal("cleanup dispatch failure consumed parent ownership")
	}
}
