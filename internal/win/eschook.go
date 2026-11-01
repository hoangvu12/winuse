package win

// The Esc stop (ticket 12): a low-level keyboard hook (WH_KEYBOARD_LL)
// that sees the physical Escape key while winuse is acting, so the user
// can take back control at any moment. The hook lives on the helper's UI
// thread — the overlay's when there is one, its own pumping thread as the
// fallback — because LL hooks are called on the installing thread while
// it pumps messages (win32.md §8). The callback stays a few comparisons
// plus atomic stores: Windows silently removes a hook whose callback
// outlives the LowLevelHooksTimeout, and this thread also pumps the
// overlay's messages.
//
// Injected keystrokes are ignored (LLKHF_INJECTED): winuse's own
// press-key Escape — and any other injector's — passes through untouched,
// so only a physical press stops winuse.
//
// While armed (an Action, a Batch or a launch wait is in progress), the
// physical Esc key is swallowed, the way Codex swallows it (codex-surface
// §4: "the key is swallowed, so the app doesn't receive it"): the user's
// Esc means "stop winuse", not "Escape into the controlled app". While
// not armed, every key — physical Esc included — passes through
// untouched, so the hook never eats the user's own typing between
// commands. The swallow pairs a key-down with its key-up (swallowing an
// up without its down would leave an app believing Esc is held), and a
// press is latched only for the duration of the armed scope: the next
// command starts clean.
//
// Headless scopes (ADR 0010, the headless spec's "Esc in Headless
// scopes") arm passively: winuse is not in control of the user's keyboard
// or foreground, so the user's Esc may well be meant for the app they are
// typing in. A physical Esc press in a passive scope still latches the
// stop (exit 130) but passes through to the app. Any swallowing scope
// armed at the same time wins — a Batch with a Headed step swallows for
// its whole run, Headless steps included.

import (
	"errors"
	"fmt"
	"sync/atomic"
	"syscall"
	"unsafe"
)

// Keyboard-hook constants (win32.md §8; LowLevelKeyboardProc docs).
const (
	whKeyboardLL = 13

	wmKeyDown    = 0x100
	wmKeyUp      = 0x101
	wmSysKeyDown = 0x104
	wmSysKeyUp   = 0x105

	vkEscape = 0x1B

	llkhfInjected = 0x10
)

var (
	procSetWindowsHookExW   = user32.NewProc("SetWindowsHookExW")
	procUnhookWindowsHookEx = user32.NewProc("UnhookWindowsHookEx")
	procCallNextHookEx      = user32.NewProc("CallNextHookEx")
)

// kbdllHookStruct is the Win32 KBDLLHOOKSTRUCT (24 bytes on amd64 and
// arm64: four uint32s, alignment pad, one pointer).
type kbdllHookStruct struct {
	VkCode, ScanCode, Flags, Time uint32
	ExtraInfo                     uintptr
}

var _ [24]byte = [unsafe.Sizeof(kbdllHookStruct{})]byte{}

// escLatch is the hook's state: the armed-scope nesting count, the latched
// press, and the down/up swallow pairing. It is the whole of the callback's
// logic, split from the syscall shim so the decision table is
// offline-testable.
type escLatch struct {
	// armed counts nested armed scopes (a Batch arms, each step's Action
	// arms again); the hook records and swallows only while it is > 0.
	armed atomic.Int32
	// passive counts nested passive scopes (Headless Actions and
	// all-Headless Batches, ADR 0010): while it is > 0 and armed is 0 the
	// hook records a press but lets the key through.
	passive atomic.Int32
	// pressed latches one press for the duration of the outermost scope of
	// either kind: several checks inside the scope all see it; leaving the
	// last scope (both counts back at 0) clears it, so a press never leaks
	// into the next command.
	pressed atomic.Bool
	// swallowUp is set when a key-down was swallowed, and cleared by its
	// key-up: an app must never see half an Esc press.
	swallowUp atomic.Bool
}

// arm enters an armed scope. Scopes nest; the outermost Disarm clears the
// latch.
func (l *escLatch) arm() { l.armed.Add(1) }

// disarm leaves an armed scope, clearing the latched press when the
// outermost scope ends. A disarm without an arm is a no-op (deferred
// disarms pair defensively), so the count never goes negative.
func (l *escLatch) disarm() { l.leave(&l.armed, &l.passive) }

// armPassive enters a passive scope (a Headless Action or Batch): presses
// latch but pass through, unless a swallowing scope is armed too. Scopes
// nest with each other and with armed ones.
func (l *escLatch) armPassive() { l.passive.Add(1) }

// disarmPassive leaves a passive scope; like disarm, a stray call is a
// no-op, and the latch clears only when no scope of either kind remains.
func (l *escLatch) disarmPassive() { l.leave(&l.passive, &l.armed) }

// leave decrements one scope count (never below zero) and clears the
// latched press when that was the last scope of its kind and none of the
// other kind is open: a passive step ending inside a swallowing Batch
// must not clear a press the Batch still has to see.
func (l *escLatch) leave(count, other *atomic.Int32) {
	for {
		cur := count.Load()
		if cur <= 0 {
			return
		}
		if count.CompareAndSwap(cur, cur-1) {
			if cur == 1 && other.Load() == 0 {
				l.pressed.Store(false)
			}
			return
		}
	}
}

// stopped reports whether the user's Esc press was latched in the armed
// scope (a peek: the latch stays until the scope ends).
func (l *escLatch) stopped() bool { return l.pressed.Load() }

// onKey decides one keyboard event: true means the hook swallows it.
// A physical Esc key-down while armed is latched and swallowed; its
// key-up is swallowed to keep the pair together. A physical Esc key-down
// while only passive scopes are open is latched and passed through (its
// key-up then passes too: swallowUp stays false). Everything else — other
// keys, injected events, a press while nothing is armed — passes through.
func (l *escLatch) onKey(msg uint32, vk, flags uint32) bool {
	if vk != vkEscape || flags&llkhfInjected != 0 {
		return false
	}
	switch msg {
	case wmKeyDown, wmSysKeyDown:
		if l.armed.Load() > 0 {
			l.pressed.Store(true)
			l.swallowUp.Store(true)
			return true
		}
		if l.passive.Load() > 0 {
			l.pressed.Store(true)
			return false
		}
		return false
	case wmKeyUp, wmSysKeyUp:
		// Swallow only the release of a press we swallowed (swallowUp is
		// false for a down that reached an app, whatever the arm state is
		// now), so an app never sees an Esc key-up without its key-down.
		return l.swallowUp.Swap(false)
	}
	return false
}

// escHookResult is one keyboard event's verdict.
type escHookResult int

const (
	escPass    escHookResult = iota // pass the event on (CallNextHookEx)
	escSwallow                      // swallow the event (return 1, no chaining)
)

// escHookEvent is the whole decision the callback makes for one event,
// split from the syscall shim so it is offline-testable: nCode < 0 must
// always be passed on to the hook chain unread (LowLevelKeyboardProc
// docs); only a real (non-injected) Esc event is otherwise considered.
func escHookEvent(nCode int32, msg uint32, k kbdllHookStruct, l *escLatch) escHookResult {
	if nCode < 0 {
		return escPass
	}
	if l.onKey(msg, k.VkCode, k.Flags) {
		return escSwallow
	}
	return escPass
}

// EscWatcher is the helper's physical-Esc watcher: a WH_KEYBOARD_LL hook
// on a pumping UI thread plus the latch the commands share. Create one
// per helper process — on the overlay's UI thread with NewEscWatcherOnUI
// (the shared thread, ADR 0006), or with NewEscWatcher when there is no
// overlay thread to share — and Close it when the helper exits. Arm and
// Disarm bracket every scope the user can stop; Stopped is polled at the
// abort checkpoints.
type EscWatcher struct {
	latch escLatch

	post     func(func()) error // posts a job to the owning UI thread
	own      *uiThread          // non-nil: it owns a fallback UI thread
	hook     uintptr            // the hook handle; owned by the UI thread
	callback uintptr            // the NewCallback shim (one per watcher)
	hookErr  error              // install's recorded failure
}

// NewEscWatcherOnUI installs the hook on the given pumping UI thread —
// the overlay's Do, the shared thread of ADR 0006. The install job runs
// on that thread (SetWindowsHookExW must be called from the thread that
// pumps the hook), bounded by the thread's job wait.
func NewEscWatcherOnUI(do func(func()) error) (*EscWatcher, error) {
	w := &EscWatcher{post: do}
	if err := do(w.install); err != nil {
		return nil, fmt.Errorf("installing the Esc keyboard hook on the UI thread: %w", err)
	}
	if err := w.installed(); err != nil {
		return nil, err
	}
	return w, nil
}

// NewEscWatcher installs the hook on its own pumping UI thread — the
// fallback when there is no overlay to share a thread with (a helper
// whose indicator could not start must still stop on Esc).
func NewEscWatcher() (*EscWatcher, error) {
	w := &EscWatcher{}
	t, err := startUIThread(func() error {
		w.install()
		return w.installed()
	})
	if err != nil {
		return nil, err
	}
	w.own = t
	w.post = t.do
	return w, nil
}

// install installs the hook. Runs on the UI thread; a failure is recorded
// rather than returned (thread jobs carry no value).
func (w *EscWatcher) install() {
	if err := LiveDesktopGuard(); err != nil {
		w.hookErr = err
		return
	}
	hMod, _, _ := procGetModuleHandleW.Call(0)
	w.callback = syscall.NewCallback(func(nCode, wParam uintptr, lParam unsafe.Pointer) uintptr {
		if escHookEvent(int32(nCode), uint32(wParam),
			*(*kbdllHookStruct)(lParam), &w.latch) == escSwallow {
			return 1
		}
		r, _, _ := procCallNextHookEx.Call(0, nCode, wParam, uintptr(lParam))
		return r
	})
	h, _, callErr := procSetWindowsHookExW.Call(whKeyboardLL, w.callback, hMod, 0)
	if h == 0 {
		w.hookErr = fmt.Errorf("SetWindowsHookExW(WH_KEYBOARD_LL) failed: %w", callErr)
		return
	}
	w.hook = h
}

// installed reports install's outcome to the constructor.
func (w *EscWatcher) installed() error {
	if w.hookErr != nil {
		return fmt.Errorf("installing the Esc keyboard hook: %w", w.hookErr)
	}
	if w.hook == 0 {
		return errors.New("the Esc keyboard hook did not install")
	}
	return nil
}

// Close uninstalls the hook (on its owning thread, as UnhookWindowsHookEx
// requires) and stops a fallback thread this watcher owns. Best effort and
// idempotent: the system removes the hook when the process exits anyway.
// Safe on a nil watcher.
func (w *EscWatcher) Close() {
	if w == nil {
		return
	}
	if w.own != nil {
		if w.hook != 0 {
			_ = w.own.do(w.unhook)
		}
		w.own.close()
		w.own = nil
		return
	}
	if w.hook != 0 && w.post != nil {
		_ = w.post(w.unhook)
	}
}

// unhook removes the hook. Runs on the UI thread.
func (w *EscWatcher) unhook() {
	if w.hook != 0 {
		procUnhookWindowsHookEx.Call(w.hook)
		w.hook = 0
	}
}

// Arm enters an armed scope: from here until the matching Disarm, a
// physical Esc press is latched (Stopped reports it) and swallowed (the
// controlled app never sees the key). Scopes nest — a Batch arms around
// its steps' Actions. Safe on a nil watcher (the helper's no-Esc
// degradation).
func (w *EscWatcher) Arm() {
	if w != nil {
		w.latch.arm()
	}
}

// Disarm leaves an armed scope. The outermost Disarm clears the latched
// press, so a stop never carries over into the next command. Safe on a
// nil watcher.
func (w *EscWatcher) Disarm() {
	if w != nil {
		w.latch.disarm()
	}
}

// ArmPassive enters a passive scope (a Headless Action or an all-Headless
// Batch, ADR 0010): until the matching DisarmPassive, a physical Esc press
// is latched (Stopped reports it) but reaches the app the user is typing
// in. An Arm scope open at the same time wins and swallows. Safe on a nil
// watcher.
func (w *EscWatcher) ArmPassive() {
	if w != nil {
		w.latch.armPassive()
	}
}

// DisarmPassive leaves a passive scope. The latch clears when no scope of
// either kind remains. Safe on a nil watcher.
func (w *EscWatcher) DisarmPassive() {
	if w != nil {
		w.latch.disarmPassive()
	}
}

// Stopped reports whether the user pressed the physical Esc key during
// the armed (or passive) scope. A nil watcher never stopped.
func (w *EscWatcher) Stopped() bool {
	if w == nil {
		return false
	}
	return w.latch.stopped()
}

// ErrEscStopped is returned by input injection aborted mid-way by the
// user's Esc press: the events already injected landed; nothing further
// is sent. The helper maps it to the exit-130 stop.
var ErrEscStopped = errors.New("the user pressed Esc")
