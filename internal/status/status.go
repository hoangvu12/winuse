// Package status is `winuse status` (desktop parity ticket 16): the
// report the CLI prints, one line each, and the shape the helper sends
// its half in. The helper fills what only it knows (its version, uptime,
// integrity level, DPI awareness, Browser Connections); the CLI adds its
// own version, or fills the machine lines itself when no helper is
// running. Every probe behind a Report is read-only: status never starts
// the helper, dials a browser or sends input.
package status

import (
	"fmt"
	"strings"
	"time"
)

// Report is everything `winuse status` prints. The helper answers the
// status command with it as JSON; the CLI decodes it, sets CLIVersion
// (and Helper when no helper answered) and prints Format.
type Report struct {
	CLIVersion string `json:"-"` // the CLI's own build; set by the CLI

	Helper Helper `json:"helper"`

	Locked  bool   `json:"locked"`            // the input desktop refuses input
	Desktop string `json:"desktop,omitempty"` // the input desktop's name ("" unreadable: the secure desktop)

	Integrity string `json:"integrity"` // the process's integrity level, e.g. "medium"
	DPI       string `json:"dpi"`       // the process's DPI awareness, e.g. "per-monitor v2"
	// Source names whose integrity and DPI awareness these are: "helper",
	// or "cli" when no helper answered (a helper the CLI starts runs at
	// the CLI's integrity level and sets the same awareness).
	Source string `json:"source"`

	// Browsers are the Browser Connections the helper holds open, one
	// phrase each; BrowsersKnown is false when they could not be read (no
	// helper answered, or its Browser service cannot list them).
	Browsers      []string `json:"browsers,omitempty"`
	BrowsersKnown bool     `json:"browsersKnown"`

	Windows string `json:"windows"` // the Windows build, e.g. "Windows 11 25H2, build 26200.6899"

	// Agents are the Agents the helper has served since it started, the
	// most recent first (multi-agent ticket 02).
	Agents []Agent `json:"agents,omitempty"`
}

// Agent is one Agent the helper has served: its label, its identity, its
// last command and how long ago that command came.
type Agent struct {
	Label   string        `json:"label"`
	ID      string        `json:"id"`
	Command string        `json:"command"`
	Ago     time.Duration `json:"ago"`
}

// Helper is the helper's line.
type Helper struct {
	State   HelperState   `json:"state"`
	Version string        `json:"version,omitempty"`
	PID     int           `json:"pid,omitempty"`
	Uptime  time.Duration `json:"uptime,omitempty"`
	Detail  string        `json:"detail,omitempty"` // why a running helper did not answer (HelperFailed)
}

// HelperState is what the CLI found on the helper's pipe.
type HelperState string

const (
	HelperRunning    HelperState = "running"     // it answered status
	HelperNotRunning HelperState = "not-running" // no helper: nothing was started
	HelperStale      HelperState = "stale"       // an older build answered stale and exits itself
	HelperFailed     HelperState = "failed"      // it is there but the exchange failed (Detail)
)

// Integrity and DPI sources.
const (
	SourceHelper = "helper"
	SourceCLI    = "cli"
)

// Format renders the report, one line each: winuse, helper, desktop,
// integrity, DPI awareness, Browser Connections, Agents, Windows.
func (r Report) Format() string {
	var b strings.Builder
	fmt.Fprintf(&b, "winuse: %s\n", r.CLIVersion)
	fmt.Fprintf(&b, "helper: %s\n", r.helperLine())
	fmt.Fprintf(&b, "desktop: %s\n", r.desktopLine())
	whose := "the helper's"
	if r.Source != SourceHelper {
		whose = "this CLI's; a helper it starts runs at the same level"
	}
	fmt.Fprintf(&b, "integrity: %s (%s)\n", orUnknown(r.Integrity), whose)
	if r.Source != SourceHelper {
		whose = "this CLI's"
	}
	fmt.Fprintf(&b, "dpi awareness: %s (%s)\n", orUnknown(r.DPI), whose)
	fmt.Fprintf(&b, "browser connections: %s\n", r.browsersLine())
	fmt.Fprintf(&b, "agents: %s\n", r.agentsLine())
	fmt.Fprintf(&b, "windows: %s\n", orUnknown(r.Windows))
	return b.String()
}

func (r Report) helperLine() string {
	h := r.Helper
	switch h.State {
	case HelperRunning:
		line := "running, " + orUnknown(h.Version)
		if h.Version != "" && r.CLIVersion != "" && h.Version != r.CLIVersion {
			line += " (a different build; the next command restarts it)"
		}
		if h.PID != 0 {
			line += fmt.Sprintf(", pid %d", h.PID)
		}
		return line + ", up " + FormatUptime(h.Uptime)
	case HelperStale:
		return "a different winuse build was running and is exiting; the next command starts a current one"
	case HelperFailed:
		return "running but not answering: " + orUnknown(h.Detail)
	}
	return "not running; the next command starts it"
}

func (r Report) desktopLine() string {
	if !r.Locked {
		return "unlocked"
	}
	if r.Desktop != "" && r.Desktop != "Default" {
		return fmt.Sprintf("locked (input desktop is %s); unlock before using winuse", r.Desktop)
	}
	return "locked; unlock before using winuse"
}

func (r Report) browsersLine() string {
	switch {
	case r.Helper.State != HelperRunning:
		return "none; only a running helper holds them"
	case !r.BrowsersKnown:
		return "unknown"
	case len(r.Browsers) == 0:
		return "none open; winuse browser status connects one"
	}
	return strings.Join(r.Browsers, "; ")
}

func (r Report) agentsLine() string {
	if r.Helper.State != HelperRunning {
		return "none; only a running helper sees them"
	}
	if len(r.Agents) == 0 {
		return "none since the helper started"
	}
	parts := make([]string, len(r.Agents))
	for i, a := range r.Agents {
		parts[i] = fmt.Sprintf("%s (%s, %s ago)", orUnknown(a.Label), orUnknown(a.Command), FormatUptime(a.Ago))
	}
	return strings.Join(parts, "; ")
}

// FormatUptime renders an uptime to the second ("4m12s"; "0s" under one).
func FormatUptime(d time.Duration) string {
	if d < time.Second {
		return "0s"
	}
	return d.Truncate(time.Second).String()
}

func orUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}
