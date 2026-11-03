package win

// Polite real input's offline tests (multi-agent ticket 08): the ledger
// of held keys and buttons, and the Esc latch's yield to the user's own
// keyboard and mouse. The Win32 reads (GetLastInputInfo, GetCursorPos,
// the WM_NULL round trip) answer the test binary's guard values; the real
// calls run only in the e2e suite.

import (
	"testing"
	"time"
	"unsafe"
)

func TestInputLedgerHoldsUntilTheUp(t *testing.T) {
	l := &inputLedger{keys: map[uint16]bool{}, buttons: map[uint32]uint32{}}
	l.note([]input{
		keyEvent(0x11, 0x1D, false, false), // Control down
		keyEvent(0x2E, 0x53, true, false),  // Delete down (extended)
		keyEvent(0x2E, 0x53, true, true),   // Delete up
		unicodeEvent('a', false),           // text is never held
		mouseEvent(mouseEventfLeftDown),    // left down
		mouseEvent(mouseEventfRightDown),   // right down
		mouseEvent(mouseEventfRightUp),     // right up
		mouseEvent(mouseEventfMove),        // a move holds nothing
		keyEvent(0x10, 0x2A, false, false), // Shift down
		keyEvent(0x10, 0x2A, false, true),  // Shift up
	})
	ups := l.releases(func(vk uint16) uint16 { return vk + 1000 })
	if len(ups) != 2 {
		t.Fatalf("%d releases, want Control's up and the left button's up", len(ups))
	}
	var key, button bool
	for _, in := range ups {
		switch in.Type {
		case inputTypeKeyboard:
			ki := (*keybdInput)(unsafe.Pointer(&in.U))
			key = ki.Vk == 0x11 && ki.Scan == 1017 && ki.Flags&keyEventfKeyUp != 0 && ki.Flags&keyEventfExtendedKey == 0
		case inputTypeMouse:
			button = (*mouseInput)(unsafe.Pointer(&in.U)).Flags == mouseEventfLeftUp
		}
	}
	if !key || !button {
		t.Errorf("releases: Control up %v, left up %v", key, button)
	}
	// Once released (the ups went through sendInput), nothing is held.
	l.note(ups)
	if rest := l.releases(func(uint16) uint16 { return 0 }); len(rest) != 0 {
		t.Errorf("%d still held after the releases went through", len(rest))
	}
}

func TestPoliteReadsAreInertInTests(t *testing.T) {
	if InputIdle() < time.Minute {
		t.Error("InputIdle read the real input in a test binary")
	}
	if _, _, ok := CursorPos(); ok {
		t.Error("CursorPos read the real cursor in a test binary")
	}
	if err := SyncWindow(1); err != nil {
		t.Errorf("SyncWindow in a test binary: %v", err)
	}
	if n, err := ReleaseHeld(); n != 0 || err != nil {
		t.Errorf("ReleaseHeld with nothing held: %d, %v", n, err)
	}
}

func TestEscLatchYieldsToTheUsersInputWhileArmed(t *testing.T) {
	l := &escLatch{}
	// Not armed: the user's keys and clicks are theirs.
	escEvent{msg: wmKeyDown, vk: 'A'}.event(l)
	l.onMouse(wmLButtonDown, 0)
	if l.stopped() {
		t.Fatal("the user's input latched a stop while nothing was armed")
	}
	// A passive (Headless) scope does not yield either.
	l.armPassive()
	escEvent{msg: wmKeyDown, vk: 'A'}.event(l)
	if l.stopped() {
		t.Error("a key latched a stop in a passive scope")
	}
	l.disarmPassive()

	l.arm()
	// Injected input, key ups and mouse moves do not yield.
	escEvent{msg: wmKeyDown, vk: 'A', flags: llkhfInjected}.event(l)
	escEvent{msg: wmKeyUp, vk: 'A'}.event(l)
	l.onMouse(wmLButtonDown, llmhfInjected)
	l.onMouse(0x0200, 0) // WM_MOUSEMOVE
	if l.stopped() {
		t.Fatal("injected input, a key up or a move latched a stop")
	}
	// A real key down yields, passes through, and reads as a yield.
	if r := (escEvent{msg: wmKeyDown, vk: 'A'}).event(l); r != escPass {
		t.Errorf("the user's key: verdict %v, want pass", r)
	}
	if !l.stopped() || !l.yielded.Load() {
		t.Errorf("after the user's key: stopped %v, yielded %v", l.stopped(), l.yielded.Load())
	}
	l.disarm()
	if l.stopped() || l.yielded.Load() {
		t.Error("the yield outlived its scope")
	}

	// A real mouse button or wheel yields too.
	for _, msg := range []uint32{wmLButtonDown, wmRButtonDown, wmMButtonDown, wmXButtonDown, wmMouseWheel, wmMouseHWheel} {
		l.arm()
		l.onMouse(msg, 0)
		if !l.stopped() || !l.yielded.Load() {
			t.Errorf("mouse message %#x did not yield", msg)
		}
		l.disarm()
	}

	// Esc during an armed scope stays Esc, not a yield.
	l.arm()
	physicalEscDown.event(l)
	if !l.stopped() || l.yielded.Load() {
		t.Errorf("Esc: stopped %v, yielded %v", l.stopped(), l.yielded.Load())
	}
	l.disarm()
}

func TestYieldedOnANilWatcher(t *testing.T) {
	var w *EscWatcher
	if w.Yielded() {
		t.Error("a nil watcher yielded")
	}
}
