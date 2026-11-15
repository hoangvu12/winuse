package helper

// launch-app's offline tests (headless-max ticket 01): the options reach
// the launcher, the refusals start nothing, and the note says when the
// App took the foreground. The desktop, the start and the foreground are
// faked; no process starts.

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/apps"
	"github.com/hoangvu12/winuse/internal/win"
)

const fakeAppPath = `C:\Apps\Tool\tool.exe`

// launchSpy fakes the desktop around one launch: the user's Window 500 is
// the foreground, and the start opens Window 300 of process 77.
type launchSpy struct {
	windows    []win.WindowInfo
	foreground uint64
	takesFG    bool // the App activates its Window when it starts
	starts     []startCall
	startErr   error
}

type startCall struct {
	spec apps.Spec
	opts win.LaunchOptions
}

func newLaunchSpy() *launchSpy {
	return &launchSpy{
		windows:    []win.WindowInfo{{ID: 500, PID: 9, ExePath: `C:\User\editor.exe`, App: "Editor", Title: "notes"}},
		foreground: 500,
	}
}

func (l *launchSpy) services() Services {
	return Services{
		Windows:    func() ([]win.WindowInfo, error) { return slices.Clone(l.windows), nil },
		Foreground: func() uint64 { return l.foreground },
		WindowPID: func(hwnd uint64) uint32 {
			for _, w := range l.windows {
				if w.ID == hwnd {
					return w.PID
				}
			}
			return 0
		},
		Sleep: func(time.Duration) {},
		StartApp: func(spec apps.Spec, opts win.LaunchOptions) (uint32, error) {
			l.starts = append(l.starts, startCall{spec, opts})
			if l.startErr != nil {
				return 0, l.startErr
			}
			l.windows = append([]win.WindowInfo{{ID: 300, PID: 77, ExePath: fakeAppPath, App: "Tool", Title: "Tool"}}, l.windows...)
			if l.takesFG {
				l.foreground = 300
			}
			return 77, nil
		},
	}
}

func (l *launchSpy) launch(t *testing.T, args string) (string, *Error) {
	t.Helper()
	return CommandsWith(l.services())["launch-app"](context.Background(), json.RawMessage(args))
}

func TestLaunchAppStaysBehindAndPrintsTheWindow(t *testing.T) {
	l := newLaunchSpy()
	out, cmdErr := l.launch(t, `{"app":`+jsonString(fakeAppPath)+`}`)
	if cmdErr != nil {
		t.Fatalf("launch-app: %+v", cmdErr)
	}
	if out != "300\tTool\tTool\n" {
		t.Errorf("out = %q, want the Window line alone (the App did not take the foreground)", out)
	}
	if len(l.starts) != 1 || l.starts[0].spec.Kind != apps.IDExe || l.starts[0].spec.ID != fakeAppPath {
		t.Fatalf("starts = %+v", l.starts)
	}
	if opts := l.starts[0].opts; opts.Minimized || opts.Args != nil || opts.Dir != "" || opts.Env != nil {
		t.Errorf("no options given, launcher got %+v", opts)
	}
}

func TestLaunchAppReportsTheAppTookTheForeground(t *testing.T) {
	l := newLaunchSpy()
	l.takesFG = true
	out, cmdErr := l.launch(t, `{"app":`+jsonString(fakeAppPath)+`}`)
	if cmdErr != nil {
		t.Fatalf("launch-app: %+v", cmdErr)
	}
	if want := "300\tTool\tTool\n" + launchForegroundNote; out != want {
		t.Errorf("out = %q, want %q", out, want)
	}
	if !strings.HasPrefix(launchForegroundNote, "Note: the App took the foreground") {
		t.Errorf("note = %q", launchForegroundNote)
	}
}

func TestLaunchAppNoNoteWhenTheAppAlreadyHadTheForeground(t *testing.T) {
	// The App's other Window was the foreground before: the launch did not
	// take it from the user.
	l := newLaunchSpy()
	l.windows = append(l.windows, win.WindowInfo{ID: 301, PID: 77, ExePath: fakeAppPath, App: "Tool", Title: "Tool (1)"})
	l.foreground = 301
	l.takesFG = true
	out, cmdErr := l.launch(t, `{"app":`+jsonString(fakeAppPath)+`}`)
	if cmdErr != nil {
		t.Fatalf("launch-app: %+v", cmdErr)
	}
	if strings.Contains(out, "Note:") {
		t.Errorf("out = %q, want no note", out)
	}
}

func TestLaunchAppCarriesTheOptions(t *testing.T) {
	l := newLaunchSpy()
	dir := t.TempDir()
	out, cmdErr := l.launch(t, `{"app":`+jsonString(fakeAppPath)+`,"minimized":true,"args":["--name","two words",""],"cwd":`+jsonString(dir)+`,"env":["A=1","B=x=y"]}`)
	if cmdErr != nil {
		t.Fatalf("launch-app: %+v", cmdErr)
	}
	if out != "300\tTool\tTool\n" {
		t.Errorf("out = %q", out)
	}
	want := win.LaunchOptions{Args: []string{"--name", "two words", ""}, Dir: dir, Env: []string{"A=1", "B=x=y"}, Minimized: true}
	got := l.starts[0].opts
	if !slices.Equal(got.Args, want.Args) || got.Dir != want.Dir || !slices.Equal(got.Env, want.Env) || got.Minimized != want.Minimized {
		t.Errorf("launcher got %+v, want %+v", got, want)
	}
}

func TestLaunchAppMinimizedWorksForEveryKind(t *testing.T) {
	l := newLaunchSpy()
	// A packaged App opens no Window of a known exe here; only the start
	// matters, so the wait is cut short by the request deadline.
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	_, _ = CommandsWith(l.services())["launch-app"](ctx, json.RawMessage(`{"app":"Microsoft.WindowsCalculator_8wekyb3d8bbwe!App","minimized":true}`))
	if len(l.starts) != 1 || !l.starts[0].opts.Minimized || l.starts[0].spec.Kind != apps.IDAppUserModelID {
		t.Fatalf("starts = %+v, want one minimized packaged start", l.starts)
	}
}

func TestLaunchAppRefusalsStartNothing(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name, args, msg, hint string
	}{
		{"--arg on a packaged App", `{"app":"Microsoft.WindowsCalculator_8wekyb3d8bbwe!App","args":["x"]}`,
			"need an executable, and Microsoft.WindowsCalculator_8wekyb3d8bbwe!App starts a packaged App; nothing was started", "launch the App's executable by its path"},
		{"--cwd on a shortcut", `{"app":"C:\\nowhere\\App.lnk","cwd":` + jsonString(dir) + `}`,
			"starts a shortcut (C:\\nowhere\\App.lnk); nothing was started", ""},
		{"--env without =", `{"app":` + jsonString(fakeAppPath) + `,"env":["NOVALUE"]}`,
			`--env "NOVALUE": want KEY=VALUE; nothing was started`, "usage: winuse launch-app"},
		{"--env with an empty name", `{"app":` + jsonString(fakeAppPath) + `,"env":["=x"]}`,
			"want KEY=VALUE; nothing was started", ""},
		{"a relative --cwd", `{"app":` + jsonString(fakeAppPath) + `,"cwd":"work"}`,
			`--cwd "work" is not an absolute path; nothing was started`, ""},
		{"a --cwd that is no folder", `{"app":` + jsonString(fakeAppPath) + `,"cwd":` + jsonString(dir+`\missing`) + `}`,
			"is not a folder; nothing was started", "pass an existing folder"},
	}
	for _, tc := range cases {
		l := newLaunchSpy()
		out, cmdErr := l.launch(t, tc.args)
		if out != "" || cmdErr == nil {
			t.Errorf("%s: got %q, %v; want a refusal", tc.name, out, cmdErr)
			continue
		}
		if !strings.Contains(cmdErr.Msg, tc.msg) {
			t.Errorf("%s: message %q, want it to contain %q", tc.name, cmdErr.Msg, tc.msg)
		}
		if !strings.Contains(cmdErr.Hint, tc.hint) {
			t.Errorf("%s: hint %q, want it to contain %q", tc.name, cmdErr.Hint, tc.hint)
		}
		if len(l.starts) != 0 {
			t.Errorf("%s: started %+v", tc.name, l.starts)
		}
	}
}

func TestLaunchAppShortcutRefusalNamesItsExecutable(t *testing.T) {
	e := exeOnlyRefusal("Tool", apps.Spec{ID: `C:\x\Tool.lnk`, Kind: apps.IDShortcut, TargetExe: fakeAppPath})
	if want := `winuse launch-app "` + strings.ReplaceAll(fakeAppPath, `\`, `\\`) + `" with the same options`; !strings.Contains(e.Hint, want) {
		t.Errorf("hint = %q, want it to contain %q", e.Hint, want)
	}
}

func TestLaunchAppStartFailure(t *testing.T) {
	l := newLaunchSpy()
	l.startErr = errors.New("access denied")
	_, cmdErr := l.launch(t, `{"app":`+jsonString(fakeAppPath)+`}`)
	if cmdErr == nil || cmdErr.Msg != "failed to launch app "+fakeAppPath+": access denied" {
		t.Errorf("err = %+v", cmdErr)
	}
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// The 2026-11-03 final live run: a launch whose App opens no Window must
// fail with "did not expose a targetable window" before the request's
// deadline, however long the name took to resolve, so the server reports
// that failure and not its generic timeout. The resolution here takes a
// third of a short request budget, and the poll sleeps for real.
func TestLaunchAppNoWindowFailsBeforeTheDeadline(t *testing.T) {
	l := newLaunchSpy()
	s := l.services()
	slowFirst := true
	windows := s.Windows
	s.Windows = func() ([]win.WindowInfo, error) {
		if slowFirst {
			slowFirst = false
			time.Sleep(time.Second) // the before-start snapshot and resolution
		}
		return windows()
	}
	s.StartApp = func(apps.Spec, win.LaunchOptions) (uint32, error) { return 77, nil } // opens nothing
	s.Sleep = time.Sleep
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, cmdErr := CommandsWith(s)["launch-app"](ctx, json.RawMessage(`{"app":`+jsonString(fakeAppPath)+`}`))
	if cmdErr == nil || !strings.Contains(cmdErr.Msg, "did not expose a targetable window") {
		t.Fatalf("launch-app: %+v, want the no-window failure", cmdErr)
	}
	if ctx.Err() != nil {
		t.Errorf("the failure came after the request's deadline: the server would report its generic timeout")
	}
}

// TestLaunchAppArmsPassively: launch-app is Headless, so its Esc scope is
// the passive one (only the user's Esc stops it), never the full Headed
// arm, where any physical key, button or wheel stops it and the stop is
// reported as an Esc (perf ticket 04).
func TestLaunchAppArmsPassively(t *testing.T) {
	l := newLaunchSpy()
	esc := &scopeEsc{}
	svc := l.services()
	svc.Esc = esc
	if _, cmdErr := CommandsWith(svc)["launch-app"](context.Background(), json.RawMessage(`{"app":`+jsonString(fakeAppPath)+`}`)); cmdErr != nil {
		t.Fatalf("launch-app: %+v", cmdErr)
	}
	if want := []string{"arm-passive", "disarm-passive"}; !slices.Equal(esc.log, want) {
		t.Errorf("Esc scopes = %v, want %v", esc.log, want)
	}
}
