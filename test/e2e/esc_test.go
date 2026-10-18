//go:build e2e

// The ticket-12 seam-1 tests: the physical Esc key stops the in-progress
// Action or Batch, and injected Esc keys do not. User-run only (see the
// package comment and test/e2e/README.md for the opt-in rules); an agent
// must never start them.
//
// The stop test is interactive: only the user's physical Esc press can
// trip the hook (injected keys are ignored by design — that is the other
// test), so the test starts a long Batch against the fixture, waits for
// the typing to start, prints a prompt, and then asserts everything the
// ticket requires of the stop:
//
//   - the command exits 130 with the message that says the user stopped
//     winuse with the physical Escape key (plus the tell-the-agent hint);
//   - the remaining Batch steps did not run (no button click, no text
//     after the stopped step);
//   - the typing aborted part-way (a proper prefix of the long text);
//   - the fixture never saw the Esc key itself (the hook swallows it the
//     way Codex does — the app must not act on the stop key);
//   - the in-control overlay is gone once winuse stopped.
//
// The injected test is self-contained: while a Batch types, a concurrent
// `press-key Escape` injects the key — it must reach the app (the fixture
// records it) without stopping the Batch, which still prints ok.

package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/fixture"
)

// escStopMessage is the user-facing line every Esc stop must print
// (restated here so the test asserts the observable output, not a shared
// constant).
const escStopMessage = "winuse was stopped by the user with the physical Escape key"

// readFixtureState reads the fixture's state file (zero value on any
// error; polling callers try again).
func readFixtureState(path string) fixture.State {
	var st fixture.State
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &st)
	}
	return st
}

// waitForTyping polls the fixture's state until its text field holds at
// least min runes of the batch's typing, or the background command ends
// first (the user may have stopped the batch before the typing began).
func waitForTyping(t *testing.T, fix *fixtureProc, bg <-chan actionResult, min int) (started bool, ended *actionResult) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case res := <-bg:
			return false, &res
		default:
		}
		if len([]rune(readFixtureState(fix.state).Text)) >= min {
			return true, nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false, nil
}

func TestEscStopsTheLongBatch(t *testing.T) {
	optIn(t)
	fix, window := startFixtureWithState(t)

	// The long Batch: click the text field, type for well over ten
	// seconds, then click a button and type again — the steps after the
	// typing must never run once the user presses Esc.
	//
	// 1900 words of 16 runes ≈ 30k runes; typing runs at ~1.6k runes/s
	// (16 runes per batch with a 10 ms pause), so the user has roughly
	// eighteen seconds after the prompt.
	const words = 1900
	text := strings.Repeat("winuse esc stop ", words)
	after := "typed after the stop"
	bg := runActionAsync(t, []string{
		"batch", window,
		"click", "id:text-field",
		"then", "type-text", text,
		"then", "click", "name:Click Me",
		"then", "type-text", after,
	}, "WINUSE_HELPER_IDLE="+e2eIdle)

	started, ended := waitForTyping(t, fix, bg, 50)
	if ended != nil {
		// The batch ended before any text: the press must have stopped it
		// during the click step.
		assertEscStopped(t, "the batch (stopped before the typing)", ended)
		return
	}
	if !started {
		res := <-bg
		t.Fatalf("the batch's typing never started; result: exit %d, stderr:\n%s", res.exit, res.stderr)
	}

	// The prompt. The typing has begun; press the physical Esc key now.
	fmt.Printf("\n\n    >>> PRESS THE PHYSICAL ESC KEY NOW — you have ~15 s <<<\n\n\n")
	t.Log("PRESS THE PHYSICAL ESC KEY NOW (~15 s left of typing)")

	select {
	case res := <-bg:
		assertEscStopped(t, "the interrupted batch", &res)
	case <-time.After(75 * time.Second):
		t.Fatal("the batch did not end within 75 s; did you press Esc?")
	}

	// The typing aborted part-way: the fixture holds a proper prefix of
	// the long text, and none of the steps after it ran.
	st := fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return true })
	typed := st.Text
	if !strings.HasPrefix(text, typed) || len([]rune(typed)) >= len([]rune(text)) {
		t.Errorf("the fixture's text (%d runes) is not a partial prefix of the batch's text (%d runes); the typing did not abort",
			len([]rune(typed)), len([]rune(text)))
	}
	if strings.Contains(typed, after) {
		t.Error("text from after the stopped step reached the app; the remaining steps must not run")
	}
	if st.LastButton != "" {
		t.Errorf("the click step after the typing ran anyway; lastButton = %q, want empty", st.LastButton)
	}
	// The swallow: the Esc key itself must not have reached the app —
	// the fixture records every key-down it sees, and a leaked Esc would
	// appear as lastKey "Escape".
	if st.LastKey == "Escape" {
		t.Error("the physical Esc key reached the app; the hook must swallow the stop key (Codex behaviour)")
	}

	// The in-control indicator went away with the stopped batch.
	waitOverlayGone(t, 5*time.Second)
}

// assertEscStopped pins the stopped-command contract: exit 130 (Codex's
// interrupt code), nothing on stdout, the user-stopped-winuse message and
// its hint on stderr.
func assertEscStopped(t *testing.T, what string, res *actionResult) {
	t.Helper()
	if res.exit != 130 {
		t.Fatalf("%s: exit %d, want 130 (Codex's interrupt code); did you press Esc? stderr:\n%s", what, res.exit, res.stderr)
	}
	if res.stdout != "" {
		t.Errorf("%s: stdout = %q, want empty", what, res.stdout)
	}
	if !strings.Contains(res.stderr, escStopMessage) {
		t.Errorf("%s: stderr = %q, want it to contain %q", what, res.stderr, escStopMessage)
	}
	if !strings.Contains(res.stderr, "hint: ") || !strings.Contains(res.stderr, "ask the user") {
		t.Errorf("%s: stderr = %q, want the stop's hint (stop and ask the user)", what, res.stderr)
	}
}

func TestInjectedEscDoesNotStopTheBatch(t *testing.T) {
	optIn(t)
	fix, window := startFixtureWithState(t)

	// A Batch typing for several seconds, and a concurrent press-key
	// injecting the Esc key into the same Window while it runs. The
	// injected key must reach the app (the fixture records the key-down)
	// and must NOT stop the Batch: only physical presses do.
	const words = 700 // ~11k runes, ~7 s of typing
	text := strings.Repeat("injected esc ", words)
	bg := runActionAsync(t, []string{
		"batch", window,
		"click", "id:text-field",
		"then", "type-text", text,
	}, "WINUSE_HELPER_IDLE="+e2eIdle)

	started, ended := waitForTyping(t, fix, bg, 100)
	if ended != nil {
		t.Fatalf("the batch ended before the injection could run: exit %d, stderr:\n%s", ended.exit, ended.stderr)
	}
	if !started {
		t.Fatal("the batch's typing never started")
	}

	stdout, stderr, exit := runWinuse(t, []string{"press-key", window, "Escape"}, "WINUSE_HELPER_IDLE="+e2eIdle)
	assertActionOK(t, "press-key Escape while a batch types", stdout, stderr, exit)

	// The Batch finished normally: the injected key did not stop it.
	select {
	case res := <-bg:
		assertActionOK(t, "the batch the Esc was injected into", res.stdout, res.stderr, res.exit)
	case <-time.After(30 * time.Second):
		t.Fatal("the batch did not finish within 30 s of the injection")
	}

	// All the text was typed, and the injected key-down was delivered:
	// the fixture saw the Escape (proof the hook passes injected keys
	// through rather than swallowing or latching on them).
	st := fix.waitState(t, 10*time.Second, func(s *fixture.State) bool { return s.Text == text })
	if st.LastKey != "Escape" {
		t.Errorf("lastKey = %q, want \"Escape\": the injected Esc key must reach the app", st.LastKey)
	}
}
