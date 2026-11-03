// Package browser owns winuse's Browser Connections: finding a Chromium
// browser's DevTools endpoint, verifying it, launching and cloning
// browsers winuse owns, and the helper's persistent connection per
// browser (manager.go).
//
// Ported from use-browser v0.9.0 (browsers.go, cdp.go's discovery,
// clone.go; see third_party/use-browser/NOTICE) and reshaped for a
// connection the helper holds for its lifetime. Every process start, port
// probe and dial goes through System, so offline tests fake the process
// table and the endpoints.
//
// Since Chromium 136, --remote-debugging-port is ignored for a browser's
// default user-data-dir, so there are three supported routes:
//
//  1. launch: a dedicated profile under %LOCALAPPDATA%\winuse\browser.
//  2. clone: a synced copy of the user's real profile at a non-default
//     path, launched with flag-based debugging (the user's logins).
//  3. connect: the user's running browser with "Allow remote debugging
//     for this browser instance" on at <browser>://inspect; Chrome 144+
//     asks the user to approve each new connection.
//
// A fourth route needs no debugging port at all: the winuse extension in
// the user's running browser (extension.go, internal/bridge), which
// discovery tries first.
package browser

import (
	"path/filepath"
	"strings"
)

// Browser is one installed Chromium browser.
type Browser struct {
	Name string // short name used on the command line: chrome, edge, ...
	Path string // executable path
}

// Display is the browser's name as the user knows it.
func (b Browser) Display() string { return DisplayName(b.Name) }

// Exe is the executable's file name, e.g. msedge.exe.
func (b Browser) Exe() string { return filepath.Base(b.Path) }

// Order is the preference order when nothing names a browser
// (ub/browsers.go:327).
var Order = []string{"chrome", "brave", "edge", "chromium", "vivaldi", "opera"}

var displayNames = map[string]string{
	"chrome": "Chrome", "brave": "Brave", "edge": "Edge",
	"chromium": "Chromium", "vivaldi": "Vivaldi", "opera": "Opera",
}

// exeNames are the browser processes' image names. chrome.exe is shared
// by Chrome and Chromium; their install paths tell them apart.
var exeNames = map[string]string{
	"chrome": "chrome.exe", "brave": "brave.exe", "edge": "msedge.exe",
	"chromium": "chrome.exe", "vivaldi": "vivaldi.exe", "opera": "opera.exe",
}

// DisplayName is a short name's display form; an unknown name is returned
// as is.
func DisplayName(name string) string {
	if d, ok := displayNames[name]; ok {
		return d
	}
	if d, ok := appDisplayNames[name]; ok {
		return d
	}
	return name
}

// Known reports whether a short name is in the browser table.
func Known(name string) bool {
	_, ok := displayNames[name]
	return ok
}

// Env holds the Windows folders the tables are built from, so tests point
// them at temporary directories.
type Env struct {
	LocalAppData    string
	AppData         string
	ProgramFiles    string
	ProgramFilesX86 string
}

// UserDataDirs maps each browser to its standard user-data-dir, where a
// DevToolsActivePort file appears while the inspect toggle is on
// (ub/browsers.go:32-70).
func (e Env) UserDataDirs() map[string]string {
	return map[string]string{
		"chrome":   filepath.Join(e.LocalAppData, `Google\Chrome\User Data`),
		"brave":    filepath.Join(e.LocalAppData, `BraveSoftware\Brave-Browser\User Data`),
		"edge":     filepath.Join(e.LocalAppData, `Microsoft\Edge\User Data`),
		"chromium": filepath.Join(e.LocalAppData, `Chromium\User Data`),
		"vivaldi":  filepath.Join(e.LocalAppData, `Vivaldi\User Data`),
		"opera":    filepath.Join(e.AppData, `Opera Software\Opera Stable`),
	}
}

// CandidatePaths lists where each browser's executable is installed
// (ub/browsers.go:273-304).
func (e Env) CandidatePaths() map[string][]string {
	pf, pf86, local := e.ProgramFiles, e.ProgramFilesX86, e.LocalAppData
	return map[string][]string{
		"chrome": {
			filepath.Join(pf, `Google\Chrome\Application\chrome.exe`),
			filepath.Join(pf86, `Google\Chrome\Application\chrome.exe`),
			filepath.Join(local, `Google\Chrome\Application\chrome.exe`),
		},
		"brave": {
			filepath.Join(pf, `BraveSoftware\Brave-Browser\Application\brave.exe`),
			filepath.Join(pf86, `BraveSoftware\Brave-Browser\Application\brave.exe`),
			filepath.Join(local, `BraveSoftware\Brave-Browser\Application\brave.exe`),
		},
		"edge": {
			filepath.Join(pf, `Microsoft\Edge\Application\msedge.exe`),
			filepath.Join(pf86, `Microsoft\Edge\Application\msedge.exe`),
		},
		"chromium": {
			filepath.Join(local, `Chromium\Application\chrome.exe`),
		},
		"vivaldi": {
			filepath.Join(local, `Vivaldi\Application\vivaldi.exe`),
		},
		"opera": {
			filepath.Join(local, `Programs\Opera\opera.exe`),
		},
	}
}

// installDirs is where each browser's binaries live: an image path under
// one of these belongs to that browser (Opera's running opera.exe sits in a
// versioned folder next to its launcher).
func (e Env) installDirs() map[string][]string {
	out := map[string][]string{}
	for name, paths := range e.CandidatePaths() {
		for _, p := range paths {
			out[name] = append(out[name], filepath.Dir(p))
		}
	}
	return out
}

// InspectURL is the page with the real profile's remote-debugging toggle.
// chrome:// works in every fork, but the fork's own scheme is nicer.
func InspectURL(name string) string {
	return Scheme(name) + "://inspect/#remote-debugging"
}

// Scheme is the browser's internal-page scheme.
func Scheme(name string) string {
	switch name {
	case "edge", "brave", "vivaldi", "opera":
		return name
	}
	return "chrome"
}

// IdentifyImage names the browser a process image path belongs to, or ""
// when it is none of them. The install folder decides first (Chrome and
// Chromium share chrome.exe); a browser installed somewhere unusual is
// still recognised by its image name.
func (e Env) IdentifyImage(path string) string {
	if path == "" {
		return ""
	}
	lower := strings.ToLower(filepath.Clean(path))
	dirs := e.installDirs()
	for _, name := range Order {
		for _, d := range dirs[name] {
			d = strings.ToLower(filepath.Clean(d))
			if d != "" && d != "." && strings.HasPrefix(lower, d+`\`) {
				return name
			}
		}
	}
	base := strings.ToLower(filepath.Base(path))
	for _, name := range Order {
		if name == "chromium" {
			continue // chrome.exe outside Chromium's folder is Chrome
		}
		if exeNames[name] == base {
			return name
		}
	}
	return ""
}

// Detect lists the installed browsers in Order, the first existing
// candidate path of each.
func (e Env) Detect(exists func(string) bool) []Browser {
	cands := e.CandidatePaths()
	var found []Browser
	for _, name := range Order {
		for _, p := range cands[name] {
			if exists(p) {
				found = append(found, Browser{Name: name, Path: p})
				break
			}
		}
	}
	return found
}

// internalPrefixes mark browser-internal pages, which are never a Page
// winuse drives (ub/cdp.go:43).
var internalPrefixes = []string{"chrome://", "chrome-untrusted://", "devtools://", "chrome-extension://", "edge://", "brave://", "vivaldi://", "opera://"}

// IsInternal reports whether a URL is a browser-internal page.
func IsInternal(u string) bool {
	for _, p := range internalPrefixes {
		if strings.HasPrefix(u, p) {
			return true
		}
	}
	return false
}
