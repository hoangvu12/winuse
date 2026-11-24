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
