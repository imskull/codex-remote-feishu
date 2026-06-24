//go:build windows

package daemon

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

const swHide = 0

// hideOwnConsoleWindow hides this process's console window when the daemon owns
// a freshly-allocated console — e.g. when the Task Scheduler logon task launches
// the console-subsystem binary, Windows attaches a brand-new console window. In
// that case the daemon is the only process on the console, so hiding the window
// gives a truly headless background daemon.
//
// When the daemon shares an existing terminal (foreground `codex-remote daemon`
// started from a shell), the parent shell is also attached to the console, so we
// leave the window visible and never touch the user's interactive terminal.
// When the process has no console at all (stdio-piped wrapper roles), this is a
// no-op.
func hideOwnConsoleWindow() {
	kernel32 := windows.NewLazySystemDLL("kernel32.dll")
	user32 := windows.NewLazySystemDLL("user32.dll")
	getConsoleWindow := kernel32.NewProc("GetConsoleWindow")
	getConsoleProcessList := kernel32.NewProc("GetConsoleProcessList")
	showWindow := user32.NewProc("ShowWindow")

	hwnd, _, _ := getConsoleWindow.Call()
	if hwnd == 0 {
		return // no console attached — nothing to hide
	}
	if !ownsConsoleAlone(getConsoleProcessList) {
		return // sharing a terminal with a parent shell — keep it visible
	}
	_, _, _ = showWindow.Call(hwnd, uintptr(swHide))
}

// ownsConsoleAlone reports whether this process is the only process attached to
// its console. GetConsoleProcessList returns the count of attached process ids;
// a count of exactly one means we own a fresh console (Task Scheduler launch).
func ownsConsoleAlone(getConsoleProcessList *windows.LazyProc) bool {
	var pids [4]uint32
	count, _, _ := getConsoleProcessList.Call(uintptr(unsafe.Pointer(&pids[0])), uintptr(len(pids)))
	return count == 1
}
