package helper

// type-text --interval-ms (desktop-parity ticket 23): the Headed and
// Posted routes type piece by piece with the pause between, the Headless
// routes say it did not apply, and an interval out of range is refused.
// Typing and posting are faked; no desktop is touched.

import (
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/win"
)

func TestPacedUnitsKeepCRLFTogether(t *testing.T) {
	got := pacedUnits("a😀\r\nb\nc")
	want := []string{"a", "😀", "\r\n", "b", "\n", "c"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("pacedUnits = %q, want %q", got, want)
	}
}

func TestTypePacedPausesBetweenPiecesAndStops(t *testing.T) {
	var typed []string
	var slept []time.Duration
	err := typePaced("abc", 40*time.Millisecond, func() bool { return false },
		func(d time.Duration) { slept = append(slept, d) },
		func(p string) error { typed = append(typed, p); return nil })
	if err != nil || strings.Join(typed, "") != "abc" || len(typed) != 3 {
		t.Fatalf("typed %q, %v", typed, err)
	}
	if len(slept) != 2 || slept[0] != 40*time.Millisecond {
		t.Errorf("slept %v, want two 40ms pauses (between the three pieces only)", slept)
	}
	// A stop during a pause ends the typing with the Esc stop.
	typed = nil
	calls := 0
	err = typePaced("abc", time.Millisecond, func() bool { calls++; return calls >= 1 },
		func(time.Duration) {}, func(p string) error { typed = append(typed, p); return nil })
	if err != win.ErrEscStopped || len(typed) != 1 {
		t.Errorf("stopped: typed %q, err %v; want one piece then ErrEscStopped", typed, err)
	}
}

func TestHeadedTypingIsPaced(t *testing.T) {
	spy := &keySpy{}
	svc := spy.services()
	var pieces []string
	svc.TypeText = func(text string, stopped func() bool) error {
		pieces = append(pieces, text)
		return nil
	}
	var slept []time.Duration
	svc.Sleep = func(d time.Duration) { slept = append(slept, d) }
	out, cmdErr := runCommand(t, CommandsWith(svc), "type-text",
		protocol.TypeTextArgs{Window: "101", Text: "hi!", Headed: true, IntervalMS: 25})
	if cmdErr != nil || out != okOutput {
		t.Fatalf("type-text --headed --interval-ms: %q, %v", out, cmdErr)
	}
	if strings.Join(pieces, "|") != "h|i|!" {
		t.Errorf("typed %q, want one character at a time", pieces)
	}
	paused := 0
	for _, d := range slept {
		if d == 25*time.Millisecond {
			paused++
		}
	}
	if paused != 2 {
		t.Errorf("paused %d times for 25ms, want 2 (all sleeps: %v)", paused, slept)
	}
}

func TestPostedTypingIsPaced(t *testing.T) {
	spy := newKeyRouteSpy(editTarget(plainEdit))
	var slept []time.Duration
	spy.svc.Sleep = func(d time.Duration) { slept = append(slept, d) }
	out, cmdErr := runCommand(t, CommandsWith(spy.svc), "type-text",
		protocol.TypeTextArgs{Window: "101", Text: "ab\r\nc", Post: true, IntervalMS: 10})
	if cmdErr != nil || out != postedOutput {
		t.Fatalf("type-text --post --interval-ms: %q, %v", out, cmdErr)
	}
	if strings.Join(spy.postedText, "|") != "a|b|\r\n|c" {
		t.Errorf("posted %q, want piece by piece with the CRLF pair together", spy.postedText)
	}
	n := 0
	for _, d := range slept {
		if d == 10*time.Millisecond {
			n++
		}
	}
	if n != 3 {
		t.Errorf("paused %d times for 10ms, want 3", n)
	}
}

func TestHeadlessTypingSaysTheIntervalDoesNotApply(t *testing.T) {
	spy := newKeyRouteSpy(editTarget(plainEdit))
	spy.states = []win.EditState{{Length: 0, SelKnown: true}, {Length: 5, SelKnown: true}}
	out, cmdErr := runCommand(t, CommandsWith(spy.svc), "type-text",
		protocol.TypeTextArgs{Window: "101", Text: "hello", IntervalMS: 50})
	if cmdErr != nil || out != "ok (--interval-ms does not apply: this route puts the text in at once)\n" {
		t.Errorf("Headless type-text with --interval-ms: %q, %v", out, cmdErr)
	}
	// The note joins a note the route already prints.
	if got := intervalNote("ok (posted; x)\n"); got != "ok (posted; x; --interval-ms does not apply: this route puts the text in at once)\n" {
		t.Errorf("intervalNote = %q", got)
	}
}

func TestIntervalOutOfRangeIsRefused(t *testing.T) {
	for _, ms := range []int{-1, 1001} {
		spy := &keySpy{}
		_, cmdErr := runCommand(t, CommandsWith(spy.services()), "type-text",
			protocol.TypeTextArgs{Window: "101", Text: "x", Headed: true, IntervalMS: ms})
		if cmdErr == nil || !strings.Contains(cmdErr.Msg, "--interval-ms must be 0 to 1000") {
			t.Errorf("%d: err = %v", ms, cmdErr)
		}
		if len(spy.recorded()) != 0 {
			t.Errorf("%d: steps = %v, want nothing touched", ms, spy.recorded())
		}
	}
}
