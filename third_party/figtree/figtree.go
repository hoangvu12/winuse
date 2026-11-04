// Package figtree embeds Figtree Bold (SIL Open Font License 1.1; see
// OFL.txt next to it), the Agent Cursor's name tag font.
package figtree

import _ "embed"

// Bold is Figtree-Bold.ttf, unmodified: the family "Figtree" at weight 700.
//
//go:embed Figtree-Bold.ttf
var Bold []byte
