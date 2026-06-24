//go:build !windows

package daemon

// hideOwnConsoleWindow is a no-op on non-Windows platforms, where launchd /
// systemd user services do not allocate a console window.
func hideOwnConsoleWindow() {}
