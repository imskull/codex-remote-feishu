package wrapper

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/claude"
	"github.com/kxn/codex-remote-feishu/internal/adapter/relayws"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestClaudeSideLaunchInheritsModelAndPermissionsWithoutReviewerRestrictions(t *testing.T) {
	app := &App{config: Config{WorkspaceRoot: t.TempDir()}}
	args, _ := app.buildClaudeChildLaunch(&claudeLaunchResumeTarget{ThreadID: "parent", ForkEphemeral: true, SideThreadID: "fork", Model: "inherited-model", PermissionMode: "acceptEdits"})
	assertArgPair(t, args, "--resume", "parent")
	assertArgPair(t, args, "--session-id", "fork")
	assertArgPair(t, args, "--model", "inherited-model")
	assertArgPair(t, args, "--permission-mode", "acceptEdits")
	assertContainsArg(t, args, "--fork-session")
	assertNotContainsArg(t, args, "--agent")
	assertNotContainsArg(t, args, "--disallowedTools")
}

func TestClaudeSideSessionRetainsTranslatorAndCannotRetargetMain(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runtime := &claudeBackendRuntime{translator: claude.NewTranslator("side"), workspaceRoot: t.TempDir()}
	runtime.translator.PrepareForChildLaunch("fork")
	session := &claudeSideSession{ctx: ctx, cancel: cancel, threadID: "fork", runtime: runtime, writeCh: make(chan []byte, 16), responses: newCommandResponseTracker()}
	manager := newClaudeSideSessions()
	manager.sessions["overlay"] = session
	app := &App{}
	for _, text := range []string{"first", "followup"} {
		err := manager.handle(ctx, app, agentproto.Command{Kind: agentproto.CommandPromptSend, SideSessionID: "overlay", Target: agentproto.Target{ThreadID: "parent"}, Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: text}}}}, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
	}
	if runtime.translator.RuntimeStateSnapshot().SessionID != "fork" {
		t.Fatal("side translator retargeted parent")
	}
	if manager.sessions["overlay"] != session {
		t.Fatal("followup replaced side session")
	}
	if len(session.writeCh) < 2 {
		t.Fatal("followups were not queued on side writer")
	}
	for len(session.writeCh) > 0 {
		if frame := <-session.writeCh; strings.Contains(string(frame), "set_permission_mode") {
			t.Fatal("side followup replaced inherited native permission mode")
		}
	}
}

func TestClaudeSideApprovalsAreNamespaced(t *testing.T) {
	runtime := &claudeBackendRuntime{translator: claude.NewTranslator("side")}
	runtime.translator.PrepareForChildLaunch("fork")
	tagged := &sideSessionRuntime{backendRuntime: runtime, id: "overlay"}
	_, err := runtime.TranslateCommand(agentproto.Command{Kind: agentproto.CommandPromptSend, SideSessionID: "overlay", Target: agentproto.Target{ThreadID: "fork"}, Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "hello"}}}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = tagged.ObserveServer([]byte(`{"type":"stream_event","event":{"type":"message_start","message":{"id":"message","type":"message","role":"assistant","content":[]}}}`))
	if err != nil {
		t.Fatal(err)
	}
	result, err := tagged.ObserveServer([]byte(`{"type":"control_request","request_id":"native-request","request":{"subtype":"can_use_tool","tool_name":"Bash","input":{"command":"echo test"},"tool_use_id":"tool"}}`))
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, event := range result.Events {
		if event.Kind == agentproto.EventRequestStarted {
			found = true
			if event.RequestID != "side/overlay/native-request" || event.SideSessionID != "overlay" {
				t.Fatalf("missing session identity: %#v", event)
			}
		}
	}
	if !found {
		t.Fatalf("missing approval: %#v", result.Events)
	}
}

func TestClaudeSideQuitCancelsStartupAndIsIdempotent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	manager := newClaudeSideSessions()
	manager.sessions["overlay"] = &claudeSideSession{ctx: ctx, cancel: cancel}
	client := relayws.NewClient("", agentproto.Hello{}, relayws.ClientCallbacks{})
	for i := 0; i < 2; i++ {
		if err := manager.handle(context.Background(), &App{}, agentproto.Command{Kind: agentproto.CommandSideSessionClose, SideSessionID: "overlay"}, client, nil); err != nil {
			t.Fatal(err)
		}
	}
	if ctx.Err() == nil || len(manager.sessions) != 0 {
		t.Fatal("quit retained side routing or startup")
	}
}

func TestClaudeSideDisconnectCancelsOnlyOwnedSessions(t *testing.T) {
	manager := newClaudeSideSessions()
	main, mainCancel := context.WithCancel(context.Background())
	defer mainCancel()
	for _, id := range []string{"one", "two"} {
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { <-ctx.Done(); close(done) }()
		manager.sessions[id] = &claudeSideSession{ctx: ctx, cancel: cancel, done: done}
	}
	start := time.Now()
	manager.closeAll()
	if len(manager.sessions) != 0 || main.Err() != nil || time.Since(start) > time.Second {
		t.Fatal("disconnect cleanup disturbed primary or failed to finish")
	}
}

func TestClaudeSideRejectsMissingOrClosedIdentity(t *testing.T) {
	manager := newClaudeSideSessions()
	for _, id := range []string{"", "closed"} {
		err := manager.handle(context.Background(), &App{}, agentproto.Command{Kind: agentproto.CommandPromptSend, SideSessionID: id}, nil, nil)
		if err == nil || !strings.Contains(err.Error(), "session") {
			t.Fatalf("expected closed session rejection: %v", err)
		}
	}
}
