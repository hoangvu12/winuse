package win

// The Esc-stop hook's offline tests (ticket 12): the latch's decision
// table (which keyboard events are swallowed and latched), the nested
// armed scopes, and the guard that no offline test can install the real
// hook. The syscall callback itself, the thread and the actual
// SetWindowsHookExW call are exercised only by the user-run e2e suite —
// a hook on the live desktop is exactly what offline tests must never
// install.

import (
	"errors"
	"testing"
)

// escEvent is one keyboard event for the decision-table tests.
type escEvent struct {
	nCode int32
	msg   uint32
	vk    uint32
	flags uint32
}

// physicalEscDown is a real Esc press.
var physicalEscDown = escEvent{nCode: 0, msg: wmKeyDown, vk: vkEscape}

// event runs one event against the latch and returns the hook's verdict.
func (e escEvent) event(l *escLatch) escHookResult {
	return escHookEvent(e.nCode, e.msg, kbdllHookStruct{VkCode: e.vk, Flags: e.flags}, l)
}

func TestEscLatchPassesEverythingWhileNotArmed(t *testing.T) {
	// The user's own keyboard is untouched between commands: a physical
	// Esc press (down and up) passes through, and nothing latches.
	l := &escLatch{}
	if r := physicalEscDown.event(l); r != escPass {
		t.Errorf("physical Esc down while not armed: verdict %v, want pass", r)
	}
	if l.stopped() {
		t.Error("a press while not armed must not latch")
	}
	if r := (escEvent{msg: wmKeyUp, vk: vkEscape}).event(l); r != escPass {
		t.Errorf("physical Esc up while not armed: verdict %v, want pass", r)
	}
}

func TestEscLatchLatchesAndSwallowsPhysicalEscWhileArmed(t *testing.T) {
	// While winuse acts, the physical Esc key-down is swallowed (Codex
	// swallows the key) and latched; its key-up is swallowed too, so the
	// app never sees half a press.
	l := &escLatch{}
	l.arm()
	defer l.disarm()

	if r := physicalEscDown.event(l); r != escSwallow {
		t.Errorf("physical Esc down while armed: verdict %v, want swallow", r)
	}
	if !l.stopped() {
		t.Error("a press while armed must latch")
	}
	if r := (escEvent{msg: wmKeyUp, vk: vkEscape}).event(l); r != escSwallow {
		t.Errorf("the swallowed press's key-up: verdict %v, want swallow", r)
	}
	// The up of a swallowed down stays swallowed even after the scope
	// ends: the pairing must survive the arm state flipping.
	if r := (escEvent{msg: wmKeyUp, vk: vkEscape}).event(l); r != escPass {
		t.Errorf("a second key-up: verdict %v, want pass (its down was already paired)", r)
	}
}

func TestEscLatchIgnoresInjectedKeys(t *testing.T) {
	// Injected events — winuse's own press-key Escape, any other
	// injector's — never latch and never swallow, in both directions,
	// armed or not. This is what keeps `press-key Escape` working.
	l := &escLatch{}
	l.arm()
	defer l.disarm()

	for _, tc := range []struct {
		name  string
		event escEvent
	}{
		{"injected down", escEvent{msg: wmKeyDown, vk: vkEscape, flags: llkhfInjected}},
		{"injected up", escEvent{msg: wmKeyUp, vk: vkEscape, flags: llkhfInjected}},
		{"injected with winuse's tag", escEvent{msg: wmKeyDown, vk: vkEscape, flags: llkhfInjected}},
	} {
		if r := tc.event.event(l); r != escPass {
			t.Errorf("%s: verdict %v, want pass", tc.name, r)
		}
	}
	if l.stopped() {
		t.Error("an injected Esc must not latch")
	}
}

func TestEscLatchSwallowsOnlyThePairedKeyUp(t *testing.T) {
	// A press that reached an app (down passed through) must not have its
	// key-up swallowed by a later armed scope: an app would believe Esc is
	// still held.
	l := &escLatch{}
	l.arm()
	l.disarm() // back to unarmed
	// The user presses Esc while nothing is armed: down and up both pass.
	if r := physicalEscDown.event(l); r != escPass {
		t.Fatalf("down while not armed: verdict %v, want pass", r)
	}
	l.arm()
	defer l.disarm()
	if r := (escEvent{msg: wmKeyUp, vk: vkEscape}).event(l); r != escPass {
		t.Errorf("an unpaired key-up while armed: verdict %v, want pass", r)
	}
}

func TestEscLatchPassesOtherKeysAndSyskeyForms(t *testing.T) {
	// Other keys pass while armed; the Alt+Esc syskey forms of the Esc
	// key are treated as Esc presses (win32.md §8 checks both message
	// forms); nCode < 0 always passes unread.
	l := &escLatch{}
	l.arm()
	defer l.disarm()

	for _, tc := range []struct {
		name  string
		event escEvent
		want  escHookResult
	}{
		{"another key", escEvent{msg: wmKeyDown, vk: 'A'}, escPass},
		{"another key up", escEvent{msg: wmKeyUp, vk: 'A'}, escPass},
		{"nCode below zero", escEvent{nCode: -1, msg: wmKeyDown, vk: vkEscape}, escPass},
		{"injected syskey", escEvent{msg: wmSysKeyDown, vk: vkEscape, flags: llkhfInjected}, escPass},
		{"physical syskey down while armed", escEvent{msg: wmSysKeyDown, vk: vkEscape}, escSwallow},
	} {
		if r := tc.event.event(l); r != tc.want {
			t.Errorf("%s: verdict %v, want %v", tc.name, r, tc.want)
		}
	}
	if !l.stopped() {
		t.Error("the syskey Esc press must latch like a plain one")
	}
	if r := (escEvent{msg: wmSysKeyUp, vk: vkEscape}).event(l); r != escSwallow {
		t.Errorf("the syskey press's up: verdict %v, want swallow", r)
	}
}

func TestEscLatchNestedScopesKeepThePress(t *testing.T) {
	// A Batch arms around its steps' Actions: a press latched in an inner
	// scope stays latched for the outer one to see, and only the
	// outermost Disarm clears it.
	l := &escLatch{}
	l.arm() // the Batch
	l.arm() // a step's Action
	if r := physicalEscDown.event(l); r != escSwallow {
		t.Fatalf("press inside a nested scope: verdict %v, want swallow", r)
	}
	l.disarm() // the step ends; the press stays latched for the Batch
	if !l.stopped() {
		t.Error("the press must stay latched for the enclosing scope")
	}
	l.disarm() // the Batch ends
	if l.stopped() {
		t.Error("the outermost Disarm must clear the latch")
	}
}

func TestEscLatchDisarmWithoutArmIsHarmless(t *testing.T) {
	// A stray Disarm (a bug elsewhere) must not make the count negative
	// and swallow presses of the next scope.
	l := &escLatch{}
	l.disarm()
	l.disarm()
	l.arm()
	if l.stopped() {
		t.Error("a stray disarm must not latch anything")
	}
	if r := physicalEscDown.event(l); r != escSwallow {
		t.Fatalf("press after stray disarms: verdict %v, want swallow", r)
	}
	l.disarm()
	if l.stopped() {
		t.Error("the scope ended; the latch must clear")
	}
}

func TestEscWatcherNeverInstallsInOfflineTests(t *testing.T) {
	// The hard rule: no offline test may install the real hook on the
	// live desktop. Both constructors refuse from inside a test binary
	// (LiveDesktopGuard inside the install job), and nothing they could
	// have installed survives.
	w, err := NewEscWatcher()
	if err == nil {
		w.Close()
		t.Fatal("the fallback watcher must refuse to install the hook inside a test binary")
	}
	if !errors.Is(err, ErrLiveDesktopInTest) {
		t.Errorf("error = %v, want the live-desktop refusal", err)
	}

	// The shared-thread form installs through the same guarded job.
	w2, err := NewEscWatcherOnUI(func(job func()) error {
		job()
		return nil
	})
	if err == nil {
		w2.Close()
		t.Fatal("the shared-thread watcher must refuse to install the hook inside a test binary")
	}
	if !errors.Is(err, ErrLiveDesktopInTest) {
		t.Errorf("error = %v, want the live-desktop refusal", err)
	}
}

func TestEscWatcherArmDisarmStoppedThroughTheWatcher(t *testing.T) {
	// The watcher's exported surface is the latch's, with a nil receiver
	// safe to call (a Services with no watcher).
	w := &EscWatcher{}
	w.Arm()
	if w.Stopped() {
		t.Error("nothing pressed yet")
	}
	w.latch.pressed.Store(true)
	if !w.Stopped() {
		t.Error("the latched press must be visible")
	}
	w.Disarm()
	if w.Stopped() {
		t.Error("the scope ended; the latch must clear")
	}

	var nilWatch *EscWatcher
	nilWatch.Arm()
	if nilWatch.Stopped() {
		t.Error("a nil watcher must never report stopped")
	}
	nilWatch.Disarm()
	nilWatch.Close()
}
