// Package notes is the store of Notes (ADR 0024): one-line lessons an
// Agent or the user leaves for a site or an App, which later Agents are
// shown. winuse never writes a Note itself and never acts on one.
//
// This file works out a Note's key. A key is never typed freely:
//   - a site is its registrable domain (gist.github.com → github.com), from
//     the Public Suffix List (golang.org/x/net/publicsuffix), except
//     localhost and IP addresses, which keep their port;
//   - an App is its exe file name, lower-cased (excel.exe).
//
// Classify sorts a `note` target into one of those, a Window reference the
// helper resolves (a browser Window to its Page's site, any other to its
// App), or a refusal. SiteKey and AppKey are the two key functions the
// Window State uses too (ticket 23).
package notes

import (
	"net"
	"net/url"
	"path"
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/net/publicsuffix"
)

// Kind is what a `note` target names.
type Kind int

const (
	// FreeForm is a name winuse cannot key ("Microsoft Excel"): refused,
	// with the Windows it matches in the hint.
	FreeForm Kind = iota
	// Window is a Window ID, or a Tab target <window>:<tab>; the helper
	// resolves it (Target.Window, Target.Tab).
	Window
	// Site is a URL or domain; Target.Key is its site key.
	Site
	// App is an exe name or path; Target.Key is its App key.
	App
	// NoSite is a URL with no site: file://, chrome://, about:blank...
	NoSite
)

// Target is a classified `note` target.
type Target struct {
	Kind   Kind
	Key    string // the Site or App key
	Window string // Kind Window: the Window ID
	Tab    string // Kind Window: the tab id of a Tab target, "" for a Window
	Scheme string // Kind NoSite: the URL's scheme ("file", "chrome")
}

var (
	windowIDPattern  = regexp.MustCompile(`^[0-9]+$`)
	tabTargetPattern = regexp.MustCompile(`^([0-9]+):([0-9A-Za-z]{4,32})$`)
	schemePattern    = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9+.-]*):`)
)

// siteSchemes are the schemes whose URLs have a site.
var siteSchemes = map[string]bool{"http": true, "https": true}

// noSiteSchemes are the schemes a target without "//" may start with that
// still name a page with no site (about:blank, data:...). Any other
// "word:" prefix is a host and port (localhost:3000).
var noSiteSchemes = map[string]bool{
	"about": true, "data": true, "javascript": true, "blob": true, "mailto": true,
	"view-source": true, "chrome": true, "edge": true, "brave": true, "vivaldi": true,
	"opera": true, "file": true, "devtools": true, "chrome-extension": true,
}

// Classify sorts a `note` target: a Window ID or Tab target, a URL or
// domain, an exe name or path, a page with no site, or a free-form name.
func Classify(target string) Target {
	t := strings.Trim(strings.TrimSpace(target), `"'`)
	if t == "" {
		return Target{Kind: FreeForm}
	}
	if windowIDPattern.MatchString(t) {
		return Target{Kind: Window, Window: t}
	}
	if m := tabTargetPattern.FindStringSubmatch(t); m != nil {
		return Target{Kind: Window, Window: m[1], Tab: m[2]}
	}
	if m := schemePattern.FindStringSubmatch(t); m != nil {
		scheme := strings.ToLower(m[1])
		if strings.HasPrefix(t[len(m[0]):], "//") || noSiteSchemes[scheme] {
			if !siteSchemes[scheme] {
				return Target{Kind: NoSite, Scheme: scheme}
			}
			if key, ok := SiteKey(t); ok {
				return Target{Kind: Site, Key: key}
			}
			return Target{Kind: FreeForm}
		}
	}
	if strings.HasSuffix(strings.ToLower(t), ".exe") {
		return Target{Kind: App, Key: AppKey(t)}
	}
	// A domain or host, with an optional port and path.
	hostport := t
	if i := strings.IndexAny(hostport, "/?#"); i >= 0 {
		hostport = hostport[:i]
	}
	host, port, ok := splitHostPort(hostport)
	if !ok {
		return Target{Kind: FreeForm}
	}
	key, ok := hostKey(host, port, "", port != "")
	if !ok {
		return Target{Kind: FreeForm}
	}
	return Target{Kind: Site, Key: key}
}

// SiteKey is the site key of a Page's URL: its registrable domain, or
// localhost or an IP address with its port. ok is false for a URL with no
// site (file://, chrome://, about:blank) or one that does not parse.
func SiteKey(rawURL string) (key string, ok bool) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || !siteSchemes[strings.ToLower(u.Scheme)] || u.Host == "" {
		return "", false
	}
	host, port, ok := splitHostPort(u.Host)
	if !ok {
		return "", false
	}
	return hostKey(host, port, strings.ToLower(u.Scheme), true)
}

// AppKey is the App key of an executable: its file name, lower-cased
// (C:\...\EXCEL.EXE → excel.exe). "" for no path.
func AppKey(exePath string) string {
	p := strings.Trim(strings.TrimSpace(exePath), `"'`)
	p = strings.ReplaceAll(p, `\`, "/")
	if p == "" {
		return ""
	}
	return strings.ToLower(path.Base(p))
}

// splitHostPort splits "host", "host:port", "[v6]" or "[v6]:port". A bare
// IPv6 address without brackets is taken whole.
func splitHostPort(hp string) (host, port string, ok bool) {
	if hp == "" {
		return "", "", false
	}
	if strings.HasPrefix(hp, "[") {
		end := strings.Index(hp, "]")
		if end < 0 {
			return "", "", false
		}
		host, rest := hp[1:end], hp[end+1:]
		if rest == "" {
			return host, "", true
		}
		port, ok := strings.CutPrefix(rest, ":")
		return host, port, ok && validPort(port)
	}
	if strings.Count(hp, ":") > 1 {
		return hp, "", net.ParseIP(hp) != nil
	}
	host, port, found := strings.Cut(hp, ":")
	if found && !validPort(port) {
		return "", "", false
	}
	return host, port, host != ""
}

func validPort(p string) bool {
	return p != "" && len(p) <= 5 && windowIDPattern.MatchString(p)
}

// defaultPorts are the ports a scheme's URL may leave out.
var defaultPorts = map[string]string{"http": "80", "https": "443"}

// hostKey keys a host. explicit says the target was plainly a site (a URL
// with a scheme, or a host with a port); without it a host must end in a
// listed public suffix, so "readme.txt" and "excel" stay free-form names.
func hostKey(host, port, scheme string, explicit bool) (string, bool) {
	if port != "" && defaultPorts[scheme] == port {
		port = ""
	}
	if ip := net.ParseIP(host); ip != nil {
		h := ip.String()
		if strings.Contains(h, ":") {
			h = "[" + h + "]"
		}
		if port != "" {
			h += ":" + port
		}
		return h, true
	}
	ascii := strings.ToLower(strings.TrimSuffix(host, "."))
	if !validHost(ascii) {
		return "", false
	}
	if ascii == "localhost" || strings.HasSuffix(ascii, ".localhost") {
		if port != "" {
			ascii += ":" + port
		}
		return ascii, true
	}
	suffix, icann := publicsuffix.PublicSuffix(ascii)
	listed := icann || strings.Contains(suffix, ".")
	if !explicit && (!listed || !strings.Contains(ascii, ".")) {
		return "", false
	}
	site, err := publicsuffix.EffectiveTLDPlusOne(ascii)
	if err != nil {
		return ascii, true // the host is itself a public suffix (github.io), or one label
	}
	return site, true
}

// validHost reports whether h is a host name: dot-separated labels of
// letters, digits, hyphens and underscores, none empty. A browser reports
// a Page's host in its ASCII (punycode) form; a Unicode host typed by hand
// is kept lower-cased as typed (no IDNA mapping, which would need
// golang.org/x/text).
func validHost(h string) bool {
	if h == "" || len(h) > 253 {
		return false
	}
	for _, label := range strings.Split(h, ".") {
		if label == "" || len(label) > 63 {
			return false
		}
		for _, r := range label {
			if r != '-' && r != '_' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
				return false
			}
		}
	}
	return true
}
