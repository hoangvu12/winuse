package cli

// The screenshot commands' CLI side (ticket 08): get-window-state's
// --screenshot flag and the zoom command. Argument parsing is local (usage
// errors exit 2 and never dial the helper); value validation stays in the
// helper so the CLI and any other client agree on the messages. The
// screenshot files land in %TEMP%\winuse and their paths are printed by the
// helper's response — the CLI prints that response verbatim.

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/hoangvu12/winuse/internal/protocol"
)

// zoomUsage is the zoom command's usage line.
const zoomUsage = "usage: winuse zoom <window> <x>,<y>,<w>,<h>   (the region, in the latest screenshot's pixels)"

// parseWindowStateArgs parses get-window-state's arguments: one Window
// reference plus the optional --screenshot/-s flag, allowed anywhere
// (before or after the reference, =joined or as a bare boolean).
func parseWindowStateArgs(args []string) (window string, screenshot bool, err error) {
	var pos []string
	afterDashDash := false
	for _, a := range args {
		if afterDashDash || a == "-" || !strings.HasPrefix(a, "-") {
			if a == "--" && !afterDashDash {
				afterDashDash = true
				continue
			}
			pos = append(pos, a)
			continue
		}
		name := strings.TrimLeft(a, "-")
		value, hasValue := "", false
		if eq := strings.IndexByte(name, '='); eq >= 0 {
			value, hasValue, name = name[eq+1:], true, name[:eq]
		}
		switch name {
		case "screenshot", "s":
			if hasValue && value != "true" && value != "false" {
				return "", false, fmt.Errorf("--screenshot is a flag (no value), got %q", a)
			}
			screenshot = !hasValue || value == "true"
		default:
			return "", false, fmt.Errorf("unknown flag %s", a)
		}
	}
	if len(pos) != 1 {
		return "", false, errors.New("get-window-state takes one window reference")
	}
	return pos[0], screenshot, nil
}

// parsePointRef parses a `@x,y` coordinate Element Reference: two
// non-negative comma-separated integers (optional spaces around the
// parts and after the @). The leading @ is required — it is what marks
// a token as a coordinate reference everywhere (click, scroll, drag,
// hover) — so screenshot pixels are what the agent saw, and negatives
// are a mistake, not a flag.
func parsePointRef(s string) (x, y int, err error) {
	bad := fmt.Errorf("a coordinate reference must be @x,y (two non-negative integers), got %q", s)
	body := strings.TrimSpace(s)
	if !strings.HasPrefix(body, "@") {
		return 0, 0, bad
	}
	parts := strings.Split(strings.TrimPrefix(body, "@"), ",")
	if len(parts) != 2 {
		return 0, 0, bad
	}
	x, err = strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil || x < 0 {
		return 0, 0, bad
	}
	y, err = strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil || y < 0 {
		return 0, 0, bad
	}
	return x, y, nil
}

// parseRegionArg parses a zoom region: "x,y,w,h" — the top-left and size
// in the latest screenshot's pixels.
func parseRegionArg(s string) (protocol.Region, error) {
	bad := fmt.Errorf("the region must be four comma-separated numbers: <x>,<y>,<w>,<h> (screenshot pixels), got %q", s)
	parts := strings.Split(strings.TrimSpace(s), ",")
	if len(parts) != 4 {
		return protocol.Region{}, bad
	}
	var v [4]int
	for i, p := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil {
			return protocol.Region{}, bad
		}
		v[i] = n
	}
	r := protocol.Region{X: v[0], Y: v[1], W: v[2], H: v[3]}
	if r.X < 0 || r.Y < 0 {
		return protocol.Region{}, fmt.Errorf("the region's coordinates must be non-negative, got %q", s)
	}
	if r.W < 1 || r.H < 1 {
		return protocol.Region{}, fmt.Errorf("the region's width and height must be at least 1 px, got %q", s)
	}
	return r, nil
}

// zoomCmd implements `winuse zoom <window> <x>,<y>,<w>,<h>`: a
// full-resolution crop of a region of the Window's latest screenshot, as
// PNG, with the path printed.
func zoomCmd(args []string) int {
	if len(args) != 2 {
		fmt.Fprintln(os.Stderr, zoomUsage)
		return 2
	}
	region, err := parseRegionArg(args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "winuse: %v\n%s\n", err, zoomUsage)
		return 2
	}
	if err := runWithArgs("zoom", protocol.ZoomArgs{Window: args[0], Region: region}); err != nil {
		reportError(os.Stderr, err)
		return ExitCode(err)
	}
	return 0
}
