package daemon

import "github.com/kxn/codex-remote-feishu/internal/core/eventcontract"

func (a *App) commandDispatchFailure(event eventcontract.Event, err error) []eventcontract.Event {
	if event.Command.SideSessionID != "" {
		return a.service.HandleSideCommandDispatchFailure(event.SurfaceSessionID, event.Command.SideSessionID, event.Command.CommandID, err)
	}
	return a.service.HandleCommandDispatchFailure(event.SurfaceSessionID, event.Command.CommandID, err)
}
