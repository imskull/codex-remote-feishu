package orchestrator

import (
	"encoding/json"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/adapter/claude"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func claudeGoalTestSetup(t *testing.T) (*Service, *state.SurfaceConsoleRecord) {
	t.Helper()
	now := nowForTest()
	svc := newServiceForTest(&now)
	surface := setupQueuedStartNoticeSurface(t, svc)
	surface.Backend = agentproto.BackendClaude
	svc.root.Instances["inst-1"].Backend = agentproto.BackendClaude
	return svc, surface
}

func claudeGoalAction(surface *state.SurfaceConsoleRecord, text string) control.Action {
	return control.Action{Kind: control.ActionGoalCommand, SurfaceSessionID: surface.SurfaceSessionID,
		ChatID: surface.ChatID, ActorUserID: surface.ActorUserID, MessageID: "goal-message", Text: text}
}

func TestClaudeGoalRoutesNativeSyntaxThroughPromptQueue(t *testing.T) {
	for _, text := range []string{"/goal", "/goal all tests pass\nand lint is clean", "/goal clear", "/goal cancel", "/goal output includes [耸肩摊手]", "/goal output includes [什么？] and [耸肩摊手]", ""} {
		t.Run(text, func(t *testing.T) {
			svc, surface := claudeGoalTestSetup(t)
			action := claudeGoalAction(surface, text)
			if text != "" {
				parsed, ok := control.ResolveFeishuTextCommand(control.CatalogContext{Backend: agentproto.BackendClaude, ProductMode: "normal"}, text)
				if !ok {
					t.Fatalf("Claude goal was not resolved: %q", text)
				}
				action.Kind = parsed.Action.Kind
				action.Text = parsed.Action.Text
			}
			events := svc.ApplySurfaceAction(action)
			command := findPromptSendCommand(events)
			if command == nil || command.Target.ThreadID != "thread-1" {
				t.Fatalf("expected a prompt to the selected Claude thread, got %#v", events)
			}
			want := text
			if want == "" {
				want = "/goal"
			}
			if len(command.Prompt.Inputs) != 1 || command.Prompt.Inputs[0].Type != agentproto.InputText || command.Prompt.Inputs[0].Text != want {
				t.Fatalf("native goal syntax changed: %#v", command.Prompt.Inputs)
			}
			if findAgentCommand(events, agentproto.CommandThreadGoalGet) != nil || findAgentCommand(events, agentproto.CommandThreadGoalSet) != nil {
				t.Fatal("Claude must not use Codex goal RPCs")
			}
		})
	}
}

func TestClaudeGoalCanStartPreparedNewThread(t *testing.T) {
	svc, surface := claudeGoalTestSetup(t)
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionNewThread, SurfaceSessionID: surface.SurfaceSessionID,
		ChatID: surface.ChatID, ActorUserID: surface.ActorUserID})
	events := svc.ApplySurfaceAction(claudeGoalAction(surface, "/goal tests pass"))
	command := findPromptSendCommand(events)
	if command == nil || command.Target.ThreadID != "" || len(command.Prompt.Inputs) != 1 || command.Prompt.Inputs[0].Text != "/goal tests pass" {
		t.Fatalf("expected goal as the new session's first input, got %#v", events)
	}
}

func TestClaudeGoalReplayedInputPreservesLiteralDetourMarkers(t *testing.T) {
	svc, surface := claudeGoalTestSetup(t)
	action := claudeGoalAction(surface, "/goal output includes [什么？] and [耸肩摊手]")
	// Pending input replay uses the ordinary text action after runtime launch.
	action.Kind = control.ActionTextMessage
	command := findPromptSendCommand(svc.ApplySurfaceAction(action))
	if command == nil || command.Target.ThreadID != "thread-1" || len(command.Prompt.Inputs) != 1 || command.Prompt.Inputs[0].Text != action.Text {
		t.Fatalf("replayed native goal must retain its condition and target: %#v", command)
	}
}

func TestClaudeGoalPreservesStagedInputsAndRequestCapture(t *testing.T) {
	for _, blocker := range []string{"image", "file", "capture", "helper"} {
		t.Run(blocker, func(t *testing.T) {
			svc, surface := claudeGoalTestSetup(t)
			switch blocker {
			case "image":
				surface.StagedImages = map[string]*state.StagedImageRecord{"image": {State: state.ImageStaged, ActorUserID: surface.ActorUserID}}
			case "file":
				surface.StagedFiles = map[string]*state.StagedFileRecord{"file": {State: state.FileStaged, ActorUserID: surface.ActorUserID, LocalPath: "reference.txt"}}
			case "capture":
				surface.ActiveRequestCapture = &state.RequestCaptureRecord{}
			case "helper":
				svc.root.Instances["inst-1"].Threads["thread-1"].TrafficClass = agentproto.TrafficClassInternalHelper
			}
			events := svc.ApplySurfaceAction(claudeGoalAction(surface, "/goal clear"))
			if findPromptSendCommand(events) != nil || len(surface.QueuedQueueItemIDs) != 0 || len(events) == 0 {
				t.Fatalf("blocked goal must return feedback without dispatching: %#v", events)
			}
			if blocker == "image" && surface.StagedImages["image"].State != state.ImageStaged {
				t.Fatal("goal consumed staged image")
			}
			if blocker == "file" && surface.StagedFiles["file"].State != state.FileStaged {
				t.Fatal("goal consumed staged file")
			}
			if blocker == "capture" && surface.ActiveRequestCapture == nil {
				t.Fatal("goal consumed request feedback")
			}
		})
	}
}

func TestClaudeNativeGoalLoopKeepsQueueUntilFinalResult(t *testing.T) {
	svc, surface := claudeGoalTestSetup(t)
	tr := claude.NewTranslator("inst-1")
	var projected []eventcontract.Event
	observe := func(frame string) []eventcontract.Event {
		t.Helper()
		result, err := tr.ObserveServer([]byte(frame))
		if err != nil {
			t.Fatal(err)
		}
		var events []eventcontract.Event
		for _, event := range result.Events {
			events = append(events, svc.ApplyAgentEvent("inst-1", event)...)
		}
		projected = append(projected, events...)
		return events
	}
	observe(`{"type":"system","subtype":"init","session_id":"thread-1","permissionMode":"default"}`)
	command := findPromptSendCommand(svc.ApplySurfaceAction(claudeGoalAction(surface, "/goal response contains GOAL_PROBE_OK")))
	if command == nil {
		t.Fatal("expected goal dispatch")
	}
	frames, err := tr.TranslateCommand(*command)
	if err != nil {
		t.Fatal(err)
	}
	var native map[string]any
	if err := json.Unmarshal(frames[len(frames)-1], &native); err != nil {
		t.Fatal(err)
	}
	if native["message"].(map[string]any)["content"] != "/goal response contains GOAL_PROBE_OK" {
		t.Fatalf("wrong native prompt: %#v", native)
	}
	// Claude 2.1.322 emits this synthetic acknowledgement without a result.
	observe(`{"type":"assistant","local_command_run":{"command":"goal","args":"response contains GOAL_PROBE_OK"},"message":{"id":"ack","model":"<synthetic>","role":"assistant","content":[{"type":"text","text":"Goal set: response contains GOAL_PROBE_OK"}]}}`)
	queued := svc.ApplySurfaceAction(control.Action{Kind: control.ActionTextMessage, SurfaceSessionID: surface.SurfaceSessionID,
		ChatID: surface.ChatID, ActorUserID: surface.ActorUserID, MessageID: "next-message", Text: "next task"})
	if findPromptSendCommand(queued) != nil {
		t.Fatal("goal acknowledgement released queued input")
	}
	// Main-model responses and evaluator retries remain inside the same runtime result.
	events := observe(`{"type":"assistant","message":{"id":"answer","role":"assistant","content":[{"type":"text","text":"GOAL_PROBE_OK"}]}}`)
	if findPromptSendCommand(events) != nil || surface.ActiveQueueItemID == "" {
		t.Fatal("assistant response released goal ownership before result")
	}
	finished := observe(`{"type":"result","subtype":"success","local_command":"goal","result":"GOAL_PROBE_OK","num_turns":1,"is_error":false}`)
	if findPromptSendCommand(finished) == nil {
		t.Fatal("final native result did not release queued input")
	}
	finals := 0
	for _, event := range projected {
		if event.Kind == eventcontract.KindBlockCommitted && event.Block != nil && event.Block.Text == "GOAL_PROBE_OK" {
			finals++
			if !event.Block.Final || event.SurfaceSessionID != surface.SurfaceSessionID {
				t.Fatalf("goal final response lacks destination: %#v", event)
			}
		}
	}
	if finals != 1 {
		t.Fatalf("expected exactly one goal final response, got %d", finals)
	}
}
