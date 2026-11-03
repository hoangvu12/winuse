package helper

// press-key's shortcut lookup (headless-max ticket 06): a chord that a
// classic menu item or a UI Automation Element labels runs that command
// Headless; a disabled one is refused with nothing sent; none, or several
// enabled ones, fall through to press-key's other routes. Menus and the
// element side are faked; no desktop is touched.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/uia"
	"github.com/hoangvu12/winuse/internal/win"
)

func pressArgs(key string) protocol.PressKeyArgs {
	return protocol.PressKeyArgs{Window: "101", Key: key}
}

func TestAShortcutRunsItsClassicMenuCommand(t *testing.T) {
	cases := []struct {
		key  string
		id   uint16
		path string
	}{
		{"Control_L+n", 101, "File > New"},
		{"Control_L+Shift_L+s", 103, "File > Save As..."},
		{"Control_R+a", 301, "Edit > Select All"},
	}
	for _, c := range cases {
		spy := newMenuSpy()
		out, cmdErr := runCommand(t, CommandsWith(spy.svc), "press-key", pressArgs(c.key))
		if cmdErr != nil {
			t.Fatalf("%s: %v", c.key, cmdErr.Error())
		}
		if want := `ok (invoked "` + c.path + `" through its shortcut)` + "\n"; out != want {
			t.Errorf("%s: output = %q, want %q", c.key, out, want)
		}
		if len(spy.commands) != 1 || spy.commands[0] != c.id {
			t.Errorf("%s: commands = %v, want WM_COMMAND(%d)", c.key, spy.commands, c.id)
		}
		assertNoV1(t, spy.recorded())
		if b := spy.esc.brackets(); !equalStrings(b, []string{"arm-passive", "disarm-passive"}) {
			t.Errorf("%s: Esc brackets = %v, want one passive scope", c.key, b)
		}
	}
}

func TestADisabledShortcutIsRefused(t *testing.T) {
	spy := newMenuSpy()
	spy.menus[fakeEdit][1].Text = "&Paste\tCtrl+V" // greyed in fakeMenus
	_, cmdErr := runCommand(t, CommandsWith(spy.svc), "press-key", pressArgs("Control_L+v"))
	if cmdErr == nil || !strings.Contains(cmdErr.Msg, `runs "Edit > Paste" in window 101, which is disabled now`) || !strings.HasSuffix(cmdErr.Msg, nothingSentNote) {
		t.Fatalf("err = %v, want the disabled refusal", cmdErr)
	}
	if len(spy.commands) != 0 {
		t.Errorf("posted %v for a disabled command", spy.commands)
	}
}

func TestAnUnlabelledChordFallsThrough(t *testing.T) {
	spy := newMenuSpy()
	_, cmdErr := runCommand(t, CommandsWith(spy.svc), "press-key", pressArgs("Control_L+q"))
	if cmdErr == nil || !strings.Contains(cmdErr.Msg, "has no background route") {
		t.Fatalf("err = %v, want press-key's own refusal", cmdErr)
	}
	if len(spy.commands) != 0 {
		t.Errorf("posted %v for an unlabelled chord", spy.commands)
	}
	// Two enabled items with one shortcut: never a guess.
	spy = newMenuSpy()
	spy.menus[fakeFile][0].Text = "&New\tCtrl+N"
	spy.menus[fakeFile][1].Text = "&Open...\tCtrl+N"
	_, cmdErr = runCommand(t, CommandsWith(spy.svc), "press-key", pressArgs("Control_L+n"))
	if cmdErr == nil || len(spy.commands) != 0 {
		t.Errorf("two matches: err %v, commands %v; want the fall-through refusal and nothing posted", cmdErr, spy.commands)
	}
}

// accelSpy is a keyRouteSpy whose element side finds shortcuts.
type accelSpy struct {
	*keyRouteSpy
	res    uia.AccelResult
	err    error
	calls  int
	expand bool
	shown  func(string, string, bool) bool
}

func (s *accelSpy) InvokeAccelerator(ctx context.Context, hwnd uint64, shown func(string, string, bool) bool, expandMenus bool) (uia.AccelResult, error) {
	s.step("accelerator")
	s.calls++
	s.expand, s.shown = expandMenus, shown
	return s.res, s.err
}

func newAccelSpy(res uia.AccelResult, err error) *accelSpy {
	s := &accelSpy{keyRouteSpy: newKeyRouteSpy(win.KeyTarget{}), res: res, err: err}
	s.svc.MenuBar = func(uint64) uint64 { return 0 } // no classic menu bar
	s.svc.Actions = s
	return s
}

func TestAShortcutRunsItsUIAElement(t *testing.T) {
	spy := newAccelSpy(uia.AccelResult{Invoked: true, Name: "Save", Enabled: 1}, nil)
	out, cmdErr := runCommand(t, CommandsWith(spy.svc), "press-key", pressArgs("Control_L+s"))
	if cmdErr != nil || out != `ok (invoked "Save" through its shortcut)`+"\n" {
		t.Fatalf("press-key: %q, %v", out, cmdErr)
	}
	if !spy.shown("Ctrl+S", "Save", true) || spy.shown("Ctrl+O", "Open", true) || !spy.shown("", "Save\tCtrl+S", true) {
		t.Error("the matcher passed to the element side does not match Ctrl+S labels only")
	}
	if spy.expand {
		t.Error("a Window that is neither WinForms nor WinUI 3 must not expand its menus")
	}
	steps := spy.recorded()
	assertNoV1(t, steps)
	if !containsStep(steps, "cursor-click") {
		t.Errorf("steps = %v, want the cursor's click effect", steps)
	}
}

func TestUIAShortcutOutcomes(t *testing.T) {
	spy := newAccelSpy(uia.AccelResult{Disabled: true, Name: "Paste"}, nil)
	_, cmdErr := runCommand(t, CommandsWith(spy.svc), "press-key", pressArgs("Control_L+v"))
	if cmdErr == nil || !strings.Contains(cmdErr.Msg, `runs "Paste"`) || !strings.HasSuffix(cmdErr.Msg, nothingSentNote) {
		t.Errorf("disabled: %v", cmdErr)
	}
	for _, res := range []uia.AccelResult{{}, {Enabled: 2}} {
		spy = newAccelSpy(res, nil)
		_, cmdErr = runCommand(t, CommandsWith(spy.svc), "press-key", pressArgs("Control_L+s"))
		if cmdErr == nil || !strings.Contains(cmdErr.Msg, "has no background route") {
			t.Errorf("%+v: err = %v, want the fall-through refusal", res, cmdErr)
		}
	}
	spy = newAccelSpy(uia.AccelResult{Name: "Save"}, &uia.WriteError{What: "invoking the Element", HR: -1})
	_, cmdErr = runCommand(t, CommandsWith(spy.svc), "press-key", pressArgs("Control_L+s"))
	if cmdErr == nil || !strings.Contains(cmdErr.Hint, "the command may have run") {
		t.Errorf("a failed invoke: %v", cmdErr)
	}
	spy = newAccelSpy(uia.AccelResult{}, errors.New("window gone"))
	if _, cmdErr = runCommand(t, CommandsWith(spy.svc), "press-key", pressArgs("Control_L+s")); cmdErr == nil {
		t.Error("a failed search must fail")
	}
	// A lone key and a dialog's Return never search.
	spy = newAccelSpy(uia.AccelResult{Invoked: true}, nil)
	runCommand(t, CommandsWith(spy.svc), "press-key", pressArgs("Return"))
	if spy.calls != 0 {
		t.Errorf("a lone key searched for a shortcut %d times", spy.calls)
	}
}

func TestShortcutExpandsMenusOnlyWhereMenusHideTheirItems(t *testing.T) {
	for class, want := range map[string]bool{
		"WindowsForms10.Window.8.app.0.1": true,
		"WinUIDesktopWin32WindowClass":    true,
		"HwndWrapper[App;;1]":             false, // WPF never labels shortcuts
		"Notepad":                         false,
		"":                                false,
	} {
		if got := shortcutExpandsMenus(class); got != want {
			t.Errorf("shortcutExpandsMenus(%q) = %v, want %v", class, got, want)
		}
	}
}
