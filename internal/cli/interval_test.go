package cli

// type-text --interval-ms's CLI side (desktop-parity ticket 23): the flag
// among type-text's leading flags, in either order with --post or --into,
// in a Batch step too, and its usage errors. Nothing dials the helper.

import (
	"strings"
	"testing"
)

func TestTypeTextIntervalFlag(t *testing.T) {
	cases := []struct {
		args     []string
		interval int
		post     bool
		into     bool
		text     string
	}{
		{[]string{"101", "--interval-ms", "50", "hello world"}, 50, false, false, "hello world"},
		{[]string{"101", "--post", "--interval-ms", "20", "hi"}, 20, true, false, "hi"},
		{[]string{"101", "--interval-ms", "0", "--into", "id:box", "hi"}, 0, false, true, "hi"},
		{[]string{"101", "--into", "3", "--interval-ms", "1000", "x"}, 1000, false, true, "x"},
		// After the flags, a flag's spelling is text.
		{[]string{"101", "--into", "3", "--post"}, 0, false, true, "--post"},
		{[]string{"101", "--interval-ms", "5", "--interval-ms"}, 5, false, false, "--interval-ms"},
	}
	for _, c := range cases {
		a, err := typeTextRequest(c.args, false)
		if err != nil {
			t.Errorf("%q: %v", c.args, err)
			continue
		}
		if a.IntervalMS != c.interval || a.Post != c.post || (a.IntoIndex != nil || a.IntoRef != "") != c.into || a.Text != c.text {
			t.Errorf("%q = %+v, want interval %d post %v into %v text %q", c.args, a, c.interval, c.post, c.into, c.text)
		}
	}
	for _, bad := range [][]string{
		{"101", "--interval-ms"},
		{"101", "--interval-ms", "fast", "x"},
		{"101", "--interval-ms", "-1", "x"},
		{"101", "--interval-ms", "1001", "x"},
	} {
		if _, err := typeTextRequest(bad, false); err == nil || !strings.Contains(err.Error(), "--interval-ms") {
			t.Errorf("%q: err = %v, want an --interval-ms usage error", bad, err)
		}
	}
}

func TestBatchTypeTextIntervalStep(t *testing.T) {
	_, steps, err := parseBatchArgs([]string{"101", "type-text", "--interval-ms", "30", "slow text"})
	if err != nil {
		t.Fatal(err)
	}
	if got := string(steps[0].Args); !strings.Contains(got, `"interval_ms":30`) || !strings.Contains(got, `"text":"slow text"`) {
		t.Errorf("step args = %s, want the interval and the text", got)
	}
}
