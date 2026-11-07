package orchestrator

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/codex"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
)

func TestSideCodexNativeTurnLifecycle(t *testing.T) {
	for _, notificationFirst := range []bool{false, true} {
		t.Run(map[bool]string{false: "response_first", true: "notification_first"}[notificationFirst], func(t *testing.T) {
			s, surface, now := sideTestService(t)
			tr := codex.NewTranslator("inst")
			observe := func(raw []byte) []eventcontract.Event {
				t.Helper()
				result, err := tr.ObserveServer(raw)
				if err != nil {
					t.Fatal(err)
				}
				var events []eventcontract.Event
				for _, event := range result.Events {
					events = append(events, s.ApplyAgentEvent("inst", event)...)
				}
				return events
			}
			reply := func(frame []byte, result map[string]any) []eventcontract.Event {
				t.Helper()
				var request map[string]any
				if err := json.Unmarshal(frame, &request); err != nil {
					t.Fatal(err)
				}
				raw, _ := json.Marshal(map[string]any{"id": request["id"], "result": result})
				return observe(raw)
			}
			observe([]byte(`{"id":"parent-config","result":{"thread":{"id":"parent"},"approvalPolicy":"never","sandbox":{"type":"readOnly"}}}`))
			open := sideCommands(sideAction(s, "/side first"))[0]
			frames, err := tr.TranslateCommand(*open)
			if err != nil {
				t.Fatal(err)
			}
			events := reply(frames[0], map[string]any{"thread": map[string]any{"id": "fork"}})
			prompt := sideCommands(events)[0]
			frames, err = tr.TranslateCommand(*prompt)
			if err != nil {
				t.Fatal(err)
			}
			started := []byte(`{"method":"turn/started","params":{"threadId":"fork","turn":{"id":"side-turn","status":"inProgress"}}}`)
			if notificationFirst {
				observe(started)
			}
			reply(frames[0], map[string]any{"turn": map[string]any{"id": "side-turn", "status": "inProgress"}})
			if !notificationFirst {
				observe(started)
			}
			if surface.SideSession.ActiveTurnID != "side-turn" {
				t.Fatal("native turn never acquired side execution ownership")
			}
			observe([]byte(`{"method":"item/completed","params":{"threadId":"fork","turnId":"side-turn","item":{"id":"answer","type":"agentMessage","text":"side answer"}}}`))
			events = observe([]byte(`{"method":"turn/completed","params":{"threadId":"fork","turn":{"id":"side-turn","status":"completed"}}}`))
			if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Text != "side answer" {
				t.Fatalf("missing side reply: %#v", events)
			}
			*now = now.Add(time.Minute)
			s.tickSideSessions(*now)
			if surface.SideSession == nil || surface.SideSession.ActiveCommandID != "" || surface.SelectedThreadID != "parent" {
				t.Fatal("completed side turn timed out or changed parent")
			}
		})
	}
}
