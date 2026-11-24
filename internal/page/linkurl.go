package page

import (
	"net/url"
	"strings"
	"unicode/utf8"
)

// LinkURLMax bounds a link's URL on a Page line, in characters, the "…"
// that marks a cut included. It is read.js's LINK_MAX.
const LinkURLMax = 60

// LinkURL is how a Page line prints a link's address (bladebro-ideas 01),
// by the rule read.js's shortURL gives `winuse read` (ticket 25): on the
// Page's own site (the same host) the path, query and fragment, or only
// the fragment when it points into the Page itself; on another site the
// host without "www." and the path. Unlike read.js, every URL is cut at
// LinkURLMax, a same-site one too, a javascript: URL prints as nothing and
// a data: URL as "data:…": a line never carries script or a whole file.
// href is the AX tree's url property (absolute), page the Tab's URL.
func LinkURL(href, page string) string {
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
		return clipURL(href)
	}
	here, herr := url.Parse(page)
	if herr == nil {
		u = here.ResolveReference(u)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return clipURL(u.String())
	}
	tail := u.EscapedPath()
	if u.RawQuery != "" {
		tail += "?" + u.RawQuery
	}
	if u.Fragment != "" {
		tail += "#" + u.EscapedFragment()
	}
	if herr == nil && here.Host != "" && strings.EqualFold(u.Host, here.Host) {
		if u.EscapedPath() == here.EscapedPath() && u.RawQuery == here.RawQuery && u.Fragment != "" {
			return clipURL("#" + u.EscapedFragment())
		}
		if tail == "" {
			tail = "/"
		}
		return clipURL(tail)
	}
	s := strings.TrimPrefix(strings.ToLower(u.Host), "www.")
	if tail != "/" {
		s += tail
	}
	return clipURL(s)
}

// clipURL cuts s to LinkURLMax characters, marking the cut with "…".
func clipURL(s string) string {
	if utf8.RuneCountInString(s) <= LinkURLMax {
		return s
	}
	return string([]rune(s)[:LinkURLMax-1]) + "…"
}
