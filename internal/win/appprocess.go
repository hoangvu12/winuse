package win

import (
	"errors"
	"fmt"

	"golang.org/x/sys/windows"
)

// ErrProcessGone is OpenAppProcess's answer for a process that has already
// exited: there is nothing left to wait for or end.
var ErrProcessGone = errors.New("the process has exited")

// AppProcess is one process of an App, held open for close-app
// (desktop-parity ticket 10). The handle names that process and no other,
// so a process id reused after it exits never answers for it.
type AppProcess struct {
	h   windows.Handle
	pid uint32
}

// OpenAppProcess opens the process with the given id to wait for its exit
// (SYNCHRONIZE) and, with terminate, to end it (PROCESS_TERMINATE, only for
// close-app --force). Opening reads nothing and changes nothing.
func OpenAppProcess(pid uint32, terminate bool) (*AppProcess, error) {
	access := uint32(windows.SYNCHRONIZE | windows.PROCESS_QUERY_LIMITED_INFORMATION)
	if terminate {
		access |= windows.PROCESS_TERMINATE
	}
	h, err := windows.OpenProcess(access, false, pid)
	if err != nil {
		if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
			return nil, ErrProcessGone // no process has this id any more
		}
		return nil, fmt.Errorf("opening process %d: %v", pid, err)
	}
	return &AppProcess{h: h, pid: pid}, nil
}

// Exited reports whether the process has ended (its handle is signalled).
func (p *AppProcess) Exited() bool {
	ev, err := windows.WaitForSingleObject(p.h, 0)
	return err == nil && ev == windows.WAIT_OBJECT_0
}

// Terminate ends the process (TerminateProcess, exit code 1): the App gets
// no chance to save. Only close-app --force calls it, after the posted
// close.
func (p *AppProcess) Terminate() error {
	if err := LiveDesktopGuard(); err != nil {
		return err
	}
	if err := windows.TerminateProcess(p.h, 1); err != nil {
		return fmt.Errorf("TerminateProcess failed for process %d: %v", p.pid, err)
	}
	return nil
}

// Close releases the handle.
func (p *AppProcess) Close() error {
	return windows.CloseHandle(p.h)
}
