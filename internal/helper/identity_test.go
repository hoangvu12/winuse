package helper

// The Window identity check's offline tests (multi-agent 09): an Action on
// the Window an ID named when it was listed acts; one whose ID a new
// Window took is refused with nothing sent, and acts again once the agent
// lists the Windows; a Window with no record is recorded at its first
// Action; an App name is not checked; reads record and list-windows drops
// the records of Windows that are gone. The identities are a fake.

import (
	"strings"
	"testing"

	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/win"
)

// fakeOwners is the desktop's Window identities; a missing ID is a gone
// Window.
type fakeOwners map[uint64]win.WindowOwner

func (f fakeOwners) identity(hwnd uint64) (win.WindowOwner, bool) {
	o, ok := f[hwnd]
	return o, ok
}

var (
	notepadOwner = win.WindowOwner{PID: 7, Started: 1000, Class: "Notepad"}
	reusedOwner  = win.WindowOwner{PID: 31, Started: 5000, Class: "CabinetWClass"}
)

func identitySpy() (*headlessSpy, fakeOwners) {
	spy := newHeadlessSpy()
	owners := fakeOwners{101: notepadOwner, 202: {PID: 9, Started: 2000, Class: "Chrome_WidgetWin_1"}}
	spy.svc.WindowIdentity = owners.identity
	return spy, owners
}

func clicked(spy *headlessSpy) bool {
	return strings.Contains(strings.Join(spy.recorded(), ","), "headless-click")
}

func TestIdentitySameWindowActs(t *testing.T) {
	spy, _ := identitySpy()
	cmds := CommandsWith(spy.svc)
	if _, e := runCommand(t, cmds, "list-windows", nil); e != nil {
		t.Fatalf("list-windows: %+v", e)
	}
	if out, e := runCommand(t, cmds, "click", clickArgs("101", 2, 1, "left")); e != nil || out != "ok\n" || !clicked(spy) {
		t.Errorf("same Window: %q, %+v", out, e)
	}
}

func TestIdentityReusedWindowIDIsRefused(t *testing.T) {
	spy, owners := identitySpy()
	cmds := CommandsWith(spy.svc)
	runCommand(t, cmds, "list-windows", nil)
	owners[101] = reusedOwner // Notepad closed; Explorer got its handle
	_, e := runCommand(t, cmds, "click", clickArgs("101", 2, 1, "left"))
	if e == nil || e.Msg != "window 101 closed; a new window reused its ID; nothing was sent" || !strings.Contains(e.Hint, "winuse list-windows") {
		t.Errorf("reused ID: %+v", e)
	}
	if clicked(spy) {
		t.Fatal("the reused Window was clicked")
	}
	// The agent lists the Windows again: the new Window is recorded.
	runCommand(t, cmds, "list-windows", nil)
	if out, e := runCommand(t, cmds, "click", clickArgs("101", 2, 1, "left")); e != nil || out != "ok\n" {
		t.Errorf("after listing again: %q, %+v", out, e)
	}
}

func TestIdentityUnknownWindowIsRecordedAtItsFirstAction(t *testing.T) {
	spy, owners := identitySpy()
	cmds := CommandsWith(spy.svc)
	// No list, no read (a helper restart): the Action acts and records.
	if out, e := runCommand(t, cmds, "click", clickArgs("101", 2, 1, "left")); e != nil || out != "ok\n" {
		t.Fatalf("unknown Window: %q, %+v", out, e)
	}
	owners[101] = reusedOwner
	if _, e := runCommand(t, cmds, "click", clickArgs("101", 2, 1, "left")); e == nil || !strings.Contains(e.Msg, "reused its ID") {
		t.Errorf("after the first Action recorded it: %+v", e)
	}
}

func TestIdentityAppNameIsNotChecked(t *testing.T) {
	spy, owners := identitySpy()
	cmds := CommandsWith(spy.svc)
	runCommand(t, cmds, "list-windows", nil)
	owners[101] = reusedOwner
	// An App name resolves afresh: there is no old ID to have gone stale.
	if out, e := runCommand(t, cmds, "click", protocol.ClickArgs{Window: "Notepad", ElementIndex: idx(2)}); e != nil || out != "ok\n" {
		t.Errorf("by App name: %q, %+v", out, e)
	}
}

func TestIdentityReadsRecordAndListsPrune(t *testing.T) {
	trees := &fakeTrees{}
	s := testServices(trees)
	owners := fakeOwners{101: notepadOwner}
	s.WindowIdentity = owners.identity
	s.windowIDs = newWindowRecords()
	cmds := CommandsWith(s)

	if _, e := runCommand(t, cmds, "get-window-state", windowRef{Window: "101"}); e != nil {
		t.Fatalf("get-window-state: %+v", e)
	}
	if o, ok := s.windowIDs.lookup(101); !ok || o != notepadOwner {
		t.Errorf("a read records its Window: %+v, %v", o, ok)
	}
	// A list sees the handle naming another Window now: it records that.
	owners[101] = reusedOwner
	s.windowIDs.record(303, notepadOwner) // a Window that has since closed
	runCommand(t, cmds, "list-windows", nil)
	if o, _ := s.windowIDs.lookup(101); o != reusedOwner {
		t.Errorf("list-windows kept the old record: %+v", o)
	}
	if _, ok := s.windowIDs.lookup(303); ok {
		t.Error("list-windows kept the record of a gone Window")
	}
}

func TestIdentityGoneWindowIsNotRefusedHere(t *testing.T) {
	spy, owners := identitySpy()
	cmds := CommandsWith(spy.svc)
	runCommand(t, cmds, "list-windows", nil)
	delete(owners, 101) // the identity cannot be read: the check steps aside
	if _, e := runCommand(t, cmds, "click", clickArgs("101", 2, 1, "left")); e != nil && strings.Contains(e.Msg, "reused its ID") {
		t.Errorf("a gone Window was reported as reused: %+v", e)
	}
}
