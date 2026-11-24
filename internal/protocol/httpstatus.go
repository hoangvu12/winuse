package protocol

// The HTTP status note of navigate, back, forward and reload
// (bladebro-ideas ticket 02): the helper prints it after the ok line when
// the main document answered 4xx or 5xx, and --json reads its code back
// into the http_status field.

import (
	"fmt"
	"strconv"
	"strings"
)

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

// HTTPStatusIn is the code of the HTTP status note in a command's output,
// 0 when it has none.
func HTTPStatusIn(out string) int {
	for _, line := range strings.Split(out, "\n") {
		rest, ok := strings.CutPrefix(line, httpStatusNote)
		if !ok {
			continue
		}
		var code int
		if _, err := fmt.Sscanf(rest, "%d", &code); err == nil && code >= 100 && code <= 999 {
			return code
		}
	}
	return 0
}
