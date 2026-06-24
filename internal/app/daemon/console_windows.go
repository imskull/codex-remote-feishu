//go:build windows

package daemon

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

const swHide = 0

var (
	consoleKernel32           = windows.NewLazySystemDLL("kernel32.dll")
	consoleUser32             = windows.NewLazySystemDLL("user32.dll")
	procGetConsoleWindow      = consoleKernel32.NewProc("GetConsoleWindow")
	procGetConsoleProcessList = consoleKernel32.NewProc("GetConsoleProcessList")
	procFreeConsole           = consoleKernel32.NewProc("FreeConsole")
	procShowWindow            = consoleUser32.NewProc("ShowWindow")
)

// hideOwnConsoleWindow detaches this process from its console when the daemon
// owns a freshly-allocated console — e.g. when the Windows Task Scheduler logon
// task launches the console-subsystem binary. It returns true when it detached
// the console, so the caller can drop os.Stderr from the log output.
//
// We first hide the window, then call FreeConsole to destroy the console
// outright. Hiding alone is not enough: once the daemon spawns child processes,
// Windows re-shows the hidden console window, so it would reappear a second or
// two after startup (verified on Windows 11). Freeing the console removes it for
// good and prevents any later re-show.
//
// When the daemon shares an existing terminal (foreground `codex-remote daemon`
// started from a shell), the parent shell is also attached to the console, so we
// leave it completely alone and return false. With no console at all (stdio-piped
// wrapper roles) this is a no-op. See console_other.go for the non-Windows stub.
func hideOwnConsoleWindow() bool {
	hwnd, _, _ := procGetConsoleWindow.Call()
	if hwnd == 0 {
		return false // no console attached — nothing to detach
	}
	if !ownsConsoleAlone() {
		return false // sharing a terminal with a parent shell — keep it
	}
	_, _, _ = procShowWindow.Call(hwnd, uintptr(swHide))
	ret, _, _ := procFreeConsole.Call()
	return ret != 0
}

// ownsConsoleAlone reports whether this process is the only one attached to its
// console. GetConsoleProcessList returns the count of attached process ids; a
// count of exactly one means we own a fresh console (the Task Scheduler launch),
// versus sharing a terminal with a parent shell (count >= 2).
func ownsConsoleAlone() bool {
	var pids [4]uint32
	count, _, _ := procGetConsoleProcessList.Call(uintptr(unsafe.Pointer(&pids[0])), uintptr(len(pids)))
	return count == 1
}
