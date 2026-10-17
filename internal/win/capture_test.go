package win

import "testing"

// The screenshot-capture layer's offline tests (ticket 08): the popup-union
// geometry is a pure function of the window list, so it is fully testable
// offline. The GDI calls themselves (BitBlt, PrintWindow, GetDIBits) are
// never run here — they touch the live desktop and belong to the user-run
// e2e suite.

// popupsFixture builds a window list around a target window at (100, 200):
// an owned menu dropping below its bottom edge, a tooltip over it, an owned
// dialog elsewhere, an unowned menu of the same process overlapping, a
// foreign window, a cloaked popup, and winuse's own overlay window.
func popupsFixture() ([]WindowInfo, WindowInfo) {
	target := WindowInfo{
		ID: 1, PID: 10, Class: "MainWnd", Title: "App",
		Bounds: Rect{Left: 100, Top: 200, Right: 900, Bottom: 670},
	}
	menu := WindowInfo{
		ID: 2, PID: 10, Owner: 1, Class: "#32768", Title: "",
		Bounds: Rect{Left: 110, Top: 670, Right: 320, Bottom: 790}, // starts exactly at the bottom edge
	}
	tooltip := WindowInfo{
		ID: 3, PID: 10, Owner: 1, Class: "tooltips_class32", Title: "",
		Bounds: Rect{Left: 400, Top: 180, Right: 550, Bottom: 230}, // over the top edge
	}
	dialog := WindowInfo{
		ID: 4, PID: 10, Owner: 1, Class: "#32770", Title: "Save As",
		Bounds: Rect{Left: 2000, Top: 2000, Right: 2300, Bottom: 2210}, // elsewhere on screen
	}
	ownedOfDialog := WindowInfo{
		ID: 5, PID: 10, Owner: 4, Class: "#32768", Title: "",
		Bounds: Rect{Left: 2040, Top: 2210, Right: 2100, Bottom: 2270},
	}
	unownedMenu := WindowInfo{
		ID: 6, PID: 10, Class: "#32768", Title: "",
		Bounds: Rect{Left: 890, Top: 500, Right: 1100, Bottom: 620}, // same process, no owner, overlapping
	}
	foreign := WindowInfo{
		ID: 7, PID: 99, Class: "OtherApp", Title: "Other",
		Bounds: Rect{Left: 500, Top: 400, Right: 700, Bottom: 500}, // overlaps but unrelated
	}
	cloakedPopup := WindowInfo{
		ID: 8, PID: 10, Owner: 1, Class: "#32768", Title: "", Cloaked: true,
		Bounds: Rect{Left: 100, Top: 200, Right: 900, Bottom: 900},
	}
	ownOverlay := WindowInfo{
		ID: 9, PID: 10, Owner: 1, Class: "winuse.overlay", OwnProcess: true,
		Bounds: Rect{Left: 100, Top: 200, Right: 900, Bottom: 670},
	}
	all := []WindowInfo{target, menu, tooltip, dialog, ownedOfDialog, unownedMenu, foreign, cloakedPopup, ownOverlay}
	return all, target
}

func TestPopupUnionPlainTargetIsItsBounds(t *testing.T) {
	_, target := popupsFixture()
	// A target with no popups: the capture region is its own bounds.
	onlyTarget := []WindowInfo{target}
	if got := PopupUnion(onlyTarget, target); got != target.Bounds {
		t.Errorf("PopupUnion = %v, want the target's bounds %v", got, target.Bounds)
	}
}

func TestPopupUnionIncludesOverlappingAndTouchingPopups(t *testing.T) {
	all, target := popupsFixture()
	got := PopupUnion(all, target)
	// The owned menu (touching the bottom edge) and the tooltip (over the
	// top edge) join the region; the unowned same-process menu overlapping
	// on the right joins too.
	want := Rect{Left: 100, Top: 180, Right: 1100, Bottom: 790}
	if got != want {
		t.Errorf("PopupUnion = %v, want %v (menu below, tooltip above, menu right)", got, want)
	}
}

func TestPopupUnionIgnoresNonOverlappingOwnedDialog(t *testing.T) {
	all, target := popupsFixture()
	// Keep only the target and its far-away owned dialog (plus its own
	// menu, to check chains through the dialog are not confused).
	keep := []WindowInfo{all[0], all[3], all[4]}
	got := PopupUnion(keep, all[0])
	if got != target.Bounds {
		t.Errorf("PopupUnion = %v, want the target's bounds %v (a dialog elsewhere is its own Window)", got, target.Bounds)
	}
}

func TestPopupUnionIgnoresForeignWindowsAndOwnCloaked(t *testing.T) {
	all, target := popupsFixture()
	keep := []WindowInfo{all[0], all[6]} // a foreign overlapping window
	if got := PopupUnion(keep, target); got != target.Bounds {
		t.Errorf("PopupUnion = %v, want %v (foreign windows never join)", got, target.Bounds)
	}
	keep = []WindowInfo{all[0], all[7]} // a cloaked popup of the target
	if got := PopupUnion(keep, target); got != target.Bounds {
		t.Errorf("PopupUnion = %v, want %v (cloaked popups never join)", got, target.Bounds)
	}
	keep = []WindowInfo{all[0], all[8]} // winuse's own overlay
	if got := PopupUnion(keep, target); got != target.Bounds {
		t.Errorf("PopupUnion = %v, want %v (winuse's own windows never join)", got, target.Bounds)
	}
}

func TestPopupUnionFollowsOwnerChains(t *testing.T) {
	// A menu owned by an owned dialog that overlaps the target: the
	// chain reaches the target, so it joins the union when it overlaps
	// the target — even though its direct owner is the dialog, not the
	// target.
	all, _ := popupsFixture()
	target := all[0]
	dialog := all[3]
	dialog.Bounds = Rect{Left: 300, Top: 300, Right: 500, Bottom: 500} // overlapping the target
	menuOfDialog := all[4]
	menuOfDialog.Bounds = Rect{Left: 880, Top: 650, Right: 1050, Bottom: 800} // past the target's edge
	keep := []WindowInfo{target, dialog, menuOfDialog}
	got := PopupUnion(keep, target)
	want := Rect{Left: 100, Top: 200, Right: 1050, Bottom: 800}
	if got != want {
		t.Errorf("PopupUnion = %v, want %v (chain-owned menu past the edge joins)", got, want)
	}
}

func TestPopupUnionMenuWithoutOwnerMatchesByProcess(t *testing.T) {
	target := WindowInfo{ID: 1, PID: 10, Class: "MainWnd", Bounds: Rect{Left: 0, Top: 0, Right: 100, Bottom: 100}}
	menu := WindowInfo{ID: 2, PID: 10, Class: "#32768", Bounds: Rect{Left: 50, Top: 100, Right: 150, Bottom: 140}}
	got := PopupUnion([]WindowInfo{target, menu}, target)
	want := Rect{Left: 0, Top: 0, Right: 150, Bottom: 140}
	if got != want {
		t.Errorf("PopupUnion = %v, want %v (an unowned #32768 of the same process joins)", got, want)
	}
	// A different process's #32768 never joins.
	menu.PID = 99
	if got := PopupUnion([]WindowInfo{target, menu}, target); got != target.Bounds {
		t.Errorf("PopupUnion = %v, want %v (foreign menus never join)", got, target.Bounds)
	}
	// A non-menu window of the same process without an owner never joins.
	menu.PID = 10
	menu.Class = "SomePane"
	if got := PopupUnion([]WindowInfo{target, menu}, target); got != target.Bounds {
		t.Errorf("PopupUnion = %v, want %v (unowned non-menu windows never join)", got, target.Bounds)
	}
}

// TestPopupPresentReportsOverlappingPopups pins the gate the
// Windows.Graphics.Capture method (ticket 09) keys off: WGC captures only
// the Window's own tree, so popups present mean "defer to the
// screen-region capture". Same population as PopupUnion.
func TestPopupPresentReportsOverlappingPopups(t *testing.T) {
	all, target := popupsFixture()

	keep := []WindowInfo{all[0]}
	if PopupPresent(keep, target) {
		t.Error("a target with no popups must not report popups present")
	}

	// The owned menu touching the bottom edge, the tooltip over the top
	// edge and the unowned same-process menu are all popups present.
	for _, idx := range []int{1, 2, 5} {
		if !PopupPresent([]WindowInfo{all[0], all[idx]}, target) {
			t.Errorf("popup %d (overlapping or touching) must be present", all[idx].ID)
		}
	}

	// A fully-inside popup counts as present too (WGC without
	// IncludeSecondaryWindows would miss it).
	inside := WindowInfo{
		ID: 10, PID: 10, Owner: 1, Class: "#32768", Title: "",
		Bounds: Rect{Left: 200, Top: 300, Right: 400, Bottom: 400},
	}
	if !PopupPresent([]WindowInfo{all[0], inside}, target) {
		t.Error("a popup fully inside the target must be present")
	}

	// Non-popups never count: the elsewhere dialog, foreign windows,
	// cloaked popups, winuse's own windows.
	for _, idx := range []int{3, 6, 7, 8} {
		if PopupPresent([]WindowInfo{all[0], all[idx]}, target) {
			t.Errorf("window %d must not count as a popup of the target", all[idx].ID)
		}
	}
}
