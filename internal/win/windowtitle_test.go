package win

import (
	"fmt"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// TestWindowTitleOfOwnWindowNeverWaitsForItsThread pins the desktop
// waits' stuck helper (desktop-parity 03): a window of this process whose
// thread is busy. GetWindowTextW sends it WM_GETTEXT and waits; windowTitle
// reads the cached caption and returns at once. The window is a
// message-only STATIC on a UI thread of the test's own, never shown and
// never enumerated, like the clipboard's owner window.
func TestWindowTitleOfOwnWindowNeverWaitsForItsThread(t *testing.T) {
	const title = "winuse title probe"
	var hwnd atomic.Uintptr
	ui, err := startUIThread(func() error {
		class, _ := windows.UTF16PtrFromString("STATIC")
		text, _ := windows.UTF16PtrFromString(title)
		inst, _, _ := procGetModuleHandleW.Call(0)
		h, _, err := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(class)), uintptr(unsafe.Pointer(text)),
			0, 0, 0, 0, 0, hwndMessage, 0, inst, 0)
		if h == 0 {
			return fmt.Errorf("CreateWindowEx: %v", err)
		}
		hwnd.Store(h)
		return nil
	})
	if err != nil {
		t.Fatalf("starting the UI thread: %v", err)
	}
	defer ui.close()
	h := windows.HWND(hwnd.Load())

	// Busy the window's thread, as a banner or cursor job busies the
	// helper's.
	busy, release := make(chan struct{}), make(chan struct{})
	go ui.do(func() { close(busy); <-release })
	<-busy
	defer close(release)

	got := make(chan string, 1)
	go func() { got <- windowTitle(h, windows.GetCurrentProcessId()) }()
	select {
	case s := <-got:
		if s != title {
			t.Errorf("windowTitle = %q, want %q", s, title)
		}
	case <-time.After(time.Second):
		t.Fatal("windowTitle waited for the busy thread of a window of this process")
	}

	// The cause, for the record: the message read does wait.
	sent := make(chan string, 1)
	go func() { sent <- windowText(h) }()
	select {
	case <-sent:
		t.Log("GetWindowTextW returned while the thread was busy: this Windows build does not send WM_GETTEXT here")
	case <-time.After(300 * time.Millisecond):
		t.Log("GetWindowTextW waits for the busy thread (WM_GETTEXT), as expected")
	}
}
