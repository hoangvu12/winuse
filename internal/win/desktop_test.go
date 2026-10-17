package win

// Smoke tests for the locked-desktop detection: reading the session state
// and the input desktop name sends no input and activates nothing, so
// they are safe offline. The actual locked behaviour needs a locked
// desktop and is user-verified.

import (
	"errors"
	"testing"
)

func TestDesktopLockedSmoke(t *testing.T) {
	locked, name := DesktopLocked()
	// On an unlocked interactive desktop the input desktop is "Default"
	// and the session is not locked. Locked (or secure) desktops may
	// report any name or none, so only the unlocked case is assertable.
	if !locked && name != "Default" {
		t.Errorf("unlocked desktop reports input desktop %q, want %q", name, "Default")
	}
}

func TestInputDesktopNameReadable(t *testing.T) {
	// The test process runs in the interactive session, so the input
	// desktop opens.
	name, readable := InputDesktopName()
	if !readable {
		t.Fatal("the input desktop could not be opened; the tests must run in the interactive session")
	}
	if name == "" {
		t.Error("the input desktop name is empty but was readable")
	}
}

func TestHasInputDesktop(t *testing.T) {
	if !HasInputDesktop() {
		t.Fatal("HasInputDesktop must be true in the interactive session")
	}
}

// TestLockedDesktopErrorMessage pins the one locked-desktop message shape
// every surface reports (Actions, the helper's startup check, the CLI's
// early exit): the bare refusal, the named-desktop variant, and Default
// (a WTS-locked session still on the default desktop) staying bare.
func TestLockedDesktopErrorMessage(t *testing.T) {
	cases := []struct {
		desktop string
		want    string
	}{
		{"", "Windows desktop is locked; unlock before using winuse"},
		{"Default", "Windows desktop is locked; unlock before using winuse"},
		{"LockApp", "Windows desktop is locked (input desktop is LockApp); unlock before using winuse"},
		{"Winlogon", "Windows desktop is locked (input desktop is Winlogon); unlock before using winuse"},
	}
	for _, tc := range cases {
		if got := (&LockedDesktopError{Desktop: tc.desktop}).Error(); got != tc.want {
			t.Errorf("LockedDesktopError{Desktop: %q} = %q, want %q", tc.desktop, got, tc.want)
		}
	}
}

// TestInputDesktopRefusalClassifiesBySession pins finding 9's
// classification: an interactive session whose input desktop cannot be
// opened gets the locked-desktop refusal, not the session-0 message.
func TestInputDesktopRefusalClassifiesBySession(t *testing.T) {
	id, err := CurrentIdentity()
	if err != nil {
		t.Fatalf("CurrentIdentity: %v", err)
	}
	err = InputDesktopRefusal()
	if id.Session == 0 {
		if !errors.Is(err, ErrNoDesktop) {
			t.Fatalf("session 0: InputDesktopRefusal = %v, want ErrNoDesktop", err)
		}
		return
	}
	var locked *LockedDesktopError
	if !errors.As(err, &locked) {
		t.Fatalf("interactive session: InputDesktopRefusal = %v, want the locked-desktop refusal", err)
	}
	if got, want := err.Error(), "Windows desktop is locked; unlock before using winuse"; got != want {
		t.Errorf("message = %q, want %q", got, want)
	}
}
