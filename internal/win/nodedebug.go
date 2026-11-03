package win

// The Node.js debug signal (headless-max ticket 15, the attach to a
// running Electron App): what `process._debugProcess(pid)` does on Windows
// (node src/node_process_methods.cc, DebugProcess). A Node process that may
// open its inspector on request publishes the address of its handler in a
// named mapping, "node-debug-handler-<pid>"; a thread started at that
// address in the process opens the inspector on 127.0.0.1 (port 9229 by
// default). An Electron App with the nodeCliInspect fuse off never
// publishes the mapping (measured on Discord 1.0.9266, probe round 3), so
// nothing can open its inspector.
//
// It runs code inside the user's App, so winuse sends it only when the
// agent asks to attach, and the skill tells the agent to ask the user
// first.

import (
	"errors"
	"fmt"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ErrNodeInspectOff is a process that publishes no Node debug handler: an
// Electron App whose nodeCliInspect fuse is off, or no Node process at
// all. Nothing was sent.
var ErrNodeInspectOff = errors.New("the process accepts no Node.js debug signal (no node-debug-handler mapping)")

// nodeDebugWait bounds the wait for the handler thread to return.
const nodeDebugWait = 5 * time.Second

var (
	procOpenFileMappingW   = kernel32.NewProc("OpenFileMappingW")
	procCreateRemoteThread = kernel32.NewProc("CreateRemoteThread")
	// procRtlMoveMemory: clipboard.go's.
)

// fileMapRead is FILE_MAP_READ.
const fileMapRead = 0x0004

// NodeDebugHandler reports whether a process publishes the Node debug
// handler, without opening anything in it: the fuse check before an
// attach.
func NodeDebugHandler(pid uint32) bool {
	h, err := openNodeDebugMapping(pid)
	if err != nil {
		return false
	}
	windows.CloseHandle(h)
	return true
}

// SignalNodeDebug asks the Node process pid to open its inspector, the way
// process._debugProcess does: it reads the handler's address from the
// process's node-debug-handler mapping and starts a thread there. A
// process without the mapping fails with ErrNodeInspectOff and nothing is
// sent.
func SignalNodeDebug(pid uint32) error {
	if err := LiveDesktopGuard(); err != nil {
		return err
	}
	mapping, err := openNodeDebugMapping(pid)
	if err != nil {
		return ErrNodeInspectOff
	}
	defer windows.CloseHandle(mapping)
	view, err := windows.MapViewOfFile(mapping, fileMapRead, 0, 0, unsafe.Sizeof(uintptr(0)))
	if err != nil {
		return fmt.Errorf("reading the Node debug handler: %w", err)
	}
	// Copied out through RtlMoveMemory: the view is an address the system
	// handed back, not a Go pointer.
	var handler uintptr
	procRtlMoveMemory.Call(uintptr(unsafe.Pointer(&handler)), view, unsafe.Sizeof(handler))
	windows.UnmapViewOfFile(view)
	if handler == 0 {
		return ErrNodeInspectOff
	}
	const access = windows.PROCESS_CREATE_THREAD | windows.PROCESS_QUERY_INFORMATION |
		windows.PROCESS_VM_OPERATION | windows.PROCESS_VM_WRITE | windows.PROCESS_VM_READ
	proc, err := windows.OpenProcess(access, false, pid)
	if err != nil {
		return fmt.Errorf("opening process %d: %w", pid, err)
	}
	defer windows.CloseHandle(proc)
	th, _, cerr := procCreateRemoteThread.Call(uintptr(proc), 0, 0, handler, 0, 0, 0)
	if th == 0 {
		return fmt.Errorf("starting the Node debug handler in process %d: %w", pid, cerr)
	}
	defer windows.CloseHandle(windows.Handle(th))
	if ev, err := windows.WaitForSingleObject(windows.Handle(th), uint32(nodeDebugWait/time.Millisecond)); err != nil || ev != windows.WAIT_OBJECT_0 {
		return fmt.Errorf("the Node debug handler in process %d did not return within %s", pid, nodeDebugWait)
	}
	return nil
}

// openNodeDebugMapping opens a process's node-debug-handler mapping for
// reading.
func openNodeDebugMapping(pid uint32) (windows.Handle, error) {
	name, err := windows.UTF16PtrFromString(fmt.Sprintf("node-debug-handler-%d", pid))
	if err != nil {
		return 0, err
	}
	h, _, cerr := procOpenFileMappingW.Call(fileMapRead, 0, uintptr(unsafe.Pointer(name)))
	if h == 0 {
		return 0, cerr
	}
	return windows.Handle(h), nil
}
