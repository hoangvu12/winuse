//go:build e2e

// The settle before --observe and the waits of bladebro-ideas 07, a
// subtest of TestBrowserPages (settle-and-waits), on testdata/page-settle.html:
//
//   - a click that fetches, then renders over 100 ms: --observe shows the
//     last step, with no settle note;
//   - a client-side route change (pushState, a fetch, the new view): the
//     same;
//   - a websocket that stays open: wait idle still settles while it is
//     quiet, and --observe notes a Page whose DOM every message rewrites;
//   - wait text in an open shadow root and a same-origin frame, wait
//     selector --gone, and wait js (true late, a syntax error, a throw).
//
// User-run only, like the rest of TestBrowserPages.

package e2e

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

// settleNote starts the note --observe prints for a Page that had not
// settled.
const settleNote = "Note: the Page had not settled after"

func (p *pageRun) settleAndWaits(t *testing.T) {
	u := p.srv.a.URL + "/page-settle.html"
	st := p.open(t, u, "socket open")

	// A late render: the observation shows its last step.
	out := p.act(t, "--observe=full", "click", p.window, pageIndex(t, st, "button", "e2e fetch render"))
	expectContains(t, "click fetch --observe", out, "e2e rendered")
	if strings.Contains(out, settleNote) {
		t.Errorf("a Page that settles got the settle note:\n%s", out)
	}

	// A route change: the new view, not "e2e route loading".
	st = p.state(t)
	out = p.act(t, "--observe=full", "click", p.window, pageIndex(t, st, "button", "e2e route"))
	expectContains(t, "click route --observe", out, "e2e route b")
	if strings.Contains(out, "e2e route loading") || strings.Contains(out, settleNote) {
		t.Errorf("the route change was read before it settled:\n%s", out)
	}
	p.expectEval(t, "location.pathname", "/page-settle-b")

	// The websocket keeps sending; quiet, it does not hold the settle.
	st = p.open(t, u, "socket open")
	start := time.Now()
	p.actOK(t, "wait", p.window, "idle", "--timeout", "5")
	if took := time.Since(start); took > 3*time.Second {
		t.Errorf("wait idle took %s with a quiet websocket open", took)
	}
	if n, _ := strconv.Atoi(p.eval(t, "window.e2eMessages")); n == 0 {
		t.Error("the websocket sent nothing, so the check above proves nothing")
	}

	// With the ticker on, every message changes the DOM: --observe reads
	// after its bound, with the note.
	out = p.act(t, "--observe=full", "click", p.window, pageIndex(t, st, "button", "e2e ticker"))
	expectContains(t, "click ticker --observe", out, settleNote)
	expectContains(t, "click ticker --observe", out, "its DOM kept changing")
	out, _ = p.failing(t, "wait", p.window, "idle", "--timeout", "1")
	expectContains(t, "wait idle on a ticking Page", out, "its DOM kept changing")
	p.expectEval(t, "window.e2eTicker = false", "false")

	// wait text reaches an open shadow root and a same-origin frame.
	p.actOK(t, "wait", p.window, "text", "e2e shadow text", "--timeout", "2")
	p.actOK(t, "wait", p.window, "text", "e2e frame text", "--timeout", "2")

	// --gone: the spinner goes 600 ms after the click.
	st = p.state(t)
	p.actOK(t, "click", p.window, pageIndex(t, st, "button", "e2e spin"))
	p.actOK(t, "wait", p.window, "selector", ".spinner", "--gone", "--timeout", "5")
	p.expectEval(t, "document.querySelectorAll('.spinner').length", "0")
	p.actOK(t, "wait", p.window, "text", "e2e spinning", "--gone", "--timeout", "5")

	// wait js: true 500 ms after the click; a syntax error fails at once; a
	// timeout names what the predicate threw.
	p.actOK(t, "click", p.window, pageIndex(t, st, "button", "e2e ready later"))
	p.actOK(t, "wait", p.window, "js", "window.e2eReady === true", "--timeout", "5")
	out, took := p.failing(t, "wait", p.window, "js", "window.e2eReady ===", "--timeout", "10")
	expectContains(t, "wait js syntax error", out, "SyntaxError")
	if took > 5*time.Second {
		t.Errorf("a syntax error waited %s", took)
	}
	out, _ = p.failing(t, "wait", p.window, "js", "window.e2eMissing.ready", "--timeout", "1")
	expectContains(t, "wait js that throws", out, "TypeError")
}

// settleHandlers adds the settle page's server side: /e2e-slow?ms=N answers
// after N ms (at most 2000), /e2e-ws is a websocket that sends a message
// every 50 ms until the page goes.
func settleHandlers(mux *http.ServeMux) {
	mux.HandleFunc("/e2e-slow", func(w http.ResponseWriter, r *http.Request) {
		ms, _ := strconv.Atoi(r.URL.Query().Get("ms"))
		time.Sleep(time.Duration(min(max(ms, 0), 2000)) * time.Millisecond)
		w.Header().Set("Cache-Control", "no-store")
		fmt.Fprint(w, "slow")
	})
	mux.HandleFunc("/e2e-ws", serveTickerSocket)
}

// serveTickerSocket upgrades to a websocket (RFC 6455, server side, text
// frames only) and sends the message count every 50 ms until a write
// fails, or for 5 minutes at most. What the browser sends is never read.
func serveTickerSocket(w http.ResponseWriter, r *http.Request) {
	key := r.Header.Get("Sec-WebSocket-Key")
	hj, ok := w.(http.Hijacker)
	if key == "" || !ok {
		http.Error(w, "websocket only", http.StatusBadRequest)
		return
	}
	conn, rw, err := hj.Hijack()
	if err != nil {
		return
	}
	defer conn.Close()
	sum := sha1.Sum([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	fmt.Fprintf(rw, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\n\r\n",
		base64.StdEncoding.EncodeToString(sum[:]))
	if rw.Flush() != nil {
		return
	}
	end := time.Now().Add(5 * time.Minute)
	for n := 1; time.Now().Before(end); n++ {
		if writeTextFrame(rw.Writer, strconv.Itoa(n)) != nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// writeTextFrame writes one unmasked text frame of a short message.
func writeTextFrame(w *bufio.Writer, msg string) error {
	w.WriteByte(0x81)
	w.WriteByte(byte(len(msg))) // under 126 bytes
	w.WriteString(msg)
	return w.Flush()
}
