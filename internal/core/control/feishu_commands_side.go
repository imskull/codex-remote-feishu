package control

type SideSessionSummary struct {
	SessionID string
	Status    string
	ThreadID  string
	Queued    int
}

func sideCommandSpec() feishuCommandSpec {
	return feishuCommandSpec{
		definition: FeishuCommandDefinition{
			ID:               FeishuCommandSide,
			GroupID:          FeishuCommandGroupCommonTools,
			Title:            "Side mode",
			CanonicalSlash:   "/side",
			CanonicalMenuKey: "side",
			ArgumentKind:     FeishuCommandArgumentText,
			Description:      "进入独立的旁支会话，原任务继续运行；/side quit 退出并恢复原会话输入。文件改动会保留。",
			Examples:         []string{"/side", "/side <message>", "/side quit"},
			ShowInHelp:       true,
		},
		textPrefixes: []feishuCommandPrefixMatch{{alias: "/side", kind: ActionSideCommand}},
	}
}
