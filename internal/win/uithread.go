package win

// The helper's UI thread (ticket 13): one OS-locked thread with a Win32
// message loop. Windows that must live on a pumping thread are created and
// owned by it — the in-control overlay today; ticket 12 installs the Esc
// low-level keyboard hook on the same thread, because hooks are called on
// the installing thread while it pumps messages. Everything else reaches
// the thread through do, which posts a job and waits bounded: jobs must
// stay quick, and must never inject input (every injected event passes
// the keyboard hook synchronously — a SendInput on this thread stalls
// input system-wide, win32.md §8) or touch UI Automation (the COM worker
// owns that).
//
// The thread is started through startUIThread's setup hook: setup runs on
// the thread before the loop starts, so window class registration and
// window creation land on it. This file holds only the thread; the window
// lives in overlay.go.

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Message-loop constants (win32.md §8).
const (
	wmQuit = 0x12
	wmApp  = 0x8000 // WM_APP: the job-wake thread message

	pmNoremove = 0

	// htTransparent is WM_NCHITTEST's "not here": the click passes to the
	// window below.
	htTransparent = ^uintptr(0)
)

var (
	procGetModuleHandleW   = user32.NewProc("GetModuleHandleW")
	procRegisterClassExW   = user32.NewProc("RegisterClassExW")
	procCreateWindowExW    = user32.NewProc("CreateWindowExW")
	procDefWindowProcW     = user32.NewProc("DefWindowProcW")
	procGetMessageW        = user32.NewProc("GetMessageW")
	procPeekMessageW       = user32.NewProc("PeekMessageW")
	procTranslateMessage   = user32.NewProc("TranslateMessage")
	procDispatchMessageW   = user32.NewProc("DispatchMessageW")
	procPostThreadMessageW = user32.NewProc("PostThreadMessageW")
)

// msg is the Win32 MSG (48 bytes on amd64 and arm64; win32.md §8).
type msg struct {
	Hwnd    uintptr
	Message uint32
	_       uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      struct{ X, Y int32 }
	_       uint32
}

var _ [48]byte = [unsafe.Sizeof(msg{})]byte{}

// uiThreadTimeout bounds how long a caller waits for one UI-thread job:
// a wedged UI thread must never hang an Action or a capture.
const uiThreadTimeout = 2 * time.Second

// errUIThreadClosed reports a job posted after close.
var errUIThreadClosed = errors.New("the UI thread is closed")

// uiThread is one OS-locked thread with a message loop. Jobs posted with
// do run on it, in order; window messages (the overlay's WM_PAINT) are
// dispatched to their window procedure by the loop.
type uiThread struct {
	tid    uint32 // the locked thread's id; set once, before the loop starts
	jobsMu sync.Mutex
	jobs   []func()
	closed bool
	exited chan struct{}
}

// startUIThread locks a fresh thread, runs setup on it (window class
// registration and window creation — anything that must happen on the
// thread), and enters the message loop. It returns once setup succeeded
// (nil error) or failed (the thread is then gone).
func startUIThread(setup func() error) (*uiThread, error) {
	t := &uiThread{exited: make(chan struct{})}
	ready := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		// The thread's message queue must exist before PostThreadMessageW
		// can wake the loop: a no-op PeekMessage creates it.
		var m msg
		procPeekMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0, pmNoremove)
		atomic.StoreUint32(&t.tid, windows.GetCurrentThreadId())
		if err := setup(); err != nil {
			ready <- err
			close(t.exited)
			return
		}
		ready <- nil
		t.loop()
	}()
	if err := <-ready; err != nil {
		return nil, err
	}
	return t, nil
}

// loop pumps messages until WM_QUIT: a thread message wakes the job
// drain; window messages go through Translate/Dispatch to their window
// procedure. Thread messages themselves (hwnd 0) are never dispatched.
func (t *uiThread) loop() {
	defer close(t.exited)
	var m msg
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			return // WM_QUIT (0) or a retrieval failure (-1)
		}
		t.drain()
		if m.Hwnd != 0 {
			procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
			procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
		}
	}
}

// drain runs every queued job, in order, on the UI thread.
func (t *uiThread) drain() {
	t.jobsMu.Lock()
	jobs := t.jobs
	t.jobs = nil
	t.jobsMu.Unlock()
	for _, job := range jobs {
		job()
	}
}

// do runs job on the UI thread and waits for it. It returns an error when
// the thread is closed or the job did not answer within uiThreadTimeout;
// the job may still run after a timeout, which is harmless for the
// overlay's cosmetic operations.
func (t *uiThread) do(job func()) error {
	done := make(chan struct{})
	t.jobsMu.Lock()
	if t.closed {
		t.jobsMu.Unlock()
		return errUIThreadClosed
	}
	t.jobs = append(t.jobs, func() { defer close(done); job() })
	t.jobsMu.Unlock()
	procPostThreadMessageW.Call(uintptr(atomic.LoadUint32(&t.tid)), wmApp, 0, 0)
	select {
	case <-done:
		return nil
	case <-time.After(uiThreadTimeout):
		return fmt.Errorf("the UI thread did not answer within %s", uiThreadTimeout)
	}
}

// close stops the loop and waits for the thread to exit. Idempotent; a
// wedged thread is leaked (the process exit reclaims it).
func (t *uiThread) close() {
	t.jobsMu.Lock()
	alreadyClosed := t.closed
	t.closed = true
	tid := atomic.LoadUint32(&t.tid)
	t.jobsMu.Unlock()
	if !alreadyClosed && tid != 0 {
		procPostThreadMessageW.Call(uintptr(tid), wmQuit, 0, 0)
	}
	select {
	case <-t.exited:
	case <-time.After(uiThreadTimeout):
	}
}
