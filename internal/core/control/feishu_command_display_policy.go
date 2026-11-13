package control

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func normalizeFeishuCommandProductMode(productMode string) string {
	switch strings.ToLower(strings.TrimSpace(productMode)) {
	case "claude":
		return string(state.ProductModeNormal)
	case "vscode":
		return string(state.ProductModeVSCode)
	default:
		return string(state.ProductModeNormal)
	}
}

func FeishuCommandDefinitionForDisplayContext(def FeishuCommandDefinition, interactive bool, ctx CatalogContext) (FeishuCommandDefinition, bool) {
	resolved, ok := ResolveFeishuCommandDisplayFamily(strings.TrimSpace(def.ID), interactive, ctx)
	if !ok {
		return FeishuCommandDefinition{}, false
	}
	return resolved.Definition, true
}

func projectFeishuCommandDefinitionForDisplay(def FeishuCommandDefinition, interactive bool, ctx CatalogContext) (FeishuCommandDefinition, bool) {
	ctx = NormalizeCatalogContext(ctx)
	profile := ResolveFeishuCommandDisplayProfileForContext(ctx)
	if !profile.IncludesFamily(def.ID) {
		return FeishuCommandDefinition{}, false
	}
	if def.ID == FeishuCommandPrimary && ctx.SurfaceScopeKind != string(CatalogSurfaceScopeKindChat) {
		return FeishuCommandDefinition{}, false
	}
	support, ok := ResolveFeishuCommandSupport(ctx, def.ID)
	if !ok || !support.Visible {
		return FeishuCommandDefinition{}, false
	}
	if interactive {
		if !def.ShowInMenu || !profile.MenuVisibleInStage(def.ID, ctx.MenuStage) {
			return FeishuCommandDefinition{}, false
		}
	} else if !def.ShowInHelp {
		return FeishuCommandDefinition{}, false
	}

	projected := cloneFeishuCommandDefinition(def)
	if def.ID == FeishuCommandGoal && ctx.Backend == agentproto.BackendClaude {
		projected.ArgumentFormHint = "<完成条件>|clear"
		projected.ArgumentFormNote = "Claude 原生 Goal：设置/替换完成条件、查看状态或清除。"
		projected.Description = "`/goal <完成条件>` 设置或替换，`/goal` 查看，`/goal clear` 清除。执行中指令会排队；立即中断请先 `/stop`，收尾后再清除。"
		projected.Examples = []string{"/goal all tests pass and lint is clean", "/goal", "/goal clear"}
	}
	return projected, true
}
func BuildFeishuCommandDisplayPageViewForContext(title, summary string, interactive bool, ctx CatalogContext) FeishuPageView {
	sections := make([]CommandCatalogSection, 0, len(feishuCommandGroups))
	for _, group := range feishuCommandGroups {
		resolved := ResolveFeishuCommandDisplayGroup(group.ID, interactive, ctx)
		entries := make([]CommandCatalogEntry, 0, len(resolved))
		for _, current := range resolved {
			def := current.Definition
			if interactive {
				entries = append(entries, buildFeishuCommandCatalogEntryWithCatalog(def, current.FamilyID, current.VariantID, ctx.Backend, catalogButtonLabel(def)))
				continue
			}
			entries = append(entries, buildFeishuCommandCatalogEntry(def, ""))
		}
		if len(entries) == 0 {
			continue
		}
		sections = append(sections, CommandCatalogSection{
			Title:   group.Title,
			Entries: entries,
		})
	}
	view := FeishuPageView{
		Title:          title,
		CatalogBackend: ctx.Backend,
		Interactive:    interactive,
		Sections:       sections,
	}
	if lines := splitFeishuCommandPageSummaryLines(summary); len(lines) != 0 {
		view.SummarySections = []FeishuCardTextSection{{Lines: lines}}
	}
	return NormalizeFeishuPageView(view)
}
