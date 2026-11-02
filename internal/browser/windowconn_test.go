package browser

// A connection per browser process: the user's Chrome through the
// extension and a Chrome winuse launched are served side by side, each
// Window reaching the process that owns it whatever the pin says, in
// either order and again after a helper restart. Also: a Window whose
// process has no endpoint, two endpoints of one process, a re-dial while
// the other connection stays up, one approval prompt per process, and
// stop and status per connection.

import (
	"context"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/bridge/bridgetest"
	"github.com/hoangvu12/winuse/internal/cdp/cdptest"
)

// twoChromes is the user's Chrome (pid 500) with the winuse extension and
// a Chrome winuse launched (pid 100) on a cdptest server.
func twoChromes(t *testing.T) (*extFake, *bridgetest.Extension, *cdptest.Server) {
	t.Helper()
	f := newExtFake(t)
	f.install("chrome")
	f.process("chrome", 100)
	f.process("chrome", 500)
	s := f.serve(100)
	f.ownProfile("chrome", ModeLaunch, s.Port())
	ext := f.connect("chrome", "ab12cd34", &bridgetest.Extension{Tabs: []bridgetest.Tab{{TargetID: "U1", URL: "https://mine.test/"}}})
	return f, ext, s
}

func TestWindowConnectionServesEachBrowserProcess(t *testing.T) {
	for _, order := range [][]uint32{{500, 100}, {100, 500}} {
		t.Run(pidOrder(order), func(t *testing.T) {
			f, _, s := twoChromes(t)
			m := NewManager(f.sys)
			defer m.Close()
			ctx := context.Background()
			got := map[uint32]*Connection{}
			for _, pid := range order {
				c, err := m.WindowConnection(ctx, "chrome", pid)
				if err != nil {
					t.Fatalf("pid %d: %v", pid, err)
				}
				got[pid] = c
			}
			if c := got[500]; c.Mode != ModeExtension || c.PID != 500 {
				t.Errorf("the user's Chrome = %+v", c)
			}
			if c := got[100]; c.Mode != ModeLaunch || c.PID != 100 {
				t.Errorf("the launched Chrome = %+v", c)
			}
			// Both stay held and are reused, whichever is asked first.
			for i := 0; i < 2; i++ {
				for _, pid := range order {
					if c, err := m.WindowConnection(ctx, "chrome", pid); err != nil || c != got[pid] || !c.Client.Alive() {
						t.Fatalf("pid %d again: %+v, %v", pid, c, err)
					}
				}
			}
			if n := s.Upgrades(); n != 1 {
				t.Errorf("handshakes to the launched Chrome = %d, want 1", n)
			}
			if n := f.dialCount(); n != 1 {
				t.Errorf("WebSocket dials = %d, want 1 (the extension is not a WebSocket)", n)
			}

			// A helper restart: a new Manager maps each Window again.
			m2 := NewManager(f.sys)
			defer m2.Close()
			for _, pid := range order {
				c, err := m2.WindowConnection(ctx, "chrome", pid)
				if err != nil || c.PID != pid || c == got[pid] {
					t.Errorf("after the restart, pid %d: %+v, %v", pid, c, err)
				}
			}
		})
	}
}

func pidOrder(order []uint32) string {
	if order[0] == 500 {
		return "extension first"
	}
	return "launched first"
}

func TestThePinDoesNotStealAWindow(t *testing.T) {
	f, _, _ := twoChromes(t)
	f.install("edge")
	f.process("edge", 700)
	edge := f.serve(700)
	f.ownProfile("edge", ModeLaunch, edge.Port())
	m := NewManager(f.sys)
	defer m.Close()
	ctx := context.Background()

	// Pinned to Chrome, the commands without a Window take the extension,
	// as they always did; a Window of the launched Chrome keeps its own.
	if err := f.sys.SetPin("chrome"); err != nil {
		t.Fatal(err)
	}
	c, err := m.Connection(ctx, "")
	if err != nil || c.Mode != ModeExtension {
		t.Fatalf("pinned Connection = %+v, %v", c, err)
	}
	if w, err := m.WindowConnection(ctx, "chrome", 100); err != nil || w.PID != 100 || w.Mode != ModeLaunch {
		t.Errorf("the launched Chrome's Window = %+v, %v", w, err)
	}
	// Pinned to Edge, a Chrome Window is still Chrome's.
	if err := f.sys.SetPin("edge"); err != nil {
		t.Fatal(err)
	}
	if w, err := m.WindowConnection(ctx, "chrome", 500); err != nil || w.PID != 500 || w.Mode != ModeExtension {
		t.Errorf("the user's Chrome Window under an Edge pin = %+v, %v", w, err)
	}
	if e, err := m.Connection(ctx, ""); err != nil || e.Browser != "edge" {
		t.Errorf("pinned Edge = %+v, %v", e, err)
	}
	// With two Chromes held, a named lookup still picks as discovery does
	// (the extension first) and reuses the held connection.
	if c2, err := m.Connection(ctx, "chrome"); err != nil || c2 != c {
		t.Errorf("Connection(chrome) with two held = %+v, %v", c2, err)
	}
}

func TestAWindowOfAChromeWinuseCannotReach(t *testing.T) {
	f, _, _ := twoChromes(t)
	m := NewManager(f.sys)
	defer m.Close()
	_, err := m.WindowConnection(context.Background(), "chrome", 777)
	p := problemOf(t, err, NoWindowEndpoint)
	if p.PID != 777 || !slices.Equal(p.Others, []string{"100", "500"}) {
		t.Errorf("problem = %+v", p)
	}
	if p.Sentence() != "this Chrome window belongs to Chrome process 777, which has no debugging connection winuse can use (winuse can reach Chrome processes 100 and 500)" {
		t.Errorf("sentence = %q", p.Sentence())
	}

	// Nothing debuggable at all: the launch command is the way out.
	g := newFake(t)
	g.install("chrome")
	g.process("chrome", 777)
	_, err = NewManager(g.sys).WindowConnection(context.Background(), "chrome", 777)
	if p := problemOf(t, err, NoWindowEndpoint); len(p.Others) != 0 || !strings.Contains(p.Next(), "winuse browser ext install chrome") || !strings.Contains(p.Next(), "winuse browser launch chrome") {
		t.Errorf("problem = %+v, next %q", p, p.Next())
	}

	// The extension of that process turned off: the user's choice shows.
	h := newExtFake(t)
	h.install("chrome")
	ext := h.connect("chrome", "ab12cd34", nil)
	ext.SetPaused(true)
	_, err = NewManager(h.sys).WindowConnection(context.Background(), "chrome", 500)
	problemOf(t, err, ExtPaused)
}

func TestTwoEndpointsOfOneProcessAreRefused(t *testing.T) {
	f := newFake(t)
	f.install("chrome")
	f.process("chrome", 100)
	f.ownProfile("chrome", ModeLaunch, 9301)
	f.listen(9301, "ws://127.0.0.1:9301/devtools/browser/a", 100)
	f.ownProfile("chrome", ModeClone, 9302)
	f.listen(9302, "ws://127.0.0.1:9302/devtools/browser/b", 100)
	m := NewManager(f.sys)
	defer m.Close()
	_, err := m.WindowConnection(context.Background(), "chrome", 100)
	p := problemOf(t, err, SeveralEndpoints)
	want := "Chrome process 100 answers on port 9301 (winuse's own profile) and port 9302 (winuse's copy of your profile, with your logins), so winuse will not guess which one to use"
	if p.Sentence() != want {
		t.Errorf("sentence = %q", p.Sentence())
	}
	if f.dialCount() != 0 {
		t.Error("an ambiguous process was dialed")
	}

	// One port named by two directories is one endpoint.
	g := newFake(t)
	g.install("chrome")
	g.process("chrome", 100)
	s := g.serve(100)
	g.ownProfile("chrome", ModeLaunch, s.Port())
	g.ownProfile("chrome", ModeClone, s.Port())
	if c, err := NewManager(g.sys).WindowConnection(context.Background(), "chrome", 100); err != nil || c.Mode != ModeLaunch {
		t.Errorf("one port, two directories = %+v, %v", c, err)
	}

	// Two extension profiles in one browser process: no guess either.
	h := newExtFake(t)
	h.install("chrome")
	h.connect("chrome", "aaaa1111", nil)
	h.connect("chrome", "bbbb2222", nil)
	_, err = NewManager(h.sys).WindowConnection(context.Background(), "chrome", 500)
	if p := problemOf(t, err, ExtSeveralProfiles); !slices.Equal(p.Others, []string{"aaaa1111", "bbbb2222"}) {
		t.Errorf("instances = %v", p.Others)
	}
}

func TestOneBrowserRestartsWhileTheOtherStaysConnected(t *testing.T) {
	f, ext, s := twoChromes(t)
	m := NewManager(f.sys)
	defer m.Close()
	ctx := context.Background()
	mine, err := m.WindowConnection(ctx, "chrome", 500)
	if err != nil {
		t.Fatal(err)
	}
	old, err := m.WindowConnection(ctx, "chrome", 100)
	if err != nil {
		t.Fatal(err)
	}

	// The launched Chrome restarts in place (same process, same port).
	s.DropAll()
	select {
	case <-old.Client.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("the dropped connection never ended")
	}
	again, err := m.WindowConnection(ctx, "chrome", 100)
	if err != nil || again == old || !again.Client.Alive() {
		t.Fatalf("after the drop = %+v, %v", again, err)
	}

	// Then it restarts as a new process on a new port: its Windows belong
	// to pid 101 now, and the dead connection is forgotten.
	f.unlisten(s.Port())
	s.DropAll()
	s2 := f.serve(101)
	f.process("chrome", 101)
	f.ownProfile("chrome", ModeLaunch, s2.Port())
	<-again.Client.Done()
	next, err := m.WindowConnection(ctx, "chrome", 101)
	if err != nil || next.PID != 101 {
		t.Fatalf("the new process = %+v, %v", next, err)
	}
	if _, err := m.WindowConnection(ctx, "chrome", 100); err == nil {
		t.Error("a Window of the exited process still found a connection")
	}
	if got := m.open("chrome"); len(got) != 2 || got[0].PID != 101 || got[1].PID != 500 {
		t.Errorf("open connections = %+v", got)
	}

	// The user's Chrome was never touched.
	if c, err := m.WindowConnection(ctx, "chrome", 500); err != nil || c != mine || !mine.Client.Alive() {
		t.Errorf("the user's Chrome = %+v, %v", c, err)
	}
	if pages, err := mine.Pages(ctx); err != nil || len(pages) != 1 || pages[0].TargetID != "U1" {
		t.Errorf("the user's Chrome's Tabs = %+v, %v", pages, err)
	}
	if n := ext.Reloads(); n != 0 {
		t.Errorf("the extension reloaded %d times", n)
	}
}

func TestOneApprovalPromptPerProcess(t *testing.T) {
	// The user's Chrome (pid 300) behind the inspect toggle holds its
	// handshake for the prompt; a launched Chrome (pid 100) is not held up.
	f := newFake(t)
	f.install("chrome")
	f.process("chrome", 100)
	f.process("chrome", 300)
	launched := f.serve(100)
	f.ownProfile("chrome", ModeLaunch, launched.Port())
	toggled := f.serve(300)
	f.listen(toggled.Port(), "", 300)
	f.toggle("chrome", toggled.Port(), "/devtools/browser/fake")
	release := toggled.Hold()
	m := NewManager(f.sys)
	m.ApprovalWait = 100 * time.Millisecond
	defer m.Close()
	ctx := context.Background()

	var wg sync.WaitGroup
	errs := make([]error, 3)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = m.WindowConnection(ctx, "chrome", 300)
		}(i)
	}
	wg.Wait()
	for _, err := range errs {
		problemOf(t, err, AwaitingApproval)
	}
	if c, err := m.WindowConnection(ctx, "chrome", 100); err != nil || c.Mode != ModeLaunch {
		t.Fatalf("the launched Chrome waited on the other's prompt: %+v, %v", c, err)
	}
	if n := toggled.Upgrades(); n != 1 {
		t.Fatalf("handshakes while the prompt is up = %d, want 1", n)
	}

	release()
	deadline := time.Now().Add(2 * time.Second)
	for {
		c, err := m.WindowConnection(ctx, "chrome", 300)
		if err == nil {
			if c.Mode != ModeToggle {
				t.Errorf("mode = %s", c.Mode)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no connection after approval: %v", err)
		}
	}
	if n := toggled.Upgrades(); n != 1 {
		t.Errorf("handshakes = %d, want the one approved dial", n)
	}
	if n := launched.Upgrades(); n != 1 {
		t.Errorf("handshakes to the launched Chrome = %d", n)
	}
}

func TestStopAndStatusPerConnection(t *testing.T) {
	f, ext, s := twoChromes(t)
	s.Handle("Browser.close", func(c *cdptest.Conn, msg cdptest.Message) (any, error) {
		f.unlisten(s.Port())
		return nil, nil
	})
	m := NewManager(f.sys)
	defer m.Close()
	ctx := context.Background()
	mine, err := m.WindowConnection(ctx, "chrome", 500)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.WindowConnection(ctx, "chrome", 100); err != nil {
		t.Fatal(err)
	}

	out, err := m.Status(ctx, "chrome")
	want := "ok chrome process 500 connected: your running browser through the winuse extension, 1 tab\nalso connected: chrome process 100, winuse's own profile\n"
	if err != nil || out != want {
		t.Errorf("status = %q, %v\nwant %q", out, err, want)
	}

	// stop closes the launched Chrome only.
	if _, err := m.Stop(ctx, "chrome"); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if !mine.Client.Alive() {
		t.Error("stop closed the connection to the user's Chrome")
	}
	if c, err := m.WindowConnection(ctx, "chrome", 500); err != nil || c != mine {
		t.Errorf("the user's Chrome after stop = %+v, %v", c, err)
	}
	for _, cmd := range ext.Commands() {
		if strings.HasPrefix(cmd.Method, "Browser.") {
			t.Errorf("%s reached the extension", cmd.Method)
		}
	}
	// One connection left: the status line is as it always was.
	if out, err := m.Status(ctx, "chrome"); err != nil || out != "ok chrome connected: your running browser through the winuse extension, 1 tab\n" {
		t.Errorf("status after stop = %q, %v", out, err)
	}
}
