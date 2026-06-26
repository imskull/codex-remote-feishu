package feishu

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func TestHandleCardActionTriggerTimesOutInlineReplacementAndAppliesLateResult(t *testing.T) {
	action := control.Action{
		Kind:             control.ActionShowCommandMenu,
		GatewayID:        "app-1",
		SurfaceSessionID: "feishu:app-1:chat:oc_1",
		ChatID:           "oc_1",
		MessageID:        "om-card-1",
		Inbound: &control.ActionInboundMeta{
			OpenMessageID:         "om-card-1",
			CardDaemonLifecycleID: "life-1",
		},
	}
	started := make(chan struct{})
	release := make(chan struct{})
	type lateCall struct {
		messageID string
		result    *ActionResult
	}
	late := make(chan lateCall, 1)
	handler := func(context.Context, control.Action) *ActionResult {
		close(started)
		<-release
		return &ActionResult{
			ReplaceCurrentCard: &Operation{
				Kind:         OperationSendCard,
				CardTitle:    "命令菜单",
				CardBody:     "已切到发送设置。",
				CardThemeKey: cardThemeInfo,
			},
		}
	}

	begin := time.Now()
	resp, err := handleCardActionTriggerWithOptions(context.Background(), action, handler, cardActionTriggerOptions{
		syncTimeout: 20 * time.Millisecond,
		lateResult: func(_ context.Context, gotAction control.Action, result *ActionResult) error {
			late <- lateCall{messageID: gotAction.MessageID, result: result}
			return nil
		},
	})
	if err != nil {
		t.Fatalf("handleCardActionTrigger returned error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected empty callback response")
	}
	if resp.Card != nil {
		t.Fatalf("expected timeout path to ack without callback replacement, got %#v", resp)
	}
	if elapsed := time.Since(begin); elapsed > 200*time.Millisecond {
		t.Fatalf("expected callback ack before Feishu timeout budget, took %s", elapsed)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("expected handler to start")
	}
	close(release)
	select {
	case call := <-late:
		if call.messageID != "om-card-1" {
			t.Fatalf("late action message id = %q, want om-card-1", call.messageID)
		}
		result := call.result
		if result == nil || result.ReplaceCurrentCard == nil || result.ReplaceCurrentCard.CardTitle != "命令菜单" {
			t.Fatalf("unexpected late result: %#v", result)
		}
	case <-time.After(time.Second):
		t.Fatal("expected late replacement result after handler finished")
	}
}

func TestHandleCardActionTriggerLateReplacementPatchesOriginalMessage(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
	type patchCall struct {
		messageID string
		content   string
	}
	patched := make(chan patchCall, 1)
	gateway.patchMessageFn = func(_ context.Context, messageID, content string) (*larkim.PatchMessageResp, error) {
		patched <- patchCall{messageID: messageID, content: content}
		return &larkim.PatchMessageResp{
			ApiResp: &larkcore.ApiResp{},
			CodeError: larkcore.CodeError{
				Code: 0,
				Msg:  "ok",
			},
		}, nil
	}
	action := control.Action{
		Kind:             control.ActionShowCommandMenu,
		GatewayID:        "app-1",
		SurfaceSessionID: "feishu:app-1:chat:oc_1",
		ChatID:           "oc_1",
		MessageID:        "om-card-1",
		Inbound: &control.ActionInboundMeta{
			OpenMessageID:         "om-card-1",
			CardDaemonLifecycleID: "life-1",
		},
	}
	release := make(chan struct{})
	handler := func(context.Context, control.Action) *ActionResult {
		<-release
		return &ActionResult{
			ReplaceCurrentCard: &Operation{
				Kind:         OperationSendCard,
				CardTitle:    "命令菜单",
				CardBody:     "已切到发送设置。",
				CardThemeKey: cardThemeInfo,
			},
		}
	}

	resp, err := handleCardActionTriggerWithOptions(context.Background(), action, handler, cardActionTriggerOptions{
		syncTimeout: 20 * time.Millisecond,
		lateResult:  gateway.applyLateCardActionReplacement,
	})
	if err != nil {
		t.Fatalf("handleCardActionTrigger returned error: %v", err)
	}
	if resp == nil || resp.Card != nil {
		t.Fatalf("expected timeout ack without inline card, got %#v", resp)
	}
	close(release)

	select {
	case call := <-patched:
		if call.messageID != "om-card-1" {
			t.Fatalf("patched message id = %q, want om-card-1", call.messageID)
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(call.content), &payload); err != nil {
			t.Fatalf("patched content is not valid json: %v", err)
		}
		config, _ := payload["config"].(map[string]any)
		if config["update_multi"] != true {
			t.Fatalf("expected late patch to keep update_multi=true, got %#v", payload)
		}
		header := payload["header"].(map[string]any)
		title := header["title"].(map[string]any)
		if title["content"] != "命令菜单" {
			t.Fatalf("unexpected patched card title payload: %#v", payload)
		}
	case <-time.After(time.Second):
		t.Fatal("expected late replacement to patch original card")
	}
}

func TestApplyLateCardActionReplacementFallsBackToReplyWhenPatchFails(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
	patchCalls := 0
	gateway.patchMessageFn = func(_ context.Context, messageID, _ string) (*larkim.PatchMessageResp, error) {
		patchCalls++
		if messageID != "om-card-1" {
			t.Fatalf("patched message id = %q, want om-card-1", messageID)
		}
		return &larkim.PatchMessageResp{
			ApiResp: &larkcore.ApiResp{},
			CodeError: larkcore.CodeError{
				Code: 999,
				Msg:  "patch disabled",
			},
		}, nil
	}
	var (
		replyMessageID string
		replyMsgType   string
		replyContent   string
	)
	gateway.replyMessageFn = func(_ context.Context, messageID, msgType, content string) (*larkim.ReplyMessageResp, error) {
		replyMessageID = messageID
		replyMsgType = msgType
		replyContent = content
		return &larkim.ReplyMessageResp{
			ApiResp: &larkcore.ApiResp{},
			CodeError: larkcore.CodeError{
				Code: 0,
				Msg:  "ok",
			},
			Data: &larkim.ReplyMessageRespData{
				MessageId: stringRef("om-fallback-1"),
			},
		}, nil
	}

	err := gateway.applyLateCardActionReplacement(context.Background(), control.Action{
		Kind:             control.ActionShowCommandMenu,
		GatewayID:        "app-1",
		SurfaceSessionID: "feishu:app-1:chat:oc_1",
		ChatID:           "oc_1",
		MessageID:        "om-card-1",
		Inbound: &control.ActionInboundMeta{
			OpenMessageID:         "om-card-1",
			CardDaemonLifecycleID: "life-1",
		},
	}, &ActionResult{
		ReplaceCurrentCard: &Operation{
			Kind:         OperationSendCard,
			CardTitle:    "命令菜单",
			CardBody:     "已切到发送设置。",
			CardThemeKey: cardThemeInfo,
		},
	})
	if err != nil {
		t.Fatalf("applyLateCardActionReplacement returned error: %v", err)
	}
	if patchCalls != 1 {
		t.Fatalf("patch calls = %d, want 1", patchCalls)
	}
	if replyMessageID != "om-card-1" || replyMsgType != "interactive" {
		t.Fatalf("unexpected fallback reply request: message=%q type=%q", replyMessageID, replyMsgType)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(replyContent), &payload); err != nil {
		t.Fatalf("fallback reply content is not valid json: %v", err)
	}
	config, _ := payload["config"].(map[string]any)
	if config["update_multi"] != true {
		t.Fatalf("expected fallback reply card to be patchable, got %#v", payload)
	}
	if gateway.messages["om-fallback-1"] != "feishu:app-1:chat:oc_1" {
		t.Fatalf("expected fallback reply to be tracked, got %#v", gateway.messages)
	}
}
