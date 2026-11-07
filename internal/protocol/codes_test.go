package protocol

import "testing"

// Real failure lines (message, hint, exit) and the code each one gets.
func TestCodeForRealFailures(t *testing.T) {
	cases := []struct {
		msg, hint string
		exit      int
		want      string
	}{
		{"winuse was stopped by the user with the physical Escape key", "stop your current task", ExitEsc, CodeStopped},
		{"winuse: --timeout takes seconds", "usage: winuse click <window> <element>", 2, CodeUsage},
		{"Windows desktop is locked; unlock before using winuse", "stop and ask the user", 1, CodeDesktopLocked},
		{"window 5 (Task Manager) is running as administrator; winuse cannot send it input", "", 1, CodePermDenied},
		{"setting the value failed", "UI Automation writes into a higher-integrity Window fail; start winuse from an elevated terminal to control administrator Windows, or ask the user to operate this Window", 1, CodePermDenied},
		{"window 5 has a modal dialog open (window 9); act on the dialog instead; nothing was sent", "run winuse get-window-state 9", 1, CodeBlockedByModal},
		{"window 5 is minimized; nothing was sent", "restore it with winuse restore-window 5", 1, CodeMinimized},
		{"winuse request timed out: click", "", 1, CodeTimeout},
		{"agent b is using the desktop; nothing was sent", "retry once it is done", 1, CodeTimeout},
		{`name "Save" matches 3 elements in the cached Window State for window 5`, "candidates:\n[3] button Save", 1, CodeAmbiguousTarget},
		{`app "notepad" has 2 open Windows; name the Window by its Window ID`, "candidates:\n101 Notepad", 1, CodeAmbiguousTarget},
		{"click at @10,20 has no background route: the point is on two overlapping Elements, 3 and 4, that act differently; nothing was sent", "", 1, CodeAmbiguousTarget},
		{"element 3 is not available in the cached Window State for window 5", "run winuse get-window-state 5 again", 1, CodeStaleRef},
		{"stale element index", "", 1, CodeStaleRef},
		{"the selection was delivered but the edit control selects characters 0 to 3 afterwards, not 0 to 5", "the Action was delivered; do not repeat it", 1, CodeVerifyFailed},
		{"window 5 is not maximized after the maximize", "", 1, CodeVerifyFailed},
		{"the Element offers no Invoke, Toggle, Select, Expand/Collapse or default action, so it cannot be clicked headless; nothing was sent", "retry with --headed", 1, CodeNoHeadlessRoute},
		{"type-text --into has no Headless route into element 4: it is read-only", "", 1, CodeNoHeadlessRoute},
		{"no open Window with ID 42", "the Window may have closed; run winuse list-windows", 1, CodeNotFound},
		{"there is no display 3; the desktop has 2 (0 to 1)", "", 1, CodeNotFound},
		{`unknown command "clickx"`, "", 1, CodeUsage},
		{"scroll: pass an element reference with a direction, or an @x,y point with two amounts, not both", "", 1, CodeUsage},
		{"saving the screenshot failed: disk full", "", 1, CodeInternal},
	}
	for _, tc := range cases {
		got, ok := CodeFor(tc.msg, tc.hint, tc.exit)
		if got != tc.want || !ok {
			t.Errorf("CodeFor(%q, %q, %d) = %q, %v; want %q", tc.msg, tc.hint, tc.exit, got, ok, tc.want)
		}
	}
	if got, ok := CodeFor("HRESULT 0x80004005", "", 1); got != CodeInternal || ok {
		t.Errorf("an unmatched message: %q, %v; want INTERNAL, false", got, ok)
	}
}

// Every delivery phrase the failures use maps to its delivery and retry.
func TestDeliveryOfEveryPhrase(t *testing.T) {
	cases := []struct {
		msg, hint       string
		delivery, retry string
	}{
		{"element 3 is disabled; nothing was sent", "", DeliveryNotDelivered, RetrySafe},
		{"--cwd \"x\" is not a folder; nothing was started", "", DeliveryNotDelivered, RetrySafe},
		{"the clipboard kept changing while winuse read it; nothing was changed", "", DeliveryNotDelivered, RetrySafe},
		{"no Element of window 5 matches name:Save; nothing was added", "", DeliveryNotDelivered, RetrySafe},
		{"the edit control rejected the value", "the Action was delivered; do not repeat it, read the Window State: winuse get-window-state 5", DeliveryDeliveredUnverified, RetryUnsafe},
		{"the navigation was delivered, and the Tab of window 5 closed", "read winuse tab list 5", DeliveryDeliveredUnverified, RetryUnsafe},
		{"the pattern call failed: E_FAIL", "the Action may have taken effect; read the Window State (winuse get-window-state 5) before repeating it", DeliveryUncertain, RetryUnsafe},
		{"winuse was stopped by the user with the physical Escape key", "the interrupted Action may have partly taken effect, so re-read a Window State", DeliveryUncertain, RetryUnsafe},
		{"winuse request timed out: click", "", DeliveryUncertain, RetryUnsafe},
		// A Batch that ran past an uncertain step: the uncertainty wins.
		{"batch: 1 of 3 Actions failed: Action 2 (click, uncertain)", "Action 2 may have taken effect: read the Window State; nothing was sent for the others", DeliveryUncertain, RetryUnsafe},
		{"no open Window with ID 42", "", "", ""},
	}
	for _, tc := range cases {
		d, r := DeliveryOf(tc.msg, tc.hint)
		if d != tc.delivery || r != tc.retry {
			t.Errorf("DeliveryOf(%q, %q) = %q, %q; want %q, %q", tc.msg, tc.hint, d, r, tc.delivery, tc.retry)
		}
	}
}

func TestStamp(t *testing.T) {
	ok := Response{OK: true, Out: "done\n"}
	ok.Stamp(CodeInternal, DeliveryUncertain)
	if ok.Code != "" || ok.Delivery != "" || ok.Retry != "" {
		t.Errorf("a success was stamped: %+v", ok)
	}
	own := Response{Err: "element 3 is disabled; nothing was sent", Exit: 1}
	own.Stamp(CodeNotFound, "")
	if own.Code != CodeNotFound || own.Delivery != DeliveryNotDelivered || own.Retry != RetrySafe {
		t.Errorf("the site's code: %+v", own)
	}
	usage := Response{Err: "winuse: --timeout takes seconds", Exit: 2}
	usage.Stamp("", "")
	if usage.Code != CodeUsage || usage.Delivery != DeliveryNotDelivered || usage.Retry != RetrySafe {
		t.Errorf("a usage error was never sent: %+v", usage)
	}
	// The site's typed delivery outranks the text, whatever its wording
	// (reliability-council 02).
	typed := Response{Err: "invoking the Element failed (HRESULT 0x80010105)", Exit: 1}
	typed.Stamp("", DeliveryUncertain)
	if typed.Delivery != DeliveryUncertain || typed.Retry != RetryUnsafe {
		t.Errorf("a typed delivery: %+v", typed)
	}
	reworded := Response{Err: "element 3 is disabled; nothing was sent", Exit: 1}
	reworded.Stamp("", DeliveryDeliveredUnverified)
	if reworded.Delivery != DeliveryDeliveredUnverified || reworded.Retry != RetryUnsafe {
		t.Errorf("the typed delivery must win over the text's: %+v", reworded)
	}
	unknown := Response{Err: "HRESULT 0x80004005", Exit: 1}
	unknown.Stamp("", "")
	if unknown.Delivery != "" || unknown.Retry != "" {
		t.Errorf("text that says nothing, untyped: %+v", unknown)
	}
}

func TestRetryOfAndSentNothing(t *testing.T) {
	for _, tc := range []struct {
		delivery, retry string
		sentNothing     bool
	}{
		{DeliveryNotDelivered, RetrySafe, true},
		{DeliveryDeliveredUnverified, RetryUnsafe, false},
		{DeliveryUncertain, RetryUnsafe, false},
		{"", "", false},
	} {
		if got := RetryOf(tc.delivery); got != tc.retry {
			t.Errorf("RetryOf(%q) = %q, want %q", tc.delivery, got, tc.retry)
		}
		if got := SentNothing(tc.delivery); got != tc.sentNothing {
			t.Errorf("SentNothing(%q) = %v, want %v", tc.delivery, got, tc.sentNothing)
		}
	}
}
