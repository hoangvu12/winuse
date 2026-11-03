package helper

// Offline tests for the browser-* handlers over a fake BrowserService:
// argument parsing, the Problem-to-hint mapping, and that a long browser
// command never holds the action gate.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/bridge"
	"github.com/hoangvu12/winuse/internal/browser"
	"github.com/hoangvu12/winuse/internal/cdp"
)

type fakeBrowsers struct {
	mu    sync.Mutex
	calls []string
	clone browser.CloneOptions
	err   error
}

func (f *fakeBrowsers) record(call, name string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call+" "+name)
	if f.err != nil {
		return "", f.err
	}
	return "ok " + call + "\n", nil
}

func (f *fakeBrowsers) Status(ctx context.Context, name string) (string, error) {
	return f.record("status", name)
}
func (f *fakeBrowsers) Use(ctx context.Context, name string) (string, error) {
	return f.record("use", name)
}
func (f *fakeBrowsers) Launch(ctx context.Context, name string) (string, error) {
	return f.record("launch", name)
}
func (f *fakeBrowsers) Connect(ctx context.Context, name string) (string, error) {
	return f.record("connect", name)
}
func (f *fakeBrowsers) Stop(ctx context.Context, name string) (string, error) {
	return f.record("stop", name)
}
func (f *fakeBrowsers) ExtInstall(ctx context.Context, name string) (string, error) {
	return f.record("ext-install", name)
}
func (f *fakeBrowsers) ExtStatus(ctx context.Context, name string) (string, error) {
	return f.record("ext-status", name)
}
func (f *fakeBrowsers) Clone(ctx context.Context, opts browser.CloneOptions) (string, error) {
	f.mu.Lock()
	f.clone = opts
	f.mu.Unlock()
	return f.record("clone", opts.Browser)
}

func TestBrowserCommandsCallTheService(t *testing.T) {
	fb := &fakeBrowsers{}
	cmds := CommandsWith(Services{Trees: &fakeTrees{}, Browser: fb})
	ctx := context.Background()
	for _, tc := range []struct{ cmd, args, want string }{
		{"browser-status", ``, "status "},
		{"browser-use", `{"browser":"edge"}`, "use edge"},
		{"browser-launch", `{"browser":"chrome"}`, "launch chrome"},
		{"browser-connect", `{}`, "connect "},
		{"browser-stop", `null`, "stop "},
		{"browser-ext-install", `{"browser":"all"}`, "ext-install all"},
		{"browser-ext-status", `{"browser":"brave"}`, "ext-status brave"},
	} {
		fb.calls = nil
		out, e := cmds[tc.cmd](ctx, json.RawMessage(tc.args))
		if e != nil {
			t.Fatalf("%s: %v", tc.cmd, e.Msg)
		}
		if len(fb.calls) != 1 || fb.calls[0] != tc.want || out == "" {
			t.Errorf("%s: calls = %q, out = %q", tc.cmd, fb.calls, out)
		}
	}
	_, e := cmds["browser-clone"](ctx, json.RawMessage(`{"browser":"brave","profile":"Profile 1","fresh":true,"close_source":true}`))
	if e != nil {
		t.Fatal(e.Msg)
	}
	if want := (browser.CloneOptions{Browser: "brave", Profile: "Profile 1", Fresh: true, CloseSource: true}); fb.clone != want {
		t.Errorf("clone options = %+v", fb.clone)
	}
	if _, e := cmds["browser-launch"](ctx, json.RawMessage(`{"browser":`)); e == nil || e.Hint != "usage: winuse browser launch [browser]" {
		t.Errorf("malformed args = %+v", e)
	}
	if _, e := cmds["browser-ext-install"](ctx, json.RawMessage(`[`)); e == nil || e.Hint != "usage: winuse browser ext install [browser|all]" {
		t.Errorf("malformed ext install args = %+v", e)
	}
	if _, e := cmds["browser-ext-status"](ctx, json.RawMessage(`[`)); e == nil || e.Hint != "usage: winuse browser ext status [browser]" {
		t.Errorf("malformed ext status args = %+v", e)
	}
}

func TestBrowserExtensionRefusalsBecomeProblems(t *testing.T) {
	fb := &fakeBrowsers{err: fmt.Errorf("x: %w", &cdp.Error{Method: "Browser.setDownloadBehavior", Code: bridge.CodeRefused, Message: "refused"})}
	cmds := CommandsWith(Services{Trees: &fakeTrees{}, Browser: fb})
	_, e := cmds["browser-status"](context.Background(), nil)
	if e == nil || e.Msg != "Browser.setDownloadBehavior needs a launched or cloned browser; the winuse extension cannot do it" || e.Hint != "run winuse browser launch or winuse browser clone, which take browser-level commands" {
		t.Errorf("refusal = %+v", e)
	}
	fb.err = &browser.Problem{Kind: browser.ExtNotLoaded, Browser: "edge", Dir: `C:\w\extension`}
	_, e = cmds["browser-ext-status"](context.Background(), json.RawMessage(`{"browser":"edge"}`))
	if e == nil || e.Exit != 1 || e.Msg != "the winuse extension is not loaded in Edge yet" || !strings.Contains(e.Hint, `click "Load unpacked" and choose C:\w\extension`) {
		t.Errorf("ext status problem = %+v", e)
	}
}

func TestBrowserProblemsBecomeOneSentenceAndANextCommand(t *testing.T) {
	fb := &fakeBrowsers{err: &browser.Problem{Kind: browser.AwaitingApproval, Browser: "chrome"}}
	cmds := CommandsWith(Services{Trees: &fakeTrees{}, Browser: fb})
	_, e := cmds["browser-status"](context.Background(), nil)
	if e == nil || e.Exit != 1 {
		t.Fatalf("err = %+v, want a failure", e)
	}
	if e.Msg != `Chrome is waiting for the user to approve remote debugging in its "Allow remote debugging?" prompt` {
		t.Errorf("msg = %q", e.Msg)
	}
	if e.Hint != `click Allow in Chrome's "Allow remote debugging?" prompt, then run winuse browser status chrome; winuse browser clone chrome never asks` {
		t.Errorf("hint = %q", e.Hint)
	}
	fb.err = errors.New("starting chrome.exe: access denied")
	_, e = cmds["browser-launch"](context.Background(), nil)
	if e == nil || e.Msg != "starting chrome.exe: access denied" || e.Hint != "run winuse browser status" {
		t.Errorf("plain error = %+v", e)
	}
}

func TestBrowserCommandsNeverWaitForTheActionGate(t *testing.T) {
	fb := &fakeBrowsers{}
	lanes := newLaneManager()
	lanes.lane(desktopLane) <- struct{}{} // another Action holds the desktop
	cmds := CommandsWith(Services{Trees: &fakeTrees{}, Browser: fb, lanes: lanes})
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	for _, name := range []string{"browser-status", "browser-use", "browser-launch", "browser-clone", "browser-connect", "browser-stop", "browser-ext-install", "browser-ext-status"} {
		if _, e := cmds[name](ctx, nil); e != nil {
			t.Errorf("%s waited for the action gate: %s", name, e.Msg)
		}
	}
}

// statusOf is a BrowserService whose status resolves to conn, as
// *browser.Manager's Connection does.
type statusOf struct {
	*fakeBrowsers
	conn *browser.Connection
}

func (s statusOf) Connection(ctx context.Context, name string) (*browser.Connection, error) {
	return s.conn, nil
}

// TestBrowserStatusNamesTheDownloadFolder: browser status on a browser
// winuse launched turns download tracking on and says where downloads go;
// through the extension it says nothing about them.
func TestBrowserStatusNamesTheDownloadFolder(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "winuse")
	old := pageDownloadDir
	pageDownloadDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { pageDownloadDir = old })

	r := newWinRig(t)
	cmds := CommandsWith(Services{Trees: &fakeTrees{}, Browser: statusOf{&fakeBrowsers{}, r.conn}})
	out, e := cmds["browser-status"](context.Background(), nil)
	if want := "ok status\nnote: downloads go to " + dir + " (winuse wait <window> download prints each file's path)\n"; e != nil || out != want {
		t.Errorf("launched status = %q, %+v\nwant %q", out, e, want)
	}
	if got := r.sent("Browser.setDownloadBehavior"); len(got) != 1 {
		t.Errorf("download behavior sent %d times", len(got))
	}
	if out, e = cmds["browser-status"](context.Background(), nil); e != nil || !strings.Contains(out, "note: downloads go to") || len(r.sent("Browser.setDownloadBehavior")) != 1 {
		t.Errorf("second status = %q, %+v; tracking turned on again", out, e)
	}

	_, conn := extensionBrowser(t)
	cmds = CommandsWith(Services{Trees: &fakeTrees{}, Browser: statusOf{&fakeBrowsers{}, conn}})
	if out, e := cmds["browser-status"](context.Background(), nil); e != nil || out != "ok status\n" {
		t.Errorf("extension status = %q, %+v", out, e)
	}
}
