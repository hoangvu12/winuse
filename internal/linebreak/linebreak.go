// Package linebreak compares texts the way a read-back must (headless-max
// ticket 24): CRLF, CR and LF are one line break. A field stores its line
// breaks its own way (a RichEdit such as Windows 11 Notepad's reads CRLF
// back as CR, a web text area as LF), so a write that reads back with
// other line breaks was applied as sent.
package linebreak

import "strings"

// Normalize turns every CRLF and lone CR in s into LF.
func Normalize(s string) string {
	if !strings.ContainsRune(s, '\r') {
		return s
	}
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
}

// Same reports whether a and b are the same text, line breaks aside.
func Same(a, b string) bool { return Normalize(a) == Normalize(b) }
