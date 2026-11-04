package win

import (
	"os"
	"testing"
)

// TestCursorCaptureIsOptIn (readme-demo, ADR 0006 note): the Agent
// Cursors stay out of other software's captures unless the helper started
// with WINUSE_CURSOR_CAPTURE=1; any other value, or none, keeps them out.
func TestCursorCaptureIsOptIn(t *testing.T) {
	for v, want := range map[string]bool{"1": true, "": false, "0": false, "true": false, " 1": false, "yes": false} {
		if got := cursorCaptureOn(v); got != want {
			t.Errorf("WINUSE_CURSOR_CAPTURE=%q opts in: %v, want %v", v, got, want)
		}
	}
	if _, set := os.LookupEnv(CursorCaptureEnv); !set && (cursorRecordable || !cursorExcluded()) {
		t.Error("a process started without WINUSE_CURSOR_CAPTURE must keep its cursors out of captures")
	}

	defer func(rec, ex bool) { cursorRecordable, cursorExcludeFromCapture = rec, ex }(cursorRecordable, cursorExcludeFromCapture)
	for _, tc := range []struct{ recordable, demo, excluded bool }{
		{false, true, true},   // the default
		{true, true, false},   // WINUSE_CURSOR_CAPTURE=1
		{false, false, false}, // the visual demo test's own switch
		{true, false, false},
	} {
		cursorRecordable, cursorExcludeFromCapture = tc.recordable, tc.demo
		if got := cursorExcluded(); got != tc.excluded {
			t.Errorf("recordable %v, exclude switch %v: excluded %v, want %v", tc.recordable, tc.demo, got, tc.excluded)
		}
		if CursorRecordable() != tc.recordable {
			t.Errorf("CursorRecordable() = %v, want %v", CursorRecordable(), tc.recordable)
		}
	}
}
