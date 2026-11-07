package feishu

import (
	"encoding/json"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"strings"
	"testing"
)

func TestSideNoticeQuitCallbackRoundTrip(t *testing.T) {
	const surfaceID = "feishu:app-1:user:user-1"
	ops := NewProjector().ProjectEvent("oc_1", eventcontract.Event{Kind: eventcontract.KindNotice,
		SurfaceSessionID: surfaceID, DaemonLifecycleID: "daemon-1",
		Notice: &control.Notice{Title: "Side mode", Text: "side reply", SideSessionID: "side-1"}})
	if len(ops) != 1 || ops[0].CardTitle != "Side mode" {
		t.Fatalf("unexpected cards: %#v", ops)
	}
	// Exercise the actual value emitted by the projector through gateway parsing.
	last := ops[0].CardElements[len(ops[0].CardElements)-1]
	value := last["actions"].([]map[string]any)[0]["value"].(map[string]any)
	event := cardActionSurfaceEvent(surfaceID, "user-1", "oc_1", "om-1")
	event.Event.Action.Value = value
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
	action, ok := gateway.parseCardActionTriggerEvent(event)
	if !ok || action.Kind != control.ActionSideQuit || action.SideSessionID != "side-1" || action.Inbound.CardDaemonLifecycleID != "daemon-1" {
		t.Fatalf("unexpected quit action: %#v, %v", action, ok)
	}
	delete(value, "daemon_lifecycle_id")
	if _, ok := gateway.parseCardActionTriggerEvent(event); ok {
		t.Fatal("unstamped quit must be rejected")
	}
	value["daemon_lifecycle_id"] = "daemon-1"
	delete(value, "side_session_id")
	if _, ok := gateway.parseCardActionTriggerEvent(event); ok {
		t.Fatal("quit without side identity must be rejected")
	}
}

func TestSideReplySplitsAtCardBudget(t *testing.T) {
	body := strings.Repeat("This is a full side answer with preserved content.\n\n", 1800)
	ops := NewProjector().ProjectEvent("oc_1", eventcontract.Event{Kind: eventcontract.KindNotice,
		SurfaceSessionID: "surface-1", DaemonLifecycleID: "daemon-1",
		Notice: &control.Notice{Code: "side_session_reply", Title: "Side mode", Text: body, SideSessionID: "side-1"}})
	if len(ops) < 2 {
		t.Fatal("expected side answer to split")
	}
	var joined string
	for i, op := range ops {
		if !strings.HasPrefix(op.CardTitle, "Side mode") {
			t.Fatalf("chunk %d lost side label", i)
		}
		data, err := json.Marshal(renderOperationCard(op, cardEnvelopeV2))
		if err != nil {
			t.Fatal(err)
		}
		if len(data) > 30*1024 {
			t.Fatalf("chunk %d exceeds card budget: %d", i, len(data))
		}
		joined += op.CardBody
	}
	if strings.Count(joined, "This is a full side answer with preserved content.") != 1800 {
		t.Fatal("side split lost content")
	}
}
