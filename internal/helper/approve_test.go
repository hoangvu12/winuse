package helper

import (
	"testing"
	"time"
)

func TestPromptPacerWaitsBeforeEachPress(t *testing.T) {
	now := time.Unix(1000, 0)
	p := newPromptPacer(func() time.Time { return now })
	step := func(d time.Duration) { now = now.Add(d) }

	if p.ready(7) {
		t.Fatal("pressed a prompt the moment it was seen")
	}
	step(promptSettle / 2)
	if p.ready(7) {
		t.Fatal("pressed a prompt shown for half the settle time")
	}
	step(promptSettle / 2)
	if !p.ready(7) {
		t.Fatal("did not press a prompt shown for the settle time")
	}
	step(promptSettle / 4)
	if p.ready(7) {
		t.Fatal("pressed again right after a press (Chromium drops it)")
	}
	step(promptSettle)
	if !p.ready(7) {
		t.Fatal("did not press again a settle time after the last press")
	}
	if p.ready(8) {
		t.Fatal("a second prompt shares the first one's wait")
	}
}
