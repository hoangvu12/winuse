package page

// desktop-parity ticket 06: CDP's modifiers on every mouse event of a click.

import "testing"

func TestClickEventsWithModifiers(t *testing.T) {
	for _, ev := range ClickEventsWith(Point{X: 1, Y: 2}, ButtonLeft, 2, 2) {
		if ev["modifiers"] != 2 {
			t.Errorf("%v lacks modifiers 2 (Control)", ev)
		}
	}
	for _, ev := range ClickEvents(Point{X: 1, Y: 2}, ButtonLeft, 1) {
		if _, ok := ev["modifiers"]; ok {
			t.Errorf("a plain click carries modifiers: %v", ev)
		}
	}
}
