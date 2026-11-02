package helper

// Offline tests for list-windows's details column and App filter
// (desktop-parity 22), over the Services fakes: the Windows, the
// foreground, IsZoomed and the integrity check are all faked, so no
// desktop is read.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/win"
)

// listWindowsServices fakes a desktop where 101 (Notepad) is focused, 202
// (Chrome) is maximized and 203 (Chrome) is minimized and elevated.
func listWindowsServices(integrityCalls *int) Services {
	s := testServices(&fakeTrees{})
	s.Windows = func() ([]win.WindowInfo, error) {
		ws := fakeWindows()
		ws[0].PID = 4312
		ws[1].PID, ws[1].Bounds = 900, win.Rect{Left: -8, Top: -8, Right: 1928, Bottom: 1048}
		ws[2].PID, ws[2].Minimized = 901, true
		ws[2].Bounds = win.Rect{Left: -32000, Top: -32000, Right: -31840, Bottom: -31972}
		return ws, nil
	}
	s.Foreground = func() uint64 { return 101 }
	s.Maximized = func(hwnd uint64) bool { return hwnd == 202 }
	s.Integrity = func(w win.WindowInfo) error {
		if integrityCalls != nil {
			*integrityCalls++
		}
		if w.ID == 203 {
			return &win.AdminWindowError{WindowID: w.ID, App: "chrome.exe"}
		}
		return nil
	}
	return s
}

func TestListWindowsDefaultIsTheCodexLine(t *testing.T) {
	// No arguments, null and {} all print Codex's list_windows lines and
	// read none of the details.
	calls := 0
	cmds := CommandsWith(listWindowsServices(&calls))
	want := "101\tNotepad\tUntitled - Notepad\n202\tChrome\tOne\n203\tChrome\tTwo\n"
	for _, raw := range []string{"", "null", "{}"} {
		out, cmdErr := cmds["list-windows"](context.Background(), json.RawMessage(raw))
		if cmdErr != nil {
			t.Fatalf("list-windows %q: %v", raw, cmdErr.Error())
		}
		if out != want {
			t.Errorf("list-windows %q:\n got %q\nwant %q", raw, out, want)
		}
	}
	if calls != 0 {
		t.Errorf("the plain listing ran %d integrity checks, want none", calls)
	}
}

func TestListWindowsDetails(t *testing.T) {
	out, cmdErr := runCommand(t, CommandsWith(listWindowsServices(nil)), "list-windows", protocol.ListWindowsArgs{Details: true})
	if cmdErr != nil {
		t.Fatalf("list-windows --details: %v", cmdErr.Error())
	}
	want := "101\tNotepad\t10,20 800x470 pid 4312 focused\tUntitled - Notepad\n" +
		"202\tChrome\t-8,-8 1936x1056 pid 900 maximized\tOne\n" +
		"203\tChrome\t-32000,-32000 160x28 pid 901 minimized admin\tTwo\n"
	if out != want {
		t.Errorf("output:\n got %q\nwant %q", out, want)
	}
}

func TestListWindowsDetailsUnreadableIntegrityIsAdmin(t *testing.T) {
	// An unreadable token is treated as elevated, as the Actions refuse
	// it; any other integrity failure (no pid) is not admin.
	s := listWindowsServices(nil)
	s.Integrity = func(w win.WindowInfo) error {
		switch w.ID {
		case 101:
			return &win.AdminWindowError{WindowID: w.ID, App: "notepad.exe", Unreadable: true}
		case 202:
			return errFake("cannot read the process of window 202")
		}
		return nil
	}
	out, cmdErr := runCommand(t, CommandsWith(s), "list-windows", protocol.ListWindowsArgs{Details: true})
	if cmdErr != nil {
		t.Fatalf("list-windows --details: %v", cmdErr.Error())
	}
	lines := strings.Split(out, "\n")
	if !strings.Contains(lines[0], " admin\t") {
		t.Errorf("unreadable integrity must read admin: %q", lines[0])
	}
	if strings.Contains(lines[1], "admin") || strings.Contains(lines[2], "admin") {
		t.Errorf("only an AdminWindowError is admin: %q", out)
	}
}

func TestListWindowsAppFilter(t *testing.T) {
	cmds := CommandsWith(listWindowsServices(nil))
	// The App display name and the executable name, any case: the
	// matching of a Window reference.
	for _, app := range []string{"Chrome", "chrome.exe", "CHROME", " chrome "} {
		out, cmdErr := runCommand(t, cmds, "list-windows", protocol.ListWindowsArgs{App: app})
		if cmdErr != nil {
			t.Fatalf("list-windows --app %q: %v", app, cmdErr.Error())
		}
		if want := "202\tChrome\tOne\n203\tChrome\tTwo\n"; out != want {
			t.Errorf("--app %q:\n got %q\nwant %q", app, out, want)
		}
	}
	// With details too.
	out, cmdErr := runCommand(t, cmds, "list-windows", protocol.ListWindowsArgs{App: "notepad", Details: true})
	if cmdErr != nil {
		t.Fatalf("list-windows --details --app notepad: %v", cmdErr.Error())
	}
	if want := "101\tNotepad\t10,20 800x470 pid 4312 focused\tUntitled - Notepad\n"; out != want {
		t.Errorf("--details --app notepad:\n got %q\nwant %q", out, want)
	}
	// A partial name or a title never matches; an App with no Window fails.
	for _, app := range []string{"Chr", "Untitled - Notepad", "Paint"} {
		_, cmdErr := runCommand(t, cmds, "list-windows", protocol.ListWindowsArgs{App: app})
		if cmdErr == nil || !strings.Contains(cmdErr.Error(), "has no open Windows") {
			t.Errorf("--app %q: %v, want the no-Windows failure", app, cmdErr)
		}
	}
}

func TestListWindowsRejectsUnknownArguments(t *testing.T) {
	cmds := CommandsWith(listWindowsServices(nil))
	for _, raw := range []string{`{"unexpected":1}`, `{"details":"yes"}`, `[1]`} {
		out, cmdErr := cmds["list-windows"](context.Background(), json.RawMessage(raw))
		if out != "" || cmdErr == nil {
			t.Errorf("list-windows %s: got %q, %v; want a refusal", raw, out, cmdErr)
		}
	}
}

// errFake is a plain error for the integrity fake.
type errFake string

func (e errFake) Error() string { return string(e) }
