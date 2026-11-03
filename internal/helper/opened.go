package helper

// The Windows an Action opened (desktop-parity 02, agent-desktop's
// `data.surfaces`). After click, perform-secondary-action Expand or
// Select, press-key, set-value, invoke-menu and a Batch, winuse
// compares the target App's Windows before and after a short settle,
// and adds one line per change after the Action's `ok`:
//
//	Note: a new Window opened: <Window ID> <App> "<title>"
//	Note: window <Window ID> closed
//
// A new Window belongs to the target's process, was not visible before,
// and is a dialog, a menu or a popup (win.OpenedSurface: a classic
// `#32768` menu, a WinUI 3 PopupHost, a Chromium menu popup). Tooltips
// and click-through windows are skipped. A Windows toast reuses
// ShellExperienceHost's window in another process, so it is never
// reported (headless-gaps probes round 5, section 7). An Action on a
// shell surface reports Start or the Action Center when it shows, though
// each lives in its own host process (desktop-parity 09), and never a toast
// banner (desktop-parity 18). A reported menu
// that list-windows drops still resolves by its Window ID
// (resolveWindow), so the agent can read it next.
//
// The diff only reads window metadata: no input, no activation. It is
// skipped when the Action failed (its error stands alone; a Batch that
// ran past a failed step still gets it after its steps' lines), when the
// Window does not resolve, and when the target is not in the snapshot
// before the Action (Services.Surfaces refused, as in an offline test).
// Page Actions add their own notes (pageactions.go): a JavaScript dialog
// their input opened, and a new Tab, found by a probe (watchNewTabs) that
// is polled in the same settle.

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/uia"
	"github.com/hoangvu12/winuse/internal/win"
)

const (
	// openedSettle bounds the wait for a Window the Action opened: a
	// dialog or menu shows within it, and the wait stops at the first
	// change.
	openedSettle = 300 * time.Millisecond
	// openedPoll is the pause between two snapshots in the settle.
	openedPoll = 50 * time.Millisecond
)

// notePrefix starts every note line an Action adds after its `ok`.
const notePrefix = "Note: "

// reportsOpened reports whether the command with these arguments is an
// Action whose opened and closed Windows are reported.
func reportsOpened(name string, args json.RawMessage) bool {
	switch name {
	case "click", "press-key", "set-value", "invoke-menu", "batch":
		return true
	case "perform-secondary-action":
		var a protocol.SecondaryActionArgs
		if json.Unmarshal(args, &a) != nil {
			return false
		}
		action, ok := uia.CanonicalSecondaryAction(a.Action)
		return ok && (action == "Expand" || action == "Select")
	}
	return false
}

// openedBefore is the snapshot taken before the Action: the target as it
// was then, and every window visible then.
type openedBefore struct {
	target win.WindowInfo
	known  map[uint64]bool
	// shells are the shell surfaces shown before (desktop-parity 09): a
	// closed Start or Action Center is a known but cloaked window.
	shells map[uint64]bool
}

// openedProbe is an extra check that a Page Action registers while it
// runs (watchNewTabs): the note lines for what opened since it was
// registered, polled together with the Window snapshots.
type openedProbe func(ctx context.Context) []string

// openedProbes are the probes registered during one reported Action (a
// Batch registers one per Page step).
type openedProbes struct {
	mu     sync.Mutex
	probes []openedProbe
}

type openedProbesKey struct{}

// addOpenedProbe registers a probe with the reported Action running under
// ctx; outside one it does nothing.
func addOpenedProbe(ctx context.Context, p openedProbe) {
	if ps, ok := ctx.Value(openedProbesKey{}).(*openedProbes); ok {
		ps.mu.Lock()
		ps.probes = append(ps.probes, p)
		ps.mu.Unlock()
	}
}

// reportingOpened reports whether ctx belongs to a reported Action, so a
// Page Action only lists Tabs when a probe will be polled.
func reportingOpened(ctx context.Context) bool {
	_, ok := ctx.Value(openedProbesKey{}).(*openedProbes)
	return ok
}

// all returns the registered probes.
func (ps *openedProbes) all() []openedProbe {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return append([]openedProbe(nil), ps.probes...)
}

// reportOpened wraps an Action command with the before-and-after diff.
// Other commands pass through unchanged.
func (s Services) reportOpened(name string, cmd Command) Command {
	return func(ctx context.Context, args json.RawMessage) (string, *Error) {
		if !reportsOpened(name, args) {
			return cmd(ctx, args)
		}
		before, windows := s.snapshotOpened(args)
		probes := &openedProbes{}
		out, err := cmd(context.WithValue(ctx, openedProbesKey{}, probes), args)
		if ranOn(err) {
			err.Out = withNotes(err.Out, s.openedNotes(ctx, before, windows, probes.all()))
			return out, err
		}
		if err != nil {
			return out, err
		}
		return withNotes(out, s.openedNotes(ctx, before, windows, probes.all())), nil
	}
}

// ranOn reports whether a failure carries the output of steps that ran (a
// Batch, batch.go, desktop-parity ticket 20): that output (Error.Out)
// gets the reports an ok gets. An Esc stop is left alone, and a spent
// budget reports nothing (openedNotes).
func ranOn(err *Error) bool {
	return err != nil && err.Out != "" && err.Exit != protocol.ExitEsc
}

// snapshotOpened resolves the Action's Window and takes the snapshot
// before it runs; false skips the diff.
func (s Services) snapshotOpened(args json.RawMessage) (openedBefore, bool) {
	if s.Surfaces == nil {
		return openedBefore{}, false
	}
	var a struct {
		Window string `json:"window"`
	}
	if json.Unmarshal(args, &a) != nil {
		return openedBefore{}, false
	}
	w, rerr := resolveWindow(s, a.Window)
	if rerr != nil {
		return openedBefore{}, false // the Action fails on it anyway
	}
	all, err := s.Surfaces()
	if err != nil {
		return openedBefore{}, false
	}
	before := openedBefore{known: make(map[uint64]bool, len(all)), shells: map[uint64]bool{}}
	found := false
	for _, x := range all {
		before.known[x.ID] = true
		if win.ShellSurface(x) {
			before.shells[x.ID] = true
		}
		if x.ID == w.ID {
			before.target, found = x, true
		}
	}
	return before, found && before.target.PID != 0
}

// openedNotes waits up to openedSettle for the target's App to open a
// Window, for the target to close, or for a probe to find something (a
// new Tab), and renders the note lines; "" when nothing changed. windows
// is false when the Window snapshot was skipped: only the probes run.
func (s Services) openedNotes(ctx context.Context, before openedBefore, windows bool, probes []openedProbe) string {
	if !windows && len(probes) == 0 {
		return ""
	}
	sleep := s.Sleep
	if sleep == nil {
		sleep = time.Sleep
	}
	for polls := int(openedSettle / openedPoll); polls > 0; polls-- {
		sleep(openedPoll)
		if ctx.Err() != nil {
			return ""
		}
		var notes string
		if windows {
			after, err := s.Surfaces()
			if err != nil {
				windows = false
			} else if opened, closed := openedChanges(before, after); len(opened) > 0 || closed {
				notes = formatOpened(opened, before.target, closed)
			}
		}
		seen := map[string]bool{} // a Batch's probes can find the same Tab
		for _, p := range probes {
			for _, line := range p(ctx) {
				if !seen[line] {
					seen[line] = true
					notes += notePrefix + line + "\n"
				}
			}
		}
		if notes != "" {
			return notes
		}
		if !windows && len(probes) == 0 {
			return ""
		}
	}
	return ""
}

// openedChanges compares a snapshot after the Action with the one before:
// the new Windows of the target's process, in z-order, and whether the
// target itself closed.
func openedChanges(before openedBefore, after []win.WindowInfo) (opened []win.WindowInfo, closed bool) {
	closed = true
	for _, w := range after {
		if w.ID == before.target.ID {
			closed = false
			continue
		}
		if before.target.Shell != "" && win.ShellSurface(w) && !before.shells[w.ID] {
			// Start or the Action Center, from its own host process. A toast
			// banner showing meanwhile is the toast's App's doing, not the
			// Action's (desktop-parity 18).
			if w.Shell != win.ShellNotification {
				opened = append(opened, w)
			}
			continue
		}
		if before.known[w.ID] || w.PID != before.target.PID || !win.OpenedSurface(w) {
			continue
		}
		opened = append(opened, w)
	}
	return opened, closed
}

// formatOpened renders one note line per opened Window, then the closed
// target's.
func formatOpened(opened []win.WindowInfo, target win.WindowInfo, closed bool) string {
	var b strings.Builder
	for _, w := range opened {
		fmt.Fprintf(&b, "%sa new Window opened: %d %s %q\n", notePrefix, w.ID, w.App, strings.Join(strings.Fields(w.Title), " "))
	}
	if closed {
		fmt.Fprintf(&b, "%swindow %d closed\n", notePrefix, target.ID)
	}
	return b.String()
}

// withNotes adds note lines after an Action's output.
func withNotes(out, notes string) string {
	if notes == "" {
		return out
	}
	if out != "" && !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	return out + notes
}

// popupByID finds an open menu or popup by its Window ID: a Window an
// Action reported opening that list-windows drops (a `#32768` menu, a
// PopupHost), or a shell surface `list-windows --shell` lists
// (desktop-parity 09, shell.go).
func (s Services) popupByID(ref string) (win.WindowInfo, bool) {
	id, err := strconv.ParseUint(strings.TrimSpace(ref), 10, 64)
	if err != nil || s.Surfaces == nil {
		return win.WindowInfo{}, false
	}
	all, err := s.Surfaces()
	if err != nil {
		return win.WindowInfo{}, false
	}
	for _, w := range all {
		if w.ID == id && (win.OpenedSurface(w) || win.ShellSurface(w)) {
			return w, true
		}
	}
	return win.WindowInfo{}, false
}
