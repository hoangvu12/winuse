package win

// Watching an App while an Action settles (speed ticket 01,
// .scratch/speed/issues/01-event-driven-opened-settle.md, ADR 0019). The
// opened-Window report (helper/opened.go) used to look for a new Window
// every 50 ms for a fixed 300 ms after every Action, though most Actions
// open nothing. An AppWatch tells it two things instead:
//
//   - when a top-level window of the App shows, hides or is destroyed,
//     through an out-of-context WinEvent hook filtered to the App's
//     process, so the report looks at once rather than at its next poll
//     (WinEvents are window-manager notifications, not the UI Automation
//     event subscriptions ADR 0004 rules out);
//   - whether the App has gone quiet: no thread of its process ran
//     between two samples, none is running or ready to, and its UI thread
//     waits for messages. Then the Action's input has been handled, and
//     a dialog or menu it opened would already be there.
//
// The watch only reads: it installs no hook in the App, sends it nothing
// and activates nothing. The hooks live on a pumping thread of their own,
// never the UI thread the Esc keyboard hook shares (ADR 0006), so a burst
// of window events can never delay the keyboard hook.

import (
	"errors"
	"fmt"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// WinEvent ids and flags (WinUser.h).
const (
	eventObjectCreate  = 0x8000
	eventObjectDestroy = 0x8001
	eventObjectShow    = 0x8002
	eventObjectHide    = 0x8003

	wineventOutOfContext   = 0x0000
	wineventSkipOwnProcess = 0x0002

	objidWindow = 0
	childidSelf = 0

	gaParent = 1 // GetAncestor GA_PARENT
)

var (
	procSetWinEventHook  = user32.NewProc("SetWinEventHook")
	procUnhookWinEvent   = user32.NewProc("UnhookWinEvent")
	procGetDesktopWindow = user32.NewProc("GetDesktopWindow")
)

// AppWatch watches one App's top-level windows while an Action settles.
// Create it before the Action, so a window the Action opens while it runs
// is heard too, and Close it after the settle.
type AppWatch struct {
	pid    uint32
	thread uint32 // the target Window's UI thread (a UWP App's content thread)
	target uint64
	hook   uintptr

	mu      sync.Mutex
	pending map[uint64]bool // top-level windows created that have neither shown nor been destroyed
	events  chan struct{}   // one buffered signal: a top-level window showed, or the target hid or went
}

// WatchApp starts watching the App behind the target Window. It refuses
// inside an offline test binary, like every live desktop read the
// opened-Window report makes.
func WatchApp(target uint64) (*AppWatch, error) {
	if err := LiveDesktopGuard(); err != nil {
		return nil, err
	}
	pid := AppPID(target)
	tid := uiThreadOf(windows.HWND(target))
	if pid == 0 || tid == 0 {
		return nil, fmt.Errorf("the Window %d has no readable process or UI thread", target)
	}
	t, err := watchThread()
	if err != nil {
		return nil, err
	}
	w := &AppWatch{pid: pid, thread: tid, target: target, pending: map[uint64]bool{}, events: make(chan struct{}, 1)}
	var hookErr error
	if err := t.do(func() {
		h, _, callErr := procSetWinEventHook.Call(eventObjectCreate, eventObjectHide, 0,
			winEventCallback(), uintptr(pid), 0, wineventOutOfContext|wineventSkipOwnProcess)
		if h == 0 {
			hookErr = fmt.Errorf("SetWinEventHook failed: %w", callErr)
			return
		}
		w.hook = h
		watchesMu.Lock()
		watches[h] = w
		watchesMu.Unlock()
	}); err != nil {
		return nil, err
	}
	if hookErr != nil {
		return nil, hookErr
	}
	return w, nil
}

// Wait waits up to d and returns true early when a top-level window of
// the App showed, or the target hid or was destroyed, since the last
// signal was taken.
func (w *AppWatch) Wait(d time.Duration) bool {
	if d <= 0 {
		select {
		case <-w.events:
			return true
		default:
			return false
		}
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-w.events:
		return true
	case <-t.C:
		return false
	}
}

// Pending reports whether a top-level window of the App was created and
// has neither shown nor been destroyed: something may still show.
func (w *AppWatch) Pending() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.pending) > 0
}

// Sample reads the App's threads now (AppSample).
func (w *AppWatch) Sample() AppSample {
	return sampleProcess(w.pid, w.thread)
}

// Close removes the hook. Idempotent.
func (w *AppWatch) Close() {
	t, err := watchThread()
	if err != nil {
		return
	}
	_ = t.do(func() {
		if w.hook == 0 {
			return
		}
		procUnhookWinEvent.Call(w.hook)
		watchesMu.Lock()
		delete(watches, w.hook)
		watchesMu.Unlock()
		w.hook = 0
	})
}

// onEvent applies one WinEvent the hook heard. Runs on the watch thread.
func (w *AppWatch) onEvent(event uint32, hwnd uint64, idObject, idChild int32) {
	kind := classifyWindowEvent(event, hwnd, idObject, idChild, w.target, isTopLevel)
	if kind == windowIgnored {
		return
	}
	w.mu.Lock()
	signal := w.apply(kind, hwnd)
	w.mu.Unlock()
	if signal {
		select {
		case w.events <- struct{}{}:
		default:
		}
	}
}

// apply records one classified event and reports whether it is a signal
// to look again. Called with mu held.
func (w *AppWatch) apply(kind windowEventKind, hwnd uint64) bool {
	switch kind {
	case windowCreated:
		w.pending[hwnd] = true
	case windowShown:
		delete(w.pending, hwnd)
		return true
	case windowDestroyed:
		delete(w.pending, hwnd)
		return hwnd == w.target
	case targetHidden:
		return true
	}
	return false
}

// windowEventKind is what one WinEvent means for a watch.
type windowEventKind int

const (
	windowIgnored   windowEventKind = iota
	windowCreated                   // a top-level window was created (it may show next)
	windowShown                     // a top-level window showed
	windowDestroyed                 // a window was destroyed (it may be pending, or the target)
	targetHidden                    // the target itself hid
)

// classifyWindowEvent sorts a WinEvent from the App's process. Only
// events about a window itself count (OBJID_WINDOW, CHILDID_SELF); a
// create or show counts for a top-level window only (child controls and
// message-only windows are not Windows); a destroyed window can no longer
// be asked whether it was top-level, so every destroy is passed on.
func classifyWindowEvent(event uint32, hwnd uint64, idObject, idChild int32, target uint64, topLevel func(uint64) bool) windowEventKind {
	if hwnd == 0 || idObject != objidWindow || idChild != childidSelf {
		return windowIgnored
	}
	switch event {
	case eventObjectCreate:
		if topLevel(hwnd) {
			return windowCreated
		}
	case eventObjectShow:
		if topLevel(hwnd) {
			return windowShown
		}
	case eventObjectDestroy:
		return windowDestroyed
	case eventObjectHide:
		if hwnd == target {
			return targetHidden
		}
	}
	return windowIgnored
}

// isTopLevel reports whether hwnd is a child of the desktop window: a
// top-level window, owned or not. Message-only windows are not.
func isTopLevel(hwnd uint64) bool {
	parent, _, _ := procGetAncestor.Call(uintptr(hwnd), gaParent)
	desktop, _, _ := procGetDesktopWindow.Call()
	return parent != 0 && parent == desktop
}

// uiThreadOf is the thread that pumps hwnd's messages: a UWP App's
// content thread rather than its ApplicationFrameHost frame's.
func uiThreadOf(hwnd windows.HWND) uint32 {
	if className(hwnd) == uwpFrameClass {
		child, _, _ := procFindWindowExW.Call(uintptr(hwnd), 0, uintptr(unsafe.Pointer(coreWindowClass)), 0)
		if child != 0 {
			tid, _ := windows.GetWindowThreadProcessId(windows.HWND(child), nil)
			return tid
		}
	}
	tid, _ := windows.GetWindowThreadProcessId(hwnd, nil)
	return tid
}

// The watch thread and the hooks it owns. The callback is created once
// (callbacks are never freed) and finds its watch by hook handle.
var (
	watchOnce   sync.Once
	watchT      *uiThread
	watchTErr   error
	watchesMu   sync.Mutex
	watches     = map[uintptr]*AppWatch{}
	watchCBOnce sync.Once
	watchCB     uintptr
)

// watchThread starts the watch thread on first use.
func watchThread() (*uiThread, error) {
	watchOnce.Do(func() {
		watchT, watchTErr = startUIThread(func() error { return nil })
	})
	return watchT, watchTErr
}

// winEventCallback is the WINEVENTPROC every watch hook shares.
func winEventCallback() uintptr {
	watchCBOnce.Do(func() {
		watchCB = syscall.NewCallback(func(hook, event, hwnd, idObject, idChild, _, _ uintptr) uintptr {
			watchesMu.Lock()
			w := watches[hook]
			watchesMu.Unlock()
			if w != nil {
				// idObject and idChild are LONGs: the low 32 bits, signed.
				w.onEvent(uint32(event), uint64(hwnd), int32(uint32(idObject)), int32(uint32(idChild)))
			}
			return 0
		})
	})
	return watchCB
}

// AppSample is one look at an App's threads.
type AppSample struct {
	OK        bool              // the process was found
	UIWaiting bool              // the UI thread waits for messages (blocked in GetMessage or the like)
	Busy      bool              // a thread of the process was running or ready to run
	Switches  map[uint32]uint32 // each thread's context switches, by thread id
}

// AppQuiet reports whether an App was quiet from sample a to sample b:
// both found it waiting, with its UI thread waiting for messages, and no
// thread of it ran in between (the same threads, each with the same
// context switch count). Any doubt is not quiet.
func AppQuiet(a, b AppSample) bool {
	if !a.OK || !b.OK || !a.UIWaiting || !b.UIWaiting || a.Busy || b.Busy || len(a.Switches) != len(b.Switches) {
		return false
	}
	for tid, n := range a.Switches {
		if m, ok := b.Switches[tid]; !ok || m != n {
			return false
		}
	}
	return true
}

// Thread states and the wait reason of a thread blocked waiting for
// window messages (KTHREAD_STATE, KWAIT_REASON).
const (
	threadReady         = 1
	threadRunning       = 2
	threadStandby       = 3
	threadWaiting       = 5
	threadTransition    = 6
	threadDeferredReady = 7

	waitWrUserRequest = 13
)

// systemThreadInformation is SYSTEM_THREAD_INFORMATION, one per thread
// after each SYSTEM_PROCESS_INFORMATION.
type systemThreadInformation struct {
	KernelTime      int64
	UserTime        int64
	CreateTime      int64
	WaitTime        uint32
	StartAddress    uintptr
	UniqueProcess   uintptr
	UniqueThread    uintptr
	Priority        int32
	BasePriority    int32
	ContextSwitches uint32
	ThreadState     uint32
	WaitReason      uint32
}

// The process snapshot buffer, reused across samples (the snapshot of
// every process runs to several hundred KB).
var (
	sampleMu  sync.Mutex
	sampleBuf = make([]byte, 1<<20)
)

// sampleProcess reads pid's threads from one system process snapshot.
// It reads only: OK is false when the snapshot fails or pid is gone.
func sampleProcess(pid, uiThread uint32) AppSample {
	sampleMu.Lock()
	defer sampleMu.Unlock()
	for range 4 {
		var n uint32
		err := windows.NtQuerySystemInformation(windows.SystemProcessInformation, unsafe.Pointer(&sampleBuf[0]), uint32(len(sampleBuf)), &n)
		if err == nil {
			return appSampleFrom(sampleBuf[:min(int(n), len(sampleBuf))], pid, uiThread)
		}
		if !errors.Is(err, windows.STATUS_INFO_LENGTH_MISMATCH) {
			return AppSample{}
		}
		sampleBuf = make([]byte, max(int(n), len(sampleBuf))+1<<16)
	}
	return AppSample{}
}

// appSampleFrom finds pid in a SystemProcessInformation buffer and reads
// its threads. Pure, over the bytes, so tests can build the buffer.
func appSampleFrom(buf []byte, pid, uiThread uint32) AppSample {
	procSize := int(unsafe.Sizeof(windows.SYSTEM_PROCESS_INFORMATION{}))
	threadSize := int(unsafe.Sizeof(systemThreadInformation{}))
	for off := 0; off+procSize <= len(buf); {
		p := (*windows.SYSTEM_PROCESS_INFORMATION)(unsafe.Pointer(&buf[off]))
		if uint32(p.UniqueProcessID) == pid {
			s := AppSample{OK: true, Switches: make(map[uint32]uint32, p.NumberOfThreads)}
			for i := 0; i < int(p.NumberOfThreads); i++ {
				at := off + procSize + i*threadSize
				if at+threadSize > len(buf) {
					return AppSample{}
				}
				t := (*systemThreadInformation)(unsafe.Pointer(&buf[at]))
				tid := uint32(t.UniqueThread)
				s.Switches[tid] = t.ContextSwitches
				switch t.ThreadState {
				case threadReady, threadRunning, threadStandby, threadTransition, threadDeferredReady:
					s.Busy = true
				}
				if tid == uiThread && t.ThreadState == threadWaiting && t.WaitReason == waitWrUserRequest {
					s.UIWaiting = true
				}
			}
			return s
		}
		if p.NextEntryOffset == 0 {
			break
		}
		off += int(p.NextEntryOffset)
	}
	return AppSample{}
}
