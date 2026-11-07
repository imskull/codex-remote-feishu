package wrapper

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestSideDisconnectQueuesCodexCleanupWithoutStoppingMain(t *testing.T) {
	runtime := newBackendRuntime(Config{Backend: agentproto.BackendCodex})
	_, err := runtime.ObserveServer([]byte(`{"id":"main-start","result":{"thread":{"id":"parent"},"approvalPolicy":"never","sandbox":{"type":"readOnly"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	opened, err := runtime.TranslateCommand(agentproto.Command{Kind: agentproto.CommandSideSessionOpen, SideSessionID: "side", Target: agentproto.Target{ThreadID: "parent"}})
	if err != nil {
		t.Fatal(err)
	}
	var request map[string]any
	if err := json.Unmarshal(flattenRuntimeCommandPhaseFrames(opened.Phases)[0], &request); err != nil {
		t.Fatal(err)
	}
	reply, _ := json.Marshal(map[string]any{"id": request["id"], "result": map[string]any{"thread": map[string]any{"id": "fork"}}})
	if _, err := runtime.ObserveServer(reply); err != nil {
		t.Fatal(err)
	}
	for _, frame := range []string{`{"method":"turn/started","params":{"threadId":"parent","turn":{"id":"main-turn"}}}`, `{"method":"turn/started","params":{"threadId":"fork","turn":{"id":"side-turn"}}}`} {
		if _, err := runtime.ObserveServer([]byte(frame)); err != nil {
			t.Fatal(err)
		}
	}
	writeCh := make(chan []byte, 1)
	if !disconnectRuntimeSideSessions(runtime, writeCh, wrapperSideDisconnectTimeout, nil) {
		t.Fatal("cleanup delivery failed")
	}
	frames := bytes.Split(bytes.TrimSpace(<-writeCh), []byte("\n"))
	if len(frames) != 2 {
		t.Fatalf("expected interrupt and unsubscribe, got %d", len(frames))
	}
	for _, frame := range frames {
		var message map[string]any
		if err := json.Unmarshal(frame, &message); err != nil {
			t.Fatal(err)
		}
		if message["params"].(map[string]any)["threadId"] != "fork" {
			t.Fatalf("cleanup reached parent %#v", message)
		}
	}
	main, err := runtime.ObserveServer([]byte(`{"method":"turn/completed","params":{"threadId":"parent","turn":{"id":"main-turn","status":"completed"}}}`))
	if err != nil || len(main.Events) != 1 || main.Events[0].SideSessionID != "" || main.Suppress {
		t.Fatalf("main execution interrupted %#v %v", main, err)
	}
}

func TestSideDisconnectWriterDeadlineIsBounded(t *testing.T) {
	runtime := newBackendRuntime(Config{Backend: agentproto.BackendCodex})
	runtime.ObserveServer([]byte(`{"id":"main-start","result":{"thread":{"id":"parent"},"approvalPolicy":"never","sandbox":{"type":"readOnly"}}}`))
	opened, err := runtime.TranslateCommand(agentproto.Command{Kind: agentproto.CommandSideSessionOpen, SideSessionID: "side", Target: agentproto.Target{ThreadID: "parent"}})
	if err != nil {
		t.Fatal(err)
	}
	var request map[string]any
	json.Unmarshal(flattenRuntimeCommandPhaseFrames(opened.Phases)[0], &request)
	reply, _ := json.Marshal(map[string]any{"id": request["id"], "result": map[string]any{"thread": map[string]any{"id": "fork"}}})
	runtime.ObserveServer(reply)
	logged := false
	if disconnectRuntimeSideSessions(runtime, make(chan []byte), 0, func(string, ...any) { logged = true }) {
		t.Fatal("blocked writer accepted cleanup")
	}
	if !logged {
		t.Fatal("cleanup timeout was not reported")
	}
	if _, err := runtime.TranslateCommand(agentproto.Command{Kind: agentproto.CommandPromptSend, SideSessionID: "side"}); err == nil {
		t.Fatal("timed out cleanup restored side routing")
	}
}
