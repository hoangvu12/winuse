package browser

// One case per failure state: the exact sentence the agent relays and
// the exact next command. A new Kind without a case here fails
// TestEveryKindHasASentence.

import (
	"strings"
	"testing"
)

var problemCases = []struct {
	name     string
	p        Problem
	sentence string
	next     string
}{
	{"no browser", Problem{Kind: NoBrowser},
		"no Chromium browser is installed (winuse looked for Chrome, Brave, Edge, Chromium, Vivaldi and Opera)",
		"install Chrome or Edge, then run winuse browser launch"},
	{"unknown browser", Problem{Kind: UnknownBrowser, Browser: "firefox"},
		`"firefox" is not a browser winuse knows; use chrome, brave, edge, chromium, vivaldi or opera`,
		"run winuse browser use to list the installed browsers"},
	{"not installed", Problem{Kind: NotInstalled, Browser: "opera", Detail: "chrome, edge"},
		"Opera is not installed (installed: chrome, edge)",
		"run winuse browser use to list the installed browsers"},
	{"pinned not installed", Problem{Kind: NotInstalled, Browser: "vivaldi", Pinned: true, Detail: "chrome"},
		"the pinned browser, Vivaldi, is not installed (installed: chrome)",
		"run winuse browser use auto, or pin an installed browser with winuse browser use <browser>"},
	{"ambiguous, two", Problem{Kind: Ambiguous, Others: []string{"chrome", "edge"}},
		"Chrome and Edge both accept a debugging connection and no browser is pinned, so winuse will not guess",
		"run winuse browser use chrome to pin one (the pin persists)"},
	{"ambiguous, three", Problem{Kind: Ambiguous, Others: []string{"chrome", "brave", "edge"}},
		"Chrome, Brave and Edge all accept a debugging connection and no browser is pinned, so winuse will not guess",
		"run winuse browser use chrome to pin one (the pin persists)"},
	{"not running", Problem{Kind: NotRunning, Browser: "chrome"},
		"Chrome is not running with a debugging connection winuse can use",
		"ask the user to open Chrome to work in their own profile (then winuse browser ext install chrome --open); only if they want a separate browser, winuse browser launch chrome for a winuse profile, or winuse browser clone chrome for a copy with their logins"},
	{"running without debugging", Problem{Kind: NoDebugging, Browser: "edge"},
		"Edge is running, but without a debugging connection winuse can use",
		"for the user's own browser with their logins and tabs, run winuse browser ext install edge --open (the user loads the extension once; then winuse browser ext status edge --wait 120); otherwise winuse browser clone edge for a copy with your logins (--close-source moves your open tabs into it), winuse browser launch edge for a winuse profile, or winuse browser connect edge to use the running browser through its inspect toggle"},
	{"wrong owner", Problem{Kind: WrongOwner, Browser: "chrome", Owner: "brave.exe", Port: 9222},
		"port 9222 answers as Chrome's debugging connection, but it belongs to brave.exe, so winuse will not use it",
		"run winuse browser launch chrome for a browser winuse starts itself"},
	{"owner not a browser", Problem{Kind: WrongOwner, Owner: "node.exe", Port: 9222},
		"port 9222 answers as a browser's debugging connection, but it belongs to node.exe, which is not a browser winuse knows, so winuse will not use it",
		"run winuse browser launch for a browser winuse starts itself"},
	{"unknown owner", Problem{Kind: UnknownOwner, Port: 9223},
		"winuse could not tell which process owns port 9223, so it will not use it",
		"run winuse browser launch for a browser winuse starts itself"},
	{"not loopback", Problem{Kind: NotLoopback, Browser: "chrome", URL: "ws://10.0.0.7:9222/devtools/browser/x"},
		"Chrome's debugging connection ws://10.0.0.7:9222/devtools/browser/x is not on this machine's loopback address, so winuse will not use it",
		"run winuse browser launch chrome for a browser winuse starts itself"},
	{"awaiting approval", Problem{Kind: AwaitingApproval, Browser: "chrome"},
		`Chrome is waiting for the user to approve remote debugging in its "Allow remote debugging?" prompt`,
		`click Allow in Chrome's "Allow remote debugging?" prompt (find its Window with winuse list-windows --app chrome, then winuse click <window> name:Allow; --headed if that is refused), then run winuse browser status chrome`},
	{"refused", Problem{Kind: Refused, Browser: "chrome", Detail: "403 Forbidden"},
		"Chrome refused the debugging connection (403 Forbidden); its approval prompt was probably declined",
		"run winuse browser connect chrome again and click Allow in the prompt (winuse click <window> name:Allow); if it cannot be allowed, tell the user and offer winuse browser clone chrome for a copy that never asks"},
	{"unreachable", Problem{Kind: Unreachable, Browser: "edge", Port: 9301, Detail: "connection refused"},
		"Edge stopped answering on port 9301 (connection refused)",
		"run winuse browser status edge again; if the browser closed, winuse browser launch edge"},
	{"toggle off", Problem{Kind: ToggleOff, Browser: "edge"},
		"remote debugging is still off in Edge",
		`turn on "Allow remote debugging for this browser instance" at edge://inspect/#remote-debugging, then run winuse browser connect edge`},
	{"profile busy", Problem{Kind: ProfileBusy, Browser: "chrome", Mode: ModeLaunch, Dir: `C:\w\profile-chrome`},
		`a Chrome is already using winuse's launch profile at C:\w\profile-chrome but does not answer as a debugging connection`,
		"close that Chrome window, then run winuse browser launch chrome again"},
	{"no user data", Problem{Kind: NoUserData, Browser: "brave", Dir: `C:\u\Brave-Browser\User Data`},
		`no Brave profile was found at C:\u\Brave-Browser\User Data (has Brave ever been run?)`,
		"open Brave once so it creates a profile, or run winuse browser launch brave"},
	{"no such profile", Problem{Kind: NoSuchProfile, Browser: "chrome", Profile: "Work", Detail: "Default, Profile 1"},
		`Chrome has no profile folder "Work"; it has Default, Profile 1`,
		"run winuse browser clone chrome --profile <one of those folders>"},
	{"not started", Problem{Kind: NotStarted, Browser: "chrome", Mode: ModeClone, PID: 4242, Port: 9301},
		"Chrome started (pid 4242) but never answered as a debugging connection on port 9301",
		"close any Chrome window using winuse's profile, then run winuse browser clone chrome again"},
	{"close failed", Problem{Kind: CloseFailed, Browser: "chrome"},
		"Chrome still holds its cookie database after winuse asked it to close",
		"close Chrome yourself, then run winuse browser clone chrome again"},
	{"several owned", Problem{Kind: SeveralOwned, Browser: "edge"},
		"more than one Edge started by winuse is running",
		"close the extra Edge window, then run winuse browser stop edge again"},

	// The winuse extension (ticket 02).
	{"ext not installed", Problem{Kind: ExtNotInstalled, Browser: "chrome"},
		"the winuse extension is not installed",
		"run winuse browser ext install chrome"},
	{"ext host binary gone", Problem{Kind: ExtNotInstalled, Browser: "chrome", Detail: `C:\old\winuse.exe`},
		`the winuse native host points at C:\old\winuse.exe, which no longer exists`,
		"run winuse browser ext install chrome"},
	{"ext not registered", Problem{Kind: ExtNotRegistered, Browser: "edge"},
		"the winuse native host is not registered for Edge, so Edge cannot start it",
		"run winuse browser ext install edge"},
	{"ext not loaded", Problem{Kind: ExtNotLoaded, Browser: "brave", Dir: `C:\w\extension`},
		"the winuse extension is not loaded in Brave yet",
		`tell the user: in Brave, open brave://extensions, turn on "Developer mode", click "Load unpacked" and choose C:\w\extension; then run winuse browser ext status brave --wait 120`},
	{"ext disabled", Problem{Kind: ExtDisabled, Browser: "chrome", Detail: "Default, Profile 1"},
		"the winuse extension is loaded in Chrome (Default, Profile 1) but switched off",
		`tell the user: in Chrome, open chrome://extensions and turn on "winuse"; then run winuse browser ext status chrome`},
	{"ext browser closed", Problem{Kind: ExtBrowserClosed, Browser: "chrome"},
		"the winuse extension is set up in Chrome, but Chrome is not running",
		"ask the user to open Chrome (the extension connects when it starts), then run winuse browser ext status chrome"},
	{"ext not connected", Problem{Kind: ExtNotConnected, Browser: "edge", Detail: "no hello from the extension: EOF"},
		"the winuse extension is loaded in Edge but has not connected to winuse (last host log: no hello from the extension: EOF)",
		`run winuse browser ext install edge (it registers the native host under every registry key Edge may read), then reload the extension: in Edge, open edge://extensions and click the reload icon on "winuse"; if its toolbar icon shows a red "!", hovering it shows why`},
	{"ext outdated", Problem{Kind: ExtOutdated, Browser: "chrome", Detail: "it did not reconnect within 15s after reloading"},
		"the winuse extension in Chrome is out of date and did not reload itself (it did not reconnect within 15s after reloading)",
		`tell the user: in Chrome, open chrome://extensions and click the reload icon on "winuse"; then run winuse browser ext status chrome`},
	{"ext paused", Problem{Kind: ExtPaused, Browser: "chrome"},
		"the user turned winuse off in Chrome (Cancel on the debugging bar, or the winuse toolbar icon)",
		"do not work around it; if the user wants winuse back, they click the winuse icon in Chrome's toolbar (under the puzzle-piece Extensions button), or use winuse browser launch chrome for a winuse profile"},
	{"ext unreachable", Problem{Kind: ExtUnreachable, Browser: "chrome", Detail: "i/o timeout"},
		"the winuse extension in Chrome is running but did not answer (i/o timeout)",
		`tell the user: in Chrome, open chrome://extensions and click the reload icon on "winuse"; then run winuse browser ext status chrome`},
	{"ext several profiles", Problem{Kind: ExtSeveralProfiles, Browser: "chrome", Others: []string{"ab12cd34", "ef56ab78"}},
		"the winuse extension runs in 2 Chrome profiles at once, so winuse will not guess which one to drive",
		"ask the user to turn winuse off in all but one Chrome profile (click its toolbar icon), then run winuse browser status chrome"},
	{"ext cannot", Problem{Kind: ExtCannot, Detail: "Browser.setDownloadBehavior"},
		"Browser.setDownloadBehavior needs a launched or cloned browser; the winuse extension cannot do it",
		"run winuse browser launch or winuse browser clone, which take browser-level commands"},

	// A Window's own browser process.
	{"no endpoint for the window", Problem{Kind: NoWindowEndpoint, Browser: "chrome", PID: 5151},
		"this Chrome window belongs to Chrome process 5151, which has no debugging connection winuse can use",
		"if it is the user's own Chrome, run winuse browser ext install chrome to reach it through the winuse extension; otherwise run winuse browser launch chrome and work in its windows"},
	{"no endpoint for the window, others reachable", Problem{Kind: NoWindowEndpoint, Browser: "chrome", PID: 5151, Others: []string{"4242", "6000"}},
		"this Chrome window belongs to Chrome process 5151, which has no debugging connection winuse can use (winuse can reach Chrome processes 4242 and 6000)",
		"if it is the user's own Chrome, run winuse browser ext install chrome to reach it through the winuse extension; otherwise work in a window of Chrome processes 4242 and 6000"},
	{"several endpoints for one process", Problem{Kind: SeveralEndpoints, Browser: "edge", PID: 4242, Others: []string{"port 9301 (winuse's own profile)", "port 9222 (a debugging port on this machine)"}},
		"Edge process 4242 answers on port 9301 (winuse's own profile) and port 9222 (a debugging port on this machine), so winuse will not guess which one to use",
		"restart that Edge with one debugging port; if winuse launched it, run winuse browser stop edge, then winuse browser launch edge"},
}

func TestProblemSentences(t *testing.T) {
	for _, tc := range problemCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.p.Sentence(); got != tc.sentence {
				t.Errorf("sentence = %q\n            want %q", got, tc.sentence)
			}
			if got := tc.p.Next(); got != tc.next {
				t.Errorf("next = %q\n        want %q", got, tc.next)
			}
			if tc.p.Error() != tc.sentence {
				t.Error("Error() is not the sentence")
			}
			for _, s := range []string{tc.sentence, tc.next} {
				if strings.ContainsAny(s, "\u2013\u2014\u2018\u2019\u201c\u201d\n") {
					t.Errorf("%q has a dash, curly quote or line break", s)
				}
			}
			// winuse's message style: no full stop, a lower-case start but
			// for a browser's name, and the glossary's words (CONTEXT.md).
			if strings.HasSuffix(tc.sentence, ".") || strings.Contains(tc.sentence, "endpoint") {
				t.Errorf("sentence %q ends in a full stop or says endpoint", tc.sentence)
			}
			if first := strings.Fields(tc.sentence)[0]; first != strings.ToLower(first) && !strings.HasPrefix(first, DisplayName(tc.p.Browser)) && first != "Browser.setDownloadBehavior" {
				t.Errorf("sentence %q starts with a capital", tc.sentence)
			}
		})
	}
}

func TestEveryKindHasASentence(t *testing.T) {
	covered := map[Kind]bool{}
	for _, tc := range problemCases {
		covered[tc.p.Kind] = true
	}
	for k := NoBrowser; k < kindEnd; k++ {
		if !covered[k] {
			t.Errorf("kind %d has no sentence case", k)
		}
		if p := (&Problem{Kind: k, Browser: "chrome", Others: []string{"chrome", "edge"}}); p.Sentence() == "the Browser Connection failed" {
			t.Errorf("kind %d falls through to the generic sentence", k)
		}
	}
}
