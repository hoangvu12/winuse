package helper

import (
	"fmt"
	"strings"
	"testing"

	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/uia"
)

// TestTreeReadErrorSaysClosedOnlyWhenGone (reliability-council 04): a
// root read that failed on a Window still open is its own failure, not
// "the Window may have closed".
func TestTreeReadErrorSaysClosedOnlyWhenGone(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		msg      string
		hint     string // a part of the hint
		code     string
		notClose bool
	}{
		{"gone", fmt.Errorf("%w: reading the Window's tree failed (HRESULT 0x80004005)", uia.ErrWindowGone),
			"window is gone: reading the Window's tree failed (HRESULT 0x80004005)", "the Window may have closed", protocol.CodeNotFound, false},
		{"still open", &uia.TreeReadError{HR: uia.HRESULT(-0x7FFFBFFB)},
			"reading the Window's tree failed (HRESULT 0x80004005); retry", "still open", protocol.CodeInternal, true},
		{"refused", &uia.TreeReadError{HR: uia.HRESULT(-0x7FF8FFFB)},
			"reading the Window's tree was refused (HRESULT 0x80070005)", "elevated terminal", protocol.CodePermDenied, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := treeReadError(tc.err)
			if e.Msg != tc.msg {
				t.Errorf("message = %q, want %q", e.Msg, tc.msg)
			}
			if !strings.Contains(e.Hint, tc.hint) {
				t.Errorf("hint = %q, want it to say %q", e.Hint, tc.hint)
			}
			if tc.notClose && strings.Contains(e.Hint, "may have closed") {
				t.Errorf("hint = %q says the Window may have closed", e.Hint)
			}
			if code, _ := protocol.CodeFor(e.Msg, e.Hint, e.Exit); code != tc.code {
				t.Errorf("code = %s, want %s", code, tc.code)
			}
		})
	}
}
