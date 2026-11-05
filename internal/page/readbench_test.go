package page

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/browser"
	"github.com/hoangvu12/winuse/internal/cdp"
	"github.com/hoangvu12/winuse/internal/cdp/cdptest"
)

// The offline Page read benchmark (speed issue 07): a cdp.Transport that
// answers from a synthetic page after a modelled round trip, so the read's
// sequential round trips show as time. Each command reaches the browser
// after half the round trip, waits its turn in its session (a browser runs
// one session's commands in order), and its answer comes back after the
// other half. Answers cost no browser time here: what is measured is the
// read's own shape and its Go-side decoding, not the renderer.

// slowWire is the modelled connection.
type slowWire struct {
	oneWay time.Duration
	serve  func(m cdptest.Message, emit func(session, method string, params any)) (any, error)

	out    chan []byte
	closed chan struct{}
	once   sync.Once

	mu       sync.Mutex
	sessions map[string]chan func()
	depth    int // the deepest wave an answer delivered so far belongs to
	waves    int // the deepest wave any command was sent in
	calls    atomic.Int64
	methods  map[string]int
}

func newSlowWire(oneWay time.Duration, serve func(cdptest.Message, func(string, string, any)) (any, error)) *slowWire {
	return &slowWire{oneWay: oneWay, serve: serve, out: make(chan []byte, 1024), closed: make(chan struct{}), sessions: map[string]chan func(){}, methods: map[string]int{}}
}

func (w *slowWire) WriteText(data []byte) error {
	var m cdptest.Message
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}
	w.calls.Add(1)
	w.mu.Lock()
	wave := w.depth + 1
	if wave > w.waves {
		w.waves = wave
	}
	w.methods[m.Method]++
	q := w.sessions[m.SessionID]
	if q == nil {
		q = make(chan func(), 1024)
		w.sessions[m.SessionID] = q
		go func() {
			for {
				select {
				case f := <-q:
					f()
				case <-w.closed:
					return
				}
			}
		}()
	}
	w.mu.Unlock()
	go func() {
		w.sleep(w.oneWay)
		q <- func() {
			var msgs [][]byte
			emit := func(session, method string, params any) {
				ev := map[string]any{"method": method, "params": params}
				if session != "" {
					ev["sessionId"] = session
				}
				b, _ := json.Marshal(ev)
				msgs = append(msgs, b)
			}
			res, err := w.serve(m, emit)
			reply := map[string]any{"id": m.ID}
			if m.SessionID != "" {
				reply["sessionId"] = m.SessionID
			}
			if err != nil {
				reply["error"] = map[string]any{"code": -32000, "message": err.Error()}
			} else {
				if res == nil {
					res = struct{}{}
				}
				reply["result"] = res
			}
			b, _ := json.Marshal(reply)
			msgs = append(msgs, b)
			go func() {
				w.sleep(w.oneWay)
				// Counted before the client can see the answer, so a command
				// sent on it is never taken for one of the same wave.
				w.mu.Lock()
				if wave > w.depth {
					w.depth = wave
				}
				w.mu.Unlock()
				for _, b := range msgs {
					select {
					case w.out <- b:
					case <-w.closed:
						return
					}
				}
			}()
		}
	}()
	return nil
}

// sleep waits d precisely: Windows' timer granularity would add about a
// millisecond to every half round trip, so the last stretch spins.
func (w *slowWire) sleep(d time.Duration) {
	if d <= 0 {
		return
	}
	deadline := time.Now().Add(d)
	if d > 2*time.Millisecond {
		time.Sleep(d - 1500*time.Microsecond)
	}
	for time.Now().Before(deadline) {
		runtime.Gosched()
	}
}

func (w *slowWire) ReadMessage(time.Time) ([]byte, error) {
	select {
	case b := <-w.out:
		return b, nil
	case <-w.closed:
		return nil, io.EOF
	}
}

func (w *slowWire) Close() error {
	w.once.Do(func() { close(w.closed) })
	return nil
}

// reset starts counting a new read: waves count from the next command.
func (w *slowWire) reset() {
	w.mu.Lock()
	w.depth, w.waves = 0, 0
	w.methods = map[string]int{}
	w.mu.Unlock()
	w.calls.Store(0)
}

func (w *slowWire) stats() (waves int, calls int64, methods map[string]int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	m := map[string]int{}
	for k, v := range w.methods {
		m[k] = v
	}
	return w.waves, w.calls.Load(), m
}

// benchPage is a synthetic shop-like page: sections of rows, each row a
// role-less clickable div (cursor:pointer), a link and a paragraph, plus a
// password field; and a cross-origin iframe with a few clickables of its
// own. Its answers are given both ways: the clickables.js pass's
// (Runtime.getProperties plus describeNode) and DOMSnapshot's.
type benchPage struct {
	ax       map[string][]AXNode         // by session
	props    map[string][]map[string]any // by session
	nodes    map[string]int64            // describeNode: objectId to backendNodeId
	snapshot map[string]map[string]any   // by session
}

func newBenchPage(rows, frameRows int) *benchPage {
	p := &benchPage{ax: map[string][]AXNode{}, props: map[string][]map[string]any{}, nodes: map[string]int64{}, snapshot: map[string]map[string]any{}}
	p.add("session-T3", "F1", rows, 1)
	p.add("CHILD1", "F2", frameRows, 100000)
	return p
}

// add builds one document of rows rows over a session.
func (p *benchPage) add(session, frame string, rows int, first int64) {
	pointer := map[string]string{"cursor": "pointer", "display": "block"}
	block := map[string]string{"display": "block"}
	var sections []*tn
	for s := 0; s*5 < rows; s++ {
		var rs []*tn
		for r := s * 5; r < rows && r < s*5+5; r++ {
			rs = append(rs, el("DIV", nil, block,
				el("DIV", []string{"class", "card"}, pointer, el("SPAN", nil, nil, txt(fmt.Sprintf("Item %d", r)))),
				el("A", []string{"href", fmt.Sprintf("/item/%d", r)}, nil, txt(fmt.Sprintf("Link %d", r))),
				el("P", nil, block, txt("Free shipping on orders over 50 EUR, returns within 30 days.")),
			))
		}
		sections = append(sections, el("SECTION", nil, block, rs...))
	}
	form := el("FORM", nil, block, el("INPUT", []string{"type", "password", "id", "pw"}, map[string]string{"display": "inline-block"}))
	doc := page(append(sections, form)...)
	doc.number(first)

	// The accessibility tree: one node per element and text node, the
	// clickable divs ignored generics as Chrome reports them.
	var nodes []AXNode
	var props []map[string]any
	var walk func(n *tn, parent string)
	walk = func(n *tn, parent string) {
		id := fmt.Sprint(n.id)
		node := AXNode{NodeID: id, ParentID: parent, BackendDOMNodeID: n.id}
		role := "generic"
		switch n.name {
		case "#document":
			role = "RootWebArea"
			node.FrameID = frame
		case "#text":
			role = "StaticText"
			node.Name = axString(n.value)
		case "A":
			role = "link"
			node.Name = axString(n.kids[0].value)
		case "P":
			role = "paragraph"
		case "INPUT":
			role = "textbox"
			node.Value = axString("••••")
		case "DIV":
			if len(n.attrs) > 0 {
				node.Ignored = true
				pid := len(props)
				obj := fmt.Sprintf("%s-el-%d", session, n.id)
				props = append(props,
					propEntry(fmt.Sprint(pid), map[string]any{"type": "object", "subtype": "node", "objectId": obj}),
					propEntry(fmt.Sprint(pid+1), map[string]any{"type": "string", "value": "c"}),
					propEntry(fmt.Sprint(pid+2), map[string]any{"type": "string", "value": n.kids[0].kids[0].value}))
				p.nodes[obj] = n.id
			}
		}
		if n.name == "INPUT" {
			pid := len(props)
			obj := fmt.Sprintf("%s-el-%d", session, n.id)
			props = append(props,
				propEntry(fmt.Sprint(pid), map[string]any{"type": "object", "subtype": "node", "objectId": obj}),
				propEntry(fmt.Sprint(pid+1), map[string]any{"type": "string", "value": "p"}),
				propEntry(fmt.Sprint(pid+2), map[string]any{"type": "string", "value": ""}))
			p.nodes[obj] = n.id
		}
		node.Role = axRole(role)
		at := len(nodes)
		nodes = append(nodes, node)
		for _, k := range n.kids {
			if k.name == "HEAD" {
				continue
			}
			nodes[at].ChildIDs = append(nodes[at].ChildIDs, fmt.Sprint(k.id))
			walk(k, id)
		}
	}
	walk(doc, "")
	p.ax[session] = nodes
	p.props[session] = props
	p.snapshot[session] = snapshotOf([]string{frame}, doc)
}

func axString(s string) *AXValue {
	b, _ := json.Marshal(s)
	return &AXValue{Type: "computedString", Value: b}
}

func axRole(s string) *AXValue {
	b, _ := json.Marshal(s)
	return &AXValue{Type: "role", Value: b}
}

// serve answers the read's commands from the page.
func (p *benchPage) serve(m cdptest.Message, emit func(string, string, any)) (any, error) {
	switch m.Method {
	case "Target.getTargets":
		return map[string]any{"targetInfos": []cdptest.Target{
			{TargetID: "T3", Type: "page", Title: "Checkout", URL: "https://shop.test/checkout"},
			{TargetID: "F2", Type: "iframe", URL: "https://pay.test/"},
		}}, nil
	case "Browser.getWindowForTarget":
		return map[string]any{"windowId": 1, "bounds": map[string]any{"left": 100, "top": 50, "width": 1200, "height": 800, "windowState": "normal"}}, nil
	case "Target.attachToTarget":
		return map[string]any{"sessionId": "session-T3"}, nil
	case "Target.setAutoAttach":
		emit(m.SessionID, "Target.attachedToTarget", map[string]any{
			"sessionId": "CHILD1", "targetInfo": map[string]any{"targetId": "F2", "type": "iframe", "url": "https://pay.test/"}, "waitingForDebugger": false,
		})
		return nil, nil
	case "Page.getFrameTree":
		if m.SessionID == "CHILD1" {
			return map[string]any{"frameTree": map[string]any{"frame": map[string]any{"id": "F2", "loaderId": "L2"}}}, nil
		}
		return map[string]any{"frameTree": map[string]any{
			"frame":       map[string]any{"id": "F1", "loaderId": "L1"},
			"childFrames": []any{map[string]any{"frame": map[string]any{"id": "F2", "loaderId": "L2"}}},
		}}, nil
	case "Accessibility.getFullAXTree":
		return map[string]any{"nodes": p.ax[m.SessionID]}, nil
	case "Runtime.evaluate":
		var e struct {
			Expression string `json:"expression"`
		}
		json.Unmarshal(m.Params, &e)
		if strings.Contains(e.Expression, "cursor-pointer pass") {
			return map[string]any{"result": map[string]any{"type": "object", "subtype": "array", "objectId": "marks-" + m.SessionID}}, nil
		}
		return map[string]any{"result": map[string]any{"type": "boolean", "value": true}}, nil
	case "Runtime.getProperties":
		return map[string]any{"result": p.props[m.SessionID]}, nil
	case "DOM.describeNode":
		var d struct {
			ObjectID string `json:"objectId"`
		}
		json.Unmarshal(m.Params, &d)
		return map[string]any{"node": map[string]any{"backendNodeId": p.nodes[d.ObjectID]}}, nil
	case "DOM.getFrameOwner":
		return map[string]any{"backendNodeId": 99999}, nil
	case "DOMSnapshot.captureSnapshot":
		if s := p.snapshot[m.SessionID]; s != nil {
			return s, nil
		}
		return nil, fmt.Errorf("no snapshot")
	}
	return nil, nil
}

// benchRead sets up a warm Reader over a slowWire (the Tab attached, as in
// the helper after its first read) and returns a read function.
func benchRead(tb testing.TB, oneWay time.Duration, rows int, js bool) (*slowWire, func() *Raw) {
	tb.Helper()
	p := newBenchPage(rows, 5)
	w := newSlowWire(oneWay, p.serve)
	c := cdp.NewClient(w)
	tb.Cleanup(func() { c.Close() })
	conn := &browser.Connection{Browser: "chrome", Mode: browser.ModeLaunch, PID: 4242, Client: c}
	r := testReader()
	setJSMarks(r, js)
	g := Geometry{Title: "Checkout - Google Chrome", DPI: 144, Rect: Box{Left: 150, Top: 75, Right: 1950, Bottom: 1275}}
	read := func() *Raw {
		raw, err := r.Read(context.Background(), conn, g)
		if err != nil {
			tb.Fatal(err)
		}
		return raw
	}
	read() // attach, auto-attach and the first enables: the helper pays them once
	return w, read
}

// BenchmarkPageRead times a warm Page read of the synthetic page (200
// clickables, a cross-origin iframe) at modelled round trips: 0 (Go-side
// work only), 1 ms (a direct CDP connection on this machine) and 4 ms (the
// extension's bridge, two more processes each way). It reports the
// sequential round trips (waves) and the commands sent per read.
//
//	go test ./internal/page -run '^$' -bench PageRead -benchtime 20x
func BenchmarkPageRead(b *testing.B) {
	for _, mode := range []string{"snapshot", "js"} {
		for _, rtt := range []time.Duration{0, time.Millisecond, 4 * time.Millisecond} {
			b.Run(fmt.Sprintf("%s/rtt=%s", mode, rtt), func(b *testing.B) {
				w, read := benchRead(b, rtt/2, 200, mode == "js")
				b.ResetTimer()
				var waves int
				var calls int64
				for i := 0; i < b.N; i++ {
					w.reset()
					read()
					waves, calls, _ = w.stats()
				}
				b.ReportMetric(float64(waves), "waves/read")
				b.ReportMetric(float64(calls), "calls/read")
			})
		}
	}
}

func setJSMarks(r *Reader, js bool) { r.JSMarks = js }

// TestReadOverlapsItsRoundTrips: a warm read of the benchmark page through
// the clickables.js pass (the fallback) sends no enable and overlaps its
// documents' reads: the Tab's own read and its
// iframe's go at once, so the round trips one after another are about
// those of the Tab's document alone: the find, the burst, the pass's
// evaluate, getProperties, 201 describeNode 16 at a time, the release.
// The read before overlapped nothing: 32.
func TestReadOverlapsItsRoundTrips(t *testing.T) {
	w, read := benchRead(t, time.Millisecond, 200, true)
	w.reset()
	read()
	waves, calls, methods := w.stats()
	if waves > 24 {
		t.Errorf("%d round trips one after another, want at most 24 (%v)", waves, methods)
	}
	if methods["DOM.enable"] != 0 || methods["Accessibility.enable"] != 0 {
		t.Errorf("a warm read turned domains on: %v", methods)
	}
	if calls != 221 {
		t.Errorf("%d commands, want 221: %v", calls, methods)
	}
}

// TestReadTurnsDomainsOnOnce: the first read of a session turns DOM and
// Accessibility on, with its other commands; later reads do not, until
// enableAgain has passed since the session's last read (the extension's
// silent idle detach).
func TestReadTurnsDomainsOnOnce(t *testing.T) {
	p := newBenchPage(5, 5)
	w := newSlowWire(0, p.serve)
	c := cdp.NewClient(w)
	t.Cleanup(func() { c.Close() })
	conn := &browser.Connection{Browser: "chrome", Mode: browser.ModeLaunch, PID: 4242, Client: c}
	r := testReader()
	g := Geometry{Title: "Checkout - Google Chrome", DPI: 144, Rect: Box{Left: 150, Top: 75, Right: 1950, Bottom: 1275}}
	for i, want := range []int{2, 0, 0} {
		w.reset()
		if _, err := r.Read(context.Background(), conn, g); err != nil {
			t.Fatal(err)
		}
		_, _, m := w.stats()
		if m["DOM.enable"] != want || m["Accessibility.enable"] != want {
			t.Errorf("read %d: %d DOM.enable, %d Accessibility.enable; want %d each (the Tab and its iframe)", i, m["DOM.enable"], m["Accessibility.enable"], want)
		}
	}
	// As if the last read were enableAgain ago.
	r.mu.Lock()
	for _, ts := range r.tabs {
		ts.mu.Lock()
		for s := range ts.read {
			ts.read[s] = time.Now().Add(-enableAgain - time.Second)
		}
		ts.mu.Unlock()
	}
	r.mu.Unlock()
	w.reset()
	if _, err := r.Read(context.Background(), conn, g); err != nil {
		t.Fatal(err)
	}
	if _, _, m := w.stats(); m["DOM.enable"] != 2 {
		t.Errorf("after enableAgain: %d DOM.enable, want 2", m["DOM.enable"])
	}
}
