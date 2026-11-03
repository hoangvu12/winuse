package helper

// browser attach and detach's offline tests (headless-max 15): the Window
// resolves to its process and executable, each refusal says nothing was
// sent and names the way on, an App launched with --cdp is left alone, and
// detach answers for an attach only. The Browser Connections are a fake;
// no process is signalled.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/hoangvu12/winuse/internal/browser"
	"github.com/hoangvu12/winuse/internal/win"
)

// attachBrowsers is a browser service with the attach side.
type attachBrowsers struct {
	fakeBrowsers
	err      error
	mode     browser.Mode
	calls    []string
	detached []uint32
}

func (b *attachBrowsers) AttachElectron(ctx context.Context, pid uint32, exePath string) (browser.AppEndpoint, error) {
	b.calls = append(b.calls, exePath)
	if b.err != nil {
		return browser.AppEndpoint{}, b.err
	}
	return browser.AppEndpoint{Kind: browser.AppElectron, Mode: b.mode, PID: pid, Port: 9229}, nil
}

func (b *attachBrowsers) DetachApp(pid uint32) error {
	if b.mode != browser.ModeAttach {
		return browser.ErrNotAttached
	}
	b.detached = append(b.detached, pid)
	return nil
}

const slackExe = `C:\Users\u\AppData\Local\slack\app-4.41\slack.exe`

func attachCommands(b *attachBrowsers) map[string]Command {
	return CommandsWith(Services{
		Trees: &fakeTrees{},
		Windows: func() ([]win.WindowInfo, error) {
			return []win.WindowInfo{{ID: 700, PID: 600, App: "Slack", ExePath: slackExe, Title: "Slack | general"}}, nil
		},
		Browser: b,
	})
}

func runAttach(t *testing.T, cmds map[string]Command, cmd string) (string, *Error) {
	t.Helper()
	return cmds[cmd](context.Background(), json.RawMessage(`{"window":"700"}`))
}

func TestBrowserAttach(t *testing.T) {
	b := &attachBrowsers{mode: browser.ModeAttach}
	cmds := attachCommands(b)
	out, e := runAttach(t, cmds, "browser-attach")
	if e != nil || out != "ok\nNote: attached to Slack process 600: get-window-state 700 reads its pages; winuse browser detach 700 ends the attach and closes its inspector\n" {
		t.Errorf("attach: %q, %+v", out, e)
	}
	if len(b.calls) != 1 || b.calls[0] != slackExe {
		t.Errorf("attached with %v", b.calls)
	}
	out, e = runAttach(t, cmds, "browser-detach")
	if e != nil || out != "ok\nNote: detached; the App's inspector is closed\n" || len(b.detached) != 1 || b.detached[0] != 600 {
		t.Errorf("detach: %q, %+v, %v", out, e, b.detached)
	}
}

func TestBrowserAttachRefusals(t *testing.T) {
	for _, c := range []struct {
		err  error
		msg  string
		hint string
	}{
		{browser.ErrNotElectron, "window 700 belongs to Slack, which is not an Electron App; nothing was sent", "Electron Apps only"},
		{browser.ErrNodeInspectOff, "Slack turned its Node.js inspector off (the nodeCliInspect fuse), so winuse cannot attach; nothing was sent", "--cdp"},
	} {
		_, e := runAttach(t, attachCommands(&attachBrowsers{err: c.err}), "browser-attach")
		if e == nil || e.Msg != c.msg || !strings.Contains(e.Hint, c.hint) {
			t.Errorf("%v: %+v", c.err, e)
		}
	}
	// An App launched with --cdp already has its connection.
	out, e := runAttach(t, attachCommands(&attachBrowsers{mode: browser.ModeApp}), "browser-attach")
	if e != nil || !strings.Contains(out, "winuse launched it with --cdp") {
		t.Errorf("launched App: %q, %+v", out, e)
	}
	_, e = runAttach(t, attachCommands(&attachBrowsers{mode: browser.ModeApp}), "browser-detach")
	if e == nil || e.Msg != "winuse is not attached to Slack (window 700)" {
		t.Errorf("detach of a launched App: %+v", e)
	}
	// A helper without the attach side.
	cmds := CommandsWith(Services{Trees: &fakeTrees{}, Windows: func() ([]win.WindowInfo, error) {
		return []win.WindowInfo{{ID: 700, PID: 600, App: "Slack", ExePath: slackExe}}, nil
	}, Browser: &fakeBrowsers{}})
	if _, e := runAttach(t, cmds, "browser-attach"); e == nil || !strings.Contains(e.Msg, "nothing was sent") {
		t.Errorf("no attach side: %+v", e)
	}
	if _, e := cmds["browser-attach"](context.Background(), json.RawMessage(`{}`)); e == nil || e.Hint != "usage: winuse browser attach <window>" {
		t.Errorf("no window: %+v", e)
	}
}
