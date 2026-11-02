package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/hoangvu12/winuse/internal/status"
	"github.com/hoangvu12/winuse/internal/version"
	"github.com/hoangvu12/winuse/internal/win"
)

// localProbes are the CLI's own read-only probes, for the lines a running
// helper would otherwise answer. Tests fake them.
type localProbes struct {
	Locked       func() (bool, string)
	Integrity    func() (uint32, error)
	DPIAwareness func() string
	WindowsBuild func() string
}

func liveProbes() localProbes {
	return localProbes{
		Locked:       win.DesktopLocked,
		Integrity:    func() (uint32, error) { return win.IntegrityLevel(0) },
		DPIAwareness: win.DPIAwareness,
		WindowsBuild: win.WindowsBuild,
	}
}

// statusCmd implements `winuse status` (desktop parity ticket 16): one
// line each for the winuse and helper versions, the helper's uptime, the
// desktop's lock state, the integrity level, the DPI awareness, the
// Browser Connections and the Windows build. It never starts the helper:
// with none running it says so and reads the rest itself. Unlike the other
// commands it runs on a locked desktop too, to report it. Exit 0 whenever
// it printed the report.
func statusCmd(args []string) int {
	if len(args) > 0 {
		fmt.Fprintln(os.Stderr, "winuse: status takes no arguments")
		return 2
	}
	client, err := DefaultClient()
	if err != nil {
		reportError(os.Stderr, err)
		return 1
	}
	return runStatus(os.Stdout, client, liveProbes())
}

// runStatus asks a running helper for its report (never starting one) and
// prints it, filling the machine lines from the CLI's own probes when no
// helper answered.
func runStatus(w io.Writer, client *Client, local localProbes) int {
	r, ok := helperReport(client)
	if !ok {
		r.Source = status.SourceCLI
		r.Locked, r.Desktop = local.Locked()
		if rid, err := local.Integrity(); err == nil {
			r.Integrity = win.IntegrityName(rid)
		}
		r.DPI = local.DPIAwareness()
		r.Windows = local.WindowsBuild()
	}
	r.CLIVersion = version.Version
	io.WriteString(w, r.Format())
	return 0
}

// helperReport asks the running helper for its status report. ok is false
// when no helper answered one; the report's Helper line then says why.
func helperReport(client *Client) (r status.Report, ok bool) {
	req, err := Request(helperStatusCommand, nil)
	if err != nil {
		r.Helper = status.Helper{State: status.HelperFailed, Detail: err.Error()}
		return r, false
	}
	resp, err := client.CallExisting(context.Background(), req)
	switch {
	case errors.Is(err, ErrNoHelper):
		r.Helper.State = status.HelperNotRunning
		return r, false
	case err != nil:
		r.Helper = status.Helper{State: status.HelperFailed, Detail: err.Error()}
		return r, false
	case resp.Stale:
		// A helper older than status's stale exemption: it answered stale
		// and is exiting, as it does for any command of another build.
		r.Helper.State = status.HelperStale
		return r, false
	case !resp.OK:
		r.Helper = status.Helper{State: status.HelperFailed, Detail: resp.Err}
		return r, false
	}
	if err := json.Unmarshal([]byte(resp.Out), &r); err != nil {
		return status.Report{Helper: status.Helper{State: status.HelperFailed, Detail: "unreadable status report: " + err.Error()}}, false
	}
	return r, true
}

// helperStatusCommand is the status command's wire name (helper.StatusCommand).
const helperStatusCommand = "status"
