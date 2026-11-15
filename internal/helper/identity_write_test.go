package helper

// The identity re-check at the write (reliability-council ticket 09): a
// Window whose handle passes to another process between the command's
// start check and its write is refused with nothing sent, on every route
// that writes to the Window's handle (Headed input, the Headless pattern
// call, posted clicks and keys, the dialog and edit routes, invoke-menu's
// messages, the Chromium route, the Window Actions); a Window that stays
// the same acts as before. The identities are a fake that hands the
// handle over right after the start check.

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/win"
)

// handleReuse is the desktop's Window identities with one handle reused
// mid-command: once armed, the first read of id (checkWindow's, at the
// command's start) sees the old Window and every later read the new one,
// which took the handle while the command waited or read. Other Windows,
// and every read before arm, keep their identity.
type handleReuse struct {
	mu    sync.Mutex
	id    uint64
	armed bool
	reads int
}

func (h *handleReuse) arm() {
	h.mu.Lock()
	h.armed, h.reads = true, 0
	h.mu.Unlock()
}

func (h *handleReuse) identity(hwnd uint64) (win.WindowOwner, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if hwnd != h.id {
		return win.WindowOwner{PID: 3, Started: 300, Class: "Other"}, true
	}
	if !h.armed {
		return notepadOwner, true
	}
	h.reads++
	if h.reads == 1 {
		return notepadOwner, true
	}
	return reusedOwner, true
}

// reuseAt wires a handle reuse of id into svc, armed.
func reuseAt(svc *Services, id uint64) *handleReuse {
	h := &handleReuse{id: id}
	svc.WindowIdentity = h.identity
	h.arm()
	return h
}

// wantReusedAtWrite fails unless the command was refused by the re-check
// at the write, typed not delivered.
func wantReusedAtWrite(t *testing.T, what string, out string, e *Error, id uint64) {
	t.Helper()
	msg := fmt.Sprintf("window %d closed during this command; a new window reused its ID; nothing was sent", id)
	if out != "" || e == nil || e.Msg != msg || !strings.Contains(e.Hint, "winuse list-windows") || e.Delivery != protocol.DeliveryNotDelivered {
		t.Errorf("%s: %q, %+v; want the refusal %q, not delivered", what, out, e, msg)
	}
}

// noStep fails if any of the write steps ran.
func noStep(t *testing.T, what string, steps []string, writes ...string) {
	t.Helper()
	for _, w := range writes {
		if containsStep(steps, w) {
			t.Errorf("%s ran %q on the reused handle: %v", what, w, steps)
		}
	}
}

func TestReusedAtTheWriteHeaded(t *testing.T) {
	// A click on an Element: refused before the activation, the first write.
	a := &actionSpy{}
	svc := a.services()
	reuseAt(&svc, 101)
	out, e := runCommand(t, CommandsWith(svc), "click", headedArgs(clickArgs("101", 2, 1, "left")))
	wantReusedAtWrite(t, "headed click", out, e, 101)
	noStep(t, "headed click", a.recorded(), "activate", "point", "click")

	// Typing and keys.
	k := &keySpy{}
	svc = k.services()
	reuseAt(&svc, 101)
	out, e = runCommand(t, CommandsWith(svc), "type-text", headedArgs(typeTextArgs("101", "hello")))
	wantReusedAtWrite(t, "headed type-text", out, e, 101)
	k2 := &keySpy{}
	svc = k2.services()
	reuseAt(&svc, 101)
	out, e = runCommand(t, CommandsWith(svc), "press-key", headedArgs(pressKeyArgs("101", "Control_L+s")))
	wantReusedAtWrite(t, "headed press-key", out, e, 101)
	noStep(t, "headed keys", append(k.recorded(), k2.recorded()...), "activate", "type", "press")

	// A coordinate Action: the screenshot is read first, then the handle
	// passes to another Window before the drag.
	p := newPointerSpy(t)
	h := &handleReuse{id: 101}
	p.svc.WindowIdentity = h.identity
	cmds := CommandsWith(p.svc)
	p.recordScreenshot(t, cmds)
	h.arm()
	out, e = runJSON(t, cmds, "drag", protocol.DragArgs{Window: "101", FromX: 100, FromY: 50, ToX: 400, ToY: 235, Headed: true})
	wantReusedAtWrite(t, "headed drag", out, e, 101)
	if p.activations != 0 || p.dragged != [5]int32{} {
		t.Errorf("headed drag: activated %d times, dragged %v", p.activations, p.dragged)
	}
}

func TestReusedAtTheWriteHeadless(t *testing.T) {
	// The pattern call on an Element.
	spy := newHeadlessSpy()
	reuseAt(&spy.svc, 101)
	out, e := runCommand(t, CommandsWith(spy.svc), "click", clickArgs("101", 2, 1, "left"))
	wantReusedAtWrite(t, "headless click", out, e, 101)
	noStep(t, "headless click", spy.recorded(), "headless-click")

	// A posted click and posted keys.
	ps := newPostSpy()
	reuseAt(&ps.svc, 101)
	out, e = runCommand(t, CommandsWith(ps.svc), "click", protocol.ClickArgs{Window: "101", Point: pt(40, 50), Post: true})
	wantReusedAtWrite(t, "posted click", out, e, 101)
	noStep(t, "posted click", ps.recorded(), "post-click")

	ks := newKeyRouteSpy(win.KeyTarget{})
	reuseAt(&ks.svc, 101)
	out, e = runCommand(t, CommandsWith(ks.svc), "press-key", protocol.PressKeyArgs{Window: "101", Key: "Return", Post: true})
	wantReusedAtWrite(t, "posted press-key", out, e, 101)
	noStep(t, "posted press-key", ks.recorded(), "post-keys")

	// The dialog's WM_COMMAND and the edit's EM_REPLACESEL.
	ks = newKeyRouteSpy(dialogTarget())
	reuseAt(&ks.svc, 101)
	out, e = runCommand(t, CommandsWith(ks.svc), "press-key", pressKeyArgs("101", "Return"))
	wantReusedAtWrite(t, "dialog press-key", out, e, 101)
	noStep(t, "dialog press-key", ks.recorded(), "dialog-command")

	ks = newKeyRouteSpy(editTarget(plainEdit))
	ks.states = []win.EditState{{Length: 5, SelStart: 5, SelEnd: 5, SelKnown: true}}
	reuseAt(&ks.svc, 101)
	out, e = runCommand(t, CommandsWith(ks.svc), "type-text", typeTextArgs("101", "hello"))
	wantReusedAtWrite(t, "edit type-text", out, e, 101)
	noStep(t, "edit type-text", ks.recorded(), "replace-sel")

	// invoke-menu: refused before WM_INITMENUPOPUP, its first message.
	ms := newMenuSpy()
	reuseAt(&ms.svc, 101)
	out, e = runCommand(t, CommandsWith(ms.svc), "invoke-menu", invokeMenuArgs("101", "File", "Save As"))
	wantReusedAtWrite(t, "invoke-menu", out, e, 101)
	noStep(t, "invoke-menu", ms.recorded(), "init-menu-popup", "menu-command")

	// The Chromium route's mouse messages.
	cs := newChromeSpy(false)
	reuseAt(&cs.svc, chromeID)
	two := 2
	out, e = runCommand(t, CommandsWith(cs.svc), "click", protocol.ClickArgs{Window: "401", ElementIndex: &two})
	wantReusedAtWrite(t, "chromium click", out, e, chromeID)
	noStep(t, "chromium click", cs.recorded(), "chromium-click")
}

func TestReusedAtTheWriteWindowActions(t *testing.T) {
	ws := newWindowSpy()
	reuseAt(&ws.svc, 101)
	out, e := runCommand(t, CommandsWith(ws.svc), "close-window", protocol.WindowActionArgs{Window: "101"})
	wantReusedAtWrite(t, "close-window", out, e, 101)
	if !ws.open {
		t.Error("close-window closed the reused handle's Window")
	}

	ws = newWindowSpy()
	reuseAt(&ws.svc, 101)
	out, e = runCommand(t, CommandsWith(ws.svc), "move-window", protocol.MoveWindowArgs{Window: "101", X: 40, Y: 40})
	wantReusedAtWrite(t, "move-window", out, e, 101)
	noStep(t, "window actions", ws.recorded(), "close", "move")
}

func TestReusedAtTheWriteBatchStepWindow(t *testing.T) {
	// A step's own Window is checked when the Batch starts and again at
	// its step's write; the Batch's Window acts.
	spy := newHeadlessSpy()
	reuseAt(&spy.svc, 203)
	out, e := runCommand(t, CommandsWith(spy.svc), "batch", protocol.BatchArgs{Window: "101", Steps: []protocol.Step{
		stepOf("click", protocol.ClickArgs{ElementIndex: idx(2)}),
		inStep("203", "click", protocol.ClickArgs{ElementIndex: idx(3)}),
	}})
	all := out
	if e != nil {
		all += e.Msg + "\n" + e.Out
	}
	if !strings.Contains(all, "Action 1 (click): ok") || !strings.Contains(all, "window 203 closed during this command; a new window reused its ID; nothing was sent") {
		t.Errorf("batch: %q, %+v; want Action 1 ok and Action 2 refused at its write", out, e)
	}
	if n := strings.Count(strings.Join(spy.recorded(), ","), "headless-click"); n != 1 {
		t.Errorf("clicked %d times, want Action 1's only: %v", n, spy.recorded())
	}
}

func TestReusedAtTheWriteDropsTheOldRecord(t *testing.T) {
	// The refused handle's record goes, so the next list or read records
	// the new Window and the agent can act on it.
	spy := newHeadlessSpy()
	h := &handleReuse{id: 101}
	spy.svc.WindowIdentity = h.identity
	cmds := CommandsWith(spy.svc)
	runCommand(t, cmds, "list-windows", nil)
	h.arm()
	_, e := runCommand(t, cmds, "click", clickArgs("101", 2, 1, "left"))
	wantReusedAtWrite(t, "click", "", e, 101)
	runCommand(t, cmds, "list-windows", nil) // reads the new Window from now on
	if out, e := runCommand(t, cmds, "click", clickArgs("101", 2, 1, "left")); e != nil || out != "ok\n" || !clicked(spy) {
		t.Errorf("after listing again: %q, %+v", out, e)
	}
}

func TestSameWindowAtTheWriteActs(t *testing.T) {
	// No reuse: the re-check passes and every route writes as before.
	spy, _ := identitySpy()
	cmds := CommandsWith(spy.svc)
	runCommand(t, cmds, "list-windows", nil)
	if out, e := runCommand(t, cmds, "click", clickArgs("101", 2, 1, "left")); e != nil || out != "ok\n" || !clicked(spy) {
		t.Errorf("headless click: %q, %+v", out, e)
	}

	a := &actionSpy{}
	svc := a.services()
	svc.WindowIdentity = fakeOwners{101: notepadOwner}.identity
	if out, e := runCommand(t, CommandsWith(svc), "click", headedArgs(clickArgs("101", 2, 1, "left"))); e != nil || out != "ok\n" {
		t.Errorf("headed click: %q, %+v", out, e)
	}
	if want := []string{"locked", "integrity", "activate", "point", "click"}; !equalStrings(a.recorded(), want) {
		t.Errorf("headed click steps = %v, want %v", a.recorded(), want)
	}

	ws := newWindowSpy()
	ws.svc.WindowIdentity = fakeOwners{101: notepadOwner}.identity
	if out, e := runCommand(t, CommandsWith(ws.svc), "close-window", protocol.WindowActionArgs{Window: "101"}); e != nil || out != "ok\n" || ws.open {
		t.Errorf("close-window: %q, %+v, still open %v", out, e, ws.open)
	}
}
