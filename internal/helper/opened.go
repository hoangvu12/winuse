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
//
// The settle is watched (speed ticket 01, ADR 0019): a WinEvent hook on
// the target's App (win.AppWatch) makes it look as soon as a top-level
// window of the App shows, and it stops early once the App is quiet (no
// thread of it ran between two samples and its UI thread waits for
// messages), with no window it created still to show. The looks are the
// same diff at the same times as the polled settle, or sooner; only the
// stop is earlier. A Chromium or Electron Window (its page and popups come
// from other processes), a shell surface (Start shows from its own host),
// a Page Action's new-Tab probe and an App the watch cannot read keep the
// polled settle.

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
	// openedQuietGap is how soon after the first look a watched settle
	// samples the App again: the shortest span it must stay quiet.
	openedQuietGap = 20 * time.Millisecond
)

// AppWatch is what a watched settle hears from the target's App
// (*win.AppWatch; tests fake it).
type AppWatch interface {
	// Wait waits up to d, returning true early when a top-level window
	// of the App showed, or the target hid or closed.
	Wait(d time.Duration) bool
	// Pending reports whether a top-level window the App created has not
	// shown yet.
	Pending() bool
	// Sample reads the App's threads; win.AppQuiet compares two.
	Sample() win.AppSample
	Close()
}

// watchesSettle reports whether the settle after an Action on target can
// be watched: not a Chromium or Electron Window, whose page and popups
// are other processes' work, nor a shell surface, whose Start and Action
// Center show from their own hosts.
func watchesSettle(target win.WindowInfo) bool {
	return target.Shell == "" && !strings.HasPrefix(target.Class, "Chrome_WidgetWin_")
}

// watchSettle starts the App watch for a reported Action; nil keeps the
// polled settle.
func (s Services) watchSettle(before openedBefore, windows bool) AppWatch {
	if !windows || s.WatchApp == nil || !watchesSettle(before.target) {
		return nil
	}
	w, err := s.WatchApp(before.target)
	if err != nil {
		return nil
	}
	return w
}

// notePrefix starts every note line an Action adds after its `ok`.
const notePrefix = "Note: "

// okWith is a success with a note line per note after its `ok`.
func okWith(notes ...string) string {
	var b strings.Builder
	b.WriteString(okOutput)
	for _, n := range notes {
		b.WriteString(notePrefix + n + "\n")
	}
	return b.String()
}

// isOK reports whether out is a success: `ok` alone on its first line.
func isOK(out string) bool {
	return strings.HasPrefix(out, okOutput)
}

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
		// A Headless ContextMenu reports its menu itself (contextmenu.go);
		// the Headed one, a right click, through the diff.
		return ok && (action == "Expand" || action == "Select" || (action == uia.ContextMenuAction && a.Headed))
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
		// The watch starts before the Action, so a window the Action opens
		// while it runs is heard too.
		watch := s.watchSettle(before, windows)
		if watch != nil {
			defer watch.Close()
		}
		probes := &openedProbes{}
		out, err := cmd(context.WithValue(ctx, openedProbesKey{}, probes), args)
		if ranOn(err) {
			err.Out = withNotes(err.Out, s.openedNotes(ctx, before, windows, probes.all(), watch))
			return out, err
		}
		if err != nil {
			return out, err
		}
		return withNotes(out, s.openedNotes(ctx, before, windows, probes.all(), watch)), nil
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
	return s.snapshotWindow(w)
}

// snapshotWindow is snapshotOpened for a resolved Window (a Batch step's
// before an `in new` step, batch.go).
func (s Services) snapshotWindow(w win.WindowInfo) (openedBefore, bool) {
	if s.Surfaces == nil {
		return openedBefore{}, false
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
// watch, when not nil, watches the settle (settleWatched); a Page
// Action's probes keep it polled.
func (s Services) openedNotes(ctx context.Context, before openedBefore, windows bool, probes []openedProbe, watch AppWatch) string {
	if !windows && len(probes) == 0 {
		return ""
	}
	// look takes one snapshot and runs the probes: the notes, and whether
	// the settle is over.
	look := func() (string, bool) {
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
		return notes, notes != "" || (!windows && len(probes) == 0)
	}
	if watch != nil && windows && len(probes) == 0 {
		return s.settleWatched(ctx, watch, look)
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
		if notes, done := look(); done {
			return notes
		}
	}
	return ""
}

// openedLooks are when a watched settle looks, from the Action's end: at
// the polled settle's first look, once more a quiet gap later (the
// earliest the App can be found quiet), then at each of its later looks.
func openedLooks() []time.Duration {
	looks := []time.Duration{openedPoll, openedPoll + openedQuietGap}
	for at := 2 * openedPoll; at <= openedSettle; at += openedPoll {
		looks = append(looks, at)
	}
	return looks
}

// settleWatched is the watched settle: it looks at openedLooks, and at
// once whenever a top-level window of the App shows, and it stops after a
// look that finds nothing when the App was quiet since the look before
// and no window it created is still to show.
func (s Services) settleWatched(ctx context.Context, watch AppWatch, look func() (string, bool)) string {
	now := s.Now
	if now == nil {
		now = time.Now
	}
	start := now()
	var prev win.AppSample
	looks := openedLooks()
	for i, at := range looks {
		for watch.Wait(at - now().Sub(start)) {
			if ctx.Err() != nil {
				return ""
			}
			if notes, done := look(); done {
				return notes
			}
		}
		if ctx.Err() != nil {
			return ""
		}
		if notes, done := look(); done || i == len(looks)-1 {
			return notes
		}
		sample := watch.Sample()
		if i > 0 && win.AppQuiet(prev, sample) && !watch.Pending() {
			return ""
		}
		prev = sample
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

// openedWithin waits up to limit for the target's App to open Windows
// (openedChanges), and returns them in z-order; nil when none opened, the
// listing failed, or the request ended or stopped meanwhile. It looks
// before its first pause: the Action may have opened the Window already.
// A Batch's `in new` step acts in what it finds (batch.go, desktop-parity
// ticket 24).
func (s Services) openedWithin(ctx context.Context, stopped func() bool, before openedBefore, limit time.Duration) []win.WindowInfo {
	for waited := time.Duration(0); ; waited += openedPoll {
		after, err := s.Surfaces()
		if err != nil {
			return nil
		}
		if opened, _ := openedChanges(before, after); len(opened) > 0 {
			return opened
		}
		if waited >= limit {
			return nil
		}
		s.pause(openedPoll)
		if ctx.Err() != nil || stopped() {
			return nil
		}
	}
}
