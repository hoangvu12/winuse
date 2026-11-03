//go:build e2e

// Windows 11 Notepad keeps its tabs across runs (headless-max 26): a tab
// left open when the test kills Notepad comes back in the next Notepad,
// the user's own included, and a Task Suite `launch-app notepad`
// restored two of them. So a test that opens a file in Notepad closes
// its tab first, without saving, the way a user discards it: the tab is
// selected, File > Close tab runs (both Headless, through winuse), and
// Notepad's "Do you want to save changes to <path>?" gets Don't save
// when the path is the test's own file. A tab whose folder is already
// gone shows "The system cannot find the path specified." when selected;
// OK closes it. Only the named tab is closed: another tab in the window
// (a session of the user's that Notepad restored into it) stays.
// Measured on Notepad 11.2607 (the ticket's comment).

package e2e

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/win"
)

// notepadTab is one tab of Notepad's tab strip in a Window State.
type notepadTab struct {
	index    string
	name     string
	selected bool
	modified bool
}

var (
	notepadTabLine    = regexp.MustCompile(`^\s*(\d+) tab \(selectable(, selected)?\) (.+?)\. (Modified|Unmodified)\.`)
	notepadSavePrompt = regexp.MustCompile(`(?m)^\s*\d+ text Do you want to save changes to (.+?)\??\s*$`)
)

// notepadMissingPath is Notepad's dialog for a tab whose folder is gone.
const notepadMissingPath = "The system cannot find the path specified."

// notepadTabs reads the tabs of Notepad's tab strip from a Window State.
func notepadTabs(state string) []notepadTab {
	var tabs []notepadTab
	for _, line := range strings.Split(state, "\n") {
		if m := notepadTabLine.FindStringSubmatch(line); m != nil {
			tabs = append(tabs, notepadTab{index: m[1], name: m[3], selected: m[2] != "", modified: m[4] == "Modified"})
		}
	}
	return tabs
}

// elementIndexOf is the Element Index of the first line of a Window State
// that matches pattern after its index; "" when none does.
func elementIndexOf(state, pattern string) string {
	re := regexp.MustCompile(`^\s*(\d+) ` + pattern)
	for _, line := range strings.Split(state, "\n") {
		if m := re.FindStringSubmatch(strings.TrimRight(line, "\r")); m != nil {
			return m[1]
		}
	}
	return ""
}

// closeNotepadTab closes the tab named name in Notepad window id without
// saving (see the file comment). It returns when the tab or the window is
// gone, and fails the test when the tab is still open after its attempts
// or Notepad asks about another file.
func closeNotepadTab(t *testing.T, id uint64, name string) {
	t.Helper()
	window := strconv.FormatUint(id, 10)
	act := func(args ...string) {
		runWinuse(t, args, "WINUSE_HELPER_IDLE="+e2eIdle)
		time.Sleep(600 * time.Millisecond)
	}
	for attempt := 0; attempt < 12; attempt++ {
		if !win.WindowOpen(id) {
			return
		}
		state, _, exit := runGetWindowState(t, window)
		if exit != 0 {
			time.Sleep(300 * time.Millisecond)
			continue
		}
		var mine, selected *notepadTab
		for _, tab := range notepadTabs(state) {
			switch {
			case tab.name == name:
				tab := tab
				mine = &tab
				if tab.selected {
					selected = &tab
				}
			case tab.selected:
				tab := tab
				selected = &tab
			}
		}
		if mine == nil {
			return
		}
		if m := notepadSavePrompt.FindStringSubmatch(state); m != nil {
			if !strings.HasSuffix(m[1], `\`+name) {
				act("click", window, elementIndexOf(state, `button Cancel `))
				t.Errorf("Notepad asks to save %q, not this test's %s: cancelled, the tab is left open", m[1], name)
				return
			}
			act("click", window, elementIndexOf(state, `button Don't save `))
			continue
		}
		if strings.Contains(state, notepadMissingPath) {
			if selected == nil || selected.name != name {
				t.Errorf("Notepad cannot find a file of tab %+v, not this test's %s: the tab is left open", selected, name)
				return
			}
			act("click", window, elementIndexOf(state, `button OK `))
			continue
		}
		if !mine.selected {
			act("perform-secondary-action", window, mine.index, "Select")
			continue
		}
		if file := elementIndexOf(state, `menu item \(collapsed\) File `); file != "" {
			act("perform-secondary-action", window, file, "Expand")
			if state, _, exit = runGetWindowState(t, window); exit != 0 {
				continue
			}
		}
		closeTab := elementIndexOf(state, `menu item Close tab$`)
		var still *notepadTab
		for _, tab := range notepadTabs(state) {
			if tab.selected {
				tab := tab
				still = &tab
			}
		}
		if closeTab == "" || still == nil || still.name != name {
			continue // the menu did not open, or another tab is selected now: read again
		}
		act("click", window, closeTab)
	}
	t.Errorf("Notepad's tab %s is still open, so Notepad will restore it: close it by hand (File > Close tab, Don't save)", name)
}

// closeLaunchedNotepadTab closes the empty tab Notepad opens when it
// starts with no saved session: an Untitled tab, Unmodified, alone in the
// window. A Notepad that restored a session adds no tab (measured), and
// its tabs are left alone.
func closeLaunchedNotepadTab(t *testing.T, id uint64) {
	t.Helper()
	state, _, exit := runGetWindowState(t, strconv.FormatUint(id, 10))
	if exit != 0 {
		return
	}
	if tabs := notepadTabs(state); len(tabs) == 1 && tabs[0].name == "Untitled" && !tabs[0].modified {
		closeNotepadTab(t, id, "Untitled")
	}
}

// TestNotepadSessionParsing reads a Notepad Window State as measured
// (Notepad 11.2607): the tabs, the save prompt and the menu's Elements.
// It touches no desktop.
func TestNotepadSessionParsing(t *testing.T) {
	state := strings.Join([]string{
		`Window: "*winuse-e2e-select-x.txt - Notepad", App: Notepad.exe.`,
		"\t0 window *winuse-e2e-select-x.txt - Notepad Secondary Actions: Raise",
		"\t\t\t\t\t7 text Do you want to save changes to C:\\Users\\a\\AppData\\Local\\Temp\\T1\\001\\winuse-e2e-select-x.txt?",
		"\t\t\t\t\t8 button Save ID: PrimaryButton",
		"\t\t\t\t\t10 button Don't save ID: SecondaryButton",
		"\t\t\t\t\t12 button Cancel ID: CloseButton",
		"\t\t\t\t\t\t7 tab (selectable) voice-flow.mmd. Unmodified. Secondary Actions: Select",
		"\t\t\t\t\t\t13 tab (selectable, selected) winuse-e2e-select-x.txt. Modified. Secondary Actions: Select",
		"\t\t\t\t\t115 menu item (collapsed) File Secondary Actions: Expand ID: File",
		"\t\t\t\t\t\t25 menu item Close tab",
		"\t\t\t\t\t\t27 menu item Close window",
	}, "\n")
	tabs := notepadTabs(state)
	want := []notepadTab{{"7", "voice-flow.mmd", false, false}, {"13", "winuse-e2e-select-x.txt", true, true}}
	if len(tabs) != len(want) || tabs[0] != want[0] || tabs[1] != want[1] {
		t.Errorf("tabs = %+v, want %+v", tabs, want)
	}
	m := notepadSavePrompt.FindStringSubmatch(state)
	if m == nil || !strings.HasSuffix(m[1], `\winuse-e2e-select-x.txt`) {
		t.Errorf("save prompt = %q", m)
	}
	for pattern, want := range map[string]string{
		`button Don't save `:            "10",
		`button Cancel `:                "12",
		`menu item \(collapsed\) File `: "115",
		`menu item Close tab$`:          "25",
		`menu item Close window$`:       "27",
		`menu item Exit$`:               "",
	} {
		if got := elementIndexOf(state, pattern); got != want {
			t.Errorf("elementIndexOf(%q) = %q, want %q", pattern, got, want)
		}
	}
}
