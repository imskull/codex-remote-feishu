package feishu

import (
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/frontstagecontract"
)

func projectSideReply(chatID string, event eventcontract.Event, notice control.Notice) []Operation {
	quit := sideQuitElement(notice.SideSessionID, event.DaemonLifecycleID, event.SurfaceSessionID)
	ops := projectFinalReplyCards(event.GatewayID, event.SurfaceSessionID, chatID, replyToMessageIDForEvent(event), "Side mode", "", notice.Text, []map[string]any{quit})
	for i := range ops {
		if i > 0 {
			ops[i].CardTitle = "Side mode (continued)"
			ops[i].card = finalReplyCardDocument(ops[i].CardTitle, ops[i].CardSubtitle, ops[i].CardBody, cardThemeFinal, ops[i].CardElements)
		}
		ops[i] = applyReplyLaneToNewOperation(event, ops[i])
	}
	return ops
}

func sideQuitElement(sessionID, lifecycleID, surfaceID string) map[string]any {
	return map[string]any{
		"tag": "action",
		"actions": []map[string]any{{
			"tag": "button", "type": "default",
			"text": map[string]any{"tag": "plain_text", "content": "退出 Side mode"},
			"value": map[string]any{
				frontstagecontract.CardActionPayloadKeyKind:              frontstagecontract.CardActionKindSideQuit,
				frontstagecontract.CardActionPayloadKeySideSessionID:     sessionID,
				frontstagecontract.CardActionPayloadKeyDaemonLifecycleID: lifecycleID,
				frontstagecontract.CardActionPayloadKeySurfaceSessionID:  surfaceID,
			},
		}},
	}
}
