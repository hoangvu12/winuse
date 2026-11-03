package win

import (
	"os/exec"
	"syscall"
	"testing"

	"golang.org/x/sys/windows"
)

func TestNoConsoleWindowAddsTheFlagAndKeepsTheOthers(t *testing.T) {
	cmd := NoConsoleWindow(exec.Command("powershell.exe"))
	if cmd.SysProcAttr == nil || cmd.SysProcAttr.CreationFlags != windows.CREATE_NO_WINDOW || cmd.SysProcAttr.HideWindow {
		t.Errorf("a plain command: %+v, want CREATE_NO_WINDOW alone and no HideWindow", cmd.SysProcAttr)
	}
	cmd = exec.Command("taskkill")
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
	if got := NoConsoleWindow(cmd).SysProcAttr.CreationFlags; got != windows.CREATE_NEW_PROCESS_GROUP|windows.CREATE_NO_WINDOW {
		t.Errorf("flags %#x, want the process group kept and CREATE_NO_WINDOW added", got)
	}
}
