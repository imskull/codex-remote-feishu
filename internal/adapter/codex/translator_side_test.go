package codex

import (
	"encoding/json"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func sideDecode(t *testing.T, frames [][]byte) map[string]any {
	t.Helper()
	if len(frames) == 0 {
		t.Fatal("missing native frame")
	}
	var message map[string]any
	if err := json.Unmarshal(frames[len(frames)-1], &message); err != nil {
		t.Fatal(err)
	}
	return message
}

func sideReply(t *testing.T, tr *Translator, request map[string]any, result map[string]any) Result {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"id": request["id"], "result": result})
	got, err := tr.ObserveServer(raw)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func sideOpen(t *testing.T, tr *Translator, id string) map[string]any {
	t.Helper()
	if tr.observedThreadConfig["parent"] == nil {
		tr.observedThreadConfig["parent"] = map[string]any{"approvalPolicy": "never", "sandbox": map[string]any{"type": "readOnly"}}
	}
	frames, err := tr.TranslateCommand(agentproto.Command{Kind: agentproto.CommandSideSessionOpen, CommandID: "open-" + id, SideSessionID: id, Target: agentproto.Target{ThreadID: "parent"}})
	if err != nil {
		t.Fatal(err)
	}
	return sideDecode(t, frames)
}

func TestSideForkPreservesParentAndPersistentFollowups(t *testing.T) {
	tr := NewTranslator("instance")
	tr.currentThreadID = "parent"
	tr.activeTurnByThread["parent"] = "main-turn"
	tr.turnStartByThread["parent"] = map[string]any{"model": "parent-model", "approvalPolicy": "on-request", "sandboxPolicy": map[string]any{"type": "workspaceWrite"}}
	request := sideOpen(t, tr, "side")
	params := request["params"].(map[string]any)
	if request["method"] != "thread/fork" || params["threadId"] != "parent" || params["beforeTurnId"] != "main-turn" {
		t.Fatalf("bad fork: %#v", request)
	}
	tr.turnStartByThread["parent"]["model"] = "later-model"
	opened := sideReply(t, tr, request, map[string]any{"thread": map[string]any{"id": "fork"}})
	if len(opened.Events) != 1 || opened.Events[0].SideSessionID != "side" || opened.Events[0].Status != "open" || opened.Events[0].CommandID != "open-side" {
		t.Fatalf("bad open: %#v", opened)
	}
	started, err := tr.ObserveServer([]byte(`{"method":"thread/started","params":{"thread":{"id":"fork"}}}`))
	if err != nil || !started.Suppress || tr.currentThreadID != "parent" {
		t.Fatalf("fork changed focus: %#v %v", started, err)
	}
	for _, text := range []string{"first question", "follow up"} {
		frames, err := tr.TranslateCommand(agentproto.Command{Kind: agentproto.CommandPromptSend, SideSessionID: "side", Target: agentproto.Target{ThreadID: "fork"}, Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: text}}}})
		if err != nil {
			t.Fatal(err)
		}
		message := sideDecode(t, frames)
		params := message["params"].(map[string]any)
		if message["method"] != "turn/start" || params["threadId"] != "fork" || params["model"] != "parent-model" || params["approvalPolicy"] != "on-request" {
			t.Fatalf("bad followup %#v", message)
		}
	}
	if tr.currentThreadID != "parent" || tr.activeTurnByThread["parent"] != "main-turn" {
		t.Fatal("main execution changed")
	}
}

func TestSidePromptResponseCorrelatesRejectionAndLateStart(t *testing.T) {
	for _, scenario := range []string{"rejected", "missing_turn", "closed"} {
		t.Run(scenario, func(t *testing.T) {
			tr := NewTranslator("instance")
			sideReply(t, tr, sideOpen(t, tr, "side"), map[string]any{"thread": map[string]any{"id": "fork"}})
			frames, err := tr.TranslateCommand(agentproto.Command{Kind: agentproto.CommandPromptSend,
				SideSessionID: "side", CommandID: "side-prompt", Target: agentproto.Target{ThreadID: "fork"}})
			if err != nil {
				t.Fatal(err)
			}
			request := sideDecode(t, frames)
			message := map[string]any{"id": request["id"], "result": map[string]any{}}
			if scenario == "rejected" {
				message["error"] = map[string]any{"code": -32602, "message": "permission denied"}
			}
			if scenario == "closed" {
				if _, err := tr.TranslateCommand(agentproto.Command{Kind: agentproto.CommandSideSessionClose, SideSessionID: "side"}); err != nil {
					t.Fatal(err)
				}
				message["result"] = map[string]any{"turn": map[string]any{"id": "late-turn"}}
			}
			raw, _ := json.Marshal(message)
			result, err := tr.ObserveServer(raw)
			if err != nil {
				t.Fatal(err)
			}
			if _, exists := tr.pendingSuppressedResponse[request["id"].(string)]; exists {
				t.Fatal("side response leaked normal response gate")
			}
			if !result.Suppress {
				t.Fatal("side response escaped to primary client")
			}
			if scenario == "closed" {
				if len(result.Events) != 0 || len(result.OutboundToCodex) != 2 {
					t.Fatalf("late start was not disposed: %#v", result)
				}
				params := sideDecode(t, result.OutboundToCodex[:1])["params"].(map[string]any)
				if params["threadId"] != "fork" || params["turnId"] != "late-turn" {
					t.Fatal("cleanup lost response turn identity")
				}
				return
			}
			if len(result.Events) != 1 {
				t.Fatalf("missing rejection: %#v", result)
			}
			event := result.Events[0]
			if event.Kind != agentproto.EventTurnCompleted || event.CommandID != "side-prompt" || event.SideSessionID != "side" || event.ThreadID != "fork" || event.Status != "failed" || event.ErrorMessage == "" {
				t.Fatalf("uncorrelated rejection: %#v", event)
			}
		})
	}
}

func TestSideCloseDuringStartupDisposesLateFork(t *testing.T) {
	tr := NewTranslator("instance")
	request := sideOpen(t, tr, "side")
	frames, err := tr.TranslateCommand(agentproto.Command{Kind: agentproto.CommandSideSessionClose, SideSessionID: "side", CommandID: "quit"})
	if err != nil {
		t.Fatal(err)
	}
	closed := sideReply(t, tr, sideDecode(t, frames), map[string]any{})
	if len(closed.Events) != 1 || closed.Events[0].Status != "closed" || closed.Events[0].CommandID != "quit" {
		t.Fatalf("bad close %#v", closed)
	}
	late := sideReply(t, tr, request, map[string]any{"thread": map[string]any{"id": "late-fork"}})
	if len(late.Events) != 0 || sideDecode(t, late.OutboundToCodex)["method"] != "thread/unsubscribe" {
		t.Fatalf("late fork leaked %#v", late)
	}
	if _, err := tr.TranslateCommand(agentproto.Command{Kind: agentproto.CommandPromptSend, SideSessionID: "side"}); err == nil {
		t.Fatal("closed session accepted prompt")
	}
	lateEvent, err := tr.ObserveServer([]byte(`{"method":"turn/started","params":{"threadId":"late-fork","turn":{"id":"late-turn"}}}`))
	if err != nil || !lateEvent.Suppress || lateEvent.Events[0].SideSessionID != "side" {
		t.Fatalf("late event untagged %#v", lateEvent)
	}
}

func TestSideForkFailureAndIndependentInterrupt(t *testing.T) {
	tr := NewTranslator("instance")
	request := sideOpen(t, tr, "failed")
	raw, _ := json.Marshal(map[string]any{"id": request["id"], "error": map[string]any{"message": "unsupported"}})
	failed, err := tr.ObserveServer(raw)
	if err != nil || failed.Events[0].Status != "failed" || failed.Events[0].ErrorMessage != "unsupported" {
		t.Fatalf("bad failure %#v %v", failed, err)
	}
	request = sideOpen(t, tr, "side")
	sideReply(t, tr, request, map[string]any{"thread": map[string]any{"id": "fork"}})
	tr.activeTurnByThread["parent"] = "main-turn"
	tr.activeTurnByThread["fork"] = "side-turn"
	frames, err := tr.TranslateCommand(agentproto.Command{Kind: agentproto.CommandTurnInterrupt, SideSessionID: "side", Target: agentproto.Target{ThreadID: "fork"}})
	if err != nil {
		t.Fatal(err)
	}
	params := sideDecode(t, frames)["params"].(map[string]any)
	if params["threadId"] != "fork" || params["turnId"] != "side-turn" || tr.activeTurnByThread["parent"] != "main-turn" {
		t.Fatalf("wrong cancellation %#v", params)
	}
}

func TestSideApprovalsKeepExplicitSessionOwnership(t *testing.T) {
	tr := NewTranslator("instance")
	sideReply(t, tr, sideOpen(t, tr, "side"), map[string]any{"thread": map[string]any{"id": "fork"}})
	approval, err := tr.ObserveServer([]byte(`{"id":"side-approval","method":"item/commandExecution/requestApproval","params":{"threadId":"fork","turnId":"side-turn","itemId":"side-command","command":"echo test","availableDecisions":["accept","decline"]}}`))
	if err != nil || !approval.Suppress || len(approval.Events) != 1 || approval.Events[0].SideSessionID != "side" {
		t.Fatalf("bad side approval %#v %v", approval, err)
	}
	_, err = tr.TranslateCommand(agentproto.Command{Kind: agentproto.CommandRequestRespond, SideSessionID: "side", Request: agentproto.Request{RequestID: "main-approval"}})
	if err == nil {
		t.Fatal("side response accepted a main request")
	}
	_, err = tr.TranslateCommand(agentproto.Command{Kind: agentproto.CommandRequestRespond, SideSessionID: "side", Request: agentproto.Request{RequestID: "side-approval", Response: map[string]any{"type": "approval", "approved": true}}})
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := tr.ObserveServer([]byte(`{"method":"serverRequest/resolved","params":{"requestId":"side-approval"}}`))
	if err != nil || !resolved.Suppress || len(resolved.Events) != 1 || resolved.Events[0].SideSessionID != "side" {
		t.Fatalf("bad request-only resolution %#v %v", resolved, err)
	}
}

func TestSideCloseCancelsLateTurnStart(t *testing.T) {
	tr := NewTranslator("instance")
	sideReply(t, tr, sideOpen(t, tr, "side"), map[string]any{"thread": map[string]any{"id": "fork"}})
	_, err := tr.TranslateCommand(agentproto.Command{Kind: agentproto.CommandSideSessionClose, SideSessionID: "side"})
	if err != nil {
		t.Fatal(err)
	}
	late, err := tr.ObserveServer([]byte(`{"method":"turn/started","params":{"threadId":"fork","turn":{"id":"late-turn"}}}`))
	if err != nil || len(late.OutboundToCodex) != 2 || !late.Suppress {
		t.Fatalf("late turn not disposed %#v %v", late, err)
	}
	var interrupt map[string]any
	if err := json.Unmarshal(late.OutboundToCodex[0], &interrupt); err != nil {
		t.Fatal(err)
	}
	if interrupt["method"] != "turn/interrupt" || interrupt["params"].(map[string]any)["turnId"] != "late-turn" {
		t.Fatalf("wrong late interrupt %#v", interrupt)
	}
}

func TestSideForkInheritsObservedEffectivePermissions(t *testing.T) {
	tr := NewTranslator("instance")
	_, err := tr.ObserveServer([]byte(`{"id":"native-start","result":{"thread":{"id":"parent"},"approvalPolicy":"on-request","sandbox":{"type":"workspaceWrite","writableRoots":["D:/scratch"],"networkAccess":false,"excludeTmpdirEnvVar":true,"excludeSlashTmp":true},"model":"parent-model","reasoningEffort":"high","cwd":"D:/scratch"}}`))
	if err != nil {
		t.Fatal(err)
	}
	request := sideOpen(t, tr, "side")
	params := request["params"].(map[string]any)
	config := params["config"].(map[string]any)
	if params["approvalPolicy"] != "on-request" || params["sandbox"] != "workspace-write" || params["model"] != "parent-model" || config["model_reasoning_effort"] != "high" {
		t.Fatalf("effective contract not inherited %#v", params)
	}
	workspace := config["sandbox_workspace_write"].(map[string]any)
	if workspace["network_access"] != false || workspace["exclude_tmpdir_env_var"] != true || workspace["exclude_slash_tmp"] != true {
		t.Fatalf("sandbox boundary lost %#v", workspace)
	}
}

func TestSideForkRejectsUnknownOrUnsupportedParentPermissions(t *testing.T) {
	tr := NewTranslator("instance")
	command := agentproto.Command{Kind: agentproto.CommandSideSessionOpen, SideSessionID: "side", Target: agentproto.Target{ThreadID: "parent"}}
	if _, err := tr.TranslateCommand(command); err == nil {
		t.Fatal("unknown permissions accepted")
	}
	tr.observedThreadConfig["parent"] = map[string]any{"approvalPolicy": "never", "sandbox": map[string]any{"type": "externalSandbox"}}
	if _, err := tr.TranslateCommand(command); err == nil {
		t.Fatal("unrepresentable sandbox accepted")
	}
	if tr.sideSessions["side"] != nil {
		t.Fatal("rejected open leaked lifecycle state")
	}
}

func TestSideNeverInheritsAnotherThreadsGlobalTemplate(t *testing.T) {
	tr := NewTranslator("instance")
	tr.latestTurnStartTemplate = map[string]any{"model": "unrelated-model", "approvalPolicy": "never", "sandboxPolicy": map[string]any{"type": "dangerFullAccess"}}
	tr.observedThreadConfig["parent"] = map[string]any{"model": "parent-model", "approvalPolicy": "on-request", "sandbox": map[string]any{"type": "readOnly"}}
	request := sideOpen(t, tr, "side")
	params := request["params"].(map[string]any)
	if params["sandbox"] != "read-only" || params["approvalPolicy"] != "on-request" || params["model"] != "parent-model" {
		t.Fatalf("global template leaked into fork %#v", params)
	}
	sideReply(t, tr, request, map[string]any{"thread": map[string]any{"id": "fork"}})
	tr.latestTurnStartTemplate["model"] = "another-later-model"
	frames, err := tr.TranslateCommand(agentproto.Command{Kind: agentproto.CommandPromptSend, SideSessionID: "side"})
	if err != nil {
		t.Fatal(err)
	}
	params = sideDecode(t, frames)["params"].(map[string]any)
	if params["model"] != nil || params["approvalPolicy"] != nil || params["sandboxPolicy"] != nil {
		t.Fatalf("global template leaked into followup %#v", params)
	}
}

func TestSideDisconnectCancelsOnlySidesAndDisposesPendingFork(t *testing.T) {
	tr := NewTranslator("instance")
	sideReply(t, tr, sideOpen(t, tr, "active"), map[string]any{"thread": map[string]any{"id": "fork"}})
	pending := sideOpen(t, tr, "pending")
	tr.currentThreadID = "parent"
	tr.activeTurnByThread["parent"] = "main-turn"
	tr.activeTurnByThread["fork"] = "side-turn"
	frames := tr.DisconnectSideSessions()
	if len(frames) != 2 {
		t.Fatalf("unexpected cleanup frames %d", len(frames))
	}
	for _, frame := range frames {
		var message map[string]any
		if err := json.Unmarshal(frame, &message); err != nil {
			t.Fatal(err)
		}
		if message["params"].(map[string]any)["threadId"] != "fork" {
			t.Fatalf("cleanup targeted main %#v", message)
		}
	}
	if tr.currentThreadID != "parent" || tr.activeTurnByThread["parent"] != "main-turn" {
		t.Fatal("disconnect changed main execution")
	}
	if len(tr.DisconnectSideSessions()) != 0 {
		t.Fatal("repeated disconnect generated duplicate cleanup")
	}
	late := sideReply(t, tr, pending, map[string]any{"thread": map[string]any{"id": "late-fork"}})
	if len(late.Events) != 0 || sideDecode(t, late.OutboundToCodex)["method"] != "thread/unsubscribe" {
		t.Fatalf("late disconnected fork leaked %#v", late)
	}
}
