package win

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

// Process creation flags for spawning the detached helper.
const (
	detachedProcess        = 0x00000008 // no console window: no flash, ever
	createNewProcessGroup  = 0x00000200 // independent of the caller's Ctrl-C group
	createBreakawayFromJob = 0x01000000 // escape harness job objects that kill children on close
)

// HelperCommand is the hidden subcommand that runs the background helper
// process (see internal/helper).
const HelperCommand = "__helper"

// SpawnHelper starts a detached helper process of the same binary with no
// console window. Std streams are nil so the helper inherits no pipe from
// the caller (or the harness behind it); its logs go to
// %LOCALAPPDATA%\winuse\helper.log.
//
// CREATE_BREAKAWAY_FROM_JOB is tried first so the helper survives the CLI's
// job object being closed; when the job forbids breakaway the spawn is retried
// without the flag (CreateProcess fails with access denied in that case).
func SpawnHelper() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locating winuse binary: %w", err)
	}
	dir := filepath.Dir(exe)
	flags := []uint32{
		detachedProcess | createNewProcessGroup | createBreakawayFromJob,
		detachedProcess | createNewProcessGroup,
	}
	var lastErr error
	for _, creationFlags := range flags {
		cmd := exec.Command(exe, HelperCommand)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
		cmd.Dir = dir
		cmd.SysProcAttr = &syscall.SysProcAttr{
			HideWindow:    true,
			CreationFlags: creationFlags,
		}
		if err := cmd.Start(); err != nil {
			lastErr = err
			continue
		}
		return cmd.Process.Release()
	}
	return fmt.Errorf("starting helper: %w", lastErr)
}
