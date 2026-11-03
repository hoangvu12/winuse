package helper

// The displays (desktop-parity ticket 08): list-displays and screenshot
// --display N, like agent-desktop's list-displays and screenshot --screen.
// Both are Desktop commands. list-displays only reads the monitors and
// stays concurrent; screenshot is serialized like a Window's screenshot,
// because its capture hides the indicator and the Agent Cursor (ADR
// 0006). The capture is Headless: Windows.Graphics.Capture of the monitor,
// then a BitBlt of its screen region, with no activation.
//
// A display screenshot is observation only. `@x,y` points and zoom regions
// belong to a Window's latest screenshot (shot.Store is keyed by Window),
// so this one records no Mapping, and its output says so.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"strconv"
	"strings"

	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/shot"
	"github.com/hoangvu12/winuse/internal/wgc"
	"github.com/hoangvu12/winuse/internal/win"
)

const screenshotUsage = "usage: winuse screenshot --display <n>   (n from winuse list-displays; 0 is the primary display)"

// displayScreenshotNote ends every display screenshot's output.
const displayScreenshotNote = "Note: a display screenshot has no @x,y points or zoom; for those, take the Window's screenshot with winuse get-window-state <window> --screenshot.\n"

// listDisplays prints one line per display (win.FormatDisplays).
func listDisplays(s Services) Command {
	return func(ctx context.Context, args json.RawMessage) (string, *Error) {
		if len(args) > 0 && string(args) != "null" && string(args) != "{}" {
			return "", Fail("list-displays takes no arguments", "usage: winuse list-displays")
		}
		ds, err := s.Displays()
		if err != nil {
			return "", Fail("listing the displays failed: "+err.Error(), "retry; the displays may be changing")
		}
		return win.FormatDisplays(ds), nil
	}
}

// displayScreenshot captures one display, scales it like a Window's
// screenshot (long edge at most 1568 px, JPEG q80) and prints
// "Screenshot: <path> (WxH px, scale S, display N at X,Y WxH)" and the
// no-binding note.
func displayScreenshot(s Services) Command {
	return func(ctx context.Context, args json.RawMessage) (string, *Error) {
		var a protocol.ScreenshotArgs
		if err := parseArgs(args, &a); err != nil {
			return "", Fail("screenshot: "+err.Error(), screenshotUsage)
		}
		if a.Display == nil {
			return "", Fail("screenshot takes --display <n>", screenshotUsage+"; a Window's screenshot is winuse get-window-state <window> --screenshot")
		}
		if locked, desktop := s.Locked(); locked {
			return "", lockedDesktopFailure(desktop)
		}
		ds, err := s.Displays()
		if err != nil {
			return "", Fail("listing the displays failed: "+err.Error(), "retry; the displays may be changing")
		}
		n := *a.Display
		if n < 0 || n >= len(ds) {
			return "", Fail(fmt.Sprintf("there is no display %d; the desktop has %d (0 to %d)", n, len(ds), len(ds)-1),
				"run winuse list-displays for the display numbers")
		}
		d := ds[n]
		frame, err := s.CaptureDisplay(d)
		if err != nil {
			return "", Fail("capturing display "+strconv.Itoa(n)+" failed: "+err.Error(), "retry; if it keeps failing, run winuse list-displays, the displays may have changed")
		}
		scale := shot.FitScale(frame.Rect.Dx(), frame.Rect.Dy())
		img := shot.ScaleImage(frame.Image, scale)
		m := shot.MappingFor(frame, scale)
		path, err := saveShot(img, uint64(n), "display", "jpg", func(w io.Writer) error {
			return jpeg.Encode(w, img, &jpeg.Options{Quality: jpegQuality})
		})
		if err != nil {
			return "", Fail("saving the screenshot failed: "+err.Error(), "")
		}
		r := frame.Rect
		return fmt.Sprintf("Screenshot: %s (%dx%d px, scale %s, display %d at %d,%d %dx%d)\n",
			path, m.Width, m.Height, formatScale(m.Scale), n, r.Min.X, r.Min.Y, r.Dx(), r.Dy()) +
			displayScreenshotNote, nil
	}
}

// monitorCapture is the Windows.Graphics.Capture half of a display
// capture (*wgc.Method).
type monitorCapture interface {
	CaptureMonitor(hmon uintptr, bounds image.Rectangle) (shot.Frame, error)
}

var _ monitorCapture = (*wgc.Method)(nil)

// displayCapture builds the display capture: Windows.Graphics.Capture of
// the monitor, then, when it fails or comes back blank, a BitBlt of the
// display's screen region (win.CaptureScreenRegion), both with the
// indicator and the Agent Cursor hidden.
func displayCapture(o Overlay, c Cursor, first monitorCapture) func(win.Display) (shot.Frame, error) {
	var oh, ch hider
	if o != nil {
		oh = o
	}
	if c != nil {
		ch = c
	}
	return displayCaptureWith(oh, ch, first, win.CaptureScreenRegion)
}

// hider is what a capture needs of the indicator and the Agent Cursor.
type hider interface{ HideWhile(f func()) }

func displayCaptureWith(o, c hider, first monitorCapture, region func(win.Rect) (*image.RGBA, error)) func(win.Display) (shot.Frame, error) {
	return func(d win.Display) (shot.Frame, error) {
		bounds := image.Rect(int(d.Bounds.Left), int(d.Bounds.Top), int(d.Bounds.Right), int(d.Bounds.Bottom))
		var frame shot.Frame
		var err error
		hideWhile(o, c, func() {
			var failures []string
			if first != nil {
				f, werr := first.CaptureMonitor(d.Handle, bounds)
				switch {
				case werr != nil:
					failures = append(failures, "Windows.Graphics.Capture: "+werr.Error())
				case f.Image == nil || shot.IsBlank(f.Image):
					failures = append(failures, "Windows.Graphics.Capture: blank frame")
				default:
					frame = f
					return
				}
			}
			img, rerr := region(d.Bounds)
			if rerr != nil {
				err = errors.New(strings.Join(append(failures, "screen region: "+rerr.Error()), "; "))
				return
			}
			frame = shot.Frame{Image: img, Rect: bounds, Anchor: bounds.Min}
		})
		return frame, err
	}
}

// hideWhile runs f with the indicator and the Agent Cursor hidden,
// whichever of the two is wired, as hiddenDuringCapture does for a
// Window's capture methods.
func hideWhile(o, c hider, f func()) {
	run := f
	if c != nil {
		inner := run
		run = func() { c.HideWhile(inner) }
	}
	if o != nil {
		inner := run
		run = func() { o.HideWhile(inner) }
	}
	run()
}
