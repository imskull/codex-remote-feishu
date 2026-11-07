package control

import "testing"

func TestWorkspacePathCommandPreservesPathAndCatalogProvenance(t *testing.T) {
	for _, input := range []string{
		`/workspace d:/stock/stock-agent`,
		`/workspace D:\stock\stock-agent`,
		`/workspace "D:/stock/my agent"`,
		`/workspace D:/stock/my agent`,
		`/workspace /srv/stock-agent`,
	} {
		t.Run(input, func(t *testing.T) {
			action, ok := ParseFeishuTextActionWithoutCatalog(input)
			if !ok || action.Kind != ActionWorkspaceRoot || action.CommandID != FeishuCommandWorkspace || action.Text != input {
				t.Fatalf("parsed path command = %#v, ok=%v", action, ok)
			}
			resolved, ok := ResolveFeishuTextCommand(CatalogContext{}, input)
			if !ok || resolved.FamilyID != FeishuCommandWorkspace || resolved.Action.Text != input {
				t.Fatalf("resolved path command = %#v, ok=%v", resolved, ok)
			}
			binding, ok := ResolveFeishuCommandBindingFromAction(resolved.Action)
			if !ok {
				t.Fatal("workspace path command has no binding")
			}
			if intent, ok := binding.IntentFromAction(resolved.Action); ok {
				t.Fatalf("path command must execute rather than open the root page: %#v", intent)
			}
		})
	}
}

func TestWorkspacePathCommandPreservesExistingSubcommands(t *testing.T) {
	for _, tt := range []struct {
		text string
		kind ActionKind
	}{
		{"/workspace", ActionWorkspaceRoot},
		{"/workspace list", ActionWorkspaceList},
		{"/workspace new", ActionWorkspaceNew},
		{"/workspace new dir", ActionWorkspaceNewDir},
		{"/workspace new git", ActionWorkspaceNewGit},
		{"/workspace new worktree", ActionWorkspaceNewWorktree},
		{"/workspace detach", ActionWorkspaceDetach},
	} {
		action, ok := ParseFeishuTextActionWithoutCatalog(tt.text)
		if !ok || action.Kind != tt.kind {
			t.Fatalf("%q parsed as %#v, ok=%v", tt.text, action, ok)
		}
	}
}
