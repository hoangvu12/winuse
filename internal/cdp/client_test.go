package cdp

// Offline tests for the WebSocket and CDP clients against cdptest's fake
// endpoint, which frames its side of the wire itself. No browser runs.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/cdp/cdptest"
)

func dial(t *testing.T, s *cdptest.Server) *Client {
	t.Helper()
	ws, err := DialWS(context.Background(), s.URL(), 2*time.Second)
	if err != nil {
		t.Fatalf("DialWS: %v", err)
	}
	c := NewClient(ws)
	t.Cleanup(func() { c.Close() })
	return c
}

func newServer(t *testing.T) *cdptest.Server {
	t.Helper()
	s := cdptest.NewServer()
	t.Cleanup(s.Close)
	return s
}

func TestCallMatchesResponsesById(t *testing.T) {
	s := newServer(t)
	// The first call is answered only after the second, so a client that
	// took the next response as its own would swap them.
	var first cdptest.Message
	var firstConn *cdptest.Conn
	held := make(chan struct{})
	s.Handle("Slow.method", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		first, firstConn = m, c
		close(held)
		return nil, cdptest.ErrNoReply
	})
	s.Handle("Fast.method", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		<-held
		c.Reply(m.ID, "", map[string]string{"who": "fast"})
		firstConn.Reply(first.ID, "", map[string]string{"who": "slow"})
		return nil, cdptest.ErrNoReply
	})
	c := dial(t, s)
	slow := make(chan string, 1)
	go func() {
		raw, err := c.Call(context.Background(), "Slow.method", nil)
		if err != nil {
			slow <- err.Error()
			return
		}
		slow <- string(raw)
	}()
	<-held
	raw, err := c.Call(context.Background(), "Fast.method", nil)
	if err != nil {
		t.Fatalf("Fast.method: %v", err)
	}
	if string(raw) != `{"who":"fast"}` {
		t.Errorf("Fast.method result = %s", raw)
	}
	if got := <-slow; got != `{"who":"slow"}` {
		t.Errorf("Slow.method result = %s", got)
	}
}

func TestCallSessionSendsTheSessionId(t *testing.T) {
	s := newServer(t)
	c := dial(t, s)
	if _, err := c.CallSession(context.Background(), "S1", "Runtime.evaluate", map[string]any{"expression": "1"}); err != nil {
		t.Fatalf("CallSession: %v", err)
	}
	got := s.Received()
	if len(got) != 1 || got[0].SessionID != "S1" || got[0].Method != "Runtime.evaluate" {
		t.Fatalf("received = %+v, want one Runtime.evaluate on S1", got)
	}
	if string(got[0].Params) != `{"expression":"1"}` {
		t.Errorf("params = %s", got[0].Params)
	}
}

func TestCallErrorResponse(t *testing.T) {
	s := newServer(t)
	s.Handle("Page.navigate", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		return nil, &cdptest.ReplyError{Code: -32000, Message: "Cannot navigate to invalid URL"}
	})
	c := dial(t, s)
	_, err := c.Call(context.Background(), "Page.navigate", nil)
	var cerr *Error
	if !errors.As(err, &cerr) {
		t.Fatalf("err = %v, want a *cdp.Error", err)
	}
	if cerr.Code != -32000 || cerr.Method != "Page.navigate" || err.Error() != "Page.navigate: Cannot navigate to invalid URL" {
		t.Errorf("error = %+v (%q)", cerr, err)
	}
}

func TestConcurrentCalls(t *testing.T) {
	s := newServer(t)
	s.Handle("Echo.n", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		return json.RawMessage(m.Params), nil
	})
	c := dial(t, s)
	var wg sync.WaitGroup
	errs := make(chan error, 64)
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			raw, err := c.Call(context.Background(), "Echo.n", map[string]int{"n": i})
			if err != nil {
				errs <- err
				return
			}
			if want := fmt.Sprintf(`{"n":%d}`, i); string(raw) != want {
				errs <- fmt.Errorf("call %d got %s", i, raw)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func TestEventsRoutePerSessionAndMethod(t *testing.T) {
	s := newServer(t)
	c := dial(t, s)
	dialogs := c.Subscribe("S1", "Page.javascriptDialogOpening")
	defer dialogs.Close()
	all2 := c.Subscribe("S2", "")
	defer all2.Close()
	browser := c.Subscribe("", "Target.targetCreated")
	defer browser.Close()
	// A round trip first, so the subscriptions are certainly registered
	// before the events are sent.
	if _, err := c.Call(context.Background(), "Ping.ping", nil); err != nil {
		t.Fatal(err)
	}
	s.Event("S2", "Page.javascriptDialogOpening", map[string]string{"message": "for S2"})
	s.Event("S1", "Page.loadEventFired", nil)
	s.Event("S1", "Page.javascriptDialogOpening", map[string]string{"message": "for S1"})
	s.Event("", "Target.targetCreated", map[string]any{"targetInfo": map[string]string{"targetId": "T9"}})
	s.Event("S2", "Network.requestWillBeSent", nil)

	ev := next(t, dialogs)
	if ev.SessionID != "S1" || !strings.Contains(string(ev.Params), "for S1") {
		t.Errorf("S1 dialog subscriber got %+v", ev)
	}
	if ev := next(t, all2); ev.Method != "Page.javascriptDialogOpening" {
		t.Errorf("S2 first event = %s", ev.Method)
	}
	if ev := next(t, all2); ev.Method != "Network.requestWillBeSent" {
		t.Errorf("S2 second event = %s", ev.Method)
	}
	if ev := next(t, browser); ev.SessionID != "" || ev.Method != "Target.targetCreated" {
		t.Errorf("browser subscriber got %+v", ev)
	}
	select {
	case ev := <-dialogs.Events():
		t.Errorf("S1 dialog subscriber got an extra event %+v", ev)
	case <-time.After(50 * time.Millisecond):
	}
}

func next(t *testing.T, s *Subscription) Event {
	t.Helper()
	select {
	case ev, ok := <-s.Events():
		if !ok {
			t.Fatal("subscription closed")
		}
		return ev
	case <-time.After(2 * time.Second):
		t.Fatal("no event within 2s")
	}
	return Event{}
}

func TestSlowSubscriberNeverStallsCalls(t *testing.T) {
	s := newServer(t)
	c := dial(t, s)
	sub := c.Subscribe("S1", "")
	defer sub.Close()
	if _, err := c.Call(context.Background(), "Ping.ping", nil); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 500; i++ {
		s.Event("S1", "Network.dataReceived", map[string]int{"i": i})
	}
	// Nobody reads the subscription, yet calls still complete.
	if _, err := c.Call(context.Background(), "Ping.ping", nil); err != nil {
		t.Fatalf("call behind 500 unread events: %v", err)
	}
	for i := 0; i < 500; i++ {
		ev := next(t, sub)
		if want := fmt.Sprintf(`{"i":%d}`, i); string(ev.Params) != want {
			t.Fatalf("event %d = %s, want in order", i, ev.Params)
		}
	}
}

func TestCloseFailsPendingCalls(t *testing.T) {
	s := newServer(t)
	s.Handle("Never.answers", func(c *cdptest.Conn, m cdptest.Message) (any, error) { return nil, cdptest.ErrNoReply })
	c := dial(t, s)
	sub := c.Subscribe("", "")
	errc := make(chan error, 1)
	go func() {
		_, err := c.Call(context.Background(), "Never.answers", nil)
		errc <- err
	}()
	waitFor(t, func() bool { return len(s.Received()) == 1 })
	c.Close()
	if err := <-errc; !errors.Is(err, ErrClosed) {
		t.Fatalf("pending call err = %v, want ErrClosed", err)
	}
	if _, err := c.Call(context.Background(), "Later.call", nil); !errors.Is(err, ErrClosed) {
		t.Errorf("call after Close err = %v, want ErrClosed", err)
	}
	if _, ok := <-sub.Events(); ok {
		t.Error("subscription still open after Close")
	}
	if c.Alive() {
		t.Error("Alive after Close")
	}
}

func TestServerDropFailsPendingCalls(t *testing.T) {
	s := newServer(t)
	s.Handle("Never.answers", func(c *cdptest.Conn, m cdptest.Message) (any, error) { return nil, cdptest.ErrNoReply })
	c := dial(t, s)
	errc := make(chan error, 1)
	go func() {
		_, err := c.Call(context.Background(), "Never.answers", nil)
		errc <- err
	}()
	waitFor(t, func() bool { return len(s.Received()) == 1 })
	s.DropAll()
	if err := <-errc; !errors.Is(err, ErrClosed) {
		t.Fatalf("pending call err = %v, want ErrClosed", err)
	}
	select {
	case <-c.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("Done not closed after the server dropped the connection")
	}
}

func TestDetachedSessionFailsItsCalls(t *testing.T) {
	s := newServer(t)
	s.Handle("Runtime.evaluate", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		c.Event("", "Target.detachedFromTarget", map[string]string{"sessionId": m.SessionID})
		return nil, cdptest.ErrNoReply
	})
	c := dial(t, s)
	_, err := c.CallSession(context.Background(), "S1", "Runtime.evaluate", nil)
	if !errors.Is(err, ErrSessionDetached) {
		t.Fatalf("err = %v, want ErrSessionDetached", err)
	}
	if !c.Alive() {
		t.Error("the connection closed with the session")
	}
}

func TestCallHonoursTheContext(t *testing.T) {
	s := newServer(t)
	s.Handle("Never.answers", func(c *cdptest.Conn, m cdptest.Message) (any, error) { return nil, cdptest.ErrNoReply })
	c := dial(t, s)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := c.Call(ctx, "Never.answers", nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want DeadlineExceeded", err)
	}
	if _, err := c.Call(context.Background(), "Ping.ping", nil); err != nil {
		t.Errorf("connection unusable after a timed-out call: %v", err)
	}
}

func TestWSReadsAndWritesLargeFrames(t *testing.T) {
	s := newServer(t)
	big := strings.Repeat("x", 70000) // 64-bit length frames both ways
	s.Handle("Echo.big", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		return json.RawMessage(m.Params), nil
	})
	c := dial(t, s)
	raw, err := c.Call(context.Background(), "Echo.big", map[string]string{"v": big})
	if err != nil {
		t.Fatalf("Echo.big: %v", err)
	}
	var v struct{ V string }
	if json.Unmarshal(raw, &v) != nil || v.V != big {
		t.Fatalf("big value lost (%d bytes back)", len(raw))
	}
	if n := s.Unmasked(); n != 0 {
		t.Errorf("%d client frames were unmasked", n)
	}
}

func TestWSReassemblesFragmentsAndAnswersPings(t *testing.T) {
	s := newServer(t)
	s.Fragment = 7
	s.Ping = true
	c := dial(t, s)
	sub := c.Subscribe("S1", "")
	defer sub.Close()
	raw, err := c.Call(context.Background(), "Echo.small", nil)
	if err != nil {
		t.Fatalf("fragmented reply: %v", err)
	}
	if string(raw) != "{}" {
		t.Errorf("fragmented reply = %s", raw)
	}
	s.Event("S1", "Page.frameNavigated", map[string]string{"url": "https://example.com/a/long/enough/url"})
	if ev := next(t, sub); !strings.Contains(string(ev.Params), "long/enough") {
		t.Errorf("fragmented event = %s", ev.Params)
	}
	// One ping before each message and one between fragments.
	waitFor(t, func() bool { return s.Pongs() >= 4 })
	if n := s.Unmasked(); n != 0 {
		t.Errorf("%d client frames were unmasked", n)
	}
}

func TestWSHandshakeRefused(t *testing.T) {
	s := newServer(t)
	s.Reject = "403 Forbidden"
	_, err := DialWS(context.Background(), s.URL(), time.Second)
	var he *HandshakeError
	if !errors.As(err, &he) || !strings.Contains(he.Status, "403") {
		t.Fatalf("err = %v, want a 403 HandshakeError", err)
	}
}

func TestWSHandshakeChecksTheAcceptKey(t *testing.T) {
	s := newServer(t)
	s.BadAccept = true
	_, err := DialWS(context.Background(), s.URL(), time.Second)
	if err == nil || !strings.Contains(err.Error(), "Sec-WebSocket-Accept") {
		t.Fatalf("err = %v, want an accept-key mismatch", err)
	}
}

func TestWSHandshakeTimesOutWhileHeld(t *testing.T) {
	s := newServer(t)
	release := s.Hold()
	defer release()
	start := time.Now()
	_, err := DialWS(context.Background(), s.URL(), 100*time.Millisecond)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want DeadlineExceeded", err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("the held handshake took %s to give up", d)
	}
}

func TestDialWSRefusesOtherSchemes(t *testing.T) {
	if _, err := DialWS(context.Background(), "wss://127.0.0.1:1/x", time.Second); err == nil || !strings.Contains(err.Error(), "only ws://") {
		t.Fatalf("err = %v, want the scheme refusal", err)
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met within 2s")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
