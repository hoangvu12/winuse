package win

import (
	"strconv"
	"strings"
)

// FormatWindows renders the `list-windows` output: one line per Window,
//
//	<Window ID>\t<App>\t<title>
//
// in the given order (z-order, topmost first). Tab-separated so a harness
// can split lines unambiguously (split on "\t" with limit 3: the title may
// contain anything). Empty output means no visible Window.
func FormatWindows(ws []WindowInfo) string {
	var b strings.Builder
	for _, w := range ws {
		b.WriteString(strconv.FormatUint(w.ID, 10))
		b.WriteByte('\t')
		b.WriteString(w.App)
		b.WriteByte('\t')
		b.WriteString(w.Title)
		b.WriteByte('\n')
	}
	return b.String()
}
