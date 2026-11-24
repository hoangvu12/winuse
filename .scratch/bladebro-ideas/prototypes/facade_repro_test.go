package helper

// TEMPORARY REPRO, do not commit. Drives winuse's real Page set-value and
// type-text --into code (page.Origin ReadField / Aim / NewInput / SetValue /
// TypeInto, then helper.pageFailure for the message) against a headless
// Chrome loading bladebro's qol_probe editor.html (copied into a temp dir).
//
//   set WINUSE_FACADE_REPRO=1
//   set WINUSE_FACADE_FIXTURE=C:\...\editor.html
//   go test ./internal/helper -run TestFacadeRepro -v -count=1

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/cdp"
	"github.com/hoangvu12/winuse/internal/page"
	"github.com/hoangvu12/winuse/internal/win"
)

type reproTab struct {
	t       *testing.T
	c       *cdp.Client
	session string
	target  string
	url     string
}

func (r *reproTab) call(ctx context.Context, method string, params any) json.RawMessage {
	r.t.Helper()
	res, err := r.c.CallSession(ctx, r.session, method, params)
	if err != nil {
		r.t.Fatalf("%s: %v", method, err)
	}
	return res
}

func (r *reproTab) eval(ctx context.Context, expr string) any {
	r.t.Helper()
	res := r.call(ctx, "Runtime.evaluate", map[string]any{"expression": expr, "returnByValue": true, "awaitPromise": true})
	var v struct {
		Result struct {
			Value any `json:"value"`
		} `json:"result"`
		ExceptionDetails any `json:"exceptionDetails"`
	}
	json.Unmarshal(res, &v)
	if v.ExceptionDetails != nil {
		r.t.Fatalf("eval %q: %v", expr, v.ExceptionDetails)
	}
	return v.Result.Value
}

// load navigates and waits for readyState complete.
func (r *reproTab) load(ctx context.Context) {
	r.call(ctx, "Page.navigate", map[string]any{"url": r.url})
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
		if s, _ := r.eval(ctx, "document.readyState + '|' + location.href").(string); strings.HasPrefix(s, "complete|"+r.url) {
			return
		}
	}
	r.t.Fatal("page did not load")
}

// origin builds the page.Origin the helper would hold for a selector's node.
func (r *reproTab) origin(ctx context.Context, sel string) *page.Origin {
	r.t.Helper()
	res := r.call(ctx, "Runtime.evaluate", map[string]any{"expression": "document.querySelector(" + jsq(sel) + ")"})
	var ev struct {
		Result struct {
			ObjectID string `json:"objectId"`
		} `json:"result"`
	}
	json.Unmarshal(res, &ev)
	if ev.Result.ObjectID == "" {
		r.t.Fatalf("no node for %s", sel)
	}
	return r.originOf(ctx, ev.Result.ObjectID)
}

func (r *reproTab) originOf(ctx context.Context, objectID string) *page.Origin {
	res := r.call(ctx, "DOM.describeNode", map[string]any{"objectId": objectID})
	var dn struct {
		Node struct {
			BackendNodeID int64  `json:"backendNodeId"`
			FrameID       string `json:"frameId"`
		} `json:"node"`
	}
	json.Unmarshal(res, &dn)
	ft := r.call(ctx, "Page.getFrameTree", nil)
	var tree struct {
		FrameTree struct {
			Frame struct {
				ID       string `json:"id"`
				LoaderID string `json:"loaderId"`
			} `json:"frame"`
		} `json:"frameTree"`
	}
	json.Unmarshal(ft, &tree)
	return &page.Origin{Browser: "chrome", Client: r.c, TargetID: r.target, SessionID: r.session,
		FrameID: tree.FrameTree.Frame.ID, LoaderID: tree.FrameTree.Frame.LoaderID, BackendNodeID: dn.Node.BackendNodeID}
}

func jsq(s string) string { b, _ := json.Marshal(s); return string(b) }

// activeJS: the focused element, piercing shadow roots.
const reproActiveJS = `(() => { let e = document.activeElement;
  while (e && e.shadowRoot && e.shadowRoot.activeElement) e = e.shadowRoot.activeElement;
  if (!e) return 'none';
  return e.tagName.toLowerCase() + (e.id ? '#' + e.id : '') + (e.isContentEditable ? ' [contenteditable]' : '') + (e.isConnected ? '' : ' [detached]'); })()`

const reproStateJS = `JSON.stringify({
  active: ` + reproActiveJS + `,
  facadeValue: document.getElementById('facade').value,
  facadeDisplay: getComputedStyle(document.getElementById('facade')).display,
  editorText: (document.getElementById('editor') || {}).innerText ?? null,
  rmEdText: (document.getElementById('rm-ed') || {}).innerText ?? null,
  rmEdCount: document.querySelectorAll('#rm-ed').length,
})`

func launchHeadlessChrome(t *testing.T) *cdp.Client {
	exe := `C:\Program Files\Google\Chrome\Application\chrome.exe`
	prof := t.TempDir()
	cmd := exec.Command(exe, "--headless=new", "--user-data-dir="+prof, "--remote-debugging-port=0",
		"--no-first-run", "--no-default-browser-check", "about:blank")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	var lines []string
	for i := 0; i < 100; i++ {
		time.Sleep(100 * time.Millisecond)
		b, err := os.ReadFile(filepath.Join(prof, "DevToolsActivePort"))
		if err == nil {
			lines = strings.Split(strings.TrimSpace(string(b)), "\n")
			if len(lines) >= 2 {
				break
			}
		}
	}
	if len(lines) < 2 {
		t.Fatal("no DevToolsActivePort")
	}
	ws, err := cdp.DialWS(context.Background(), "ws://127.0.0.1:"+strings.TrimSpace(lines[0])+strings.TrimSpace(lines[1]), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	c := cdp.NewClient(ws)
	t.Cleanup(func() { c.Close() })
	return c
}

func newReproTab(ctx context.Context, t *testing.T, c *cdp.Client, url string) *reproTab {
	res, err := c.Call(ctx, "Target.createTarget", map[string]any{"url": "about:blank"})
	if err != nil {
		t.Fatal(err)
	}
	var ct struct {
		TargetID string `json:"targetId"`
	}
	json.Unmarshal(res, &ct)
	res, err = c.Call(ctx, "Target.attachToTarget", map[string]any{"targetId": ct.TargetID, "flatten": true})
	if err != nil {
		t.Fatal(err)
	}
	var at struct {
		SessionID string `json:"sessionId"`
	}
	json.Unmarshal(res, &at)
	r := &reproTab{t: t, c: c, session: at.SessionID, target: ct.TargetID, url: url}
	r.call(ctx, "Page.enable", nil)
	r.call(ctx, "Runtime.enable", nil)
	r.call(ctx, "DOM.getDocument", map[string]any{"depth": 0})
	return r
}

// runSetValue is pageSetValue's sequence: ReadField, Refusal, Aim{Scroll},
// NewInput, SetValue; the error goes through pageFailure as runPage does.
func runSetValue(ctx context.Context, t *testing.T, r *reproTab, o *page.Origin, value string) (page.Field, error, *Error) {
	f, err := o.ReadField(ctx)
	if err != nil {
		t.Logf("  ReadField (runPage check) refused: %v", err)
		return f, err, pageFailure(err, pageRun{noun: "value", done: "set", verb: "set value", w: win.WindowInfo{ID: 4242}, index: 7}, 0)
	}
	if rf := f.Refusal(); rf != nil {
		t.Fatalf("Refusal: %v", rf)
	}
	if _, err := o.Aim(ctx, page.AimOptions{Scroll: true}); err != nil {
		t.Fatalf("Aim: %v", err)
	}
	in, err := page.NewInput(ctx, r.c, r.session, func() bool { return false }, r.session)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	err = o.SetValue(ctx, in, f, value)
	var rerr *Error
	if err != nil {
		rerr = pageFailure(err, pageRun{noun: "value", done: "set", verb: "set value", w: win.WindowInfo{ID: 4242}, index: 7}, in.Sent)
	}
	return f, err, rerr
}

func runTypeInto(ctx context.Context, t *testing.T, r *reproTab, o *page.Origin, text string) error {
	f, err := o.ReadField(ctx)
	if err != nil {
		t.Fatalf("ReadField: %v", err)
	}
	if rf := f.Refusal(); rf != nil {
		t.Fatalf("Refusal: %v", rf)
	}
	if _, err := o.Aim(ctx, page.AimOptions{Scroll: true}); err != nil {
		t.Fatalf("Aim: %v", err)
	}
	in, err := page.NewInput(ctx, r.c, r.session, func() bool { return false }, r.session)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	return o.TypeInto(ctx, in, text)
}

func describe(err error, rerr *Error) string {
	if err == nil {
		return "CheckValue: nil (ok)"
	}
	return fmt.Sprintf("CheckValue err: %v\n    helper Error: Msg=%q\n    Hint=%q\n    Delivery=%q", err, rerr.Msg, rerr.Hint, Delivery(rerr))
}

// activeOrigin is the proposed fix's lookup: the focused editable after
// DOM.focus, as an Origin (nil when it is the target itself).
func activeOrigin(ctx context.Context, r *reproTab) *page.Origin {
	res := r.call(ctx, "Runtime.evaluate", map[string]any{"expression": `(() => { let e = document.activeElement;
  while (e && e.shadowRoot && e.shadowRoot.activeElement) e = e.shadowRoot.activeElement; return e; })()`})
	var ev struct {
		Result struct {
			ObjectID string `json:"objectId"`
		} `json:"result"`
	}
	json.Unmarshal(res, &ev)
	return r.originOf(ctx, ev.Result.ObjectID)
}

func TestFacadeRepro(t *testing.T) {
	if os.Getenv("WINUSE_FACADE_REPRO") != "1" {
		t.Skip("temporary repro; set WINUSE_FACADE_REPRO=1")
	}
	fixture := os.Getenv("WINUSE_FACADE_FIXTURE")
	html, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(html)
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	c := launchHeadlessChrome(t)
	r := newReproTab(ctx, t, c, srv.URL+"/editor.html")
	state := func(label string) { t.Logf("  [%s] %v", label, r.eval(ctx, reproStateJS)) }
	setDraft := func(d string) {
		if d == "" {
			r.eval(ctx, "localStorage.removeItem('comment-draft-items-probe')")
		} else {
			r.eval(ctx, "localStorage.setItem('comment-draft-items-probe', "+jsq(d)+")")
		}
	}

	// 0. activeElement right after DOM.focus on the facade.
	r.load(ctx)
	setDraft("")
	r.load(ctx)
	o := r.origin(ctx, "#facade")
	state("before focus")
	r.call(ctx, "DOM.focus", map[string]any{"backendNodeId": o.BackendNodeID})
	t.Logf("CASE 0 facade: activeElement right after DOM.focus = %v", r.eval(ctx, reproActiveJS))
	state("after DOM.focus")

	// 1. set-value on the facade, no draft.
	t.Log("CASE 1 facade set-value, no draft")
	r.load(ctx)
	o = r.origin(ctx, "#facade")
	f, err, rerr := runSetValue(ctx, t, r, o, "hello facade")
	t.Logf("  field=%+v\n    %s", f, describe(err, rerr))
	state("right after")
	time.Sleep(800 * time.Millisecond)
	t.Logf("  CheckValue(facade) again after 800ms: %v", o.CheckValue(ctx, f, "hello facade"))
	state("after 800ms")

	// 2. set-value on the facade, with a stored draft (hydrates 600ms after mount).
	t.Log("CASE 2 facade set-value, draft 'DRAFT' in localStorage")
	setDraft("DRAFT")
	r.load(ctx)
	o = r.origin(ctx, "#facade")
	f, err, rerr = runSetValue(ctx, t, r, o, "hello facade")
	t.Logf("  %s", describe(err, rerr))
	state("right after")
	ed := activeOrigin(ctx, r)
	t.Logf("  fix-sim: CheckValue(active editor, editable) immediately: %v", ed.CheckValue(ctx, page.Field{Kind: "editable"}, "hello facade"))
	time.Sleep(800 * time.Millisecond)
	t.Logf("  fix-sim: CheckValue(active editor, editable) after 800ms: %v", ed.CheckValue(ctx, page.Field{Kind: "editable"}, "hello facade"))
	state("after 800ms")
	setDraft("")

	// 2b. fix simulation end to end: after DOM.focus, write to and read back
	// from the active editable instead (no draft).
	t.Log("CASE 2b fix simulation: DOM.focus(facade), then ReplaceText+CheckValue on the active editor")
	r.load(ctx)
	o = r.origin(ctx, "#facade")
	r.call(ctx, "DOM.focus", map[string]any{"backendNodeId": o.BackendNodeID})
	ed = activeOrigin(ctx, r)
	edField, _ := ed.ReadField(ctx)
	in, _ := page.NewInput(ctx, r.c, r.session, func() bool { return false }, r.session)
	err = ed.SetValue(ctx, in, edField, "hello facade")
	in.Close()
	t.Logf("  editor field=%+v SetValue(editor): %v", edField, err)
	state("right after")

	// 3. type-text --into on the facade.
	t.Log("CASE 3 facade type-text --into (no read-back by design)")
	r.load(ctx)
	o = r.origin(ctx, "#facade")
	t.Logf("  TypeInto err: %v", runTypeInto(ctx, t, r, o, "typed facade"))
	state("right after")

	// 4. set-value on the remount editor.
	t.Log("CASE 4 remount set-value")
	r.load(ctx)
	o = r.origin(ctx, "#rm-ed")
	f, err, rerr = runSetValue(ctx, t, r, o, "hello remount")
	t.Logf("  field=%+v\n    %s", f, describe(err, rerr))
	t.Logf("  old node connected? %v", func() any {
		var b bool
		e := callOnOrigin(ctx, o, `function(){ return this.isConnected }`, &b)
		return fmt.Sprintf("%v (err %v)", b, e)
	}())
	state("right after")

	// 4b. second set-value through the same (now detached) Origin.
	t.Log("CASE 4b remount: a second set-value on the same Origin (old backendNodeId)")
	f, err, rerr = runSetValue(ctx, t, r, o, "second")
	t.Logf("  %s", describe(err, rerr))
	state("right after")

	// 5. set-value with a multi-line value on the remount editor.
	t.Log("CASE 5 remount set-value multi-line 'a\\nb' (one insertText)")
	r.load(ctx)
	o = r.origin(ctx, "#rm-ed")
	f, err, rerr = runSetValue(ctx, t, r, o, "a\nb")
	t.Logf("  %s", describe(err, rerr))
	state("right after")

	// 5c. control: the same multi-line value on the plain contenteditable.
	t.Log("CASE 5c control: plain contenteditable set-value 'a\\nb'")
	r.load(ctx)
	o = r.origin(ctx, "#plain-ce")
	f, err, rerr = runSetValue(ctx, t, r, o, "a\nb")
	t.Logf("  %s", describe(err, rerr))
	t.Logf("  plain-ce innerText: %q", r.eval(ctx, "document.getElementById('plain-ce').innerText"))

	// 6. type-text --into on the remount editor, split text (insert, Return, insert).
	t.Log("CASE 6 remount type-text --into 'one\\ntwo'")
	r.load(ctx)
	o = r.origin(ctx, "#rm-ed")
	t.Logf("  TypeInto err: %v", runTypeInto(ctx, t, r, o, "one\ntwo"))
	state("right after")
}

func callOnOrigin(ctx context.Context, o *page.Origin, fn string, out any) error {
	res, err := o.Client.CallSession(ctx, o.SessionID, "DOM.resolveNode", map[string]any{"backendNodeId": o.BackendNodeID})
	if err != nil {
		return err
	}
	var rn struct {
		Object struct {
			ObjectID string `json:"objectId"`
		} `json:"object"`
	}
	json.Unmarshal(res, &rn)
	res, err = o.Client.CallSession(ctx, o.SessionID, "Runtime.callFunctionOn", map[string]any{"objectId": rn.Object.ObjectID, "functionDeclaration": fn, "returnByValue": true})
	if err != nil {
		return err
	}
	var cr struct {
		Result struct {
			Value json.RawMessage `json:"value"`
		} `json:"result"`
	}
	json.Unmarshal(res, &cr)
	return json.Unmarshal(cr.Result.Value, out)
}
