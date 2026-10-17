package helper

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/hoangvu12/winuse/internal/apps"
	"image"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hoangvu12/winuse/internal/keys"
	"github.com/hoangvu12/winuse/internal/shot"
	"github.com/hoangvu12/winuse/internal/uia"
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
	// SendInput implementations; tests fake them.
	TypeText  func(text string) error      // Unicode key-event typing
	PressKeys func(chord keys.Chord) error // key or chord press (downs in order, ups in reverse)
	// The screenshot services (ticket 08). Nil values fall back to the
	// real ones; tests fake them so offline tests never capture the
	// screen.
	Capture     func(hwnd uint64) (shot.Frame, error)  // the capture chain (region → PrintWindow; ticket 09 prepends WGC)
	Shots       *shot.Store                            // per-Window latest screenshot mappings (`@x,y`, zoom)
	FrameOrigin func(hwnd uint64) (image.Point, error) // the Window's extended frame bounds top-left, read fresh at Action time
}

// CommandsWith builds the command registry over the given services.
// Commands() wires the real ones.
func CommandsWith(s Services) map[string]Command {
	s = defaultServices(s)
	// The Start-apps listing is shared by list-apps and launch-app and
	// cached for the helper's lifetime (it is a ~1 s PowerShell call).
	store := &apps.StoreSource{Run: apps.GetStartApps}
	return map[string]Command{
		"list-windows":             listWindows,
		"get-window-state":         func(ctx context.Context, args json.RawMessage) (string, *Error) { return getWindowState(s, ctx, args) },
		"record-tree":              func(ctx context.Context, args json.RawMessage) (string, *Error) { return recordTree(s, ctx, args) },
		"zoom":                     zoom(s),
		"click":                    click(s),
		"activate-window":          activateWindow(s),
		"type-text":                typeText(s),
		"press-key":                pressKey(s),
		"set-value":                setValue(s),
		"perform-secondary-action": performSecondaryAction(s),
		"list-apps":                listApps(store),
		"launch-app":               launchApp(store),
	}
}

// defaultServices fills a Services' zero values with the real
// implementations: the desktop enumeration, the UIA client and budget, the
// Action pipeline's Win32 steps, and the screenshot chain — the GDI
// fallbacks (region, then PrintWindow; ticket 09 prepends
// Windows.Graphics.Capture in front of them).
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
	if s.Shots == nil {
		s.Shots = shot.NewStore()
	}
	if s.Capture == nil {
		// The GDI fallback chain (spec, Observation details): the Window's
		// screen region first, then PrintWindow. Ticket 09 prepends
		// Windows.Graphics.Capture in front of these.
		s.Capture = shot.Chain{Methods: []shot.Method{win.RegionMethod{}, win.PrintWindowMethod{}}}.CaptureWindow
	}
	if s.FrameOrigin == nil {
		s.FrameOrigin = win.FrameOrigin
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
		return windowStateScreenshot(s, ctx, w)
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
