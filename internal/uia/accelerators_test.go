package uia

import "testing"

func TestPickAccelerator(t *testing.T) {
	cases := []struct {
		name  string
		cands []accelCand
		pick  int
		want  AccelResult
	}{
		{"none", nil, -1, AccelResult{}},
		{"one enabled", []accelCand{{"Save", true}}, 0, AccelResult{Name: "Save", Enabled: 1}},
		{"one enabled among disabled", []accelCand{{"Save", false}, {"Save", true}}, 1, AccelResult{Name: "Save", Enabled: 1}},
		{"all disabled", []accelCand{{"Paste", false}, {"Paste", false}}, -1, AccelResult{Name: "Paste", Disabled: true}},
		{"two enabled: no guess", []accelCand{{"Save", true}, {"Save all", true}}, -1, AccelResult{Enabled: 2}},
	}
	for _, c := range cases {
		pick, got := pickAccelerator(c.cands)
		if pick != c.pick || got != c.want {
			t.Errorf("%s: pickAccelerator = %d, %+v, want %d, %+v", c.name, pick, got, c.pick, c.want)
		}
	}
}
