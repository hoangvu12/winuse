package helper

// invoke-menu's offline tests (headless-max ticket 08): the name matching,
// the walk down a faked menu bar (WM_INITMENUPOPUP before each submenu is
// read, the grey check at every level), the refusals that say "nothing was
// sent", the posted WM_COMMAND, the Agent Cursor on the menu bar item, the
// Esc stop, and the Batch's scope. Everything Win32 is faked; no desktop
// is touched.

import (
	"errors"
	"strings"
	"testing"

	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/win"
)

// The faked menu bar of window 101:
//
//	&File   > &New\tCtrl+N, &Open..., ---, Save &As...\tCtrl+Shift+S, &Recent > one.txt, E&xit
//	&Edit   > Select &All\tCtrl+A, &Paste (greyed), Big (ID 70000)
//	&Help (greyed) > &About
const (
	fakeBar    = 0x10
	fakeFile   = 0x20
	fakeRecent = 0x21
	fakeEdit   = 0x30
	fakeHelp   = 0x40
)

func fakeMenus() map[uint64][]win.MenuItem {
	return map[uint64][]win.MenuItem{
		fakeBar: {
			{Pos: 0, Text: "&File", Flags: win.MenuPopup, Submenu: fakeFile},
			{Pos: 1, Text: "&Edit", Flags: win.MenuPopup, Submenu: fakeEdit},
			{Pos: 2, Text: "&Help", Flags: win.MenuPopup | win.MenuGrayed, Submenu: fakeHelp},
		},
		fakeFile: {
			{Pos: 0, Text: "&New\tCtrl+N", ID: 101},
			{Pos: 1, Text: "&Open...", ID: 102},
			{Pos: 2, Flags: win.MenuSeparator},
			{Pos: 3, Text: "Save &As...\tCtrl+Shift+S", ID: 103},
			{Pos: 4, Text: "&Recent", Flags: win.MenuPopup, Submenu: fakeRecent},
			{Pos: 5, Text: "E&xit", ID: 105},
		},
		fakeRecent: {{Pos: 0, Text: "one.txt", ID: 201}},
		fakeEdit: {
			{Pos: 0, Text: "Select &All\tCtrl+A", ID: 301},
			{Pos: 1, Text: "&Paste", ID: 302, Flags: win.MenuGrayed},
			{Pos: 2, Text: "Big", ID: 70000},
		},
		fakeHelp: {{Pos: 0, Text: "&About", ID: 401}},
	}
}

// fakeBarItem is the rectangle the faked GetMenuItemRect reports for the
// menu bar item at pos.
func fakeBarItem(pos int) win.Rect {
	return win.Rect{Left: 100 + int32(pos)*50, Top: 130, Right: 140 + int32(pos)*50, Bottom: 150}
}

// menuSpy is a keyRouteSpy whose menu seams are faked: the menus by HMENU,
// the init messages and the posted commands recorded in order.
type menuSpy struct {
	*keyRouteSpy
	bar      uint64
	menus    map[uint64][]win.MenuItem
	readErr  error
	initErr  error
	inits    []uint64
	commands []uint16
	cmdErr   error
}

func newMenuSpy() *menuSpy {
	s := &menuSpy{keyRouteSpy: newKeyRouteSpy(win.KeyTarget{}), bar: fakeBar, menus: fakeMenus()}
	s.svc.MenuBar = func(hwnd uint64) uint64 { return s.bar }
	s.svc.ReadMenu = func(menu uint64) ([]win.MenuItem, error) {
		if s.readErr != nil {
			return nil, s.readErr
		}
		return s.menus[menu], nil
	}
	s.svc.InitMenuPopup = func(hwnd, submenu uint64, pos int) error {
		s.step("init-menu-popup")
		s.inits = append(s.inits, submenu)
		return s.initErr
	}
	s.svc.MenuItemRect = func(hwnd uint64, pos int) (win.Rect, bool) { return fakeBarItem(pos), true }
	s.svc.MenuCommand = func(hwnd uint64, id uint16) error {
		s.step("menu-command")
		s.commands = append(s.commands, id)
		s.esc.pressIf("act")
		return s.cmdErr
	}
	return s
}

func invokeMenuArgs(window string, path ...string) protocol.InvokeMenuArgs {
	return protocol.InvokeMenuArgs{Window: window, Path: path}
}

func TestMenuNameAndMatching(t *testing.T) {
	for in, want := range map[string]string{
		"Save &As...\tCtrl+Shift+S": "Save As...",
		"&File":                     "File",
		"Save && E&xit":             "Save & Exit",
		"  Zoom   &In ":             "Zoom In",
	} {
		if got := menuName(in); got != want {
			t.Errorf("menuName(%q) = %q, want %q", in, got, want)
		}
	}

	items := []win.MenuItem{
		{Pos: 0, Text: "&Save\tCtrl+S", ID: 1},
		{Pos: 1, Text: "Save &As...", ID: 2},
		{Pos: 2, Flags: win.MenuSeparator},
		{Pos: 3, Text: "Save && E&xit", ID: 3},
		{Pos: 4, Text: "", ID: 4}, // owner-drawn: no text
	}
	cases := []struct {
		name string
		want []uint32
	}{
		{"save", []uint32{1}},         // any case, without & and the shortcut
		{"Save As...", []uint32{2}},   // exact
		{"save as", []uint32{2}},      // the ellipsis may be left out
		{"Save\tCtrl+S", []uint32{1}}, // a shortcut copied along is dropped
		{"Save & Exit", []uint32{3}},  // the agent's & is literal
		{"Save as......", nil},        // only one trailing ellipsis is ignored
		{"Open", nil},                 // no such item
		{"", nil},                     // the separator and the textless item never match
	}
	for _, c := range cases {
		var got []uint32
		for _, it := range matchMenuItems(items, c.name) {
			got = append(got, it.ID)
		}
		if len(got) != len(c.want) || (len(got) > 0 && got[0] != c.want[0]) {
			t.Errorf("matchMenuItems(%q) = %v, want %v", c.name, got, c.want)
		}
	}

	// Exact names win over ellipsis matches: "Print" is not ambiguous with
	// "Print...".
	both := []win.MenuItem{{Pos: 0, Text: "&Print", ID: 1}, {Pos: 1, Text: "Print...", ID: 2}}
	if got := matchMenuItems(both, "print"); len(got) != 1 || got[0].ID != 1 {
		t.Errorf("matchMenuItems(print) = %+v, want only the exact item", got)
	}
	if got := matchMenuItems(both, "print..."); len(got) != 1 || got[0].ID != 2 {
		t.Errorf("matchMenuItems(print...) = %+v, want only the exact item", got)
	}
}

func TestInvokeMenuPostsTheCommand(t *testing.T) {
	spy := newMenuSpy()
	out, cmdErr := runCommand(t, CommandsWith(spy.svc), "invoke-menu", invokeMenuArgs("101", "file", "save as"))
	if cmdErr != nil {
		t.Fatalf("invoke-menu: %v", cmdErr.Error())
	}
	if out != "ok\n" {
		t.Errorf("output = %q, want ok", out)
	}
	want := []string{"locked", "integrity", "ready", "init-menu-popup", "cursor-travel", "menu-command", "cursor-click"}
	if got := spy.recorded(); !equalStrings(got, want) {
		t.Errorf("steps = %v, want %v", got, want)
	}
	assertNoV1(t, spy.recorded())
	if len(spy.inits) != 1 || spy.inits[0] != fakeFile {
		t.Errorf("WM_INITMENUPOPUP went to %#x, want the File submenu only", spy.inits)
	}
	if len(spy.commands) != 1 || spy.commands[0] != 103 {
		t.Errorf("commands = %v, want WM_COMMAND(103)", spy.commands)
	}
	if got := spy.esc.brackets(); !equalStrings(got, []string{"arm-passive", "disarm-passive"}) {
		t.Errorf("Esc brackets = %v, want one passive scope", got)
	}
	c := spy.cursor
	if len(c.labels) != 1 || c.labels[0] != `menu "File > Save As..."` {
		t.Errorf("cursor labels = %q, want the menu's own names", c.labels)
	}
	if len(c.targets) != 1 || c.targets[0] != fakeBarItem(0) || len(c.clicks) != 1 || c.clicks[0] != fakeBarItem(0) {
		t.Errorf("cursor targets %v clicks %v, want the File bar item", c.targets, c.clicks)
	}

	// Two levels of submenus: each is initialized before it is read.
	spy = newMenuSpy()
	if _, cmdErr := runCommand(t, CommandsWith(spy.svc), "invoke-menu", invokeMenuArgs("101", "File", "Recent", "one.txt")); cmdErr != nil {
		t.Fatalf("invoke-menu: %v", cmdErr.Error())
	}
	if len(spy.inits) != 2 || spy.inits[0] != fakeFile || spy.inits[1] != fakeRecent {
		t.Errorf("inits = %#x, want File then Recent", spy.inits)
	}
	if len(spy.commands) != 1 || spy.commands[0] != 201 {
		t.Errorf("commands = %v, want WM_COMMAND(201)", spy.commands)
	}
}

func TestInvokeMenuReadsTheMenuAsItIsWhenTheInitFails(t *testing.T) {
	spy := newMenuSpy()
	spy.initErr = errors.New("SendMessageTimeout(0x117) failed")
	if _, cmdErr := runCommand(t, CommandsWith(spy.svc), "invoke-menu", invokeMenuArgs("101", "Edit", "Select All")); cmdErr != nil {
		t.Fatalf("invoke-menu: %v", cmdErr.Error())
	}
	if len(spy.commands) != 1 || spy.commands[0] != 301 {
		t.Errorf("commands = %v, want WM_COMMAND(301)", spy.commands)
	}
}

func TestInvokeMenuRefusalsSayNothingWasSent(t *testing.T) {
	cases := []struct {
		name  string
		path  []string
		setup func(s *menuSpy)
		want  []string
		inits int // WM_INITMENUPOPUP messages sent before the refusal
	}{
		{"no menu bar", []string{"File", "Save"}, func(s *menuSpy) { s.bar = 0 },
			[]string{"window 101 has no classic menu bar", "click the menu Elements instead"}, 0},
		{"an unknown bar item", []string{"Fiel", "Save"}, nil,
			[]string{`the menu bar has no item "Fiel"; its items are "File", "Edit", "Help"`}, 0},
		{"an unknown item", []string{"File", "Save"}, nil,
			[]string{`"File" has no item "Save"; its items are "New", "Open...", "Save As...", "Recent", "Exit"`}, 1},
		{"a greyed command", []string{"Edit", "Paste"}, nil,
			[]string{`the menu item "Edit > Paste" is greyed out`, "does not offer this command now"}, 1},
		{"a greyed menu", []string{"Help", "About"}, nil,
			[]string{`the menu item "Help" is greyed out`}, 0},
		{"a command mid-path", []string{"File", "Exit", "Now"}, nil,
			[]string{`"File > Exit" is a command with no submenu, so "Now" cannot follow it`}, 1},
		{"a submenu at the end", []string{"File", "Recent"}, nil,
			[]string{`"File > Recent" opens a submenu, not a command; its items are "one.txt"`}, 2},
		{"a bar item at the end", []string{"File"}, nil,
			[]string{`"File" opens a submenu, not a command`}, 1},
		{"an ID WM_COMMAND cannot carry", []string{"Edit", "Big"}, nil,
			[]string{"command ID 70000, which WM_COMMAND cannot carry"}, 1},
		{"an unreadable menu", []string{"File", "New"}, func(s *menuSpy) { s.readErr = errors.New("menu 0x10 cannot be read") },
			[]string{"menu 0x10 cannot be read"}, 0},
		{"two items of one name", []string{"File", "New"}, func(s *menuSpy) {
			s.menus[fakeFile] = append(s.menus[fakeFile], win.MenuItem{Pos: 6, Text: "Ne&w", ID: 106})
		}, []string{`"File" has 2 items named "New"`}, 1},
		{"locked desktop", []string{"File", "New"}, func(s *menuSpy) { s.locked, s.lockedDesktop = true, "Winlogon" },
			[]string{"locked"}, 0},
		{"administrator Window", []string{"File", "New"}, func(s *menuSpy) { s.refuse = &win.AdminWindowError{WindowID: 101} },
			[]string{"administrator"}, 0},
		{"modal dialog", []string{"File", "New"}, func(s *menuSpy) { s.readyErr = &win.ModalDialogError{WindowID: 101, DialogID: 555} },
			[]string{"modal dialog"}, 0},
		{"post failed", []string{"File", "New"}, func(s *menuSpy) { s.cmdErr = errors.New("PostMessage(WM_COMMAND) failed") },
			[]string{"PostMessage"}, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spy := newMenuSpy()
			if tc.setup != nil {
				tc.setup(spy)
			}
			_, cmdErr := runCommand(t, CommandsWith(spy.svc), "invoke-menu", invokeMenuArgs("101", tc.path...))
			if cmdErr == nil {
				t.Fatal("invoke-menu succeeded, want a refusal")
			}
			for _, w := range tc.want {
				if !strings.Contains(cmdErr.Msg+" | "+cmdErr.Hint, w) {
					t.Errorf("error = %q | %q, want %q", cmdErr.Msg, cmdErr.Hint, w)
				}
			}
			if !strings.HasSuffix(cmdErr.Msg, nothingSentNote) {
				t.Errorf("message = %q, want it to end with %q", cmdErr.Msg, nothingSentNote)
			}
			if len(spy.inits) != tc.inits {
				t.Errorf("sent %d WM_INITMENUPOPUP, want %d", len(spy.inits), tc.inits)
			}
			if tc.name != "post failed" && len(spy.commands) != 0 {
				t.Errorf("posted %v on a refusal", spy.commands)
			}
			if tc.name != "post failed" && containsStep(spy.recorded(), "cursor-travel") {
				t.Errorf("the Agent Cursor travelled before a refusal: %v", spy.recorded())
			}
			assertNoV1(t, spy.recorded())
		})
	}
}

func TestInvokeMenuArgumentFailures(t *testing.T) {
	for _, path := range [][]string{nil, {"File", " "}} {
		spy := newMenuSpy()
		_, cmdErr := runCommand(t, CommandsWith(spy.svc), "invoke-menu", invokeMenuArgs("101", path...))
		if cmdErr == nil || !strings.Contains(cmdErr.Hint, "usage: winuse invoke-menu") {
			t.Errorf("path %q: err = %v, want a usage failure", path, cmdErr)
		}
		if len(spy.recorded()) != 0 {
			t.Errorf("path %q: steps = %v, want none", path, spy.recorded())
		}
	}
}

func TestInvokeMenuEscStop(t *testing.T) {
	spy := newMenuSpy()
	spy.esc.pressOn = "travel"
	_, cmdErr := runCommand(t, CommandsWith(spy.svc), "invoke-menu", invokeMenuArgs("101", "File", "New"))
	if cmdErr == nil || cmdErr.Exit != protocol.ExitEsc || !strings.Contains(cmdErr.Hint, "nothing was sent") {
		t.Fatalf("err = %v, want the Esc stop before the post", cmdErr)
	}
	if len(spy.commands) != 0 {
		t.Errorf("posted %v after an Esc stop during the travel", spy.commands)
	}

	spy = newMenuSpy()
	spy.esc.pressOn = "act"
	_, cmdErr = runCommand(t, CommandsWith(spy.svc), "invoke-menu", invokeMenuArgs("101", "File", "New"))
	if cmdErr == nil || cmdErr.Exit != protocol.ExitEsc || strings.Contains(cmdErr.Hint, "nothing was sent") {
		t.Fatalf("err = %v, want the plain Esc stop after the post", cmdErr)
	}
}

func TestHeadlessStepInvokeMenu(t *testing.T) {
	if !headlessStep("invoke-menu", batchStepArgs(invokeMenuArgs("101", "File", "New")), nil) {
		t.Error("invoke-menu with a path must be Headless")
	}
	if headlessStep("invoke-menu", batchStepArgs(invokeMenuArgs("101")), nil) {
		t.Error("invoke-menu with no path fails, so it must not count as Headless")
	}
}

func TestBatchInvokeMenuKeepsTheHeadlessScope(t *testing.T) {
	spy := newMenuSpy()
	ov := &overlaySpy{share: spy.step}
	spy.svc.Overlay = ov
	out, cmdErr := runCommand(t, CommandsWith(spy.svc), "batch", protocol.BatchArgs{
		Window: "101",
		Steps: []protocol.Step{
			stepOf("click", protocol.ClickArgs{ElementIndex: intPtrBatch(2)}),
			stepOf("invoke-menu", protocol.InvokeMenuArgs{Path: []string{"Edit", "Select All"}}),
		},
	})
	if cmdErr != nil || out != "Action 1 (click): ok\nAction 2 (invoke-menu): ok\n" {
		t.Fatalf("batch: %q, %v", out, cmdErr)
	}
	assertNoV1(t, spy.recorded())
	if b := spy.esc.brackets(); containsStep(b, "arm") || len(b) == 0 || b[0] != "arm-passive" {
		t.Errorf("Esc brackets = %v, want the passive scope", b)
	}
	if len(spy.commands) != 1 || spy.commands[0] != 301 {
		t.Errorf("commands = %v, want WM_COMMAND(301)", spy.commands)
	}

	// A headed Batch leaves invoke-menu Headless: it has no headed form.
	spy = newMenuSpy()
	if _, cmdErr := runCommand(t, CommandsWith(spy.svc), "batch", protocol.BatchArgs{
		Window: "101",
		Headed: true,
		Steps:  []protocol.Step{stepOf("invoke-menu", protocol.InvokeMenuArgs{Path: []string{"File", "New"}})},
	}); cmdErr != nil {
		t.Fatalf("headed batch: %v", cmdErr.Error())
	}
	if len(spy.commands) != 1 || spy.commands[0] != 101 {
		t.Errorf("commands = %v, want WM_COMMAND(101)", spy.commands)
	}
}
