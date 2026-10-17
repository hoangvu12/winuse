package helper

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/hoangvu12/winuse/internal/apps"
	"image"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hoangvu12/winuse/internal/keys"
	"github.com/hoangvu12/winuse/internal/shot"
	"github.com/hoangvu12/winuse/internal/uia"
	"github.com/hoangvu12/winuse/internal/wgc"
	"github.com/hoangvu12/winuse/internal/win"
	"github.com/hoangvu12/winuse/internal/wstate"
)

// TreeReader reads one Window's tree through UI Automation on the helper's
// locked COM thread. The real implementation is uia.Client; tests fake it
// with recorded trees (seam 2 at the command level).
type TreeReader interface {
	Capture(ctx context.Context, hwnd uint64, title, app string, budget time.Duration) (*wstate.Tree, error)
}

// ElementPointer is the element-side half of Actions: resolving an
// Element Index of a Window's latest Window State to the screen point to
// inject input at (scrolling the Element into view, clickable point or
// bounds centre), on the UIA worker. The real implementation is
// *uia.Client (Actions defaults to Trees when it is one); later Action
// tickets add their own element work as separate interfaces type-asserted
// from Services.Actions (ticket 07's ElementActor in setvalue.go: set
// value, secondary actions) rather than widening this one.
type ElementPointer interface {
	ElementPoint(ctx context.Context, hwnd uint64, index int) (uia.Point, error)
}

// EscWatch is the helper's Esc-stop service (ticket 12): Arm and Disarm
// bracket every scope the user can stop with the physical Esc key (an
// Action, a Batch, a launch wait — nestable), and Stopped reports whether
// the user pressed it inside the scope (cleared when the outermost scope
// ends, so a stop never leaks into the next command). The real watcher is
// *win.EscWatcher — the low-level keyboard hook on the UI thread;
// CommandsWith installs a no-op when there is none (offline tests, a
// helper whose hook could not start), so every command still runs.
type EscWatch interface {
	Arm()
	Disarm()
	Stopped() bool
}

// Services carries the commands' dependencies so tests can inject fakes
// (the desktop enumeration, the tree source and the Action pipeline's
// Win32 steps); zero values fall back to the real ones.
type Services struct {
	Windows func() ([]win.WindowInfo, error) // visible Windows; nil: the live desktop
	Trees   TreeReader                       // nil: a real UI Automation client (lazy COM)
	Budget  time.Duration                    // tree-read time limit; 0: uia.DefaultTreeBudget

	// The Action pipeline (ticket 05). Nil values fall back to the real
	// Win32 implementations; tests fake them so offline tests never
	// activate a window or inject input.
	Locked    func() (locked bool, desktop string)                                     // locked-desktop refusal
	Integrity func(win.WindowInfo) error                                               // administrator-Window refusal
	Activate  func(hwnd uint64) error                                                  // foreground activation + verification
	Actions   ElementPointer                                                           // element-side Action work (UIA worker)
	Click     func(target uint64, x, y int32, button win.MouseButton, count int) error // SendInput click

	// Keyboard input (ticket 06). Nil values fall back to the real
	// SendInput implementations; tests fake them. stopped is the Esc-stop
	// checkpoint (ticket 12), polled between typing batches.
	TypeText  func(text string, stopped func() bool) error // Unicode key-event typing
	PressKeys func(chord keys.Chord) error                 // key or chord press (downs in order, ups in reverse)

	// The Esc stop (ticket 12): nil falls back to a no-op (no physical-Esc
	// stopping); tests fake it with a latch they can press.
	Esc EscWatch
	// The screenshot services (tickets 08 and 09). Nil values fall back
	// to the real ones; tests fake them so offline tests never capture the
	// screen.
	Capture     func(hwnd uint64) (shot.Frame, error)  // the capture chain (Windows.Graphics.Capture → region → PrintWindow)
	Shots       *shot.Store                            // per-Window latest screenshot mappings (`@x,y`, zoom)
	FrameOrigin func(hwnd uint64) (image.Point, error) // the Window's extended frame bounds top-left, read fresh at Action time

	// The pointer input (ticket 10). Nil values fall back to the real
	// SendInput implementations; tests fake them. dx/dy are Codex's
	// scroll amounts (positive right/down, wheel units — the win layer
	// flips the vertical to wheel semantics). Drag's stopped is the abort
	// checkpoint between its interpolation steps (the Esc stop or the
	// request's deadline, finding 10).
	Scroll func(target uint64, x, y, dx, dy int32) error                        // wheel scroll at a point
	Drag   func(target uint64, fx, fy, tx, ty int32, stopped func() bool) error // left-button drag, from point to point
	Hover  func(target uint64, x, y int32) error                                // move the pointer and leave it there

	// The in-control indicator (ticket 13): raised over the target Window
	// for the duration of every Action, click-through, never focused, and
	// hidden around every capture. Nil means no indicator (offline tests;
	// a helper whose indicator could not start). The real one is
	// *win.Overlay, wired by Main; the Esc keyboard hook of ticket 12 will
	// join its UI thread.
	Overlay Overlay

	// actionGate serializes every command that activates a Window, injects
	// input or shows the overlay (finding 11): two concurrent CLI calls
	// must never interleave control of the desktop. It is a capacity-1
	// channel — a mutex whose wait is bounded by the waiting request's own
	// budget. Created by defaultServices so one Services (one helper)
	// shares one gate; observation-only commands skip it.
	actionGate chan struct{}
}

// CommandsWith builds the command registry over the given services.
// Commands() wires the real ones.
//
// Commands that activate a Window, inject input or show the overlay are
// serialized on the helper-wide action mutex (finding 11): one winuse
// command controls the desktop at a time, exactly what codex-parity's
// "one request at a time" promises. Observation-only commands —
// list-windows, get-window, list-apps, the plain tree read, record-tree,
// launch-app — stay concurrent; get-window-state's --screenshot branch
// and zoom join the serialized set inside their handlers.
func CommandsWith(s Services) map[string]Command {
	s = defaultServices(s)
	// The indicator's brackets nest (finding 12): a Batch brackets its
	// whole run and the steps' own brackets ride inside without hiding
	// the indicator between Actions. Wrapping before the commands are
	// built wires every bracket (and every capture's HideWhile) through
	// the nesting.
	if s.Overlay != nil {
		s.Overlay = newBracketOverlay(s.Overlay)
	}
	// The Start-apps listing is shared by list-apps and launch-app and
	// cached for the helper's lifetime (it is a ~1 s PowerShell call).
	store := &apps.StoreSource{Run: apps.GetStartApps}
	cmds := map[string]Command{
		"list-windows":             listWindows,
		"get-window":               getWindow(s),
		"get-window-state":         func(ctx context.Context, args json.RawMessage) (string, *Error) { return getWindowState(s, ctx, args) },
		"record-tree":              func(ctx context.Context, args json.RawMessage) (string, *Error) { return recordTree(s, ctx, args) },
		"zoom":                     zoom(s),
		"click":                    click(s),
		"activate-window":          activateWindow(s),
		"type-text":                typeText(s),
		"press-key":                pressKey(s),
		"set-value":                setValue(s),
		"perform-secondary-action": performSecondaryAction(s),
		"scroll":                   scroll(s),
		"drag":                     drag(s),
		"hover":                    hover(s),
		"list-apps":                listApps(store),
		"launch-app":               launchApp(store, s.Esc),
	}
	// The Batch dispatches its steps to the other commands, and brackets
	// itself as one armed Esc scope and one overlay bracket around them
	// (batch.go, tickets 11 and 12, finding 12). It dispatches to the raw
	// step commands: the batch as a whole is serialized below, and the
	// action gate is not reentrant.
	cmds["batch"] = batch(cmds, s)

	// The serialized registry: every control-path command runs alone.
	out := make(map[string]Command, len(cmds))
	for name, cmd := range cmds {
		if observationOnlyCommand(name) {
			out[name] = cmd
			continue
		}
		out[name] = s.serialized(cmd)
	}
	return out
}

// observationOnlyCommand reports whether a command never activates a
// Window, injects input or shows the overlay, so it may run concurrently
// with a serialized one (finding 11). get-window-state is listed here for
// its plain tree read; its --screenshot branch serializes inside the
// handler. launch-app starts a process and watches for its Window — it
// injects nothing.
func observationOnlyCommand(name string) bool {
	switch name {
	case "list-windows", "get-window", "list-apps", "get-window-state", "record-tree", "launch-app":
		return true
	}
	return false
}

// serialized wraps one command in the helper-wide action gate (finding
// 11): the command runs alone — no other activating, injecting or
// overlay-showing command interleaves with it. The wait for the gate is
// bounded by the waiting request's own budget: a request queued behind
// another Action until its budget expired fails instead of acting late
// (the server's central timeout policy reports it).
func (s Services) serialized(cmd Command) Command {
	if s.actionGate == nil {
		return cmd // a zero Services (no defaultServices): tests wrap nothing
	}
	gate := s.actionGate
	return func(ctx context.Context, args json.RawMessage) (string, *Error) {
		select {
		case gate <- struct{}{}:
		case <-ctx.Done():
			return "", queuedOutError()
		}
		defer func() { <-gate }()
		if err := ctx.Err(); err != nil {
			// The budget expired in the instant the gate opened.
			return "", queuedOutError()
		}
		return cmd(ctx, args)
	}
}

// queuedOutError is the failure a serialized command reports when its
// request's budget expired while another Action held the desktop.
func queuedOutError() *Error {
	return Fail("another winuse Action was still running when this request's budget expired",
		"retry once the other Action finishes")
}

// noopEscWatch is the Esc service when there is no watcher: no armed
// scopes, no stops. Commands still run; the user just cannot stop them
// with Esc (a helper whose hook could not start logs it).
type noopEscWatch struct{}

func (noopEscWatch) Arm()          {}
func (noopEscWatch) Disarm()       {}
func (noopEscWatch) Stopped() bool { return false }

// defaultServices fills a Services' zero values with the real
// implementations: the desktop enumeration, the UIA client and budget, the
// Action pipeline's Win32 steps, and the screenshot chain —
// Windows.Graphics.Capture first (ticket 09), then the GDI fallbacks
// (region, PrintWindow).
func defaultServices(s Services) Services {
	if s.Windows == nil {
		s.Windows = func() ([]win.WindowInfo, error) {
			all, err := win.Collect()
			if err != nil {
				return nil, err
			}
			return win.VisibleWindows(all), nil
		}
	}
	if s.Trees == nil {
		s.Trees = uia.New()
	}
	if s.Budget <= 0 {
		s.Budget = uia.DefaultTreeBudget
	}
	if s.Locked == nil {
		s.Locked = win.DesktopLocked
	}
	if s.Integrity == nil {
		s.Integrity = win.CheckWindowIntegrity
	}
	if s.Activate == nil {
		s.Activate = win.Activate
	}
	if s.Actions == nil {
		if ep, ok := s.Trees.(ElementPointer); ok {
			s.Actions = ep
		}
	}
	if s.Click == nil {
		s.Click = win.MouseClick
	}
	if s.TypeText == nil {
		s.TypeText = win.TypeText
	}
	if s.PressKeys == nil {
		s.PressKeys = win.PressChord
	}
	if s.Esc == nil {
		s.Esc = noopEscWatch{}
	}
	if s.Scroll == nil {
		s.Scroll = win.MouseScroll
	}
	if s.Drag == nil {
		s.Drag = win.MouseDrag
	}
	if s.Hover == nil {
		s.Hover = win.MouseHover
	}
	if s.Shots == nil {
		s.Shots = shot.NewStore()
	}
	if s.Capture == nil {
		// The capture chain (spec, Observation details): Windows.Graphics
		// Capture first (ticket 09, Codex's method — falling back on any
		// failure, black frame or timeout), then the GDI fallbacks: the
		// Window's screen region, then PrintWindow. The wgc method logs its
		// captures into the helper log. With an indicator wired, every
		// method captures with it hidden (ticket 13), so winuse's own
		// screenshots never contain it.
		wgcCapture := wgc.New()
		wgcCapture.Log = log.Printf
		s.Capture = shot.Chain{Methods: captureMethods(s.Overlay, wgcCapture)}.CaptureWindow
	}
	if s.FrameOrigin == nil {
		s.FrameOrigin = win.FrameOrigin
	}
	if s.actionGate == nil {
		s.actionGate = make(chan struct{}, 1)
	}
	return s
}

// windowRef is the shared argument shape of commands that name a Window.
type windowRef struct {
	Window     string `json:"window"`     // a Window ID, or an App name (exactly one Window)
	Screenshot bool   `json:"screenshot"` // get-window-state --screenshot: capture and save a screenshot too
}

// getWindowState answers `winuse get-window-state <window>`: the resolved
// Window's State as Codex's depth-first, indented, numbered tree. A read
// that hits the tree budget returns the partial tree with a note. With
// --screenshot (screenshot.go), the Window is activated, captured through
// the method chain, the image saved scaled (long edge at most 1568 px,
// JPEG q80) with its path printed and its mapping recorded for `@x,y`
// references and zoom regions.
func getWindowState(s Services, ctx context.Context, args json.RawMessage) (string, *Error) {
	var a windowRef
	if err := parseArgs(args, &a); err != nil {
		return "", Fail("get-window-state: "+err.Error(), "run winuse get-window-state <window> [--screenshot]")
	}
	w, rerr := resolveWindow(s, a.Window)
	if rerr != nil {
		return "", rerr
	}
	if a.Screenshot {
		// The screenshot path activates the Window and captures: serialize
		// it like an Action (finding 11), while the plain tree read stays
		// concurrent.
		return s.serialized(func(ctx context.Context, args json.RawMessage) (string, *Error) {
			return windowStateScreenshot(s, ctx, w)
		})(ctx, args)
	}
	tree, err := s.Trees.Capture(ctx, w.ID, w.Title, win.AppExeName(w), s.Budget)
	if err != nil {
		return "", treeReadError(err)
	}
	return wstate.Render(tree), nil
}

// recordArgs is record-tree's argument shape.
type recordArgs struct {
	Window string `json:"window"`
	File   string `json:"file"` // absolute output path (the CLI resolves it)
}

// recordTree answers `winuse record-tree <window> <file>`: it captures a
// real Window's tree and saves it as a fixture file in the recorder's
// format (wstate.Tree JSON) for the seam-2 offline rendering tests.
func recordTree(s Services, ctx context.Context, args json.RawMessage) (string, *Error) {
	var a recordArgs
	if err := parseArgs(args, &a); err != nil {
		return "", Fail("record-tree: "+err.Error(), "run winuse record-tree <window> <file>")
	}
	if strings.TrimSpace(a.File) == "" {
		return "", Fail("record-tree: an output file is required", "run winuse record-tree <window> <file>")
	}
	w, rerr := resolveWindow(s, a.Window)
	if rerr != nil {
		return "", rerr
	}
	tree, err := s.Trees.Capture(ctx, w.ID, w.Title, win.AppExeName(w), s.Budget)
	if err != nil {
		return "", treeReadError(err)
	}
	now := time.Now()
	tree.Format = wstate.FixtureFormat
	tree.Captured = &now
	tree.Source = fmt.Sprintf("recorded by winuse record-tree from %q (Window %d)", w.Title, w.ID)
	data, err := json.MarshalIndent(tree, "", "  ")
	if err != nil {
		return "", Fail("encoding the fixture: "+err.Error(), "")
	}
	if err := writeFileAtomic(a.File, append(data, '\n')); err != nil {
		return "", Fail("writing "+a.File+": "+err.Error(), "")
	}
	note := ""
	if len(tree.Notes) > 0 {
		note = " (" + strings.Join(tree.Notes, "; ") + ")"
	}
	return fmt.Sprintf("recorded %s: %d elements%s\n", a.File, len(tree.Elements), note), nil
}

// parseArgs unmarshals a command's arguments, refusing missing or malformed
// JSON with a usage-flavoured message.
func parseArgs(args json.RawMessage, v any) error {
	if len(args) == 0 || string(args) == "null" {
		return errors.New("arguments are required")
	}
	if err := json.Unmarshal(args, v); err != nil {
		return errors.New("invalid arguments: " + err.Error())
	}
	return nil
}

// resolveWindow names a Window by Window ID or App name over the live
// desktop (see win.ResolveWindow for the matching rules).
func resolveWindow(s Services, ref string) (win.WindowInfo, *Error) {
	ws, err := s.Windows()
	if err != nil {
		return win.WindowInfo{}, Fail("enumerating windows: "+err.Error(), "")
	}
	w, rerr := win.ResolveWindow(ws, ref)
	if rerr != nil {
		var re *win.ResolveError
		if errors.As(rerr, &re) {
			return win.WindowInfo{}, &Error{Msg: re.Msg, Hint: re.Hint, Exit: 1}
		}
		return win.WindowInfo{}, Fail(rerr.Error(), "")
	}
	return w, nil
}

// treeReadError maps a capture failure to a user-facing error. Context
// deadlines stay plain: the server's central timeout policy answers those.
func treeReadError(err error) *Error {
	if errors.Is(err, context.DeadlineExceeded) {
		return Fail("the tree read did not complete in time", "retry; if it keeps failing, the app's accessibility tree is slow")
	}
	hint := ""
	switch {
	case errors.Is(err, uia.ErrWindowGone):
		hint = "the Window may have closed; run winuse list-windows"
	case errors.Is(err, uia.ErrInit):
		hint = "UI Automation could not start in the helper; report this if it persists"
	}
	return Fail(err.Error(), hint)
}

// writeFileAtomic writes a file via a temp sibling + rename, so a reader
// never sees a partial fixture.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}
