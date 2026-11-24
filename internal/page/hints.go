package page

import (
	"net/url"
	"path"
	"strings"
	"unicode"
)

// Hints on unnamed or duplicate controls (learn-from 04, agent-browser
// ab#2578): for each control of a document whose accessible name is empty
// or repeats (its role and name shared with another control of the
// document), what the DOMSnapshot the read already took says of it: an
// icon (an SVG sprite's <use href>, an <svg>'s id, data-icon or <title>,
// an icon library's class), an image's file name, a test id, a field
// name or element id, and the text of the row, list item or card it sits
// in. It is computed from the snapshot alone: no command per Element.
// wstate.Hints decides where a view prints it.

// NodeHint is what the snapshot says of one control besides its name.
type NodeHint struct {
	BackendNodeID int64  `json:"backendNodeId"`
	Hint          string `json:"hint,omitempty"` // "icon trash, testid del-1"
	Row           string `json:"row,omitempty"`  // the text of its row, list item or card, its own text left out
}

// The caps of the hint pass.
const (
	hintCap      = 300 // controls hinted per document
	hintScanCap  = 64  // nodes of a control's subtree looked at for an icon or image
	hintPartCap  = 40  // characters of one part
	hintRowCap   = 80  // characters of a row's text kept (wstate prints fewer)
	hintRowDepth = 12  // ancestors looked at for the row
)

// hintRoles are the Window State roles of the controls that get hints;
// wstate.Hints prints them for the same roles.
var hintRoles = map[string]bool{
	"button": true, "link": true, "check box": true, "radio button": true, "menu item": true,
	"edit": true, "combo box": true, "slider": true, "spinner": true,
}

// iconPrefixes are the icon libraries' class prefixes (agent-browser's).
var iconPrefixes = []string{"lucide-", "fa-", "bi-", "mdi-", "ri-", "ti-", "icon-", "glyphicon-", "codicon-", "octicon-"}

// iconClassNoise are icon classes that name no icon.
var iconClassNoise = setOf("fa-solid", "fa-regular", "fa-light", "fa-thin", "fa-brands", "fa-duotone", "fa-sharp", "fa-fw", "fa-lg", "fa-sm", "fa-xs", "fa-2x", "fa-3x", "icon-sm", "icon-lg", "icon-md")

// hintTargets are the backendNodeIds of the controls in nodes whose name is
// empty or repeats, plus the clickables whose text is empty or repeats.
func hintTargets(nodes []AXNode, clickables []Clickable) map[int64]bool {
	type key struct{ role, name string }
	type ctl struct {
		id  int64
		key key
	}
	ctls := make([]ctl, 0, len(clickables)+64)
	count := make(map[key]int, len(clickables)+64)
	add := func(id int64, role, name string) {
		k := key{role, plainName(name)}
		if k.name == "" {
			k.role = "" // every unnamed control is a target
		}
		ctls = append(ctls, ctl{id, k})
		count[k]++
	}
	for i := range nodes {
		n := &nodes[i]
		if n.Ignored || n.BackendDOMNodeID == 0 {
			continue
		}
		if role, ok := hintRole(n.Role); ok {
			add(n.BackendDOMNodeID, role, n.Name.String())
		}
	}
	for _, c := range clickables {
		add(c.BackendNodeID, "clickable", c.Text)
	}
	out := map[int64]bool{}
	for _, c := range ctls {
		if c.key.name == "" || count[c.key] > 1 {
			out[c.id] = true
		}
	}
	return out
}

// plainName is Collapse(s), without its work for the usual name: printable
// ASCII with single inner spaces.
func plainName(s string) string {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 0x21 && (c != ' ' || i == 0 || i == len(s)-1 || s[i-1] == ' ') || c >= 0x7f {
			return Collapse(s)
		}
	}
	return s
}

// hintRole is the Window State role of a CDP role that gets hints. Every
// node's role is looked at, so a plain JSON string is matched in place,
// with no decoding or allocation.
func hintRole(v *AXValue) (string, bool) {
	if v == nil {
		return "", false
	}
	raw := v.Value
	if len(raw) < 2 || len(raw) > 34 || raw[0] != '"' || raw[len(raw)-1] != '"' {
		r := rule(v.String())
		return r.role, hintRoles[r.role]
	}
	var buf [32]byte
	low := buf[:0]
	for _, c := range raw[1 : len(raw)-1] {
		if 'A' <= c && c <= 'Z' {
			c += 'a' - 'A'
		}
		low = append(low, c)
	}
	r, ok := roleTable[string(low)]
	return r.role, ok && hintRoles[r.role]
}

// hints is the hint pass over the document v: the hints of the targets
// it holds, at most hintCap.
func (v *docView) hints(targets map[int64]bool) []NodeHint {
	if len(targets) == 0 {
		return nil
	}
	var out []NodeHint
	rows := map[int]string{}
	for i := 0; i < v.n && len(out) < hintCap; i++ {
		if !targets[v.d.Nodes.BackendNodeID[i]] || !v.isElement(i) || v.outside[i] {
			continue
		}
		h := NodeHint{BackendNodeID: v.d.Nodes.BackendNodeID[i], Hint: v.ownHint(i)}
		if row := v.row(i); row >= 0 {
			text, ok := rows[row]
			if !ok {
				text = jsCollapse(v.innerText(row))
				rows[row] = text
			}
			h.Row = rowText(text, jsCollapse(v.innerText(i)))
		}
		if h.Hint != "" || h.Row != "" {
			out = append(out, h)
		}
	}
	return out
}

// ownHint is a control's icon, image and identifying attribute, joined
// with ", ": "icon trash, img avatar.png, testid delete-1".
func (v *docView) ownHint(i int) string {
	var parts []string
	icon, img := v.iconAndImage(i)
	if icon != "" {
		parts = append(parts, "icon "+icon)
	}
	if img != "" {
		parts = append(parts, "img "+img)
	}
	for _, a := range []struct{ attr, word string }{
		{"data-testid", "testid"}, {"data-test-id", "testid"}, {"data-test", "testid"}, {"data-cy", "testid"},
		{"name", "name"}, {"id", "id"},
	} {
		if val, ok := v.attr(i, a.attr); ok {
			if val = jsCollapse(val); val != "" && !generated(val) {
				parts = append(parts, a.word+" "+CutRunes(val, hintPartCap))
				break
			}
		}
	}
	return strings.Join(parts, ", ")
}

// iconAndImage looks at the control and its first hintScanCap descendants
// for an icon and an image: the first of each found.
func (v *docView) iconAndImage(i int) (icon, img string) {
	seen := 0
	var walk func(k int)
	walk = func(k int) {
		if seen >= hintScanCap || icon != "" && img != "" {
			return
		}
		seen++
		if v.isElement(k) && !v.outside[k] {
			if icon == "" {
				icon = v.iconOf(k)
			}
			if img == "" && strings.EqualFold(v.name(k), "IMG") {
				if src, ok := v.attr(k, "src"); ok {
					img = fileName(src)
				}
			}
		}
		for _, c := range v.kids[k] {
			walk(c)
		}
	}
	walk(i)
	return icon, img
}

// iconOf is the icon one element names: a <use> sprite reference, an
// <svg>'s data-icon, aria-label-less <title> or id, or an icon library's
// class.
func (v *docView) iconOf(k int) string {
	name := v.name(k)
	switch strings.ToLower(name) {
	case "use":
		for _, a := range []string{"href", "xlink:href"} {
			if h, ok := v.attr(k, a); ok {
				if _, frag, found := strings.Cut(h, "#"); found && frag != "" {
					return CutRunes(frag, hintPartCap)
				}
			}
		}
	case "svg":
		if d, ok := v.attr(k, "data-icon"); ok && d != "" {
			return CutRunes(d, hintPartCap)
		}
		for _, c := range v.kids[k] {
			if strings.EqualFold(v.name(c), "title") {
				if t := jsCollapse(v.textContent(c)); t != "" {
					return CutRunes(t, hintPartCap)
				}
			}
		}
		if id, ok := v.attr(k, "id"); ok && id != "" && !generated(id) {
			return CutRunes(id, hintPartCap)
		}
	}
	if d, ok := v.attr(k, "data-icon"); ok && d != "" {
		return CutRunes(d, hintPartCap)
	}
	if class, ok := v.attr(k, "class"); ok {
		for _, c := range strings.Fields(class) {
			lc := strings.ToLower(c)
			if iconClassNoise[lc] {
				continue
			}
			for _, p := range iconPrefixes {
				if strings.HasPrefix(lc, p) && len(lc) > len(p) {
					return CutRunes(c, hintPartCap)
				}
			}
		}
	}
	return ""
}

// row is the nearest ancestor of i that is a row, a list item or a card:
// a <tr>, <li> or <article>, an element whose role is row, listitem or
// article, or one with a class that names a card; -1 for none.
func (v *docView) row(i int) int {
	k := v.parent(i)
	for depth := 0; k >= 0 && v.isElement(k) && depth < hintRowDepth; depth++ {
		switch strings.ToUpper(v.name(k)) {
		case "TR", "LI", "ARTICLE":
			return k
		case "BODY", "HTML":
			return -1
		}
		if r, ok := v.attr(k, "role"); ok {
			switch strings.ToLower(strings.TrimSpace(r)) {
			case "row", "listitem", "article":
				return k
			}
		}
		if class, ok := v.attr(k, "class"); ok {
			for _, c := range strings.Fields(class) {
				if strings.Contains(strings.ToLower(c), "card") {
					return k
				}
			}
		}
		k = v.parent(k)
	}
	return -1
}

// rowText is a row's text with the control's own text taken out once,
// cut to hintRowCap.
func rowText(row, own string) string {
	if own != "" {
		row = jsCollapse(strings.Replace(row, own, " ", 1))
	}
	return CutRunes(row, hintRowCap)
}

// fileName is an image URL's last path segment ("" for a data: URL).
func fileName(src string) string {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(src)), "data:") {
		return ""
	}
	p := src
	if u, err := url.Parse(src); err == nil {
		p = u.Path
	}
	name := path.Base(strings.TrimRight(p, "/"))
	if name == "." || name == "/" || generated(name) {
		return ""
	}
	return CutRunes(name, hintPartCap)
}

// generated reports a value that looks made by a build or a framework
// rather than by a person (agent-browser's rule: three digits in a row),
// or a React useId (":r1:").
func generated(s string) bool {
	if strings.HasPrefix(s, ":") && strings.HasSuffix(s, ":") {
		return true
	}
	run := 0
	for _, r := range s {
		if unicode.IsDigit(r) {
			if run++; run >= 3 {
				return true
			}
		} else {
			run = 0
		}
	}
	return false
}

// CutRunes cuts s to n runes, the "…" that marks the cut included: the one
// cut of a Page line's parts, link URLs and log entries.
func CutRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
