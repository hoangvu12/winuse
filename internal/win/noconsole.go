package win

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// NoConsoleWindow starts cmd, a console program (powershell.exe, taskkill,
// go, the winuse CLI), with CREATE_NO_WINDOW: it gets a console, but no
// window. A parent without a console of its own (the helper runs detached,
// and a test runner may too) would otherwise give the child a new console,
// which on Windows 11 opens a Windows Terminal window that takes the
// foreground for a moment. A GUI program ignores the flag, so its windows
// show as usual. HideWindow stays unset: it would hide a GUI child's first
// window (a WPF test App run by powershell.exe, for one). The child's other
// creation flags are kept.
func NoConsoleWindow(cmd *exec.Cmd) *exec.Cmd {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= windows.CREATE_NO_WINDOW
	return cmd
}
