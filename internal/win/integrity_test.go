package win

// Offline tests for the integrity policy (the pure comparison) and the
// live token reads of this process's own integrity (read-only, no
// desktop interaction). Real cross-process refusals are exercised by the
// e2e suite only.

import (
	"errors"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func TestRefusesInputFollowsNoWriteUp(t *testing.T) {
	cases := []struct {
		target, own uint32
		want        bool
		label       string
	}{
		{IntegrityHigh, IntegrityMedium, true, "high target, medium own: refused"},
		{IntegritySystem, IntegrityHigh, true, "system target, high own: refused"},
		{IntegrityMedium, IntegrityMedium, false, "equal: allowed"},
		{IntegrityMedium, IntegrityHigh, false, "medium target, high own (elevated winuse): allowed"},
		{IntegrityLow, IntegrityMedium, false, "low target: allowed"},
		{IntegrityMediumPlus, IntegrityMedium, true, "medium plus is strictly higher: refused"},
	}
	for _, tc := range cases {
		if got := RefusesInput(tc.target, tc.own); got != tc.want {
			t.Errorf("%s: RefusesInput(%#x, %#x) = %v", tc.label, tc.target, tc.own, got)
		}
	}
}

func TestIntegrityLevelOfOwnProcess(t *testing.T) {
	rid, err := IntegrityLevel(0)
	if err != nil {
		t.Fatalf("reading own integrity: %v", err)
	}
	if rid < IntegrityLow {
		t.Errorf("own integrity RID = %#x, want at least low (0x1000)", rid)
	}
}

func TestCheckWindowIntegrityAllowsOwnProcess(t *testing.T) {
	// A Window of this very process: same integrity, never refused.
	w := WindowInfo{ID: 123, PID: windows.GetCurrentProcessId(), ExePath: "test.exe"}
	if err := CheckWindowIntegrity(w); err != nil {
		t.Fatalf("a same-integrity Window must not be refused: %v", err)
	}
}

func TestCheckWindowIntegrityUnknownProcess(t *testing.T) {
	w := WindowInfo{ID: 9, PID: 0}
	err := CheckWindowIntegrity(w)
	if err == nil || !strings.Contains(err.Error(), "cannot read the process of window 9") {
		t.Fatalf("a Window without a PID must fail, got %v", err)
	}
}

func TestCheckWindowIntegrityRefusesUnreadableTarget(t *testing.T) {
	// A PID that does not exist: the OpenProcess failure must map to the
	// "likely elevated" refusal, not a raw error.
	w := WindowInfo{ID: 5, PID: 0x4DC5C5C5, ExePath: `C:\Apps\Protected.exe`}
	err := CheckWindowIntegrity(w)
	var admin *AdminWindowError
	if !errors.As(err, &admin) {
		t.Fatalf("an unreadable target must be an AdminWindowError, got %v", err)
	}
	if !admin.Unreadable {
		t.Error("the refusal must be marked unreadable")
	}
	if want := "window 5 (Protected.exe) is likely running as administrator; winuse cannot send it input"; err.Error() != want {
		t.Errorf("error = %q, want %q", err.Error(), want)
	}
}

func TestAdminWindowErrorMessage(t *testing.T) {
	e := &AdminWindowError{WindowID: 77, App: "Notepad.exe"}
	if want := "window 77 (Notepad.exe) is running as administrator; winuse cannot send it input"; e.Error() != want {
		t.Errorf("error = %q, want %q", e.Error(), want)
	}
}
