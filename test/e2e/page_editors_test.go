//go:build e2e

// Placeholder and remounting rich-text editors (bladebro-ideas 09, the
// regression test of ticket 15's reproduction): set-value and type-text
// --into on testdata/page-editors.html (bladebro's editor.html sections
// 1-3), through winuse's real Page code in process, in the order
// pageSetValue and pageTypeInto run it: ReadField, its refusal, Aim with
// the scroll, NewInput, then SetValue or TypeInto. No winuse binary and no
// helper.
//
//   - a plain contenteditable: one line, several lines (each a <p>, which
//     innerText reads with a blank line between), and Unicode;
//   - the placeholder ("facade") textarea that mounts the real editor on
//     focus, hands it the focus and hides itself: set-value (also with a
//     draft that lands 600 ms later: the read-back is immediate) and
//     type-text --into, both noted;
//   - the editor that replaces its own node on the first input: one line
//     and several lines, read back through the editor that took its place,
//     a second set-value through the stale node refused, and type-text
//     --into with a newline, which must not scramble the text.
//
// Each installed Chrome and Edge is started headless on a throwaway
// profile (startIsolatedBrowser). User-run only (see the package comment);
// an agent must never start it.

package e2e

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/browser"
	"github.com/hoangvu12/winuse/internal/cdp"
	"github.com/hoangvu12/winuse/internal/page"
)

// The notes the editors' Actions print after their ok.
const (
	facadeNote  = `the field handed its focus to edit "Comment editor"; the text went there`
	remountNote = `the field was replaced after the input; its value was read back from the focused edit "Remount editor"`
	typedNote   = `the field was replaced while typing; the rest of the text went to the focused edit "Remount editor", at its end`
	draftKey    = "comment-draft-items-probe"
)

func TestPageEditors(t *testing.T) {
	optIn(t)
	sys := browser.Live()
	srv := startPageServers(t)
	u := srv.a.URL + "/page-editors.html"
	ran := 0
	for _, name := range []string{"chrome", "edge"} {
		b, ok := installedBrowsers(sys)[name]
		if !ok {
			t.Logf("%s is not installed in a standard location; skipped", browser.DisplayName(name))
			continue
		}
		ran++
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			e := openEditorTab(t, ctx, sys, b.Path, u)
			e.cases(t)
		})
	}
	if ran == 0 {
		t.Skip("neither Chrome nor Edge is installed; nothing was run")
	}
}

// editorTab is one headless browser's Tab on page-editors.html.
type editorTab struct {
	ctx     context.Context
	c       *cdp.Client
	target  string
	session string
	url     string
}

// openEditorTab starts the browser headless on a throwaway profile, dials
// it and attaches a session to a new Tab.
func openEditorTab(t *testing.T, ctx context.Context, sys *browser.System, exe, u string) *editorTab {
	t.Helper()
	port := freePort(t)
	pid := startIsolatedBrowser(t, exe, "--headless=new", fmt.Sprintf("--remote-debugging-port=%d", port), "about:blank")
	var ws string
	var err error
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(250 * time.Millisecond) {
		if ws, err = sys.VersionWS(ctx, port); err == nil {
			break
		}
	}
	if err != nil {
		t.Fatalf("/json/version on port %d: %v", port, err)
	}
	tr, err := cdp.DialWS(ctx, ws, 20*time.Second)
	if err != nil {
		t.Fatalf("dialing %s: %v", ws, err)
	}
	c := cdp.NewClient(tr)
	// Closed the browser's own way before startIsolatedBrowser's cleanup
	// terminates it, so its helper processes let go of the profile.
	t.Cleanup(func() {
		cctx, ccancel := context.WithTimeout(context.Background(), 5*time.Second)
		c.Call(cctx, "Browser.close", nil)
		ccancel()
		c.Close()
		for deadline := time.Now().Add(10 * time.Second); pidAlive(pid) && time.Now().Before(deadline); {
			time.Sleep(100 * time.Millisecond)
		}
		time.Sleep(time.Second)
	})
	res, err := c.Call(ctx, "Target.createTarget", map[string]any{"url": "about:blank"})
	if err != nil {
		t.Fatalf("Target.createTarget: %v", err)
	}
	var ct struct {
		TargetID string `json:"targetId"`
	}
	json.Unmarshal(res, &ct)
	conn := &browser.Connection{Browser: "headless", Mode: browser.ModeLaunch, WSURL: ws, Port: port, Client: c}
	sess, err := conn.AttachPage(ctx, ct.TargetID)
	if err != nil {
		t.Fatalf("attaching to tab %s: %v", ct.TargetID, err)
	}
	e := &editorTab{ctx: ctx, c: c, target: ct.TargetID, session: sess, url: u}
	e.call(t, "Page.enable", nil)
	e.call(t, "DOM.getDocument", map[string]any{"depth": 0})
	return e
}

func (e *editorTab) call(t *testing.T, method string, params any) json.RawMessage {
	t.Helper()
	cctx, cancel := context.WithTimeout(e.ctx, 15*time.Second)
	defer cancel()
	res, err := e.c.CallSession(cctx, e.session, method, params)
	if err != nil {
		t.Fatalf("%s: %v", method, err)
	}
	return res
}

// eval returns an expression's value, JSON-encoded unless it is a string.
func (e *editorTab) eval(t *testing.T, expr string) string {
	t.Helper()
	res := e.call(t, "Runtime.evaluate", map[string]any{"expression": expr, "returnByValue": true, "awaitPromise": true})
	var v struct {
		Result struct {
			Value json.RawMessage `json:"value"`
		} `json:"result"`
		ExceptionDetails *struct {
			Text string `json:"text"`
		} `json:"exceptionDetails"`
	}
	json.Unmarshal(res, &v)
	if v.ExceptionDetails != nil {
		t.Fatalf("eval %q: %s", expr, v.ExceptionDetails.Text)
	}
	var s string
	if json.Unmarshal(v.Result.Value, &s) == nil {
		return s
	}
	return string(v.Result.Value)
}

// load navigates to the page (with draft in localStorage, or none) and
// waits for it to load.
func (e *editorTab) load(t *testing.T, draft string) {
	t.Helper()
	e.call(t, "Page.navigate", map[string]any{"url": e.url})
	e.waitLoaded(t)
	set := "localStorage.removeItem(" + jsString(draftKey) + ")"
	if draft != "" {
		set = "localStorage.setItem(" + jsString(draftKey) + ", " + jsString(draft) + ")"
	}
	e.eval(t, set)
	e.call(t, "Page.navigate", map[string]any{"url": e.url}) // the draft is read on the next load
	e.waitLoaded(t)
}

func (e *editorTab) waitLoaded(t *testing.T) {
	t.Helper()
	for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		if s := e.eval(t, "document.readyState + ' ' + location.href"); s == "complete "+e.url && e.eval(t, "String(!!document.getElementById('rm-ed'))") == "true" {
			return
		}
	}
	t.Fatalf("%s did not load", e.url)
}

// origin is the page.Origin a Window State would hold for the node a
// selector finds now.
func (e *editorTab) origin(t *testing.T, sel string) *page.Origin {
	t.Helper()
	res := e.call(t, "Runtime.evaluate", map[string]any{"expression": "document.querySelector(" + jsString(sel) + ")"})
	var ev struct {
		Result struct {
			ObjectID string `json:"objectId"`
		} `json:"result"`
	}
	json.Unmarshal(res, &ev)
	if ev.Result.ObjectID == "" {
		t.Fatalf("no node for %s", sel)
	}
	defer e.call(t, "Runtime.releaseObject", map[string]any{"objectId": ev.Result.ObjectID})
	var dn struct {
		Node struct {
			BackendNodeID int64 `json:"backendNodeId"`
		} `json:"node"`
	}
	json.Unmarshal(e.call(t, "DOM.describeNode", map[string]any{"objectId": ev.Result.ObjectID}), &dn)
	var tree struct {
		FrameTree struct {
			Frame struct {
				ID       string `json:"id"`
				LoaderID string `json:"loaderId"`
			} `json:"frame"`
		} `json:"frameTree"`
	}
	json.Unmarshal(e.call(t, "Page.getFrameTree", nil), &tree)
	return &page.Origin{Browser: "chrome", Client: e.c, TargetID: e.target, SessionID: e.session,
		FrameID: tree.FrameTree.Frame.ID, LoaderID: tree.FrameTree.Frame.LoaderID, BackendNodeID: dn.Node.BackendNodeID}
}

// act runs one Page Action the way runPage does after its staleness check:
// the field read and its refusal (checkType adds type-text --into's: a
// field that takes no text), the aim with its scroll, the input, then the
// Action. It returns the Action's error and the notes its ok would print.
func (e *editorTab) act(t *testing.T, o *page.Origin, run func(ctx context.Context, in *page.Input, f page.Field) error) ([]string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(e.ctx, 30*time.Second)
	defer cancel()
	f, err := o.ReadField(ctx)
	if err != nil {
		return nil, err
	}
	if rf := f.Refusal(); rf != nil {
		t.Fatalf("the field was refused: %v", rf)
	}
	if _, err := o.Aim(ctx, page.AimOptions{Scroll: true}); err != nil {
		t.Fatalf("aim: %v", err)
	}
	in, err := page.NewInput(ctx, e.c, e.session, func() bool { return false }, e.session)
	if err != nil {
		t.Fatalf("NewInput: %v", err)
	}
	defer in.Close()
	err = run(ctx, in, f)
	return in.Notes, err
}

func (e *editorTab) setValue(t *testing.T, sel, value string) ([]string, error) {
	t.Helper()
	o := e.origin(t, sel)
	return e.act(t, o, func(ctx context.Context, in *page.Input, f page.Field) error {
		return o.SetValue(ctx, in, f, value)
	})
}

func (e *editorTab) typeInto(t *testing.T, sel, text string) ([]string, error) {
	t.Helper()
	o := e.origin(t, sel)
	return e.act(t, o, func(ctx context.Context, in *page.Input, f page.Field) error {
		return o.TypeInto(ctx, in, text)
	})
}

// blocks is an editor's text by its block children, joined by "|" (each
// <p> one), with no-break spaces as spaces: what the Page holds, read
// without innerText's blank lines.
func (e *editorTab) blocks(t *testing.T, id string) string {
	t.Helper()
	return e.eval(t, `(() => { const el = document.getElementById(`+jsString(id)+`); if (!el) return '<none>';
	  const nb = new RegExp(String.fromCharCode(160), 'g');
	  return Array.from(el.children).map(c => c.textContent.replace(nb, ' ')).join('|'); })()`)
}

func (e *editorTab) cases(t *testing.T) {
	expectOK := func(t *testing.T, what string, notes []string, err error, want ...string) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: %v", what, err)
		}
		if len(want) == 0 {
			want = nil
		}
		if !reflect.DeepEqual(notes, want) {
			t.Errorf("%s: notes %q, want %q", what, notes, want)
		}
	}
	expectBlocks := func(t *testing.T, id, want string) {
		t.Helper()
		if got := e.blocks(t, id); got != want {
			t.Errorf("#%s holds %q, want %q", id, got, want)
		}
	}

	t.Run("plain-single-line", func(t *testing.T) {
		e.load(t, "")
		notes, err := e.setValue(t, "#plain-ce", "hello plain")
		expectOK(t, "set-value", notes, err)
		expectBlocks(t, "plain-ce", "hello plain")
	})
	t.Run("plain-multi-line", func(t *testing.T) {
		// Each line a <p>: innerText reads "a\n\nb", the false failure ticket
		// 15 found.
		e.load(t, "")
		notes, err := e.setValue(t, "#plain-ce", "a\nb")
		expectOK(t, "set-value", notes, err)
		expectBlocks(t, "plain-ce", "a|b")
	})
	t.Run("plain-blank-line", func(t *testing.T) {
		e.load(t, "")
		notes, err := e.setValue(t, "#plain-ce", "a\n\nb")
		expectOK(t, "set-value", notes, err)
		expectBlocks(t, "plain-ce", "a||b")
	})
	t.Run("plain-unicode", func(t *testing.T) {
		e.load(t, "")
		v := "héllo wörld, 日本語 and Ελληνικά 🎉"
		notes, err := e.setValue(t, "#plain-ce", v)
		expectOK(t, "set-value", notes, err)
		expectBlocks(t, "plain-ce", v)
	})

	t.Run("facade-set-value", func(t *testing.T) {
		e.load(t, "")
		notes, err := e.setValue(t, "#facade", "hello facade")
		expectOK(t, "set-value", notes, err, facadeNote)
		expectBlocks(t, "editor", "hello facade")
		if got := e.eval(t, "document.getElementById('facade').value"); got != "" {
			t.Errorf("the facade holds %q, want nothing", got)
		}
	})
	t.Run("facade-set-value-late-draft", func(t *testing.T) {
		// The draft lands 600 ms after the mount, before the text: the
		// read-back is immediate, so it reads the text alone.
		e.load(t, "DRAFT")
		notes, err := e.setValue(t, "#facade", "hello facade")
		expectOK(t, "set-value", notes, err, facadeNote)
		time.Sleep(900 * time.Millisecond)
		t.Logf("after the draft landed: #editor holds %q", e.blocks(t, "editor"))
	})
	t.Run("facade-type-text-into", func(t *testing.T) {
		e.load(t, "")
		notes, err := e.typeInto(t, "#facade", "typed facade")
		expectOK(t, "type-text --into", notes, err, facadeNote)
		expectBlocks(t, "editor", "typed facade")
	})

	t.Run("remount-single-line", func(t *testing.T) {
		e.load(t, "")
		o := e.origin(t, "#rm-ed")
		notes, err := e.act(t, o, func(ctx context.Context, in *page.Input, f page.Field) error {
			return o.SetValue(ctx, in, f, "hello remount")
		})
		expectOK(t, "set-value", notes, err, remountNote)
		expectBlocks(t, "rm-ed", "hello remount")
		if n := e.eval(t, "String(document.querySelectorAll('#rm-ed').length)"); n != "1" {
			t.Errorf("%s editors after the remount, want 1", n)
		}
		// The Origin the Window State held now names the replaced node: a
		// second set-value through it is refused, nothing sent.
		_, err = e.act(t, o, func(ctx context.Context, in *page.Input, f page.Field) error {
			t.Error("a set-value through the replaced node got past its field read")
			return nil
		})
		if !errors.Is(err, page.ErrStale) {
			t.Errorf("set-value through the replaced node: %v, want stale", err)
		}
	})
	t.Run("remount-multi-line", func(t *testing.T) {
		e.load(t, "")
		notes, err := e.setValue(t, "#rm-ed", "a\nb")
		expectOK(t, "set-value", notes, err, remountNote)
		expectBlocks(t, "rm-ed", "a|b")
	})
	t.Run("remount-type-text-into", func(t *testing.T) {
		// The remount after "one" put the caret at the start: without the
		// follow the editor held "\n\n\ntwoone", and the command said ok.
		e.load(t, "")
		notes, err := e.typeInto(t, "#rm-ed", "one\ntwo")
		expectOK(t, "type-text --into", notes, err, typedNote)
		expectBlocks(t, "rm-ed", "one|two")
	})
}
