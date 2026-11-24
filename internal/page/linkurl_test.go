package page

import (
	"strings"
	"testing"
)

// TestLinkURLShortensOnlyTheSameOrigin (bladebro-ideas 01): a Page line
// prints a link on the Page's own origin (scheme, host and port) as its
// path; a link that differs in any of the three prints its host too.
func TestLinkURLShortensOnlyTheSameOrigin(t *testing.T) {
	const page = "https://example.test/login"
	cases := []struct{ href, want string }{
		{"https://example.test/reset", "/reset"},
		{"https://EXAMPLE.test:443/reset", "/reset"}, // the default port is no port
		{"http://example.test/plain", "example.test/plain"},
		{"https://example.test:8443/x", "example.test:8443/x"},
		{"https://www.example.test/x", "example.test/x"},
		{"https://docs.example.test/x", "docs.example.test/x"},
		{"https://example.test/login#help", "#help"},
		{"http://example.test/login#help", "example.test/login#help"},
	}
	for _, c := range cases {
		if got := LinkURL(c.href, page); got != c.want {
			t.Errorf("LinkURL(%q) on %s = %q, want %q", c.href, page, got, c.want)
		}
	}
}

// TestSiteURLShortensTheWholeSite (bladebro-ideas 25, 29): `winuse read`
// and the failed-request note print a URL on the Page's own site (its
// registrable domain, or localhost or an IP address with its port) as its
// path, uncut; another site's as its host without "www." and its path, cut
// at LinkURLMax.
func TestSiteURLShortensTheWholeSite(t *testing.T) {
	long := strings.Repeat("p", 80)
	cases := []struct{ page, href, want string }{
		{"https://www.example.com/a/b", "https://www.example.com/c", "/c"},
		{"https://www.example.com/a/b", "https://example.com/c", "/c"},
		{"https://www.example.com/a/b", "https://docs.example.com/c?x=1", "/c?x=1"},
		{"https://www.example.com/a/b", "http://example.com/c", "/c"},
		{"https://www.example.com/a/b", "https://www.example.com/a/b#s", "#s"},
		{"https://www.example.com/a/b", "https://example.com/a/b#s", "/a/b#s"}, // another host: not this document
		{"https://www.example.com/a/b", "https://www.example.com/", "/"},
		{"https://www.example.com/a/b", "/" + long, "/" + long},
		{"https://www.example.com/a/b", "https://other.org/", "other.org"},
		{"https://www.example.com/a/b", "https://www.other.org/p", "other.org/p"},
		{"https://www.example.com/a/b", "https://other.org/" + long, "other.org/" + strings.Repeat("p", 49) + "…"},
		{"https://news.bbc.co.uk/x", "https://www.bbc.co.uk/y", "/y"},
		{"https://b.github.io/", "https://a.github.io/x", "a.github.io/x"},
		{"https://b.github.io/", "https://b.github.io/x", "/x"},
		{"http://127.0.0.1:8080/", "http://127.0.0.1:8080/x", "/x"},
		{"http://127.0.0.1:8080/", "http://127.0.0.1:9090/x", "127.0.0.1:9090/x"},
		{"http://localhost:3000/", "http://localhost:3000/api", "/api"},
		{"https://example.com/", "mailto:ada@example.com", "mailto:ada@example.com"},
		{"https://example.com/", "javascript:void(0)", ""},
		{"https://example.com/", "data:text/html;base64,PGgxPg==", "data:…"},
		{"", "https://example.com/c", "example.com/c"},
	}
	for _, c := range cases {
		if got := SiteURL(c.href, c.page); got != c.want {
			t.Errorf("SiteURL(%q) on %q = %q, want %q", c.href, c.page, got, c.want)
		}
	}
}
