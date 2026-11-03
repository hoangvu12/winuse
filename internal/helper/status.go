package helper

// winuse status (desktop parity ticket 16): the helper's half of the
// report. Every probe only reads: the desktop's lock state, the helper's
// own integrity level and DPI awareness, the Windows build, and the
// Browser Connections the helper already holds (it never discovers or
// dials a browser for this). It touches no Window, so it is
// observation-only and never waits behind an Action.

import (
	"context"
	"encoding/json"
	"os"

	"github.com/hoangvu12/winuse/internal/status"
	"github.com/hoangvu12/winuse/internal/version"
	"github.com/hoangvu12/winuse/internal/win"
)

// StatusCommand is the status command's wire name; the server answers it
// for a CLI of any build, without the stale exit.
const StatusCommand = "status"

// heldBrowsers is the Browser service's read-only listing of its open
// connections (*browser.Manager's Held). A service without it reports the
// connections as unknown rather than probing for them.
type heldBrowsers interface {
	Held() []string
}

// statusCommand answers the status command with a status.Report as JSON;
// the CLI adds its own version and prints it.
func statusCommand(s Services) Command {
	return func(ctx context.Context, args json.RawMessage) (string, *Error) {
		if len(args) > 0 && string(args) != "null" && string(args) != "{}" {
			return "", Fail("status takes no arguments", "")
		}
		r := status.Report{
			Helper: status.Helper{
				State:   status.HelperRunning,
				Version: version.Version,
				Build:   version.Build(),
				PID:     os.Getpid(),
				Uptime:  s.Now().Sub(s.Started),
			},
			Source:  status.SourceHelper,
			DPI:     s.DPIAwareness(),
			Windows: s.WindowsBuild(),
		}
		r.Locked, r.Desktop = s.Locked()
		if rid, err := s.OwnIntegrity(); err == nil {
			r.Integrity = win.IntegrityName(rid)
		}
		if h, ok := s.Browser.(heldBrowsers); ok {
			r.Browsers, r.BrowsersKnown = h.Held(), true
		}
		r.Agents = s.Agents.list(s.Now())
		raw, err := json.Marshal(r)
		if err != nil {
			return "", Fail("status: "+err.Error(), "")
		}
		return string(raw), nil
	}
}
