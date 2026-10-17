package helper

// The screenshot half of observing a Window (ticket 08): get-window-state
// --screenshot (the Window is activated, captured through the method chain,
// scaled to a model-friendly size, saved as JPEG q80, its path printed and
// its Mapping recorded for `@x,y` references and zoom regions) and zoom
// (a full-resolution PNG crop of a region of the latest screenshot).
//
// Everything capture-shaped is injected through Services (Capture, Shots,
// FrameOrigin), so the offline tests fake it and never touch the screen;
// Commands wires the real chain (screen region, then PrintWindow; ticket
// 09 prepends Windows.Graphics.Capture).

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/shot"
	"github.com/hoangvu12/winuse/internal/win"
	"github.com/hoangvu12/winuse/internal/wstate"
)

// jpegQuality is the screenshot encoding quality (orchestrator decision:
// JPEG q80, as Codex; zoom crops are PNG instead — reading, not
// recognition).
const jpegQuality = 80

// zoomUsage is the zoom command's usage line.
const zoomUsage = "usage: winuse zoom <window> <x>,<y>,<w>,<h>   (the region, in the latest screenshot's pixels)"

// windowStateScreenshot is get-window-state's --screenshot path: refuse a
// locked desktop, activate the Window (the screen-region capture needs the
// foreground, spec: "capturing the Window's screen region (after it is in
// the foreground)"), capture, save the scaled image, record the Mapping,
// then read the tree — so the tree and the screenshot describe the same
// activated Window. Any failure fails the whole command (Codex's
// fail-loudly behaviour): the agent re-observes.
func windowStateScreenshot(s Services, ctx context.Context, w win.WindowInfo) (string, *Error) {
	if locked, desktop := s.Locked(); locked {
		msg := "Windows desktop is locked; unlock before using winuse"
		if desktop != "" && desktop != "Default" {
			msg = fmt.Sprintf("Windows desktop is locked (input desktop is %s); unlock before using winuse", desktop)
		}
		return "", Fail(msg, "stop and ask the user to unlock the desktop, then read a fresh Window State before acting")
	}
	if err := s.Activate(w.ID); err != nil {
		return "", activationError(err)
	}
	frame, err := s.Capture(w.ID)
	if err != nil {
		return "", captureError(err)
	}
	scale := shot.FitScale(frame.Rect.Dx(), frame.Rect.Dy())
	img := shot.ScaleImage(frame.Image, scale)
	m := shot.MappingFor(frame, scale)
	path, err := saveShot(img, w.ID, "screenshot", "jpg", func(wtr io.Writer) error {
		return jpeg.Encode(wtr, img, &jpeg.Options{Quality: jpegQuality})
	})
	if err != nil {
		return "", Fail("saving the screenshot failed: "+err.Error(), "")
	}
	tree, err := s.Trees.Capture(ctx, w.ID, w.Title, win.AppExeName(w), s.Budget)
	if err != nil {
		return "", treeReadError(err)
	}
	// Recorded only on full success: the agent's @x,y references mean the
	// screenshot it actually saw (the tree is part of the same answer).
	s.Shots.Record(w.ID, m)
	return wstate.Render(tree) +
		fmt.Sprintf("Screenshot: %s (%dx%d px, scale %s)\n", path, m.Width, m.Height, formatScale(m.Scale)), nil
}

// zoom runs `winuse zoom <window> <region>`: the Window is activated and
// captured fresh at full resolution through the same chain, and the
// region — given in the latest screenshot's coordinate space — is mapped
// through that screenshot's recorded Mapping and cropped. The crop is
// saved as PNG (full resolution, no scaling) and its path printed.
//
// The recorded Mapping is not replaced: the agent's coordinates still mean
// the screenshot it saw; the fresh capture only supplies the pixels.
func zoom(s Services) Command {
	return func(ctx context.Context, args json.RawMessage) (string, *Error) {
		var a protocol.ZoomArgs
		if err := parseArgs(args, &a); err != nil {
			return "", Fail("zoom: "+err.Error(), zoomUsage)
		}
		if a.Region.X < 0 || a.Region.Y < 0 {
			return "", Fail("zoom: the region's coordinates must be non-negative", zoomUsage)
		}
		if a.Region.W < 1 || a.Region.H < 1 {
			return "", Fail("zoom: the region's width and height must be at least 1 px", zoomUsage)
		}
		w, rerr := resolveWindow(s, a.Window)
		if rerr != nil {
			return "", rerr
		}
		// The cheap checks refuse before anything touches the screen.
		m, ok := s.Shots.Lookup(w.ID)
		if !ok {
			return "", Fail(fmt.Sprintf("no screenshot is available for window %d", w.ID),
				"run winuse get-window-state "+strconv.FormatUint(w.ID, 10)+" --screenshot first; zoom regions are in that screenshot's pixels")
		}
		want := m.WindowRect(a.Region.X, a.Region.Y, a.Region.W, a.Region.H)
		if want.Intersect(m.Coverage).Empty() {
			return "", Fail(fmt.Sprintf("the zoom region @%d,%d %dx%d is outside the latest screenshot of window %d (%dx%d px)",
				a.Region.X, a.Region.Y, a.Region.W, a.Region.H, w.ID, m.Width, m.Height),
				"use a region within the screenshot; take a fresh get-window-state --screenshot if the Window changed")
		}
		if locked, desktop := s.Locked(); locked {
			msg := "Windows desktop is locked; unlock before using winuse"
			if desktop != "" && desktop != "Default" {
				msg = fmt.Sprintf("Windows desktop is locked (input desktop is %s); unlock before using winuse", desktop)
			}
			return "", Fail(msg, "stop and ask the user to unlock the desktop, then read a fresh Window State before acting")
		}
		if err := s.Activate(w.ID); err != nil {
			return "", activationError(err)
		}
		frame, err := s.Capture(w.ID)
		if err != nil {
			return "", captureError(err)
		}
		// The fresh frame's window-space coverage; the crop is the
		// requested region clipped to what the frame actually covers.
		frameCover := frame.Rect.Sub(frame.Anchor)
		crop := want.Intersect(frameCover)
		if crop.Empty() {
			return "", Fail("the zoom region is no longer within the Window",
				"the Window may have moved, resized or closed; take a fresh screenshot and zoom again")
		}
		sub, ok := frame.Image.SubImage(crop.Sub(frameCover.Min)).(*image.RGBA)
		if !ok {
			return "", Fail("cropping the capture failed", "")
		}
		path, err := saveShot(sub, w.ID, "zoom", "png", func(wtr io.Writer) error {
			enc := png.Encoder{CompressionLevel: png.BestSpeed}
			return enc.Encode(wtr, sub)
		})
		if err != nil {
			return "", Fail("saving the zoom crop failed: "+err.Error(), "")
		}
		return fmt.Sprintf("Zoom: %s (%dx%d px)\n", path, sub.Rect.Dx(), sub.Rect.Dy()), nil
	}
}

// shotPoint resolves a `@x,y` Element Reference of w's latest screenshot
// to the screen point to inject input at: the Mapping carries the point
// in window space (anchored to the extended frame bounds of the moment
// the screenshot was taken), and the anchor is re-read fresh — so the
// input survives the Window having moved since the screenshot. verb
// names the action ("click", "scroll", ...) in the no-screenshot
// recovery hint.
func shotPoint(s Services, w win.WindowInfo, x, y int, verb string) (image.Point, *Error) {
	m, ok := s.Shots.Lookup(w.ID)
	if !ok {
		return image.Point{}, Fail(fmt.Sprintf("no screenshot is available for window %d", w.ID),
			"run winuse get-window-state "+strconv.FormatUint(w.ID, 10)+" --screenshot, then "+verb+" @x,y coordinates from that screenshot")
	}
	if !m.Contains(x, y) {
		return image.Point{}, Fail(fmt.Sprintf("point @%d,%d is outside the latest screenshot of window %d (%dx%d px)", x, y, w.ID, m.Width, m.Height),
			"use coordinates within the screenshot; take a fresh get-window-state --screenshot if the Window changed")
	}
	origin, err := s.FrameOrigin(w.ID)
	if err != nil {
		return image.Point{}, Fail(err.Error(), "the Window may have closed; run winuse list-windows")
	}
	return origin.Add(m.WindowPoint(x, y)), nil
}

// captureError maps a capture-chain failure to the user-facing line.
func captureError(err error) *Error {
	if errors.Is(err, win.ErrGone) {
		return Fail(err.Error(), "the Window may have closed; run winuse list-windows")
	}
	return Fail(err.Error(), "retry; if it keeps failing, the App may block capture or be hung — operate from the Element tree instead")
}

// saveShot encodes img and writes it into %TEMP%\winuse (created) as
// winuse-<kind>-<window>-<timestamp>.<ext>, atomically (temp sibling +
// rename), returning the absolute path. The kind, Window and timestamp
// make repeated screenshots of the same Window never collide.
func saveShot(img image.Image, windowID uint64, kind, ext string, encode func(io.Writer) error) (string, error) {
	var buf bytes.Buffer
	if err := encode(&buf); err != nil {
		return "", err
	}
	dir := filepath.Join(os.TempDir(), "winuse")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, fmt.Sprintf("winuse-%s-%d-%d.%s", kind, windowID, time.Now().UnixNano(), ext))
	if err := writeFileAtomic(path, buf.Bytes()); err != nil {
		return "", err
	}
	return path, nil
}

// formatScale renders a scale for the Screenshot line: three significant
// digits ("1", "0.5", "0.408") — enough to recognise the scaling, short
// enough for one line.
func formatScale(scale float64) string {
	return strconv.FormatFloat(scale, 'g', 3, 64)
}
