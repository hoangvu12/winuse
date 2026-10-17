package win

// Smoke tests for the locked-desktop detection: reading the session state
// and the input desktop name sends no input and activates nothing, so
// they are safe offline. The actual locked behaviour needs a locked
// desktop and is user-verified.

import "testing"

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
