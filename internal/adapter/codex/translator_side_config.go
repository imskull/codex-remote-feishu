package codex

import (
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/xutil"
)

func validateSideThreadConfig(observed, turn map[string]any) error {
	approval := observed["approvalPolicy"]
	if value := turn["approvalPolicy"]; value != nil {
		approval = value
	}
	sandbox, _ := observed["sandbox"].(map[string]any)
	if value, ok := turn["sandboxPolicy"].(map[string]any); ok {
		sandbox = value
	}
	if approval == nil || sandbox == nil {
		return fmt.Errorf("side session requires observed parent permissions; resume the parent conversation first")
	}
	switch sandbox["type"] {
	case "readOnly", "workspaceWrite", "dangerFullAccess":
		return nil
	default:
		return fmt.Errorf("side session cannot preserve parent sandbox type %v", sandbox["type"])
	}
}

// Native thread/fork takes its permission defaults from the server, rather than
// the parent thread. Carry the observed contract explicitly to avoid escalation.
func inheritSideThreadConfig(params, observed, turn map[string]any) {
	for _, key := range []string{"approvalPolicy", "approvalsReviewer", "model", "modelProvider", "cwd", "serviceTier", "runtimeWorkspaceRoots"} {
		if value := observed[key]; value != nil {
			params[key] = value
		}
	}
	for _, key := range []string{"approvalPolicy", "model"} {
		if value := turn[key]; value != nil {
			params[key] = value
		}
	}
	sandbox, _ := observed["sandbox"].(map[string]any)
	if value, ok := turn["sandboxPolicy"].(map[string]any); ok {
		sandbox = value
	}
	config := lookupMapFromAny(params["config"])
	if effort := xutil.LookupStringFromAny(observed["reasoningEffort"]); effort != "" {
		config["model_reasoning_effort"] = effort
	}
	if effort := xutil.LookupStringFromAny(turn["effort"]); effort != "" {
		config["model_reasoning_effort"] = effort
	}
	switch sandbox["type"] {
	case "readOnly":
		params["sandbox"] = "read-only"
	case "dangerFullAccess":
		params["sandbox"] = "danger-full-access"
	case "workspaceWrite":
		params["sandbox"] = "workspace-write"
		workspace := map[string]any{}
		for native, key := range map[string]string{"writableRoots": "writable_roots", "networkAccess": "network_access", "excludeTmpdirEnvVar": "exclude_tmpdir_env_var", "excludeSlashTmp": "exclude_slash_tmp"} {
			if value := sandbox[native]; value != nil {
				workspace[key] = value
			}
		}
		config["sandbox_workspace_write"] = workspace
	}
	if len(config) > 0 {
		params["config"] = config
	}
}
