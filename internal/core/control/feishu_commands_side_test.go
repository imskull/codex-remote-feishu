package control

import (
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"testing"
)

func TestParseSideCommandKeepsMessageAndQuit(t *testing.T) {
	for _, text := range []string{"/side", "/side explain this", "/side quit", "/SIDE another message"} {
		action, ok := ParseFeishuTextActionWithoutCatalog(text)
		if !ok || action.Kind != ActionSideCommand || action.CommandID != FeishuCommandSide || action.Text != text {
			t.Fatalf("%q: %#v, %v", text, action, ok)
		}
	}
	if _, ok := ParseFeishuTextActionWithoutCatalog("/sideways"); ok {
		t.Fatal("must not claim unrelated slash commands")
	}
}

func TestSideBackendSupport(t *testing.T) {
	for _, backend := range []agentproto.Backend{agentproto.BackendCodex, agentproto.BackendClaude, agentproto.BackendOpenCode} {
		support, ok := ResolveFeishuCommandSupport(CatalogContext{Backend: backend, ProductMode: "normal"}, FeishuCommandSide)
		want := backend != agentproto.BackendOpenCode
		if !ok || support.Visible != want || support.DispatchAllowed != want {
			t.Fatalf("%s: %#v", backend, support)
		}
	}
}
