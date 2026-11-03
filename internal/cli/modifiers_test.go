package cli

// --modifiers on click and scroll (desktop-parity ticket 06).

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/hoangvu12/winuse/internal/protocol"
)

func TestModifiersFlag(t *testing.T) {
	_, _, o, err := parseClickArgs([]string{"101", "3", "--modifiers", "ctrl,shift"})
	if err != nil || !slices.Equal(o.mods, []string{"ctrl", "shift"}) {
		t.Errorf("click --modifiers = %+v, %v", o, err)
	}
	if _, _, o, err := parseClickArgs([]string{"101", "--modifiers=alt", "@1,2"}); err != nil || !slices.Equal(o.mods, []string{"alt"}) {
		t.Errorf("click --modifiers=alt = %+v, %v", o, err)
	}
	a, err := scrollRequest([]string{"101", "@5,6", "0", "-120", "--modifiers", "ctrl"}, false)
	if err != nil || !slices.Equal(a.Modifiers, []string{"ctrl"}) || a.ScrollY != -120 {
		t.Errorf("scroll --modifiers ctrl = %+v, %v", a, err)
	}
	if _, err := scrollRequest([]string{"101", "3", "down", "--modifiers", "ctrl"}, false); err == nil {
		t.Error("an Element scroll with --modifiers must be refused")
	}
	st, err := parseBatchStep([]string{"click", "id:item", "--modifiers", "ctrl"})
	if err != nil {
		t.Fatal(err)
	}
	var c protocol.ClickArgs
	if json.Unmarshal(st.Args, &c) != nil || !slices.Equal(c.Modifiers, []string{"ctrl"}) {
		t.Errorf("batch click step = %s", st.Args)
	}
	st, err = parseBatchStep([]string{"scroll", "@5,6", "0", "120", "--modifiers", "ctrl"})
	if err != nil {
		t.Fatal(err)
	}
	var sc protocol.ScrollArgs
	if json.Unmarshal(st.Args, &sc) != nil || !slices.Equal(sc.Modifiers, []string{"ctrl"}) {
		t.Errorf("batch scroll step = %s", st.Args)
	}
}
