package page

import (
	"net/url"
	"strings"

	"github.com/hoangvu12/winuse/internal/notes"
)

// LinkURLMax bounds a shortened URL, in characters, the "…" that marks a
// cut included: every URL on a Page line, and another site's in `winuse
// read`.
const LinkURLMax = 60

// LinkURL is how a Page line prints a link's address (bladebro-ideas 01):
// on the Page's own origin (scheme, host and port) the path, query and
// fragment, or only the fragment when it points into the Page itself;
// elsewhere the host without "www." and the path. Every URL is cut at
// LinkURLMax. href is the AX tree's url property (absolute), page the
// Tab's URL.
func LinkURL(href, page string) string {
	return shortURL(href, page, sameOrigin, true)
}

// SiteURL is how `winuse read` (ticket 25) and the failed-request note
// (ticket 29) print a URL: LinkURL's rule, with the Page's own site (its
// registrable domain, or localhost or an IP address with its port, as a
// Note's key) in place of its origin, and a URL on that site left uncut.
func SiteURL(href, page string) string {
	return shortURL(href, page, sameSite, false)
}

// shortURL is LinkURL's and SiteURL's one rule; own says whether a URL is
// the Page's own, cutOwn whether an own URL is cut too. A javascript: URL
// prints as nothing and a data: URL as "data:…", so a line never carries
// script or a whole file.
func shortURL(href, page string, own func(u, here *url.URL) bool, cutOwn bool) string {
	href = strings.TrimSpace(href)
	lower := strings.ToLower(href)
	switch {
	case href == "", strings.HasPrefix(lower, "javascript:"):
		return ""
	case strings.HasPrefix(lower, "data:"):
		return "data:…"
	}
	u, err := url.Parse(href)
	if err != nil {
		return CutRunes(href, LinkURLMax)
	}
	here, herr := url.Parse(page)
	if herr == nil {
		u = here.ResolveReference(u)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return CutRunes(u.String(), LinkURLMax)
	}
	tail := u.EscapedPath()
	if u.RawQuery != "" {
		tail += "?" + u.RawQuery
	}
	if u.Fragment != "" {
		tail += "#" + u.EscapedFragment()
	}
	if herr == nil && here.Host != "" && own(u, here) {
		if sameOrigin(u, here) && u.EscapedPath() == here.EscapedPath() && u.RawQuery == here.RawQuery && u.Fragment != "" {
			tail = "#" + u.EscapedFragment()
		} else if tail == "" {
			tail = "/"
		}
		if cutOwn {
			return CutRunes(tail, LinkURLMax)
		}
		return tail
	}
	s := strings.TrimPrefix(strings.ToLower(u.Host), "www.")
	if tail != "/" {
		s += tail
	}
	return CutRunes(s, LinkURLMax)
}

// defaultPorts are the ports a scheme's URL may leave out.
var defaultPorts = map[string]string{"http": "80", "https": "443"}

// sameOrigin reports whether u and here share scheme, host and port.
func sameOrigin(u, here *url.URL) bool {
	port := func(x *url.URL) string {
		if p := x.Port(); p != "" {
			return p
		}
		return defaultPorts[strings.ToLower(x.Scheme)]
	}
	return strings.EqualFold(u.Scheme, here.Scheme) && strings.EqualFold(u.Hostname(), here.Hostname()) && port(u) == port(here)
}

// sameSite reports whether u and here have the same site key.
func sameSite(u, here *url.URL) bool {
	a, ok := notes.SiteKey(u.String())
	b, hok := notes.SiteKey(here.String())
	return ok && hok && a == b
}
