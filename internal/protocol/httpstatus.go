package protocol

// The HTTP status note of navigate, back, forward and reload
// (bladebro-ideas ticket 02): the helper prints it after the ok line when
// the main document answered 4xx or 5xx; under --json its code is the
// http_status field (Response.HTTPStatus).

import "strconv"

// httpStatusNote starts the note's line.
const httpStatusNote = "Note: the Page answered HTTP "

// HTTPStatusNote is the note for a 4xx/5xx answer: the code, its reason
// phrase when known, and for an empty body that Chrome replaced with its
// own error page, that.
func HTTPStatusNote(code int, reason string, emptyBody bool) string {
	s := httpStatusNote + strconv.Itoa(code)
	if reason != "" {
		s += " " + reason
	}
	if emptyBody {
		s += " with an empty body; Chrome shows its error page"
	}
	return s
}
