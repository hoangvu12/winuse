package browser

import (
	"fmt"
	"strings"
)

// Kind is one way a Browser Connection can fail. Each kind has exactly one
// sentence the agent can relay to the user and one next command
// (`browser status` prints both; every other browser command fails with
// the same pair).
type Kind int

const (
	NoBrowser        Kind = iota + 1 // nothing from the browser table is installed
	UnknownBrowser                   // a name that is not in the browser table
	NotInstalled                     // a named or pinned browser is not installed
	Ambiguous                        // two browsers are debuggable and none is pinned
	NotRunning                       // the browser is not running
	NoDebugging                      // the browser runs without a usable debugging endpoint
	WrongOwner                       // the endpoint's port belongs to another process
	UnknownOwner                     // the port's owner could not be read
	NotLoopback                      // the endpoint's WebSocket URL is not loopback
	AwaitingApproval                 // Chrome 144+ holds the handshake for the user's approval
	Refused                          // the browser refused the WebSocket handshake
	Unreachable                      // the endpoint stopped answering
	ToggleOff                        // connect: the inspect toggle never came on
	ProfileBusy                      // a browser holds winuse's profile without serving DevTools
	NoUserData                       // clone: the browser has no user-data-dir
	NoSuchProfile                    // clone: the named profile folder is missing
	NotStarted                       // launch/clone: the started browser never served DevTools
	CloseFailed                      // clone --close-source: the browser kept its cookies locked
	SeveralOwned                     // stop: more than one winuse-started instance runs

	// The winuse extension (extension.go, ticket 02).
	ExtNotInstalled    // the extension folder or the native host manifest is missing
	ExtNotRegistered   // the native host is not registered for this browser
	ExtNotLoaded       // the user has not loaded the extension in this browser yet
	ExtDisabled        // the extension is loaded but switched off
	ExtBrowserClosed   // the extension is set up, but the browser is not running
	ExtNotConnected    // loaded and on, the browser runs, but no bridge came up
	ExtOutdated        // a different build is loaded and it did not reload itself
	ExtPaused          // the user turned winuse off (Cancel, or the toolbar icon)
	ExtUnreachable     // a bridge is up but the extension did not answer
	ExtSeveralProfiles // the extension runs in several profiles of one browser
	ExtCannot          // a browser-level method the extension cannot run

	// A Window's own browser process (a connection per process).
	NoWindowEndpoint // no endpoint belongs to the process that owns the Window
	SeveralEndpoints // two debugging endpoints belong to that one process

	kindEnd // one past the last kind (tests)
)

// Problem is a Browser Connection failure with what the agent needs to
// recover from it.
type Problem struct {
	Kind    Kind
	Browser string   // the browser's short name ("" when unknown)
	Others  []string // Ambiguous: every debuggable browser, in Order; NoWindowEndpoint: the pids winuse can reach; SeveralEndpoints: the endpoints
	Owner   string   // WrongOwner: the image name holding the port
	Port    int
	URL     string
	Dir     string
	Profile string // NoSuchProfile: the profile folder asked for
	Detail  string // NotInstalled: installed browsers; NoSuchProfile: the profiles there; Refused, Unreachable: the error
	Pinned  bool   // NotInstalled: the browser came from the pin
	Mode    Mode   // ProfileBusy, NotStarted: launch or clone
	PID     int    // NotStarted; NoWindowEndpoint, SeveralEndpoints: the Window's browser process
}

func (p *Problem) Error() string { return p.Sentence() }

// name is the browser's display name for the sentences.
func (p *Problem) name() string {
	if p.Browser == "" {
		return "the browser"
	}
	return DisplayName(p.Browser)
}

// arg is the browser argument for a next command ("" adds nothing).
func (p *Problem) arg() string {
	if p.Browser == "" {
		return ""
	}
	return " " + p.Browser
}

// Sentence is the one sentence that says what is wrong, for the agent to
// relay as is. Like every winuse message it starts in lower case (a
// browser's name keeps its capital) and has no full stop, so a caller can
// add to it ("; nothing was sent").
func (p *Problem) Sentence() string {
	switch p.Kind {
	case NoBrowser:
		return "no Chromium browser is installed (winuse looked for Chrome, Brave, Edge, Chromium, Vivaldi and Opera)"
	case UnknownBrowser:
		return fmt.Sprintf("%q is not a browser winuse knows; use chrome, brave, edge, chromium, vivaldi or opera", p.Browser)
	case NotInstalled:
		s := p.name() + " is not installed"
		if p.Pinned {
			s = "the pinned browser, " + p.name() + ", is not installed"
		}
		if p.Detail != "" {
			s += " (installed: " + p.Detail + ")"
		}
		return s
	case Ambiguous:
		names := make([]string, len(p.Others))
		for i, n := range p.Others {
			names[i] = DisplayName(n)
		}
		return fmt.Sprintf("%s %s accept a debugging connection and no browser is pinned, so winuse will not guess", joinAnd(names), bothOrAll(len(names)))
	case NotRunning:
		return p.name() + " is not running with a debugging connection winuse can use"
	case NoDebugging:
		return p.name() + " is running, but without a debugging connection winuse can use"
	case WrongOwner:
		if p.Browser == "" {
			return fmt.Sprintf("port %d answers as a browser's debugging connection, but it belongs to %s, which is not a browser winuse knows, so winuse will not use it", p.Port, p.Owner)
		}
		return fmt.Sprintf("port %d answers as %s's debugging connection, but it belongs to %s, so winuse will not use it", p.Port, p.name(), p.Owner)
	case UnknownOwner:
		return fmt.Sprintf("winuse could not tell which process owns port %d, so it will not use it", p.Port)
	case NotLoopback:
		return fmt.Sprintf("%s's debugging connection %s is not on this machine's loopback address, so winuse will not use it", p.name(), p.URL)
	case AwaitingApproval:
		return p.name() + " is waiting for the user to approve remote debugging in its \"Allow remote debugging?\" prompt"
	case Refused:
		return fmt.Sprintf("%s refused the debugging connection (%s); its approval prompt was probably declined", p.name(), p.Detail)
	case Unreachable:
		return fmt.Sprintf("%s stopped answering on port %d (%s)", p.name(), p.Port, p.Detail)
	case ToggleOff:
		return "remote debugging is still off in " + p.name()
	case ProfileBusy:
		return fmt.Sprintf("a %s is already using winuse's %s profile at %s but does not answer as a debugging connection", p.name(), p.Mode, p.Dir)
	case NoUserData:
		return fmt.Sprintf("no %s profile was found at %s (has %s ever been run?)", p.name(), p.Dir, p.name())
	case NoSuchProfile:
		return fmt.Sprintf("%s has no profile folder %q; it has %s", p.name(), p.Profile, p.Detail)
	case NotStarted:
		return fmt.Sprintf("%s started (pid %d) but never answered as a debugging connection on port %d", p.name(), p.PID, p.Port)
	case CloseFailed:
		return fmt.Sprintf("%s still holds its cookie database after winuse asked it to close", p.name())
	case SeveralOwned:
		return fmt.Sprintf("more than one %s started by winuse is running", p.name())
	case ExtNotInstalled:
		if p.Detail != "" {
			return fmt.Sprintf("the winuse native host points at %s, which no longer exists", p.Detail)
		}
		return "the winuse extension is not installed"
	case ExtNotRegistered:
		return fmt.Sprintf("the winuse native host is not registered for %s, so %s cannot start it", p.name(), p.name())
	case ExtNotLoaded:
		return fmt.Sprintf("the winuse extension is not loaded in %s yet", p.name())
	case ExtDisabled:
		return fmt.Sprintf("the winuse extension is loaded in %s (%s) but switched off", p.name(), p.Detail)
	case ExtBrowserClosed:
		return fmt.Sprintf("the winuse extension is set up in %s, but %s is not running", p.name(), p.name())
	case ExtNotConnected:
		s := fmt.Sprintf("the winuse extension is loaded in %s but has not connected to winuse", p.name())
		if p.Detail != "" {
			s += " (last host log: " + p.Detail + ")"
		}
		return s
	case ExtOutdated:
		return fmt.Sprintf("the winuse extension in %s is out of date and did not reload itself (%s)", p.name(), p.Detail)
	case ExtPaused:
		return fmt.Sprintf("the user turned winuse off in %s (Cancel on the debugging bar, or the winuse toolbar icon)", p.name())
	case ExtUnreachable:
		return fmt.Sprintf("the winuse extension in %s is running but did not answer (%s)", p.name(), p.Detail)
	case ExtSeveralProfiles:
		return fmt.Sprintf("the winuse extension runs in %d %s profiles at once, so winuse will not guess which one to drive", len(p.Others), p.name())
	case ExtCannot:
		return p.Detail + " needs a launched or cloned browser; the winuse extension cannot do it"
	case NoWindowEndpoint:
		s := fmt.Sprintf("this %s window belongs to %s process %d, which has no debugging connection winuse can use", p.name(), p.name(), p.PID)
		if r := p.Reachable(); r != "" {
			s += " (winuse can reach " + r + ")"
		}
		return s
	case SeveralEndpoints:
		return fmt.Sprintf("%s process %d answers on %s, so winuse will not guess which one to use", p.name(), p.PID, joinAnd(p.Others))
	}
	return "the Browser Connection failed"
}

// Reachable names the other processes of a NoWindowEndpoint's browser that
// winuse can reach ("Chrome process 4242", "Chrome processes 4242 and
// 5151"), or "" for none.
func (p *Problem) Reachable() string {
	switch len(p.Others) {
	case 0:
		return ""
	case 1:
		return p.name() + " process " + p.Others[0]
	}
	return p.name() + " processes " + joinAnd(p.Others)
}

// Next is the command (or user step) that recovers, worded as winuse's
// other hints are.
func (p *Problem) Next() string {
	b := p.arg()
	switch p.Kind {
	case NoBrowser:
		return "install Chrome or Edge, then run winuse browser launch"
	case UnknownBrowser, NotInstalled:
		if p.Pinned {
			return "run winuse browser use auto, or pin an installed browser with winuse browser use <browser>"
		}
		return "run winuse browser use to list the installed browsers"
	case Ambiguous:
		return "run winuse browser use " + p.Others[0] + " to pin one (the pin persists)"
	case NotRunning:
		return "ask the user to open " + p.name() + " to work in their own profile (then winuse browser ext install" + b + " --open); only if they want a separate browser, winuse browser launch" + b + " for a winuse profile, or winuse browser clone" + b + " for a copy with their logins"
	case NoDebugging:
		return "for the user's own browser with their logins and tabs, run winuse browser ext install" + b + " --open (the user loads the extension once; then winuse browser ext status" + b + " --wait 120); otherwise winuse browser clone" + b + " for a copy with your logins (--close-source moves your open tabs into it), winuse browser launch" + b + " for a winuse profile, or winuse browser connect" + b + " to use the running browser through its inspect toggle"
	case WrongOwner, UnknownOwner, NotLoopback:
		return "run winuse browser launch" + b + " for a browser winuse starts itself"
	case AwaitingApproval:
		return "click Allow in " + p.name() + "'s \"Allow remote debugging?\" prompt (find its Window with winuse list-windows --app " + p.Browser + ", then winuse click <window> name:Allow; --headed if that is refused), then run winuse browser status" + b
	case Refused:
		return "run winuse browser connect" + b + " again and click Allow in the prompt (winuse click <window> name:Allow); if it cannot be allowed, tell the user and offer winuse browser clone" + b + " for a copy that never asks"
	case Unreachable:
		return "run winuse browser status" + b + " again; if the browser closed, winuse browser launch" + b
	case ToggleOff:
		return "turn on \"Allow remote debugging for this browser instance\" at " + InspectURL(p.Browser) + ", then run winuse browser connect" + b
	case ProfileBusy:
		return "close that " + p.name() + " window, then run winuse browser " + string(p.Mode) + b + " again"
	case NoUserData:
		return "open " + p.name() + " once so it creates a profile, or run winuse browser launch" + b
	case NoSuchProfile:
		return "run winuse browser clone" + b + " --profile <one of those folders>"
	case NotStarted:
		return "close any " + p.name() + " window using winuse's profile, then run winuse browser " + string(p.Mode) + b + " again"
	case CloseFailed:
		return "close " + p.name() + " yourself, then run winuse browser clone" + b + " again"
	case SeveralOwned:
		return "close the extra " + p.name() + " window, then run winuse browser stop" + b + " again"
	case ExtNotInstalled, ExtNotRegistered:
		return "run winuse browser ext install" + b
	case ExtNotLoaded:
		return "tell the user: in " + p.name() + ", open " + Scheme(p.Browser) + `://extensions, turn on "Developer mode", click "Load unpacked" and choose ` + p.Dir + "; then run winuse browser ext status" + b + " --wait 120"
	case ExtDisabled:
		return "tell the user: in " + p.name() + ", open " + Scheme(p.Browser) + `://extensions and turn on "winuse"; then run winuse browser ext status` + b
	case ExtBrowserClosed:
		return "ask the user to open " + p.name() + " (the extension connects when it starts), then run winuse browser ext status" + b
	case ExtNotConnected:
		return "run winuse browser ext install" + b + " (it registers the native host under every registry key " + p.name() + " may read), then reload the extension: in " + p.name() + ", open " + Scheme(p.Browser) + `://extensions and click the reload icon on "winuse"; if its toolbar icon shows a red "!", hovering it shows why`
	case ExtOutdated, ExtUnreachable:
		return "tell the user: in " + p.name() + ", open " + Scheme(p.Browser) + `://extensions and click the reload icon on "winuse"; then run winuse browser ext status` + b
	case ExtPaused:
		return "do not work around it; if the user wants winuse back, they click the winuse icon in " + p.name() + "'s toolbar (under the puzzle-piece Extensions button), or use winuse browser launch" + b + " for a winuse profile"
	case ExtSeveralProfiles:
		return "ask the user to turn winuse off in all but one " + p.name() + " profile (click its toolbar icon), then run winuse browser status" + b
	case ExtCannot:
		return "run winuse browser launch" + b + " or winuse browser clone" + b + ", which take browser-level commands"
	case NoWindowEndpoint:
		s := "if it is the user's own " + p.name() + ", run winuse browser ext install" + b + " to reach it through the winuse extension; "
		if r := p.Reachable(); r != "" {
			return s + "otherwise work in a window of " + r
		}
		return s + "otherwise run winuse browser launch" + b + " and work in its windows"
	case SeveralEndpoints:
		return "restart that " + p.name() + " with one debugging port; if winuse launched it, run winuse browser stop" + b + ", then winuse browser launch" + b
	}
	return "run winuse browser status"
}

// joinAnd joins names as "A and B" or "A, B and C".
func joinAnd(names []string) string {
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}

func bothOrAll(n int) string {
	if n == 2 {
		return "both"
	}
	return "all"
}
