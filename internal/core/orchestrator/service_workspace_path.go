package orchestrator

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

// switchWorkspacePath shares the picker continuation, without creating a card
// owner or preserving a picker that could later retarget the new workspace.
func (s *Service) switchWorkspacePath(surface *state.SurfaceConsoleRecord, text string) []eventcontract.Event {
	if !s.surfaceIsHeadless(surface) {
		return notice(surface, "command_rejected", "请先切到 /mode codex、/mode claude 或 /mode opencode，再用 /workspace <绝对路径> 切换工作区。")
	}
	argument := strings.TrimSpace(text)
	if i := strings.IndexAny(argument, " \t\r\n"); i >= 0 {
		argument = strings.TrimSpace(argument[i:])
	} else {
		argument = ""
	}
	if len(argument) >= 2 && (argument[0] == '"' || argument[0] == '\'') && argument[len(argument)-1] == argument[0] {
		argument = argument[1 : len(argument)-1]
	}
	if argument == "" || !filepath.IsAbs(filepath.FromSlash(argument)) {
		return notice(surface, "workspace_path_invalid", "请使用 /workspace <绝对路径>，例如 /workspace d:/stock/stock-agent。路径包含空格时可加引号。")
	}
	workspaceKey, err := state.ResolveWorkspaceRootOnHost(argument)
	if err != nil {
		return notice(surface, "workspace_path_invalid", fmt.Sprintf("工作区路径无效：%v", err))
	}
	info, err := os.Stat(workspaceKey)
	if err != nil || !info.IsDir() {
		return notice(surface, "workspace_path_invalid", fmt.Sprintf("工作区目录不存在或不可访问：%s。请指定已有目录。", argument))
	}
	if s.surfaceCurrentWorkspaceKey(surface) == normalizeWorkspaceClaimKey(workspaceKey) && surface.AttachedInstanceID != "" {
		return notice(surface, "workspace_already_attached", fmt.Sprintf("当前已接管工作区：%s。", workspaceKey))
	}
	// Check before resolving a managed restart so a rejected switch cannot emit
	// a kill command for the destination runtime.
	if blocked := s.blockFreshThreadAttach(surface, surfaceOverlayRouteCleanupOptions{}); blocked != nil {
		return blocked
	}
	if owner := s.workspaceBusyOwnerForSurface(surface, workspaceKey); owner != nil {
		return notice(surface, "workspace_busy", "目标 workspace 当前已被其他飞书会话接管，请等待对方 /detach。")
	}
	if blocked := s.preflightFeishuRoomWorkspaceChange(surface, workspaceKey); blocked != nil {
		return blocked
	}
	backend := s.surfaceBackend(surface)
	continuation := s.buildHeadlessWorkspaceContinuation(surface, workspaceKey, backend, true)
	resolution := s.resolveWorkspaceContract(surface, workspaceKey, backend)
	return s.executeResolvedWorkspaceContinuation(surface, continuation, resolution, attachWorkspaceOptions{PrepareNewThread: true})
}
