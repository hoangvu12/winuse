package helper

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/cdp/cdptest"
	"github.com/hoangvu12/winuse/internal/page"
	"github.com/hoangvu12/winuse/internal/protocol"
)

// ariaPage is a Page with one custom list widget on pageRig's Element 8
// (node 80): its choice read, its list as each read finds it, and its
// options as nodes 90 + i, clicked at (200, 300 + 10i).
type ariaPage struct {
	r      *pageRig
	mu     sync.Mutex
	choice map[string]any
	// list answers each "after" read of the list, given the clicks and
	// texts sent so far; nil: no list shown.
	list   func(clicks []string, typed string) map[string]any
	before map[string]any // the "before" read; nil: no list shown
	optSel map[int64]bool // an option node's own selected state after the click
	reads  int
}

func newARIAPage(t *testing.T, choice map[string]any) *ariaPage {
	t.Helper()
	listWait, noList, verify := page.ARIAListWait, page.ARIANoListWait, page.ARIAVerifyWait
	page.ARIAListWait, page.ARIANoListWait, page.ARIAVerifyWait = 150*time.Millisecond, 60*time.Millisecond, 100*time.Millisecond
	t.Cleanup(func() { page.ARIAListWait, page.ARIANoListWait, page.ARIAVerifyWait = listWait, noList, verify })
	a := &ariaPage{r: newPageRig(t), choice: choice, optSel: map[int64]bool{}}
	a.r.srv.Handle("DOM.resolveNode", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		var p struct {
			Node int64 `json:"backendNodeId"`
		}
		json.Unmarshal(m.Params, &p)
		return map[string]any{"object": map[string]any{"type": "object", "objectId": "n" + nodeID(p.Node)}}, nil
	})
	a.r.srv.Handle("DOM.describeNode", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		var p struct {
			ObjectID string `json:"objectId"`
		}
		json.Unmarshal(m.Params, &p)
		var n int64
		json.Unmarshal([]byte(strings.TrimPrefix(p.ObjectID, "opt")), &n)
		return map[string]any{"node": map[string]any{"backendNodeId": n}}, nil
	})
	a.r.srv.Handle("Runtime.callFunctionOn", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		var p struct {
			Fn   string `json:"functionDeclaration"`
			Obj  string `json:"objectId"`
			Args []struct {
				Value json.RawMessage `json:"value"`
			} `json:"arguments"`
		}
		json.Unmarshal(m.Params, &p)
		var node int64
		json.Unmarshal([]byte(strings.TrimPrefix(p.Obj, "n")), &node)
		val := func(v any) (any, error) {
			return map[string]any{"result": map[string]any{"type": "object", "value": v}}, nil
		}
		clicks, typed := a.state()
		a.mu.Lock()
		defer a.mu.Unlock()
		switch {
		case strings.Contains(p.Fn, "winuse:choice"):
			return val(a.choice)
		case strings.Contains(p.Fn, "winuse:arialist"):
			var mode string
			json.Unmarshal(p.Args[0].Value, &mode)
			if mode == "before" {
				if a.before == nil {
					return val(map[string]any{"state": "none"})
				}
				return val(a.before)
			}
			a.reads++
			if l := a.list(clicks, typed); l != nil {
				return val(l)
			}
			return val(map[string]any{"state": "none"})
		case strings.Contains(p.Fn, "winuse:ariaoption"):
			var i int64
			json.Unmarshal(p.Args[0].Value, &i)
			return map[string]any{"result": map[string]any{"type": "object", "subtype": "node", "objectId": "opt" + nodeID(90+i)}}, nil
		case strings.Contains(p.Fn, "winuse:ariaoptstate"):
			return val(map[string]any{"selected": a.optSel[node], "gone": false})
		case strings.Contains(p.Fn, "winuse:aim"):
			st := map[string]any{"state": "ok", "l": 10.0, "t": 20.0, "w": 50.0, "h": 10.0, "x": 35.0, "y": 25.0, "chain": true, "dpr": 1.0, "name": "Fruit"}
			if node >= 90 {
				st["x"], st["y"] = 200.0, 300.0+10*float64(node-90)
			}
			return val(st)
		case strings.Contains(p.Fn, "winuse:selecttext"):
			return val(nil)
		case strings.Contains(p.Fn, "winuse:select"):
			a.r.record("select")
			return val(true)
		case strings.Contains(p.Fn, "winuse:field"):
			return val(map[string]any{"kind": "text"})
		case strings.Contains(p.Fn, "winuse:read"):
			return val(typed)
		case strings.Contains(p.Fn, "isConnected"):
			return val(true)
		}
		return val(nil)
	})
	return a
}

func nodeID(n int64) string { b, _ := json.Marshal(n); return string(b) }

// state is the clicks (as "x,y") and the text sent so far.
func (a *ariaPage) state() (clicks []string, typed string) {
	for _, s := range a.r.sent() {
		if strings.HasPrefix(s, "dispatchMouseEvent mouseReleased") {
			f := strings.Fields(s)
			clicks = append(clicks, strings.TrimPrefix(f[3], "x=")+","+strings.TrimPrefix(f[4], "y="))
		}
		if strings.HasPrefix(s, "insertText ") {
			typed = strings.TrimPrefix(s, "insertText ")
		}
	}
	return clicks, typed
}

func fruits(names ...string) []map[string]any {
	var out []map[string]any
	for _, n := range names {
		out = append(out, map[string]any{"name": n})
	}
	return out
}

func (a *ariaPage) set(t *testing.T, v string) (string, *Error) {
	t.Helper()
	return a.r.run(t, "set-value", protocol.SetValueArgs{Window: "301", ElementIndex: idx(8), Value: v})
}

// TestPageSetValueARIATrigger: a Radix-like trigger is opened by a trusted
// click, its option clicked where it shows, and the trigger read back.
func TestPageSetValueARIATrigger(t *testing.T) {
	a := newARIAPage(t, map[string]any{"kind": "aria", "expanded": false})
	a.before = map[string]any{"state": "none", "value": "Pick a fruit"}
	a.list = func(clicks []string, typed string) map[string]any {
		switch len(clicks) {
		case 0:
			return nil
		case 1:
			return map[string]any{"state": "ok", "lists": 1, "expanded": true, "value": "Pick a fruit", "options": fruits("Apple", "Banana", "Blood orange")}
		}
		return map[string]any{"state": "none", "value": "Banana"} // the list closed on the choice
	}
	out, e := a.r.run(t, "set-value", protocol.SetValueArgs{Window: "301", ElementIndex: idx(8), Value: "banana"})
	if e != nil || out != okOutput {
		t.Fatalf("set-value: %q %+v", out, e)
	}
	clicks, _ := a.state()
	if strings.Join(clicks, " ") != "35,25 200,310" {
		t.Errorf("clicks %v (sent %v)", clicks, a.r.sent())
	}
	if len(a.r.cursor.labels) != 2 || a.r.cursor.labels[0] != `select "Email"` || a.r.cursor.labels[1] != `select "Banana"` {
		t.Errorf("cursor labels %q", a.r.cursor.labels)
	}
	a.r.noForeground(t)
}

// TestPageSetValueARIANoSuchOption: the list it opened has no such option:
// a delivered failure naming the options, the list left open; an open
// widget's list without it is refused with nothing sent.
func TestPageSetValueARIANoSuchOption(t *testing.T) {
	a := newARIAPage(t, map[string]any{"kind": "aria", "expanded": false})
	a.list = func(clicks []string, typed string) map[string]any {
		if len(clicks) == 0 {
			return nil
		}
		return map[string]any{"state": "ok", "lists": 1, "expanded": true, "options": fruits("Apple", "Banana")}
	}
	_, e := a.set(t, "Kiwi")
	if e == nil || e.Msg != `cannot set element 8: the list has no option "Kiwi"; its options are "Apple", "Banana"; the click opened its list, which is still open` ||
		!strings.Contains(e.Hint, "do not repeat it") {
		t.Errorf("opened: %+v", e)
	}

	// Open already: nothing is clicked, and nothing was sent.
	b := newARIAPage(t, map[string]any{"kind": "aria", "expanded": true})
	b.list = func(clicks []string, typed string) map[string]any {
		return map[string]any{"state": "ok", "lists": 1, "expanded": true, "options": fruits("Apple", "Banana")}
	}
	_, e = b.set(t, "Kiwi")
	if e == nil || e.Msg != `cannot set element 8: the list has no option "Kiwi"; its options are "Apple", "Banana"; nothing was sent` {
		t.Errorf("open: %+v", e)
	}
	if len(b.r.sent()) != 0 {
		t.Errorf("sent %v", b.r.sent())
	}

	// Several options of the same name: an error, never a guess.
	b.list = func(clicks []string, typed string) map[string]any {
		return map[string]any{"state": "ok", "lists": 1, "expanded": true, "options": fruits("Apple", "APPLE")}
	}
	_, e = b.set(t, "apple")
	if e == nil || !strings.HasPrefix(e.Msg, `cannot set element 8: several options match "apple": "Apple (option 1 of 2)", "APPLE (option 2 of 2)"`) || !strings.HasSuffix(e.Msg, nothingSentNote) {
		t.Errorf("several: %+v", e)
	}

	// No list shows at all after the click.
	c := newARIAPage(t, map[string]any{"kind": "aria"})
	c.list = func(clicks []string, typed string) map[string]any { return nil }
	_, e = c.set(t, "Apple")
	if e == nil || !strings.Contains(e.Msg, "it shows no list of options") || !strings.HasSuffix(e.Msg, "the click was delivered, and no list showed") {
		t.Errorf("no list: %+v", e)
	}

	// A disabled option.
	d := newARIAPage(t, map[string]any{"kind": "aria", "expanded": true})
	d.list = func(clicks []string, typed string) map[string]any {
		return map[string]any{"state": "ok", "lists": 1, "options": []map[string]any{{"name": "Apple", "disabled": true}}}
	}
	_, e = d.set(t, "Apple")
	if e == nil || e.Msg != `cannot set element 8: its option "Apple" is disabled; nothing was sent` {
		t.Errorf("disabled: %+v", e)
	}

	// No option named: refused before anything.
	_, e = d.set(t, "  ")
	if e == nil || !strings.Contains(e.Msg, "set-value picks one of its options by name") || !strings.HasSuffix(e.Msg, nothingSentNote) {
		t.Errorf("empty: %+v", e)
	}
}

// TestPageSetValueARIAUnverified: the option was clicked, and the widget
// does not show it chosen: delivered, unverified.
func TestPageSetValueARIAUnverified(t *testing.T) {
	a := newARIAPage(t, map[string]any{"kind": "aria", "expanded": true})
	a.before = map[string]any{"state": "ok", "value": "Pick"}
	a.list = func(clicks []string, typed string) map[string]any {
		return map[string]any{"state": "ok", "lists": 1, "value": "Pick", "options": fruits("Apple", "Banana")}
	}
	_, e := a.set(t, "Apple")
	if e == nil || !strings.HasPrefix(e.Msg, `the option "Apple" was clicked, but element 8 does not show it chosen afterwards`) || !strings.Contains(e.Hint, "do not repeat it") {
		t.Errorf("unverified: %+v", e)
	}
	// The option reads selected: verified, although the list stays open.
	a.optSel[90] = true
	if out, e := a.set(t, "Apple"); e != nil || out != okOutput {
		t.Errorf("selected: %q %+v", out, e)
	}
}

// TestPageSetValueARIAListbox: a listbox named itself; an option already
// selected in a multi-select list is left alone (a click turns it off).
func TestPageSetValueARIAListbox(t *testing.T) {
	a := newARIAPage(t, map[string]any{"kind": "aria", "list": true})
	a.list = func(clicks []string, typed string) map[string]any {
		return map[string]any{"state": "ok", "lists": 1, "multi": true, "options": []map[string]any{{"name": "Apple", "selected": true}, {"name": "Banana"}}}
	}
	if out, e := a.set(t, "Apple"); e != nil || out != okOutput || len(a.r.sent()) != 0 {
		t.Errorf("already selected: %q %+v %v", out, e, a.r.sent())
	}
	a.optSel[91] = true
	if out, e := a.set(t, "Banana"); e != nil || out != okOutput {
		t.Fatalf("banana: %q %+v", out, e)
	}
	// Nothing was sent before the option's click: the cursor stays on the
	// listbox, which ripples on it.
	if clicks, _ := a.state(); strings.Join(clicks, " ") != "200,310" || len(a.r.cursor.labels) != 2 {
		t.Errorf("clicks %v, labels %q", clicks, a.r.cursor.labels)
	}
}

// TestPageSetValueARIATypeahead: a combobox text field is typed into, then
// the option its text names is clicked; with no list it is a plain field;
// with a list that lacks the text, the text stays and a note names the
// options.
func TestPageSetValueARIATypeahead(t *testing.T) {
	choice := map[string]any{"kind": "aria", "editable": true, "field": "text"}
	a := newARIAPage(t, choice)
	a.list = func(clicks []string, typed string) map[string]any {
		switch {
		case typed == "":
			return nil
		case len(clicks) == 0:
			return map[string]any{"state": "ok", "lists": 1, "expanded": true, "value": typed, "options": fruits("Apple", "Apricot")}
		}
		return map[string]any{"state": "none", "value": "Apple"}
	}
	if out, e := a.set(t, "apple"); e != nil || out != okOutput {
		t.Fatalf("typeahead: %q %+v", out, e)
	}
	got := strings.Join(a.r.sent(), ",")
	if !strings.HasPrefix(got, "focus,select,insertText apple,") || !strings.Contains(got, "mouseReleased left x=200 y=300") {
		t.Errorf("sent %s", got)
	}

	// No list shows: a plain text field, read back as one.
	b := newARIAPage(t, choice)
	b.list = func(clicks []string, typed string) map[string]any { return nil }
	if out, e := b.set(t, "free text"); e != nil || out != okOutput {
		t.Errorf("plain: %q %+v", out, e)
	}
	if clicks, _ := b.state(); len(clicks) != 0 {
		t.Errorf("plain clicked %v", clicks)
	}

	// A list without the text: kept, with a note.
	c := newARIAPage(t, choice)
	c.list = func(clicks []string, typed string) map[string]any {
		if typed == "" {
			return nil
		}
		return map[string]any{"state": "ok", "lists": 1, "options": fruits("cats", "cat videos")}
	}
	out, e := c.set(t, "cat")
	if e != nil || out != "ok\nNote: element 8 holds the text typed, but no option was picked: the list has no option \"cat\"; its options are \"cats\", \"cat videos\"\n" {
		t.Errorf("no match: %q %+v", out, e)
	}

	// Clearing a combobox's text is the text route's.
	d := newARIAPage(t, choice)
	d.list = func(clicks []string, typed string) map[string]any { return nil }
	if out, e := d.set(t, ""); e != nil || out != okOutput || !strings.Contains(strings.Join(d.r.sent(), ","), "dispatchKeyEvent") {
		t.Errorf("clear: %q %+v %v", out, e, d.r.sent())
	}
}
