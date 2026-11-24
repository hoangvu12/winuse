package protocol

import "testing"

func TestHTTPStatusNote(t *testing.T) {
	cases := []struct {
		code      int
		reason    string
		emptyBody bool
		want      string
	}{
		{404, "Not Found", false, "Note: the Page answered HTTP 404 Not Found"},
		{429, "Too Many Requests", true, "Note: the Page answered HTTP 429 Too Many Requests with an empty body; Chrome shows its error page"},
		{499, "", false, "Note: the Page answered HTTP 499"},
	}
	for _, c := range cases {
		if got := HTTPStatusNote(c.code, c.reason, c.emptyBody); got != c.want {
			t.Errorf("HTTPStatusNote(%d) = %q, want %q", c.code, got, c.want)
		}
	}
}

func TestHTTPStatusIn(t *testing.T) {
	for out, want := range map[string]int{
		"ok https://a.test/\n": 0,
		"ok https://a.test/missing\nNote: the Page answered HTTP 404 Not Found\n":                                                                                  404,
		"ok https://a.test/x (the page was still interactive after 9s; winuse wait 301 load waits longer)\nNote: the Page answered HTTP 503 Service Unavailable\n": 503,
		"ok https://a.test/l\nNote: the Page answered HTTP 429 Too Many Requests with an empty body; Chrome shows its error page\n":                                429,
		"ok https://a.test/\nthe Page answered HTTP 404 in its text\n":                                                                                             0,
	} {
		if got := HTTPStatusIn(out); got != want {
			t.Errorf("HTTPStatusIn(%q) = %d, want %d", out, got, want)
		}
	}
}
